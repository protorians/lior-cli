package pkg

import (
	"fmt"
	"os"
	"path/filepath"
)

// DataDir returns the CLI data directory (~/.sentient-cli), creating it with
// user-only permissions when missing.
func DataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve the home directory: %w", err)
	}
	dir := filepath.Join(home, ".sentient-cli")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create directory %s: %w", dir, err)
	}
	return dir, nil
}

// MachineSecret returns the per-user secret used to encrypt the fallback
// stores (credentials.enc / signing.enc). The value is a random 32-byte key
// persisted at ~/.sentient-cli/machine.secret with 0600 permissions; it is
// created on first use. Never a hard-coded passphrase (see R-002).
func MachineSecret() ([]byte, error) {
	dataDir, err := DataDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, "machine.secret")
	if FileExists(path) {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read the machine secret: %w", err)
		}
		if len(data) == 32 {
			return data, nil
		}
	}

	key, err := NewRandomKey()
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, fmt.Errorf("failed to write the machine secret: %w", err)
	}
	return key, nil
}
