package debug

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/module"
	"github.com/protorians/lior-cli/internal/tui"
)

func createTestModule(t *testing.T, root, id string) {
	t.Helper()
	creator := &module.Creator{Root: root}
	if _, err := creator.Create(module.ModuleSpec{Domain: "com.test." + id, ID: id, Description: "Test module"}); err != nil {
		t.Fatalf("Creator.Create(%q): %v", id, err)
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
	isolateToolchain(t)

	debugger := &Debugger{Root: root}
	result, err := debugger.DebugModule("com.test.my-module")
	if err != nil {
		t.Fatalf("DebugModule: %v", err)
	}
	if result.Module != "com.test.my-module" {
		t.Errorf("Module = %q, want com.test.my-module", result.Module)
	}
	// Without a package manager or build script, should succeed with validation only
	if result.Status != "OK" && result.Status != "WARNING" {
		t.Errorf("Status = %q, want OK or WARNING", result.Status)
	}
}

func TestDebugModuleCtxCancelled(t *testing.T) {
	root := setupDebugProject(t)
	createTestModule(t, root, "my-module")
	moduleDir := filepath.Join(root, config.ExternalModulesDir, "com.test.my-module")
	if err := os.WriteFile(filepath.Join(moduleDir, "package.json"),
		[]byte(`{"scripts":{"build":"echo hi"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	pathShim := t.TempDir()
	writeFakeBin(t, filepath.Join(pathShim, "bun"))
	t.Setenv("PATH", pathShim+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	debugger := &Debugger{Root: root}
	result, err := debugger.DebugModuleCtx(ctx, "com.test.my-module")
	if !errors.Is(err, tui.ErrCancelled) {
		t.Fatalf("err = %v, want tui.ErrCancelled", err)
	}
	if result == nil || result.Status != "CANCELLED" {
		t.Fatalf("status = %v, want CANCELLED", result)
	}
}

// writeSleepingBun writes a fake package manager that prints a line then
// blocks, standing in for a dev server / watcher.
func writeSleepingBun(t *testing.T, path, line string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nif [ \"$1\" = \"run\" ]; then echo \"" + line + "\"; sleep 30; fi\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestDebugModuleDevScriptStopsAfterWindow(t *testing.T) {
	root := setupDebugProject(t)
	createTestModule(t, root, "my-module")
	moduleDir := filepath.Join(root, config.ExternalModulesDir, "com.test.my-module")
	if err := os.WriteFile(filepath.Join(moduleDir, "package.json"),
		[]byte(`{"scripts":{"dev":"vite"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	pathShim := t.TempDir()
	writeSleepingBun(t, filepath.Join(pathShim, "bun"), "ready on :5173")
	t.Setenv("PATH", pathShim+string(os.PathListSeparator)+os.Getenv("PATH"))

	debugger := &Debugger{Root: root, Timeout: 1500 * time.Millisecond}
	start := time.Now()
	result, err := debugger.DebugModule("com.test.my-module")
	if err != nil {
		t.Fatalf("DebugModule: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 6*time.Second {
		t.Fatalf("un script dev doit être arrêté après sa fenêtre, durée %s", elapsed)
	}
	if result.Status != "OK" {
		t.Errorf("status = %q, want OK", result.Status)
	}

	buildNotice := false
	for _, s := range result.Steps {
		if s.ID == "build:com.test.my-module" && s.Status == tui.StatusNotice {
			buildNotice = true
		}
	}
	if !buildNotice {
		t.Errorf("attendu une étape build en notice pour le script dev arrêté : %v", result.Steps)
	}
}

func TestDebugModuleBuildTimeoutFails(t *testing.T) {
	root := setupDebugProject(t)
	createTestModule(t, root, "my-module")
	moduleDir := filepath.Join(root, config.ExternalModulesDir, "com.test.my-module")
	if err := os.WriteFile(filepath.Join(moduleDir, "package.json"),
		[]byte(`{"scripts":{"build":"tsc"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	pathShim := t.TempDir()
	writeSleepingBun(t, filepath.Join(pathShim, "bun"), "compiling")
	t.Setenv("PATH", pathShim+string(os.PathListSeparator)+os.Getenv("PATH"))

	debugger := &Debugger{Root: root, Timeout: 300 * time.Millisecond}
	result, err := debugger.DebugModule("com.test.my-module")
	if err != nil {
		t.Fatalf("DebugModule: %v", err)
	}
	if result.Status != "ERROR" {
		t.Errorf("status = %q, want ERROR (build dépassé)", result.Status)
	}
}

func TestDebugModuleReportsAndRecordsSteps(t *testing.T) {
	root := setupDebugProject(t)
	createTestModule(t, root, "my-module")
	isolateToolchain(t)

	var reported []Step
	debugger := &Debugger{Root: root, Reporter: func(s Step) { reported = append(reported, s) }}
	result, err := debugger.DebugModule("com.test.my-module")
	if err != nil {
		t.Fatalf("DebugModule: %v", err)
	}
	if len(reported) == 0 {
		t.Fatal("l'exécution doit rapporter des étapes en direct")
	}
	if len(result.Steps) == 0 {
		t.Fatal("l'exécution doit enregistrer des étapes pour le résumé")
	}

	// The validation stage is surfaced live as a single aggregate line.
	validation := false
	for _, s := range reported {
		if s.Label == i18n.T("debug.step.validate") {
			validation = true
		}
	}
	if !validation {
		t.Errorf("étape de validation absente du flux : %v", reported)
	}
}

func TestDebugModuleSummaryCountsWarningFinding(t *testing.T) {
	root := setupDebugProject(t)
	createTestModule(t, root, "my-module")
	isolateToolchain(t)

	debugger := &Debugger{Root: root}
	result, err := debugger.DebugModule("com.test.my-module")
	if err != nil {
		t.Fatalf("DebugModule: %v", err)
	}

	// A freshly created module carries a domain warning, so the recap must
	// report at least one warning even though the overall status stays OK.
	counts := tui.Summarize(result.Steps)
	if counts[tui.StatusWarning] == 0 {
		t.Errorf("le résumé doit compter l'avertissement de domaine : %v", result.Steps)
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
	manifestPath := filepath.Join(root, config.ExternalModulesDir, "com.test.bad-mod", "manifest.json")
	manifest, _ := module.LoadManifest(manifestPath)
	manifest.Token = "invalid-token"
	if err := manifest.Save(manifestPath); err != nil {
		t.Fatal(err)
	}

	debugger := &Debugger{Root: root}
	result, err := debugger.DebugModule("com.test.bad-mod")
	if err != nil {
		t.Fatalf("DebugModule: %v", err)
	}
	if result.Status != "ERROR" {
		t.Errorf("Status = %q, want ERROR", result.Status)
	}
	if result.Errors == 0 {
		t.Error("Errors doit être > 0 pour un manifest invalide")
	}
	counts := tui.Summarize(result.Steps)
	if counts[tui.StatusError] == 0 {
		t.Errorf("le résumé doit compter au moins une erreur : %v", result.Steps)
	}
}

func TestDebugAll(t *testing.T) {
	root := setupDebugProject(t)
	createTestModule(t, root, "mod-a")
	createTestModule(t, root, "mod-b")
	isolateToolchain(t)

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
		t.Error("DebugAll sans library/modules/ doit échouer")
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
	moduleDir := filepath.Join(root, config.ExternalModulesDir, "com.test.my-module")

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
	moduleDir := filepath.Join(root, config.ExternalModulesDir, "com.test.my-module")

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
	moduleDir := filepath.Join(root, config.ExternalModulesDir, "com.test.my-module")

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

// isolateToolchain empties PATH down to a bare shim directory (no bundlers,
// no package managers) so the bundler/tsc PATH fallback (spec §5.9) cannot
// leak globally installed executables from the developer machine.
func isolateToolchain(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

// writeFakeBin writes an executable shim that reports the command line and
// exits successfully.
func writeFakeBin(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho \"[fake] $0 $*\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// writeFixtureModule scaffolds a module plus an index.tsx entry and a fake
// bundler/toolchain, so findBundlerBuildCommand can resolve it.
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

func TestFindBundlerBuildCommandModuleNodeModules(t *testing.T) {
	root := setupDebugProject(t)
	moduleDir := writeFixtureModule(t, root, "my-module")

	writeFakeBin(t, filepath.Join(moduleDir, "node_modules", ".bin", "esbuild"))

	d := &Debugger{Root: root}
	build := d.findBundlerBuildCommand(moduleDir)
	if build == nil {
		t.Fatal("esbuild doit être résolu dans le node_modules du module")
	}
	if !build.realBuild {
		t.Error("la commande bundler doit être marquée realBuild")
	}
	wantOut := filepath.Join(moduleDir, "dist")
	if build.outDir != wantOut {
		t.Errorf("outDir = %q, want %q", build.outDir, wantOut)
	}
	if len(build.cmd) < 4 || build.cmd[0] != filepath.Join(moduleDir, "node_modules", ".bin", "esbuild") {
		t.Errorf("commande inattendue : %v", build.cmd)
	}
}

func TestFindBundlerBuildCommandRootNodeModules(t *testing.T) {
	root := setupDebugProject(t)
	moduleDir := writeFixtureModule(t, root, "my-module")

	writeFakeBin(t, filepath.Join(root, "node_modules", ".bin", "tsup"))

	d := &Debugger{Root: root}
	build := d.findBundlerBuildCommand(moduleDir)
	if build == nil {
		t.Fatal("tsup doit être résolu dans le node_modules racine")
	}
	wantOut := filepath.Join(moduleDir, "dist")
	if build.outDir != wantOut {
		t.Errorf("outDir = %q, want %q", build.outDir, wantOut)
	}
	if len(build.cmd) < 4 || build.cmd[0] != filepath.Join(root, "node_modules", ".bin", "tsup") || build.cmd[3] != "--out-dir" {
		t.Errorf("commande inattendue : %v", build.cmd)
	}
}

func TestFindBundlerBuildCommandNone(t *testing.T) {
	root := setupDebugProject(t)
	moduleDir := writeFixtureModule(t, root, "my-module")
	isolateToolchain(t)

	d := &Debugger{Root: root}
	if build := d.findBundlerBuildCommand(moduleDir); build != nil {
		t.Errorf("aucun bundler ne doit matcher, obtenu %v", build.cmd)
	}
}

func TestDebugModuleBundlerBuild(t *testing.T) {
	root := setupDebugProject(t)
	writeFixtureModule(t, root, "my-module")

	pathShim := t.TempDir()
	writeFakeBin(t, filepath.Join(pathShim, "esbuild"))

	t.Setenv("PATH", pathShim+string(os.PathListSeparator)+os.Getenv("PATH"))

	debugger := &Debugger{Root: root}
	result, err := debugger.DebugModule("com.test.my-module")
	if err != nil {
		t.Fatalf("DebugModule: %v", err)
	}
	if result.Status != "OK" {
		t.Fatalf("Status = %q, want OK (logs: %v)", result.Status, result.Logs)
	}
	if !contains(strings.Join(result.Logs, "\n"), "esbuild") {
		t.Errorf("les logs doivent mentionner le bundler esbuild : %v", result.Logs)
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
