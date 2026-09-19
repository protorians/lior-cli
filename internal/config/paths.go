package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/protorians/lior-cli/internal/i18n"
)

// Well-known directory and file names within a Liorian project.
const (
	ConfigFileName     = "lorian.config.json"
	LorianConfigName   = "lorian.config.toml"
	ExternalModulesDir  = "external_modules"
	InternalModulesDir  = "src/modules"
	PublicAssetsDir     = "public/assets"
	AppSrcDir           = "src/app"
	LorianDir         = ".lorian"
	LorianBuildsDir   = ".lorian/build"
	ManifestFileName    = "manifest.json"
	ModuleEntryFileName = "index.tsx"
	// ArchiveExt is the extension of built module archives (`<name>-<version>.SenMod`).
	ArchiveExt = ".SenMod"
)

// IsProjectRoot reports whether dir looks like a Liorian project root.
func IsProjectRoot(dir string) bool {
	if fileExists(filepath.Join(dir, LorianConfigName)) {
		return true
	}
	if fileExists(filepath.Join(dir, ConfigFileName)) {
		return true
	}
	if dirExists(filepath.Join(dir, ExternalModulesDir)) {
		return true
	}
	return false
}

// FindProjectRoot walks up from start (default: current directory) looking
// for a Liorian project root. Returns an error when none is found.
func FindProjectRoot(start string) (string, error) {
	dir := start
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("%s: %w", i18n.T("config.error.cwd"), err)
		}
	}
	for {
		if IsProjectRoot(dir) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New(i18n.Tf("config.error.no_project",
				LorianConfigName, ExternalModulesDir))
		}
		dir = parent
	}
}

// Absolute returns path joined to root when root is non-empty.
func Absolute(root, path string) string {
	return filepath.Join(root, path)
}

// ConfigPath returns the CLI config file path for a project root.
func ConfigPath(root string) string {
	return filepath.Join(root, ConfigFileName)
}

// ModuleDir returns the directory of a module.
func ModuleDir(root, module string) string {
	return filepath.Join(root, ExternalModulesDir, module)
}

// ModuleAssetsDir returns the assets directory of a module.
func ModuleAssetsDir(root, module string) string {
	return filepath.Join(root, PublicAssetsDir, module)
}

// ModuleAppSrcDir returns the app source directory of a module.
func ModuleAppSrcDir(root, module string) string {
	return filepath.Join(root, AppSrcDir, module)
}

// BuildDir returns the `.lorian/build/` directory for a project root.
func BuildDir(root string) string {
	return filepath.Join(root, LorianBuildsDir)
}

// ManifestPath returns the path of a module manifest.
func ManifestPath(root, module string) string {
	return filepath.Join(ModuleDir(root, module), ManifestFileName)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
