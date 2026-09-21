package module

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/protorians/lior-cli/internal/config"
)

func TestModuleExists(t *testing.T) {
	root := t.TempDir()

	if moduleExists := ModuleExists(root, "missing"); moduleExists {
		t.Error("an absent module must not exist")
	}

	// External module (library/modules/<domain>/).
	if err := os.MkdirAll(filepath.Join(root, config.ExternalModulesDir, "com.ext.mod"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !ModuleExists(root, "com.ext.mod") {
		t.Error("an external library/modules/ module must be found by its folder domain")
	}

	// External module referenced by its manifest id.
	if err := os.MkdirAll(filepath.Join(root, config.ExternalModulesDir, "com.example.analytics"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := NewManifest("analytics", "")
	if err := m.Save(filepath.Join(root, config.ExternalModulesDir, "com.example.analytics", config.ManifestFileName)); err != nil {
		t.Fatal(err)
	}
	if !ModuleExists(root, "analytics") {
		t.Error("an external library/modules/ module must be found by its manifest id")
	}

	// Internal module (src/modules/<name>/).
	if err := os.MkdirAll(filepath.Join(root, config.InternalModulesDir, "int-mod"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !ModuleExists(root, "int-mod") {
		t.Error("an internal src/modules/ module must be found")
	}
}

func TestMissingRequirements(t *testing.T) {
	root, creator := setupProject(t)
	if _, err := creator.Create(ModuleSpec{Domain: "com.example.dep-a", ID: "dep-a"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, config.InternalModulesDir, "int-b"), 0o755); err != nil {
		t.Fatal(err)
	}

	manifest := NewManifest("consumer", "")
	manifest.Requirements = map[string]any{
		"dep-a":        true, // library/modules/
		"int-b":        true, // src/modules/
		"organization": true, // platform core
		"identity":     true, // platform core
		"missing-mod":  true, // nowhere
	}

	missing := creator.MissingRequirements(&manifest)
	if len(missing) != 1 || missing[0] != "missing-mod" {
		t.Errorf("MissingRequirements = %v, want [missing-mod]", missing)
	}
}

func TestResolveDependenciesNoPackageManager(t *testing.T) {
	creator := &Creator{Root: t.TempDir()}
	t.Setenv("PATH", "/nonexistent")

	pm, err := creator.ResolveDependencies("")
	if err != nil {
		t.Fatalf("ResolveDependencies without a package manager must not error: %v", err)
	}
	if pm != "" {
		t.Errorf("ResolveDependencies = %q, want '' when no package manager", pm)
	}
}

func TestResolveDependenciesRunsInstall(t *testing.T) {
	root := t.TempDir()
	creator := &Creator{Root: root}

	// Fake `bun` (first detected manager) that records its invocation and
	// exits cleanly, so the resolution never touches a real installer.
	bin := t.TempDir()
	marker := filepath.Join(bin, "bun-install.log")
	script := "#!/bin/sh\nprintf '%s %s' \"$*\" \"$PWD\" > \"" + marker + "\"\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "bun"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	pm, err := creator.ResolveDependencies("")
	if err != nil {
		t.Fatalf("ResolveDependencies: %v", err)
	}
	if pm != "bun" {
		t.Errorf("ResolveDependencies = %q, want bun", pm)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("the fake bun must have been called: %v", err)
	}
	parts := strings.Fields(string(data))
	if len(parts) != 2 || parts[0] != "install" {
		t.Errorf("bun must run 'install', got %q", string(data))
	}
	if parts[1] != root {
		t.Errorf("install must run in the project root, got %q", parts[1])
	}
}
