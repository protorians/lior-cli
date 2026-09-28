package cmd

import (
	"fmt"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/module"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/tui"
	"github.com/spf13/cobra"
)

var typecheckCmd = &cobra.Command{
	Use:   "typecheck [module]",
	Short: "Type-check a module (tsc --noEmit)",
	Long: `Runs the TypeScript gate (rule 2 of the isolated-runtime pack rules,
docs/specs/applications/module-isolated-runtime.md §4.4) on one module without
producing an archive — the same pass ` + "`liora pack`" + ` runs before packing.

The module is resolved from the workspace source tree first (modules/<id>),
then from the installation tree (library/modules/<id>). Without an argument,
the module is selected from those available.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTypecheckCmd(args)
	},
}

func runTypecheckCmd(args []string) error {
	root, err := requireProjectRoot()
	if err != nil {
		return err
	}

	name, err := resolveModule(root, args)
	if err != nil {
		return err
	}

	dir := config.WorkspaceModuleDir(root, name)
	if !pkg.DirExists(dir) {
		dir = config.ResolveInstalledModuleDir(root, name)
	}

	if err := module.RunTypecheck(dir); err != nil {
		return pkg.NewError("typecheck", err.Error(), pkg.ExitError)
	}

	s := tui.NewStyles()
	fmt.Println(s.Success.Render("✓ " + name + " — tsc --noEmit passed"))
	return nil
}
