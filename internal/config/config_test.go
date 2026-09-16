package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigDefaults(t *testing.T) {
	cfg := Default()
	if !cfg.Publish.AutoAudit {
		t.Error("AutoAudit doit être true par défaut")
	}
	if cfg.Publish.DefaultRegistry != "https://store.sentient.dev" {
		t.Errorf("DefaultRegistry incorrect: %q", cfg.Publish.DefaultRegistry)
	}
	if cfg.Debug.LogLevel != "info" {
		t.Errorf("LogLevel incorrect: %q", cfg.Debug.LogLevel)
	}
}

func TestConfigSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sentients.config.json")

	cfg := Default()
	cfg.Project.Name = "mon-projet"
	cfg.Project.PackageManager = "bun"
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Project.Name != "mon-projet" {
		t.Errorf("Name = %q, want mon-projet", loaded.Project.Name)
	}
	if loaded.Project.PackageManager != "bun" {
		t.Errorf("PackageManager = %q, want bun", loaded.Project.PackageManager)
	}
}

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Project.Name != "" {
		t.Error("config non-existante doit renvoyer les défauts")
	}
}

func TestTestConfigRunnerPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sentients.config.json")

	cfg := Default()
	cfg.Project.PackageManager = "bun"
	cfg.Test.PackageManager = "pnpm"
	cfg.Test.SetRunner("", "vitest")
	cfg.Test.SetRunner("com.test.blog", "jest")
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Test.PackageManager != "pnpm" {
		t.Errorf("Test.PackageManager = %q, want pnpm", loaded.Test.PackageManager)
	}
	if got := loaded.Test.RunnerFor("com.test.other"); got != "vitest" {
		t.Errorf("RunnerFor(autre) = %q, want vitest (défaut)", got)
	}
	if got := loaded.Test.RunnerFor("com.test.blog"); got != "jest" {
		t.Errorf("RunnerFor(blog) = %q, want jest (override)", got)
	}
}

func TestTestConfigSetRunnerClearsEntry(t *testing.T) {
	cfg := Default()
	cfg.Test.SetRunner("com.test.blog", "jest")
	cfg.Test.SetRunner("com.test.blog", "")
	if got := cfg.Test.RunnerFor("com.test.blog"); got != "" {
		t.Errorf("RunnerFor après suppression = %q, want vide", got)
	}
	if len(cfg.Test.Modules) != 0 {
		t.Errorf("Modules = %v, want vide après suppression", cfg.Test.Modules)
	}
}

func TestFindProjectRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ExternalModulesDir), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	found, err := FindProjectRoot(sub)
	if err != nil {
		t.Fatalf("FindProjectRoot: %v", err)
	}
	if found != root {
		t.Errorf("racine trouvée = %q, want %q", found, root)
	}
}

func TestFindProjectRootNotFound(t *testing.T) {
	dir := t.TempDir()
	if _, err := FindProjectRoot(dir); err == nil {
		t.Error("aucune racine ne doit être trouvée")
	}
}
