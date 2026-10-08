package artifactbind

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/protorians/lior-cli/internal/devlink"
	"github.com/protorians/lior-cli/internal/socle"
)

// writeModule écrit un module minimal (manifest.json + .liorian/artifact/) et
// un socle minimal (library/, serve.mjs) dans deux répertoires temporaires.
func writeModule(t *testing.T) (moduleDir, socleDir string) {
	t.Helper()
	base := t.TempDir()
	moduleDir = filepath.Join(base, "modules", "hello-world")
	socleDir = filepath.Join(base, "liorian-socle")

	if err := os.MkdirAll(filepath.Join(moduleDir, ".liorian", "artifact"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"hello-world","version":"1.2.0","domain":"mod.liorian.hello-world"}`
	if err := os.WriteFile(filepath.Join(moduleDir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, ".liorian", "artifact", "module.js"), []byte("export {};"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(socleDir, "library", "modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(socleDir, "serve.mjs"), []byte("// socle"), 0o644); err != nil {
		t.Fatal(err)
	}
	return moduleDir, socleDir
}

func TestBindWritesLibraryLayoutAndEnv(t *testing.T) {
	moduleDir, socleDir := writeModule(t)

	result, err := Bind(Options{SocleDir: socleDir, ModuleDir: moduleDir})
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	// Layout de bibliothèque : manifeste + artefact liés, pointeur `current`.
	versionDir := filepath.Join(socleDir, "library", "modules", "hello-world", "1.2.0")
	if _, err := os.Stat(versionDir); err != nil {
		t.Fatalf("version dir: %v", err)
	}
	for _, linked := range []string{"manifest.json", "artifact"} {
		info, err := os.Lstat(filepath.Join(versionDir, linked))
		if err != nil {
			t.Fatalf("%s: %v", linked, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s should be a symlink", linked)
		}
	}
	if current := strings.TrimSpace(string(mustRead(t, filepath.Join(versionDir, "..", "current")))); current != "1.2.0" {
		t.Fatalf("current pointer = %q", current)
	}
	if _, err := os.Stat(filepath.Join(versionDir, BindMarkerFile)); err != nil {
		t.Fatalf("bind marker: %v", err)
	}

	// Câblage HMR du socle.
	env := string(mustRead(t, filepath.Join(socleDir, EnvFile)))
	for _, key := range []string{
		DevModulesURLKey + "=https://localhost:5178",
		DevModulesIDsKey + "=hello-world",
	} {
		if !strings.Contains(env, key) {
			t.Fatalf(".env.local missing %s:\n%s", key, env)
		}
	}

	// Lien de développement du module.
	link, ok := devlink.Read(moduleDir)
	if !ok {
		t.Fatal("dev link missing")
	}
	if link.SocleDir != socleDir || link.DevPort != 5178 || link.SocleScheme != "https" {
		t.Fatalf("dev link = %+v", link)
	}
	if result.Mode != ModeSymlink {
		t.Fatalf("mode = %s", result.Mode)
	}
}

func TestBindIsIdempotentAndRetiresStaleVersions(t *testing.T) {
	moduleDir, socleDir := writeModule(t)
	if _, err := Bind(Options{SocleDir: socleDir, ModuleDir: moduleDir}); err != nil {
		t.Fatal(err)
	}
	// Re-packé sous une nouvelle version : l'ancienne version liée doit
	// disparaître, le pointeur suit.
	manifest := `{"id":"hello-world","version":"1.3.0","domain":"mod.liorian.hello-world"}`
	if err := os.WriteFile(filepath.Join(moduleDir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Bind(Options{SocleDir: socleDir, ModuleDir: moduleDir}); err != nil {
		t.Fatal(err)
	}
	moduleRoot := filepath.Join(socleDir, "library", "modules", "hello-world")
	if _, err := os.Stat(filepath.Join(moduleRoot, "1.2.0")); !os.IsNotExist(err) {
		t.Fatalf("stale bound version still present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(moduleRoot, "1.3.0")); err != nil {
		t.Fatalf("new version: %v", err)
	}
}

func TestUnbindRemovesOnlyOwnedBinding(t *testing.T) {
	moduleDir, socleDir := writeModule(t)
	if _, err := Bind(Options{SocleDir: socleDir, ModuleDir: moduleDir}); err != nil {
		t.Fatal(err)
	}

	result, err := Unbind(Options{SocleDir: socleDir, ModuleDir: moduleDir})
	if err != nil {
		t.Fatalf("unbind: %v", err)
	}
	if result.RemovedDir == "" {
		t.Fatal("expected the bound library entry to be removed")
	}
	if _, err := os.Stat(filepath.Join(socleDir, "library", "modules", "hello-world")); !os.IsNotExist(err) {
		t.Fatalf("library entry still present: %v", err)
	}
	if _, ok := devlink.Read(moduleDir); ok {
		t.Fatal("dev link should be removed")
	}
	env := string(mustRead(t, filepath.Join(socleDir, EnvFile)))
	if strings.Contains(env, DevModulesIDsKey) || strings.Contains(env, DevModulesURLKey) {
		t.Fatalf("env keys should be removed when the id list empties:\n%s", env)
	}

	// Une installation réelle (sans marqueur) n'est jamais touchée.
	realInstall := filepath.Join(socleDir, "library", "modules", "hello-world", "1.2.0")
	if err := os.MkdirAll(realInstall, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realInstall, "manifest.json"), []byte(`{"id":"hello-world"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Unbind(Options{SocleDir: socleDir, ModuleDir: moduleDir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(realInstall); err != nil {
		t.Fatalf("real install must survive unbind: %v", err)
	}
}

func TestBindRegistersModuleDevRegistry(t *testing.T) {
	moduleDir, socleDir := writeModule(t)

	result, err := Bind(Options{SocleDir: socleDir, ModuleDir: moduleDir})
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	// Le registre des modules en dev est tenu à jour, chemin absolu à l'appui.
	if result.ModuleDevPath != filepath.Join(socleDir, ".liorian", "module.dev.json") {
		t.Fatalf("ModuleDevPath = %q", result.ModuleDevPath)
	}
	entries, err := socle.ReadModuleDev(socleDir)
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %v, want exactly one", entries)
	}
	if entries[0].Identifier != "hello-world" {
		t.Fatalf("identifier = %q", entries[0].Identifier)
	}
	abs, err := filepath.Abs(moduleDir)
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Dir != abs {
		t.Fatalf("dir = %q, want %q", entries[0].Dir, abs)
	}

	// Re-bind : une seule entrée (idempotent).
	if _, err := Bind(Options{SocleDir: socleDir, ModuleDir: moduleDir}); err != nil {
		t.Fatalf("rebind: %v", err)
	}
	if entries, err = socle.ReadModuleDev(socleDir); err != nil || len(entries) != 1 {
		t.Fatalf("entries after rebind = %v (%v), want exactly one", entries, err)
	}

	// Unbind retire l'entrée du registre.
	unbound, err := Unbind(Options{SocleDir: socleDir, ModuleDir: moduleDir})
	if err != nil {
		t.Fatalf("unbind: %v", err)
	}
	if !unbound.ModuleDevRemoved {
		t.Fatal("ModuleDevRemoved should be true")
	}
	if entries, err = socle.ReadModuleDev(socleDir); err != nil {
		t.Fatalf("read registry: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %v, want empty registry", entries)
	}
}

func TestBindRejectsNonSocleDirectory(t *testing.T) {
	moduleDir, socleDir := writeModule(t)
	empty := filepath.Join(socleDir, "..", "not-a-socle")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Bind(Options{SocleDir: empty, ModuleDir: moduleDir}); err == nil {
		t.Fatal("expected bind to fail on a non-socle directory")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
