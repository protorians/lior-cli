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
		"url":       "https://store.liorian.dev/modules/" + slug,
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
		ID: "cat_" + slug, Slug: slug, Type: "EXTERNAL", Domain: slug,
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

// buildModuleArchive packs a conformant module (manifest + entry + page +
// assets) into a `.SenMod` ZIP, mirroring `internal/module/packer.go`.
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
		"type":          "EXTERNAL",
		"entry":         "index.tsx",
		"uri":           "/" + page,
		"category":      "SYSTEM",
		"token":         uuid.NewString(),
		"publisher":     map[string]any{"id": "pub-protorians", "name": "protorians"},
		"platforms": map[string]any{
			"web": map[string]any{"supported": true, "modes": []string{"web"}},
		},
		"managerCompatibility": map[string]any{"min": "0.17.1", "max": "0.17.x"},
		"apiCompatibility":     map[string]any{"min": "0.27.0", "max": "0.27.x"},
		"permissions":          []string{},
		"optionalRequirements": map[string]string{},
		"requirements":         map[string]any{},
		"capabilities":         map[string]any{"needsNetwork": true},
	}
	raw, _ := json.MarshalIndent(manifest, "", "  ")

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entries := map[string]string{
		"library/modules/" + name + "/manifest.json": string(raw) + "\n",
		"library/modules/" + name + "/index.tsx":     "export default function Demo() {\n  return <div>Demo</div>;\n}\n",
		"src/app/" + page + "/page.tsx":              "export default function Page() { return <div>Page</div>; }\n",
		"public/assets/" + name + "/README.txt":      "hello from the catalog\n",
	}
	// deterministic order keeps archives stable across runs
	for _, rel := range []string{
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
	m.ArtifactURL = "http://" + r.Host + "/catalog/artifacts/" + m.Slug + ".SenMod"
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
	slug := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/catalog/artifacts/"), ".SenMod")
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
