package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/protorians/lior-cli/internal/artifactdev"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/socle"
	"github.com/protorians/lior-cli/internal/tui"
	"github.com/spf13/cobra"
)

// socleCmd regroupe les commandes du socle hébergeant : le serveur de
// développement central qui remplace le dev-server par module.
var socleCmd = &cobra.Command{
	Use:   "socle",
	Args:  cobra.NoArgs,
	Short: i18n.T("cmd.socle.short"),
	Long:  i18n.T("cmd.socle.long"),
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var (
	socleDevPort   int
	socleDevHost   string
	socleDevHTTPS  bool
	socleDevHTTP   bool
	socleDevStrict bool
	socleDevNoApp  bool
)

var socleDevCmd = &cobra.Command{
	Use:   "dev [socle]",
	Args:  cobra.MaximumNArgs(1),
	Short: i18n.T("cmd.socle_dev.short"),
	Long:  i18n.T("cmd.socle_dev.long"),
	RunE: func(cmd *cobra.Command, args []string) error {
		socleDir, err := resolveSocleDirArg(args)
		if err != nil {
			return err
		}
		dev := artifactdev.DevOptions{
			Port:       socleDevPort,
			Host:       socleDevHost,
			StrictPort: socleDevStrict,
		}
		switch {
		case socleDevHTTPS:
			dev.HTTPS = &artifactHTTPSFlagTrue
		case socleDevHTTP:
			dev.HTTPS = &artifactHTTPSFlagFalse
		}
		return runCentralDev(centralDevOptions{
			SocleDir: socleDir,
			Dev:      dev,
			StartApp: !socleDevNoApp,
		})
	},
}

func init() {
	socleCmd.AddCommand(socleDevCmd)

	socleDevCmd.Flags().IntVar(&socleDevPort, "port", 0, i18n.T("artifact.flag.port"))
	socleDevCmd.Flags().StringVar(&socleDevHost, "host", "", i18n.T("artifact.flag.host"))
	socleDevCmd.Flags().BoolVar(&socleDevHTTPS, "https", false, i18n.T("artifact.flag.https"))
	socleDevCmd.Flags().BoolVar(&socleDevHTTP, "http", false, i18n.T("artifact.flag.http"))
	socleDevCmd.Flags().BoolVar(&socleDevStrict, "strict-port", false, i18n.T("artifact.flag.strict_port"))
	socleDevCmd.Flags().BoolVar(&socleDevNoApp, "no-app", false, i18n.T("socle.flag.no_app"))

	i18nHelp(socleCmd, "cmd.socle.short", "cmd.socle.long")
	i18nHelp(socleDevCmd, "cmd.socle_dev.short", "cmd.socle_dev.long")
	for name, key := range map[string]string{
		"port": "artifact.flag.port", "host": "artifact.flag.host",
		"https": "artifact.flag.https", "http": "artifact.flag.http",
		"strict-port": "artifact.flag.strict_port", "no-app": "socle.flag.no_app",
	} {
		i18nFlag(socleDevCmd, name, key)
	}
}

// resolveSocleDirArg résout le socle visé par `liora socle dev` : l'argument
// (absolu ou depuis le répertoire d'invocation), sinon le répertoire courant,
// sinon le socle le plus proche des parents — la commande est lancée depuis le
// socle (`bun run dev`), souvent depuis un module ou la racine du workspace.
func resolveSocleDirArg(args []string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", pkg.NewError(i18n.T("cat.project"), i18n.T("modules.error.cwd"), pkg.ExitError)
	}
	target := cwd
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		target = args[0]
		if !filepath.IsAbs(target) {
			target = filepath.Join(cwd, target)
		}
		target = filepath.Clean(target)
		if !pkg.DirExists(target) {
			return "", pkg.NewErrorWithFix(i18n.T("cat.project"),
				i18n.Tf("socle.dev.dir_absent", target),
				i18n.T("socle.dev.not_socle.fix"), pkg.ExitError)
		}
	}
	if socle.IsSocle(target) {
		return filepath.Abs(target)
	}
	if found := socle.FindSocle(target); found != "" {
		return found, nil
	}
	return "", pkg.NewErrorWithFix(i18n.T("cat.toolchain"),
		i18n.Tf("socle.dev.not_socle", target),
		i18n.T("socle.dev.not_socle.fix"), pkg.ExitError)
}

// printCentralDelegation annonce la délégation de `liora artifact dev` au
// serveur central du socle : un seul serveur héberge les modules liés.
func printCentralDelegation(socleDir string) {
	s := tui.NewStyles()
	fmt.Println(s.Info.Render(i18n.Tf("artifact.dev.delegated", socleDir)))
}
