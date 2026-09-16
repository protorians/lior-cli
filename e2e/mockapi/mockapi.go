// Package mockapi is an in-memory HTTP server replicating the
// `sentient-connect` wire contract (Raiton envelope `{message, data,
// statusCode}`) for the E2E testscript suite: auth, guarded MFA and the
// developer-store pipeline (product → version → artifact).
package mockapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// Product mirrors the StoreModuleProduct entity.
type Product struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Slug            string `json:"slug"`
	Type            string `json:"type"`
	PrimaryCategory string `json:"primaryCategory"`
	IsDeprecated    bool   `json:"isDeprecated"`
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

// Server is the in-memory fake of the sentient-connect API.
type Server struct {
	mu        sync.Mutex
	products  map[string]*Product
	versions  map[string][]Version
	created   map[string]int
	nextID    int
	knownToks map[string]string // token -> email
}

// New builds a fresh mock server with no state.
func New() *Server {
	return &Server{
		products:  map[string]*Product{},
		versions:  map[string][]Version{},
		created:   map[string]int{},
		knownToks: map[string]string{},
	}
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

	mux.HandleFunc("/api/developer-store/modules/", s.storeModules)
	mux.HandleFunc("/api/developer-store/modules", s.storeModules)
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
	// /:id, /:id/versions, /:id/versions/:vid/artifact
	parts := strings.Split(strings.Trim(p, "/"), "/")
	id := parts[0]
	switch {
	case len(parts) == 1:
		s.handleModule(w, r, id)
	case len(parts) == 2 && parts[1] == "versions":
		s.handleVersions(w, r, id)
	case len(parts) == 4 && parts[1] == "versions" && parts[3] == "artifact":
		s.handleArtifact(w, r, id, parts[2])
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
			Name            string `json:"name"`
			Slug            string `json:"slug"`
			Type            string `json:"type"`
			PrimaryCategory string `json:"primaryCategory"`
		}
		if !decodeBody(w, r, &req) {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		// Products carry UUID ids (like the real store); the manifest token is
		// synchronized to this id after a successful publish.
		id := uuid.NewString()
		if _, exists := s.products[id]; exists {
			writeError(w, 409, "A module with this slug already exists")
			return
		}
		s.products[id] = &Product{
			ID: id, Name: req.Name, Slug: req.Slug,
			Type: req.Type, PrimaryCategory: req.PrimaryCategory,
		}
		writeData(w, http.StatusCreated, s.products[id])
	default:
		writeError(w, 405, "Method not allowed")
	}
}

func (s *Server) handleModule(w http.ResponseWriter, r *http.Request, id string) {
	s.mu.Lock()
	prod, ok := s.products[id]
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
			Name            string `json:"name"`
			Type            string `json:"type"`
			PrimaryCategory string `json:"primaryCategory"`
		}
		if !decodeBody(w, r, &req) {
			return
		}
		s.mu.Lock()
		prod.Name = orDefault(req.Name, prod.Name)
		prod.Type = orDefault(req.Type, prod.Type)
		prod.PrimaryCategory = orDefault(req.PrimaryCategory, prod.PrimaryCategory)
		s.mu.Unlock()
		writeData(w, http.StatusOK, prod)
	default:
		writeError(w, 405, "Method not allowed")
	}
}

func (s *Server) handleVersions(w http.ResponseWriter, r *http.Request, id string) {
	s.mu.Lock()
	if _, ok := s.products[id]; !ok {
		s.mu.Unlock()
		writeError(w, 404, "Module not found")
		return
	}
	s.mu.Unlock()

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
	var req struct {
		Checksum  string `json:"checksum"`
		Signature string `json:"signature"`
		SizeBytes int64  `json:"size"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	s.mu.Lock()
	s.created[productID]++
	slug := s.products[productID].Slug
	s.mu.Unlock()
	writeData(w, http.StatusCreated, map[string]any{
		"url":       "https://store.sentient.dev/modules/" + slug,
		"key":       "artifacts/" + productID + "/" + versionID + ".SenMod",
		"checksum":  req.Checksum,
		"signature": req.Signature,
		"sizeBytes": req.SizeBytes,
	})
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
