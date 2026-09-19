package auth

import (
	"context"
	"os"
	"time"

	"github.com/protorians/lior-cli/internal/appconfig"
	"github.com/protorians/lior-cli/internal/pkg"
)

// EnvAPIBase overrides the resolved API base URL.
const EnvAPIBase = "LIORIAN_AUTH_API"

// API paths (global `/api` prefix, Raiton envelope responses).
const (
	APISignInPage     = "/api/auth/sign-in"
	APILogoutPage     = "/api/auth/logout"
	APIRefreshPage    = "/api/auth/sessions/refresh"
	APIChallengePage  = "/api/mfa/challenge"
	APITOTPVerify     = "/api/mfa/totp/verify"
	APIRecoveryVerify = "/api/mfa/recovery/verify"
)

// Role is a role carried by the authenticated user (UserVm.roles).
type Role struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// User is the authenticated developer (UserVm).
type User struct {
	ID       string `json:"id"`
	Email    string `json:"email,omitempty"`
	Username string `json:"username,omitempty"`
	Avatar   string `json:"avatar,omitempty"`
	Status   string `json:"status,omitempty"`
	Roles    []Role `json:"roles,omitempty"`
	// Name and Role are legacy/derived fields kept for backward-compatible
	// display: they fall back to Username and the first role name.
	Name string `json:"name,omitempty"`
	Role string `json:"role,omitempty"`
}

// normalized returns a copy of the user with `Name`/`Role` filled from the
// modern `username`/`roles` fields when the legacy fields are empty.
func (u User) normalized() User {
	if u.Name == "" {
		u.Name = u.Username
	}
	if u.Role == "" && len(u.Roles) > 0 {
		u.Role = u.Roles[0].Name
	}
	return u
}

// SignInRequest is the payload for POST /api/auth/sign-in.
type SignInRequest struct {
	Email    string `json:"email,omitempty"`
	Password string `json:"password"`
	OTP      string `json:"otp,omitempty"`
}

// SignInResponse is the `data` returned by POST /api/auth/sign-in
// (SignInVm: `{user, token, device}`).
type SignInResponse struct {
	User          User           `json:"user"`
	Token         string         `json:"token"`
	Device        string         `json:"device"`
	Organizations []Organization `json:"organizations,omitempty"`
}

// Organization is a compact organization entry exposed by sign-in.
type Organization struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// RefreshSessionRequest is the payload for POST /api/auth/sessions/refresh.
type RefreshSessionRequest struct {
	ClientID string `json:"clientId,omitempty"`
}

// RefreshSessionResponse is the `data` returned by POST /api/auth/sessions/refresh.
type RefreshSessionResponse struct {
	Token string `json:"token"`
}

// LogoutRequest is the payload for POST /api/auth/logout.
type LogoutRequest struct {
	ClientID string `json:"clientId,omitempty"`
}

// MFAFactor is a factor exposed by POST /api/mfa/challenge (MfaFactorVm).
type MFAFactor struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Label      string `json:"label"`
	Enabled    bool   `json:"enabled"`
	VerifiedAt string `json:"verifiedAt,omitempty"`
}

// ChallengeResponse is the `data` returned by POST /api/mfa/challenge
// (MfaChallengeVm: `{mfaRequired, challenge?, factors}`).
type ChallengeResponse struct {
	MFARequired bool        `json:"mfaRequired"`
	Challenge   string      `json:"challenge,omitempty"`
	Factors     []MFAFactor `json:"factors"`
}

// VerifyRequest is the payload for MFA verification endpoints.
type VerifyRequest struct {
	Code string `json:"code"`
}

// VerifyResponse is the `data` returned by POST /api/mfa/totp/verify and
// POST /api/mfa/recovery/verify (MfaVerifyVm: `{mfaVerified, mfaToken?}`).
type VerifyResponse struct {
	MFAVerified bool   `json:"mfaVerified"`
	MFAToken    string `json:"mfaToken,omitempty"`
}

// Connector talks to the liorian-auth API.
type Connector struct {
	Client *pkg.Client
}

// apiBaseURL resolves the liorian-auth base URL. Resolution order:
//  1. `LIORIAN_AUTH_API` environment variable,
//  2. workspace `app.config.json` (walked up from the current directory),
//  3. the `app.config.json` registry embedded in the binary.
//
// The URL always comes from the API-side configuration of `liorian-auth`
// (the `api.baseUrl` entry of `app.config.json`) — never a hardcoded domain.
func apiBaseURL() string {
	if v := os.Getenv(EnvAPIBase); v != "" {
		return v
	}
	base, _ := appconfig.Resolved("").BaseURL(appconfig.AuthAppID)
	return base
}

// apiTimeout resolves the liorian-auth API timeout from `app.config.json`
// (`api.timeout`, milliseconds), falling back to the CLI default.
func apiTimeout() time.Duration {
	return appconfig.Resolved("").Timeout(appconfig.AuthAppID)
}

// DebugInfo returns the resolved API base URL and the configuration source
// used to resolve it (env override, workspace file or embedded registry).
func DebugInfo() (baseURL, source string) {
	source = "none"
	if v := os.Getenv(EnvAPIBase); v != "" {
		return v, "env:" + EnvAPIBase
	}
	cfg := appconfig.Resolved("")
	base, ok := cfg.BaseURL(appconfig.AuthAppID)
	if ok {
		source = cfg.Source()
	}
	return base, source
}

// NewConnector builds a connector against the resolved API base URL.
func NewConnector() *Connector {
	return &Connector{
		Client: pkg.NewClientWithTimeout(apiBaseURL(), apiTimeout()),
	}
}

// SignIn authenticates with email + password and returns the single session
// token (SignInVm `{user, token, device}`).
func (c *Connector) SignIn(ctx context.Context, req SignInRequest) (*SignInResponse, error) {
	var out SignInResponse
	if err := c.Client.Do(ctx, "POST", APISignInPage, req, &out); err != nil {
		return nil, err
	}
	out.User = out.User.normalized()
	return &out, nil
}

// SignOut invalidates the current token server-side.
func (c *Connector) SignOut(ctx context.Context, clientID string) error {
	return c.Client.Do(ctx, "POST", APILogoutPage, LogoutRequest{ClientID: clientID}, nil)
}

// RefreshSession rotates the current session token via POST /api/auth/sessions/refresh.
func (c *Connector) RefreshSession(ctx context.Context, clientID string) (*RefreshSessionResponse, error) {
	var out RefreshSessionResponse
	if err := c.Client.Do(ctx, "POST", APIRefreshPage, RefreshSessionRequest{ClientID: clientID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ChallengeMFA requests the available MFA factors for the current session.
func (c *Connector) ChallengeMFA(ctx context.Context) (*ChallengeResponse, error) {
	var out ChallengeResponse
	if err := c.Client.Do(ctx, "POST", APIChallengePage, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// VerifyTOTP verifies a 6-digit TOTP code.
func (c *Connector) VerifyTOTP(ctx context.Context, code string) (*VerifyResponse, error) {
	var out VerifyResponse
	if err := c.Client.Do(ctx, "POST", APITOTPVerify, VerifyRequest{Code: code}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// VerifyRecovery verifies a backup/recovery code.
func (c *Connector) VerifyRecovery(ctx context.Context, code string) (*VerifyResponse, error) {
	var out VerifyResponse
	if err := c.Client.Do(ctx, "POST", APIRecoveryVerify, VerifyRequest{Code: code}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
