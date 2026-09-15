package auth

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/protorians/sentient-cli/internal/i18n"
)

// TokenTTL is the client-side lifetime of a session token. The API responses
// carry no `expires_in`, so the CLI estimates the JWT expiry (matching the
// backend's 24h access token TTL).
const TokenTTL = 24 * time.Hour

// Session represents a stored authenticated session.
type Session struct {
	Store       Store
	AccessToken string
	MFAToken    string
	Device      string
	ExpiresAt   *time.Time
	User        *User
}

// Combined auth+session flow for interactive connectors keeps the two terms
// distinct: sign-in performs the API exchange; the session then persists it.

// LoadSession reads credentials from the store, if present.
func LoadSession(store Store) (*Session, error) {
	s := &Session{Store: store}

	access, err := store.Get(KeyAccessToken)
	if err != nil {
		return nil, nil // not authenticated
	}
	s.AccessToken = access

	s.MFAToken, _ = store.Get(KeyMFAToken)
	s.Device, _ = store.Get(KeyDevice)

	if raw, err := store.Get(KeyExpiresAt); err == nil {
		if ts, perr := strconv.ParseInt(raw, 10, 64); perr == nil {
			t := time.Unix(ts, 0)
			s.ExpiresAt = &t
		}
	}

	email, _ := store.Get(KeyUserEmail)
	id, _ := store.Get(KeyUserID)
	s.User = &User{Email: email, ID: id}

	return s, nil
}

// IsAuthenticated reports whether an access token is present.
func (s *Session) IsAuthenticated() bool {
	return s != nil && s.AccessToken != ""
}

// IsExpired reports whether the access token has passed its expiry.
func (s *Session) IsExpired() bool {
	return s != nil && s.ExpiresAt != nil && time.Now().After(*s.ExpiresAt)
}

// Save persists the tokens and user info into the store.
func (s *Session) Save() error {
	if s.User == nil {
		s.User = &User{}
	}
	calls := []struct {
		key   string
		value string
	}{
		{KeyAccessToken, s.AccessToken},
		{KeyMFAToken, s.MFAToken},
		{KeyDevice, s.Device},
		{KeyUserEmail, s.User.Email},
		{KeyUserID, s.User.ID},
	}
	if s.ExpiresAt != nil {
		calls = append(calls, struct {
			key   string
			value string
		}{KeyExpiresAt, strconv.FormatInt(s.ExpiresAt.Unix(), 10)})
	}
	for _, c := range calls {
		if err := s.Store.Set(c.key, c.value); err != nil {
			return err
		}
	}
	return nil
}

// Refresh rotates the current session token via POST /api/auth/sessions/refresh,
// using the current token as the bearer credential.
func (s *Session) Refresh(ctx context.Context, connector *Connector) error {
	if !s.IsAuthenticated() {
		return fmt.Errorf("%s", i18n.T("auth.error.no_token"))
	}
	connector.Client.Token = s.AccessToken
	resp, err := connector.RefreshSession(ctx, s.Device)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("auth.error.refresh"), err)
	}
	s.AccessToken = resp.Token
	t := time.Now().Add(TokenTTL)
	s.ExpiresAt = &t
	return s.Save()
}

// Clear removes every stored credential.
func (s *Session) Clear() error {
	return s.Store.DeleteAll()
}

// ValidToken returns a non-expired access token, refreshing when needed.
func (s *Session) ValidToken(ctx context.Context, connector *Connector) (string, error) {
	if !s.IsAuthenticated() {
		return "", fmt.Errorf("%s", i18n.T("auth.error.not_authenticated"))
	}
	if s.IsExpired() {
		if err := s.Refresh(ctx, connector); err != nil {
			return "", err
		}
	}
	return s.AccessToken, nil
}
