package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/protorians/lior-cli/internal/auth"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/tui"
	"github.com/spf13/cobra"
)

var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Connect to Liorian Connect",
	Long: `Authenticates the developer with their liorian-connect account
(email + password, MFA supported) and stores the credentials securely
(system keychain).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runConnect(cmd)
	},
}

func init() {
	i18nHelp(connectCmd, "cmd.connect.short", "cmd.connect.long")
}

func runConnect(cmd *cobra.Command) error {
	return doConnect()
}

// doConnect runs the connect flow: existing-session check, credentials input
// (interactive or CI env vars), sign-in, MFA and secure storage. It is reused
// by `publish` for the spec §5.6 step 1 auto-authenticate behaviour.
func doConnect() error {
	ctx := context.Background()
	store := auth.NewStore()

	// Existing session?
	sess, err := auth.LoadSession(store)
	if err != nil {
		return pkg.NewError(i18n.T("cat.authentication"), err.Error(), pkg.ExitAuth)
	}
	if sess != nil && sess.IsAuthenticated() {
		email := "user"
		if sess.User != nil && sess.User.Email != "" {
			email = sess.User.Email
		}
		fmt.Println()
		fmt.Println(i18n.Tf("connect.already", email))
		if tui.IsInteractive() {
			again, err := tui.Confirm(i18n.T("connect.confirm.reconnect"), false)
			if err != nil {
				return err
			}
			if !again {
				return nil
			}
		}
	}

	// Credentials input. In non-interactive (CI) mode the values come from
	// the environment (LIORIAN_CLI_CONNECT_EMAIL/PASSWORD/MFA_CODE) — the
	// same pattern as `LIORIAN_CLI_YES`.
	email := ""
	if tui.IsInteractive() {
		value, err := tui.AskText(i18n.T("connect.prompt.email"), "")
		if err != nil {
			return err
		}
		email = strings.TrimSpace(value)
	} else {
		email = strings.TrimSpace(os.Getenv("LIORIAN_CLI_CONNECT_EMAIL"))
	}
	if email == "" {
		return pkg.NewError(i18n.T("cat.authentication"), i18n.T("connect.error.email"), pkg.ExitAuth)
	}

	password := ""
	if tui.IsInteractive() {
		value, err := tui.AskSecret(i18n.T("connect.prompt.password"))
		if err != nil {
			return err
		}
		password = value
	} else {
		password = os.Getenv("LIORIAN_CLI_CONNECT_PASSWORD")
	}
	if password == "" {
		return pkg.NewError(i18n.T("cat.authentication"), i18n.T("connect.error.password"), pkg.ExitAuth)
	}

	connector := auth.NewConnector()
	base, source := auth.DebugInfo()
	debugf("API liorian-connect : %s (source: %s)", base, source)

	signIn, err := tui.RunWithSpinner(i18n.T("connect.spinner.checking"), func() (*auth.SignInResponse, error) {
		return connector.SignIn(ctx, auth.SignInRequest{Email: email, Password: password})
	})
	if err != nil {
		return classifyConnectorError("cat.authentication", "connect.error.credentials.fix", err)
	}

	session := &auth.Session{
		Store:       store,
		AccessToken: signIn.Token,
		Device:      signIn.Device,
		User:        &signIn.User,
	}
	t := time.Now().Add(auth.TokenTTL)
	session.ExpiresAt = &t

	// MFA: challenge and verification run against the guarded `/api/mfa/*`
	// endpoints, so the session token must be attached to the connector first.
	authn := &auth.Authenticator{Connector: connector}
	connector.Client.Token = session.AccessToken
	mfaResp, err := runMFA(ctx, authn)
	if err != nil {
		return err
	}
	if mfaResp != nil && mfaResp.MFAVerified && mfaResp.MFAToken != "" {
		session.MFAToken = mfaResp.MFAToken
	}

	if err := session.Save(); err != nil {
		return pkg.NewError(i18n.T("cat.authentication"), i18n.Tf("connect.error.store", err.Error()), pkg.ExitAuth)
	}

	printConnectSummary(session)
	return nil
}

func runMFA(ctx context.Context, authn *auth.Authenticator) (*auth.VerifyResponse, error) {
	prompt := func(factor auth.FactorKind) (string, error) {
		label := i18n.T("connect.mfa.totp")
		if factor == auth.FactorRecovery {
			label = i18n.T("connect.mfa.recovery")
		}
		if !tui.IsInteractive() {
			// CI: read the verification code from the environment.
			if code := os.Getenv("LIORIAN_CLI_MFA_CODE"); code != "" {
				return code, nil
			}
			return "", pkg.NewError(i18n.T("cat.mfa"), i18n.T("connect.mfa.non_interactive"), pkg.ExitMFA)
		}
		return tui.AskText(label, "")
	}

	resp, err := tui.RunWithSpinner(i18n.T("connect.spinner.verify"), func() (*auth.VerifyResponse, error) {
		return authn.Verify(ctx, prompt)
	})
	if err != nil {
		return nil, classifyConnectorError("cat.mfa", "connect.mfa.error.fix", err)
	}
	return resp, nil
}

// classifyConnectorError maps an API/network failure to a categorised CLI error.
// categoryKey and fixKey are i18n keys; the category is localized at render time.
func classifyConnectorError(categoryKey, fixKey string, err error) error {
	if _, ok := err.(*pkg.APIError); ok {
		return pkg.NewErrorWithFix(i18n.T(categoryKey), err.Error(), i18n.T(fixKey), pkg.ExitAuth)
	}
	return pkg.NewErrorWithFix(i18n.T("cat.network"), err.Error(),
		i18n.T("connect.error.network.fix"), pkg.ExitNetwork)
}

func printConnectSummary(session *auth.Session) {
	s := tui.NewStyles()
	email := ""
	role := i18n.T("label.developer")
	if session.User != nil {
		email = session.User.Email
		role = session.User.Role
		if role == "" {
			role = i18n.T("label.developer")
		}
	}
	expiry := "—"
	if session.ExpiresAt != nil {
		expiry = session.ExpiresAt.UTC().Format("2006-01-02 15:04:05 UTC")
	}
	card := s.SummaryCard(
		s.Success.Render(i18n.Tf("connect.success", email)),
		s.KeyValue(i18n.T("label.role"), s.Value.Render(role)),
		s.KeyValue(i18n.T("label.token_expires"), s.Value.Render(expiry)),
	)
	fmt.Println()
	fmt.Println(card)
	fmt.Println()
}
