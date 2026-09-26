package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/protorians/lior-cli/internal/audit"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/module"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/tui"
	"github.com/spf13/cobra"
)

var auditOutput string

func init() {
	auditCmd.Flags().StringVar(&auditOutput, "output", "", i18n.T("audit.flag.output"))
	_ = auditCmd.RegisterFlagCompletionFunc("output", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"table", "json"}, cobra.ShellCompDirectiveDefault
	})
	i18nHelp(auditCmd, "cmd.audit.short", "cmd.audit.long")
}

// resolveAuditOutput maps the --output flag to the render mode, rejecting
// unknown values (spec §5.10 : `--output json` is the only machine format).
func resolveAuditOutput() (string, error) {
	switch auditOutput {
	case "", "table":
		return "table", nil
	case "json":
		return "json", nil
	default:
		return "", pkg.NewErrorWithFix(i18n.T("cat.audit"),
			i18n.Tf("audit.error.format", auditOutput),
			i18n.T("audit.error.format.fix"), pkg.ExitError)
	}
}

var auditCmd = &cobra.Command{
	Use:   "audit [module]",
	Short: "Audit a module's conformance",
	Long: `Audits the conformance of one (or all) module(s) against the
Liora rules: Clean Architecture, manifest.json and index.tsx.

Without an argument, all modules in library/modules/ are audited.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAudit(cmd, args)
	},
}

func runAudit(cmd *cobra.Command, args []string) error {
	root, err := requireProjectRoot()
	if err != nil {
		return err
	}

	name := ""
	if len(args) > 0 {
		name = args[0]
	}

	mode, err := resolveAuditOutput()
	if err != nil {
		return err
	}

	auditor := &audit.Auditor{Root: root}

	result, err := tui.RunWithSpinner(i18n.T("audit.spinner"), func() (*audit.AuditResult, error) {
		return auditor.AuditModules(name)
	})
	if err != nil {
		return pkg.NewError(i18n.T("cat.audit"), err.Error(), pkg.ExitError)
	}

	if mode == "json" {
		return printAuditJSON(result)
	}

	printAuditResult(result)
	return nil
}

// printAuditJSON exports the audit report as JSON (spec §5.10).
func printAuditJSON(result *audit.AuditResult) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return pkg.NewError(i18n.T("cat.audit"), i18n.Tf("audit.error.json", err.Error()), pkg.ExitError)
	}
	fmt.Println(string(data))
	return nil
}

func printAuditResult(result *audit.AuditResult) {
	s := tui.NewStyles()
	fmt.Println()

	for _, mod := range result.Modules {
		printAuditModule(s, mod)
	}

	printAuditSummary(s, result)
}

// printAuditModule renders one module report: a verdict header, the table of
// actionable findings (errors and warnings only) and a compact tally of the
// checks that passed. Hiding the OK rows keeps the report focused on what the
// developer must act on.
func printAuditModule(s *tui.Styles, mod module.Result) {
	errors := mod.ErrorCount()
	warnings := mod.WarningCount()

	severity := tui.StatusSuccess
	verdict := i18n.T("audit.verdict.ok")
	if errors > 0 {
		severity = tui.StatusError
		verdict = i18n.T("audit.verdict.ko")
	} else if warnings > 0 {
		severity = tui.StatusWarning
		verdict = i18n.T("audit.verdict.warn")
	}

	fmt.Println(s.ReportHeading(i18n.Tf("audit.header", mod.Module), verdict, severity))

	t := tui.NewTable([]string{i18n.T("label.category"), i18n.T("label.rule"), i18n.T("label.issue")})
	problems := 0
	for _, f := range mod.Findings {
		if f.Severity == module.LevelOK {
			continue
		}
		mark, style := "⚠", s.Warning
		if f.Severity == module.LevelError {
			mark, style = "✗", s.Error
		}
		t.AddRow(f.Category, f.Rule, style.Render(mark+" "+f.Message))
		problems++
	}
	if problems > 0 {
		fmt.Println(t.Render())
	}

	fmt.Println(s.CountsLine(auditCounts(s, mod)...))
	fmt.Println()
}

// auditCounts builds the compact "N checks passed · N warnings · N errors"
// tally of a module, in severity order.
func auditCounts(s *tui.Styles, mod module.Result) []string {
	parts := []string{}
	if passed := mod.OKCount(); passed > 0 {
		parts = append(parts, s.Success.Render("✓ "+i18n.Tf("audit.checks", passed)))
	}
	if warnings := mod.WarningCount(); warnings > 0 {
		parts = append(parts, s.Warning.Render("⚠ "+i18n.Tf("audit.warnings", warnings)))
	}
	if errors := mod.ErrorCount(); errors > 0 {
		parts = append(parts, s.Error.Render("✗ "+i18n.Tf("audit.errors", errors)))
	}
	if len(parts) == 0 {
		parts = append(parts, s.Muted.Render(i18n.T("audit.none")))
	}
	return parts
}

// printAuditSummary renders the closing recap of the whole run. Blocking errors
// go to stderr so a scripted run can grep them; a fully clean run closes on a
// success card.
func printAuditSummary(s *tui.Styles, result *audit.AuditResult) {
	errors := result.TotalErrors()
	warnings := result.TotalWarnings()

	if errors == 0 && warnings == 0 {
		fmt.Println(s.SuccessPanel(s.Success.Render(i18n.T("audit.passed"))))
		fmt.Println()
		return
	}

	parts := []string{}
	if errors > 0 {
		parts = append(parts, i18n.Tf("audit.errors", errors))
	}
	if warnings > 0 {
		parts = append(parts, i18n.Tf("audit.warnings", warnings))
	}
	header := i18n.T("audit.summary")
	for i, p := range parts {
		if i > 0 {
			header += ", "
		}
		header += p
	}
	if errors > 0 {
		fmt.Fprintln(os.Stderr, s.Error.Render(header))
	} else {
		fmt.Println(s.Warning.Render(header))
	}
}
