package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/protorians/lior-cli/internal/appconfig"
	"github.com/protorians/lior-cli/internal/auth"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/tui"
	"github.com/spf13/cobra"
)

var disconnectCmd = &cobra.Command{
	Use:   "disconnect",
	Short: "Disconnect",
	Long:  "Removes all stored credentials and disconnects the developer.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDisconnect(cmd)
	},
}

func init() {
	i18nHelp(disconnectCmd, "cmd.disconnect.short", "cmd.disconnect.long")
}

func runDisconnect(cmd *cobra.Command) error {
	sess, err := auth.LoadSession(auth.NewStore())
	if err != nil {
		return pkg.NewError(i18n.T("cat.authentication"), err.Error(), pkg.ExitAuth)
	}
	if sess == nil || !sess.IsAuthenticated() {
		fmt.Println()
		fmt.Println(tui.NewStyles().Muted.Render(i18n.T("disconnect.none")))
		return nil
	}

	email := ""
	if sess.User != nil {
		email = sess.User.Email
	}
	fmt.Println()
	fmt.Println(i18n.Tf("disconnect.confirm", email))
	confirm, err := tui.Confirm(i18n.T("disconnect.confirm.title"), false)
	if err != nil {
		return err
	}
	if !confirm {
		return nil
	}

	// Best-effort server-side token invalidation (FR-008, POST /api/auth/logout).
	connector := auth.NewConnector()
	connector.Client.Token = sess.AccessToken
	if err := connector.SignOut(context.Background(), sess.Device); err != nil {
		debugf("server-side token invalidation: %v", err)
	}

	// Best-effort OAuth2 revocation (RFC 7009) of the refresh/access tokens
	// obtained via `liorian auth` (spec §8.1 / §6.3).
	oauth := appconfig.Resolved("").OAuth(appconfig.AuthAppID)
	store := auth.NewStore()
	if refresh, rerr := store.Get(auth.KeyOAuthRefreshToken); rerr == nil && strings.TrimSpace(refresh) != "" {
		if err := auth.RevokeToken(context.Background(), connector.Client, oauth.RevokeEndpoint, oauth.ClientID, refresh, "refresh_token"); err != nil {
			debugf("oauth refresh token revocation: %v", err)
		}
	}
	if err := auth.RevokeToken(context.Background(), connector.Client, oauth.RevokeEndpoint, oauth.ClientID, sess.AccessToken, "access_token"); err != nil {
		debugf("oauth access token revocation: %v", err)
	}

	if _, err := tui.RunWithSpinner(i18n.T("disconnect.spinner"), func() (struct{}, error) {
		return struct{}{}, sess.Clear()
	}); err != nil {
		return pkg.NewError(i18n.T("cat.authentication"), i18n.Tf("disconnect.error.remove", err.Error()), pkg.ExitAuth)
	}

	s := tui.NewStyles()
	fmt.Println()
	fmt.Println(s.SummaryCard(
		s.Success.Render(i18n.T("disconnect.success")),
		s.Muted.Render(strings.TrimSpace(i18n.T("disconnect.cleared"))),
	))
	fmt.Println()
	return nil
}
