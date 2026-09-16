package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/protorians/sentient-cli/internal/pkg"
)

func TestGeneratePKCE(t *testing.T) {
	p, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE: %v", err)
	}
	if len(p.Verifier) < 43 || len(p.Verifier) > 128 {
		t.Errorf("Verifier length = %d, want within [43, 128]", len(p.Verifier))
	}
	sum := sha256.Sum256([]byte(p.Verifier))
	if want := base64.RawURLEncoding.EncodeToString(sum[:]); p.Challenge != want {
		t.Errorf("Challenge = %q, want %q", p.Challenge, want)
	}
}

func TestGeneratePKCEUnique(t *testing.T) {
	a, _ := GeneratePKCE()
	b, _ := GeneratePKCE()
	if a.Verifier == b.Verifier {
		t.Error("deux verifiers doivent être distincts")
	}
}

func TestRandomStateUnique(t *testing.T) {
	a, err := RandomState()
	if err != nil {
		t.Fatalf("RandomState: %v", err)
	}
	b, _ := RandomState()
	if a == "" || a == b {
		t.Errorf("state non unique: %q / %q", a, b)
	}
}

func TestAuthorizationURL(t *testing.T) {
	got, err := AuthorizationURL("https://auth.example.com", "/oauth/authorize",
		"sentient-cli", "http://127.0.0.1:4242/callback", "openid profile", "challenge-abc", "state-xyz")
	if err != nil {
		t.Fatalf("AuthorizationURL: %v", err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("URL invalide: %v", err)
	}
	if u.Scheme != "https" || u.Host != "auth.example.com" || u.Path != "/oauth/authorize" {
		t.Errorf("URL = %s", got)
	}
	q := u.Query()
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q", q.Get("response_type"))
	}
	if q.Get("client_id") != "sentient-cli" {
		t.Errorf("client_id = %q", q.Get("client_id"))
	}
	if q.Get("redirect_uri") != "http://127.0.0.1:4242/callback" {
		t.Errorf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	if q.Get("code_challenge") != "challenge-abc" || q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge = %q, method = %q", q.Get("code_challenge"), q.Get("code_challenge_method"))
	}
	if q.Get("state") != "state-xyz" {
		t.Errorf("state = %q", q.Get("state"))
	}
	if q.Get("scope") != "openid profile" {
		t.Errorf("scope = %q", q.Get("scope"))
	}
}

func TestExchangeAuthorizationCode(t *testing.T) {
	var gotForm url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
			t.Errorf("Content-Type = %q", ct)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at-1","token_type":"Bearer","expires_in":3600,"refresh_token":"rt-1","scope":"openid"}`))
	}))
	defer server.Close()

	tok, err := ExchangeAuthorizationCode(t.Context(), pkg.NewClient(server.URL), "/oauth/token",
		"sentient-cli", "http://127.0.0.1:1/callback", "the-code", "the-verifier")
	if err != nil {
		t.Fatalf("ExchangeAuthorizationCode: %v", err)
	}
	if tok.AccessToken != "at-1" || tok.TokenType != "Bearer" || tok.ExpiresIn != 3600 || tok.RefreshToken != "rt-1" {
		t.Errorf("TokenResponse = %+v", tok)
	}
	if gotForm.Get("grant_type") != "authorization_code" || gotForm.Get("code") != "the-code" ||
		gotForm.Get("code_verifier") != "the-verifier" || gotForm.Get("client_id") != "sentient-cli" {
		t.Errorf("form = %v", gotForm)
	}
}

func TestExchangeAuthorizationCodeEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"ok","statusCode":200,"data":{"access_token":"at-env","token_type":"Bearer","expires_in":60}}`))
	}))
	defer server.Close()

	tok, err := ExchangeAuthorizationCode(t.Context(), pkg.NewClient(server.URL), "/oauth/token",
		"sentient-cli", "http://127.0.0.1:1/callback", "code", "verifier")
	if err != nil {
		t.Fatalf("ExchangeAuthorizationCode (envelope): %v", err)
	}
	if tok.AccessToken != "at-env" {
		t.Errorf("AccessToken = %q, want at-env", tok.AccessToken)
	}
}

func TestExchangeAuthorizationCodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"code expired"}`))
	}))
	defer server.Close()

	_, err := ExchangeAuthorizationCode(t.Context(), pkg.NewClient(server.URL), "/oauth/token",
		"sentient-cli", "http://127.0.0.1:1/callback", "code", "verifier")
	if err == nil {
		t.Fatal("ExchangeAuthorizationCode doit échouer")
	}
	apiErr, ok := err.(*pkg.APIError)
	if !ok {
		t.Fatalf("erreur = %T, want *pkg.APIError", err)
	}
	if !strings.Contains(apiErr.Message, "invalid_grant") {
		t.Errorf("Message = %q, want invalid_grant", apiErr.Message)
	}
}

func TestExchangeMissingAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"token_type":"Bearer","expires_in":60}`))
	}))
	defer server.Close()

	if _, err := ExchangeAuthorizationCode(t.Context(), pkg.NewClient(server.URL), "/oauth/token",
		"sentient-cli", "http://127.0.0.1:1/callback", "code", "verifier"); err == nil {
		t.Fatal("une réponse sans access_token doit échouer")
	}
}

func TestStoreOAuthSession(t *testing.T) {
	store := NewStoreVolatile()
	tok := &TokenResponse{AccessToken: "at-1", TokenType: "Bearer", ExpiresIn: 3600, RefreshToken: "rt-1"}
	if err := StoreOAuthSession(store, tok); err != nil {
		t.Fatalf("StoreOAuthSession: %v", err)
	}

	sess, err := LoadSession(store)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if sess == nil || sess.AccessToken != "at-1" {
		t.Fatalf("session = %+v", sess)
	}
	if sess.ExpiresAt == nil || sess.IsExpired() {
		t.Error("la session doit être fraîche avec une expiration future")
	}
	if rt, err := store.Get(KeyOAuthRefreshToken); err != nil || rt != "rt-1" {
		t.Errorf("refresh token = %q, err = %v", rt, err)
	}
}

func TestSessionRefreshUsesOAuthRefreshToken(t *testing.T) {
	var gotGrant, gotToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/oauth/token") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		gotGrant = r.PostForm.Get("grant_type")
		gotToken = r.PostForm.Get("refresh_token")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at-new","token_type":"Bearer","expires_in":1800,"refresh_token":"rt-new"}`))
	}))
	defer server.Close()

	store := NewStoreVolatile()
	if err := store.Set(KeyAccessToken, "at-old"); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(KeyOAuthRefreshToken, "rt-old"); err != nil {
		t.Fatal(err)
	}
	sess := &Session{Store: store, AccessToken: "at-old"}
	connector := &Connector{Client: pkg.NewClient(server.URL)}

	if err := sess.Refresh(t.Context(), connector); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if gotGrant != "refresh_token" || gotToken != "rt-old" {
		t.Errorf("form = grant %q / token %q, want refresh_token / rt-old", gotGrant, gotToken)
	}
	if sess.AccessToken != "at-new" {
		t.Errorf("AccessToken = %q, want at-new", sess.AccessToken)
	}
	if rt, err := store.Get(KeyOAuthRefreshToken); err != nil || rt != "rt-new" {
		t.Errorf("refresh token = %q (err %v), want rt-new (rotation)", rt, err)
	}
}

func TestLoopbackCallbackServer(t *testing.T) {
	cs, redirect, err := StartLoopbackCallback("/callback")
	if err != nil {
		t.Fatalf("StartLoopbackCallback: %v", err)
	}
	defer cs.Close()

	if !strings.HasPrefix(redirect, "http://127.0.0.1:") || !strings.HasSuffix(redirect, "/callback") {
		t.Errorf("redirect = %q", redirect)
	}

	go func() {
		resp, err := http.Get(redirect + "?code=abc&state=s1")
		if err != nil {
			t.Errorf("GET callback: %v", err)
			return
		}
		resp.Body.Close()
	}()

	res, err := cs.Wait(t.Context())
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if res.Code != "abc" || res.State != "s1" {
		t.Errorf("CallbackResult = %+v", res)
	}
}
