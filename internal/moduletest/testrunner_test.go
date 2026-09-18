package moduletest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/protorians/liorian-cli/internal/config"
	"github.com/protorians/liorian-cli/internal/module"
	"github.com/protorians/liorian-cli/internal/pkg"
	"github.com/protorians/liorian-cli/internal/tui"
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

// writeInstallablePM writes a fake package manager that also handles dev
// dependency installation: it drops a harmless executable in the current
// directory's node_modules/.bin, mimicking a real install.
func writeInstallablePM(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
case "$1" in
  add|install)
    for last; do :; done
    /bin/mkdir -p node_modules/.bin
    printf '#!/bin/sh\necho "[fixture] %s"\nexit 0\n' "$last" > "node_modules/.bin/$last"
    /bin/chmod 755 "node_modules/.bin/$last"
    echo "[fake] installed $last"
    exit 0
    ;;
  run)
    echo "[fake] run $2"
    exit 0
    ;;
esac
exit 0
`
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

func TestTestModuleSkipsWithoutTestFiles(t *testing.T) {
	root := setupTestProject(t)
	createTestModule(t, root, "my-module")
	moduleDir := filepath.Join(root, config.ExternalModulesDir, "com.test.my-module")
	// A test script is available but the module has no test file: the run
	// must skip instead of launching a runner that would fail.
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
	if result.Status != "SKIPPED" {
		t.Fatalf("Status = %q, want SKIPPED (logs: %v)", result.Status, result.Logs)
	}
	if result.Errors != 0 {
		t.Errorf("Errors = %d, want 0", result.Errors)
	}
	for _, s := range result.Steps {
		if s.ID == "test:com.test.my-module" {
			t.Errorf("aucune étape d'exécution attendue pour un module sans fichier de test : %v", s)
		}
	}
}

func TestModuleHasTests(t *testing.T) {
	write := func(t *testing.T, path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"no test", "index.tsx", false},
		{"test file", filepath.Join("src", "foo.test.ts"), true},
		{"spec file", filepath.Join("src", "foo.spec.tsx"), true},
		{"test directory", filepath.Join("tests", "smoke.ts"), true},
		{"spec directory", filepath.Join("spec", "helpers.ts"), true},
		{"node_modules ignored", filepath.Join("node_modules", "pkg", "foo.test.ts"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, filepath.Join(dir, tc.path), "")
			if got := moduleHasTests(dir); got != tc.want {
				t.Errorf("moduleHasTests(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
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
	writeTestFile(t, moduleDir)

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
	writeTestFile(t, filepath.Join(root, config.ExternalModulesDir, "com.test.my-module"))
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
	writeTestFile(t, moduleDir)

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
	writeTestFile(t, moduleDir)

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
	writeTestFile(t, moduleDir)

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
	if tc := tester.scriptCommand("bun", moduleDir); tc != nil {
		t.Errorf("aucun script test exact ne doit matcher, obtenu %v", tc.cmd)
	}
}

// writeTestFile adds a test file so moduleHasTests reports true.
func writeTestFile(t *testing.T, moduleDir string) {
	t.Helper()
	dir := filepath.Join(moduleDir, "__tests__")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "basic.ts"), []byte("it('works', () => {});\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeRunnerBin drops an executable runner shim in a node_modules/.bin dir.
func writeRunnerBin(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho '[fixture] runner ok'\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestTestModuleUsesConfiguredRunner(t *testing.T) {
	root := setupTestProject(t)
	moduleDir := writeFixtureModule(t, root, "my-module")
	writeTestFile(t, moduleDir)
	writeRunnerBin(t, filepath.Join(moduleDir, "node_modules", ".bin", "vitest"))

	pathShim := t.TempDir()
	writeFakePM(t, filepath.Join(pathShim, "bun"), "ok")
	t.Setenv("PATH", pathShim)

	tester := &Tester{Root: root, Config: config.TestConfig{Runner: "vitest"}}
	result, err := tester.TestModule("com.test.my-module")
	if err != nil {
		t.Fatalf("TestModule: %v", err)
	}
	if result.Status != "OK" {
		t.Fatalf("Status = %q, want OK (logs: %v)", result.Status, result.Logs)
	}
	if result.Runner != "vitest" {
		t.Errorf("Runner = %q, want vitest", result.Runner)
	}
	if !strings.Contains(result.Command, "vitest") {
		t.Errorf("Command = %q, want a vitest run", result.Command)
	}
}

func TestTestModuleInstallsConfiguredRunner(t *testing.T) {
	root := setupTestProject(t)
	moduleDir := writeFixtureModule(t, root, "my-module")
	writeTestFile(t, moduleDir)

	pathShim := t.TempDir()
	writeInstallablePM(t, filepath.Join(pathShim, "npm"))
	t.Setenv("PATH", pathShim)

	tester := &Tester{Root: root, Config: config.TestConfig{Runner: "mocha"}}
	result, err := tester.TestModule("com.test.my-module")
	if err != nil {
		t.Fatalf("TestModule: %v", err)
	}
	if result.Status != "OK" {
		t.Fatalf("Status = %q, want OK (logs: %v)", result.Status, result.Logs)
	}
	if result.Runner != "mocha" {
		t.Errorf("Runner = %q, want mocha", result.Runner)
	}
	if !pkg.FileExists(filepath.Join(moduleDir, "node_modules", ".bin", "mocha")) {
		t.Error("le package de test configuré n'a pas été installé dans le module")
	}
}

func TestTestModuleInstallsRunnerChoice(t *testing.T) {
	root := setupTestProject(t)
	writeFixtureModule(t, root, "my-module")
	writeTestFile(t, filepath.Join(root, config.ExternalModulesDir, "com.test.my-module"))

	pathShim := t.TempDir()
	writeInstallablePM(t, filepath.Join(pathShim, "npm"))
	t.Setenv("PATH", pathShim)

	tester := &Tester{Root: root}
	if !tester.NeedsRunnerChoice("com.test.my-module") {
		t.Fatal("un choix de package de test était attendu")
	}
	tester.SetRunnerChoice("com.test.my-module", RunnerChoice{Package: "mocha", Install: true})

	result, err := tester.TestModule("com.test.my-module")
	if err != nil {
		t.Fatalf("TestModule: %v", err)
	}
	if result.Status != "OK" {
		t.Fatalf("Status = %q, want OK (logs: %v)", result.Status, result.Logs)
	}
	if result.Runner != "mocha" {
		t.Errorf("Runner = %q, want mocha", result.Runner)
	}
}

func TestTestModuleUsesConfiguredPackageManager(t *testing.T) {
	root := setupTestProject(t)
	createTestModule(t, root, "my-module")
	moduleDir := filepath.Join(root, config.ExternalModulesDir, "com.test.my-module")
	if err := os.WriteFile(filepath.Join(moduleDir, "package.json"),
		[]byte(`{"scripts":{"test":"vitest run"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, moduleDir)

	pathShim := t.TempDir()
	writeFakePM(t, filepath.Join(pathShim, "npm"), "ok")
	t.Setenv("PATH", pathShim)

	tester := &Tester{Root: root, ProjectPackageManager: "npm"}
	pm, fromConfig := tester.effectivePackageManager()
	if pm != "npm" || !fromConfig {
		t.Fatalf("effectivePackageManager = (%q, %v), want (npm, true)", pm, fromConfig)
	}

	result, err := tester.TestModule("com.test.my-module")
	if err != nil {
		t.Fatalf("TestModule: %v", err)
	}
	if result.Command != "npm run test" {
		t.Errorf("Command = %q, want npm run test", result.Command)
	}
	if result.PackageManager != "npm" {
		t.Errorf("PackageManager = %q, want npm", result.PackageManager)
	}
}

func TestTestModuleUsesBuiltinBunRunner(t *testing.T) {
	root := setupTestProject(t)
	moduleDir := writeFixtureModule(t, root, "my-module")
	writeTestFile(t, moduleDir)

	pathShim := t.TempDir()
	writeFakePM(t, filepath.Join(pathShim, "bun"), "ok")
	t.Setenv("PATH", pathShim)

	tester := &Tester{Root: root}
	if tester.NeedsRunnerChoice("com.test.my-module") {
		t.Error("aucun choix ne doit être requis avec le runner intégré bun")
	}
	result, err := tester.TestModule("com.test.my-module")
	if err != nil {
		t.Fatalf("TestModule: %v", err)
	}
	if result.Status != "OK" {
		t.Fatalf("Status = %q, want OK (logs: %v)", result.Status, result.Logs)
	}
	if result.Command != "bun test" {
		t.Errorf("Command = %q, want bun test", result.Command)
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
