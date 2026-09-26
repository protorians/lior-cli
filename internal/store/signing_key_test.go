package store

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/protorians/lior-cli/internal/auth"
	"github.com/protorians/lior-cli/internal/module"
	"github.com/protorians/lior-cli/internal/pkg"
)


// TestCatalogIdentifier pins the identifier form the server recomputes
// (`catalogIdentifier` in api-resources) to verify the artefact signature.
func TestCatalogIdentifier(t *testing.T) {
	cases := []struct {
		publisher, module, want string
	}{
		{"acme", "crm", "mod.acme.crm"},
		{"", "crm", "mod.developer.crm"},
		{"Acme Corp!", "Mon Module", "mod.acme-corp.mon-module"},
		{"café", "crème", "mod.cafe.creme"},
	}
	for _, c := range cases {
		if got := CatalogIdentifier(c.publisher, c.module); got != c.want {
			t.Errorf("CatalogIdentifier(%q, %q) = %q, want %q", c.publisher, c.module, got, c.want)
		}
	}
}

func TestProductSlug(t *testing.T) {
	if got := ProductSlug(&module.Manifest{ID: "acme-crm"}); got != "acme-crm" {
		t.Errorf("ProductSlug = %q, want %q", got, "acme-crm")
	}
	if got := ProductSlug(&module.Manifest{Key: "acme_crm"}); got != "acme-crm" {
		t.Errorf("ProductSlug fallback = %q, want %q", got, "acme-crm")
	}
}

// TestEnsureSigningKeyReusesRegisteredKey checks the idempotent path: an
// ACTIVE key carrying the exact public key is reused, nothing is created.
func TestEnsureSigningKeyReusesRegisteredKey(t *testing.T) {
	created := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/signing-keys"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"message": "ok",
				"data": []map[string]any{{
					"keyId": "sign_abc", "algorithm": "Ed25519", "status": "ACTIVE",
					"publicKey": "-----BEGIN PUBLIC KEY-----\nKEY\n-----END PUBLIC KEY-----\n",
				}},
			})
		default:
			created++
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := &Client{Connector: &auth.Connector{Client: pkg.NewClient(server.URL)}}
	key, createdFlag, err := client.EnsureSigningKey(context.Background(), "-----BEGIN PUBLIC KEY-----\nKEY\n-----END PUBLIC KEY-----\n")
	if err != nil {
		t.Fatalf("EnsureSigningKey: %v", err)
	}
	if createdFlag {
		t.Error("an already-registered key must be reused, not re-created")
	}
	if key.KeyID != "sign_abc" {
		t.Errorf("unexpected key id: %q", key.KeyID)
	}
	if created != 0 {
		t.Error("no creation request must be issued when the key is already registered")
	}
}

// TestEnsureSigningKeyCreatesWhenMissing checks the registration path: without
// a matching ACTIVE key the local public key is registered (Ed25519).
func TestEnsureSigningKeyCreatesWhenMissing(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/signing-keys"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"message": "ok",
				"data": []map[string]any{{
					"keyId": "sign_old", "algorithm": "Ed25519", "status": "ROTATED",
					"publicKey": "-----BEGIN PUBLIC KEY-----\nOTHER\n-----END PUBLIC KEY-----\n",
				}},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/signing-keys"):
			_ = json.NewDecoder(r.Body).Decode(&body)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"message": "created",
				"data":    map[string]any{"keyId": "sign_new", "algorithm": "Ed25519", "status": "ACTIVE"},
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := &Client{Connector: &auth.Connector{Client: pkg.NewClient(server.URL)}}
	key, createdFlag, err := client.EnsureSigningKey(context.Background(), "-----BEGIN PUBLIC KEY-----\nKEY\n-----END PUBLIC KEY-----\n")
	if err != nil {
		t.Fatalf("EnsureSigningKey: %v", err)
	}
	if !createdFlag {
		t.Error("a missing key must be registered")
	}
	if key.KeyID != "sign_new" {
		t.Errorf("unexpected key id: %q", key.KeyID)
	}
	if body["publicKey"] != "-----BEGIN PUBLIC KEY-----\nKEY\n-----END PUBLIC KEY-----\n" {
		t.Errorf("the public key must be forwarded PEM/SPKI, got: %v", body["publicKey"])
	}
	if body["algorithm"] != "Ed25519" {
		t.Errorf("unexpected algorithm: %v", body["algorithm"])
	}
}
