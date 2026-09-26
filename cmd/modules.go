package cmd

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/tui"
)

// requireProjectRoot locates the current Liora project root or returns a
// dedicated error (exit code 3 per spec).
func requireProjectRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", pkg.NewError(i18n.T("cat.project"), i18n.T("modules.error.cwd"), pkg.ExitError)
	}
	root, err := config.FindProjectRoot(cwd)
	if err != nil {
		return "", pkg.NewErrorWithFix(
			i18n.T("cat.project"),
			err.Error(),
			i18n.T("modules.error.root.fix"),
			pkg.ExitModuleNotFound,
		)
	}
	debugf("project root: %s", root)
	return root, nil
}

// relToRoot returns path relative to the project root for display purposes,
// falling back to the original path when it cannot be made relative.
func relToRoot(root, path string) string {
	if path == "" {
		return path
	}
	if rel, err := filepath.Rel(root, path); err == nil {
		return rel
	}
	return path
}

// listModules returns the module names present in `library/modules/`.
func listModules(root string) ([]string, error) {
	dir := filepath.Join(root, config.ExternalModulesDir)
	if !pkg.DirExists(dir) {
		return nil, pkg.NewError(
			i18n.T("cat.project"),
			i18n.Tf("modules.error.dir", config.ExternalModulesDir),
			pkg.ExitModuleNotFound,
		)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, pkg.NewError(i18n.T("cat.project"), i18n.T("modules.error.read"), pkg.ExitError)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if pkg.FileExists(filepath.Join(dir, e.Name(), config.ManifestFileName)) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// resolveModule returns the module name to operate on: the positional arg if
// provided (path decorations like `./` or `library/modules/` are stripped),
// otherwise a single-module selection or an interactive menu.
func resolveModule(root string, args []string) (string, error) {
	if len(args) > 0 {
		name := normalizeModuleArg(args[0])
		if !pkg.DirExists(filepath.Join(root, config.ExternalModulesDir, name)) {
			return "", pkg.NewErrorWithFix(
				i18n.T("cat.module"),
				i18n.Tf("modules.error.module_absent", name, config.ExternalModulesDir),
				i18n.T("modules.error.module_absent.fix"),
				pkg.ExitModuleNotFound,
			)
		}
		return name, nil
	}

	modules, err := listModules(root)
	if err != nil {
		return "", err
	}
	if len(modules) == 0 {
		return "", pkg.NewError(
			i18n.T("cat.module"),
			i18n.T("modules.error.none"),
			pkg.ExitModuleNotFound,
		)
	}
	if len(modules) == 1 {
		return modules[0], nil
	}

	if !tui.IsInteractive() {
		return "", pkg.NewError(
			i18n.T("cat.selection"),
			i18n.T("modules.error.multiple"),
			pkg.ExitError,
		)
	}

	return selectModule(modules)
}

func selectModule(modules []string) (string, error) {
	selected, err := tui.Select(i18n.T("modules.prompt.select"), modules)
	if err != nil {
		return "", err
	}
	return selected, nil
}

// splitModuleSpec splits a "name@version" argument into its components. The
// version part is optional and empty when absent.
func splitModuleSpec(arg string) (name, version string) {
	if i := strings.IndexByte(arg, '@'); i >= 0 {
		return arg[:i], arg[i+1:]
	}
	return arg, ""
}

// resolveModuleWithVersion behaves like resolveModule but accepts an optional
// "@version" suffix on the positional argument (e.g. "blog-manager@1.2.0").
func resolveModuleWithVersion(root string, args []string) (name, version string, err error) {
	clean := args
	if len(args) > 0 {
		name, version = splitModuleSpec(args[0])
		clean = []string{name}
	}
	name, err = resolveModule(root, clean)
	return name, version, err
}
