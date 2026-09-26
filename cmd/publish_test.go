package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/protorians/lior-cli/internal/auth"
	"github.com/protorians/lior-cli/internal/module"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/store"
)

// storeTestClient builds a Developer Store client bound to a test server, with
// the token and the auto-refresh wiring left to the caller.
func storeTestClient(serverURL string) *store.Client {
	return &store.Client{Connector: &auth.Connector{Client: pkg.NewClient(serverURL)}}
}

func TestResolveDeveloperIdentityFromStore(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/developer-store/accounts/me" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"ok","statusCode":200,"data":{"id":"acc-42","slug":"protorians","name":"Protorians"}}`))
	}))
	defer server.Close()

	manifest := &module.Manifest{}
	sess := &auth.Session{User: &auth.User{ID: "session-user"}}

	if updated := resolveDeveloperIdentity(t.Context(), storeTestClient(server.URL), sess, manifest); !updated {
		t.Error("le manifeste doit être signalé comme modifié")
	}
	if manifest.Publisher.ID != "acc-42" {
		t.Errorf("publisher.id = %q, want acc-42 (identifiant de compte Liorian)", manifest.Publisher.ID)
	}
	if manifest.Publisher.Name != "Protorians" {
		t.Errorf("publisher.name = %q, want Protorians", manifest.Publisher.Name)
	}
}

func TestResolveDeveloperIdentityFallsBackToSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	manifest := &module.Manifest{}
	sess := &auth.Session{User: &auth.User{ID: "session-user"}}

	if updated := resolveDeveloperIdentity(t.Context(), storeTestClient(server.URL), sess, manifest); !updated {
		t.Error("le manifeste doit être signalé comme modifié")
	}
	if manifest.Publisher.ID != "session-user" {
		t.Errorf("publisher.id = %q, want session-user (repli session)", manifest.Publisher.ID)
	}
	if manifest.Publisher.Name != "" {
		t.Errorf("publisher.name = %q, want vide (le nom reste à saisir)", manifest.Publisher.Name)
	}
}

func TestResolveDeveloperIdentityKeepsManifestValues(t *testing.T) {
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	manifest := &module.Manifest{Publisher: module.Publisher{ID: "kept-id", Name: "Kept Name"}}
	sess := &auth.Session{User: &auth.User{ID: "session-user"}}

	if updated := resolveDeveloperIdentity(t.Context(), storeTestClient(server.URL), sess, manifest); updated {
		t.Error("un manifeste déjà complet ne doit pas être modifié")
	}
	if called {
		t.Error("le compte ne doit pas être interrogé quand le manifeste est déjà complet")
	}
	if manifest.Publisher.ID != "kept-id" || manifest.Publisher.Name != "Kept Name" {
		t.Errorf("publisher écrasé: %+v", manifest.Publisher)
	}
}

func TestPublisherLabel(t *testing.T) {
	cases := []struct {
		name      string
		publisher module.Publisher
		want      string
	}{
		{"nom et id", module.Publisher{ID: "acc-42", Name: "Protorians"}, "Protorians (acc-42)"},
		{"nom seul", module.Publisher{Name: "Protorians"}, "Protorians"},
		{"id seul", module.Publisher{ID: "acc-42"}, "acc-42"},
		{"vide", module.Publisher{}, ""},
	}
	for _, tc := range cases {
		m := &module.Manifest{Publisher: tc.publisher}
		if got := publisherLabel(m); got != tc.want {
			t.Errorf("%s: publisherLabel = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestIsVersionConflict(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"409 conflict", &pkg.APIError{StatusCode: http.StatusConflict, Message: "version already published"}, true},
		{"message version already exists", &pkg.APIError{StatusCode: http.StatusBadRequest, Message: "version 0.1.0 already exists"}, true},
		{"message version conflict", &pkg.APIError{StatusCode: http.StatusBadRequest, Message: "version conflict detected"}, true},
		{"message without version", &pkg.APIError{StatusCode: http.StatusBadRequest, Message: "invalid manifest"}, false},
		{"non API error", pkg.NewError("Publication", "boom", pkg.ExitPublish), false},
		{"generic error", &pkg.APIError{StatusCode: http.StatusBadRequest, Message: "bad request"}, false},
	}
	for _, tc := range cases {
		if got := isVersionConflict(tc.err); got != tc.want {
			t.Errorf("%s: isVersionConflict(%v) = %v, want %v", tc.name, tc.err, got, tc.want)
		}
	}
}

func TestPublishCommandFlagsRegistered(t *testing.T) {
	for _, name := range []string{"file", "version", "allow-unsigned"} {
		if f := publishCmd.Flag(name); f == nil {
			t.Errorf("publishCmd must expose the --%s flag (release session flow)", name)
		}
	}
}

func TestPackCommandFlagsRegistered(t *testing.T) {
	for _, name := range []string{"out", "version"} {
		if f := packCmd.Flag(name); f == nil {
			t.Errorf("packCmd must expose the --%s flag", name)
		}
	}
}

func TestSignKeyFlagRegistered(t *testing.T) {
	if f := signCmd.Flag("key"); f == nil {
		t.Error("signCmd must expose the --key flag")
	}
}

func TestMarketplaceInstallFlagsRegistered(t *testing.T) {
	for _, name := range []string{"force", "allow-unsigned"} {
		if f := marketplaceInstallCmd.Flag(name); f == nil {
			t.Errorf("marketplaceInstallCmd must expose the --%s flag", name)
		}
	}
}

func TestNormalizeModuleArg(t *testing.T) {
	cases := map[string]string{
		"mod.acme.crm":                  "mod.acme.crm",
		"./acme-crm":                    "acme-crm",
		"library/modules/mod.acme.crm":  "mod.acme.crm",
		"library/modules/mod.acme.crm/": "mod.acme.crm",
	}
	for in, want := range cases {
		if got := normalizeModuleArg(in); got != want {
			t.Errorf("normalizeModuleArg(%q) = %q, want %q", in, got, want)
		}
	}
}
