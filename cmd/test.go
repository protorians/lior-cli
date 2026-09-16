package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/protorians/sentient-cli/internal/i18n"
	"github.com/protorians/sentient-cli/internal/moduletest"
	"github.com/protorians/sentient-cli/internal/pkg"
	"github.com/protorians/sentient-cli/internal/tui"
	"github.com/spf13/cobra"
)

var testTimeout time.Duration

var testCmd = &cobra.Command{
	Use:   "test [module]",
	Short: "Run the tests of a module or all modules",
	Long: `Runs the tests of one (or all) module(s) in external_modules/.

Validates the module, resolves a test command (package.json test script,
vitest/jest runner or bun test) and streams its output in real time.
The run exits non-zero when any module's tests fail.

Without an argument, all modules are tested.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTest(cmd, args)
	},
}

func init() {
	testCmd.Flags().DurationVar(&testTimeout, "timeout", 0, i18n.T("test.flag.timeout"))
	i18nFlag(testCmd, "timeout", "test.flag.timeout")
	i18nHelp(testCmd, "cmd.test.short", "cmd.test.long")
}

func runTest(cmd *cobra.Command, args []string) error {
	root, err := requireProjectRoot()
	if err != nil {
		return err
	}

	tester := &moduletest.Tester{Root: root, Timeout: testTimeout}

	if len(args) > 0 {
		return runTestSingle(tester, args[0])
	}
	return runTestAll(tester)
}

func runTestSingle(tester *moduletest.Tester, name string) error {
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

	printTestResult(result)
	if result.Status == "ERROR" {
		return pkg.NewError(i18n.T("cat.test"), i18n.Tf("test.failed", name), pkg.ExitTest)
	}
	return nil
}

func runTestAll(tester *moduletest.Tester) error {
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

func printTestResult(result *moduletest.TestResult) {
	s := tui.NewStyles()
	fmt.Println()

	status := s.Success.Render("✓ " + result.Status)
	if result.Status == "ERROR" {
		status = s.Error.Render("✗ " + result.Status)
	} else if result.Status == "WARNING" {
		status = s.Warning.Render("⚠ " + result.Status)
	}

	lines := []string{
		s.KeyValue(i18n.T("label.module"), s.Value.Render(result.Module)),
		s.KeyValue(i18n.T("label.status"), status),
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
