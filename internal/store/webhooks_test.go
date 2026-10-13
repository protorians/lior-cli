package store

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/protorians/lior-cli/internal/module"
)

func webhooksDeclarationForTest() *module.WebhooksDeclaration {
	return &module.WebhooksDeclaration{
		SchemaVersion: module.WebhooksSchemaVersion,
		Version:       "1.2.0",
		Actions: []module.WebhookActionDeclaration{
			{
				Action:      "crm:customer.created",
				Task:        "customer",
				Name:        "created",
				Description: "Émise quand un client est créé.",
				Payload:     []byte(`{"type":"object"}`),
			},
		},
		Subscriptions: []module.WebhookSubscriptionDeclaration{
			{Action: "blogging:post.published"},
		},
	}
}

// TestPushWebhooks relays the declaration to
// `POST /api/developer-store/modules/:id/webhooks` and returns the registry
// summary (AC-12).
func TestPushWebhooks(t *testing.T) {
	var gotPath, gotMethod, gotAuth string
	var gotBody RegisterWebhooksRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod, gotAuth = r.URL.Path, r.Method, r.Header.Get("Authorization")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("lecture du corps : %v", err)
		}
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Errorf("corps illisible : %v", err)
		}
		raiton(w, http.StatusOK, `{"created":1,"updated":0,"deactivated":0,"total":1}`)
	}))
	defer server.Close()

	client := testClient(server.URL)
	client.SetToken("test-token")

	summary, err := client.PushWebhooks(t.Context(), "prod-1", webhooksDeclarationForTest(), "crm")
	if err != nil {
		t.Fatalf("PushWebhooks: %v", err)
	}
	if gotPath != "/api/developer-store/modules/prod-1/webhooks" || gotMethod != http.MethodPost {
		t.Errorf("chemin/méthode = %q %q", gotPath, gotMethod)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want Bearer test-token", gotAuth)
	}
	if gotBody.ModuleKey != "crm" || gotBody.ModuleToken != "prod-1" || gotBody.Version != "1.2.0" {
		t.Errorf("corps inattendu : %+v", gotBody)
	}
	if len(gotBody.Actions) != 1 || gotBody.Actions[0].Action != "crm:customer.created" ||
		gotBody.Actions[0].Task != "customer" || gotBody.Actions[0].Name != "created" {
		t.Errorf("action transportée inattendue : %+v", gotBody.Actions)
	}
	if len(gotBody.Subscriptions) != 1 || gotBody.Subscriptions[0].Action != "blogging:post.published" {
		t.Errorf("subscription transportée inattendue : %+v", gotBody.Subscriptions)
	}
	if summary.Created != 1 || summary.Total != 1 {
		t.Errorf("résumé incorrect : %+v", summary)
	}
}

// TestListWebhooks lists registry rows with the optional version query.
func TestListWebhooks(t *testing.T) {
	var gotPath, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.Query().Get("version")
		raiton(w, http.StatusOK, `[{"action":"crm:customer.created","version":"1.2.0","task":"customer","name":"created","description":"…","isActive":true}]`)
	}))
	defer server.Close()

	client := testClient(server.URL)
	client.SetToken("test-token")

	entries, err := client.ListWebhooks(t.Context(), "prod-1", "1.2.0")
	if err != nil {
		t.Fatalf("ListWebhooks: %v", err)
	}
	if gotPath != "/api/developer-store/modules/prod-1/webhooks" || gotQuery != "1.2.0" {
		t.Errorf("chemin/requête = %q (%s)", gotPath, gotQuery)
	}
	if len(entries) != 1 || entries[0].Action != "crm:customer.created" || entries[0].Version != "1.2.0" || !entries[0].IsActive {
		t.Errorf("lignes inattendues : %+v", entries)
	}
}

// TestPublishCarriesWebhooksField covers path 3 (AC-13): a publish with a
// validated declaration sends the JSON of the declaration in the `webhooks`
// multipart field; absent → no field.
func TestPublishCarriesWebhooksField(t *testing.T) {
	manifest := module.NewManifest("test-webhooks", "A test")
	manifest.Token = "" // force product creation through POST /modules

	for i, decl := range []*module.WebhooksDeclaration{webhooksDeclarationForTest(), nil} {
		var gotWebhooksField, gotFile string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == modulesPath && r.Method == http.MethodPost:
				raiton(w, http.StatusOK, `{"id":"prod-x","name":"test-webhooks"}`)
			case r.URL.Path == modulesPath+"/prod-x/versions" && r.Method == http.MethodPost:
				raiton(w, http.StatusOK, `{"id":"ver-x","versionString":"0.1.0"}`)
			case r.URL.Path == modulesPath+"/prod-x/versions" && r.Method == http.MethodGet:
				raiton(w, http.StatusOK, `[]`) // fetchVersions (best-effort)
			case r.URL.Path == modulesPath+"/prod-x/versions/ver-x/artifact/upload":
				gotWebhooksField = r.FormValue("webhooks")
				file, head, ferr := r.FormFile("file")
				if ferr != nil {
					t.Errorf("partie fichier absente : %v", ferr)
				} else {
					gotFile = head.Filename
					file.Close()
				}
				raiton(w, http.StatusOK, `{"url":"https://cdn/x","checksum":"c","sizeBytes":1}`)
			default:
				t.Errorf("route non simulée : %s", r.URL.Path)
			}
		}))
		client := testClient(server.URL)
		client.SetToken("test-token")

		archivePath := filepath.Join(t.TempDir(), "test-webhooks.LiorArtifactPackage")
		if err := os.WriteFile(archivePath, []byte("fake-zip-content"), 0o644); err != nil {
			t.Fatalf("archive de test : %v", err)
		}
		if _, err := client.Publish(t.Context(), archivePath, &manifest, decl); err != nil {
			t.Fatalf("Publish #%d: %v", i, err)
		}
		if gotFile != "test-webhooks.LiorArtifactPackage" {
			t.Errorf("Publish #%d: fichier = %q", i, gotFile)
		}
		if i == 0 {
			if !strings.Contains(gotWebhooksField, `"action":"crm:customer.created"`) {
				t.Errorf("Publish #%d: champ webhooks inattendu : %q", i, gotWebhooksField)
			}
		} else if gotWebhooksField != "" {
			t.Errorf("Publish #%d: champ webhooks inattendu : %q", i, gotWebhooksField)
		}
		server.Close()
	}
}
