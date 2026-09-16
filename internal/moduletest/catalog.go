package moduletest

import (
	"path/filepath"
	"strings"

	"github.com/protorians/sentient-cli/internal/pkg"
)

// Sentinels used as a runner name in `sentients.config.json` and in-memory.
const (
	// ScriptRunner forces the module's (then the project's) package.json
	// `test` script: `<pm> run test`.
	ScriptRunner = "script"
	// BuiltinRunner forces the package manager's built-in test runner (e.g.
	// `bun test`).
	BuiltinRunner = "builtin"
)

// TestPackage is a well-known test package that can be installed through the
// project's package manager and run against a module.
type TestPackage struct {
	// Name is the package (and default binary) name, e.g. "vitest".
	Name string
	// Args are the default runner arguments appended to the binary.
	Args []string
}

// TestPackages is the catalog of the main test packages offered to the
// developer when no runner is configured or installed.
var TestPackages = []TestPackage{
	{Name: "vitest", Args: []string{"run"}},
	{Name: "jest", Args: []string{"--ci", "--runInBand"}},
	{Name: "mocha"},
	{Name: "ava"},
}

// TestPackageNames returns the catalog package names in order.
func TestPackageNames() []string {
	names := make([]string, 0, len(TestPackages))
	for _, tp := range TestPackages {
		names = append(names, tp.Name)
	}
	return names
}

// FindTestPackage returns the catalog entry for name.
func FindTestPackage(name string) (TestPackage, bool) {
	for _, tp := range TestPackages {
		if tp.Name == name {
			return tp, true
		}
	}
	return TestPackage{}, false
}

// RunnerBinary returns the executable name of a runner: the catalog package
// name, or the last path segment for a scoped/qualified custom package.
func RunnerBinary(name string) string {
	if _, ok := FindTestPackage(name); ok {
		return name
	}
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// FindRunnerBinary resolves an installed runner binary: the module's
// node_modules, then the project root's, then PATH. It returns "" when the
// runner is not installed.
func FindRunnerBinary(moduleDir, root, name string) string {
	bin := RunnerBinary(name)
	for _, path := range []string{
		filepath.Join(moduleDir, "node_modules", ".bin", bin),
		filepath.Join(root, "node_modules", ".bin", bin),
	} {
		if pkg.FileExists(path) {
			return path
		}
	}
	if pkg.HasCommand(bin) {
		return bin
	}
	return ""
}

// InstalledRunner reports whether a runner is already available locally (module
// or root node_modules/.bin) or on PATH.
func InstalledRunner(moduleDir, root, name string) bool {
	return FindRunnerBinary(moduleDir, root, name) != ""
}
