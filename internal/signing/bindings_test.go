package signing

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBindingCRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	// pkg.DataDir() resolves from os.UserHomeDir(); force the data dir under
	// the temp home for the duration of the test.

	if b, err := LookupBinding("mod.acme.crm"); err != nil || b != nil {
		t.Fatalf("expected no binding, got %v, err %v", b, err)
	}

	if err := BindBinding("mod.acme.crm", "fp-1", "sign_1"); err != nil {
		t.Fatalf("BindBinding: %v", err)
	}
	b, err := LookupBinding("mod.acme.crm")
	if err != nil || b == nil || b.Fingerprint != "fp-1" || b.KeyID != "sign_1" {
		t.Fatalf("LookupBinding after bind: %v, %v", b, err)
	}

	// Overwrite: the domain carries the newest key.
	if err := BindBinding("mod.acme.crm", "fp-2", "sign_2"); err != nil {
		t.Fatalf("rebind: %v", err)
	}
	b, _ = LookupBinding("mod.acme.crm")
	if b.Fingerprint != "fp-2" {
		t.Fatalf("expected rotated fingerprint, got %s", b.Fingerprint)
	}

	all, err := ListBindings()
	if err != nil || len(all) != 1 {
		t.Fatalf("ListBindings: %v, %v", all, err)
	}

	if err := RemoveBinding("mod.acme.crm"); err != nil {
		t.Fatalf("RemoveBinding: %v", err)
	}
	if b, _ := LookupBinding("mod.acme.crm"); b != nil {
		t.Fatalf("expected binding removed")
	}
}

func TestBindingsFilePermissions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if err := BindBinding("mod.acme.crm", "fp", ""); err != nil {
		t.Fatalf("BindBinding: %v", err)
	}
	path := filepath.Join(dir, ".lorian-cli", "signing-bindings.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("registry file missing: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected 0600 permissions, got %o", perm)
	}
}
