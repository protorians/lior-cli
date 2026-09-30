package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/protorians/lior-cli/internal/catalog"
	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/localinstall"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/tui"
	"github.com/spf13/cobra"
)

var localInstallForce bool

var localInstallCmd = &cobra.Command{
	Use:   "install <archive.LiorArtifactPackage>",
	Short: "Install a local module archive (.LiorArtifactPackage)",
	Long: `Installs a local .LiorArtifactPackage archive into the workspace — without the
marketplace and without api-core.

The archive is audited and validated by the same fail-closed engine as
"liora marketplace install", then installed multi-version into
library/modules/<id>/<version>/ (manifest.json + src/** + artifact/**),
and the current version pointer is moved to the installed version.

A .sig sidecar next to the archive is verified against the developer
keychain when present; a missing or unverifiable signature is a warning —
the marketplace/api-core chain remains the trust authority.

Use --force to replace an already-installed version.`,
	Example: `  liora install .lorian/build/blog-manager-1.2.0.LiorArtifactPackage
  liora install ./dist/blog-manager-1.2.0.LiorArtifactPackage --force`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runLocalInstall(args[0])
	},
}

func init() {
	localInstallCmd.Flags().BoolVar(&localInstallForce, "force", false, i18n.T("install.flag.force"))
	i18nHelp(localInstallCmd, "cmd.install.short", "cmd.install.long")
	i18nFlag(localInstallCmd, "force", "install.flag.force")
}

func runLocalInstall(archiveArg string) error {
	root, err := requireProjectRoot()
	if err != nil {
		return err
	}

	archivePath := strings.TrimSpace(archiveArg)
	if !filepath.IsAbs(archivePath) {
		archivePath, err = filepath.Abs(archivePath)
		if err != nil {
			return pkg.NewError(i18n.T("cat.install"), err.Error(), pkg.ExitError)
		}
	}
	if !pkg.FileExists(archivePath) && !strings.HasSuffix(archivePath, config.ArchiveExt) {
		if pkg.FileExists(archivePath + config.ArchiveExt) {
			archivePath += config.ArchiveExt
		}
	}
	if !pkg.FileExists(archivePath) {
		return pkg.NewError(i18n.T("cat.install"),
			i18n.Tf("install.error.archive_missing", archiveArg), pkg.ExitError)
	}

	res, err := tui.RunWithSpinner(i18n.T("install.spinner"), func() (*catalog.InstallResult, error) {
		installer := &localinstall.Installer{Root: root, Force: localInstallForce}
		return installer.Install(archivePath)
	})
	if err != nil {
		if e, ok := err.(*pkg.Error); ok {
			return e
		}
		return pkg.NewError(i18n.T("cat.install"), err.Error(), pkg.ExitError)
	}

	s := tui.NewStyles()
	fmt.Println()
	rows := []string{
		s.KeyValue(i18n.T("label.module"), res.Module+" v"+res.Version),
		s.KeyValue(i18n.T("label.destination"), s.Info.Render(config.ExternalModulesDir+"/"+res.Module+"/"+res.Version+"/")),
	}
	if res.Files > 0 {
		rows = append(rows, s.KeyValue(i18n.T("label.files"), s.Value.Render(fmt.Sprintf("%d", res.Files))))
	}
	switch res.SignatureStatus {
	case catalog.SignatureVerified:
		rows = append(rows, s.KeyValue(i18n.T("label.signature"), s.Success.Render(i18n.T("sign.verified"))))
	case catalog.SignatureUnverified:
		rows = append(rows, s.KeyValue(i18n.T("label.signature"), s.Warning.Render(i18n.T("sign.unverified"))))
	default:
		rows = append(rows, s.KeyValue(i18n.T("label.signature"), s.Warning.Render(i18n.T("sign.skipped.no_key"))))
	}
	for _, warning := range res.Warnings {
		rows = append(rows, s.KeyValue(i18n.T("label.warning"), s.Warning.Render(warning)))
	}

	fmt.Println(s.SummaryCard(
		s.Success.Render(i18n.T("install.success")),
		rows...,
	))
	fmt.Println()
	return nil
}
