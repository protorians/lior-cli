package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/protorians/sentient-cli/internal/pkg"
	"github.com/zalando/go-keyring"
)

// Keychain keys storing credentials.
const (
	ServiceName = "sentient-cli"
	prefix      = "sentient-cli."

	KeyAccessToken = prefix + "access_token"
	KeyMFAToken    = prefix + "mfa_token"
	KeyDevice      = prefix + "device"
	KeyExpiresAt   = prefix + "expires_at"
	KeyUserID      = prefix + "user_id"
	KeyUserEmail   = prefix + "user_email"
	KeyMFASecret   = prefix + "mfa_secret"
	// KeyOAuthRefreshToken stores the OAuth2 refresh token obtained by
	// `sentients auth` (authorization-code + PKCE).
	KeyOAuthRefreshToken = prefix + "oauth_refresh_token"
	// KeyRefreshTokenLegacy was dropped from the session model (single-token
	// sessions). Kept in AllKeys so stale keychain entries are purged on
	// disconnect.
	KeyRefreshTokenLegacy = prefix + "refresh_token"
)

// AllKeys lists every credential key used by the CLI.
var AllKeys = []string{
	KeyAccessToken,
	KeyMFAToken,
	KeyDevice,
	KeyExpiresAt,
	KeyUserID,
	KeyUserEmail,
	KeyMFASecret,
	KeyOAuthRefreshToken,
	KeyRefreshTokenLegacy,
}

// Store is a secure credential storage.
type Store interface {
	// Get returns the value stored under key.
	Get(key string) (string, error)
	// Set stores value under key.
	Set(key string, value string) error
	// Delete removes a single key.
	Delete(kind string) error
	// DeleteAll removes every sentient-cli credential.
	DeleteAll() error
}

// keyringStore uses the operating system keychain via go-keyring.
type keyringStore struct{}

// fallbackStore persists credentials in an AES-256-GCM encrypted file, used
// when no system keychain is available (see risk R-002). The AES key is
// derived (PBKDF2) from the per-user machine secret, never a hard-coded
// passphrase.
type fallbackStore struct {
	path   string
	secret []byte
	mu     sync.Mutex
}

// StoreEnv forces the credential backend in headless/CI runs
// (`SENTIENT_CLI_STORE=file` → encrypted vault, `=keychain` → OS keychain).
// An empty value keeps the automatic platform probe.
const StoreEnv = "SENTIENT_CLI_STORE"

// NewStore returns the appropriate credential store for the platform. The
// system keychain is used when reachable; otherwise the CLI transparently
// falls back to a vault file encrypted with the per-user machine secret.
// `SENTIENT_CLI_STORE` can force either backend for CI/headless runs.
func NewStore() Store {
	switch os.Getenv(StoreEnv) {
	case "file":
		return newFallbackStore(defaultFallbackPath())
	case "keychain":
		return &keyringStore{}
	}
	if keychainAvailable() {
		return &keyringStore{}
	}
	return newFallbackStore(defaultFallbackPath())
}

// NewStoreVolatile returns an isolated fallback store backed by a temporary
// file with a fresh random secret. Used by tests and headless runs that must
// not touch either the system keychain or the user data directory.
func NewStoreVolatile() Store {
	return &fallbackStore{
		path:   filepath.Join(os.TempDir(), "sentient-cli-test-"+pkg.NewUUID()+".enc"),
		secret: mustRandomSecret(),
	}
}

// newFallbackStore builds a fallback store on the given vault path using the
// per-user machine secret.
func newFallbackStore(path string) *fallbackStore {
	secret, err := pkg.MachineSecret()
	if err != nil {
		secret = mustRandomSecret()
	}
	return &fallbackStore{path: path, secret: secret}
}

func defaultFallbackPath() string {
	dir, err := pkg.DataDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "sentient-cli-credentials.enc")
	}
	return filepath.Join(dir, "credentials.enc")
}

func mustRandomSecret() []byte {
	secret, err := pkg.NewRandomKey()
	if err != nil {
		panic(err) // crypto/rand failure: nothing secure can be written in this state
	}
	return secret
}

// keychainAvailable probes the OS keychain with a harmless read of a key that
// cannot exist. A backend that answers ErrNotFound is usable; any other error
// (missing Secret Service, no Credential Manager, headless session…) means the
// CLI must fall back to the encrypted file store.
func keychainAvailable() bool {
	_, err := keyring.Get(ServiceName, "__sentient-cli_probe__")
	return err == nil || isKeyringNotExist(err)
}

func (s *keyringStore) Get(key string) (string, error) {
	v, err := keyring.Get(ServiceName, key)
	if err != nil {
		return "", fmt.Errorf("failed to read credential %q: %w", key, err)
	}
	return v, nil
}

func (s *keyringStore) Set(key, value string) error {
	if value == "" {
		return nil
	}
	if err := keyring.Set(ServiceName, key, value); err != nil {
		return fmt.Errorf("failed to store credential %q: %w", key, err)
	}
	return nil
}

func (s *keyringStore) Delete(kind string) error {
	if err := keyring.Delete(ServiceName, kind); err != nil {
		if isKeyringNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to delete credential %q: %w", kind, err)
	}
	return nil
}

func (s *keyringStore) DeleteAll() error {
	for _, k := range AllKeys {
		if err := s.Delete(k); err != nil {
			return err
		}
	}
	return nil
}

func (s *fallbackStore) Get(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.load()
	if err != nil {
		return "", err
	}
	if v, ok := data[key]; ok {
		return v, nil
	}
	return "", fmt.Errorf("credential %q missing", key)
}

func (s *fallbackStore) Set(key, value string) error {
	if value == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.load()
	if err != nil {
		return err
	}
	data[key] = value
	return s.save(data)
}

func (s *fallbackStore) Delete(kind string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.load()
	if err != nil {
		return err
	}
	delete(data, kind)
	return s.save(data)
}

func (s *fallbackStore) DeleteAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete the credentials file: %w", err)
	}
	return nil
}

func (s *fallbackStore) load() (map[string]string, error) {
	if !pkg.FileExists(s.path) {
		return map[string]string{}, nil
	}
	ciphertext, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("failed to read the credentials file: %w", err)
	}
	plain, err := pkg.DecryptVault(s.secret, ciphertext)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	if err := json.Unmarshal(plain, &out); err != nil {
		return nil, fmt.Errorf("failed to decode the credentials file: %w", err)
	}
	return out, nil
}

func (s *fallbackStore) save(data map[string]string) error {
	plain, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to serialize credentials: %w", err)
	}
	ciphertext, err := pkg.EncryptVault(s.secret, plain)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("failed to create the credentials directory: %w", err)
	}
	return os.WriteFile(s.path, ciphertext, 0o600)
}

func isKeyringNotExist(err error) bool {
	// go-keyring returns ErrNotFound for missing entries.
	return err == keyring.ErrNotFound
}
