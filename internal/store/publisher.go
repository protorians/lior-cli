package store

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/protorians/lior-cli/internal/appconfig"
	"github.com/protorians/lior-cli/internal/auth"
	"github.com/protorians/lior-cli/internal/module"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/signing"
)

// Developer store API paths (Raiton envelope, `/api` prefix).
const modulesPath = "/api/developer-store/modules"

// accountsPath is the Developer Store account root, serving the developer's own
// editor account (`DeveloperAccountVm`).
const accountsPath = "/api/developer-store/accounts"

// EnvConnectAPI overrides the resolved Developer Store (liorian-connect) base URL.
const EnvConnectAPI = "LIORIAN_CONNECT_API"

// ErrNoStoreRoute reports that the resolved Developer Store base URL answered 404
// on the `/api/developer-store/*` routes: the URL points at a service that does
// not expose the developer store (typically `liorian-api-core` instead of
// `liorian-api-connect`), so the failure is a configuration problem, not a
// connectivity one.
var ErrNoStoreRoute = errors.New("the resolved Developer Store base URL does not serve " + modulesPath)

// DeveloperModuleType enum values exposed by the store.
const (
	ModuleTypeConfiguration  = "CONFIGURATION"
	ModuleTypeExternalURL    = "EXTERNAL_URL"
	ModuleTypeWebAppRemote   = "WEB_APP_REMOTE"
	ModuleTypeWebAppCached   = "WEB_APP_CACHED"
	ModuleTypeWebAppLocal    = "WEB_APP_LOCAL"
	ModuleTypeRemoteFrontend = "REMOTE_FRONTEND"
)

// defaultPrimaryCategory is the store category used for products whose
// manifest declares no `category`.
const defaultPrimaryCategory = "SYSTEM"

// Product is a StoreModuleProduct entity (`CreateModuleProductDto` payload).
type Product struct {
	ID                string  `json:"id"`
	AccountID         string  `json:"accountId"`
	ModuleStoreID     string  `json:"moduleStoreId,omitempty"`
	Name              string  `json:"name"`
	Slug              string  `json:"slug"`
	Type              string  `json:"type"`
	Description       string  `json:"description,omitempty"`
	Icon              string  `json:"icon,omitempty"`
	PrimaryCategory   string  `json:"primaryCategory"`
	SecondaryCategory string  `json:"secondaryCategory,omitempty"`
	Status            string  `json:"status,omitempty"`
	DeveloperSlug     string  `json:"developerSlug,omitempty"`
	DeveloperName     string  `json:"developerName,omitempty"`
	IsDeprecated      bool    `json:"isDeprecated"`
	RemovedAt         *string `json:"removedAt,omitempty"`
}

// Version is a ModuleVersion entity.
type Version struct {
	ID               string `json:"id"`
	ModuleProductID  string `json:"moduleProductId"`
	VersionString    string `json:"versionString"`
	BuildNumber      int    `json:"buildNumber"`
	Status           string `json:"status"`
	ArtifactURL      string `json:"artifactUrl,omitempty"`
	ArtifactChecksum string `json:"artifactChecksum,omitempty"`
	SizeBytes        int64  `json:"sizeBytes,omitempty"`
}

// Artifact is the `data` returned when an artifact is declared.
type Artifact struct {
	URL       string          `json:"url"`
	Key       string          `json:"key"`
	Checksum  string          `json:"checksum"`
	SizeBytes int64           `json:"sizeBytes"`
	Manifest  json.RawMessage `json:"manifest,omitempty"`
}

// RemoteModule is a module registered on the store, as surfaced by link/display.
type RemoteModule struct {
	Token       string `json:"token"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Status      string `json:"status"`
}

// DeveloperAccount is the developer's own store account (`DeveloperAccountVm`).
// `ID` is the Liorian account identifier the store scopes every resource to
// (`DeveloperProduct.developerId`) — never an Apple or third-party developer id.
type DeveloperAccount struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// RemoteModuleResponse is the detailed remote module returned by GetModule.
type RemoteModuleResponse struct {
	Token       string `json:"token"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Status      string `json:"status"`
	Publisher   struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"publisher"`
}

// PublishResponse is the result of a successful publish.
type PublishResponse struct {
	Token   string `json:"token"`
	Version string `json:"version"`
	URL     string `json:"url"`
}

// Client talks to the developer-store API.
type Client struct {
	// Connector authenticates against liorian-api-core (sign-in, refresh).
	Connector *auth.Connector
	// HTTP is the Developer Store (liorian-api-connect) client. When nil it
	// falls back to the connector client (legacy behaviour).
	HTTP *pkg.Client
}

// NewClient builds a store client with the current session token. The Developer
// Store lives on `liorian-api-connect` (spec §8.2): its base URL is resolved
// from `app.config.json` (`liorian-connect`) or `LIORIAN_CONNECT_API`, falling
// back to the auth connector when unconfigured.
func NewClient() *Client {
	connector := auth.NewConnector()
	client := &Client{Connector: connector}
	if base := connectBaseURL(); base != "" {
		client.HTTP = pkg.NewClientWithTimeout(base, connectTimeout())
		client.HTTP.Token = connector.Client.Token
	}
	return client
}

// connectBaseURL resolves the Developer Store base URL (env override, then the
// `liorian-connect` entry of the workspace registry).
func connectBaseURL() string {
	if v := strings.TrimSpace(os.Getenv(EnvConnectAPI)); v != "" {
		return v
	}
	base, _ := appconfig.Resolved("").BaseURL(appconfig.ConnectAppID)
	return strings.TrimSpace(base)
}

// connectTimeout resolves the Developer Store API timeout.
func connectTimeout() time.Duration {
	return appconfig.Resolved("").Timeout(appconfig.ConnectAppID)
}

// http returns the client used for Developer Store calls.
func (c *Client) http() *pkg.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return c.Connector.Client
}

// SetToken sets the bearer token for API calls.
func (c *Client) SetToken(token string) {
	c.Connector.Client.Token = token
	if c.HTTP != nil {
		c.HTTP.Token = token
	}
}

// WithAutoRefresh wires automatic 401-retry token refresh to the underlying
// HTTP client. When an API call returns HTTP 401, the session is refreshed
// via POST /api/auth/sessions/refresh and the request is retried once.
func (c *Client) WithAutoRefresh(sess *auth.Session) {
	refresh := func() (string, error) {
		if err := sess.Refresh(context.Background(), c.Connector); err != nil {
			return "", err
		}
		return sess.AccessToken, nil
	}
	c.Connector.Client.TokenRefreshFunc = refresh
	if c.HTTP != nil {
		c.HTTP.TokenRefreshFunc = refresh
	}
}

// ListModules returns the modules owned by the authenticated developer.
// The response may be a bare array or the paginated envelope `{items, total, …}`.
func (c *Client) ListModules(ctx context.Context) ([]RemoteModule, error) {
	var raw json.RawMessage
	if err := c.http().Do(ctx, "GET", modulesPath, nil, &raw); err != nil {
		return nil, fmt.Errorf("failed to fetch modules: %w", err)
	}
	var products []Product
	if err := json.Unmarshal(raw, &products); err == nil {
		modules := make([]RemoteModule, 0, len(products))
		for _, p := range products {
			mod := remoteFromProduct(p)
			mod.Version = c.latestVersion(ctx, p.ID)
			modules = append(modules, mod)
		}
		return modules, nil
	}
	var page struct {
		Items []Product `json:"items"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		return nil, fmt.Errorf("failed to fetch modules: %w", err)
	}
	modules := make([]RemoteModule, 0, len(page.Items))
	for _, p := range page.Items {
		mod := remoteFromProduct(p)
		mod.Version = c.latestVersion(ctx, p.ID)
		modules = append(modules, mod)
	}
	return modules, nil
}

// GetMyAccount returns the editor account behind the session token
// (`GET /api/developer-store/accounts/me`). The endpoint provisions the account
// on first access, so `ID` is always resolvable once authenticated. It is the
// canonical developer identifier to record in `manifest.publisher`.
func (c *Client) GetMyAccount(ctx context.Context) (*DeveloperAccount, error) {
	var out DeveloperAccount
	if err := c.http().Do(ctx, "GET", accountsPath+"/me", nil, &out); err != nil {
		return nil, fmt.Errorf("failed to fetch the developer account: %w", err)
	}
	if strings.TrimSpace(out.ID) == "" {
		return nil, errors.New("the developer account has no id")
	}
	return &out, nil
}

// GetModule returns a remote module by its id (product id).
func (c *Client) GetModule(ctx context.Context, id string) (*RemoteModuleResponse, error) {
	var product Product
	if err := c.http().Do(ctx, "GET", modulesPath+"/"+url.PathEscape(id), nil, &product); err != nil {
		return nil, fmt.Errorf("failed to fetch module %q: %w", id, err)
	}
	mod := remoteFromProduct(product)
	mod.Version = c.latestVersion(ctx, product.ID)
	response := &RemoteModuleResponse{
		Token:       mod.Token,
		Name:        mod.Name,
		Description: mod.Description,
		Version:     mod.Version,
		Status:      mod.Status,
	}
	response.Publisher.ID = product.AccountID
	response.Publisher.Name = product.DeveloperName
	return response, nil
}

// fetchVersions returns the published versions of a product (best-effort).
func (c *Client) fetchVersions(ctx context.Context, productID string) []Version {
	var raw json.RawMessage
	if err := c.http().Do(ctx, "GET", modulesPath+"/"+productID+"/versions", nil, &raw); err != nil {
		return nil
	}
	var versions []Version
	if err := json.Unmarshal(raw, &versions); err != nil {
		var page struct {
			Items []Version `json:"items"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil
		}
		versions = page.Items
	}
	return versions
}

// latestVersion returns the most recent published version string (best-effort).
func (c *Client) latestVersion(ctx context.Context, productID string) string {
	best := ""
	bestBuild := 0
	for _, v := range c.fetchVersions(ctx, productID) {
		if v.BuildNumber >= bestBuild {
			best = v.VersionString
			bestBuild = v.BuildNumber
		}
	}
	return best
}

// nextBuildNumber returns one more than the highest published build number of
// the product (spec connect §2.2: default = last build + 1). It defaults to 1
// when the versions cannot be resolved.
func (c *Client) nextBuildNumber(ctx context.Context, productID string) int {
	maxBuild := 0
	for _, v := range c.fetchVersions(ctx, productID) {
		if v.BuildNumber > maxBuild {
			maxBuild = v.BuildNumber
		}
	}
	return maxBuild + 1
}

// UpdateModule syncs a module's remote metadata via PUT /api/developer-store/modules/:id.
func (c *Client) UpdateModule(ctx context.Context, id string, m *module.Manifest) error {
	if err := c.http().Do(ctx, "PUT", modulesPath+"/"+url.PathEscape(id), updateProductRequest(m), nil); err != nil {
		return fmt.Errorf("failed to update module %q: %w", id, err)
	}
	return nil
}

// Publish registers the module product, creates its version and uploads the
// archive artifact (spec connect §21): product → version → S3-backed artifact.
func (c *Client) Publish(ctx context.Context, archivePath string, manifest *module.Manifest) (*PublishResponse, error) {
	// 1. Resolve (or create) the remote product behind the manifest token.
	productID, err := c.resolveProduct(ctx, manifest)
	if err != nil {
		return nil, err
	}

	// 2. Publish a new version for the product.
	version, err := c.createVersion(ctx, productID, manifest)
	if err != nil {
		return nil, err
	}

	// 3. Upload the archive binary. The API writes it to the configured storage
	// driver (S3 in production) before persisting its metadata.
	artifact, err := c.uploadArtifact(ctx, productID, version.ID, archivePath, manifest)
	if err != nil {
		return nil, err
	}

	result := &PublishResponse{Token: productID, Version: version.VersionString}
	if version.ArtifactURL != "" {
		result.URL = version.ArtifactURL
	} else if artifact != nil && artifact.URL != "" {
		result.URL = artifact.URL
	}
	return result, nil
}

func (c *Client) createProduct(ctx context.Context, m *module.Manifest) (*Product, error) {
	var out Product
	if err := c.http().Do(ctx, "POST", modulesPath, createProductRequest(m), &out); err != nil {
		// A 404 on the collection route is never a payload problem: the resolved
		// base URL does not expose the developer store at all. Say so, with the
		// URL that was used, instead of surfacing a bare "Not Found".
		if pkg.IsNotFound(err) {
			return nil, fmt.Errorf("%w (base URL: %s)", ErrNoStoreRoute, c.http().BaseURL)
		}
		return nil, fmt.Errorf("failed to create the remote module: %w", err)
	}
	return &out, nil
}

// resolveProduct returns the remote product id linked to the manifest token.
// A local token (freshly generated UUID) is not known by the API and yields a
// 404, in which case a new product is created. Linking therefore works without
// trusting the token format.
func (c *Client) resolveProduct(ctx context.Context, m *module.Manifest) (string, error) {
	id := strings.TrimSpace(m.Token)
	if id != "" {
		var product Product
		err := c.http().Do(ctx, "GET", modulesPath+"/"+url.PathEscape(id), nil, &product)
		if err == nil {
			return product.ID, nil
		}
		if !isNotFound(err) {
			return "", fmt.Errorf("failed to resolve the remote module: %w", err)
		}
	}
	product, err := c.createProduct(ctx, m)
	if err != nil {
		return "", err
	}
	return product.ID, nil
}

// isNotFound reports whether an error is an HTTP 404.
func isNotFound(err error) bool {
	apiErr, ok := err.(*pkg.APIError)
	return ok && apiErr.StatusCode == 404
}

func (c *Client) createVersion(ctx context.Context, productID string, m *module.Manifest) (*Version, error) {
	releaseNotes := json.RawMessage(`{}`)
	socle := m.EffectiveSocle()
	api := m.EffectiveAPI()
	body := createVersionRequest{
		VersionString:     m.Version,
		BuildNumber:       c.nextBuildNumber(ctx, productID),
		ReleaseNotes:      &releaseNotes,
		MinManager:        socle.Min,
		MaxManager:        socle.Max,
		MinAPI:            api.Min,
		MaxAPI:            api.Max,
		SupportedRuntimes: supportedRuntimes(m),
	}
	var out Version
	if err := c.http().Do(ctx, "POST", modulesPath+"/"+productID+"/versions", body, &out); err != nil {
		return nil, fmt.Errorf("failed to create the version: %w", err)
	}
	return &out, nil
}

func (c *Client) uploadArtifact(ctx context.Context, productID, versionID, archivePath string, m *module.Manifest) (*Artifact, error) {
	data, err := os.ReadFile(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read the archive: %w", err)
	}
	checksum := sha256.Sum256(data)
	signature, _ := artifactSignature(archivePath)
	manifestJSON, merr := json.Marshal(m)
	if merr != nil {
		return nil, fmt.Errorf("failed to serialize the manifest: %w", merr)
	}
	fields := map[string]string{
		"manifest":  string(manifestJSON),
		"checksum":  hex.EncodeToString(checksum[:]),
		"signature": signature,
	}
	var out Artifact
	if err := c.http().DoMultipart(ctx,
		modulesPath+"/"+productID+"/versions/"+versionID+"/artifact/upload",
		fields, "file", filepath.Base(archivePath), data, &out); err != nil {
		return nil, fmt.Errorf("failed to upload the artifact: %w", err)
	}
	return &out, nil
}

// artifactSignatureKeyID returns the fingerprint of the local developer
// public key: the `signatureKeyId` distributed with the artefact (spec §7.1,
// trousseau pinning). Empty when no signing key exists (unsigned publish).
func artifactSignatureKeyID() string {
	ks := signing.NewKeyStore()
	pub, err := ks.GetPublicKey()
	if err != nil || len(pub) == 0 {
		return ""
	}
	return signing.Fingerprint(pub)
}

// artifactSignature base64-encodes the `.liozip.sig` signature file when
// present (produced by `liora sign`). The signature stays empty when absent
// (unsigned publish — refused by the server per ADR-010 unless explicitly
// allowed with `--allow-unsigned`).
func artifactSignature(archivePath string) (string, error) {
	raw, err := os.ReadFile(archivePath + ".sig")
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// catalogIdentifierNamespace is the canonical catalogue identifier prefix
// (`mod.<publisher>.<module>`) shared with the server
// (`catalogIdentifier` in api-resources).
const catalogIdentifierNamespace = "mod"

// defaultPublisherSlug is the publisher slug fallback used when the developer
// account exposes no slug (mirrors DEFAULT_PUBLISHER_SLUG server-side).
const defaultPublisherSlug = "developer"

// CatalogSlug normalizes a string into the kebab-case slug the catalogue
// identifiers use (counterpart of `toCatalogSlug` server-side: NFKD
// normalization, diacritics stripped, non-alphanumerics collapsed to `-`).
func CatalogSlug(input string) string {
	decomposed := norm.NFKD.String(strings.ToLower(strings.TrimSpace(input)))
	var b strings.Builder
	lastDash := true // trim leading dashes
	for _, r := range decomposed {
		// Diacritics surface as combining marks after NFKD: dropped, exactly
		// like the server-side `replace(/[\u0300-\u036f]/g, '')`.
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// CatalogIdentifier computes the canonical module identifier the store
// recomputes to verify the artefact signature:
// `mod.<publisherSlug>.<moduleSlug>`. It must stay byte-identical to the
// server-side `catalogIdentifier(developerSlug, slug)`.
func CatalogIdentifier(developerSlug, moduleSlug string) string {
	publisher := CatalogSlug(developerSlug)
	if publisher == "" {
		publisher = defaultPublisherSlug
	}
	return fmt.Sprintf("%s.%s.%s", catalogIdentifierNamespace, publisher, CatalogSlug(moduleSlug))
}

// EnsureSigningKey synchronizes the local signing key with the store: it
// returns the existing ACTIVE key carrying this exact public key, or
// registers a new ACTIVE key (`POST /developer-store/signing-keys`) when the
// account has none. Without this synchronization the server rejects the
// signed upload (`422` — no ACTIVE public key for the account).
func (c *Client) EnsureSigningKey(ctx context.Context, publicKeyPEM string) (*SigningKey, bool, error) {
	keys, err := c.ListSigningKeys(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("failed to list the signing keys: %w", err)
	}
	for _, key := range keys {
		if key.Status == "ACTIVE" && key.PublicKey != nil && strings.TrimSpace(*key.PublicKey) == strings.TrimSpace(publicKeyPEM) {
			return &key, false, nil
		}
	}
	created, err := c.CreateSigningKey(ctx, CreateSigningKeyRequest{
		Algorithm: "Ed25519",
		PublicKey: publicKeyPEM,
	})
	if err != nil {
		return nil, false, fmt.Errorf("failed to register the signing key: %w", err)
	}
	return created, true, nil
}

// createProductRequest maps the manifest to CreateModuleProductDto. The
// manifest token is forwarded so the store can reuse the product on subsequent
// publishes (idempotence), alongside the descriptive metadata.
func createProductRequest(m *module.Manifest) map[string]string {
	body := productMetadata(m)
	body["slug"] = ProductSlug(m)
	return body
}

// updateProductRequest maps the manifest to the PUT /modules/:id body.
func updateProductRequest(m *module.Manifest) map[string]string {
	return productMetadata(m)
}

// productMetadata builds the shared product fields (name, type, category,
// description, icon, token and optional secondary category).
func productMetadata(m *module.Manifest) map[string]string {
	body := map[string]string{
		"name":            m.Name,
		"type":            developerTypeFor(m.Type),
		"primaryCategory": primaryCategoryFor(m),
	}
	if token := strings.TrimSpace(m.Token); token != "" {
		body["token"] = token
	}
	if description := strings.TrimSpace(m.Description); description != "" {
		body["description"] = description
	}
	if icon := strings.TrimSpace(m.Icon); icon != "" {
		body["icon"] = icon
	}
	if secondary := secondaryCategoryFor(m); secondary != "" {
		body["secondaryCategory"] = secondary
	}
	return body
}

// primaryCategoryFor returns the manifest category, defaulting to SYSTEM.
func primaryCategoryFor(m *module.Manifest) string {
	if category := strings.ToUpper(strings.TrimSpace(m.Category)); category != "" {
		return category
	}
	return defaultPrimaryCategory
}

// secondaryCategoryFor reads the optional `secondaryCategory` extension field
// preserved from the manifest.
func secondaryCategoryFor(m *module.Manifest) string {
	raw, ok := m.Extra["secondaryCategory"]
	if !ok {
		return ""
	}
	var secondary string
	if err := json.Unmarshal(raw, &secondary); err != nil {
		return ""
	}
	return strings.TrimSpace(secondary)
}

type createVersionRequest struct {
	VersionString     string           `json:"versionString"`
	BuildNumber       int              `json:"buildNumber"`
	ReleaseNotes      *json.RawMessage `json:"releaseNotes,omitempty"`
	MinManager        string           `json:"minManager,omitempty"`
	MaxManager        string           `json:"maxManager,omitempty"`
	MinAPI            string           `json:"minApi,omitempty"`
	MaxAPI            string           `json:"maxApi,omitempty"`
	SupportedRuntimes []string         `json:"supportedRuntimes,omitempty"`
}

type declareArtifactRequest struct {
	Manifest         json.RawMessage `json:"manifest"`
	Checksum         string          `json:"checksum"`
	ManifestChecksum string          `json:"manifestChecksum"`
	Signature        string          `json:"signature"`
	SignatureKeyID   string          `json:"signatureKeyId,omitempty"`
	SizeBytes        int64           `json:"size"`
}

// ProductSlug derives the product slug from the manifest id (kebab-case), a
// unique per-account module identifier.
func ProductSlug(m *module.Manifest) string {
	if id := strings.TrimSpace(m.ID); id != "" {
		return id
	}
	key := strings.ToLower(strings.ReplaceAll(m.Key, "_", "-"))
	if key == "" {
		return "module"
	}
	return key
}

// developerTypeFor maps the manifest module type to a DeveloperModuleType.
// The canonical `ModuleType` values pass through unchanged; the legacy
// `EXTERNAL` (remotely-served web app) maps to `WEB_APP_REMOTE` and
// `INTERNAL` (socle-bundled) to `WEB_APP_LOCAL`.
func developerTypeFor(manifestType string) string {
	switch strings.ToUpper(strings.TrimSpace(manifestType)) {
	case "CONFIGURATION":
		return ModuleTypeConfiguration
	case "EXTERNAL_URL", "REMOTE_FRONTEND":
		return ModuleTypeExternalURL
	case "WEB_APP_CACHED":
		return ModuleTypeWebAppCached
	case "WEB_APP_LOCAL", "INTERNAL":
		return ModuleTypeWebAppLocal
	case "", "EXTERNAL", "WEB", "WEB_APP", "WEB_APP_REMOTE":
		return ModuleTypeWebAppRemote
	default:
		return ModuleTypeWebAppRemote
	}
}

// supportedRuntimes lists the canonical runtime identifiers enabled in the
// manifest platforms (spec `module-installation.md` §4.3, ADR-004):
// `web`, `tauri-desktop-{windows,macos,linux}` and
// `tauri-mobile-{android,ios}`.
func supportedRuntimes(m *module.Manifest) []string {
	if m == nil {
		return nil
	}
	var runtimes []string
	if m.Platforms.Web.Supported {
		runtimes = append(runtimes, "web")
	}
	if m.Platforms.Desktop.Supported {
		runtimes = append(runtimes, desktopRuntimes(m.Platforms.Desktop.OS)...)
	}
	if m.Platforms.Mobile.Supported {
		runtimes = append(runtimes, mobileRuntimes(m.Platforms.Mobile.OS)...)
	}
	return runtimes
}

// desktopRuntimes expands a desktop `os` list to canonical runtime ids.
// An empty list means every desktop OS.
func desktopRuntimes(osList []string) []string {
	if len(osList) == 0 {
		return []string{"tauri-desktop-windows", "tauri-desktop-macos", "tauri-desktop-linux"}
	}
	var out []string
	for _, osName := range osList {
		switch strings.ToLower(strings.TrimSpace(osName)) {
		case "windows":
			out = append(out, "tauri-desktop-windows")
		case "macos", "darwin":
			out = append(out, "tauri-desktop-macos")
		case "linux":
			out = append(out, "tauri-desktop-linux")
		}
	}
	return out
}

// mobileRuntimes expands a mobile `os` list to canonical runtime ids.
// An empty list means every mobile OS.
func mobileRuntimes(osList []string) []string {
	if len(osList) == 0 {
		return []string{"tauri-mobile-android", "tauri-mobile-ios"}
	}
	var out []string
	for _, osName := range osList {
		switch strings.ToLower(strings.TrimSpace(osName)) {
		case "android":
			out = append(out, "tauri-mobile-android")
		case "ios":
			out = append(out, "tauri-mobile-ios")
		}
	}
	return out
}

// remoteFromProduct maps a Product entity to the lightweight RemoteModule shape.
func remoteFromProduct(p Product) RemoteModule {
	status := strings.TrimSpace(p.Status)
	if status == "" && p.IsDeprecated {
		status = "DEPRECATED"
	}
	return RemoteModule{Token: p.ID, Name: p.Name, Description: p.Description, Status: status}
}
