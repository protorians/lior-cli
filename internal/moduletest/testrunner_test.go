package moduletest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/protorians/sentient-cli/internal/config"
	"github.com/protorians/sentient-cli/internal/module"
	"github.com/protorians/sentient-cli/internal/tui"
)

func createTestModule(t *testing.T, root, id string) {
	t.Helper()
	creator := &module.Creator{Root: root}
	if _, err := creator.Create(module.ModuleSpec{Domain: "com.test." + id, ID: id, Description: "Test module"}); err != nil {
		t.Fatalf("Creator.Create(%q): %v", id, err)
	}
}

func setupTestProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, config.ExternalModulesDir), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// writeFakePM writes a fake package manager to PATH. mode determines the
// behaviour on `run test`: "ok" exits 0, "fail" exits 1 and "sleep" stays up.
func writeFakePM(t *testing.T, path, mode string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "exit 0"
	switch mode {
	case "fail":
		body = "exit 1"
	case "sleep":
		body = "sleep 30"
	}
	script := "#!/bin/sh\nif [ \"$1\" = \"run\" ] && [ \"$2\" = \"test\" ]; then echo \"[fake] run test\"; " + body + "; fi\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestTestModuleValid(t *testing.T) {
	root := setupTestProject(t)
	createTestModule(t, root, "my-module")

	pathShim := t.TempDir()
	writeFakePM(t, filepath.Join(pathShim, "bun"), "ok")
	t.Setenv("PATH", pathShim+string(os.PathListSeparator)+os.Getenv("PATH"))

	tester := &Tester{Root: root}
	result, err := tester.TestModule("com.test.my-module")
	if err != nil {
		t.Fatalf("TestModule: %v", err)
	}
	if result.Module != "com.test.my-module" {
		t.Errorf("Module = %q, want com.test.my-module", result.Module)
	}
	// Without a test script or test files, validation alone → WARNING.
	if result.Status != "WARNING" {
		t.Errorf("Status = %q, want WARNING", result.Status)
	}
}

func TestTestModuleRunsPackageScript(t *testing.T) {
	root := setupTestProject(t)
	createTestModule(t, root, "my-module")
	moduleDir := filepath.Join(root, config.ExternalModulesDir, "com.test.my-module")
	if err := os.WriteFile(filepath.Join(moduleDir, "package.json"),
		[]byte(`{"scripts":{"test":"vitest run"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	pathShim := t.TempDir()
	writeFakePM(t, filepath.Join(pathShim, "bun"), "ok")
	t.Setenv("PATH", pathShim+string(os.PathListSeparator)+os.Getenv("PATH"))

	tester := &Tester{Root: root}
	result, err := tester.TestModule("com.test.my-module")
	if err != nil {
		t.Fatalf("TestModule: %v", err)
	}
	if result.Status != "OK" {
		t.Fatalf("Status = %q, want OK (logs: %v)", result.Status, result.Logs)
	}
	if result.Command != "bun run test" {
		t.Errorf("Command = %q, want bun run test", result.Command)
	}
	if !contains(strings.Join(result.Logs, "\n"), "[fake] run test") {
		t.Errorf("les logs doivent contenir la sortie du test : %v", result.Logs)
	}

	run := false
	for _, s := range result.Steps {
		if s.ID == "test:com.test.my-module" && s.Status == tui.StatusSuccess {
			run = true
		}
	}
	if !run {
		t.Errorf("attendu une étape d'exécution en succès : %v", result.Steps)
	}
}

func TestTestModuleRootScriptFallback(t *testing.T) {
	root := setupTestProject(t)
	createTestModule(t, root, "my-module")
	// Only the project root exposes a `test` script.
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"test":"jest --ci"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	pathShim := t.TempDir()
	writeFakePM(t, filepath.Join(pathShim, "bun"), "ok")
	t.Setenv("PATH", pathShim+string(os.PathListSeparator)+os.Getenv("PATH"))

	tester := &Tester{Root: root}
	result, err := tester.TestModule("com.test.my-module")
	if err != nil {
		t.Fatalf("TestModule: %v", err)
	}
	if result.Status != "OK" {
		t.Fatalf("Status = %q, want OK", result.Status)
	}
}

func TestTestModuleFallbackRunnerWithTestFiles(t *testing.T) {
	root := setupTestProject(t)
	moduleDir := writeFixtureModule(t, root, "my-module")
	testFile := filepath.Join(moduleDir, "__tests__", "basic.ts")
	if err := os.MkdirAll(filepath.Dir(testFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(testFile, []byte("it('works', () => {});\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pathShim := t.TempDir()
	writeFakePM(t, filepath.Join(pathShim, "bun"), "ok")
	writeFakeRunner(t, filepath.Join(pathShim, "vitest"), "ok")
	t.Setenv("PATH", pathShim+string(os.PathListSeparator)+os.Getenv("PATH"))

	tester := &Tester{Root: root}
	result, err := tester.TestModule("com.test.my-module")
	if err != nil {
		t.Fatalf("TestModule: %v", err)
	}
	if result.Status != "OK" {
		t.Fatalf("Status = %q, want OK (logs: %v)", result.Status, result.Logs)
	}
	if !strings.Contains(result.Command, "vitest") {
		t.Errorf("Command = %q, want a vitest run", result.Command)
	}
}

func TestTestModuleFailure(t *testing.T) {
	root := setupTestProject(t)
	createTestModule(t, root, "my-module")
	moduleDir := filepath.Join(root, config.ExternalModulesDir, "com.test.my-module")
	if err := os.WriteFile(filepath.Join(moduleDir, "package.json"),
		[]byte(`{"scripts":{"test":"vitest run"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	pathShim := t.TempDir()
	writeFakePM(t, filepath.Join(pathShim, "bun"), "fail")
	t.Setenv("PATH", pathShim+string(os.PathListSeparator)+os.Getenv("PATH"))

	tester := &Tester{Root: root}
	result, err := tester.TestModule("com.test.my-module")
	if err != nil {
		t.Fatalf("TestModule: %v", err)
	}
	if result.Status != "ERROR" {
		t.Fatalf("Status = %q, want ERROR", result.Status)
	}
	if result.Errors != 1 {
		t.Errorf("Errors = %d, want 1", result.Errors)
	}
}

func TestTestModuleTimeoutFails(t *testing.T) {
	root := setupTestProject(t)
	createTestModule(t, root, "my-module")
	moduleDir := filepath.Join(root, config.ExternalModulesDir, "com.test.my-module")
	if err := os.WriteFile(filepath.Join(moduleDir, "package.json"),
		[]byte(`{"scripts":{"test":"vitest run"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	pathShim := t.TempDir()
	writeFakePM(t, filepath.Join(pathShim, "bun"), "sleep")
	t.Setenv("PATH", pathShim+string(os.PathListSeparator)+os.Getenv("PATH"))

	tester := &Tester{Root: root, Timeout: 300 * time.Millisecond}
	result, err := tester.TestModule("com.test.my-module")
	if err != nil {
		t.Fatalf("TestModule: %v", err)
	}
	if result.Status != "ERROR" {
		t.Fatalf("Status = %q, want ERROR (tests dépassés)", result.Status)
	}
}

func TestTestModuleCtxCancelled(t *testing.T) {
	root := setupTestProject(t)
	createTestModule(t, root, "my-module")
	moduleDir := filepath.Join(root, config.ExternalModulesDir, "com.test.my-module")
	if err := os.WriteFile(filepath.Join(moduleDir, "package.json"),
		[]byte(`{"scripts":{"test":"echo hi"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	pathShim := t.TempDir()
	writeFakePM(t, filepath.Join(pathShim, "bun"), "ok")
	t.Setenv("PATH", pathShim+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tester := &Tester{Root: root}
	result, err := tester.TestModuleCtx(ctx, "com.test.my-module")
	if !errors.Is(err, tui.ErrCancelled) {
		t.Fatalf("err = %v, want tui.ErrCancelled", err)
	}
	if result == nil || result.Status != "CANCELLED" {
		t.Fatalf("status = %v, want CANCELLED", result)
	}
}

func TestTestAll(t *testing.T) {
	root := setupTestProject(t)
	createTestModule(t, root, "mod-a")
	createTestModule(t, root, "mod-b")

	pathShim := t.TempDir()
	writeFakePM(t, filepath.Join(pathShim, "bun"), "ok")
	t.Setenv("PATH", pathShim+string(os.PathListSeparator)+os.Getenv("PATH"))

	tester := &Tester{Root: root}
	results, err := tester.TestAll()
	if err != nil {
		t.Fatalf("TestAll: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("attendu 2 résultats, reçu %d", len(results))
	}
}

func TestTestAllSkipsNonModules(t *testing.T) {
	root := setupTestProject(t)
	createTestModule(t, root, "real-module")
	if err := os.MkdirAll(filepath.Join(root, config.ExternalModulesDir, "not-a-module"), 0o755); err != nil {
		t.Fatal(err)
	}

	pathShim := t.TempDir()
	writeFakePM(t, filepath.Join(pathShim, "bun"), "ok")
	t.Setenv("PATH", pathShim+string(os.PathListSeparator)+os.Getenv("PATH"))

	tester := &Tester{Root: root}
	results, err := tester.TestAll()
	if err != nil {
		t.Fatalf("TestAll: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("attendu 1 résultat (skip non-modules), reçu %d", len(results))
	}
}

func TestFindTestCommandNoSubstringFalsePositive(t *testing.T) {
	root := setupTestProject(t)
	createTestModule(t, root, "my-module")
	moduleDir := filepath.Join(root, config.ExternalModulesDir, "com.test.my-module")

	// Only "test:unit" exists — plain `test` must NOT match a script.
	if err := os.WriteFile(filepath.Join(moduleDir, "package.json"),
		[]byte(`{"scripts":{"test:unit":"vitest"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	tester := &Tester{Root: root}
	if tc := tester.findTestCommand("bun", moduleDir); tc != nil {
		t.Errorf("aucun script test exact ne doit matcher, obtenu %v", tc.cmd)
	}
}

// writeFixtureModule scaffolds a module plus an index.tsx entry.
func writeFixtureModule(t *testing.T, root, id string) string {
	t.Helper()
	createTestModule(t, root, id)
	moduleDir := filepath.Join(root, config.ExternalModulesDir, "com.test."+id)
	entry := filepath.Join(moduleDir, "index.tsx")
	if err := os.WriteFile(entry, []byte("export default {};\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return moduleDir
}

// writeFakeRunner writes an executable test-runner shim to PATH.
func writeFakeRunner(t *testing.T, path, mode string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "exit 0"
	if mode == "fail" {
		body = "exit 1"
	}
	script := "#!/bin/sh\necho \"[fixture] $(basename \"$0\") $*\"\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestFormatTestLogs(t *testing.T) {
	logs := []string{"Module chargé", "Aucune erreur"}
	formatted := FormatTestLogs("my-mod", logs)
	if len(formatted) != 2 {
		t.Fatalf("attendu 2 lignes, reçu %d", len(formatted))
	}
	for i, line := range formatted {
		if line == "" {
			t.Errorf("ligne %d vide", i)
		}
		if !contains(line, "my-mod") {
			t.Errorf("ligne %d ne contient pas le nom du module: %s", i, line)
		}
	}
}

func TestFormatTestLogsEmpty(t *testing.T) {
	if formatted := FormatTestLogs("mod", nil); len(formatted) != 0 {
		t.Errorf("attendu 0 lignes, reçu %d", len(formatted))
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
