package module

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// Packer builds `.LiorArtifactPackage` archives (renamed ZIP) for a module.
type Packer struct {
	Root    string
	Version string
	// Out overrides the archive destination (e.g. `--out acme-crm.LiorArtifactPackage`).
	// When empty, the archive is written to `.liorian/build/`.
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

// Pack builds and moves the archive of `name` into `.liorian/build/` (or `Out`
// when set).
//
// The module is resolved from the workspace source tree first
// (`modules/<name>`, D5): the archive then follows the isolated-runtime
// layout — `manifest.json` + `src/**` (D4) + `artifact/**` (§4.4). A module
// that only exists in the legacy installation tree (`library/modules/<name>`)
// still packs the legacy layout until the phase-8 migration.
func (p *Packer) Pack(name string) (*PackResult, error) {
	// Une seule résolution pour toute la chaîne : `config.ResolveModuleDir`
	// cherche l'arbre source puis l'arbre d'installation, et accepte l'identité
	// déclarée par le manifeste comme le nom de répertoire. Réimplémenter la
	// recherche ici faisait dépendre le pack du seul nom de dossier.
	moduleDir := config.ResolveModuleDir(p.Root, name)
	if moduleDir == "" {
		return nil, errors.New(i18n.Tf("val.module_not_found", name,
			config.WorkspaceModulesDir+"|"+config.ExternalModulesDir))
	}
	if config.IsWorkspaceModuleDir(p.Root, moduleDir) {
		return p.packModern(name, moduleDir)
	}
	return p.packLegacy(name, moduleDir)
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

	if err := RunTypecheck(moduleSrc); err != nil {
		return nil, err
	}

	archivePath, err := p.archiveDestination(name, version)
	if err != nil {
		return nil, err
	}

	manifestChecksum, err := ManifestFileChecksum(moduleSrc)
	if err != nil {
		return nil, err
	}
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

	manifestChecksum, err := ManifestFileChecksum(moduleSrc)
	if err != nil {
		return nil, err
	}
	if err := p.createArchive(archivePath, moduleSrc, appSrc, assetsSrc, m); err != nil {
		return nil, err
	}
	return p.finalizeArchive(archivePath, name, version, manifestChecksum)
}

// archiveDestination resolves the output path of the archive: `Out` when set,
// `.liorian/build/<name>-<version>.LiorArtifactPackage` otherwise.
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

// RunTypecheck enforces rule 2 of §4.4: `tsc --noEmit` passes before an
// archive is produced (D6: blocking typecheck). The module `typecheck` npm
// script is preferred; bare `tsc` is the fallback. Fail-closed: an
// unavailable toolchain is an error, never a silent pass. Shared with the
// top-level `liora typecheck` command.
func RunTypecheck(moduleDir string) error {
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

// ManifestDocumentChecksum returns the SHA-256 hex of the canonical form of a
// manifest *document* — the `manifest.json` bytes parsed generically and
// re-serialized with the shared canonical-JSON contract (keys sorted, no
// whitespace, no HTML escaping).
//
// This is the `manifestChecksum` covered by the artefact signature (spec §7.1),
// and it must be computed on the document, never on the Go projection of it:
// the store recomputes it at publication (`manifestChecksumOf` in
// `@liorian/api-resources`), `api-core` recomputes it at installation
// (`module-artifact-verifier`) and the socle re-verifies it against the relayed
// attestation (E-007). A struct projection silently drops the fields the schema
// does not model and normalizes the ones it does, so the two values differ for
// virtually every real manifest — the signature then verifies locally and is
// rejected server-side with `422`.
func ManifestDocumentChecksum(document []byte) (string, error) {
	var parsed any
	if err := json.Unmarshal(document, &parsed); err != nil {
		return "", fmt.Errorf("manifest is not valid JSON: %w", err)
	}
	canonical, err := pkg.CanonicalJSON(parsed)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// ManifestFileChecksum is ManifestDocumentChecksum on the manifest file of a
// module directory.
func ManifestFileChecksum(moduleDir string) (string, error) {
	document, err := os.ReadFile(filepath.Join(moduleDir, config.ManifestFileName))
	if err != nil {
		return "", err
	}
	return ManifestDocumentChecksum(document)
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
		return "", 0, fmt.Errorf("not a valid %s archive: %w", config.ArchiveFormatLabel, err)
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
	// Development layout (`.liorian/artifact/`, D7) resolved on disk; the
	// distribution prefix stays `artifact/`.
	artifactDir := filepath.Join(moduleSrc, m.SourceArtifactDir(moduleSrc))
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

	// The manifest document at the archive root, verbatim: the signed
	// `manifestChecksum` covers the developer's own file (the store re-hashes
	// the uploaded document, the local installer re-hashes the archived one).
	// Embedding a normalized projection here made the two disagree, and the
	// signature unverifiable after install.
	manifestDocument, err := os.ReadFile(filepath.Join(moduleSrc, config.ManifestFileName))
	if err != nil {
		return fmt.Errorf("failed to read the manifest: %w", err)
	}
	count++
	w, err := zw.Create(config.ManifestFileName)
	if err != nil {
		return fmt.Errorf("failed to write the manifest to archive: %w", err)
	}
	if _, err := w.Write(manifestDocument); err != nil {
		return fmt.Errorf("failed to write the manifest to archive: %w", err)
	}
	uncompressed += int64(len(manifestDocument))
	return nil
}
