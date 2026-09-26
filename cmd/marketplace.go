package cmd

import (
	"context"
	"fmt"

	"github.com/protorians/lior-cli/internal/catalog"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/tui"
	"github.com/spf13/cobra"
)

var (
	marketplaceCategory      string
	marketplaceLimit         int
	marketplaceOffset        int
	marketplaceForce         bool
	marketplaceAllowUnsigned bool
)

var marketplaceCmd = &cobra.Command{
	Use:   "marketplace",
	Short: i18n.T("cmd.marketplace.short"),
	Long:  i18n.T("cmd.marketplace.long"),
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var marketplaceSearchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: i18n.T("cmd.marketplace.search.short"),
	Long:  i18n.T("cmd.marketplace.search.long"),
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMarketplaceSearch(cmd, args)
	},
}

var marketplaceInstallCmd = &cobra.Command{
	Use:   "install <module>",
	Short: i18n.T("cmd.marketplace.install.short"),
	Long:  i18n.T("cmd.marketplace.install.long"),
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMarketplaceInstall(cmd, args)
	},
}

func init() {
	marketplaceSearchCmd.Flags().StringVar(&marketplaceCategory, "category", "", i18n.T("marketplace.flag.category"))
	i18nFlag(marketplaceSearchCmd, "category", "marketplace.flag.category")
	marketplaceSearchCmd.Flags().IntVar(&marketplaceLimit, "limit", 20, i18n.T("marketplace.flag.limit"))
	i18nFlag(marketplaceSearchCmd, "limit", "marketplace.flag.limit")
	marketplaceSearchCmd.Flags().IntVar(&marketplaceOffset, "offset", 0, i18n.T("marketplace.flag.offset"))
	i18nFlag(marketplaceSearchCmd, "offset", "marketplace.flag.offset")

	marketplaceInstallCmd.Flags().BoolVar(&marketplaceForce, "force", false, i18n.T("marketplace.flag.force"))
	i18nFlag(marketplaceInstallCmd, "force", "marketplace.flag.force")
	marketplaceInstallCmd.Flags().BoolVar(&marketplaceAllowUnsigned, "allow-unsigned", false, i18n.T("marketplace.flag.allow_unsigned"))
	i18nFlag(marketplaceInstallCmd, "allow-unsigned", "marketplace.flag.allow_unsigned")

	marketplaceCmd.AddCommand(marketplaceSearchCmd, marketplaceInstallCmd)
	i18nHelp(marketplaceCmd, "cmd.marketplace.short", "cmd.marketplace.long")
	i18nHelp(marketplaceSearchCmd, "cmd.marketplace.search.short", "cmd.marketplace.search.long")
	i18nHelp(marketplaceInstallCmd, "cmd.marketplace.install.short", "cmd.marketplace.install.long")
}

// runMarketplaceSearch queries the public catalog and prints the results.
func runMarketplaceSearch(cmd *cobra.Command, args []string) error {
	query := ""
	if len(args) > 0 {
		query = args[0]
	}
	opts := catalog.SearchOptions{
		Query:    query,
		Category: marketplaceCategory,
		Limit:    marketplaceLimit,
	}
	if opts.Limit <= 0 {
		opts.Limit = 20
	}
	opts.Offset = marketplaceOffset

	res, err := tui.RunWithSpinner(i18n.T("marketplace.spinner.search"), func() (*catalog.SearchResult, error) {
		return catalog.NewClient().Search(context.Background(), opts)
	})
	if err != nil {
		return pkg.NewErrorWithFix(i18n.T("cat.marketplace"),
			err.Error(),
			i18n.T("marketplace.error.search.fix"),
			pkg.ExitNetwork)
	}

	s := tui.NewStyles()
	if res == nil || len(res.Items) == 0 {
		fmt.Println()
		fmt.Println(s.NeutralPanel(
			s.Warning.Render(i18n.T("marketplace.search.none")) + "\n\n" +
				s.KeyValue(i18n.T("label.query"), s.Value.Render(query)) + "\n" +
				s.Hint.Render("  "+i18n.T("marketplace.search.none.fix")),
		))
		return nil
	}

	t := tui.NewTable([]string{
		i18n.T("label.module"),
		i18n.T("label.version"),
		i18n.T("label.category"),
		i18n.T("label.publisher"),
	})
	for _, m := range res.Items {
		t.AddRow(m.Slug, m.Version, m.PrimaryCategory, publisherName(m))
	}

	fmt.Println()
	fmt.Println(s.SubHeader.Render(i18n.Tf("marketplace.search.results", res.Total)))
	fmt.Println()
	fmt.Print(t.Render())
	fmt.Println()
	return nil
}

// runMarketplaceInstall downloads, verifies and extracts a module.
func runMarketplaceInstall(cmd *cobra.Command, args []string) error {
	root, err := requireProjectRoot()
	if err != nil {
		return err
	}

	inst := &catalog.Installer{Root: root, Force: marketplaceForce, AllowUnsigned: marketplaceAllowUnsigned}
	result, err := tui.RunWithSpinner(i18n.Tf("marketplace.spinner.install", args[0]), func() (*catalog.InstallResult, error) {
		return inst.Install(context.Background(), args[0])
	})
	if err != nil {
		if e, ok := err.(*pkg.Error); ok {
			return e
		}
		return pkg.NewError(i18n.T("cat.marketplace"), err.Error(), pkg.ExitError)
	}
	if result == nil {
		return nil // the replace confirmation was declined
	}

	s := tui.NewStyles()
	for _, w := range result.Warnings {
		warn(w)
	}

	fmt.Println()
	fmt.Println(s.SummaryCard(
		s.Success.Render(i18n.T("marketplace.install.success")),
		s.KeyValue(i18n.T("label.module"),
			s.Value.Render(result.Module+" v"+result.Version)),
		s.KeyValue(i18n.T("label.signature"), signatureStatus(s, result.SignatureStatus)),
		s.KeyValue(i18n.T("marketplace.install.files"),
			s.Value.Render(fmt.Sprintf("%d", result.Files))),
	))
	fmt.Println()
	return nil
}

// signatureStatus renders the archive signature verdict.
func signatureStatus(s *tui.Styles, status string) string {
	switch status {
	case catalog.SignatureVerified:
		return s.Success.Render(i18n.T("marketplace.verify.verified"))
	case catalog.SignatureUnverified:
		return s.Warning.Render(i18n.T("marketplace.verify.unverified_label"))
	default:
		return s.Warning.Render(i18n.T("marketplace.verify.missing"))
	}
}

// publisherName returns the publisher label of a catalog entry.
func publisherName(m catalog.CatalogModule) string {
	if m.Publisher.Name != "" {
		return m.Publisher.Name
	}
	if m.Publisher.ID != "" {
		return m.Publisher.ID
	}
	return "—"
}
