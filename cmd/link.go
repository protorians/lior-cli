package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/protorians/sentient-cli/internal/auth"
	"github.com/protorians/sentient-cli/internal/config"
	"github.com/protorians/sentient-cli/internal/i18n"
	"github.com/protorians/sentient-cli/internal/module"
	"github.com/protorians/sentient-cli/internal/pkg"
	"github.com/protorians/sentient-cli/internal/store"
	"github.com/protorians/sentient-cli/internal/tui"
	"github.com/spf13/cobra"
)

var linkCmd = &cobra.Command{
	Use:   "link [module] [token]",
	Short: "Link a local module to a remote module",
	Long: `Links a module created in sentient-connect with the local module (via its token).

Checks authentication, lists the online modules, and associates
the chosen local module with the provided remote token.

In non-interactive (CI) mode, provide the local module and the remote
token as arguments: link <module> <token>.`,
	Args: cobra.MaximumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runLink(cmd, args)
	},
}

var unlinkCmd = &cobra.Command{
	Use:   "unlink [module]",
	Short: "Unlink a local module from sentient-connect",
	Long: `Unlinks a local module from its counterpart in sentient-connect.
The manifest.json token is replaced with a new local UUID token.

--sync-remote first syncs the local metadata (name, type,
description) to the remote product via PUT /api/developer-store/modules/:id
(best-effort, requires being connected).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runUnlink(cmd, args)
	},
}

var flagUnlinkSyncRemote bool

func init() {
	unlinkCmd.Flags().BoolVar(&flagUnlinkSyncRemote, "sync-remote", false,
		i18n.T("flag.sync_remote"))
	i18nHelp(linkCmd, "cmd.link.short", "cmd.link.long")
	i18nHelp(unlinkCmd, "cmd.unlink.short", "cmd.unlink.long")
}

func runLink(cmd *cobra.Command, args []string) error {
	root, err := requireProjectRoot()
	if err != nil {
		return err
	}

	// Check auth
	sess, err := auth.LoadSession(auth.NewStore())
	if err != nil || sess == nil || !sess.IsAuthenticated() {
		return pkg.NewErrorWithFix(i18n.T("cat.authentication"),
			i18n.T("link.error.not_connected"),
			i18n.T("publish.error.connect.fix"), pkg.ExitAuth)
	}

	// Non-interactive: module + token supplied as arguments.
	if !tui.IsInteractive() && len(args) < 2 {
		return pkg.NewErrorWithFix(i18n.T("cat.selection"),
			i18n.T("link.error.non_interactive"),
			i18n.T("link.error.non_interactive.fix"), pkg.ExitError)
	}

	// Select local module
	moduleArg := []string{}
	if len(args) > 0 {
		moduleArg = []string{args[0]}
	}
	localName, err := resolveModule(root, moduleArg)
	if err != nil {
		return err
	}

	// Fetch remote modules
	client := store.NewClient()
	client.SetToken(sess.AccessToken)
	client.WithAutoRefresh(sess)

	remoteModules, err := tui.RunWithSpinner(i18n.T("link.spinner.fetch"), func() ([]store.RemoteModule, error) {
		return client.ListModules(context.Background())
	})
	if err != nil {
		return pkg.NewErrorWithFix(i18n.T("cat.store"), err.Error(),
			i18n.T("link.error.fetch.fix"), pkg.ExitNetwork)
	}

	if len(remoteModules) == 0 && len(args) < 2 {
		return pkg.NewErrorWithFix(i18n.T("cat.store"),
			i18n.T("link.error.no_modules"),
			i18n.T("link.error.no_modules.fix"),
			pkg.ExitError)
	}

	// Ask for remote token
	remoteToken := ""
	if len(args) > 1 {
		remoteToken = args[1]
	} else if tui.IsInteractive() {
		var items []string
		for _, m := range remoteModules {
			items = append(items, fmt.Sprintf("%s — %s v%s", m.Token, m.Name, m.Version))
		}
		selected, err := tui.Select(i18n.T("link.prompt.remote"), items)
		if err != nil {
			return err
		}
		remoteToken = strings.Split(selected, " — ")[0]
	} else {
		return pkg.NewError(i18n.T("cat.selection"),
			i18n.T("link.error.no_token"),
			pkg.ExitError)
	}

	// Validate remote module
	remote, err := tui.RunWithSpinner(i18n.T("link.spinner.verify"), func() (*store.RemoteModuleResponse, error) {
		return client.GetModule(context.Background(), remoteToken)
	})
	if err != nil {
		return pkg.NewErrorWithFix(i18n.T("cat.store"), err.Error(),
			i18n.T("link.error.remote.fix"), pkg.ExitError)
	}

	// Link
	linker := &module.Linker{Root: root}
	result, err := linker.Link(localName, module.RemoteInfo{
		Token:         remote.Token,
		Name:          remote.Name,
		Description:   remote.Description,
		Version:       remote.Version,
		PublisherID:   remote.Publisher.ID,
		PublisherName: remote.Publisher.Name,
	})
	if err != nil {
		return pkg.NewError(i18n.T("cat.link"), err.Error(), pkg.ExitError)
	}

	s := tui.NewStyles()
	fmt.Println()
	fmt.Println(s.SummaryCard(
		s.Success.Render(i18n.T("link.success")),
		s.KeyValue(i18n.T("label.local"), config.ExternalModulesDir+"/"+result.LocalModule+"/"),
		s.KeyValue(i18n.T("label.remote"),
			fmt.Sprintf("%s (%s v%s)", result.RemoteToken, result.RemoteName, result.RemoteVersion)),
	))
	fmt.Println()
	return nil
}

func runUnlink(cmd *cobra.Command, args []string) error {
	root, err := requireProjectRoot()
	if err != nil {
		return err
	}

	linker := &module.Linker{Root: root}

	// Find linked modules (spec §5.8: list local modules with a remote token)
	linked, err := linker.LinkedModules()
	if err != nil {
		return err
	}
	if len(linked) == 0 {
		return pkg.NewError(i18n.T("cat.module"),
			i18n.T("link.error.none_linked"),
			pkg.ExitModuleNotFound)
	}

	// Select module to unlink: positional arg, or interactive menu.
	localName := ""
	if len(args) > 0 {
		localName = args[0]
		if !pkg.DirExists(filepath.Join(root, config.ExternalModulesDir, localName)) {
			return pkg.NewErrorWithFix(
				i18n.T("cat.module"),
				i18n.Tf("link.error.module_absent", localName, config.ExternalModulesDir),
				i18n.T("link.error.module_absent.fix"),
				pkg.ExitModuleNotFound,
			)
		}
		linkedOk := false
		for _, n := range linked {
			if n == localName {
				linkedOk = true
				break
			}
		}
		if !linkedOk {
			return pkg.NewError(i18n.T("cat.module"),
				i18n.Tf("link.error.not_linked", localName),
				pkg.ExitError)
		}
	} else if len(linked) > 1 {
		if !tui.IsInteractive() {
			return pkg.NewError(i18n.T("cat.selection"),
				i18n.T("link.error.multi"),
				pkg.ExitError)
		}
		selected, err := tui.Select(i18n.T("link.prompt.unlink"), linked)
		if err != nil {
			return err
		}
		localName = selected
	} else {
		localName = linked[0]
	}

	// Show the current remote binding
	if m, err := module.LoadManifest(config.ManifestPath(root, localName)); err == nil && m.Token != "" {
		remote := fmt.Sprintf("%s (v%s)", m.Name, m.Version)
		if m.Name == "" {
			remote = m.Version
		}
		fmt.Println(i18n.Tf("link.current", m.Token, remote))
	}

	// Optional remote metadata sync before unlinking (PUT /modules/:id).
	if flagUnlinkSyncRemote {
		remoteToken := linker.RemoteToken(localName)
		if remoteToken == "" {
			warn(i18n.T("link.warn.no_remote"))
		} else if err := syncRemoteBeforeUnlink(root, localName, remoteToken); err != nil {
			debugf("remote sync before unlink: %v", err)
			warn(i18n.Tf("link.warn.sync_failed", err.Error()))
		} else {
			fmt.Println(i18n.T("link.success.synced"))
		}
	}

	// Confirm
	if tui.IsInteractive() {
		confirm, err := tui.Confirm(i18n.Tf("link.confirm.unlink", localName), false)
		if err != nil {
			return err
		}
		if !confirm {
			return nil
		}
	}

	if err := linker.Unlink(localName); err != nil {
		return pkg.NewError(i18n.T("cat.unlink"), err.Error(), pkg.ExitError)
	}

	s := tui.NewStyles()
	fmt.Println()
	fmt.Println(s.SummaryCard(
		s.Success.Render(i18n.T("link.success.unlink")),
		s.Value.Render(i18n.Tf("link.unlinked", config.ExternalModulesDir+"/"+localName+"/")),
	))
	fmt.Println()
	return nil
}

// syncRemoteBeforeUnlink pushes the local manifest metadata to the remote
// product (best-effort) — the developer-store PUT endpoint.
func syncRemoteBeforeUnlink(root, localName, remoteToken string) error {
	sess, err := auth.LoadSession(auth.NewStore())
	if err != nil || sess == nil || !sess.IsAuthenticated() {
		return fmt.Errorf("%s", i18n.T("auth.error.not_authenticated"))
	}
	m, err := module.LoadManifest(config.ManifestPath(root, localName))
	if err != nil {
		return err
	}
	client := store.NewClient()
	client.SetToken(sess.AccessToken)
	client.WithAutoRefresh(sess)
	return client.UpdateModule(context.Background(), remoteToken, m)
}
