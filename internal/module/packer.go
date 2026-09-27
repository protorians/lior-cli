package module

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
)

// Archive limits (spec `module-installation.md` NFR-006..008): maximum archive
// size, maximum entries per archive (anti-zip-bomb) and maximum overall
// decompression ratio.
const (
	MaxArchiveSize   = 50 * 1024 * 1024
	MaxArchiveFiles  = 5000
	MaxArchiveRatio  = 100
	MaxArchiveFileMB = 10
)

// blockedArchiveExts are never packed: executables and platform packages have
// no place in a module artefact (spec §7.2, fail-closed archive audit).
var blockedArchiveExts = map[string]bool{
	".so": true, ".dylib": true, ".dll": true, ".exe": true,
	".bat": true, ".cmd": true, ".msi": true, ".dmg": true,
	".sh": true, ".node": true,
}

// Packer builds `.liozip` archives (renamed ZIP) for a module.
type Packer struct {
	Root    string
	Version string
	// Out overrides the archive destination (e.g. `--out acme-crm.liozip`).
	// When empty, the archive is written to `.lorian/build/`.
	Out string
}

// PackResult describes a created archive.
type PackResult struct {
	Module           string
	Version          string
	Path             string
	Size             int64
	Checksum         string // SHA-256 hex of the archive bytes (FR-004)
	ManifestChecksum string // SHA-256 hex of the canonical manifest
	FileCount        int
}

// Pack builds and moves the archive of `name` into `.lorian/build/` (or `Out`
// when set).
//
// The module is resolved from the workspace source tree first
// (`modules/<name>`, D5): the archive then follows the isolated-runtime
// layout — `manifest.json` + `src/**` (D4) + `artifact/**` (§4.4). A module
// that only exists in the legacy installation tree (`library/modules/<name>`)
// still packs the legacy layout until the phase-8 migration.
func (p *Packer) Pack(name string) (*PackResult, error) {
	switch {
	case pkg.DirExists(config.WorkspaceModuleDir(p.Root, name)):
		return p.packModern(name, config.WorkspaceModuleDir(p.Root, name))
	case pkg.DirExists(config.ModuleDir(p.Root, name)):
		return p.packLegacy(name, config.ModuleDir(p.Root, name))
	default:
		return nil, errors.New(i18n.Tf("val.module_not_found", name,
			config.WorkspaceModulesDir+"|"+config.ExternalModulesDir))
	}
}

// packModern packs an isolated-runtime module (D4/§4.4): the module sources
// under `src/`, the built payload under `artifact/`, the manifest at the
// archive root. The §4.4 validation rules are blocking, and so is the
// `tsc --noEmit` typecheck (D6, rule 2).
func (p *Packer) packModern(name, moduleSrc string) (*PackResult, error) {
	v := &Validator{}
	res, err := v.ValidateModuleDir(moduleSrc)
	if err != nil {
		return nil, err
	}
	if res.HasErrors() {
		return nil, errors.New(i18n.Tf("pack.error.validation", name, res.ErrorCount()))
	}

	m, err := LoadManifest(filepath.Join(moduleSrc, config.ManifestFileName))
	if err != nil {
		return nil, err
	}

	version := p.Version
	if version == "" {
		version = m.Version
	} else if !isSemver(version) {
		return nil, errors.New(i18n.Tf("pack.error.version", version))
	}

	if err := runTypecheck(moduleSrc); err != nil {
		return nil, err
	}

	archivePath, err := p.archiveDestination(name, version)
	if err != nil {
		return nil, err
	}

	manifestChecksum := ManifestChecksum(m)
	if err := p.createModernArchive(archivePath, moduleSrc, m); err != nil {
		return nil, err
	}
	return p.finalizeArchive(archivePath, name, version, manifestChecksum)
}

// packLegacy is the pre-isolated-runtime pack flow: the module tree, its page
// and its assets keep their workspace-relative archive paths.
func (p *Packer) packLegacy(name, moduleSrc string) (*PackResult, error) {
	v := &Validator{Root: p.Root}
	res, err := v.ValidateModule(name)
	if err != nil {
		return nil, err
	}
	if res.HasErrors() {
		return nil, errors.New(i18n.Tf("pack.error.validation", name, res.ErrorCount()))
	}

	m, err := LoadManifest(config.ManifestPath(p.Root, name))
	if err != nil {
		return nil, err
	}

	version := p.Version
	if version == "" {
		version = m.Version
	} else if !isSemver(version) {
		return nil, errors.New(i18n.Tf("pack.error.version", version))
	}

	archivePath, err := p.archiveDestination(name, version)
	if err != nil {
		return nil, err
	}

	// The app sources live in `src/app/<url>/`: the page folder of the
	// deployed module manifest (its `uri`, falling back to the identifier).
	pageDir := strings.TrimPrefix(m.URI, "/")
	if pageDir == "" {
		pageDir = m.ID
	}
	appSrc := config.ModuleAppSrcDir(p.Root, pageDir)
	assetsSrc := config.ModuleAssetsDir(p.Root, name)

	manifestChecksum := ManifestChecksum(m)
	if err := p.createArchive(archivePath, moduleSrc, appSrc, assetsSrc, m); err != nil {
		return nil, err
	}
	return p.finalizeArchive(archivePath, name, version, manifestChecksum)
}

// archiveDestination resolves the output path of the archive: `Out` when set,
// `.lorian/build/<name>-<version>.liozip` otherwise.
func (p *Packer) archiveDestination(name, version string) (string, error) {
	archivePath := strings.TrimSpace(p.Out)
	if archivePath == "" {
		buildDir := config.BuildDir(p.Root)
		if err := pkg.CreateDir(buildDir); err != nil {
			return "", err
		}
		return filepath.Join(buildDir, fmt.Sprintf("%s-%s%s", name, version, config.ArchiveExt)), nil
	}
	if dir := filepath.Dir(archivePath); dir != "" {
		if err := pkg.CreateDir(dir); err != nil {
			return "", err
		}
	}
	return archivePath, nil
}

// finalizeArchive stats, size-checks and digests a freshly written archive.
func (p *Packer) finalizeArchive(archivePath, name, version, manifestChecksum string) (*PackResult, error) {
	info, err := os.Stat(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat archive: %w", err)
	}
	if info.Size() > MaxArchiveSize {
		os.Remove(archivePath)
		return nil, errors.New(i18n.Tf("pack.error.max_size", MaxArchiveSize/(1024*1024)))
	}

	checksum, files, err := archiveDigest(archivePath)
	if err != nil {
		os.Remove(archivePath)
		return nil, err
	}

	return &PackResult{
		Module:           name,
		Version:          version,
		Path:             archivePath,
		Size:             info.Size(),
		Checksum:         checksum,
		ManifestChecksum: manifestChecksum,
		FileCount:        files,
	}, nil
}

// runTypecheck enforces rule 2 of §4.4: `tsc --noEmit` passes before an
// archive is produced (D6: blocking typecheck). The module `typecheck` npm
// script is preferred; bare `tsc` is the fallback. Fail-closed: an
// unavailable toolchain is an error, never a silent pass.
func runTypecheck(moduleDir string) error {
	np := pkg.LoadNodePackage(filepath.Join(moduleDir, "package.json"))

	bun, _ := exec.LookPath("bun")
	tsc, _ := exec.LookPath("tsc")

	var cmd *exec.Cmd
	switch {
	case np.HasScript("typecheck") && bun != "":
		cmd = exec.Command(bun, "run", "typecheck")
	case tsc != "":
		cmd = exec.Command(tsc, "--noEmit")
	case bun != "":
		cmd = exec.Command(bun, "x", "tsc", "--noEmit")
	default:
		return errors.New(i18n.T("pack.error.no_typecheck"))
	}
	cmd.Dir = moduleDir
	var output bytes.Buffer
	cmd.Stderr = &output
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s\n%s", i18n.T("pack.error.typecheck_failed"), strings.TrimSpace(output.String()))
	}
	return nil
}

// ManifestChecksum returns the SHA-256 hex of the canonical manifest JSON:
// keys sorted, UTF-8, no whitespace (spec §7.1, `manifestChecksum`).
func ManifestChecksum(m *Manifest) string {
	sum := sha256.Sum256(CanonicalManifestJSON(m))
	return hex.EncodeToString(sum[:])
}

// CanonicalManifestJSON serializes a manifest with the shared canonical-JSON
// contract (object keys sorted recursively, no whitespace, no HTML escaping) —
// byte-identical to the `canonicalJson` normalization the server applies to
// the uploaded manifest before hashing it (`manifestChecksumOf`). A struct-
// order `json.Marshal` would produce a checksum the server never recomputes.
func CanonicalManifestJSON(m *Manifest) []byte {
	data, err := pkg.CanonicalJSON(m)
	if err != nil {
		return nil
	}
	return data
}

// archiveDigest returns the SHA-256 hex of an archive file and its entry
// count, enforcing the anti-zip-bomb ratio (NFR-008).
func archiveDigest(archivePath string) (string, int, error) {
	data, err := os.ReadFile(archivePath)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read the archive: %w", err)
	}
	sum := sha256.Sum256(data)
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", 0, fmt.Errorf("not a valid .liozip archive: %w", err)
	}
	var uncompressed int64
	for _, f := range zr.File {
		uncompressed += int64(f.UncompressedSize64)
	}
	if int64(len(data))*MaxArchiveRatio < uncompressed {
		return "", 0, errors.New(i18n.T("pack.error.zip_bomb"))
	}
	return hex.EncodeToString(sum[:]), len(zr.File), nil
}

// createModernArchive zips an isolated-runtime module into `dest`
// (spec §4.4): the manifest at the archive root, the module sources under
// `src/` (D4) and the built payload under `artifact/`. Build tooling state
// (`node_modules`, dependency caches, VCS dirs) is never packed.
func (p *Packer) createModernArchive(dest, moduleSrc string, m *Manifest) error {
	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("failed to create archive %s: %w", dest, err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	counter := &zipCounter{zw: zw}
	if err := counter.addFile(
		filepath.Join(moduleSrc, config.ManifestFileName), config.ManifestFileName); err != nil {
		return err
	}

	// The manifest travels at the archive root only; it is not module
	// source (D4).
	if err := counter.addTree(moduleSrc, "src", func(name string) bool {
		return name == config.ManifestFileName || excludedSourceDirs[name]
	}); err != nil {
		return err
	}
	artifactDir := filepath.Join(moduleSrc, m.EffectiveArtifactDir())
	return counter.addTree(artifactDir, config.ModuleArtifactDir, nil)
}

// zipCounter writes archive entries while enforcing the shared archive
// limits (entry count, per-file size, blocked extensions, unsafe paths).
type zipCounter struct {
	zw           *zip.Writer
	count        int
	uncompressed int64
}

// addTree walks src and writes every regular file under the archive prefix.
// skip receives each entry name (directory or file) and returns true to
// exclude it.
func (c *zipCounter) addTree(src, prefix string, skip func(name string) bool) error {
	if !pkg.DirExists(src) {
		return nil
	}
	return filepath.Walk(src, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if skip != nil && skip(info.Name()) && filePath != src {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(src, filePath)
		if err != nil {
			return err
		}
		return c.addFile(filePath, path.Join(prefix, filepath.ToSlash(rel)))
	})
}

// addFile writes one file into the archive at the given slash path, after
// the fail-closed checks shared with the legacy packer.
func (c *zipCounter) addFile(filePath, archivePath string) error {
	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refused symlink in archive: %s", filePath)
	}
	if info.Size() > MaxArchiveFileMB*1024*1024 {
		return fmt.Errorf("file too large for archive (max %d MB): %s", MaxArchiveFileMB, filePath)
	}
	if blockedArchiveExts[strings.ToLower(filepath.Ext(filePath))] {
		return fmt.Errorf("executable refused in archive: %s", filePath)
	}
	if archivePath == "" || strings.HasPrefix(archivePath, "/") || strings.HasPrefix(archivePath, "../") {
		return fmt.Errorf("unsafe path in archive: %s", archivePath)
	}
	c.count++
	if c.count > MaxArchiveFiles {
		return errors.New(i18n.Tf("pack.error.max_files", MaxArchiveFiles))
	}
	c.uncompressed += info.Size()
	w, err := c.zw.Create(archivePath)
	if err != nil {
		return fmt.Errorf("failed to write to archive: %w", err)
	}
	in, err := os.Open(filePath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, in); err != nil {
		in.Close()
		return fmt.Errorf("failed to copy %s into archive: %w", filePath, err)
	}
	return in.Close()
}

// createArchive zips `moduleSrc` (prefixed `library/modules/<name>/`),
// `appSrc` (prefixed `src/app/<name>/`), `assetsSrc` (prefixed
// `public/assets/<name>/`) when present, and the canonical `manifest.json` at
// the archive root (spec TECH-002) into `dest`.
func (p *Packer) createArchive(dest, moduleSrc, appSrc, assetsSrc string, m *Manifest) error {
	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("failed to create archive %s: %w", dest, err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	var count int
	var uncompressed int64
	addToZip := func(src string) error {
		if !pkg.DirExists(src) {
			return nil
		}
		return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("refused symlink in archive: %s", path)
			}
			if info.Size() > MaxArchiveFileMB*1024*1024 {
				return fmt.Errorf("file too large for archive (max %d MB): %s", MaxArchiveFileMB, path)
			}
			if blockedArchiveExts[strings.ToLower(filepath.Ext(path))] {
				return fmt.Errorf("executable refused in archive: %s", path)
			}
			rel, err := filepath.Rel(p.Root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if rel == "" || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "../") {
				return fmt.Errorf("unsafe path in archive: %s", rel)
			}
			count++
			if count > MaxArchiveFiles {
				return errors.New(i18n.Tf("pack.error.max_files", MaxArchiveFiles))
			}
			uncompressed += info.Size()
			w, err := zw.Create(rel)
			if err != nil {
				return fmt.Errorf("failed to write to archive: %w", err)
			}
			in, err := os.Open(path)
			if err != nil {
				return err
			}
			if _, err := io.Copy(w, in); err != nil {
				in.Close()
				return fmt.Errorf("failed to copy %s into archive: %w", path, err)
			}
			return in.Close()
		})
	}

	if err := addToZip(moduleSrc); err != nil {
		return err
	}
	if err := addToZip(appSrc); err != nil {
		return err
	}
	if err := addToZip(assetsSrc); err != nil {
		return err
	}

	// Canonical manifest at the archive root: the installation chain
	// (spec §7.1) verifies the signature against these exact bytes.
	manifestJSON := append(CanonicalManifestJSON(m), '\n')
	count++
	w, err := zw.Create("manifest.json")
	if err != nil {
		return fmt.Errorf("failed to write the manifest to archive: %w", err)
	}
	if _, err := w.Write(manifestJSON); err != nil {
		return fmt.Errorf("failed to write the manifest to archive: %w", err)
	}
	uncompressed += int64(len(manifestJSON))
	return nil
}
