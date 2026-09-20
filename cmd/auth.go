package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jetbrains/lior-cli/internal/appconfig"
	"github.com/jetbrains/lior-cli/internal/auth"
	"github.com/jetbrains/lior-cli/internal/i18n"
	"github.com/jetbrains/lior-cli/internal/pkg"
	"github.com/jetbrains/lior-cli/internal/tui"
	"github.com/spf13/cobra"
)

// EnvAuthCode lets a non-interactive (CI/headless) run feed the authorization
// code directly, skipping the browser and the local callback server — the same
// pattern as `LIORIAN_CLI_CONNECT_*`.
const envAuthCode = "LIORIAN_CLI_AUTH_CODE"

// authCallbackPath is the loopback path receiving the OAuth redirect.
const authCallbackPath = "/callback"

// browserWaitTimeout bounds the time spent waiting for the browser redirect.
const browserWaitTimeout = 5 * time.Minute

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authenticate via OAuth2 (browser)",
	Long: `Authenticates the developer using the OAuth2 authorization-code flow
with PKCE (RFC 7636): opens the browser on the liorian-auth authorization
endpoint, receives the redirect on a local loopback server, exchanges the code
for tokens, then stores the session securely (system keychain).

The OAuth endpoints come from the 'oauth' entry of 'liorian-auth' in
app.config.json (authorizationEndpoint, tokenEndpoint, revokeEndpoint,
clientId, scopes).

In non-interactive (CI) mode, provide the authorization code via the
LIORIAN_CLI_AUTH_CODE environment variable to skip the browser step.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAuth()
	},
}

func init() {
	i18nHelp(authCmd, "cmd.auth.short", "cmd.auth.long")
}

func runAuth() error {
	ctx := context.Background()
	store := auth.NewStore()

	sess, err := auth.LoadSession(store)
	if err != nil {
		return pkg.NewError(i18n.T("cat.authentication"), err.Error(), pkg.ExitAuth)
	}
	if sess != nil && sess.IsAuthenticated() {
		email := ""
		if sess.User != nil {
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

	connector := auth.NewConnector()
	base, source := auth.DebugInfo()
	debugf("API liorian-auth : %s (source: %s)", base, source)

	oauth := appconfig.Resolved("").OAuth(appconfig.AuthAppID)
	scope := strings.Join(oauth.Scopes, " ")

	pkce, err := auth.GeneratePKCE()
	if err != nil {
		return pkg.NewError(i18n.T("cat.authentication"), err.Error(), pkg.ExitAuth)
	}
	state, err := auth.RandomState()
	if err != nil {
		return pkg.NewError(i18n.T("cat.authentication"), err.Error(), pkg.ExitAuth)
	}

	redirectURI := "http://127.0.0.1" + authCallbackPath
	code := strings.TrimSpace(os.Getenv(envAuthCode))
	if code == "" && !tui.IsInteractive() {
		return pkg.NewErrorWithFix(i18n.T("cat.authentication"),
			i18n.T("auth.oauth.error.code"),
			i18n.T("auth.oauth.non_interactive.fix"),
			pkg.ExitAuth)
	}
	if code == "" {
		callback, uri, startErr := auth.StartLoopbackCallback(authCallbackPath)
		if startErr != nil {
			return pkg.NewError(i18n.T("cat.authentication"), startErr.Error(), pkg.ExitAuth)
		}
		defer callback.Close()
		redirectURI = uri

		authURL, err := auth.AuthorizationURL(base, oauth.AuthorizationEndpoint,
			oauth.ClientID, redirectURI, scope, pkce.Challenge, state)
		if err != nil {
			return pkg.NewError(i18n.T("cat.authentication"), err.Error(), pkg.ExitAuth)
		}

		s := tui.NewStyles()
		fmt.Println()
		fmt.Println(s.Info.Render(i18n.T("auth.oauth.open_browser")))
		fmt.Println(s.Info.Render(authURL))
		fmt.Println()
		if tui.IsInteractive() {
			if err := pkg.OpenBrowser(authURL); err != nil {
				debugf("failed to open the browser: %v", err)
			}
		}

		waitCtx, cancel := context.WithTimeout(ctx, browserWaitTimeout)
		defer cancel()
		res, err := callback.Wait(waitCtx)
		if err != nil {
			return pkg.NewError(i18n.T("cat.authentication"), err.Error(), pkg.ExitAuth)
		}
		if res.Error != "" {
			return pkg.NewError(i18n.T("cat.authentication"),
				i18n.Tf("auth.oauth.error.denied", res.Error), pkg.ExitAuth)
		}
		if res.State != state {
			return pkg.NewError(i18n.T("cat.authentication"), i18n.T("auth.oauth.error.state"), pkg.ExitAuth)
		}
		code = res.Code
	}

	if code == "" {
		return pkg.NewError(i18n.T("cat.authentication"), i18n.T("auth.oauth.error.code"), pkg.ExitAuth)
	}

	token, err := tui.RunWithSpinner(i18n.T("auth.oauth.spinner.exchange"), func() (*auth.TokenResponse, error) {
		return auth.ExchangeAuthorizationCode(ctx, connector.Client, oauth.TokenEndpoint,
			oauth.ClientID, redirectURI, code, pkce.Verifier)
	})
	if err != nil {
		return classifyConnectorError("cat.authentication", "auth.oauth.error.exchange.fix", err)
	}

	if err := auth.StoreOAuthSession(store, token); err != nil {
		return pkg.NewError(i18n.T("cat.authentication"), i18n.Tf("connect.error.store", err.Error()), pkg.ExitAuth)
	}

	printAuthSummary(token)
	return nil
}

func printAuthSummary(token *auth.TokenResponse) {
	s := tui.NewStyles()
	expiry := "—"
	if token.ExpiresIn > 0 {
		expiry = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second).UTC().Format("2006-01-02 15:04:05 UTC")
	}
	tokenType := token.TokenType
	if tokenType == "" {
		tokenType = "Bearer"
	}
	lines := []string{
		s.KeyValue(i18n.T("label.token_expires"), s.Value.Render(expiry)),
		s.KeyValue(i18n.T("auth.oauth.label.token_type"), s.Value.Render(tokenType)),
	}
	if token.Scope != "" {
		lines = append(lines, s.KeyValue(i18n.T("auth.oauth.label.scope"), s.Value.Render(token.Scope)))
	}
	if token.RefreshToken != "" {
		lines = append(lines, s.KeyValue(i18n.T("auth.oauth.label.refresh"), s.Value.Render("✓")))
	}
	fmt.Println()
	fmt.Println(s.SummaryCard(s.Success.Render(i18n.T("auth.oauth.success")), lines...))
	fmt.Println()
}
