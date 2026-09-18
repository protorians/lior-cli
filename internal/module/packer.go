package module

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/protorians/liorian-cli/internal/config"
	"github.com/protorians/liorian-cli/internal/i18n"
	"github.com/protorians/liorian-cli/internal/pkg"
)

// MaxArchiveSize is the maximum allowed archive size (store limit: 50 MB).
const MaxArchiveSize = 50 * 1024 * 1024

// Packer builds `.SenMod` archives (renamed ZIP) for a module.
type Packer struct {
	Root    string
	Version string
}

// PackResult describes a created archive.
type PackResult struct {
	Module  string
	Version string
	Path    string
	Size    int64
}

// Pack builds and moves the archive of `name` into `.liorian/build/`.
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

	buildDir := config.BuildDir(p.Root)
	if err := pkg.CreateDir(buildDir); err != nil {
		return nil, err
	}

	archivePath := filepath.Join(buildDir, fmt.Sprintf("%s-%s%s", name, version, config.ArchiveExt))

	// The app sources live in `src/app/<url>/`: the page folder of the
	// deployed module manifest (its `uri`, falling back to the identifier).
	pageDir := strings.TrimPrefix(m.URI, "/")
	if pageDir == "" {
		pageDir = m.ID
	}
	moduleSrc := config.ModuleDir(p.Root, name)
	appSrc := config.ModuleAppSrcDir(p.Root, pageDir)
	assetsSrc := config.ModuleAssetsDir(p.Root, name)

	if err := p.createArchive(archivePath, moduleSrc, appSrc, assetsSrc); err != nil {
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

	return &PackResult{
		Module:  name,
		Version: version,
		Path:    archivePath,
		Size:    info.Size(),
	}, nil
}

// createArchive zips `moduleSrc` (prefixed `external_modules/<name>/`),
// `appSrc` (prefixed `src/app/<name>/`) and, when present, `assetsSrc`
// (prefixed `public/assets/<name>/`) into `dest`.
func (p *Packer) createArchive(dest, moduleSrc, appSrc, assetsSrc string) error {
	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("failed to create archive %s: %w", dest, err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

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
			rel, err := filepath.Rel(p.Root, path)
			if err != nil {
				return err
			}
			w, err := zw.Create(filepath.ToSlash(rel))
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
	return addToZip(assetsSrc)
}
