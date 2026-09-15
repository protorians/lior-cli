package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/protorians/sentient-cli/internal/pkg"
)

func TestSessionSaveLoadClear(t *testing.T) {
	store := NewStoreVolatile()
	exp := time.Now().Add(time.Hour)
	sess := &Session{
		Store:       store,
		AccessToken: "access-123",
		MFAToken:    "mfa-tok",
		Device:      "device-42",
		ExpiresAt:   &exp,
		User:        &User{ID: "u1", Email: "dev@example.com", Username: "dev", Roles: []Role{{ID: "r1", Name: "Developer"}}},
	}

	if err := sess.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadSession(store)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if loaded == nil || !loaded.IsAuthenticated() {
		t.Fatal("session chargée non authentifiée")
	}
	if loaded.AccessToken != "access-123" || loaded.MFAToken != "mfa-tok" || loaded.Device != "device-42" {
		t.Errorf("session incorrecte: %+v", loaded)
	}
	if loaded.User == nil || loaded.User.Email != "dev@example.com" {
		t.Errorf("user incorrect: %+v", loaded.User)
	}
	if loaded.IsExpired() {
		t.Error("session fraîche doit ne pas être expirée")
	}

	if err := sess.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	after, err := LoadSession(store)
	if err != nil {
		t.Fatalf("LoadSession après Clear: %v", err)
	}
	if after != nil && after.IsAuthenticated() {
		t.Error("les credentials doivent être supprimées après Clear")
	}
}

func TestSessionExpired(t *testing.T) {
	store := NewStoreVolatile()
	past := time.Now().Add(-time.Minute)
	sess := &Session{
		Store:       store,
		AccessToken: "token",
		ExpiresAt:   &past,
	}
	if err := sess.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, _ := LoadSession(store)
	if !loaded.IsExpired() {
		t.Error("token dans le passé doit être marqué expiré")
	}
}

func TestAPIConfigFromWorkspaceAppConfig(t *testing.T) {
	t.Setenv(EnvAPIBase, "")

	root := t.TempDir()
	sub := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `{"applications":{"sentient-auth":{"api":{"baseUrl":"https://workspace.example.com","timeout":5000}}}}`
	if err := os.WriteFile(filepath.Join(root, "app.config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(sub); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(wd) }()

	if base := apiBaseURL(); base != "https://workspace.example.com" {
		t.Errorf("apiBaseURL = %q, want https://workspace.example.com", base)
	}
	if d := apiTimeout(); d != 5*time.Second {
		t.Errorf("apiTimeout = %v, want 5s", d)
	}
}

func TestConnectorSignIn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/sign-in" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"Connexion réussie","statusCode":200,"data":{"user":{"id":"u1","email":"dev@example.com","username":"dev","roles":[{"id":"r1","name":"Developer"}]},"token":"tok-session","device":"d1"}}`))
	}))
	defer server.Close()

	conn := &Connector{Client: pkg.NewClient(server.URL)}
	resp, err := conn.SignIn(t.Context(), SignInRequest{Email: "dev@example.com", Password: "secret"})
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if resp.Token != "tok-session" {
		t.Errorf("Token = %q, want tok-session", resp.Token)
	}
	if resp.User.Email != "dev@example.com" || resp.User.Username != "dev" {
		t.Errorf("User incorrect: %+v", resp.User)
	}
	if resp.User.Role != "Developer" {
		t.Errorf("Role dérivé = %q, want Developer", resp.User.Role)
	}
	if resp.Device != "d1" {
		t.Errorf("Device = %q, want d1", resp.Device)
	}
}

func TestConnectorSignInAuthError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"statusCode":401,"message":"Identifiants invalides","data":null,"code":"INVALID_CREDENTIALS"}`))
	}))
	defer server.Close()

	conn := &Connector{Client: pkg.NewClient(server.URL)}
	_, err := conn.SignIn(t.Context(), SignInRequest{Email: "x", Password: "y"})
	if err == nil {
		t.Fatal("SignIn doit échouer")
	}
	apiErr, ok := err.(*pkg.APIError)
	if !ok {
		t.Fatalf("erreur = %T, want *pkg.APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", apiErr.StatusCode)
	}
	if apiErr.Message != "Identifiants invalides" {
		t.Errorf("Message = %q, want Identifiants invalides", apiErr.Message)
	}
}

func TestConnectorMFAFlow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-session" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/mfa/challenge":
			_, _ = w.Write([]byte(`{"message":"ok","statusCode":200,"data":{"mfaRequired":true,"challenge":"foo","factors":[{"id":"f1","type":"totp","label":"TOTP","enabled":true}]}}`))
		case "/api/mfa/totp/verify":
			var body VerifyRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Code != "123456" {
				http.Error(w, "bad code", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"message":"ok","statusCode":200,"data":{"mfaVerified":true,"mfaToken":"mfa-jwt"}}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	conn := &Connector{Client: pkg.NewClient(server.URL)}
	conn.Client.Token = "tok-session"
	authn := &Authenticator{Connector: conn}

	gotCode := false
	resp, err := authn.Verify(t.Context(), func(kind FactorKind) (string, error) {
		if kind != FactorTOTP {
			t.Errorf("facteur = %q, want totp", kind)
		}
		gotCode = true
		return "123456", nil
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !gotCode {
		t.Error("le prompt TOTP doit être appelé")
	}
	if resp == nil || !resp.MFAVerified || resp.MFAToken != "mfa-jwt" {
		t.Errorf("réponse MFA incorrecte: %+v", resp)
	}
}

func TestConnectorMFAFlowNotRequired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message":"ok","statusCode":200,"data":{"mfaRequired":false,"factors":[]}}`))
	}))
	defer server.Close()

	conn := &Connector{Client: pkg.NewClient(server.URL)}
	conn.Client.Token = "tok-session"
	authn := &Authenticator{Connector: conn}

	resp, err := authn.Verify(t.Context(), func(FactorKind) (string, error) {
		t.Error("aucun prompt ne doit être appelé si MFA n'est pas requise")
		return "", nil
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if resp != nil {
		t.Errorf("réponse = %+v, want nil (pas de MFA requise)", resp)
	}
}
