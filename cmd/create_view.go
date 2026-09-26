package cmd

import (
	"fmt"
	"strings"

	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/module"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/tui"
	"github.com/spf13/cobra"
)

var createViewCmd = &cobra.Command{
	Use:   "view [name]",
	Short: "Create a new view for a module",
	Long: `Creates a new presentation view inside an existing module at
library/modules/<module>/presentation/views/<view-name>.view.tsx from the
embedded hello-world view mockup, renamed with the given identifier
(component <ViewName>, displayed title and description).

The module is resolved interactively (or passed as the first positional
argument) and the view identifier is deduced from the domain when omitted.

Usage : liora create view <module> [name] [--name my-view] [--label "My View"]`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCreateView(cmd, args)
	},
}

var (
	createViewMockup      string
	createViewName        string
	createViewLabel       string
	createViewDescription string
)

func init() {
	createViewCmd.Flags().StringVar(&createViewMockup, "mockup", "", i18n.T("create_view.flag.mockup"))
	createViewCmd.Flags().StringVar(&createViewName, "name", "", i18n.T("create_view.flag.name"))
	createViewCmd.Flags().StringVar(&createViewLabel, "label", "", i18n.T("create_view.flag.label"))
	createViewCmd.Flags().StringVar(&createViewDescription, "description", "", i18n.T("create_view.flag.description"))
	i18nHelp(createViewCmd, "cmd.create_view.short", "cmd.create_view.long")
	for _, name := range []string{"mockup", "name", "label", "description"} {
		i18nFlag(createViewCmd, name, "create_view.flag."+name)
	}
	createCmd.AddCommand(createViewCmd)
}

func runCreateView(cmd *cobra.Command, args []string) error {
	root, err := requireProjectRoot()
	if err != nil {
		return err
	}

	// The first positional argument names the module; the second, if any,
	// names the view (unless --name is set).
	moduleArg := ""
	if len(args) > 0 {
		moduleArg = args[0]
	}
	spec := module.ViewSpec{
		Name:        strings.TrimSpace(createViewName),
		Label:       strings.TrimSpace(createViewLabel),
		Description: strings.TrimSpace(createViewDescription),
	}
	if len(args) > 1 {
		if spec.Name != "" && spec.Name != args[1] {
			return pkg.NewError(i18n.T("cat.view"), i18n.T("create_view.error.name_conflict"), pkg.ExitError)
		}
		spec.Name = args[1]
	}

	// Resolve the target module (interactive selection or positional arg).
	selector := []string{}
	if moduleArg != "" {
		selector = []string{moduleArg}
	}
	spec.Module, err = resolveModule(root, selector)
	if err != nil {
		return err
	}

	if err := collectViewSpec(&spec); err != nil {
		return err
	}

	creator := &module.ViewCreator{Root: root, MockupDir: createViewMockup}
	if createViewMockup != "" && !pkg.FileExists(createViewMockup) {
		warn(i18n.Tf("create_view.warn.mockup_ignored", createViewMockup))
	}
	result, err := creator.Create(spec)
	if err != nil {
		if module.IsViewExistsError(err) {
			return pkg.NewError(i18n.T("cat.view"), err.Error(), pkg.ExitError)
		}
		return err
	}

	s := tui.NewStyles()
	panel := s.Success.Render(i18n.Tf("create_view.success.path", relToRoot(root, result.Path))) + "\n" +
		s.Success.Render(i18n.Tf("create_view.success.component", module.ViewComponentName(result.Name)))
	fmt.Println()
	fmt.Println(s.SuccessPanel(panel))
	fmt.Println()
	return nil
}

// collectViewSpec completes the view spec with the interactive prompts and
// validates each answer before moving on to the next step.
func collectViewSpec(spec *module.ViewSpec) error {
	if !tui.IsInteractive() {
		if spec.Name == "" {
			return pkg.NewError(i18n.T("cat.view"), i18n.T("create_view.error.no_name"), pkg.ExitError)
		}
		return validateViewSpec(spec)
	}

	if spec.Name == "" {
		name, err := askValidated(i18n.T("create_view.prompt.name"), "my-view", module.ValidateName)
		if err != nil {
			return err
		}
		spec.Name = name
	}
	if spec.Label == "" {
		def := module.DisplayName(spec.Name)
		label, err := tui.AskText(i18n.T("create_view.prompt.label"), def)
		if err != nil {
			return err
		}
		spec.Label = strings.TrimSpace(label)
		if spec.Label == "" {
			spec.Label = def
		}
	}
	if spec.Description == "" {
		desc, err := tui.AskText(i18n.T("create_view.prompt.description"), "")
		if err != nil {
			return err
		}
		spec.Description = strings.TrimSpace(desc)
	}
	return validateViewSpec(spec)
}

// validateViewSpec checks the required view fields before scaffolding.
func validateViewSpec(spec *module.ViewSpec) error {
	if spec.Module == "" {
		return pkg.NewError(i18n.T("cat.view"), i18n.T("view.error.no_module"), pkg.ExitError)
	}
	if err := module.ValidateName(spec.Name); err != nil {
		return pkg.NewError(i18n.T("cat.view"), err.Error(), pkg.ExitError)
	}
	return nil
}
