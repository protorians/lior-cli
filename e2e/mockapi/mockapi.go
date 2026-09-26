// Package mockapi is an in-memory HTTP server replicating the
// `liorian-connect` wire contract (Raiton envelope `{message, data,
// statusCode}`) for the E2E testscript suite: auth, guarded MFA, the
// developer-store pipeline (product → version → artifact), and the public
// module catalog behind `marketplace` (`/api/catalog/*`).
package mockapi

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// Product mirrors the StoreModuleProduct entity.
type Product struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Slug              string `json:"slug"`
	Type              string `json:"type"`
	Description       string `json:"description,omitempty"`
	Icon              string `json:"icon,omitempty"`
	PrimaryCategory   string `json:"primaryCategory,omitempty"`
	SecondaryCategory string `json:"secondaryCategory,omitempty"`
	Token             string `json:"token,omitempty"`
	IsDeprecated      bool   `json:"isDeprecated"`
}

// Version mirrors the ModuleVersion entity.
type Version struct {
	ID            string `json:"id"`
	ModuleProduct string `json:"moduleProductId"`
	VersionString string `json:"versionString"`
	BuildNumber   int    `json:"buildNumber"`
	Status        string `json:"status"`
	ArtifactURL   string `json:"artifactUrl,omitempty"`
}

// Server is the in-memory fake of the liorian-connect API.
type Server struct {
	mu        sync.Mutex
	products  map[string]*Product
	versions  map[string][]Version
	created   map[string]int
	nextID    int
	knownToks map[string]string // session token -> email
	tokens    map[string]string // manifest token -> product id

	// Public catalog state (marketplace): storefront entries and their
	// artifact blobs, keyed by slug.
	catalog          []CatalogModule
	catalogArtifacts map[string][]byte

	// Module-lifecycle state: environment variables keyed by id.
	envVars  map[string]*envVar
	envVarID int
}

// envVar is a stateful environment variable served by the mock.
type envVar struct {
	ID           string   `json:"id"`
	Key          string   `json:"key"`
	Value        string   `json:"value,omitempty"`
	Environments []string `json:"environments"`
	Visibility   string   `json:"visibility"`
	ProductID    string   `json:"productId"`
}

// New builds a fresh mock server with no state, seeded with the public
// catalog modules used by the `marketplace` scenarios.
func New() *Server {
	s := &Server{
		products:         map[string]*Product{},
		versions:         map[string][]Version{},
		created:          map[string]int{},
		knownToks:        map[string]string{},
		tokens:           map[string]string{},
		catalogArtifacts: map[string][]byte{},
		envVars: map[string]*envVar{
			"var-1": {ID: "var-1", Key: "API_URL", Environments: []string{"PRODUCTION"}, Visibility: "MASKED"},
		},
		envVarID: 1,
	}
	s.seedCatalog()
	return s
}

// Handler returns the routing http.Handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/auth/sign-in", s.signIn)
	mux.HandleFunc("/api/auth/logout", s.logout)
	mux.HandleFunc("/api/auth/sessions/refresh", s.refresh)
	mux.HandleFunc("/api/mfa/challenge", s.challenge)
	mux.HandleFunc("/api/mfa/totp/verify", s.verifyTOTP)
	mux.HandleFunc("/api/mfa/recovery/verify", s.verifyRecovery)

	mux.HandleFunc("/oauth/token", s.oauthToken)

	mux.HandleFunc("/api/developer-store/modules/", s.storeModules)
	mux.HandleFunc("/api/developer-store/modules", s.storeModules)

	mux.HandleFunc("/api/developer-store/signing-keys", s.signingKeys)
	mux.HandleFunc("/api/developer-store/signing-keys/", s.signingKeys)
	mux.HandleFunc("/api/developer-store/accreditations", s.accreditations)
	mux.HandleFunc("/api/developer-store/accreditations/", s.accreditations)
	mux.HandleFunc("/api/developer-store/environment-variables", s.environmentVariables)
	mux.HandleFunc("/api/developer-store/environment-variables/", s.environmentVariables)
	mux.HandleFunc("/api/developer-store/github", s.github)
	mux.HandleFunc("/api/developer-store/github/", s.github)

	mux.HandleFunc("/api/catalog/modules", s.catalogSearch)
	mux.HandleFunc("/api/catalog/modules/", s.catalogGet)
	mux.HandleFunc("/catalog/artifacts/", s.catalogArtifact)
	return mux
}

// --- helpers ---

func writeData(w http.ResponseWriter, status int, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		writeError(w, 500, "internal serialization error")
		return
	}
	writeEnvelope(w, status, 200, "OK", payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeEnvelope(w, status, status, message, nil)
}

func writeEnvelope(w http.ResponseWriter, status, code int, message string, data json.RawMessage) {
	body := map[string]json.RawMessage{
		"message":    json.RawMessage(strconv.Quote(message)),
		"statusCode": json.RawMessage(strconv.Itoa(code)),
	}
	if data != nil {
		body["data"] = data
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func decodeBody(w http.ResponseWriter, r *http.Request, out any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		writeError(w, 400, "invalid request body")
		return false
	}
	return true
}

// bearerEmail extracts the session email from `Authorization: Bearer <tok>“.
func (s *Server) bearerEmail(r *http.Request) (string, bool) {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	token := strings.TrimPrefix(auth, "Bearer ")
	s.mu.Lock()
	defer s.mu.Unlock()
	email, ok := s.knownToks[token]
	return email, ok
}

func requireAuth(s *Server, w http.ResponseWriter, r *http.Request) bool {
	if _, ok := s.bearerEmail(r); ok {
		return true
	}
	writeError(w, 401, "Not authenticated")
	return false
}

// registerToken links a session token to its account email.
func (s *Server) registerToken(token, email string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.knownToks[token] = email
}

// --- auth & MFA ---

func (s *Server) signIn(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "Method not allowed")
		return
	}
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Email == "invalid@example.com" || req.Password == "wrong" {
		writeError(w, 401, "Invalid credentials")
		return
	}
	token := "tok-" + req.Email
	s.registerToken(token, req.Email)
	writeData(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id":       "usr_" + strings.Split(req.Email, "@")[0],
			"username": req.Email,
			"email":    req.Email,
			"roles":    []map[string]string{{"id": "role-1", "name": "Developer"}},
		},
		"token":  token,
		"device": "device-1",
		"organizations": []map[string]string{
			{"id": "org-1", "name": "My Organization", "slug": "my-org"},
		},
	})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	email, ok := s.bearerEmail(r)
	_ = email
	if ok {
		s.mu.Lock()
		delete(s.knownToks, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		s.mu.Unlock()
	}
	writeData(w, http.StatusOK, map[string]any{})
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	if !requireAuth(s, w, r) {
		return
	}
	writeData(w, http.StatusOK, map[string]string{"token": "tok-refreshed"})
}

func (s *Server) challenge(w http.ResponseWriter, r *http.Request) {
	email, ok := s.bearerEmail(r)
	if !ok {
		writeError(w, 401, "Invalid session")
		return
	}
	if !strings.Contains(email, "mfa") {
		writeData(w, http.StatusOK, map[string]any{"mfaRequired": false, "factors": []any{}})
		return
	}
	writeData(w, http.StatusOK, map[string]any{
		"mfaRequired": true,
		"challenge":   "challenge-123",
		"factors": []map[string]any{
			{"id": "f-totp", "type": "totp", "label": "Authenticator app", "enabled": true},
			{"id": "f-recovery", "type": "recovery", "label": "Recovery codes", "enabled": true},
		},
	})
}

func (s *Server) verifyTOTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Code != "123456" {
		writeError(w, 401, "Invalid TOTP code")
		return
	}
	writeData(w, http.StatusOK, map[string]any{"mfaVerified": true, "mfaToken": "mfa-totp-ok"})
}

func (s *Server) verifyRecovery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Code != "1111-2222" {
		writeError(w, 401, "Invalid recovery code")
		return
	}
	writeData(w, http.StatusOK, map[string]any{"mfaVerified": true, "mfaToken": "mfa-recovery-ok"})
}

// --- OAuth2 token endpoint (raw OAuth JSON, not the Raiton envelope) ---

func (s *Server) oauthToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "Method not allowed")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, 400, "invalid form body")
		return
	}
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		if r.PostForm.Get("code") == "" || r.PostForm.Get("code_verifier") == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_request","error_description":"code and code_verifier required"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"oauth-tok","token_type":"Bearer","expires_in":3600,"refresh_token":"oauth-refresh","scope":"openid profile email"}`))
	case "refresh_token":
		if r.PostForm.Get("refresh_token") == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_request","error_description":"refresh_token required"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"oauth-tok","token_type":"Bearer","expires_in":3600,"refresh_token":"oauth-refresh-rotated","scope":"openid profile email"}`))
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"unsupported_grant_type"}`))
	}
}

// --- developer store ---

func (s *Server) storeModules(w http.ResponseWriter, r *http.Request) {
	if !requireAuth(s, w, r) {
		return
	}
	p := r.URL.Path[len("/api/developer-store/modules"):]
	if p == "" {
		s.handleModulesRoot(w, r)
		return
	}
	// /:id, /:id/versions, /:id/versions/:vid/artifact/upload, /:id/<lifecycle>
	parts := strings.Split(strings.Trim(p, "/"), "/")
	id := parts[0]
	switch {
	case len(parts) == 1:
		s.handleModule(w, r, id)
	case len(parts) == 2 && parts[1] == "versions":
		s.handleVersions(w, r, id)
	case len(parts) == 5 && parts[1] == "versions" && parts[3] == "artifact" && parts[4] == "upload":
		s.handleArtifact(w, r, id, parts[2])
	case len(parts) >= 2 && isLifecyclePath(parts[1]):
		s.handleLifecycle(w, r, parts)
	default:
		writeError(w, 404, "Unknown route")
	}
}

// isLifecyclePath reports whether the path segment heads a module-lifecycle
// resource served by liorian-api-connect (spec module-lifecycle).
func isLifecyclePath(segment string) bool {
	switch segment {
	case "knowledge", "workflows", "channels", "platforms", "requirements",
		"dev-builds", "fingerprints", "caches", "observer", "usage":
		return true
	default:
		return false
	}
}

// handleLifecycle serves the module-lifecycle read/write routes.
func (s *Server) handleLifecycle(w http.ResponseWriter, r *http.Request, parts []string) {
	resource := parts[1]
	switch resource {
	case "knowledge":
		s.knowledge(w, r, parts)
	case "workflows":
		s.workflows(w, r, parts)
	case "channels":
		s.channels(w, r, parts)
	case "platforms":
		s.platforms(w, r, parts)
	case "requirements":
		s.requirements(w, r, parts)
	case "dev-builds":
		s.devBuilds(w, r, parts)
	case "fingerprints":
		s.fingerprints(w, r, parts)
	case "caches":
		s.caches(w, r, parts)
	case "observer":
		writeData(w, http.StatusOK, map[string]any{
			"productId": parts[0],
			"metrics": []map[string]any{
				{"id": "errors", "label": "Erreurs (7 jours)", "value": "1", "trend": "stable", "hint": "1 crash / 1 k requêtes"},
			},
			"crashes": []any{},
		})
	case "usage":
		writeData(w, http.StatusOK, map[string]any{
			"productId": parts[0],
			"series":    []map[string]any{{"label": "S28", "installations": 3, "activations": 2}},
			"totals":    map[string]int{"installations": 3, "activations": 2, "activeOrganizations": 1},
		})
	default:
		writeError(w, 404, "Unknown lifecycle resource")
	}
}

func (s *Server) knowledge(w http.ResponseWriter, r *http.Request, parts []string) {
	// parts: [moduleId, knowledge] or [moduleId, knowledge, articleId]
	if len(parts) == 2 {
		switch r.Method {
		case http.MethodGet:
			writeData(w, http.StatusOK, []map[string]any{
				{"id": "art-1", "title": "Démarrage", "slug": "demarrage", "kind": "GUIDE", "status": "PUBLISHED", "readingMinutes": 4},
			})
		case http.MethodPost:
			var req struct {
				Title          string `json:"title"`
				Slug           string `json:"slug"`
				Kind           string `json:"kind"`
				Summary        string `json:"summary"`
				ReadingMinutes int    `json:"readingMinutes"`
				Publish        bool   `json:"publish"`
			}
			if !decodeBody(w, r, &req) {
				return
			}
			status := "DRAFT"
			if req.Publish {
				status = "PUBLISHED"
			}
			writeData(w, http.StatusCreated, map[string]any{
				"id": "art-2", "title": req.Title, "slug": req.Slug, "kind": req.Kind,
				"summary": req.Summary, "readingMinutes": req.ReadingMinutes, "status": status,
			})
		default:
			writeError(w, 405, "Method not allowed")
		}
		return
	}
	// len(parts) == 3
	articleID := parts[2]
	switch r.Method {
	case http.MethodPut:
		writeData(w, http.StatusOK, map[string]any{
			"id": articleID, "title": "Démarrage", "slug": "demarrage", "kind": "GUIDE",
			"status": "PUBLISHED", "readingMinutes": 4,
		})
	case http.MethodDelete:
		writeData(w, http.StatusOK, nil)
	default:
		writeError(w, 405, "Method not allowed")
	}
}

func (s *Server) workflows(w http.ResponseWriter, r *http.Request, parts []string) {
	// parts: [moduleId, workflows] / [moduleId, workflows, id] / [moduleId, workflows, id, run]
	if len(parts) == 2 {
		switch r.Method {
		case http.MethodGet:
			writeData(w, http.StatusOK, []map[string]any{
				{"id": "wf-1", "name": "CI", "source": "acme/app", "trigger": "push", "status": "IDLE", "runCount": 2},
			})
		case http.MethodPost:
			var req struct {
				Name    string `json:"name"`
				Source  string `json:"source"`
				Trigger string `json:"trigger"`
				Status  string `json:"status"`
			}
			if !decodeBody(w, r, &req) {
				return
			}
			if req.Status == "" {
				req.Status = "IDLE"
			}
			writeData(w, http.StatusCreated, map[string]any{
				"id": "wf-2", "name": req.Name, "source": req.Source, "trigger": req.Trigger, "status": req.Status, "runCount": 0,
			})
		default:
			writeError(w, 405, "Method not allowed")
		}
		return
	}
	workflowID := parts[2]
	switch {
	case len(parts) == 4 && parts[3] == "run" && r.Method == http.MethodPost:
		writeData(w, http.StatusOK, map[string]any{
			"id": workflowID, "name": "CI", "source": "acme/app", "trigger": "push", "status": "RUNNING", "runCount": 3,
		})
	case len(parts) == 3 && r.Method == http.MethodPut:
		var req struct {
			Name    string `json:"name"`
			Source  string `json:"source"`
			Trigger string `json:"trigger"`
			Status  string `json:"status"`
		}
		if !decodeBody(w, r, &req) {
			return
		}
		writeData(w, http.StatusOK, map[string]any{
			"id": workflowID, "name": req.Name, "source": req.Source, "trigger": req.Trigger, "status": req.Status, "runCount": 0,
		})
	case len(parts) == 3 && r.Method == http.MethodDelete:
		writeData(w, http.StatusOK, nil)
	default:
		writeError(w, 405, "Method not allowed")
	}
}

func (s *Server) channels(w http.ResponseWriter, r *http.Request, parts []string) {
	// parts: [moduleId, channels] / [moduleId, channels, channel] / [moduleId, channels, channel, action]
	if len(parts) == 2 {
		writeData(w, http.StatusOK, []map[string]any{
			{"id": "ch-release", "name": "RELEASE", "versionString": "1.0.0", "status": "ACTIVE", "updateCount": 1, "rollbackAllowed": true},
		})
		return
	}
	channel := parts[2]
	if len(parts) == 4 && r.Method == http.MethodPost {
		switch parts[3] {
		case "publish":
			writeData(w, http.StatusOK, map[string]any{
				"id": "ch-" + strings.ToLower(channel), "name": channel, "versionString": "1.1.0", "status": "ACTIVE", "updateCount": 2, "rollbackAllowed": true,
			})
		case "rollback", "pause":
			writeData(w, http.StatusOK, map[string]any{
				"id": "ch-" + strings.ToLower(channel), "name": channel, "versionString": "1.0.0", "status": "PAUSED", "updateCount": 1, "rollbackAllowed": false,
			})
		default:
			writeError(w, 404, "Unknown channel action")
		}
		return
	}
	writeError(w, 405, "Method not allowed")
}

func (s *Server) platforms(w http.ResponseWriter, r *http.Request, parts []string) {
	if len(parts) != 2 {
		writeError(w, 404, "Unknown route")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeData(w, http.StatusOK, []map[string]any{
			{"platform": "WEB", "supported": false, "modes": []string{}, "os": []string{}},
			{"platform": "DESKTOP", "supported": false, "modes": []string{}, "os": []string{}},
			{"platform": "MOBILE", "supported": false, "modes": []string{}, "os": []string{}},
		})
	case http.MethodPut:
		var req struct {
			Platforms []map[string]any `json:"platforms"`
		}
		if !decodeBody(w, r, &req) {
			return
		}
		writeData(w, http.StatusOK, req.Platforms)
	default:
		writeError(w, 405, "Method not allowed")
	}
}

func (s *Server) requirements(w http.ResponseWriter, r *http.Request, parts []string) {
	if len(parts) == 2 {
		switch r.Method {
		case http.MethodGet:
			writeData(w, http.StatusOK, []map[string]any{
				{"id": "req-1", "moduleId": "core.crm", "name": "CRM", "versionRange": ">=1.0.0", "kind": "OPTIONAL"},
			})
		case http.MethodPost:
			var req struct {
				ModuleID     string `json:"moduleId"`
				Name         string `json:"name"`
				VersionRange string `json:"versionRange"`
				Kind         string `json:"kind"`
			}
			if !decodeBody(w, r, &req) {
				return
			}
			writeData(w, http.StatusCreated, map[string]any{
				"id": "req-2", "moduleId": req.ModuleID, "name": req.Name, "versionRange": req.VersionRange, "kind": req.Kind,
			})
		default:
			writeError(w, 405, "Method not allowed")
		}
		return
	}
	if len(parts) == 3 && r.Method == http.MethodDelete {
		writeData(w, http.StatusOK, nil)
		return
	}
	writeError(w, 405, "Method not allowed")
}

func (s *Server) devBuilds(w http.ResponseWriter, r *http.Request, parts []string) {
	if len(parts) == 2 && r.Method == http.MethodGet {
		writeData(w, http.StatusOK, []map[string]any{
			{"id": "db-1", "runtimeVersion": "1.0.0", "platform": "BUN", "artifactState": "READY", "canInstall": true},
		})
		return
	}
	if len(parts) == 2 && r.Method == http.MethodPost {
		var req struct {
			RuntimeVersion string `json:"runtimeVersion"`
			Platform       string `json:"platform"`
			ArtifactState  string `json:"artifactState"`
		}
		if !decodeBody(w, r, &req) {
			return
		}
		if req.ArtifactState == "" {
			req.ArtifactState = "PREPARING"
		}
		writeData(w, http.StatusCreated, map[string]any{
			"id": "db-2", "runtimeVersion": req.RuntimeVersion, "platform": req.Platform,
			"artifactState": req.ArtifactState, "canInstall": req.ArtifactState == "READY",
		})
		return
	}
	if len(parts) == 3 && r.Method == http.MethodDelete {
		writeData(w, http.StatusOK, nil)
		return
	}
	writeError(w, 405, "Method not allowed")
}

func (s *Server) fingerprints(w http.ResponseWriter, r *http.Request, parts []string) {
	if len(parts) == 2 && r.Method == http.MethodGet {
		writeData(w, http.StatusOK, []map[string]any{
			{"id": "fp-1", "hash": "abc123", "runtimeVersions": []string{"1.0.0"}, "channels": []string{"RELEASE"}},
		})
		return
	}
	if len(parts) == 2 && r.Method == http.MethodPost {
		var req struct {
			Hash            string   `json:"hash"`
			RuntimeVersions []string `json:"runtimeVersions"`
			Channels        []string `json:"channels"`
		}
		if !decodeBody(w, r, &req) {
			return
		}
		writeData(w, http.StatusCreated, map[string]any{
			"id": "fp-2", "hash": req.Hash, "runtimeVersions": req.RuntimeVersions, "channels": req.Channels,
		})
		return
	}
	if len(parts) == 3 && r.Method == http.MethodDelete {
		writeData(w, http.StatusOK, nil)
		return
	}
	writeError(w, 405, "Method not allowed")
}

func (s *Server) caches(w http.ResponseWriter, r *http.Request, parts []string) {
	if len(parts) == 2 && r.Method == http.MethodGet {
		writeData(w, http.StatusOK, []map[string]any{
			{"id": "cache-1", "key": "deps", "sizeBytes": 2048, "ttlSeconds": 3600, "hits": 12},
		})
		return
	}
	if r.Method == http.MethodDelete {
		writeData(w, http.StatusOK, nil)
		return
	}
	writeError(w, 405, "Method not allowed")
}

func (s *Server) signingKeys(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/developer-store/signing-keys")
	rest = strings.Trim(rest, "/")
	if rest == "" {
		switch r.Method {
		case http.MethodGet:
			writeData(w, http.StatusOK, []map[string]any{
				{"id": "k1", "keyId": "sign_abc", "algorithm": "Ed25519", "status": "ACTIVE"},
			})
		case http.MethodPost:
			writeData(w, http.StatusCreated, map[string]any{
				"id": "k3", "keyId": "sign_new", "algorithm": "Ed25519", "status": "ACTIVE",
			})
		default:
			writeError(w, 405, "Method not allowed")
		}
		return
	}
	parts := strings.Split(rest, "/")
	if len(parts) == 2 && parts[1] == "rotate" && r.Method == http.MethodPost {
		writeData(w, http.StatusOK, map[string]any{
			"id": "k2", "keyId": "sign_def", "algorithm": "Ed25519", "status": "ACTIVE",
		})
		return
	}
	if len(parts) == 1 && r.Method == http.MethodDelete {
		writeData(w, http.StatusOK, nil)
		return
	}
	writeError(w, 404, "Unknown route")
}

func (s *Server) accreditations(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/developer-store/accreditations")
	rest = strings.Trim(rest, "/")
	if rest == "" {
		switch r.Method {
		case http.MethodGet:
			writeData(w, http.StatusOK, []map[string]any{
				{"id": "acc-1", "kind": "CI_CD_TOKEN", "name": "GitHub Actions", "status": "LINKED"},
			})
		case http.MethodPost:
			var req struct {
				Kind string `json:"kind"`
				Name string `json:"name"`
			}
			if !decodeBody(w, r, &req) {
				return
			}
			writeData(w, http.StatusCreated, map[string]any{
				"id": "acc-2", "kind": req.Kind, "name": req.Name, "status": "PENDING",
			})
		default:
			writeError(w, 405, "Method not allowed")
		}
		return
	}
	if r.Method == http.MethodDelete {
		writeData(w, http.StatusOK, nil)
		return
	}
	writeError(w, 405, "Method not allowed")
}

func (s *Server) environmentVariables(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/developer-store/environment-variables")
	rest = strings.Trim(rest, "/")
	if rest == "" {
		switch r.Method {
		case http.MethodGet:
			s.mu.Lock()
			out := make([]*envVar, 0, len(s.envVars))
			for _, v := range s.envVars {
				out = append(out, v)
			}
			s.mu.Unlock()
			writeData(w, http.StatusOK, out)
		case http.MethodPost:
			var req struct {
				Key          string   `json:"key"`
				Value        string   `json:"value"`
				Environments []string `json:"environments"`
				Visibility   string   `json:"visibility"`
				ProductID    string   `json:"productId"`
			}
			if !decodeBody(w, r, &req) {
				return
			}
			s.mu.Lock()
			s.envVarID++
			id := "var-" + strconv.Itoa(s.envVarID)
			v := &envVar{ID: id, Key: req.Key, Environments: req.Environments, Visibility: req.Visibility, ProductID: req.ProductID}
			s.envVars[id] = v
			s.mu.Unlock()
			writeData(w, http.StatusCreated, v)
		default:
			writeError(w, 405, "Method not allowed")
		}
		return
	}
	variableID := rest
	s.mu.Lock()
	current, ok := s.envVars[variableID]
	s.mu.Unlock()
	if !ok {
		writeError(w, 404, "Variable not found")
		return
	}
	switch r.Method {
	case http.MethodPut:
		var req struct {
			Key          string   `json:"key"`
			Environments []string `json:"environments"`
			Visibility   string   `json:"visibility"`
		}
		if !decodeBody(w, r, &req) {
			return
		}
		current.Key = orDefault(req.Key, current.Key)
		if len(req.Environments) > 0 {
			current.Environments = req.Environments
		}
		current.Visibility = orDefault(req.Visibility, current.Visibility)
		writeData(w, http.StatusOK, current)
	case http.MethodDelete:
		s.mu.Lock()
		delete(s.envVars, variableID)
		s.mu.Unlock()
		writeData(w, http.StatusOK, nil)
	default:
		writeError(w, 405, "Method not allowed")
	}
}

func (s *Server) github(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/developer-store/github")
	rest = strings.Trim(rest, "/")
	switch {
	case rest == "" && r.Method == http.MethodGet:
		writeData(w, http.StatusOK, map[string]any{
			"id": "gh-1", "repository": "acme/app", "defaultBranch": "main", "workflowPath": ".github/workflows/ci.yml", "connected": true,
		})
	case rest == "" && r.Method == http.MethodDelete:
		writeData(w, http.StatusOK, nil)
	case rest == "connect" && r.Method == http.MethodPost:
		var req struct {
			Repository    string `json:"repository"`
			DefaultBranch string `json:"defaultBranch"`
			WorkflowPath  string `json:"workflowPath"`
			ProductID     string `json:"productId"`
		}
		if !decodeBody(w, r, &req) {
			return
		}
		writeData(w, http.StatusOK, map[string]any{
			"id": "gh-2", "repository": req.Repository, "defaultBranch": orDefault(req.DefaultBranch, "main"),
			"workflowPath": orDefault(req.WorkflowPath, ".github/workflows/ci.yml"), "connected": true,
		})
	default:
		writeError(w, 404, "Unknown route")
	}
}

func (s *Server) handleModulesRoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		out := make([]*Product, 0, len(s.products))
		for _, p := range s.products {
			out = append(out, p)
		}
		s.mu.Unlock()
		writeData(w, http.StatusOK, out)
	case http.MethodPost:
		var req struct {
			Name              string `json:"name"`
			Slug              string `json:"slug"`
			Type              string `json:"type"`
			Description       string `json:"description"`
			Icon              string `json:"icon"`
			PrimaryCategory   string `json:"primaryCategory"`
			SecondaryCategory string `json:"secondaryCategory"`
			Token             string `json:"token"`
		}
		if !decodeBody(w, r, &req) {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		// A reused manifest token resolves to the existing product (idempotent
		// publish, spec connect §2.1).
		if token := strings.TrimSpace(req.Token); token != "" {
			if existingID, ok := s.tokens[token]; ok {
				writeData(w, http.StatusOK, s.products[existingID])
				return
			}
		}
		// Products carry UUID ids (like the real store); the manifest token is
		// synchronized to this id after a successful publish unless provided.
		id := uuid.NewString()
		token := strings.TrimSpace(req.Token)
		if token == "" {
			token = id
		}
		s.products[id] = &Product{
			ID: id, Name: req.Name, Slug: req.Slug, Type: req.Type,
			Description: req.Description, Icon: req.Icon,
			PrimaryCategory: req.PrimaryCategory, SecondaryCategory: req.SecondaryCategory,
			Token: token,
		}
		s.tokens[token] = id
		writeData(w, http.StatusCreated, s.products[id])
	default:
		writeError(w, 405, "Method not allowed")
	}
}

// lookupProduct resolves a product by its id or by its manifest token. The
// caller must hold s.mu.
func (s *Server) lookupProduct(id string) (*Product, bool) {
	if p, ok := s.products[id]; ok {
		return p, true
	}
	if pid, ok := s.tokens[id]; ok {
		if p, ok := s.products[pid]; ok {
			return p, true
		}
	}
	return nil, false
}

func (s *Server) handleModule(w http.ResponseWriter, r *http.Request, id string) {
	s.mu.Lock()
	prod, ok := s.lookupProduct(id)
	s.mu.Unlock()
	if !ok {
		writeError(w, 404, "Module not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeData(w, http.StatusOK, prod)
	case http.MethodPut:
		var req struct {
			Name              string `json:"name"`
			Type              string `json:"type"`
			Description       string `json:"description"`
			Icon              string `json:"icon"`
			PrimaryCategory   string `json:"primaryCategory"`
			SecondaryCategory string `json:"secondaryCategory"`
		}
		if !decodeBody(w, r, &req) {
			return
		}
		s.mu.Lock()
		prod.Name = orDefault(req.Name, prod.Name)
		prod.Type = orDefault(req.Type, prod.Type)
		prod.Description = orDefault(req.Description, prod.Description)
		prod.Icon = orDefault(req.Icon, prod.Icon)
		prod.PrimaryCategory = orDefault(req.PrimaryCategory, prod.PrimaryCategory)
		prod.SecondaryCategory = orDefault(req.SecondaryCategory, prod.SecondaryCategory)
		s.mu.Unlock()
		writeData(w, http.StatusOK, prod)
	default:
		writeError(w, 405, "Method not allowed")
	}
}

func (s *Server) handleVersions(w http.ResponseWriter, r *http.Request, id string) {
	s.mu.Lock()
	_, ok := s.lookupProduct(id)
	s.mu.Unlock()
	if !ok {
		writeError(w, 404, "Module not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		out := s.versions[id]
		s.mu.Unlock()
		if out == nil {
			out = []Version{}
		}
		writeData(w, http.StatusOK, out)
	case http.MethodPost:
		var req struct {
			VersionString string `json:"versionString"`
			BuildNumber   int    `json:"buildNumber"`
		}
		if !decodeBody(w, r, &req) {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.nextID++
		vs := s.versions[id]
		for _, v := range vs {
			if v.VersionString == req.VersionString {
				writeError(w, http.StatusConflict, "An identical version already exists for this module")
				return
			}
		}
		build := req.BuildNumber
		if build == 0 {
			build = len(vs) + 1
		}
		nv := Version{
			ID:            id + "-v" + strconv.Itoa(len(vs)+1),
			ModuleProduct: id,
			VersionString: req.VersionString,
			BuildNumber:   build,
			Status:        "PUBLISHED",
		}
		s.versions[id] = append(vs, nv)
		writeData(w, http.StatusCreated, nv)
	default:
		writeError(w, 405, "Method not allowed")
	}
}

func (s *Server) handleArtifact(w http.ResponseWriter, r *http.Request, productID, versionID string) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "Method not allowed")
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid multipart artifact")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Archive missing")
		return
	}
	defer file.Close()
	archive, err := io.ReadAll(file)
	if err != nil || len(archive) == 0 {
		writeError(w, http.StatusBadRequest, "Archive empty")
		return
	}
	checksum := r.FormValue("checksum")
	if checksum == "" {
		writeError(w, http.StatusBadRequest, "Checksum missing")
		return
	}
	s.mu.Lock()
	s.created[productID]++
	slug := s.products[productID].Slug
	s.mu.Unlock()
	writeData(w, http.StatusCreated, map[string]any{
		"url":       "https://store.liorian.dev/modules/" + slug,
		"key":       "artifacts/" + productID + "/" + versionID + ".liozip",
		"checksum":  checksum,
		"signature": r.FormValue("signature"),
		"sizeBytes": len(archive),
	})
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// --- public catalog (marketplace) ---

// CatalogModule mirrors the storefront entry served by `/api/catalog/*`
// (the consumer side of the developer-store Product/Version entities).
type CatalogModule struct {
	ID                string `json:"id"`
	Slug              string `json:"slug"`
	Type              string `json:"type,omitempty"`
	Domain            string `json:"domain,omitempty"`
	Name              string `json:"name"`
	Description       string `json:"description,omitempty"`
	Icon              string `json:"icon,omitempty"`
	PrimaryCategory   string `json:"primaryCategory,omitempty"`
	SecondaryCategory string `json:"secondaryCategory,omitempty"`
	Publisher         struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"publisher"`
	Version            string `json:"version"`
	ArtifactURL        string `json:"artifactUrl,omitempty"`
	ArtifactChecksum   string `json:"artifactChecksum,omitempty"`
	Signature          string `json:"signature,omitempty"`
	SignaturePublicKey string `json:"signaturePublicKey,omitempty"`
	SizeBytes          int64  `json:"sizeBytes,omitempty"`
	Installs           int64  `json:"installs,omitempty"`
	PublishedAt        string `json:"publishedAt,omitempty"`
}

// seedCatalog populates the storefront with deterministic modules: one
// Ed25519-signed, one unsigned, and one whose catalog checksum does NOT match
// its artifact (to exercise the checksum guard without breaking the others).
func (s *Server) seedCatalog() {
	s.seedModule("com.example.blog-manager", "blog-manager", "Blog Manager",
		"Manage the blog editorial workflow", "COMMUNICATION", "1.2.0", 1290, true)
	s.seedModule("com.analytics.visitors", "visitors", "Visitor Analytics",
		"Track and report storefront visitors", "DATA", "0.4.1", 512, false)
	s.seedModule("com.example.corrupted", "corrupted", "Corrupted Module",
		"Artifact whose catalog checksum is wrong", "SYSTEM", "0.1.0", 7, false)
	for i := range s.catalog {
		if s.catalog[i].Slug == "com.example.corrupted" {
			s.catalog[i].ArtifactChecksum = strings.Repeat("0", 64)
		}
	}
}

func (s *Server) seedModule(slug, page, name, desc, category, version string, installs int64, signed bool) {
	data := buildModuleArchive(slug, page, version)
	entry := CatalogModule{
		ID: "cat_" + slug, Slug: slug, Type: "WEB_APP_LOCAL", Domain: slug,
		Name: name, Description: desc, Icon: "PuzzleIcon",
		PrimaryCategory: category, SecondaryCategory: "OPERATIONS",
		Publisher: func() struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} {
			var p struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			p.ID = "pub-protorians"
			p.Name = "protorians"
			return p
		}(),
		Version:          version,
		ArtifactChecksum: sha256HexBytes(data),
		SizeBytes:        int64(len(data)),
		Installs:         installs,
		PublishedAt:      "2026-01-15T10:00:00Z",
	}
	if signed {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			panic("mock api: failed to generate signing key: " + err.Error())
		}
		entry.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data))
		entry.SignaturePublicKey = base64.StdEncoding.EncodeToString(pub)
	}
	s.catalog = append(s.catalog, entry)
	s.catalogArtifacts[slug] = data
}

func sha256HexBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// buildModuleArchive packs a conformant module (canonical manifest at the
// root + module tree + page + assets) into a `.liozip` ZIP, mirroring
// `internal/module/packer.go` (spec TECH-002).
func buildModuleArchive(name, page, version string) []byte {
	manifest := map[string]any{
		"schemaVersion": 1,
		"id":            name,
		"domain":        name,
		"key":           "MARKETPLACE_DEMO",
		"name":          "Marketplace Demo",
		"description":   "A module distributed through the public catalog",
		"version":       version,
		"icon":          "PuzzleIcon",
		"type":          "WEB_APP_LOCAL",
		"external":      true,
		"entry":         "index.tsx",
		"uri":           "/" + page,
		"category":      "SYSTEM",
		"token":         uuid.NewString(),
		"platforms": map[string]any{
			"web": map[string]any{"supported": true, "modes": []string{"web"}},
		},
		"compatibility": map[string]any{
			"socle": map[string]any{"min": "0.17.1", "max": "0.17.x"},
			"api":   map[string]any{"min": "0.27.0", "max": "0.27.x"},
		},
		"permissions":          []string{"User:Get"},
		"oauth":                map[string]any{"scopes": []string{"openid"}},
		"optionalRequirements": map[string]string{},
		"requirements":         map[string]any{},
		"capabilities":         []string{"core:default"},
	}
	raw, _ := json.MarshalIndent(manifest, "", "  ")

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entries := map[string]string{
		"manifest.json": string(raw) + "\n",
		"library/modules/" + name + "/manifest.json": string(raw) + "\n",
		"library/modules/" + name + "/index.tsx":     "export default function Demo() {\n  return <div>Demo</div>;\n}\n",
		"src/app/" + page + "/page.tsx":              "export default function Page() { return <div>Page</div>; }\n",
		"public/assets/" + name + "/README.txt":      "hello from the catalog\n",
	}
	// deterministic order keeps archives stable across runs
	for _, rel := range []string{
		"manifest.json",
		"library/modules/" + name + "/manifest.json",
		"library/modules/" + name + "/index.tsx",
		"src/app/" + page + "/page.tsx",
		"public/assets/" + name + "/README.txt",
	} {
		fw, err := zw.Create(rel)
		if err != nil {
			panic("mock api: " + err.Error())
		}
		if _, err := fw.Write([]byte(entries[rel])); err != nil {
			panic("mock api: " + err.Error())
		}
	}
	if err := zw.Close(); err != nil {
		panic("mock api: " + err.Error())
	}
	return buf.Bytes()
}

// withArtifactURL fills the absolute storefront artifact URL for a request.
func (m CatalogModule) withArtifactURL(r *http.Request) CatalogModule {
	m.ArtifactURL = "http://" + r.Host + "/catalog/artifacts/" + m.Slug + ".liozip"
	return m
}

func (s *Server) catalogSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(r.URL.Query().Get("q"))
	cat := strings.ToUpper(r.URL.Query().Get("category"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	s.mu.Lock()
	defer s.mu.Unlock()
	items := []CatalogModule{}
	total := 0
	for _, m := range s.catalog {
		if q != "" && !strings.Contains(strings.ToLower(m.Name+" "+m.Slug+" "+m.Description), q) {
			continue
		}
		if cat != "" && !strings.EqualFold(cat, m.PrimaryCategory) && !strings.EqualFold(cat, m.SecondaryCategory) {
			continue
		}
		total++
		if total <= offset || len(items) >= limit {
			continue
		}
		items = append(items, m.withArtifactURL(r))
	}
	writeData(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "offset": offset, "limit": limit,
	})
}

func (s *Server) catalogGet(w http.ResponseWriter, r *http.Request) {
	ref := strings.TrimPrefix(r.URL.Path, "/api/catalog/modules/")
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.catalog {
		if m.ID == ref || m.Slug == ref || m.Domain == ref {
			writeData(w, http.StatusOK, m.withArtifactURL(r))
			return
		}
	}
	writeError(w, http.StatusNotFound, "Module not found")
}

func (s *Server) catalogArtifact(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimPrefix(r.URL.Path, "/catalog/artifacts/")
	slug = strings.TrimSuffix(slug, ".liozip")
	slug = strings.TrimSuffix(slug, ".SenMod")
	s.mu.Lock()
	data, ok := s.catalogArtifacts[slug]
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "Artifact not found")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(data)
}
