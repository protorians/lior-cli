package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/protorians/sentient-cli/internal/i18n"
	"github.com/protorians/sentient-cli/internal/module"
	"github.com/protorians/sentient-cli/internal/pkg"
	"github.com/protorians/sentient-cli/internal/tui"
	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create module [name]",
	Short: "Create a new module",
	Long: `Creates a new module in external_modules/ from the embedded
hello-world mockup, renamed with the given module name: manifest.json,
index.tsx, package.json, application/, domain/, infrastructure/,
presentation/.

When the module declaration declares a uri/url, a page is also scaffolded
in src/app/ from the embedded page mockup.

The module's unique UUID token is generated automatically.

Usage : sentients create module [name]`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCreate(cmd, args)
	},
}

var (
	createMockup     string
	createPageMockup string
)

// createSkipInstallEnv disables the dependency installation step of
// `create module` (used by the unit test suite and CI-constrained runs).
const createSkipInstallEnv = "SENTIENT_CLI_SKIP_INSTALL"

func init() {
	createCmd.Flags().StringVar(&createMockup, "mockup", "", i18n.T("create.flag.mockup"))
	createCmd.Flags().StringVar(&createPageMockup, "page-mockup", "", i18n.T("create.flag.page_mockup"))
	i18nHelp(createCmd, "cmd.create.short", "cmd.create.long")
	i18nFlag(createCmd, "mockup", "create.flag.mockup")
	i18nFlag(createCmd, "page-mockup", "create.flag.page_mockup")
}

func runCreate(cmd *cobra.Command, args []string) error {
	// Accept both `create module <name>` and `create <name>`.
	if len(args) > 0 && args[0] == "module" {
		args = args[1:]
	}
	if len(args) > 1 {
		return pkg.NewError(i18n.T("cat.module"), i18n.T("create.error.too_many"), pkg.ExitError)
	}

	root, err := requireProjectRoot()
	if err != nil {
		return err
	}

	name := ""
	if len(args) > 0 {
		name = args[0]
	}
	if name == "" {
		if !tui.IsInteractive() {
			return pkg.NewError(i18n.T("cat.input"), i18n.T("create.error.no_name"), pkg.ExitError)
		}
		n, err := tui.AskText(i18n.T("create.prompt.name"), "")
		if err != nil {
			return err
		}
		name = strings.TrimSpace(n)
	}
	if err := module.ValidateName(name); err != nil {
		return pkg.NewError(i18n.T("cat.module"), err.Error(), pkg.ExitError)
	}

	description := ""
	if tui.IsInteractive() {
		d, err := tui.AskText(i18n.T("create.prompt.description"), "")
		if err != nil {
			return err
		}
		description = strings.TrimSpace(d)
	}

	creator := &module.Creator{Root: root, MockupDir: createMockup, PageMockup: createPageMockup}
	if createMockup != "" && !module.IsModuleMockup(createMockup) {
		warn(i18n.Tf("create.warn.mockup_ignored", createMockup))
	}
	if createPageMockup != "" && !pkg.FileExists(createPageMockup) {
		warn(i18n.Tf("create.warn.page_mockup_ignored", createPageMockup))
	}
	result, err := creator.Create(name, description)
	if err != nil {
		if module.IsExistsError(err) {
			return pkg.NewError(i18n.T("cat.module"), err.Error(), pkg.ExitError)
		}
		return err
	}

	// Block the creation when a module required by the manifest (requirements)
	// is not available locally — external_modules/ or internal src/modules/.
	if missing := creator.MissingRequirements(result.Manifest); len(missing) > 0 {
		if err := os.RemoveAll(result.Dir); err != nil {
			debugf("rollback of %s: %v", result.Dir, err)
		}
		if result.Page != "" {
			if err := os.Remove(result.Page); err != nil {
				debugf("rollback of %s: %v", result.Page, err)
			}
		}
		return pkg.NewErrorWithFix(i18n.T("cat.module"),
			i18n.Tf("create.error.requirements_missing", strings.Join(missing, ", ")),
			i18n.T("create.error.requirements_missing.fix"), pkg.ExitError)
	}

	// Resolve the dependencies/devDependencies declared in the manifest with
	// the first available package manager (bun → pnpm → yarn → npm).
	depsPM := ""
	if os.Getenv(createSkipInstallEnv) == "" {
		var err error
		depsPM, err = tui.RunWithSpinner(i18n.T("create.spinner.deps"), creator.ResolveDependencies)
		if err != nil {
			warn(i18n.Tf("create.warn.install", err.Error()))
			depsPM = ""
		} else if depsPM == "" {
			warn(i18n.T("create.warn.pm_none"))
		}
	}

	s := tui.NewStyles()
	panel := s.Success.Render(i18n.Tf("create.success.dir", result.Dir)) + "\n" +
		s.Success.Render(i18n.Tf("create.success.token", result.Token)) + "\n" +
		s.Success.Render(i18n.T("create.success.manifest")) + "\n" +
		s.Success.Render(i18n.T("create.success.entry")) + "\n" +
		s.Success.Render(i18n.T("create.success.requirements"))
	if result.Page != "" {
		panel += "\n" + s.Success.Render(i18n.Tf("create.success.page", result.Page))
	}
	if depsPM != "" {
		panel += "\n" + s.Success.Render(i18n.Tf("create.success.deps", depsPM))
	}
	fmt.Println()
	fmt.Println(s.SuccessPanel(panel))
	fmt.Println(s.StepsList(i18n.T("init.next"),
		s.Info.Render("sentients connect"),
		s.Info.Render("sentients pack "+result.Name),
		s.Info.Render("sentients publish"),
	))
	fmt.Println()
	return nil
}
