package e2e

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestUpdateNotification wraps the S-015 / NFR-006 contract end to end: a
// CLI built with a real SemVer version checks a release endpoint once a day
// and prints an informational notification when a newer release exists —
// nothing is downloaded or installed.
//
// The default E2E binary reports `dev` (which skips the check entirely), so a
// dedicated binary with an injected version is built here.
func TestUpdateNotification(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "liorian-update")
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build",
		"-ldflags", "-X main.version=0.14.0", "-o", bin, ".")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build versioned CLI: %v\n%s", err, out)
	}

	// The release endpoint serves a newer version and records the requests:
	// a download would show up as an extra request (e.g. to an asset path).
	var mu sync.Mutex
	var hits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v99.0.0"}`))
	}))
	defer srv.Close()

	// Isolate temp cache and vault; opt-in even if an ambient CI is present.
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("LIORIAN_CLI_STORE", "file")
	t.Setenv("LIORIAN_CLI_UPDATE", "1")
	t.Setenv("LIORIAN_CLI_UPDATE_URL", srv.URL)

	cmd := exec.Command(bin, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run CLI: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Update available") {
		t.Errorf("notification d'update absente:\n%s", out)
	}
	if !strings.Contains(string(out), "https://github.com/protorians/lior-cli/releases/latest") {
		t.Errorf("la notification doit pointer vers la page de release:\n%s", out)
	}

	// S-015: notification seule — exactement une requête (le check), jamais de
	// téléchargement automatique de la nouvelle version.
	mu.Lock()
	defer mu.Unlock()
	if got := len(hits); got != 1 {
		t.Errorf("le check a émis %d requête(s), une seule attendue (pas de download): %v", got, hits)
	}
	for _, h := range hits {
		if !strings.HasPrefix(h, "GET ") {
			t.Errorf("requête non-GET émise par l'update check: %s", h)
		}
	}
}
