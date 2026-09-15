package cmd

import (
	"fmt"
	"strings"

	"github.com/protorians/sentient-cli/internal/debug"
	"github.com/protorians/sentient-cli/internal/i18n"
	"github.com/protorians/sentient-cli/internal/pkg"
	"github.com/protorians/sentient-cli/internal/tui"
	"github.com/spf13/cobra"
)

var debugCmd = &cobra.Command{
	Use:   "debug [module]",
	Short: "Debug a module or all modules",
	Long: `Runs the debug of one (or all) module(s) in external_modules/.

Checks the module's conformance, tries to compile in debug mode,
and displays logs and errors in real time.

Without an argument, all modules are debugged.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDebug(cmd, args)
	},
}

func init() {
	i18nHelp(debugCmd, "cmd.debug.short", "cmd.debug.long")
}

func runDebug(cmd *cobra.Command, args []string) error {
	root, err := requireProjectRoot()
	if err != nil {
		return err
	}

	debugger := &debug.Debugger{Root: root}

	if len(args) > 0 {
		return runDebugSingle(debugger, args[0])
	}
	return runDebugAll(debugger)
}

func runDebugSingle(debugger *debug.Debugger, name string) error {
	result, err := tui.RunWithSpinner(i18n.Tf("debug.spinner.single", name), func() (*debug.DebugResult, error) {
		return debugger.DebugModule(name)
	})
	if err != nil {
		return pkg.NewError(i18n.T("cat.debug"), err.Error(), pkg.ExitBuild)
	}

	printDebugResult(result)
	return nil
}

func runDebugAll(debugger *debug.Debugger) error {
	results, err := tui.RunWithSpinner(i18n.T("debug.spinner.all"), func() ([]*debug.DebugResult, error) {
		return debugger.DebugAll()
	})
	if err != nil {
		return pkg.NewError(i18n.T("cat.debug"), err.Error(), pkg.ExitBuild)
	}

	s := tui.NewStyles()
	fmt.Println()

	t := tui.NewTable([]string{i18n.T("label.module"), i18n.T("label.status"), i18n.T("label.errors")})
	for _, r := range results {
		status := s.Success.Render("✓ " + r.Status)
		if r.Status == "ERROR" {
			status = s.Error.Render("✗ " + r.Status)
		} else if r.Status == "WARNING" {
			status = s.Warning.Render("⚠ " + r.Status)
		}
		t.AddRow(r.Module, status, fmt.Sprintf("%d", r.Errors))
	}

	fmt.Print(t.Render())

	// Print logs for modules with errors
	for _, r := range results {
		if len(r.Logs) > 0 {
			logs := debug.FormatDebugLogs(r.Module, r.Logs)
			fmt.Println(s.LogsBlock(i18n.Tf("debug.logs_for", r.Module), logs))
			fmt.Println()
		}
	}

	return nil
}

func printDebugResult(result *debug.DebugResult) {
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
	fmt.Println(s.NeutralPanel(strings.Join(lines, "\n")))

	if len(result.Logs) > 0 {
		fmt.Println()
		logs := debug.FormatDebugLogs(result.Module, result.Logs)
		fmt.Println(s.LogsBlock(i18n.T("debug.logs"), logs))
	}

	fmt.Println()
}
