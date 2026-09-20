package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/moduletest"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/tui"
	"github.com/spf13/cobra"
)

var (
	testTimeout time.Duration
	testRunner  string
)

var testCmd = &cobra.Command{
	Use:   "test [module]",
	Short: "Run the tests of a module or all modules",
	Long: `Runs the tests of one (or all) module(s) in library/modules/.

Validates the module, resolves a test package with the package manager chosen
at install (bun/pnpm/yarn/npm), installs it within that manager's scope when
needed and streams the suite output in real time. The chosen test package is
persisted in lorian.config.json. The run exits non-zero when any module's
tests fail.

Without an argument, all modules are tested.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTest(cmd, args)
	},
}

func init() {
	testCmd.Flags().DurationVar(&testTimeout, "timeout", 0, i18n.T("test.flag.timeout"))
	i18nFlag(testCmd, "timeout", "test.flag.timeout")
	testCmd.Flags().StringVar(&testRunner, "runner", "", i18n.T("test.flag.runner"))
	i18nFlag(testCmd, "runner", "test.flag.runner")
	i18nHelp(testCmd, "cmd.test.short", "cmd.test.long")
}

func runTest(cmd *cobra.Command, args []string) error {
	root, err := requireProjectRoot()
	if err != nil {
		return err
	}

	cfg, err := config.Load(config.ConfigPath(root))
	if err != nil {
		debugf("loading config for test: %v", err)
		cfg = config.Default()
	}

	tester := &moduletest.Tester{
		Root:                  root,
		Timeout:               testTimeout,
		Config:                cfg.Test,
		ProjectPackageManager: cfg.Project.PackageManager,
		Runner:                testRunner,
	}

	if err := prepareRunners(tester, args); err != nil {
		return err
	}

	if len(args) > 0 {
		return runTestSingle(tester, args[0], root)
	}
	return runTestAll(tester, root)
}

// prepareRunners lets the developer pick a test package up front, outside the
// live step runner so no two Bubble Tea programs collide. It only runs in an
// interactive session and only for modules that cannot resolve a runner on
// their own.
func prepareRunners(tester *moduletest.Tester, args []string) error {
	if !tui.IsInteractive() || tester.Runner != "" {
		return nil
	}
	modules := args
	if len(modules) == 0 {
		var err error
		modules, err = listModules(tester.Root)
		if err != nil {
			return err
		}
	}
	for _, module := range modules {
		if !tester.NeedsRunnerChoice(module) {
			continue
		}
		choice, err := chooseRunner(module, tester.RunnerOptions(module))
		if err != nil {
			return err
		}
		tester.SetRunnerChoice(module, choice)
	}
	return nil
}

// chooseRunner lists the main test packages (and their installation state) and
// returns the developer's selection. An empty package means "skip".
func chooseRunner(module string, options []moduletest.RunnerOption) (moduletest.RunnerChoice, error) {
	installedSuffix := i18n.T("test.prompt.runner.installed")
	customLabel := i18n.T("test.prompt.runner.custom")

	labels := make([]string, 0, len(options)+1)
	for _, o := range options {
		label := o.Package
		if o.Installed {
			label += installedSuffix
		}
		labels = append(labels, label)
	}
	labels = append(labels, customLabel)

	selected, err := tui.Select(i18n.Tf("test.prompt.runner", module), labels)
	if err != nil {
		return moduletest.RunnerChoice{}, err
	}

	name := ""
	installed := false
	if selected == customLabel {
		value, err := tui.AskText(i18n.T("test.prompt.runner.custom_title"), "")
		if err != nil {
			return moduletest.RunnerChoice{}, err
		}
		name = strings.TrimSpace(value)
	} else {
		for _, o := range options {
			if selected == o.Package || selected == o.Package+installedSuffix {
				name, installed = o.Package, o.Installed
				break
			}
		}
	}
	if name == "" {
		return moduletest.RunnerChoice{}, nil
	}

	install := !installed
	if install {
		ok, err := tui.Confirm(i18n.Tf("test.prompt.runner.install", name), true)
		if err != nil {
			return moduletest.RunnerChoice{}, err
		}
		if !ok {
			return moduletest.RunnerChoice{}, nil
		}
	}
	return moduletest.RunnerChoice{Package: name, Install: install}, nil
}

func runTestSingle(tester *moduletest.Tester, name, root string) error {
	result, err := tui.RunWithSteps(i18n.Tf("test.spinner.single", name), func(ctx context.Context, report func(tui.Step)) (*moduletest.TestResult, error) {
		tester.Reporter = report
		return tester.TestModuleCtx(ctx, name)
	})
	if errors.Is(err, tui.ErrCancelled) {
		return pkg.NewError(i18n.T("cat.test"), i18n.T("test.cancelled"), pkg.ExitCancelled)
	}
	if err != nil {
		return pkg.NewError(i18n.T("cat.test"), err.Error(), pkg.ExitTest)
	}

	persistTestConfig(root, result)
	printTestResult(result)
	if result.Status == "ERROR" {
		return pkg.NewError(i18n.T("cat.test"), i18n.Tf("test.failed", name), pkg.ExitTest)
	}
	return nil
}

func runTestAll(tester *moduletest.Tester, root string) error {
	results, err := tui.RunWithSteps(i18n.T("test.spinner.all"), func(ctx context.Context, report func(tui.Step)) ([]*moduletest.TestResult, error) {
		tester.Reporter = report
		return tester.TestAllCtx(ctx)
	})
	if errors.Is(err, tui.ErrCancelled) {
		return pkg.NewError(i18n.T("cat.test"), i18n.T("test.cancelled"), pkg.ExitCancelled)
	}
	if err != nil {
		return pkg.NewError(i18n.T("cat.test"), err.Error(), pkg.ExitTest)
	}

	persistTestConfig(root, results...)

	s := tui.NewStyles()
	fmt.Println()

	t := tui.NewTable([]string{i18n.T("label.module"), i18n.T("label.status"), i18n.T("label.errors")})
	var steps []tui.Step
	failed := 0
	for _, r := range results {
		status := s.Success.Render("✓ " + r.Status)
		if r.Status == "ERROR" {
			status = s.Error.Render("✗ " + r.Status)
			failed++
		} else if r.Status == "WARNING" {
			status = s.Warning.Render("⚠ " + r.Status)
		} else if r.Status == "SKIPPED" {
			status = s.Info.Render("◆ " + r.Status)
		}
		t.AddRow(r.Module, status, fmt.Sprintf("%d", r.Errors))
		steps = append(steps, r.Steps...)
	}

	fmt.Print(t.Render())

	// Print logs for modules with errors
	for _, r := range results {
		if len(r.Logs) > 0 {
			logs := moduletest.FormatTestLogs(r.Module, r.Logs)
			fmt.Println(s.LogsBlock(i18n.Tf("test.logs_for", r.Module), logs))
			fmt.Println()
		}
	}

	// Closing recap: severity breakdown of the whole run.
	fmt.Println(s.SummaryBlock(tui.Summarize(steps)))
	fmt.Println()

	if failed > 0 {
		return pkg.NewError(i18n.T("cat.test"), i18n.Tf("test.failed_all", failed), pkg.ExitTest)
	}
	return nil
}

// persistTestConfig stores the resolved test packages and package manager in
// lorian.config.json so later runs skip detection and selection.
func persistTestConfig(root string, results ...*moduletest.TestResult) {
	cfg, err := config.Load(config.ConfigPath(root))
	if err != nil {
		debugf("loading config for test persistence: %v", err)
		return
	}

	changed := false
	for _, r := range results {
		if r == nil || r.Runner == "" || r.Runner == moduletest.ScriptRunner {
			continue
		}
		if cfg.Test.RunnerFor(r.Module) != r.Runner {
			cfg.Test.SetRunner(r.Module, r.Runner)
			changed = true
		}
	}

	// Record the package manager actually used unless the project already
	// pins the same one.
	for _, r := range results {
		if r == nil || r.PackageManager == "" {
			continue
		}
		if cfg.Test.PackageManager != r.PackageManager && cfg.Project.PackageManager != r.PackageManager {
			cfg.Test.PackageManager = r.PackageManager
			changed = true
		}
		break
	}

	if !changed {
		return
	}
	if err := cfg.Save(config.ConfigPath(root)); err != nil {
		debugf("saving config after test: %v", err)
	}
}

func printTestResult(result *moduletest.TestResult) {
	s := tui.NewStyles()
	fmt.Println()

	status := s.Success.Render("✓ " + result.Status)
	if result.Status == "ERROR" {
		status = s.Error.Render("✗ " + result.Status)
	} else if result.Status == "WARNING" {
		status = s.Warning.Render("⚠ " + result.Status)
	} else if result.Status == "SKIPPED" {
		status = s.Info.Render("◆ " + result.Status)
	}

	lines := []string{
		s.KeyValue(i18n.T("label.module"), s.Value.Render(result.Module)),
		s.KeyValue(i18n.T("label.status"), status),
	}
	if result.Runner != "" && result.Runner != moduletest.ScriptRunner {
		lines = append(lines, s.KeyValue(i18n.T("label.runner"), s.Value.Render(result.Runner)))
	}
	if result.Errors > 0 {
		lines = append(lines, s.KeyValue(i18n.T("label.errors"), s.Value.Render(fmt.Sprintf("%d", result.Errors))))
	}
	if result.Command != "" {
		lines = append(lines, s.KeyValue(i18n.T("label.command"), s.Value.Render(result.Command)))
	}
	fmt.Println(s.NeutralPanel(strings.Join(lines, "\n")))

	if len(result.Logs) > 0 {
		fmt.Println()
		logs := moduletest.FormatTestLogs(result.Module, result.Logs)
		fmt.Println(s.LogsBlock(i18n.T("test.logs"), logs))
	}

	// Closing recap: success / notice / warning / error / deprecated breakdown.
	fmt.Println()
	fmt.Println(s.SummaryBlock(tui.Summarize(result.Steps)))

	fmt.Println()
}
