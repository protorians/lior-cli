package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/protorians/lior-cli/internal/pkg"
)

// PKCE method (RFC 7636).
const pkceMethodS256 = "S256"

// OAuth grant types (RFC 6749).
const (
	OAuthGrantTypeAuthorizationCode = "authorization_code"
	OAuthGrantTypeRefreshToken      = "refresh_token"
)

// PKCE holds a code verifier and its S256 code challenge.
type PKCE struct {
	Verifier  string
	Challenge string
}

// GeneratePKCE builds a PKCE code verifier (43 base64url chars) and its S256
// challenge (RFC 7636).
func GeneratePKCE() (*PKCE, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return nil, fmt.Errorf("failed to generate the PKCE verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	return &PKCE{
		Verifier:  verifier,
		Challenge: base64.RawURLEncoding.EncodeToString(sum[:]),
	}, nil
}

// RandomState generates a random, opaque state value (CSRF protection).
func RandomState() (string, error) {
	raw := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", fmt.Errorf("failed to generate the state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// AuthorizationURL builds the browser URL for the OAuth2 authorization request
// (response_type=code, PKCE S256, state). The endpoint is a relative path
// joined to the API base URL.
func AuthorizationURL(baseURL, endpoint, clientID, redirectURI, scope, challenge, state string) (string, error) {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", pkceMethodS256)
	q.Set("state", state)
	if scope != "" {
		q.Set("scope", scope)
	}
	return strings.TrimRight(baseURL, "/") + endpoint + "?" + q.Encode(), nil
}

// TokenResponse is the standard OAuth2 token response (RFC 6749 §5.1).
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
}

// ExchangeAuthorizationCode exchanges an authorization code for tokens at the
// OAuth2 token endpoint (grant_type=authorization_code, PKCE verifier).
func ExchangeAuthorizationCode(ctx context.Context, client *pkg.Client, endpoint, clientID, redirectURI, code, verifier string) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", OAuthGrantTypeAuthorizationCode)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", clientID)
	form.Set("code_verifier", verifier)
	return exchange(ctx, client, endpoint, form)
}

// ExchangeRefreshToken refreshes an access token at the OAuth2 token endpoint
// (grant_type=refresh_token).
func ExchangeRefreshToken(ctx context.Context, client *pkg.Client, endpoint, clientID, refreshToken string) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", OAuthGrantTypeRefreshToken)
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", clientID)
	return exchange(ctx, client, endpoint, form)
}

// exchange performs a form-encoded OAuth2 token request against the endpoint
// and decodes the standard token response. The Raiton envelope is tolerated
// for the (non-standard) case where the server wraps the token in `data`.
func exchange(ctx context.Context, client *pkg.Client, endpoint string, form url.Values) (*TokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(client.BaseURL, "/")+endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to build the token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", pkg.UserAgent())

	resp, err := client.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read the token response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, oauthError(resp.StatusCode, data)
	}

	var tok TokenResponse
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, fmt.Errorf("failed to decode the token response: %w", err)
	}
	if tok.AccessToken == "" {
		// Tolerate the Raiton envelope `{message, data, statusCode}` wrapping the
		// standard OAuth token object in `data`.
		var envelope pkg.RaitonResponse
		if json.Unmarshal(data, &envelope) == nil && len(envelope.Data) > 0 {
			if err := json.Unmarshal(envelope.Data, &tok); err != nil {
				return nil, fmt.Errorf("failed to decode the token response: %w", err)
			}
		}
	}
	if tok.AccessToken == "" {
		return nil, errors.New("token response missing access_token")
	}
	return &tok, nil
}

// oauthError builds a categorized error from a non-2xx token response. It
// tolerates the Raiton envelope, the standard OAuth `{error, error_description}`
// shape, and arbitrary bodies.
func oauthError(status int, data []byte) error {
	var envelope pkg.RaitonResponse
	if json.Unmarshal(data, &envelope) == nil && envelope.Message != "" {
		return &pkg.APIError{StatusCode: status, Message: envelope.Message}
	}
	var oauthErr struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if json.Unmarshal(data, &oauthErr) == nil && oauthErr.Error != "" {
		msg := oauthErr.Error
		if oauthErr.ErrorDescription != "" {
			msg = oauthErr.Error + ": " + oauthErr.ErrorDescription
		}
		return &pkg.APIError{StatusCode: status, Message: msg}
	}
	if msg := strings.TrimSpace(string(data)); msg != "" {
		return &pkg.APIError{StatusCode: status, Message: msg}
	}
	return &pkg.APIError{StatusCode: status, Message: http.StatusText(status)}
}

// StoreOAuthSession persists an OAuth2 token response into the credential
// store as an authenticated session (access token + expiry + refresh token).
func StoreOAuthSession(store Store, tok *TokenResponse) error {
	sess := &Session{Store: store, AccessToken: tok.AccessToken}
	expiry := TokenTTL
	if tok.ExpiresIn > 0 {
		expiry = time.Duration(tok.ExpiresIn) * time.Second
	}
	t := time.Now().Add(expiry)
	sess.ExpiresAt = &t
	if err := sess.Save(); err != nil {
		return err
	}
	if tok.RefreshToken != "" {
		return store.Set(KeyOAuthRefreshToken, tok.RefreshToken)
	}
	return nil
}

// CallbackResult is the outcome of the OAuth2 authorization redirect received
// by the local loopback server.
type CallbackResult struct {
	Code  string
	State string
	Error string
}

// CallbackServer is a loopback HTTP server (127.0.0.1) that receives the OAuth2
// authorization redirect.
type CallbackServer struct {
	listener net.Listener
	server   *http.Server
	ch       chan CallbackResult
}

// StartLoopbackCallback starts a loopback server on 127.0.0.1:0 and returns the
// server together with its redirect URI (`http://127.0.0.1:<port><path>`).
func StartLoopbackCallback(path string) (*CallbackServer, string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", fmt.Errorf("failed to start the local callback server: %w", err)
	}
	cs := &CallbackServer{ch: make(chan CallbackResult, 1)}
	mux := http.NewServeMux()
	mux.HandleFunc(path, cs.handle)
	cs.server = &http.Server{Handler: mux}
	cs.listener = ln
	go func() { _ = cs.server.Serve(ln) }()

	port := ln.Addr().(*net.TCPAddr).Port
	redirect := fmt.Sprintf("http://127.0.0.1:%d%s", port, path)
	return cs, redirect, nil
}

func (cs *CallbackServer) handle(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	res := CallbackResult{Code: q.Get("code"), State: q.Get("state")}
	if res.Code == "" {
		if res.Error = q.Get("error"); res.Error == "" {
			res.Error = q.Get("error_description")
		}
	}
	select {
	case cs.ch <- res:
	default:
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, "<html><body><h3>Authorization complete. You can close this window.</h3></body></html>")
}

// Wait blocks until the redirect is received or the context is done.
func (cs *CallbackServer) Wait(ctx context.Context) (CallbackResult, error) {
	select {
	case res := <-cs.ch:
		return res, nil
	case <-ctx.Done():
		return CallbackResult{}, ctx.Err()
	}
}

// Close shuts the callback server down.
func (cs *CallbackServer) Close() error {
	if cs.listener != nil {
		return cs.listener.Close()
	}
	return nil
}
