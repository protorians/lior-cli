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
func (p *Packer) Pack(name string) (*PackResult, error) {
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

	archivePath := strings.TrimSpace(p.Out)
	if archivePath == "" {
		buildDir := config.BuildDir(p.Root)
		if err := pkg.CreateDir(buildDir); err != nil {
			return nil, err
		}
		archivePath = filepath.Join(buildDir, fmt.Sprintf("%s-%s%s", name, version, config.ArchiveExt))
	} else if dir := filepath.Dir(archivePath); dir != "" {
		if err := pkg.CreateDir(dir); err != nil {
			return nil, err
		}
	}

	// The app sources live in `src/app/<url>/`: the page folder of the
	// deployed module manifest (its `uri`, falling back to the identifier).
	pageDir := strings.TrimPrefix(m.URI, "/")
	if pageDir == "" {
		pageDir = m.ID
	}
	moduleSrc := config.ModuleDir(p.Root, name)
	appSrc := config.ModuleAppSrcDir(p.Root, pageDir)
	assetsSrc := config.ModuleAssetsDir(p.Root, name)

	manifestChecksum := ManifestChecksum(m)
	if err := p.createArchive(archivePath, moduleSrc, appSrc, assetsSrc, m); err != nil {
		return nil, err
	}

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
