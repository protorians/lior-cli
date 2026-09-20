package signing

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/zalando/go-keyring"
)

// Keychain keys for signing key storage.
const (
	signingServiceName = "lorian-cli-signing"
	keyPublicKey       = "signing_public_key"
	keyPrivateKey      = "signing_private_key"
)

// KeyStore is a secure storage for Ed25519 signing keys.
type KeyStore interface {
	GetPublicKey() ([]byte, error)
	SetPublicKey(data []byte) error
	GetPrivateKey() ([]byte, error)
	SetPrivateKey(data []byte) error
	DeleteKeys() error
	HasKeys() bool
}

// keyringKeyStore uses the OS keychain via go-keyring.
type keyringKeyStore struct{}

// fallbackKeyStore persists keys in an AES-256-GCM encrypted file. The AES key
// is derived (PBKDF2) from the per-user machine secret (never hard-coded).
type fallbackKeyStore struct {
	path   string
	secret []byte
	mu     sync.Mutex
}

// NewKeyStore returns the appropriate key store for the platform. The system
// keychain is used when reachable; otherwise the CLI transparently falls back
// to an encrypted vault file (spec §7.6, risk R-002). `LIORIAN_CLI_STORE` can
// force either backend for CI/headless runs (see auth.StoreEnv).
func NewKeyStore() KeyStore {
	switch os.Getenv("LIORIAN_CLI_STORE") {
	case "file":
		return newFallbackKeyStore(defaultKeyStorePath())
	case "keychain":
		return &keyringKeyStore{}
	}
	if keychainAvailable() {
		return &keyringKeyStore{}
	}
	return newFallbackKeyStore(defaultKeyStorePath())
}

// newFallbackKeyStore builds a fallback key store on the given vault path
// using the per-user machine secret.
func newFallbackKeyStore(path string) *fallbackKeyStore {
	secret, err := pkg.MachineSecret()
	if err != nil {
		secret = newRandomSecret()
	}
	return &fallbackKeyStore{path: path, secret: secret}
}

func defaultKeyStorePath() string {
	dir, err := pkg.DataDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "lorian-cli-signing.enc")
	}
	return filepath.Join(dir, "signing.enc")
}

// keychainAvailable probes the OS keychain with a harmful read of a key that
// cannot exist (see auth.credentials.keychainAvailable).
func keychainAvailable() bool {
	_, err := keyring.Get(signingServiceName, "__lorian-cli_probe__")
	return err == nil || isKeyringNotExist(err)
}

func newRandomSecret() []byte {
	secret, err := pkg.NewRandomKey()
	if err != nil {
		panic(err) // crypto/rand failure: nothing secure can be written in this state
	}
	return secret
}

// --- keyringKeyStore ---

func (s *keyringKeyStore) GetPublicKey() ([]byte, error) {
	v, err := keyring.Get(signingServiceName, keyPublicKey)
	if err != nil {
		return nil, fmt.Errorf("public key not in the keychain: %w", err)
	}
	return []byte(v), nil
}

func (s *keyringKeyStore) SetPublicKey(data []byte) error {
	return keyring.Set(signingServiceName, keyPublicKey, string(data))
}

func (s *keyringKeyStore) GetPrivateKey() ([]byte, error) {
	v, err := keyring.Get(signingServiceName, keyPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("private key not in the keychain: %w", err)
	}
	return []byte(v), nil
}

func (s *keyringKeyStore) SetPrivateKey(data []byte) error {
	return keyring.Set(signingServiceName, keyPrivateKey, string(data))
}

func (s *keyringKeyStore) DeleteKeys() error {
	if err := keyring.Delete(signingServiceName, keyPublicKey); err != nil && !isKeyringNotExist(err) {
		return err
	}
	if err := keyring.Delete(signingServiceName, keyPrivateKey); err != nil && !isKeyringNotExist(err) {
		return err
	}
	return nil
}

func (s *keyringKeyStore) HasKeys() bool {
	_, err := keyring.Get(signingServiceName, keyPublicKey)
	return err == nil
}

// --- fallbackKeyStore ---

func (s *fallbackKeyStore) GetPublicKey() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil {
		return nil, err
	}
	v, ok := data[keyPublicKey]
	if !ok {
		return nil, fmt.Errorf("public key missing")
	}
	return decodeValue(v)
}

func (s *fallbackKeyStore) SetPublicKey(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.load()
	if err != nil {
		return err
	}
	d[keyPublicKey] = encodeValue(data)
	return s.save(d)
}

func (s *fallbackKeyStore) GetPrivateKey() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil {
		return nil, err
	}
	v, ok := data[keyPrivateKey]
	if !ok {
		return nil, fmt.Errorf("private key missing")
	}
	return decodeValue(v)
}

func (s *fallbackKeyStore) SetPrivateKey(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.load()
	if err != nil {
		return err
	}
	d[keyPrivateKey] = encodeValue(data)
	return s.save(d)
}

func (s *fallbackKeyStore) DeleteKeys() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete the key file: %w", err)
	}
	return nil
}

func (s *fallbackKeyStore) HasKeys() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil {
		return false
	}
	_, ok := data[keyPublicKey]
	return ok
}

func (s *fallbackKeyStore) load() (map[string]string, error) {
	if !pkg.FileExists(s.path) {
		return map[string]string{}, nil
	}
	ciphertext, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("failed to read the key file: %w", err)
	}
	plain, err := pkg.DecryptVault(s.secret, ciphertext)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	if err := json.Unmarshal(plain, &out); err != nil {
		return nil, fmt.Errorf("failed to decode the key file: %w", err)
	}
	return out, nil
}

func (s *fallbackKeyStore) save(data map[string]string) error {
	plain, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to serialize keys: %w", err)
	}
	ciphertext, err := pkg.EncryptVault(s.secret, plain)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("failed to create the key directory: %w", err)
	}
	return os.WriteFile(s.path, ciphertext, 0o600)
}

// encodeValue base64-encodes binary key material so it survives a JSON round
// trip (JSON would otherwise corrupt non-UTF-8 bytes).
func encodeValue(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// decodeValue base64-decodes a value previously stored via encodeValue.
func decodeValue(v string) ([]byte, error) {
	out, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return nil, fmt.Errorf("failed to decode the stored key: %w", err)
	}
	return out, nil
}

func isKeyringNotExist(err error) bool {
	return err == keyring.ErrNotFound
}
