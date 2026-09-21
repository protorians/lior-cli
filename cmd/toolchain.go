package cmd

import (
	"context"
	"errors"
	"strings"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/toolchain"
	"github.com/protorians/lior-cli/internal/tui"
	"github.com/spf13/cobra"
)

// toolchainCmd builds a passthrough command that proxies the package.json
// script backing the given application lifecycle command (spec
// docs/specs/liorian-toolchain.md). Every argument — including flags such as
// `-p` or `--experimental-https` — is forwarded verbatim to the script (an
// optional `--` separator is accepted and dropped); hooks configured in
// `lorian.config.json` run before/after it.
func toolchainCmd(name string) *cobra.Command {
	return &cobra.Command{
		Use:   name + " [--] [args...]",
		Short: i18n.T("cmd." + name + ".short"),
		Long:  i18n.T("cmd." + name + ".long"),
		Args:  cobra.ArbitraryArgs,
		// The backing script owns these arguments: flags such as `-p` or
		// `--experimental-https` must be forwarded verbatim instead of being
		// parsed (and rejected) by the CLI. Global flags placed before the
		// command (`liorian --no-color dev …`) are still parsed by Cobra
		// because the root command traverses its children first.
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
				return cmd.Help()
			}
			return runToolchain(cmd, name, args)
		},
	}
}

// forwardArgs normalises the raw arguments of a passthrough command: an
// optional `--` separator is dropped so the remaining tokens are forwarded
// verbatim to the underlying script (`liorian dev -- --port 3000` and
// `liorian dev --port 3000` are equivalent).
func forwardArgs(args []string) []string {
	for i, a := range args {
		if a == "--" {
			out := make([]string, 0, len(args)-1)
			out = append(out, args[:i]...)
			out = append(out, args[i+1:]...)
			return out
		}
	}
	return args
}

var (
	devCmd   = toolchainCmd(toolchain.Dev)
	buildCmd = toolchainCmd(toolchain.Build)
	startCmd = toolchainCmd(toolchain.Start)
	checkCmd = toolchainCmd(toolchain.Check)
)

func init() {
	i18nHelp(devCmd, "cmd.dev.short", "cmd.dev.long")
	i18nHelp(buildCmd, "cmd.build.short", "cmd.build.long")
	i18nHelp(startCmd, "cmd.start.short", "cmd.start.long")
	i18nHelp(checkCmd, "cmd.check.short", "cmd.check.long")
}

// runToolchain executes a passthrough lifecycle command for the current
// project: the required module health checks, then the backing script with its
// configured pre/post hooks, streaming the underlying command live.
func runToolchain(cmd *cobra.Command, name string, args []string) error {
	root, err := requireProjectRoot()
	if err != nil {
		return err
	}

	cfg, err := config.Load(config.ConfigPath(root))
	if err != nil {
		debugf("loading config for %s: %v", name, err)
		cfg = config.Default()
	}

	// Pre-flight gate (spec docs/specs/liorian-toolchain.md, §5.7): the
	// modules must be healthy before the application lifecycle command runs.
	// dev → debug + test ; build/start → debug + test + audit.
	if err := runToolchainGate(root, cfg, name); err != nil {
		return err
	}

	r := toolchain.New(root, effectiveToolchainPM(cfg), cfg.Toolchain)

	_, runErr := tui.RunWithSteps(i18n.Tf("toolchain.spinner."+name), func(ctx context.Context, report func(tui.Step)) (struct{}, error) {
		r.Reporter = report
		return struct{}{}, r.Execute(ctx, name, forwardArgs(args))
	})
	if errors.Is(runErr, tui.ErrCancelled) {
		return pkg.NewError(i18n.T("cat.toolchain"), i18n.T("toolchain.cancelled"), pkg.ExitCancelled)
	}
	return runErr
}

// effectiveToolchainPM returns the package manager used to run the lifecycle
// scripts: the one chosen at init when still available, else the PATH detection
// (bun → pnpm → yarn → npm).
func effectiveToolchainPM(cfg config.Config) string {
	pm := strings.TrimSpace(cfg.Project.PackageManager)
	if pm != "" && pkg.HasCommand(pm) {
		return pm
	}
	return pkg.DetectPackageManager()
}
