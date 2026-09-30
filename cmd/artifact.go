package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/protorians/lior-cli/internal/artifactkit"
	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/tui"
	"github.com/spf13/cobra"
)

var artifactCmd = &cobra.Command{
	Use:   "artifact <action> [--] [args...]",
	Short: "Passthrough to the artifact CLI (@liorian/artifact-kit)",
	Long: `Forwards an action to the ` + "`artifact`" + ` CLI of @liorian/artifact-kit — the
build toolchain every Liora module declares.

Actions (build, dev, pack, typecheck, test) and their flags are
forwarded verbatim, so the underlying CLI keeps its own contract:
  liora artifact build            bundle + host document
  liora artifact dev              watch + dev-server
  liora artifact pack             D6/D16 validation + .LiorArtifactPackage
  liora artifact typecheck        tsc --noEmit
  liora artifact test             the module test script
  liora artifact dev --help       the underlying CLI usage

The module is the one holding the current directory, else the one
named by the arguments. @liorian/artifact-kit is installed into that
module when its package.json does not declare it yet.

Two actions take the socle directory as their first argument and are
resolved before the passthrough:
  liora artifact bind:socle <socle> [module]    link the module into the
                                               socle (symlinks + HMR wiring)
  liora artifact unbind:socle <socle> [module] remove the link`,
	Example: `  liora artifact build
  liora artifact dev --port 5178
  liora artifact pack modules/acme-crm
  liora artifact typecheck
  liora artifact bind:socle apps/liorian-socle
  liora artifact unbind:socle apps/liorian-socle`,
	Args: cobra.MinimumNArgs(1),
	// The backing CLI owns its own flags (--port, --out, --host): they must
	// reach it verbatim instead of being parsed by Cobra. Global flags placed
	// before the command (`liora --no-color artifact dev`) are still parsed,
	// because the root command traverses its children first.
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if isArtifactHelpRequest(args) {
			return cmd.Help()
		}
		return runArtifact(args)
	},
}

func init() {
	i18nHelp(artifactCmd, "cmd.artifact.short", "cmd.artifact.long")
}

// isArtifactHelpRequest reports whether the invocation only asks for the
// passthrough usage. `liora artifact <action> --help` is *not* one of them:
// that help belongs to the underlying CLI and is forwarded.
func isArtifactHelpRequest(args []string) bool {
	if len(args) != 1 {
		return false
	}
	return args[0] == "--help" || args[0] == "-h" || args[0] == "help"
}

func runArtifact(args []string) error {
	root, err := requireProjectRoot()
	if err != nil {
		return err
	}

	cfg, err := config.Load(config.ConfigPath(root))
	if err != nil {
		debugf("loading config for artifact passthrough: %v", err)
		cfg = config.Default()
	}

	action, rest := args[0], args[1:]
	if isSocleBindAction(action) {
		return runArtifactSocle(root, cfg, action, rest)
	}

	moduleDir, explicit := artifactModuleDir(root, rest)
	if moduleDir == "" {
		name, err := resolveModule(root, nil)
		if err != nil {
			return err
		}
		moduleDir = config.ResolveModuleDir(root, name)
		if moduleDir == "" {
			return pkg.NewError(i18n.T("cat.module"),
				i18n.Tf("artifact.error.module_absent", name), pkg.ExitModuleNotFound)
		}
	}

	pm := effectiveToolchainPM(cfg)
	installed, err := artifactkit.Ensure(moduleDir, pm)
	if err != nil {
		return err
	}
	if installed {
		reportArtifactInstall(root, moduleDir, pm)
	}

	binary, err := artifactkit.Resolve(root, moduleDir)
	if err != nil {
		return err
	}

	// The underlying CLI defaults its target to the current directory. When
	// the module was resolved elsewhere (module selection, or an argument the
	// CLI reads as a flag value), the target is appended explicitly — it is a
	// positional the CLI accepts after the action, next to its flags.
	if !explicit {
		if cwd, cerr := os.Getwd(); cerr == nil && !sameDir(cwd, moduleDir) {
			rest = append(rest, displayPath(cwd, moduleDir))
		}
	}

	debugf("artifact passthrough: %s %v (in %s)", binary, append([]string{action}, rest...), moduleDir)
	return artifactkit.Exec(binary, moduleDir, append([]string{action}, rest...))
}

// socleBindActions are the artifact actions liora resolves itself instead of
// forwarding blindly: their first argument is the socle directory, not a
// module, so the generic target resolution would mistake it for one.
var socleBindActions = map[string]bool{
	"bind:socle":   true,
	"unbind:socle": true,
}

func isSocleBindAction(action string) bool {
	return socleBindActions[action]
}

// runArtifactSocle forwards `artifact bind:socle|unbind:socle <socle> [module]`
// to the artifact CLI. The socle path is resolved against the invocation
// directory before the child starts in the module directory, so a relative
// path keeps its meaning. The module is the one holding the current directory,
// else the one named by the arguments, else the one selected at the root.
func runArtifactSocle(root string, cfg config.Config, action string, rest []string) error {
	socleIndex := firstNonFlagIndex(rest)
	if socleIndex < 0 {
		return pkg.NewErrorWithFix(
			i18n.T("cat.link"),
			i18n.Tf("artifact.error.socle_arg", action, action),
			i18n.T("artifact.error.socle_arg.fix"),
			pkg.ExitError,
		)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return pkg.NewError(i18n.T("cat.project"), i18n.T("modules.error.cwd"), pkg.ExitError)
	}

	socleAbs := rest[socleIndex]
	if !filepath.IsAbs(socleAbs) {
		socleAbs = filepath.Join(cwd, socleAbs)
	}
	socleAbs = filepath.Clean(socleAbs)

	moduleArg := ""
	moduleIndex := -1
	if next := firstNonFlagIndexFrom(rest, socleIndex+1); next >= 0 {
		moduleArg = rest[next]
		moduleIndex = next
	}
	moduleDir, err := socleBindModuleDir(root, cwd, moduleArg)
	if err != nil {
		return err
	}

	pm := effectiveToolchainPM(cfg)
	installed, err := artifactkit.Ensure(moduleDir, pm)
	if err != nil {
		return err
	}
	if installed {
		reportArtifactInstall(root, moduleDir, pm)
	}

	binary, err := artifactkit.Resolve(root, moduleDir)
	if err != nil {
		return err
	}

	// The socle path is re-emitted absolutised; the optional module positional
	// is dropped (the child runs inside the resolved module directory, which is
	// the CLI's default target) while every flag is forwarded verbatim.
	forward := []string{action, socleAbs}
	for i, arg := range rest {
		if i == socleIndex || i == moduleIndex {
			continue
		}
		forward = append(forward, arg)
	}

	debugf("artifact %s: %s %v (module %s)", action, binary, forward, moduleDir)
	return artifactkit.Exec(binary, moduleDir, forward)
}

// socleBindModuleDir resolves the module a bind action applies to: the one
// named by the argument, else the one holding the current directory, else the
// one selected from the project root.
func socleBindModuleDir(root, cwd, moduleArg string) (string, error) {
	if moduleArg != "" {
		if dir := artifactkit.ModuleDirFromArg([]string{moduleArg}); dir != "" {
			return dir, nil
		}
		if dir := absoluteModuleDir(root, moduleArg); dir != "" {
			return dir, nil
		}
		name, err := resolveModule(root, []string{moduleArg})
		if err != nil {
			return "", err
		}
		if dir := absoluteModuleDir(root, name); dir != "" {
			return dir, nil
		}
		return "", pkg.NewError(i18n.T("cat.module"),
			i18n.Tf("artifact.error.module_absent", moduleArg), pkg.ExitModuleNotFound)
	}

	if dir := artifactkit.ModuleDirFromCwd(root, cwd); dir != "" {
		return dir, nil
	}
	name, err := resolveModule(root, nil)
	if err != nil {
		return "", err
	}
	if dir := absoluteModuleDir(root, name); dir != "" {
		return dir, nil
	}
	return "", pkg.NewError(i18n.T("cat.module"),
		i18n.Tf("artifact.error.module_absent", name), pkg.ExitModuleNotFound)
}

// absoluteModuleDir resolves a module reference to an absolute directory.
func absoluteModuleDir(root, reference string) string {
	dir := config.ResolveModuleDir(root, reference)
	if dir == "" {
		return ""
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// firstNonFlagIndex returns the index of the first argument that is not an
// option (does not start with "-"), or -1 when none is.
func firstNonFlagIndex(args []string) int {
	return firstNonFlagIndexFrom(args, 0)
}

func firstNonFlagIndexFrom(args []string, start int) int {
	for i := start; i < len(args); i++ {
		if !strings.HasPrefix(args[i], "-") {
			return i
		}
	}
	return -1
}

// reportArtifactInstall announces the dependency install on stderr, so the
// passthrough keeps a clean stdout for the underlying CLI's own output.
func reportArtifactInstall(root, moduleDir, pm string) {
	s := tui.NewStyles()
	fmt.Fprintln(os.Stderr, s.Info.Render("→ "+i18n.Tf("artifact.installed",
		artifactkit.PackageName, pm, relToRoot(root, moduleDir))))
}

// artifactModuleDir resolves the module an artifact action applies to: the one
// named by an argument, else the one holding the current directory. The second
// return value reports whether the arguments already named the directory (so
// the caller must not append it again).
func artifactModuleDir(root string, args []string) (string, bool) {
	if dir := artifactkit.ModuleDirFromArg(args); dir != "" {
		return dir, true
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	if dir := artifactkit.ModuleDirFromCwd(root, cwd); dir != "" {
		return dir, false
	}
	return "", false
}

// sameDir reports whether two paths designate the same directory.
func sameDir(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	if absA == absB {
		return true
	}
	resolvedA, errA := filepath.EvalSymlinks(absA)
	resolvedB, errB := filepath.EvalSymlinks(absB)
	return errA == nil && errB == nil && resolvedA == resolvedB
}

// displayPath renders target relative to base when it reads better in a
// command line (it stays inside the project), else absolute.
func displayPath(base, target string) string {
	if rel, err := filepath.Rel(base, target); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return target
}
