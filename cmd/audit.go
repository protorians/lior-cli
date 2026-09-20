package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/jetbrains/lior-cli/internal/audit"
	"github.com/jetbrains/lior-cli/internal/i18n"
	"github.com/jetbrains/lior-cli/internal/module"
	"github.com/jetbrains/lior-cli/internal/pkg"
	"github.com/jetbrains/lior-cli/internal/tui"
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
Liorian rules: Clean Architecture, manifest.json and index.tsx.

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
		fmt.Println(s.Heading(i18n.Tf("audit.header", mod.Module)))

		t := tui.NewTable([]string{i18n.T("label.category"), i18n.T("label.rule"), i18n.T("label.status")})
		for _, f := range mod.Findings {
			status := s.Success.Render("✓ " + f.Message)
			switch f.Severity {
			case module.LevelError:
				status = s.Error.Render("✗ " + f.Message)
			case module.LevelWarning:
				status = s.Warning.Render("⚠ " + f.Message)
			}
			t.AddRow(f.Category, f.Rule, status)
		}

		if len(mod.Findings) > 0 {
			fmt.Println(t.Render())
		} else {
			fmt.Println(s.Muted.Render(i18n.T("audit.none")))
		}
		fmt.Println()
	}

	errors := result.TotalErrors()
	warnings := result.TotalWarnings()

	if errors == 0 && warnings == 0 {
		fmt.Println(s.SuccessPanel(s.Success.Render(i18n.T("audit.passed"))))
		fmt.Println()
	} else {
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
}
