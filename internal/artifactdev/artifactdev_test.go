package artifactdev

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeModule écrit un module minimally buildable : entrée TS, bootstrap SDK
// simulé (via node_modules/@liorian/sdk), template du document hôte.
func writeModule(t *testing.T) string {
	t.Helper()
	moduleDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(moduleDir, "node_modules", "@liorian", "sdk",
		"infrastructure", "module-runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "node_modules", "@liorian", "sdk",
		"infrastructure", "module-runtime", "module-bootstrap.ts"),
		[]byte("export function bootstrapModule(definition: any): void { void definition; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "main.tsx"), []byte(
		"export function mount(): void {}\nexport function unmount(): void {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "manifest.json"), []byte(
		`{"id":"hello-world","name":"Hello World","version":"1.0.0","entry":"main.tsx"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "index.html"), []byte(
		"<!doctype html><html><head><title>{{manifest.name}}</title></head><body><div id=\"root\"></div><script type=\"module\" src=\"./module.js\"></script></body></html>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return moduleDir
}

func TestBuildProducesBundleAndRenderedDocument(t *testing.T) {
	moduleDir := writeModule(t)
	result, err := Build(BuildOptions{ModuleDir: moduleDir}, func(string) {})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := os.Stat(result.BundlePath); err != nil {
		t.Fatalf("bundle: %v", err)
	}
	document, err := os.ReadFile(result.DocumentPath)
	if err != nil {
		t.Fatal(err)
	}
	// Le placeholder du manifeste est substitué, le script de reload n'est
	// pas injecté au build (c'est le dev-server qui le fait).
	if filepath.Base(result.DocumentPath) != "index.html" {
		t.Fatalf("document path = %s", result.DocumentPath)
	}
	if got := string(document); !filepath.IsAbs(result.BundlePath) || got == "" {
		t.Fatalf("document = %q", got)
	}
	// Le wrapper est retiré de la source du module (§7.7).
	if _, err := os.Stat(filepath.Join(moduleDir, WrapperName)); !os.IsNotExist(err) {
		t.Fatalf("wrapper should be removed after build: %v", err)
	}
}

func TestBuildInjectsStylesheetLinkWhenBundleHasCSS(t *testing.T) {
	moduleDir := writeModule(t)

	// Sans CSS importé, aucun lien n'est injecté.
	plain, err := Build(BuildOptions{ModuleDir: moduleDir}, func(string) {})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	document, _ := os.ReadFile(plain.DocumentPath)
	if strings.Contains(string(document), "module.css") {
		t.Fatalf("link injected without css import: %s", document)
	}

	// Avec un import CSS, esbuild produit module.css et le document hôte le
	// référence — le développeur n'édite jamais son template pour ça.
	if err := os.WriteFile(filepath.Join(moduleDir, "main.tsx"), []byte(
		"import \"./style.css\";\nexport function mount(): void {}\nexport function unmount(): void {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "style.css"), []byte(
		".hello { color: red; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Build(BuildOptions{ModuleDir: moduleDir}, func(string) {})
	if err != nil {
		t.Fatalf("build with css: %v", err)
	}
	document, err = os.ReadFile(result.DocumentPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document), `<link rel="stylesheet" href="./module.css" />`) {
		t.Fatalf("stylesheet link missing: %s", document)
	}
}

func TestInjectStylesheetLinkIsIdempotent(t *testing.T) {
	moduleDir := writeModule(t)
	_, cfg, err := Resolve(BuildOptions{ModuleDir: moduleDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.ArtifactDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.ArtifactDir, BundleCSS), []byte(".a{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	html := `<html><head><link rel="stylesheet" href="./module.css" /></head><body></body></html>`
	if got := InjectStylesheetLink(cfg, html); got != html {
		t.Fatalf("double injection: %s", got)
	}
}

func TestListenWithFallbackSkipsBusyLoopbackPort(t *testing.T) {
	// On occupe le port demandé sur les deux piles : la bascule doit sauter
	// sur le suivant, et libérer les listeners retournés.
	busy, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port

	// IPv6 tenu aussi, pour reproduire le scénario EADDRINUSE ::1.
	busy6, err := net.Listen("tcp6", "[::1]:"+itoa(port))
	if err != nil {
		t.Fatalf("ipv6 busy listener: %v", err)
	}
	defer busy6.Close()

	logs := []string{}
	listeners, gotPort, shifted, err := listenWithFallback("localhost", port, false, func(m string) {
		logs = append(logs, m)
	})
	if err != nil {
		t.Fatalf("listenWithFallback: %v", err)
	}
	defer func() {
		for _, ln := range listeners {
			ln.Close()
		}
	}()
	if gotPort != port+1 {
		t.Fatalf("expected fallback to port %d, got %d (logs: %v)", port+1, gotPort, logs)
	}
	if !shifted {
		t.Fatal("expected shifted=true")
	}
	if len(listeners) == 0 {
		t.Fatal("expected listeners")
	}
}

func TestListenWithFallbackStrictRefusesBusyPort(t *testing.T) {
	busy, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port

	if _, _, _, err := listenWithFallback("localhost", port, true, func(string) {}); err == nil {
		t.Fatal("strict mode should fail on a busy port")
	}
}

func TestResolveDefaultsAndManifestArtifactFields(t *testing.T) {
	moduleDir := writeModule(t)
	manifest, cfg, err := Resolve(BuildOptions{ModuleDir: moduleDir, Dev: DevOptions{Port: 5999}})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ID != "hello-world" {
		t.Fatalf("manifest id = %q", manifest.ID)
	}
	if cfg.Entry != "main.tsx" || cfg.Bundle != "module.js" || cfg.Document != "index.html" {
		t.Fatalf("cfg = %+v", cfg)
	}
	if !filepath.IsAbs(cfg.ArtifactDir) || filepath.Base(filepath.Dir(cfg.ArtifactDir)) != ".liorian" {
		t.Fatalf("artifact dir = %s", cfg.ArtifactDir)
	}
	if cfg.Dev.Port != 5999 || cfg.Dev.Host != "localhost" {
		t.Fatalf("dev = %+v", cfg.Dev)
	}
	// manifest.artifact surcharge bundle/document.
	updated, _ := json.Marshal(map[string]any{
		"id": "hello-world", "name": "Hello World", "version": "1.0.0", "entry": "main.tsx",
		"artifact": map[string]any{"bundle": "custom.js", "document": "host.html"},
	})
	if err := os.WriteFile(filepath.Join(moduleDir, "manifest.json"), updated, 0o644); err != nil {
		t.Fatal(err)
	}
	_, cfg, err = Resolve(BuildOptions{ModuleDir: moduleDir})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bundle != "custom.js" || cfg.Document != "host.html" {
		t.Fatalf("artifact overrides ignored: %+v", cfg)
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var digits []byte
	for v > 0 {
		digits = append([]byte{byte('0' + v%10)}, digits...)
		v /= 10
	}
	return string(digits)
}
