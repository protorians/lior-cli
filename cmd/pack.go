package cmd

import (
	"fmt"

	"github.com/protorians/sentient-cli/internal/i18n"
	"github.com/protorians/sentient-cli/internal/module"
	"github.com/protorians/sentient-cli/internal/pkg"
	"github.com/protorians/sentient-cli/internal/tui"
	"github.com/spf13/cobra"
)

var packCmd = &cobra.Command{
	Use:   "pack [module]",
	Short: "Build a module archive (.smp)",
	Long: `Builds a module and creates a compressed .smp archive
(moved to .sentients/build/).

Without an argument, a selector lets you choose the module.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPack(cmd, args)
	},
}

func init() {
	i18nHelp(packCmd, "cmd.pack.short", "cmd.pack.long")
}

func runPack(cmd *cobra.Command, args []string) error {
	root, err := requireProjectRoot()
	if err != nil {
		return err
	}

	name, err := resolveModule(root, args)
	if err != nil {
		return err
	}

	result, err := tui.RunWithSpinner(i18n.T("pack.spinner"), func() (*module.PackResult, error) {
		p := &module.Packer{Root: root}
		return p.Pack(name)
	})
	if err != nil {
		if _, ok := err.(*pkg.Error); ok {
			return err
		}
		return pkg.NewError(i18n.T("cat.pack"), err.Error(), pkg.ExitBuild)
	}

	s := tui.NewStyles()
	fmt.Println()
	fmt.Println(s.SummaryCard(
		s.Success.Render(i18n.T("pack.success")),
		s.KeyValue(i18n.T("label.module"), name+" v"+result.Version),
		s.KeyValue(i18n.T("label.file"), s.Info.Render(result.Path)),
		s.KeyValue(i18n.T("label.size"), humanSize(result.Size)),
	))
	fmt.Println()
	return nil
}

// humanSize renders a byte count in a human-friendly way.
func humanSize(bytes int64) string {
	const kb = 1024
	const mb = kb * 1024
	const gb = mb * 1024
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
