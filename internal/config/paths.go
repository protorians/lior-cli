package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
)

// Well-known directory and file names within a Liora project.
const (
	ConfigFileName     = "lorian.config.json"
	LorianConfigName   = "lorian.config.toml"
	ExternalModulesDir = "library/modules"
	InternalModulesDir = "src/modules"
	PublicAssetsDir    = "public/assets"
	AppSrcDir          = "src/app"
	LorianDir          = ".lorian"
	LorianBuildsDir    = ".lorian/build"
	ManifestFileName   = "manifest.json"
	// ModuleEntryFileName is the canonical TypeScript entry of a module
	// (spec docs/specs/applications/module-isolated-runtime.md, D6). The
	// legacy names are still read: `entry.tsx` (first isolated-runtime
	// revision) and `index.tsx` (LegacyDeclarationFileName, pre-runtime
	// declaration file).
	ModuleEntryFileName = "main.tsx"
	// LegacyEntryFileName is the entry name of modules predating the
	// `main.tsx` canonical name (first isolated-runtime revision).
	LegacyEntryFileName = "entry.tsx"
	// LegacyDeclarationFileName is the legacy module declaration file
	// (`index.tsx`, ModuleDeclarationInterface) of modules predating the
	// isolated runtime (§8.1: it disappears in the canonical layout).
	LegacyDeclarationFileName = "index.tsx"
	// ModuleArtifactSourceDir is the build output directory of a module in
	// its development tree (D7): `<module>/.liorian/artifact/` holds the
	// executable iframe payload (`index.html`, `module.js`, `assets/**`).
	// It is the layout of the module source only — the distribution keeps
	// ModuleArtifactDir.
	ModuleArtifactSourceDir = ".liorian/artifact"
	// ModuleArtifactDir is the artifact directory of the distribution
	// layout (D7): the `.LiorArtifactPackage` archive stores the built payload
	// under `artifact/**` and an installed module lands as
	// `library/modules/<id>/<version>/artifact/**`.
	ModuleArtifactDir = "artifact"
	// CurrentPointerName is the version pointer file written at
	// `library/modules/<id>/current`: it holds the active version, enabling
	// enable/disable and rollback without moving directories (§4.4).
	CurrentPointerName = "current"
	// WorkspaceModulesDir is the source directory of the first-party modules
	// at the workspace root, as opposed to ExternalModulesDir which is the
	// installation destination of a published module (spec
	// docs/specs/applications/module-isolated-runtime.md, D5 and D11):
	// `modules/<id>/` is where a module is written, `library/modules/<id>/`
	// is where an installed module lands.
	WorkspaceModulesDir = "modules"
	// ArchiveExt is the canonical extension of built module archives
	// (`<name>-<version>.LiorArtifactPackage`, a renamed ZIP — ADR-003 of the
	// module-installation spec). It supersedes `.liozip`, which superseded the
	// historical `.SenMod`/`.smp` names: none of them are read anymore, the
	// rename is a clean break.
	ArchiveExt = ".LiorArtifactPackage"
	// ArchiveFormatLabel is the human-readable name of the archive format
	// (a ZIP container behind a dedicated extension), used in help screens and
	// error messages.
	ArchiveFormatLabel = "Lior Artifact Package"
)

// IsWorkspaceRoot reports whether dir is the root of a Liorian workspace, i.e.
// it holds a `modules/` directory with at least one module in it.
//
// The presence of a module is required, not just the directory: a bare
// `modules/` folder is too weak a signal, and treating one as a project root
// would make FindProjectRoot stop at unrelated directories while walking up.
//
// A `library/` directory is excluded: its `modules/` child is the installation
// destination (D11), so `<project>/library` would otherwise validate as a
// workspace root and make FindProjectRoot return it while walking up from an
// installed module instead of the project that hosts it.
func IsWorkspaceRoot(dir string) bool {
	if filepath.Base(dir) == "library" {
		return false
	}
	return countWorkspaceModules(dir) > 0
}

// countWorkspaceModules returns the number of modules (subdirectories holding a
// manifest.json) directly under `<dir>/modules/`.
func countWorkspaceModules(dir string) int {
	entries, err := os.ReadDir(filepath.Join(dir, WorkspaceModulesDir))
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if fileExists(filepath.Join(dir, WorkspaceModulesDir, e.Name(), ManifestFileName)) {
			count++
		}
	}
	return count
}

// IsProjectRoot reports whether dir looks like a Liora project root.
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
	return IsWorkspaceRoot(dir)
}

// FindProjectRoot walks up from start (default: current directory) looking
// for a Liora project root. Returns an error when none is found.
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
				LorianConfigName, ExternalModulesDir, WorkspaceModulesDir))
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

// WorkspaceModulesDirPath returns the source directory of the first-party
// modules (`<root>/modules/`) — the development side, as opposed to
// ModuleDir which is the installation destination.
func WorkspaceModulesDirPath(root string) string {
	return filepath.Join(root, WorkspaceModulesDir)
}

// NativeModulesDirs are the roots holding the modules provided by the socle
// itself — the ones a first-party module declares in `requirements` and that
// are never published (`notification`, `media-library`, `identity`…). Both
// project layouts are searched: a standalone project keeps them in
// `src/modules/`, the monorepo keeps them in `apps/liorian-socle/src/modules/`.
var NativeModulesDirs = []string{
	InternalModulesDir,
	filepath.Join("apps", "liorian-socle", "src", "modules"),
}

// NativeModuleDir returns the socle source directory of a native module, or
// the empty string when the project holds none of the known layouts.
func NativeModuleDir(root, module string) string {
	for _, dir := range NativeModulesDirs {
		candidate := filepath.Join(root, dir, module)
		if fileExists(filepath.Join(candidate, ManifestFileName)) || dirExists(candidate) {
			return candidate
		}
	}
	return ""
}

// WorkspaceModuleDir returns the source directory of a first-party module.
func WorkspaceModuleDir(root, module string) string {
	return filepath.Join(root, WorkspaceModulesDir, module)
}

// WorkspaceModuleEntryPath returns the source path of a first-party module.
func WorkspaceModuleEntryPath(root, module string) string {
	return filepath.Join(WorkspaceModuleDir(root, module), ModuleEntryFileName)
}

// ListWorkspaceModules returns the identifiers of the first-party modules found
// under `<root>/modules/`, sorted. A directory without a manifest.json is not a
// module and is skipped.
func ListWorkspaceModules(root string) ([]string, error) {
	entries, err := os.ReadDir(WorkspaceModulesDirPath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !fileExists(filepath.Join(WorkspaceModulesDirPath(root), e.Name(), ManifestFileName)) {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// ModuleAssetsDir returns the assets directory of a module.
func ModuleAssetsDir(root, module string) string {
	return filepath.Join(root, PublicAssetsDir, module)
}

// ModuleAppSrcDir returns the app source directory of a module.
func ModuleAppSrcDir(root, module string) string {
	return filepath.Join(root, AppSrcDir, module)
}

// ModuleArtifactDirPath returns the artifact directory of a module in its
// development tree (`modules/<id>/.liorian/artifact/`, D7).
func ModuleArtifactDirPath(root, module string) string {
	return filepath.Join(WorkspaceModuleDir(root, module), ModuleArtifactSourceDir)
}

// InstalledModuleVersionsRoot returns the multi-version root of an installed
// module: `library/modules/<id>/` holds one directory per installed version
// plus the `current` pointer (D11, §4.4).
func InstalledModuleVersionsRoot(root, module string) string {
	return filepath.Join(root, ExternalModulesDir, module)
}

// InstalledModuleVersionDir returns the installation directory of one version
// of a module: `library/modules/<id>/<version>/` (manifest.json + src/** +
// artifact/**).
func InstalledModuleVersionDir(root, module, version string) string {
	return filepath.Join(InstalledModuleVersionsRoot(root, module), version)
}

// WriteCurrentPointer atomically writes the active-version pointer of an
// installed module. The pointer is a plain text file (not a symlink): it must
// survive on filesystems and platforms without symlink support.
func WriteCurrentPointer(root, module, version string) error {
	dir := InstalledModuleVersionsRoot(root, module)
	if err := pkg.CreateDir(dir); err != nil {
		return err
	}
	return pkg.WriteFile(filepath.Join(dir, CurrentPointerName), []byte(version+"\n"))
}

// ReadCurrentPointer returns the active version of an installed module, or ""
// when the pointer is absent or unreadable.
func ReadCurrentPointer(root, module string) string {
	data, err := os.ReadFile(filepath.Join(InstalledModuleVersionsRoot(root, module), CurrentPointerName))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// ResolveInstalledModuleDir returns the active installation directory of a
// module: the `current` pointer target when the module was installed
// multi-version, the legacy flat directory otherwise.
func ResolveInstalledModuleDir(root, module string) string {
	if version := ReadCurrentPointer(root, module); version != "" {
		dir := InstalledModuleVersionDir(root, module, version)
		if dirExists(dir) {
			return dir
		}
	}
	return ModuleDir(root, module)
}

// BuildDir returns the `.lorian/build/` directory for a project root.
func BuildDir(root string) string {
	return filepath.Join(root, LorianBuildsDir)
}

// ResolveModuleDir returns the directory holding a module, searching the trees
// in the order the lifecycle creates them: the source tree first
// (`modules/<id>/`, D5 — where a first-party module is developed), then the
// installation tree (`library/modules/<id>/`, D11). It returns "" when neither
// holds the module.
//
// One resolver for both trees: a command that resolves only the installation
// tree sees a first-party module as non-existent, so `sign`, `sign verify`,
// `publish` and `link` all failed on modules that `pack` handled fine.
// ResolveModuleDir returns the source directory of a module in whichever tree
// holds it, searching the first-party tree (`modules/`) before the installation
// tree (`library/modules/`).
//
// A module is addressable by its directory name *and* by the identity its
// manifest declares (`id` or `domain`): `liora create module` prints
// `liora pack <domain>` as the next step, and the first-party tree names its
// directories after the id. Without the manifest scan that instruction would
// resolve to nothing, and the freshly created module could not be packed.
func ResolveModuleDir(root, module string) string {
	if dir := WorkspaceModuleDir(root, module); dirExists(dir) {
		return dir
	}
	if dir := ModuleDir(root, module); dirExists(dir) {
		return dir
	}
	for _, tree := range []string{WorkspaceModulesDir, ExternalModulesDir} {
		if dir := matchModuleByManifest(filepath.Join(root, tree), module); dir != "" {
			return dir
		}
	}
	return ""
}

// IsWorkspaceModuleDir reports whether a resolved module directory belongs to
// the first-party source tree (`<root>/modules/`). The packer keys the
// isolated-runtime layout (D4) and the legacy one on that distinction.
func IsWorkspaceModuleDir(root, dir string) bool {
	if dir == "" || root == "" {
		return false
	}
	source, err := filepath.Abs(WorkspaceModulesDirPath(root))
	if err != nil {
		return false
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(source, abs)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// matchModuleByManifest scans a module tree for the directory whose manifest
// declares the requested identity. Manifests are read with a minimal decoder:
// `config` cannot depend on `internal/module`, which depends on `config`.
func matchModuleByManifest(tree, reference string) string {
	entries, err := os.ReadDir(tree)
	if err != nil {
		return ""
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names) // deterministic when two manifests claim the identity
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(tree, name, ManifestFileName))
		if err != nil {
			continue
		}
		var identity struct {
			ID     string `json:"id"`
			Domain string `json:"domain"`
		}
		if err := json.Unmarshal(raw, &identity); err != nil {
			continue
		}
		if identity.ID == reference || identity.Domain == reference {
			return filepath.Join(tree, name)
		}
	}
	return ""
}

// ManifestPath returns the path of a module manifest, in whichever tree holds
// the module. When neither does, the installation path is returned so the
// caller reports a coherent "not found" on a single location.
func ManifestPath(root, module string) string {
	if dir := ResolveModuleDir(root, module); dir != "" {
		return filepath.Join(dir, ManifestFileName)
	}
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
