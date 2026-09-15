package store

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/protorians/sentient-cli/internal/auth"
	"github.com/protorians/sentient-cli/internal/module"
	"github.com/protorians/sentient-cli/internal/pkg"
)

func raiton(w http.ResponseWriter, status int, data string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	msg := `{"message":"ok","statusCode":` + itoa(status) + `,"data":`
	if status >= 400 {
		_, _ = w.Write([]byte(`{"message":"` + data + `","statusCode":` + itoa(status) + `,"data":null}`))
		return
	}
	_, _ = w.Write([]byte(msg + data + `}`))
}

func raitonError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"statusCode":` + itoa(status) + `,"message":"` + msg +
		`","data":null,"code":"` + code + `"}`))
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

func testClient(serverURL string) *Client {
	return &Client{Connector: &auth.Connector{Client: pkg.NewClient(serverURL)}}
}

func TestListModulesBareArray(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/developer-store/modules" || r.Method != http.MethodGet {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		raiton(w, http.StatusOK, `[{"id":"prod-1","name":"Mod A","isDeprecated":false},{"id":"prod-2","name":"Mod B","isDeprecated":true}]`)
	}))
	defer server.Close()

	client := testClient(server.URL)
	client.SetToken("test-token")

	modules, err := client.ListModules(t.Context())
	if err != nil {
		t.Fatalf("ListModules: %v", err)
	}
	if len(modules) != 2 {
		t.Fatalf("attendu 2 modules, reçu %d", len(modules))
	}
	if modules[0].Token != "prod-1" || modules[0].Name != "Mod A" {
		t.Errorf("premier module incorrect: %+v", modules[0])
	}
	if modules[1].Status != "DEPRECATED" {
		t.Errorf("deuxième module status = %q, want DEPRECATED", modules[1].Status)
	}
}

func TestListModulesPaginated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raiton(w, http.StatusOK, `{"items":[{"id":"p9","name":"Pag","isDeprecated":false}],"total":1,"page":1,"limit":20}`)
	}))
	defer server.Close()

	client := testClient(server.URL)
	modules, err := client.ListModules(t.Context())
	if err != nil {
		t.Fatalf("ListModules: %v", err)
	}
	if len(modules) != 1 || modules[0].Token != "p9" {
		t.Errorf("modules incorrects: %+v", modules)
	}
}

func TestListModulesAuthError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raitonError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Non authentifié")
	}))
	defer server.Close()

	client := testClient(server.URL)
	_, err := client.ListModules(t.Context())
	if err == nil {
		t.Error("ListModules doit échouer sans token valide")
	}
}

func TestGetModule(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/developer-store/modules/tok-abc":
			raiton(w, http.StatusOK, `{"id":"tok-abc","accountId":"dev1","name":"Blog","slug":"blog","type":"WEB_APP_REMOTE","primaryCategory":"SYSTEM","isDeprecated":false}`)
		case "/api/developer-store/modules/tok-abc/versions":
			raiton(w, http.StatusOK, `[{"id":"v9","moduleProductId":"tok-abc","versionString":"1.0.0","buildNumber":1,"status":"PUBLISHED"}]`)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := testClient(server.URL)
	mod, err := client.GetModule(t.Context(), "tok-abc")
	if err != nil {
		t.Fatalf("GetModule: %v", err)
	}
	if mod.Token != "tok-abc" || mod.Name != "Blog" || mod.Version != "1.0.0" {
		t.Errorf("module incorrect: %+v", mod)
	}
}

func TestGetModuleNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raitonError(w, http.StatusNotFound, "NOT_FOUND", "Module introuvable")
	}))
	defer server.Close()

	client := testClient(server.URL)
	_, err := client.GetModule(t.Context(), "missing")
	if err == nil {
		t.Error("GetModule doit échouer pour un module absent")
	}
}

func TestPublishCreatesProductThenVersionThenArtifact(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "test-module-0.1.0.smp")
	if err := os.WriteFile(archivePath, []byte("fake-zip-content"), 0o644); err != nil {
		t.Fatal(err)
	}

	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "expected POST", http.StatusBadRequest)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-pub-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/developer-store/modules":
			calls = append(calls, "create")
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "bad body", http.StatusBadRequest)
				return
			}
			if body["slug"] != "test-module" || body["type"] != "WEB_APP_REMOTE" {
				http.Error(w, "create body mismatch: "+body["type"]+"/"+body["slug"], http.StatusBadRequest)
				return
			}
			raiton(w, http.StatusCreated, `{"id":"prod-new","accountId":"dev1","name":"Test Module","slug":"test-module","type":"WEB_APP_REMOTE","primaryCategory":"SYSTEM"}`)
		case "/api/developer-store/modules/prod-new/versions":
			calls = append(calls, "version")
			var body createVersionRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "bad body", http.StatusBadRequest)
				return
			}
			if body.VersionString != "0.1.0" || body.BuildNumber != 1 {
				http.Error(w, "version body mismatch", http.StatusBadRequest)
				return
			}
			raiton(w, http.StatusCreated, `{"id":"version-1","moduleProductId":"prod-new","versionString":"0.1.0","buildNumber":1,"status":"DRAFT"}`)
		case "/api/developer-store/modules/prod-new/versions/version-1/artifact":
			calls = append(calls, "artifact")
			var body declareArtifactRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "bad body", http.StatusBadRequest)
				return
			}
			if body.Checksum == "" || body.Signature != "" || body.SizeBytes != int64(len("fake-zip-content")) {
				http.Error(w, "artifact body mismatch", http.StatusBadRequest)
				return
			}
			var m module.Manifest
			if err := json.Unmarshal(body.Manifest, &m); err != nil {
				http.Error(w, "manifest JSON invalid", http.StatusBadRequest)
				return
			}
			if m.ID != "test-module" {
				http.Error(w, "manifest id = "+m.ID, http.StatusBadRequest)
				return
			}
			raiton(w, http.StatusCreated, `{"url":"https://cdn.sentient.dev/artifacts/prod-new/version-1.smp","key":"prod-new/version-1","checksum":"`+body.Checksum+`","sizeBytes":17}`)
		default:
			http.Error(w, "not found: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := testClient(server.URL)
	client.SetToken("test-pub-token")

	manifest := module.NewManifest("test-module", "A test")
	manifest.Token = ""
	resp, err := client.Publish(t.Context(), archivePath, &manifest)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if resp.Token != "prod-new" {
		t.Errorf("Token = %q, want prod-new", resp.Token)
	}
	if resp.Version != "0.1.0" {
		t.Errorf("Version = %q, want 0.1.0", resp.Version)
	}
	if resp.URL != "https://cdn.sentient.dev/artifacts/prod-new/version-1.smp" {
		t.Errorf("URL = %q, want CDN URL", resp.URL)
	}
	if len(calls) != 3 {
		t.Errorf("attendu 3 appels (product/version/artifact), reçu %v", calls)
	}
}

func TestPublishReusesLinkedProduct(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "m-0.1.0.smp")
	if err := os.WriteFile(archivePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	publishedOn := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/developer-store/modules/linked-id":
			raiton(w, http.StatusOK, `{"id":"linked-id","accountId":"dev1","name":"Linked","slug":"m","type":"WEB_APP_REMOTE","primaryCategory":"SYSTEM"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/developer-store/modules/linked-id/versions":
			raiton(w, http.StatusOK, `{"id":"v1","moduleProductId":"linked-id","versionString":"0.1.0","buildNumber":1,"status":"DRAFT"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/developer-store/modules/linked-id/versions/v1/artifact":
			raiton(w, http.StatusOK, `{"url":"https://cdn.dev/linked-id/v1","sizeBytes":1}`)
			publishedOn = true
		default:
			http.Error(w, "not found: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := testClient(server.URL)
	manifest := module.NewManifest("linked", "")
	manifest.Token = "linked-id"
	resp, err := client.Publish(t.Context(), archivePath, &manifest)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if resp.Token != "linked-id" {
		t.Errorf("Token = %q, want linked-id", resp.Token)
	}
	if !publishedOn {
		t.Error("la publication doit se faire sur le produit lié")
	}
}

func TestPublishMissingArchive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "should not reach", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := testClient(server.URL)
	manifest := module.NewManifest("mod", "")
	manifest.Token = ""
	_, err := client.Publish(t.Context(), "/nonexistent/archive.smp", &manifest)
	if err == nil {
		t.Error("Publish doit échouer avec un fichier inexistant")
	}
}

func TestPublishServerError(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "mod.smp")
	if err := os.WriteFile(archivePath, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/developer-store/modules" && r.Method == http.MethodPost {
			raitonError(w, http.StatusConflict, "VERSION_CONFLICT", "Une version identique existe déjà pour ce module")
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	client := testClient(server.URL)
	client.SetToken("tok")
	manifest := module.NewManifest("mod", "")
	manifest.Token = ""
	_, err := client.Publish(t.Context(), archivePath, &manifest)
	if err == nil {
		t.Error("Publish doit échouer sur conflit de version")
	}
	var apiErr *pkg.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("erreur = %T, want *pkg.APIError (errors.As)", err)
	}
	if apiErr.StatusCode != http.StatusConflict {
		t.Errorf("StatusCode = %d, want 409", apiErr.StatusCode)
	}
}

func TestDeveloperTypeFor(t *testing.T) {
	cases := map[string]string{
		"":              ModuleTypeWebAppRemote,
		"EXTERNAL":      ModuleTypeWebAppRemote,
		"WEB_APP_LOCAL": ModuleTypeWebAppLocal,
		"EXTERNAL_URL":  ModuleTypeExternalURL,
		"CONFIGURATION": ModuleTypeConfiguration,
	}
	for in, want := range cases {
		if got := developerTypeFor(in); got != want {
			t.Errorf("developerTypeFor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSetToken(t *testing.T) {
	client := NewClient()
	client.SetToken("my-token")
	if client.Connector.Client.Token != "my-token" {
		t.Errorf("Token = %q, want my-token", client.Connector.Client.Token)
	}
}

func TestNewClient(t *testing.T) {
	client := NewClient()
	if client == nil {
		t.Fatal("NewClient() returned nil")
	}
	if client.Connector == nil {
		t.Error("Connector doit être initialisé")
	}
}
