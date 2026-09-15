// Package appconfig reads the workspace `app.config.json` registry used to
// parameterise the CLI: each Sentient application carries its API `baseUrl`
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

	"github.com/protorians/sentient-cli/internal/pkg"
)

// FileName is the well-known workspace registry file.
const FileName = "app.config.json"

// Well-known application ids in `applications`.
const (
	AuthAppID  = "sentient-auth"
	StoreAppID = "sentient-store"
)

// Config mirrors the registry schema (sentient.config.schema.json).
type Config struct {
	Version      string                 `json:"version"`
	Applications map[string]Application `json:"applications"`

	// sourcePath records where the config came from (for debug output).
	sourcePath string
}

// Application is one entry of `applications`.
type Application struct {
	API APIConfig `json:"api"`
}

// APIConfig is the API-side configuration (`api.baseUrl`, `api.timeout`).
type APIConfig struct {
	BaseURL string `json:"baseUrl"`
	Timeout int    `json:"timeout"` // milliseconds
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