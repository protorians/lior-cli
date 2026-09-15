package cmd

import (
	"fmt"
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

func init() {
	i18nHelp(createCmd, "cmd.create.short", "cmd.create.long")
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

	creator := &module.Creator{Root: root}
	result, err := creator.Create(name, description)
	if err != nil {
		if module.IsExistsError(err) {
			return pkg.NewError(i18n.T("cat.module"), err.Error(), pkg.ExitError)
		}
		return err
	}

	s := tui.NewStyles()
	panel := s.Success.Render(i18n.Tf("create.success.dir", result.Dir)) + "\n" +
		s.Success.Render(i18n.Tf("create.success.token", result.Token)) + "\n" +
		s.Success.Render(i18n.T("create.success.manifest")) + "\n" +
		s.Success.Render(i18n.T("create.success.entry"))
	if result.Page != "" {
		panel += "\n" + s.Success.Render(i18n.Tf("create.success.page", result.Page))
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
