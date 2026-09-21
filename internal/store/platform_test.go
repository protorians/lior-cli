package store

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListKnowledgeArticles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/developer-store/modules/mod-1/knowledge" || r.Method != http.MethodGet {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		raiton(w, http.StatusOK, `[{"id":"a1","title":"Guide","slug":"guide","kind":"GUIDE","status":"PUBLISHED","readingMinutes":3}]`)
	}))
	defer server.Close()

	client := testClient(server.URL)
	articles, err := client.ListKnowledgeArticles(t.Context(), "mod-1")
	if err != nil {
		t.Fatalf("ListKnowledgeArticles: %v", err)
	}
	if len(articles) != 1 || articles[0].Title != "Guide" || articles[0].ReadingMinutes != 3 {
		t.Errorf("articles incorrects: %+v", articles)
	}
}

func TestCreateKnowledgeArticle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/developer-store/modules/mod-1/knowledge" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var body CreateKnowledgeArticleRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if body.Title != "Mon guide" || body.Slug != "mon-guide" || body.Kind != "REFERENCE" || !body.Publish {
			http.Error(w, "body mismatch", http.StatusBadRequest)
			return
		}
		raiton(w, http.StatusCreated, `{"id":"a2","title":"Mon guide","slug":"mon-guide","kind":"REFERENCE","status":"PUBLISHED","readingMinutes":1}`)
	}))
	defer server.Close()

	client := testClient(server.URL)
	article, err := client.CreateKnowledgeArticle(t.Context(), "mod-1", CreateKnowledgeArticleRequest{
		Title: "Mon guide", Slug: "mon-guide", Kind: "REFERENCE", ReadingMinutes: 1, Publish: true,
	})
	if err != nil {
		t.Fatalf("CreateKnowledgeArticle: %v", err)
	}
	if article.ID != "a2" || article.Status != "PUBLISHED" {
		t.Errorf("article incorrect: %+v", article)
	}
}

func TestPublishKnowledgeArticle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/developer-store/modules/mod-1/knowledge/a1" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var body map[string]bool
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if !body["publish"] {
			http.Error(w, "publish must be true", http.StatusBadRequest)
			return
		}
		raiton(w, http.StatusOK, `{"id":"a1","title":"Guide","slug":"guide","kind":"GUIDE","status":"PUBLISHED","readingMinutes":0}`)
	}))
	defer server.Close()

	client := testClient(server.URL)
	article, err := client.PublishKnowledgeArticle(t.Context(), "mod-1", "a1", true)
	if err != nil {
		t.Fatalf("PublishKnowledgeArticle: %v", err)
	}
	if article.Status != "PUBLISHED" {
		t.Errorf("status = %q, want PUBLISHED", article.Status)
	}
}

func TestDeleteWorkflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/developer-store/modules/mod-1/workflows/wf-1" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		raiton(w, http.StatusOK, `null`)
	}))
	defer server.Close()

	client := testClient(server.URL)
	if err := client.DeleteWorkflow(t.Context(), "mod-1", "wf-1"); err != nil {
		t.Fatalf("DeleteWorkflow: %v", err)
	}
}

func TestRunWorkflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/developer-store/modules/mod-1/workflows/wf-1/run" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		raiton(w, http.StatusOK, `{"id":"wf-1","name":"CI","status":"RUNNING","runCount":4}`)
	}))
	defer server.Close()

	client := testClient(server.URL)
	workflow, err := client.RunWorkflow(t.Context(), "mod-1", "wf-1")
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	if workflow.Status != "RUNNING" || workflow.RunCount != 4 {
		t.Errorf("workflow incorrect: %+v", workflow)
	}
}

func TestPublishChannel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/developer-store/modules/mod-1/channels/BETA/publish" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if body["versionString"] != "1.2.0" {
			http.Error(w, "version mismatch", http.StatusBadRequest)
			return
		}
		raiton(w, http.StatusOK, `{"id":"c1","name":"BETA","versionString":"1.2.0","status":"ACTIVE","updateCount":2,"rollbackAllowed":true}`)
	}))
	defer server.Close()

	client := testClient(server.URL)
	channel, err := client.PublishChannel(t.Context(), "mod-1", "BETA", "1.2.0")
	if err != nil {
		t.Fatalf("PublishChannel: %v", err)
	}
	if channel.VersionString != "1.2.0" || !channel.RollbackAllowed {
		t.Errorf("canal incorrect: %+v", channel)
	}
}

func TestRollbackChannelConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/developer-store/modules/mod-1/channels/RELEASE/rollback" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		raitonError(w, http.StatusConflict, "CONFLICT", "Aucun rollback disponible")
	}))
	defer server.Close()

	client := testClient(server.URL)
	if _, err := client.RollbackChannel(t.Context(), "mod-1", "RELEASE"); err == nil {
		t.Error("RollbackChannel doit échouer sur un canal non rollbackable")
	}
}

func TestPauseChannel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/developer-store/modules/mod-1/channels/NIGHTLY/pause" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		raiton(w, http.StatusOK, `{"id":"c2","name":"NIGHTLY","versionString":"","status":"PAUSED","updateCount":0,"rollbackAllowed":false}`)
	}))
	defer server.Close()

	client := testClient(server.URL)
	channel, err := client.PauseChannel(t.Context(), "mod-1", "NIGHTLY")
	if err != nil {
		t.Fatalf("PauseChannel: %v", err)
	}
	if channel.Status != "PAUSED" {
		t.Errorf("status = %q, want PAUSED", channel.Status)
	}
}

func TestSaveModulePlatforms(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/developer-store/modules/mod-1/platforms" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var body struct {
			Platforms []PlatformSupport `json:"platforms"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if len(body.Platforms) != 2 || !body.Platforms[0].Supported {
			http.Error(w, "platforms mismatch", http.StatusBadRequest)
			return
		}
		raiton(w, http.StatusOK, `[{"platform":"WEB","supported":true,"modes":["DEFAULT"],"os":[]},{"platform":"DESKTOP","supported":false,"modes":[],"os":[]}]`)
	}))
	defer server.Close()

	client := testClient(server.URL)
	saved, err := client.SaveModulePlatforms(t.Context(), "mod-1", []PlatformSupport{
		{Platform: "WEB", Supported: true, Modes: []string{"DEFAULT"}},
		{Platform: "DESKTOP", Supported: false},
	})
	if err != nil {
		t.Fatalf("SaveModulePlatforms: %v", err)
	}
	if len(saved) != 2 || !saved[0].Supported {
		t.Errorf("plateformes incorrectes: %+v", saved)
	}
}

func TestCreateRequirement(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/developer-store/modules/mod-1/requirements" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var body CreateRequirementRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if body.ModuleID != "core.crm" || body.Kind != "OPTIONAL" {
			http.Error(w, "body mismatch", http.StatusBadRequest)
			return
		}
		raiton(w, http.StatusCreated, `{"id":"r1","moduleId":"core.crm","name":"CRM","versionRange":">=1.0.0","kind":"OPTIONAL","reason":""}`)
	}))
	defer server.Close()

	client := testClient(server.URL)
	requirement, err := client.CreateRequirement(t.Context(), "mod-1", CreateRequirementRequest{
		ModuleID: "core.crm", Name: "CRM", VersionRange: ">=1.0.0", Kind: "OPTIONAL",
	})
	if err != nil {
		t.Fatalf("CreateRequirement: %v", err)
	}
	if requirement.ID != "r1" || requirement.Kind != "OPTIONAL" {
		t.Errorf("requirement incorrect: %+v", requirement)
	}
}

func TestListSigningKeysAndRotate(t *testing.T) {
	rotated := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/developer-store/signing-keys":
			raiton(w, http.StatusOK, `[{"id":"k1","keyId":"sign_abc","algorithm":"Ed25519","status":"ACTIVE"}]`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/developer-store/signing-keys/sign_abc/rotate":
			rotated = true
			raiton(w, http.StatusOK, `{"id":"k2","keyId":"sign_def","algorithm":"Ed25519","status":"ACTIVE"}`)
		default:
			http.Error(w, "not found: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := testClient(server.URL)
	keys, err := client.ListSigningKeys(t.Context())
	if err != nil {
		t.Fatalf("ListSigningKeys: %v", err)
	}
	if len(keys) != 1 || keys[0].KeyID != "sign_abc" {
		t.Errorf("clés incorrectes: %+v", keys)
	}
	key, err := client.RotateSigningKey(t.Context(), "sign_abc")
	if err != nil {
		t.Fatalf("RotateSigningKey: %v", err)
	}
	if !rotated || key.KeyID != "sign_def" {
		t.Errorf("rotation incorrecte: %+v", key)
	}
}

func TestListAccreditationsAndCreate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/developer-store/accreditations":
			raiton(w, http.StatusOK, `[{"id":"acc1","kind":"CI_CD_TOKEN","name":"GitHub Actions","status":"LINKED"}]`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/developer-store/accreditations":
			var body CreateAccreditationRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "bad body", http.StatusBadRequest)
				return
			}
			if body.Kind != "SIGNING_KEY" || body.Name != "Release key" {
				http.Error(w, "body mismatch", http.StatusBadRequest)
				return
			}
			raiton(w, http.StatusCreated, `{"id":"acc2","kind":"SIGNING_KEY","name":"Release key","status":"PENDING"}`)
		default:
			http.Error(w, "not found: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := testClient(server.URL)
	accreditations, err := client.ListAccreditations(t.Context())
	if err != nil {
		t.Fatalf("ListAccreditations: %v", err)
	}
	if len(accreditations) != 1 || accreditations[0].Status != "LINKED" {
		t.Errorf("accréditations incorrectes: %+v", accreditations)
	}
	accreditation, err := client.CreateAccreditation(t.Context(), CreateAccreditationRequest{
		Kind: "SIGNING_KEY", Name: "Release key",
	})
	if err != nil {
		t.Fatalf("CreateAccreditation: %v", err)
	}
	if accreditation.ID != "acc2" {
		t.Errorf("accréditation incorrecte: %+v", accreditation)
	}
}

func TestEnvironmentVariablesModuleScoped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/developer-store/environment-variables" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("moduleId") != "mod-1" {
			http.Error(w, "moduleId missing", http.StatusBadRequest)
			return
		}
		raiton(w, http.StatusOK, `[{"id":"v1","key":"API_URL","environments":["PRODUCTION"],"visibility":"MASKED","productId":"mod-1"}]`)
	}))
	defer server.Close()

	client := testClient(server.URL)
	variables, err := client.ListEnvironmentVariables(t.Context(), "mod-1")
	if err != nil {
		t.Fatalf("ListEnvironmentVariables: %v", err)
	}
	if len(variables) != 1 || variables[0].Key != "API_URL" {
		t.Errorf("variables incorrectes: %+v", variables)
	}
}

func TestCreateEnvironmentVariable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/developer-store/environment-variables" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var body SaveEnvironmentVariableRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if body.Key != "API_KEY" || body.Value != "secret" || len(body.Environments) != 1 {
			http.Error(w, "body mismatch", http.StatusBadRequest)
			return
		}
		raiton(w, http.StatusCreated, `{"id":"v2","key":"API_KEY","environments":["STAGING"],"visibility":"MASKED","productId":""}`)
	}))
	defer server.Close()

	client := testClient(server.URL)
	variable, err := client.CreateEnvironmentVariable(t.Context(), SaveEnvironmentVariableRequest{
		Key: "API_KEY", Value: "secret", Environments: []string{"STAGING"}, Visibility: "MASKED",
	})
	if err != nil {
		t.Fatalf("CreateEnvironmentVariable: %v", err)
	}
	if variable.ID != "v2" {
		t.Errorf("variable incorrecte: %+v", variable)
	}
}

func TestUpdateAndDeleteEnvironmentVariable(t *testing.T) {
	updated := false
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/developer-store/environment-variables/v2" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		switch r.Method {
		case http.MethodPut:
			updated = true
			raiton(w, http.StatusOK, `{"id":"v2","key":"API_KEY","environments":["PRODUCTION"],"visibility":"PLAIN_TEXT","productId":""}`)
		case http.MethodDelete:
			deleted = true
			raiton(w, http.StatusOK, `null`)
		default:
			http.Error(w, "unexpected method", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := testClient(server.URL)
	if _, err := client.UpdateEnvironmentVariable(t.Context(), "v2", SaveEnvironmentVariableRequest{
		Key: "API_KEY", Value: "new", Environments: []string{"PRODUCTION"}, Visibility: "PLAIN_TEXT",
	}); err != nil {
		t.Fatalf("UpdateEnvironmentVariable: %v", err)
	}
	if err := client.DeleteEnvironmentVariable(t.Context(), "v2"); err != nil {
		t.Fatalf("DeleteEnvironmentVariable: %v", err)
	}
	if !updated || !deleted {
		t.Errorf("updated=%v deleted=%v, want both true", updated, deleted)
	}
}

func TestGithubConnection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/developer-store/github":
			raiton(w, http.StatusOK, `{"id":"g1","repository":"acme/app","defaultBranch":"main","workflowPath":".github/workflows/ci.yml","connected":true}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/developer-store/github/connect":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "bad body", http.StatusBadRequest)
				return
			}
			if body["repository"] != "acme/app" || body["productId"] != "mod-1" {
				http.Error(w, "body mismatch", http.StatusBadRequest)
				return
			}
			raiton(w, http.StatusOK, `{"id":"g2","repository":"acme/app","defaultBranch":"main","workflowPath":".github/workflows/ci.yml","connected":true}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/developer-store/github":
			raiton(w, http.StatusOK, `null`)
		default:
			http.Error(w, "not found: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := testClient(server.URL)
	connection, err := client.GetGithubConnection(t.Context(), "")
	if err != nil {
		t.Fatalf("GetGithubConnection: %v", err)
	}
	if connection == nil || connection.Repository != "acme/app" || !connection.Connected {
		t.Errorf("connexion incorrecte: %+v", connection)
	}
	linked, err := client.ConnectGithub(t.Context(), "acme/app", "main", ".github/workflows/ci.yml", "mod-1")
	if err != nil {
		t.Fatalf("ConnectGithub: %v", err)
	}
	if linked.ID != "g2" {
		t.Errorf("connexion liée incorrecte: %+v", linked)
	}
	if err := client.DisconnectGithub(t.Context(), ""); err != nil {
		t.Fatalf("DisconnectGithub: %v", err)
	}
}

func TestGetObserverAndUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/developer-store/modules/mod-1/observer":
			raiton(w, http.StatusOK, `{"productId":"mod-1","metrics":[{"id":"errors","label":"Erreurs","value":"2","trend":"stable","hint":"7 jours"}],"crashes":[]}`)
		case "/api/developer-store/modules/mod-1/usage":
			raiton(w, http.StatusOK, `{"productId":"mod-1","series":[{"label":"S28","installations":3,"activations":2}],"totals":{"installations":3,"activations":2,"activeOrganizations":1}}`)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := testClient(server.URL)
	observer, err := client.GetObserver(t.Context(), "mod-1")
	if err != nil {
		t.Fatalf("GetObserver: %v", err)
	}
	if len(observer.Metrics) != 1 || observer.Metrics[0].Label != "Erreurs" {
		t.Errorf("observer incorrect: %+v", observer)
	}
	usage, err := client.GetUsage(t.Context(), "mod-1")
	if err != nil {
		t.Fatalf("GetUsage: %v", err)
	}
	if len(usage.Series) != 1 || usage.Totals.ActiveOrganizations != 1 {
		t.Errorf("usage incorrect: %+v", usage)
	}
}

func TestPlatformPathEscapesModuleID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/developer-store/modules/mod-1/channels/RC/rollback" {
			http.Error(w, "not found: "+r.URL.Path, http.StatusNotFound)
			return
		}
		raiton(w, http.StatusOK, `{"id":"c3","name":"RC","versionString":"1.0.0","status":"ACTIVE","updateCount":1,"rollbackAllowed":false}`)
	}))
	defer server.Close()

	client := testClient(server.URL)
	if _, err := client.RollbackChannel(t.Context(), "mod-1", "RC"); err != nil {
		t.Fatalf("RollbackChannel: %v", err)
	}
}
