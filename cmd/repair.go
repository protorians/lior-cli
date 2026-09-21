package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/module"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/repair"
	"github.com/protorians/lior-cli/internal/tui"
	"github.com/spf13/cobra"
)

var (
	repairOutput        string
	repairDryRun        bool
	repairWarnings      bool
	repairNoInstall     bool
	repairNoInteraction bool
)

func init() {
	repairCmd.Flags().StringVar(&repairOutput, "output", "", i18n.T("repair.flag.output"))
	_ = repairCmd.RegisterFlagCompletionFunc("output", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"table", "json"}, cobra.ShellCompDirectiveDefault
	})
	repairCmd.Flags().BoolVar(&repairDryRun, "dry-run", false, i18n.T("repair.flag.dry_run"))
	repairCmd.Flags().BoolVar(&repairWarnings, "warnings", false, i18n.T("repair.flag.warnings"))
	repairCmd.Flags().BoolVar(&repairNoInstall, "no-install", false, i18n.T("repair.flag.no_install"))
	repairCmd.Flags().BoolVar(&repairNoInteraction, "no-interaction", false, i18n.T("repair.flag.no_interaction"))
	i18nFlag(repairCmd, "output", "repair.flag.output")
	i18nFlag(repairCmd, "dry-run", "repair.flag.dry_run")
	i18nFlag(repairCmd, "warnings", "repair.flag.warnings")
	i18nFlag(repairCmd, "no-install", "repair.flag.no_install")
	i18nFlag(repairCmd, "no-interaction", "repair.flag.no_interaction")
	i18nHelp(repairCmd, "cmd.repair.short", "cmd.repair.long")
}

var repairCmd = &cobra.Command{
	Use:   "repair [module]",
	Short: "Repair a module's audit failures",
	Long: `Automatically repairs the failing (blocking) points reported by
'liorian audit' for one (or all) module(s): manifest metadata (id, name,
version, token, entry, permissions, platforms, compatibility, capabilities,
category, domain), the module directory name (proposed with a tab-to-fill
prompt, applied directly with --no-interaction), missing npm dependencies and
malformed manifest fields.

Findings that require a code change (Clean Architecture violations, missing
default export or module declaration, missing requirements, publisher
identity, empty assets) are reported as step-by-step instructions for the
developer to apply.

Without an argument, all modules in library/modules/ are repaired.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRepair(cmd, args)
	},
}

// resolveRepairOutput maps the --output flag to the render mode.
func resolveRepairOutput() (string, error) {
	switch repairOutput {
	case "", "table":
		return "table", nil
	case "json":
		return "json", nil
	default:
		return "", pkg.NewErrorWithFix(i18n.T("cat.repair"),
			i18n.Tf("repair.error.format", repairOutput),
			i18n.T("repair.error.format.fix"), pkg.ExitError)
	}
}

func runRepair(cmd *cobra.Command, args []string) error {
	root, err := requireProjectRoot()
	if err != nil {
		return err
	}

	name := ""
	if len(args) > 0 {
		name = args[0]
	}

	mode, err := resolveRepairOutput()
	if err != nil {
		return err
	}

	repairer := &repair.Repairer{
		Root:            root,
		IncludeWarnings: repairWarnings,
		DryRun:          repairDryRun,
		SkipInstall:     repairNoInstall,
		NoInteraction:   repairNoInteraction,
		Rename:          repairRenamePrompt,
	}

	result, err := tui.RunWithSpinner(i18n.T("repair.spinner"), func() (*repair.RepairResult, error) {
		return repairer.RepairModules(name)
	})
	if err != nil {
		return pkg.NewError(i18n.T("cat.repair"), err.Error(), pkg.ExitError)
	}

	if mode == "json" {
		return printRepairJSON(result)
	}

	printRepairResult(result)

	if remaining := result.TotalRemainingErrors(); remaining > 0 {
		return pkg.NewErrorWithFix(i18n.T("cat.repair"),
			i18n.Tf("repair.error.remaining", remaining),
			i18n.T("repair.error.remaining.fix"), pkg.ExitError)
	}
	return nil
}

// repairRenamePrompt asks the developer to confirm (and possibly edit) the
// suggested module directory name. The suggestion is offered as the input
// placeholder, so pressing tab fills it in. It returns the chosen name and
// whether the rename was accepted (false on decline or cancellation, which
// leaves the manual instruction in the report). In a non-interactive run the
// suggestion is reported instead of prompted.
func repairRenamePrompt(message, suggestion string) (string, bool) {
	if !tui.IsInteractive() {
		return "", false
	}
	value, err := tui.AskText(message, suggestion)
	if err != nil {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		value = suggestion
	}
	return value, true
}

// printRepairJSON exports the repair report as JSON.
func printRepairJSON(result *repair.RepairResult) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return pkg.NewError(i18n.T("cat.repair"), i18n.Tf("repair.error.json", err.Error()), pkg.ExitError)
	}
	fmt.Println(string(data))
	return nil
}

func printRepairResult(result *repair.RepairResult) {
	s := tui.NewStyles()
	fmt.Println()

	for _, mod := range result.Modules {
		printRepairModule(s, mod)
	}

	printRepairSummary(result)
}

// printRepairModule renders one module report: a verdict header, the table of
// automatic fixes applied and the step-by-step manual actions still required.
// It mirrors the audit report layout so both commands read the same way.
func printRepairModule(s *tui.Styles, mod repair.ModuleResult) {
	remaining, applied := 0, 0
	for _, f := range mod.Remaining {
		if f.Severity == module.LevelError {
			remaining++
		}
	}
	for _, a := range mod.Actions {
		if a.Applied {
			applied++
		}
	}
	instructions := len(mod.Instructions)

	severity := tui.StatusSuccess
	verdict := i18n.T("repair.verdict.clean")
	switch {
	case remaining > 0:
		severity, verdict = tui.StatusError, i18n.T("repair.verdict.failed")
	case instructions > 0:
		severity, verdict = tui.StatusWarning, i18n.T("repair.verdict.manual")
	case applied > 0:
		verdict = i18n.T("repair.verdict.fixed")
	}

	fmt.Println(s.ReportHeading(i18n.Tf("repair.header", mod.Module), verdict, severity))

	if len(mod.Actions) > 0 {
		fmt.Println(s.SubHeader.Render(i18n.T("repair.actions.title")))
		t := tui.NewTable([]string{i18n.T("label.category"), i18n.T("label.rule"), i18n.T("label.fix")})
		for _, a := range mod.Actions {
			status := s.Success.Render("✓ " + a.Detail)
			if !a.Applied {
				status = s.Muted.Render("… " + a.Detail)
			}
			t.AddRow(a.Category, a.Rule, status)
		}
		fmt.Println(t.Render())
		fmt.Println()
	}

	if len(mod.Instructions) > 0 {
		fmt.Println(s.Warning.Render(i18n.T("repair.instructions.title")))
		fmt.Println()
		for _, ins := range mod.Instructions {
			fmt.Println("  " + s.Warning.Render("⚠ "+ins.Category+" · "+ins.Rule))
			if ins.Message != "" {
				fmt.Println("    " + s.Muted.Render(ins.Message))
			}
			for _, step := range ins.Steps {
				fmt.Println("    " + s.Muted.Render("• "+step))
			}
			fmt.Println()
		}
	}

	if len(mod.Actions) == 0 && len(mod.Instructions) == 0 {
		fmt.Println(s.Success.Render(i18n.T("repair.nothing")))
		fmt.Println()
	}
}

func printRepairSummary(result *repair.RepairResult) {
	s := tui.NewStyles()

	lines := []string{s.Success.Render("✓ " + i18n.Tf("repair.applied", result.TotalApplied()))}
	if manual := result.TotalInstructions(); manual > 0 {
		lines = append(lines, s.Warning.Render("⚠ "+i18n.Tf("repair.manual", manual)))
	}
	if remaining := result.TotalRemainingErrors(); remaining > 0 {
		lines = append(lines, s.Error.Render("✗ "+i18n.Tf("repair.remaining", remaining)))
	}
	if repairDryRun {
		lines = append(lines, s.Info.Render("◆ "+i18n.T("repair.dry_run")))
	}

	body := s.SubHeader.Render(i18n.T("repair.summary")) + "\n" + strings.Join(lines, "\n")
	fmt.Println(s.NeutralPanel(body))
	fmt.Println()
}
