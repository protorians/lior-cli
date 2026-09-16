package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config mirrors the optional `sentients.config.json` file at the project root.
type Config struct {
	Project ProjectConfig `json:"project"`
	Publish PublishConfig `json:"publish"`
	Debug   DebugConfig   `json:"debug"`
	Cli     CliConfig     `json:"cli"`
}

// CliConfig configures CLI-level settings.
type CliConfig struct {
	// Lang is the UI locale of the CLI (e.g. "fr-FR", "en-US"). An empty
	// value keeps the auto-detection (SENTIENT_CLI_LANG / OS locale).
	Lang string `json:"lang"`
}

// ProjectConfig configures the project-level settings.
type ProjectConfig struct {
	Name           string `json:"name"`
	PackageManager string `json:"packageManager"`
}

// PublishConfig configures publication defaults.
type PublishConfig struct {
	DefaultRegistry string `json:"defaultRegistry"`
	AutoAudit       bool   `json:"autoAudit"`
}

// DebugConfig configures debug/log behaviour.
type DebugConfig struct {
	Verbose  bool   `json:"verbose"`
	LogLevel string `json:"logLevel"`
}

// Default returns a Config populated with sensible defaults.
func Default() Config {
	return Config{
		Project: ProjectConfig{},
		Publish: PublishConfig{
			DefaultRegistry: "https://store.sentient.dev",
			AutoAudit:       true,
		},
		Debug: DebugConfig{
			Verbose:  false,
			LogLevel: "info",
		},
	}
}

// Load reads a config file at path. A missing file yields the defaults.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("failed to read config file %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("failed to decode config file %s: %w", path, err)
	}
	return cfg, nil
}

// Save writes the config as pretty-printed JSON at path.
func (c Config) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode config file %s: %w", path, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write config file %s: %w", path, err)
	}
	return nil
}
