package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Config mirrors the optional `lorian.config.json` file at the project root.
type Config struct {
	Project   ProjectConfig   `json:"project"`
	Publish   PublishConfig   `json:"publish"`
	Debug     DebugConfig     `json:"debug"`
	Test      TestConfig      `json:"test"`
	Cli       CliConfig       `json:"cli"`
	Toolchain ToolchainConfig `json:"toolchain"`
}

// TestConfig configures `liora test`: which package manager installs and
// runs the test packages, and which test package each module uses. The values
// are persisted in `lorian.config.json` so later runs skip detection and
// selection.
type TestConfig struct {
	// PackageManager overrides the package manager used to install/run test
	// packages. Empty falls back to project.packageManager (chosen at init).
	PackageManager string `json:"packageManager,omitempty"`
	// Runner is the default test package (e.g. "vitest", "jest") used when a
	// module has no override. The values "script" (the module's package.json
	// `test` script) and "builtin" (e.g. `bun test`) are also accepted.
	Runner string `json:"runner,omitempty"`
	// Modules overrides the runner per module (keyed by module domain).
	Modules map[string]ModuleTestConfig `json:"modules,omitempty"`
}

// ModuleTestConfig overrides the test configuration for a single module.
type ModuleTestConfig struct {
	Runner string `json:"runner,omitempty"`
}

// RunnerFor returns the configured test runner for a module: the module
// override first, then the project default (both trimmed).
func (c TestConfig) RunnerFor(module string) string {
	if m, ok := c.Modules[module]; ok {
		if runner := strings.TrimSpace(m.Runner); runner != "" {
			return runner
		}
	}
	return strings.TrimSpace(c.Runner)
}

// SetRunner persists a runner for a module. An empty module sets the project
// default; an empty runner clears the entry.
func (c *TestConfig) SetRunner(module, runner string) {
	runner = strings.TrimSpace(runner)
	if module == "" {
		c.Runner = runner
		return
	}
	if c.Modules == nil {
		c.Modules = map[string]ModuleTestConfig{}
	}
	if runner == "" {
		delete(c.Modules, module)
		return
	}
	c.Modules[module] = ModuleTestConfig{Runner: runner}
}

// CliConfig configures CLI-level settings.
type CliConfig struct {
	// Lang is the UI locale of the CLI (e.g. "fr-FR", "en-US"). An empty
	// value keeps the auto-detection (LIORIAN_CLI_LANG / OS locale).
	Lang string `json:"lang"`
	// NoColor disables colored output. An empty/false value keeps the default
	// terminal color profile.
	NoColor bool `json:"noColor"`
}

// ToolchainConfig configures the application passthrough commands (`dev`,
// `build`, `start`, `check`): the project can rename the package.json script
// backing each command and register pre/post actions around it.
type ToolchainConfig struct {
	// Commands maps a liora command ("dev", "build", "start", "check") to
	// the package.json script that backs it. An empty value falls back to the
	// default mapping ("dev"→"dev", "build"→"build", "start"→"start",
	// "check"→"lint").
	Commands map[string]string `json:"commands,omitempty"`
	// Before lists package.json scripts run through the package manager
	// before the toolchain command, keyed by liora command name. A failing
	// hook aborts the command.
	Before map[string][]string `json:"before,omitempty"`
	// After lists package.json scripts run through the package manager after
	// the toolchain command, keyed by liora command name. A failing hook
	// makes the run non-zero.
	After map[string][]string `json:"after,omitempty"`
}

// Command returns the configured package.json script for a liora command
// name, falling back to an empty string when none is configured.
func (c ToolchainConfig) Command(name string) string {
	return strings.TrimSpace(c.Commands[name])
}

// BeforeHooks returns the configured pre-action hook names for a liora
// command.
func (c ToolchainConfig) BeforeHooks(name string) []string {
	return c.CommandsHooks(c.Before, name)
}

// AfterHooks returns the configured post-action hook names for a liora
// command.
func (c ToolchainConfig) AfterHooks(name string) []string {
	return c.CommandsHooks(c.After, name)
}

// CommandsHooks returns the hook names configured for a command under the
// given map, filtering out empty entries.
func (c ToolchainConfig) CommandsHooks(hooks map[string][]string, name string) []string {
	var out []string
	for _, h := range hooks[name] {
		if h = strings.TrimSpace(h); h != "" {
			out = append(out, h)
		}
	}
	return out
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
			DefaultRegistry: "https://store.liorian.dev",
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
