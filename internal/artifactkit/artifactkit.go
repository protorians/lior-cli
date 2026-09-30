// Package artifactkit backs the `liora artifact <action>` passthrough command.
//
// A module is built by the `artifact` CLI shipped by `@liorian/artifact-kit`
// — the package every module declares and the one its `build`/`dev`/
// `typecheck` scripts call. `liora artifact <action>` makes that CLI reachable
// from the single Liora entry point without changing the developer's package
// manager or runtime: it resolves the module the action applies to, installs
// `@liorian/artifact-kit` when the module does not declare it yet, then
// forwards the action and its arguments verbatim to the `artifact` executable.
//
// The passthrough is deliberately transparent — no step view, no spinner, the
// child's stdio wired straight to the terminal — so `artifact dev` keeps its
// live output, its ANSI colours and its Ctrl+C, and the exit code of the
// underlying CLI is the exit code of `liora artifact`.
package artifactkit

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
)

const (
	// PackageName is the npm package that ships the `artifact` CLI. It is the
	// module build toolchain, declared as a runtime dependency because a
	// module's package.json scripts (`build`, `dev`, `typecheck`) call the
	// binary it installs.
	PackageName = "@liorian/artifact-kit"
	// BinaryName is the CLI name installed in `node_modules/.bin`.
	BinaryName = "artifact"
)

// ModuleDirFromCwd walks up from cwd and returns the first module directory it
// crosses: `modules/<id>`, `library/modules/<id>` or
// `library/modules/<id>/<version>` holding a manifest.json. It returns "" when
// cwd is not inside a module — the caller then falls back to module selection.
//
// A bare manifest.json is not enough: the workspace root itself may declare
// none, but an unrelated directory must not be mistaken for a module.
func ModuleDirFromCwd(root, cwd string) string {
	if root == "" || cwd == "" {
		return ""
	}
	for dir := cwd; isInside(root, dir); dir = filepath.Dir(dir) {
		if isModuleDir(root, dir) {
			return dir
		}
	}
	return ""
}

// ModuleDirFromArg returns the module directory the given arguments point at:
// the first argument naming an existing directory that holds a manifest.json.
// It returns "" when none does.
func ModuleDirFromArg(args []string) string {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if !pkg.DirExists(arg) || !pkg.FileExists(filepath.Join(arg, config.ManifestFileName)) {
			continue
		}
		if abs, err := filepath.Abs(arg); err == nil {
			return abs
		}
		return arg
	}
	return ""
}

// isInside reports whether dir is root or lives under it.
func isInside(root, dir string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absDir)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// isModuleDir reports whether dir is a module of the project rooted at root:
// the source tree (`modules/<id>`, D5) or an installation of it
// (`library/modules/<id>` and its per-version directories, D11). A directory
// outside the project, and the project root itself, are never modules.
func isModuleDir(root, dir string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absDir)
	if err != nil || rel == "." {
		return false
	}
	if !pkg.FileExists(filepath.Join(absDir, config.ManifestFileName)) {
		return false
	}
	segments := strings.Split(filepath.ToSlash(rel), "/")
	// modules/<id>
	if len(segments) == 2 && segments[0] == config.WorkspaceModulesDir {
		return true
	}
	// library/modules/<id>[/<version>]
	installed := strings.SplitN(config.ExternalModulesDir, "/", 2)
	if len(segments) > len(installed) && slices.Equal(segments[:len(installed)], installed) {
		return true
	}
	return false
}

// Declares reports whether the package.json of moduleDir declares
// `@liorian/artifact-kit`. A module without a package.json declares nothing:
// its build toolchain still has to be installed.
func Declares(moduleDir string) bool {
	np := pkg.LoadNodePackage(filepath.Join(moduleDir, "package.json"))
	_, runtime := np.Dependencies[PackageName]
	_, dev := np.DevDependencies[PackageName]
	return runtime || dev
}

// Ensure installs `@liorian/artifact-kit` in moduleDir when it is missing from
// its dependencies, using the package manager pm, and reports whether an
// installation ran. It is a no-op when the module already declares the
// package — the declaration alone is the contract, the resolved binary is
// verified afterwards.
func Ensure(moduleDir, pm string) (bool, error) {
	if Declares(moduleDir) {
		return false, nil
	}
	if strings.TrimSpace(pm) == "" {
		return false, pkg.NewErrorWithFix(
			i18n.T("cat.package_manager"),
			i18n.Tf("artifact.error.pm_none", PackageName),
			i18n.T("artifact.error.pm_none.fix"),
			pkg.ExitError,
		)
	}
	args := pkg.DependencyArgs(pm, PackageName)
	if args == nil {
		return false, pkg.NewErrorWithFix(
			i18n.T("cat.package_manager"),
			i18n.Tf("artifact.error.pm_unsupported", pm),
			i18n.T("artifact.error.pm_none.fix"),
			pkg.ExitError,
		)
	}
	if err := pkg.StreamCommandIn(moduleDir, pm, args...); err != nil {
		return false, pkg.NewErrorWithFix(
			i18n.T("cat.pack"),
			i18n.Tf("artifact.error.install_failed", PackageName, err.Error()),
			i18n.Tf("artifact.error.install_failed.fix", PackageName),
			pkg.ExitBuild,
		)
	}
	// Fail-closed: an install that silently left the declaration behind would
	// make every later `liora artifact` retry it, and the binary would still be
	// missing. Report the failure now.
	if !Declares(moduleDir) {
		return false, pkg.NewErrorWithFix(
			i18n.T("cat.pack"),
			i18n.Tf("artifact.error.install_undeclared", PackageName),
			i18n.Tf("artifact.error.install_failed.fix", PackageName),
			pkg.ExitBuild,
		)
	}
	return true, nil
}

// binaryName resolves the name of the CLI executable for the host platform
// (Windows needs the `.cmd` shim to be executable).
func binaryName() string {
	if runtime.GOOS == "windows" {
		return BinaryName + ".cmd"
	}
	return BinaryName
}

// Resolve locates the `artifact` executable reachable from moduleDir: the one
// installed in the module's own `node_modules/.bin`, then the closest one
// while walking up to the project root (a workspace hoists the binary, so
// `modules/<id>` may not own it). It returns a categorized error when no
// binary is installed.
func Resolve(root, moduleDir string) (string, error) {
	binary := binaryName()
	for dir := moduleDir; isInside(root, dir); dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "node_modules", ".bin", binary)
		if pkg.FileExists(candidate) {
			return candidate, nil
		}
	}
	return "", pkg.NewErrorWithFix(
		i18n.T("cat.pack"),
		i18n.Tf("artifact.error.binary_not_found", BinaryName, relToRoot(root, moduleDir)),
		i18n.Tf("artifact.error.binary_not_found.fix", PackageName),
		pkg.ExitBuild,
	)
}

// relToRoot renders a path relative to root for display, falling back to the
// absolute path when it cannot be made relative.
func relToRoot(root, path string) string {
	if root == "" || path == "" {
		return path
	}
	if rel, err := filepath.Rel(root, path); err == nil {
		return rel
	}
	return path
}

// Exec runs the resolved `artifact` CLI inside moduleDir with args forwarded
// verbatim, wiring the child's stdio to the terminal. It returns the child's
// exit code as a categorized error so `liora artifact pack` and
// `artifact pack` fail identically.
//
// No process group is created: the child stays in the CLI's own foreground
// group, so Ctrl+C reaches the whole tree (a watcher's esbuild children
// included) exactly as it would when the developer ran `artifact dev`
// directly.
func Exec(binary, moduleDir string, args []string) error {
	cmd := exec.Command(binary, args...)
	cmd.Dir = moduleDir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err == nil {
		return nil
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return pkg.NewError(i18n.T("cat.pack"), fmt.Sprintf("%s: %v", BinaryName, err), pkg.ExitBuild)
	}
	// A signalled child reports -1; the shell convention (128 + signal) is
	// what a direct invocation of the CLI would have printed.
	code := exit.ExitCode()
	if code < 0 {
		code = pkg.ExitCancelled
	}
	return pkg.NewError(i18n.T("cat.pack"), i18n.Tf("artifact.error.failed", BinaryName, code), code)
}
