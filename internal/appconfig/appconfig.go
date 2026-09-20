// Package appconfig reads the workspace `app.config.json` registry used to
// parameterise the CLI: each Liorian application carries its API `baseUrl`
// and `timeout`. The registry is embedded in the binary at build time, so any
// command keeps working outside a workspace; a local `app.config.json`
// (walked up from the current directory) overrides the embedded one.
package appconfig

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jetbrains/lior-cli/internal/pkg"
)

// FileName is the well-known workspace registry file.
const FileName = "app.config.json"

// Well-known application ids in `applications`.
const (
	AuthAppID  = "liorian-auth"
	StoreAppID = "liorian-store"
)

// Config mirrors the registry schema (lorian.config.schema.json).
type Config struct {
	Version      string                 `json:"version"`
	Applications map[string]Application `json:"applications"`

	// sourcePath records where the config came from (for debug output).
	sourcePath string
}

// Application is one entry of `applications`.
type Application struct {
	API   APIConfig   `json:"api"`
	OAuth OAuthConfig `json:"oauth"`
}

// APIConfig is the API-side configuration (`api.baseUrl`, `api.timeout`).
type APIConfig struct {
	BaseURL string `json:"baseUrl"`
	Timeout int    `json:"timeout"` // milliseconds
}

// OAuthConfig is the OAuth2 (authorization-code + PKCE) configuration of an
// application. Endpoints are relative paths appended to the API base URL.
type OAuthConfig struct {
	AuthorizationEndpoint string   `json:"authorizationEndpoint"`
	TokenEndpoint         string   `json:"tokenEndpoint"`
	RevokeEndpoint        string   `json:"revokeEndpoint"`
	ClientID              string   `json:"clientId"`
	Scopes                []string `json:"scopes"`
}

// embedded is the registry compiled into the binary (injected by main).
var embedded []byte

// SetEmbedded installs the `app.config.json` bytes baked into the binary. It
// is called once during CLI startup (cmd.Execute).
func SetEmbedded(data []byte) {
	embedded = data
}

// Find walks up from start (default: the current directory) looking for an
// `app.config.json` file (workspace root).
func Find(start string) (string, error) {
	dir := start
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	for {
		candidate := filepath.Join(dir, FileName)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New(FileName + " not found")
		}
		dir = parent
	}
}

// Load reads the registry at path.
func Load(path string) (Config, error) {
	var cfg Config
	if path == "" {
		return cfg, errors.New("empty " + FileName + " path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	cfg.sourcePath = path
	return cfg, nil
}

// Resolved returns the effective registry: the workspace one when present
// (walked up from start), otherwise the one embedded in the binary.
func Resolved(start string) Config {
	if path, err := Find(start); err == nil {
		if cfg, err := Load(path); err == nil {
			return cfg
		}
	}
	var cfg Config
	if len(embedded) > 0 {
		if err := json.Unmarshal(embedded, &cfg); err == nil {
			cfg.sourcePath = "embedded:" + FileName
		}
	}
	return cfg
}

// Source describes where the resolved registry came from, for debug output.
func (c Config) Source() string {
	if c.sourcePath == "" {
		return "none"
	}
	return c.sourcePath
}

// BaseURL returns the API base URL configured for an application id.
func (c Config) BaseURL(appID string) (string, bool) {
	app, ok := c.Applications[appID]
	if !ok || strings.TrimSpace(app.API.BaseURL) == "" {
		return "", false
	}
	return app.API.BaseURL, true
}

// Timeout returns the API timeout configured for an application id, falling
// back to the CLI default when missing or below the schema minimum (1000 ms).
func (c Config) Timeout(appID string) time.Duration {
	if app, ok := c.Applications[appID]; ok && app.API.Timeout >= 1000 {
		return time.Duration(app.API.Timeout) * time.Millisecond
	}
	return pkg.DefaultHTTPTimeout
}

// Default OAuth2 (authorization-code + PKCE) configuration used when the
// workspace registry does not declare an `oauth` block.
const (
	DefaultOAuthAuthorizationEndpoint = "/oauth/authorize"
	DefaultOAuthTokenEndpoint         = "/oauth/token"
	DefaultOAuthRevokeEndpoint        = "/oauth/revoke"
	DefaultOAuthClientID              = "lior-cli"
)

// OAuth returns the OAuth2 configuration for an application id, filling any
// missing field with its default. The endpoints are relative paths (joined to
// the API base URL by the caller).
func (c Config) OAuth(appID string) OAuthConfig {
	o := OAuthConfig{}
	if app, ok := c.Applications[appID]; ok {
		o = app.OAuth
	}
	if strings.TrimSpace(o.AuthorizationEndpoint) == "" {
		o.AuthorizationEndpoint = DefaultOAuthAuthorizationEndpoint
	}
	if strings.TrimSpace(o.TokenEndpoint) == "" {
		o.TokenEndpoint = DefaultOAuthTokenEndpoint
	}
	if strings.TrimSpace(o.RevokeEndpoint) == "" {
		o.RevokeEndpoint = DefaultOAuthRevokeEndpoint
	}
	if strings.TrimSpace(o.ClientID) == "" {
		o.ClientID = DefaultOAuthClientID
	}
	if len(o.Scopes) == 0 {
		o.Scopes = []string{"openid", "profile", "email"}
	}
	return o
}
