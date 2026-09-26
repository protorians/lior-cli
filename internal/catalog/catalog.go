// Package catalog talks to the public Liora module catalog
// (`liorian-api-store`, `/api/catalog/*`): it backs the `marketplace`
// command (search + install of third-party modules, spec §2.4 Future Scope).
//
// The catalog service is the public storefront: unlike the developer-store
// API (`liorian-api-connect`, authenticated), it exposes published modules to
// CLI consumers without a session.
package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/protorians/lior-cli/internal/appconfig"
	"github.com/protorians/lior-cli/internal/module"
	"github.com/protorians/lior-cli/internal/pkg"
)

// EnvAPIBase overrides the resolved catalog API base URL. Mirrors the
// `LIORIAN_AUTH_API` convention of the auth connector.
const EnvAPIBase = "LIORIAN_STORE_API"

// Catalog API paths (Raiton envelope, `/api` prefix).
const (
	searchPath    = "/api/catalog/modules"
	artifactsPath = "/catalog/artifacts/"
)

// CatalogModule is a storefront module entry surfaced by the catalog, usable
// for search/install. It is the consumer-side counterpart of the
// developer-store `Product`/`Version` entities.
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

// SearchResult is the paginated response of a catalog search.
type SearchResult struct {
	Items  []CatalogModule `json:"items"`
	Total  int             `json:"total,omitempty"`
	Offset int             `json:"offset,omitempty"`
	Limit  int             `json:"limit,omitempty"`
}

// SearchOptions parametrises a catalog search.
type SearchOptions struct {
	Query    string
	Category string
	Limit    int
	Offset   int
}

// Client talks to the public catalog API.
type Client struct {
	HTTP *pkg.Client
}

// apiBaseURL resolves the catalog base URL. Resolution order:
//  1. `LIORIAN_STORE_API` environment variable,
//  2. workspace `app.config.json` (walked up from the current directory),
//  3. the `app.config.json` registry embedded in the binary.
//
// It always comes from the API-side configuration of `liorian-store`
// (the `api.baseUrl` entry of `app.config.json`) — never a hardcoded domain.
func apiBaseURL() string {
	if v := os.Getenv(EnvAPIBase); v != "" {
		return v
	}
	base, _ := appconfig.Resolved("").BaseURL(appconfig.StoreAppID)
	return base
}

// apiTimeout resolves the catalog API timeout from `app.config.json`
// (`api.timeout`, milliseconds), falling back to the CLI default.
func apiTimeout() time.Duration {
	return appconfig.Resolved("").Timeout(appconfig.StoreAppID)
}

// NewClient builds a catalog client against the resolved API base URL.
func NewClient() *Client {
	return &Client{HTTP: pkg.NewClientWithTimeout(apiBaseURL(), apiTimeout())}
}

// Search lists the public catalog, optionally filtered by a query, a store
// category, and paginated via limit/offset. The service may answer with a
// bare array (legacy) or with the paginated `{items, total, …}` envelope.
func (c *Client) Search(ctx context.Context, opts SearchOptions) (*SearchResult, error) {
	q := url.Values{}
	if v := strings.TrimSpace(opts.Query); v != "" {
		q.Set("q", v)
	}
	if v := strings.TrimSpace(opts.Category); v != "" {
		q.Set("category", strings.ToUpper(v))
	}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Offset > 0 {
		q.Set("offset", strconv.Itoa(opts.Offset))
	}
	path := searchPath
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	var raw json.RawMessage
	if err := c.HTTP.Do(ctx, "GET", path, nil, &raw); err != nil {
		return nil, fmt.Errorf("failed to search the catalog: %w", err)
	}

	var items []CatalogModule
	if err := json.Unmarshal(raw, &items); err == nil {
		return &SearchResult{Items: items, Total: len(items)}, nil
	}
	var page SearchResult
	if err := json.Unmarshal(raw, &page); err != nil {
		return nil, fmt.Errorf("failed to decode the catalog response: %w", err)
	}
	if page.Total == 0 {
		page.Total = len(page.Items)
	}
	return &page, nil
}

// GetModule returns a single catalog entry, resolved by its slug, domain or
// product id.
func (c *Client) GetModule(ctx context.Context, ref string) (*CatalogModule, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, errors.New("empty module reference")
	}
	var out CatalogModule
	if err := c.HTTP.Do(ctx, "GET", searchPath+"/"+url.PathEscape(ref), nil, &out); err != nil {
		return nil, fmt.Errorf("failed to fetch module %q: %w", ref, err)
	}
	return &out, nil
}

// DownloadArtifact fetches the raw `.liozip` archive bytes of a catalog
// module. It enforces the store archive limit (module.MaxArchiveSize).
func (c *Client) DownloadArtifact(ctx context.Context, artifactURL string) ([]byte, error) {
	artifactURL = strings.TrimSpace(artifactURL)
	if artifactURL == "" {
		return nil, errors.New("module has no downloadable artifact")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, artifactURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build the artifact request: %w", err)
	}
	req.Header.Set("User-Agent", pkg.UserAgent())
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := c.HTTP.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error while downloading the artifact: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("artifact download failed with HTTP %d", resp.StatusCode)
	}
	reader := io.LimitReader(resp.Body, module.MaxArchiveSize+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read the artifact: %w", err)
	}
	if int64(len(data)) > module.MaxArchiveSize {
		return nil, fmt.Errorf("artifact exceeds the maximum size of %d MB", module.MaxArchiveSize/(1024*1024))
	}
	return data, nil
}
