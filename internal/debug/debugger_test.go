package debug

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/protorians/sentient-cli/internal/config"
	"github.com/protorians/sentient-cli/internal/module"
)

func createTestModule(t *testing.T, root, name string) {
	t.Helper()
	creator := &module.Creator{Root: root}
	if _, err := creator.Create(name, "Test module"); err != nil {
		t.Fatalf("Creator.Create(%q): %v", name, err)
	}
}

func setupDebugProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, config.ExternalModulesDir), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestDebugModuleValid(t *testing.T) {
	root := setupDebugProject(t)
	createTestModule(t, root, "my-module")

	debugger := &Debugger{Root: root}
	result, err := debugger.DebugModule("my-module")
	if err != nil {
		t.Fatalf("DebugModule: %v", err)
	}
	if result.Module != "my-module" {
		t.Errorf("Module = %q, want my-module", result.Module)
	}
	// Without a package manager or build script, should succeed with validation only
	if result.Status != "OK" && result.Status != "WARNING" {
		t.Errorf("Status = %q, want OK or WARNING", result.Status)
	}
}

func TestDebugModuleMissing(t *testing.T) {
	root := setupDebugProject(t)
	debugger := &Debugger{Root: root}
	_, err := debugger.DebugModule("nonexistent")
	if err == nil {
		t.Error("DebugModule d'un module absent doit échouer")
	}
}

func TestDebugModuleInvalidManifest(t *testing.T) {
	root := setupDebugProject(t)
	createTestModule(t, root, "bad-mod")

	// Corrupt the manifest token
	manifestPath := filepath.Join(root, config.ExternalModulesDir, "bad-mod", "manifest.json")
	manifest, _ := module.LoadManifest(manifestPath)
	manifest.Token = "invalid-token"
	if err := manifest.Save(manifestPath); err != nil {
		t.Fatal(err)
	}

	debugger := &Debugger{Root: root}
	result, err := debugger.DebugModule("bad-mod")
	if err != nil {
		t.Fatalf("DebugModule: %v", err)
	}
	if result.Status != "ERROR" {
		t.Errorf("Status = %q, want ERROR", result.Status)
	}
	if result.Errors == 0 {
		t.Error("Errors doit être > 0 pour un manifest invalide")
	}
}

func TestDebugAll(t *testing.T) {
	root := setupDebugProject(t)
	createTestModule(t, root, "mod-a")
	createTestModule(t, root, "mod-b")

	debugger := &Debugger{Root: root}
	results, err := debugger.DebugAll()
	if err != nil {
		t.Fatalf("DebugAll: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("attendu 2 résultats, reçu %d", len(results))
	}
}

func TestDebugAllMissingDir(t *testing.T) {
	root := t.TempDir()
	debugger := &Debugger{Root: root}
	_, err := debugger.DebugAll()
	if err == nil {
		t.Error("DebugAll sans external_modules/ doit échouer")
	}
}

func TestDebugAllSkipsNonModules(t *testing.T) {
	root := setupDebugProject(t)
	createTestModule(t, root, "real-module")
	// Create a directory without manifest.json
	if err := os.MkdirAll(filepath.Join(root, config.ExternalModulesDir, "not-a-module"), 0o755); err != nil {
		t.Fatal(err)
	}

	debugger := &Debugger{Root: root}
	results, err := debugger.DebugAll()
	if err != nil {
		t.Fatalf("DebugAll: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("attendu 1 résultat (skip non-modules), reçu %d", len(results))
	}
}

func TestFindBuildCommandPrefersModulePackage(t *testing.T) {
	root := setupDebugProject(t)
	createTestModule(t, root, "my-module")
	moduleDir := filepath.Join(root, config.ExternalModulesDir, "my-module")

	// Root defines only a "build:prod" script; the module defines "build".
	rootPkg := filepath.Join(root, "package.json")
	if err := os.WriteFile(rootPkg, []byte(`{"scripts":{"build:prod":"tsc"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	modPkg := filepath.Join(moduleDir, "package.json")
	if err := os.WriteFile(modPkg, []byte(`{"scripts":{"build":"tsc -p ."}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	d := &Debugger{Root: root}
	build := d.findBuildCommand("npm", moduleDir)
	if build == nil {
		t.Fatal("un script build doit être trouvé dans le package.json du module")
	}
	if build.dir != moduleDir {
		t.Errorf("le script doit être exécuté depuis le dossier du module, obtenu %s", build.dir)
	}
	if len(build.cmd) != 3 || build.cmd[0] != "npm" || build.cmd[2] != "build" {
		t.Errorf("commande inattendue : %v", build.cmd)
	}
}

func TestFindBuildCommandRootFallback(t *testing.T) {
	root := setupDebugProject(t)
	createTestModule(t, root, "my-module")
	moduleDir := filepath.Join(root, config.ExternalModulesDir, "my-module")

	// Only the project root exposes a "debug" script.
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"debug":"vite --debug"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	d := &Debugger{Root: root}
	build := d.findBuildCommand("bun", moduleDir)
	if build == nil {
		t.Fatal("le script debug racine doit être trouvé en repli")
	}
	if build.dir != root {
		t.Errorf("le script racine doit être exécuté depuis la racine, obtenu %s", build.dir)
	}
	if build.cmd[2] != "debug" {
		t.Errorf("script attendu : debug, obtenu %v", build.cmd)
	}
}

func TestFindBuildCommandNoSubstringFalsePositive(t *testing.T) {
	root := setupDebugProject(t)
	createTestModule(t, root, "my-module")
	moduleDir := filepath.Join(root, config.ExternalModulesDir, "my-module")

	// Only "build:prod" exists — plain "build" must NOT match.
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"build:prod":"tsc"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "package.json"), []byte(`{"scripts":{"buildx":"echo"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	d := &Debugger{Root: root}
	if build := d.findBuildCommand("npm", moduleDir); build != nil {
		t.Errorf("aucun script exact debug/dev/build ne doit matcher, obtenu %v", build.cmd)
	}
}

func TestFormatDebugLogs(t *testing.T) {
	logs := []string{"Module chargé", "Aucune erreur"}
	formatted := FormatDebugLogs("my-mod", logs)
	if len(formatted) != 2 {
		t.Fatalf("attendu 2 lignes, reçu %d", len(formatted))
	}
	for i, line := range formatted {
		if line == "" {
			t.Errorf("ligne %d vide", i)
		}
		// Should contain module name
		if !contains(line, "my-mod") {
			t.Errorf("ligne %d ne contient pas le nom du module: %s", i, line)
		}
	}
}

func TestFormatDebugLogsEmpty(t *testing.T) {
	formatted := FormatDebugLogs("mod", nil)
	if len(formatted) != 0 {
		t.Errorf("attendu 0 lignes, reçu %d", len(formatted))
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstr(s, sub))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
