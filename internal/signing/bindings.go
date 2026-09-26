package signing

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/protorians/lior-cli/internal/pkg"
)

// Binding associates a module domain (`mod.<publisher>.<module>`) with the
// signing key that published it. The association is local: the server keeps
// its own account-level registry (`DeveloperSigningKey`), the binding tells
// the CLI which of the developer's keys signs a given module.
type Binding struct {
	Fingerprint string    `json:"fingerprint"`
	KeyID       string    `json:"keyId,omitempty"`
	BoundAt     time.Time `json:"boundAt"`
}

// bindings is the on-disk shape of the binding registry: a plain map keyed by
// module domain. It lives in ~/.lorian-cli/signing-bindings.json — not in the
// keychain, because it holds no secret (fingerprints and key ids only).
type bindings map[string]Binding

func bindingsPath() (string, error) {
	dir, err := pkg.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "signing-bindings.json"), nil
}

func loadBindings() (bindings, error) {
	path, err := bindingsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return bindings{}, nil
		}
		return nil, fmt.Errorf("failed to read the signing bindings: %w", err)
	}
	var out bindings
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("failed to parse the signing bindings: %w", err)
	}
	if out == nil {
		out = bindings{}
	}
	return out, nil
}

func saveBindings(b bindings) error {
	path, err := bindingsPath()
	if err != nil {
		return err
	}
	entries := make([]string, 0, len(b))
	for domain := range b {
		entries = append(entries, domain)
	}
	sort.Strings(entries)
	ordered := make(map[string]Binding, len(b))
	for _, domain := range entries {
		ordered[domain] = b[domain]
	}
	data, err := json.MarshalIndent(ordered, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode the signing bindings: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write the signing bindings: %w", err)
	}
	return nil
}

// LookupBinding returns the key bound to a module domain, if any.
func LookupBinding(domain string) (*Binding, error) {
	b, err := loadBindings()
	if err != nil {
		return nil, err
	}
	if binding, ok := b[domain]; ok {
		return &binding, nil
	}
	return nil, nil
}

// BindBinding associates a module domain with a signing key (fingerprint +
// store key id), overwriting any previous binding for the domain.
func BindBinding(domain, fingerprint, keyID string) error {
	b, err := loadBindings()
	if err != nil {
		return err
	}
	b[domain] = Binding{Fingerprint: fingerprint, KeyID: keyID, BoundAt: time.Now().UTC()}
	return saveBindings(b)
}

// RemoveBinding drops the binding of a module domain.
func RemoveBinding(domain string) error {
	b, err := loadBindings()
	if err != nil {
		return err
	}
	delete(b, domain)
	return saveBindings(b)
}

// ListBindings returns all domain→key bindings sorted by domain.
func ListBindings() (map[string]Binding, error) {
	b, err := loadBindings()
	if err != nil {
		return nil, err
	}
	return map[string]Binding(b), nil
}
