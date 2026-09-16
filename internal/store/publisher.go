package store

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/protorians/sentient-cli/internal/auth"
	"github.com/protorians/sentient-cli/internal/module"
	"github.com/protorians/sentient-cli/internal/pkg"
)

// Developer store API paths (Raiton envelope, `/api` prefix).
const modulesPath = "/api/developer-store/modules"

// DeveloperModuleType enum values exposed by the store.
const (
	ModuleTypeConfiguration  = "CONFIGURATION"
	ModuleTypeExternalURL    = "EXTERNAL_URL"
	ModuleTypeWebAppRemote   = "WEB_APP_REMOTE"
	ModuleTypeWebAppCached   = "WEB_APP_CACHED"
	ModuleTypeWebAppLocal    = "WEB_APP_LOCAL"
	ModuleTypeRemoteFrontend = "REMOTE_FRONTEND"
)

// defaultPrimaryCategory is used for products whose manifest declares no
// category (the manifest format has no category field yet).
const defaultPrimaryCategory = "SYSTEM"

// Product is a StoreModuleProduct entity (`CreateModuleProductDto` payload).
type Product struct {
	ID                string  `json:"id"`
	AccountID         string  `json:"accountId"`
	ModuleStoreID     string  `json:"moduleStoreId,omitempty"`
	Name              string  `json:"name"`
	Slug              string  `json:"slug"`
	Type              string  `json:"type"`
	PrimaryCategory   string  `json:"primaryCategory"`
	SecondaryCategory string  `json:"secondaryCategory,omitempty"`
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
	Connector *auth.Connector
}

// NewClient builds a store client with the current session token.
func NewClient() *Client {
	return &Client{Connector: auth.NewConnector()}
}

// SetToken sets the bearer token for API calls.
func (c *Client) SetToken(token string) {
	c.Connector.Client.Token = token
}

// ListModules returns the modules owned by the authenticated developer.
// The response may be a bare array or the paginated envelope `{items, total, …}`.
func (c *Client) ListModules(ctx context.Context) ([]RemoteModule, error) {
	var raw json.RawMessage
	if err := c.Connector.Client.Do(ctx, "GET", modulesPath, nil, &raw); err != nil {
		return nil, fmt.Errorf("failed to fetch modules: %w", err)
	}
	var products []Product
	if err := json.Unmarshal(raw, &products); err == nil {
		modules := make([]RemoteModule, 0, len(products))
		for _, p := range products {
			modules = append(modules, remoteFromProduct(p))
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
		modules = append(modules, remoteFromProduct(p))
	}
	return modules, nil
}

// GetModule returns a remote module by its id (product id).
func (c *Client) GetModule(ctx context.Context, id string) (*RemoteModuleResponse, error) {
	var product Product
	if err := c.Connector.Client.Do(ctx, "GET", modulesPath+"/"+url.PathEscape(id), nil, &product); err != nil {
		return nil, fmt.Errorf("failed to fetch module %q: %w", id, err)
	}
	mod := remoteFromProduct(product)
	mod.Version = c.latestVersion(ctx, product.ID)
	return &RemoteModuleResponse{
		Token:   mod.Token,
		Name:    mod.Name,
		Version: mod.Version,
		Status:  mod.Status,
	}, nil
}

// latestVersion returns the most recent published version string (best-effort).
func (c *Client) latestVersion(ctx context.Context, productID string) string {
	var raw json.RawMessage
	if err := c.Connector.Client.Do(ctx, "GET", modulesPath+"/"+productID+"/versions", nil, &raw); err != nil {
		return ""
	}
	var versions []Version
	if err := json.Unmarshal(raw, &versions); err != nil {
		var page struct {
			Items []Version `json:"items"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return ""
		}
		versions = page.Items
	}
	best := ""
	bestBuild := 0
	for _, v := range versions {
		if v.BuildNumber >= bestBuild {
			best = v.VersionString
			bestBuild = v.BuildNumber
		}
	}
	return best
}

// UpdateModule syncs a module's remote metadata via PUT /api/developer-store/modules/:id.
func (c *Client) UpdateModule(ctx context.Context, id string, m *module.Manifest) error {
	if err := c.Connector.Client.Do(ctx, "PUT", modulesPath+"/"+url.PathEscape(id), updateProductRequest(m), nil); err != nil {
		return fmt.Errorf("failed to update module %q: %w", id, err)
	}
	return nil
}

// Publish registers the module product, creates its version and declares the
// archive artifact (spec connect §21): product → version → artifact.
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

	// 3. Declare the artifact: manifest, SHA-256 checksum, signature, size.
	artifact, err := c.declareArtifact(ctx, productID, version.ID, archivePath, manifest)
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
	if err := c.Connector.Client.Do(ctx, "POST", modulesPath, createProductRequest(m), &out); err != nil {
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
		err := c.Connector.Client.Do(ctx, "GET", modulesPath+"/"+url.PathEscape(id), nil, &product)
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
	body := createVersionRequest{
		VersionString:     m.Version,
		BuildNumber:       1,
		ReleaseNotes:      &releaseNotes,
		MinManager:        m.ManagerCompat.Min,
		MaxManager:        m.ManagerCompat.Max,
		MinAPI:            m.APICompat.Min,
		MaxAPI:            m.APICompat.Max,
		SupportedRuntimes: supportedRuntimes(m),
	}
	var out Version
	if err := c.Connector.Client.Do(ctx, "POST", modulesPath+"/"+productID+"/versions", body, &out); err != nil {
		return nil, fmt.Errorf("failed to create the version: %w", err)
	}
	return &out, nil
}

func (c *Client) declareArtifact(ctx context.Context, productID, versionID, archivePath string, m *module.Manifest) (*Artifact, error) {
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
	body := declareArtifactRequest{
		Manifest:  manifestJSON,
		Checksum:  hex.EncodeToString(checksum[:]),
		Signature: signature,
		SizeBytes: int64(len(data)),
	}
	var out Artifact
	if err := c.Connector.Client.Do(ctx, "POST",
		modulesPath+"/"+productID+"/versions/"+versionID+"/artifact", body, &out); err != nil {
		return nil, fmt.Errorf("failed to declare the artifact: %w", err)
	}
	return &out, nil
}

// artifactSignature base64-encodes the `.SenMod.sig` signature file when present
// (produced by `sentients sign`). The signature stays empty when absent.
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

// createProductRequest maps the manifest to CreateModuleProductDto.
func createProductRequest(m *module.Manifest) map[string]string {
	return map[string]string{
		"name":            m.Name,
		"slug":            slugFor(m),
		"type":            developerTypeFor(m.Type),
		"primaryCategory": defaultPrimaryCategory,
	}
}

// updateProductRequest maps the manifest to the PUT /modules/:id body.
func updateProductRequest(m *module.Manifest) map[string]string {
	return map[string]string{
		"name":            m.Name,
		"type":            developerTypeFor(m.Type),
		"primaryCategory": defaultPrimaryCategory,
	}
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
	Manifest  json.RawMessage `json:"manifest"`
	Checksum  string          `json:"checksum"`
	Signature string          `json:"signature"`
	SizeBytes int64           `json:"size"`
}

// slugFor derives the product slug from the manifest id (kebab-case), a unique
// per-account module identifier.
func slugFor(m *module.Manifest) string {
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
// Remote web apps are the default (the "EXTERNAL" module type published in the
// storefront consumes a remote frontend served by Sentient).
func developerTypeFor(manifestType string) string {
	switch strings.ToUpper(strings.TrimSpace(manifestType)) {
	case "", "EXTERNAL", "WEB", "WEB_APP", "WEB_APP_REMOTE":
		return ModuleTypeWebAppRemote
	case "EXTERNAL_URL", "REMOTE_FRONTEND":
		return ModuleTypeExternalURL
	case "CONFIGURATION":
		return ModuleTypeConfiguration
	case "WEB_APP_CACHED":
		return ModuleTypeWebAppCached
	case "WEB_APP_LOCAL":
		return ModuleTypeWebAppLocal
	default:
		return ModuleTypeWebAppRemote
	}
}

// supportedRuntimes lists the runtimes enabled in the manifest platforms.
func supportedRuntimes(m *module.Manifest) []string {
	if m == nil {
		return nil
	}
	var runtimes []string
	if m.Platforms.Web.Supported {
		runtimes = append(runtimes, "WEB")
	}
	if m.Platforms.Desktop.Supported {
		runtimes = append(runtimes, "DESKTOP")
	}
	if m.Platforms.Mobile.Supported {
		runtimes = append(runtimes, "MOBILE")
	}
	return runtimes
}

// remoteFromProduct maps a Product entity to the lightweight RemoteModule shape.
func remoteFromProduct(p Product) RemoteModule {
	status := ""
	if p.IsDeprecated {
		status = "DEPRECATED"
	}
	return RemoteModule{Token: p.ID, Name: p.Name, Status: status}
}
