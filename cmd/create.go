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
	Long: `Creates a new module in external_modules/{domain}/ from the embedded
hello-world mockup, renamed with the given identifier (manifest.json,
index.tsx, package.json, application/, domain/, infrastructure/,
presentation/).

Interactive creation asks for the module domain (reverse-DNS form like
com.organization.domain), the identifier (kebab-case like hello-world),
the application name, and optional version / icon / page url / description.
The optional page url drives both the scaffolded src/app/{url}/ page and
the manifest.json uri of the deployed module (default: the identifier).

The module's unique UUID token is generated automatically.

Usage : sentients create module [name] [--domain com.org.app] [--id hello-world]`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCreate(cmd, args)
	},
}

var (
	createMockup      string
	createPageMockup  string
	createDomain      string
	createID          string
	createAppName     string
	createVersion     string
	createIcon        string
	createURL         string
	createDescription string
	createType        string
	createCategory    string
)

// createSkipInstallEnv disables the dependency installation step of
// `create module` (used by the unit test suite and CI-constrained runs).
const createSkipInstallEnv = "SENTIENT_CLI_SKIP_INSTALL"

func init() {
	createCmd.Flags().StringVar(&createMockup, "mockup", "", i18n.T("create.flag.mockup"))
	createCmd.Flags().StringVar(&createPageMockup, "page-mockup", "", i18n.T("create.flag.page_mockup"))
	createCmd.Flags().StringVar(&createDomain, "domain", "", i18n.T("create.flag.domain"))
	createCmd.Flags().StringVar(&createID, "id", "", i18n.T("create.flag.id"))
	createCmd.Flags().StringVar(&createAppName, "name", "", i18n.T("create.flag.name"))
	createCmd.Flags().StringVar(&createVersion, "version", "", i18n.T("create.flag.version"))
	createCmd.Flags().StringVar(&createIcon, "icon", "", i18n.T("create.flag.icon"))
	createCmd.Flags().StringVar(&createURL, "url", "", i18n.T("create.flag.url"))
	createCmd.Flags().StringVar(&createDescription, "description", "", i18n.T("create.flag.description"))
	createCmd.Flags().StringVar(&createType, "type", "", i18n.T("create.flag.type"))
	createCmd.Flags().StringVar(&createCategory, "category", "", i18n.T("create.flag.category"))
	i18nHelp(createCmd, "cmd.create.short", "cmd.create.long")
	for _, name := range []string{"mockup", "page-mockup", "domain", "id", "name", "version", "icon", "url", "description", "type", "category"} {
		i18nFlag(createCmd, name, "create.flag."+name)
	}
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

	spec := module.ModuleSpec{
		Domain:      strings.TrimSpace(createDomain),
		ID:          strings.TrimSpace(createID),
		AppName:     strings.TrimSpace(createAppName),
		Description: strings.TrimSpace(createDescription),
		Version:     strings.TrimSpace(createVersion),
		Icon:        strings.TrimSpace(createIcon),
		URL:         strings.TrimSpace(createURL),
		Type:        strings.TrimSpace(createType),
		Category:    strings.TrimSpace(createCategory),
	}

	// The positional argument is a convenience shorthand for the identifier.
	if len(args) > 0 {
		if spec.ID != "" && spec.ID != args[0] {
			return pkg.NewError(i18n.T("cat.module"), i18n.T("create.error.id_conflict"), pkg.ExitError)
		}
		spec.ID = args[0]
	}

	if err := collectCreateSpec(&spec); err != nil {
		return err
	}

	creator := &module.Creator{Root: root, MockupDir: createMockup, PageMockup: createPageMockup}
	if createMockup != "" && !module.IsModuleMockup(createMockup) {
		warn(i18n.Tf("create.warn.mockup_ignored", createMockup))
	}
	if createPageMockup != "" && !pkg.FileExists(createPageMockup) {
		warn(i18n.Tf("create.warn.page_mockup_ignored", createPageMockup))
	}
	result, err := creator.Create(spec)
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

// collectCreateSpec completes the module spec with the interactive prompts
// (domain and identifier first) and validates the final values.
func collectCreateSpec(spec *module.ModuleSpec) error {
	if tui.IsInteractive() {
		// The domain and the identifier are asked first.
		if spec.Domain == "" {
			d, err := tui.AskText(i18n.T("create.prompt.domain"), "com.organization.domain")
			if err != nil {
				return err
			}
			spec.Domain = strings.TrimSpace(d)
		}
		if spec.ID == "" {
			id, err := tui.AskText(i18n.T("create.prompt.id"), "")
			if err != nil {
				return err
			}
			spec.ID = strings.TrimSpace(id)
		}
		if spec.AppName == "" {
			n, err := tui.AskText(i18n.T("create.prompt.app_name"), module.DisplayName(spec.ID))
			if err != nil {
				return err
			}
			spec.AppName = strings.TrimSpace(n)
		}
		if spec.Version == "" {
			v, err := tui.AskText(i18n.T("create.prompt.version"), "0.0.0")
			if err != nil {
				return err
			}
			spec.Version = strings.TrimSpace(v)
		}
		if spec.Icon == "" {
			icon, err := tui.AskText(i18n.T("create.prompt.icon"), "PuzzleIcon")
			if err != nil {
				return err
			}
			spec.Icon = strings.TrimSpace(icon)
		}
		if spec.URL == "" {
			u, err := tui.AskText(i18n.T("create.prompt.url"), spec.ID)
			if err != nil {
				return err
			}
			spec.URL = strings.TrimSpace(u)
		}
		if spec.Description == "" {
			desc, err := tui.AskText(i18n.T("create.prompt.description"), "")
			if err != nil {
				return err
			}
			spec.Description = strings.TrimSpace(desc)
		}
	}

	if spec.Domain == "" {
		return pkg.NewError(i18n.T("cat.module"), i18n.T("create.error.no_domain"), pkg.ExitError)
	}
	if err := module.ValidateDomain(spec.Domain); err != nil {
		return pkg.NewError(i18n.T("cat.module"), err.Error(), pkg.ExitError)
	}
	if err := module.ValidateName(spec.ID); err != nil {
		return pkg.NewError(i18n.T("cat.module"), err.Error(), pkg.ExitError)
	}
	if err := module.ValidateVersion(spec.Version); err != nil {
		return pkg.NewError(i18n.T("cat.module"), err.Error(), pkg.ExitError)
	}
	if err := module.ValidateIcon(spec.Icon); err != nil {
		return pkg.NewError(i18n.T("cat.module"), err.Error(), pkg.ExitError)
	}
	if err := module.ValidateType(spec.Type); err != nil {
		return pkg.NewError(i18n.T("cat.module"), err.Error(), pkg.ExitError)
	}
	if err := module.ValidateCategory(spec.Category); err != nil {
		return pkg.NewError(i18n.T("cat.module"), err.Error(), pkg.ExitError)
	}
	return nil
}
