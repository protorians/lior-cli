package appconfig

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/protorians/sentient-cli/internal/pkg"
)

func TestResolvedWorkspaceOverridesEmbedded(t *testing.T) {
	SetEmbedded([]byte(`{"applications":{"sentient-auth":{"api":{"baseUrl":"https://embedded.example.com","timeout":15000}}}}`))

	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, FileName),
		[]byte(`{"applications":{"sentient-auth":{"api":{"baseUrl":"https://workspace.example.com","timeout":8000}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(sub); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(wd) }()

	cfg := Resolved("")
	base, ok := cfg.BaseURL(AuthAppID)
	if !ok || base != "https://workspace.example.com" {
		t.Errorf("BaseURL = %q, %v; want https://workspace.example.com, true", base, ok)
	}
	if d := cfg.Timeout(AuthAppID); d != 8*time.Second {
		t.Errorf("Timeout = %v, want 8s", d)
	}
	wantSource, err := Find("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Source() != wantSource {
		t.Errorf("Source = %q, want %q", cfg.Source(), wantSource)
	}
}

func TestResolvedFallsBackToEmbedded(t *testing.T) {
	SetEmbedded([]byte(`{"applications":{"sentient-auth":{"api":{"baseUrl":"https://embedded.example.com","timeout":20000}}}}`))

	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(wd) }()

	cfg := Resolved("")
	base, ok := cfg.BaseURL(AuthAppID)
	if !ok || base != "https://embedded.example.com" {
		t.Errorf("BaseURL = %q, %v; want https://embedded.example.com, true", base, ok)
	}
	if d := cfg.Timeout(AuthAppID); d != 20*time.Second {
		t.Errorf("Timeout = %v, want 20s", d)
	}
	if cfg.Source() != "embedded:"+FileName {
		t.Errorf("Source = %q, want embedded:%s", cfg.Source(), FileName)
	}
}

func TestBaseURLMissing(t *testing.T) {
	SetEmbedded([]byte(`{"applications":{"other-app":{"api":{"baseUrl":"https://x.example.com"}}}}`))
	var cfg Config
	if base, ok := cfg.BaseURL(AuthAppID); ok || base != "" {
		t.Errorf("BaseURL = %q, %v; want empty, false", base, ok)
	}
}

func TestTimeoutDefaults(t *testing.T) {
	var cfg Config
	if d := cfg.Timeout(AuthAppID); d != pkg.DefaultHTTPTimeout {
		t.Errorf("Timeout = %v, want default %v", d, pkg.DefaultHTTPTimeout)
	}
	cfg = Config{Applications: map[string]Application{
		AuthAppID: {API: APIConfig{BaseURL: "https://x.example.com", Timeout: 50}},
	}}
	if d := cfg.Timeout(AuthAppID); d != pkg.DefaultHTTPTimeout {
		t.Errorf("Timeout = %v, want default %v (below schema minimum)", d, pkg.DefaultHTTPTimeout)
	}
}

func TestFindWalksUp(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "x", "y", "z")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, FileName), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	path, err := Find(sub)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if want := filepath.Join(root, FileName); path != want {
		t.Errorf("Find = %q, want %q", path, want)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(sub); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(wd) }()

	if _, err := Find(""); err != nil {
		t.Errorf("Find(cwd) should find %s walked up from cwd: %v", FileName, err)
	}
}