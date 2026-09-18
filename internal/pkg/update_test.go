package pkg

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"v0.1.0", "0.1.0"},
		{"0.2.0", "0.2.0"},
		{"  v1.0.0  ", "1.0.0"},
		{"v1.2.3-beta+build", "1.2.3-beta+build"},
	}
	for _, tt := range tests {
		got := normalizeVersion(tt.input)
		if got != tt.want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseSemver(t *testing.T) {
	tests := []struct {
		input string
		maj   int
		min   int
		pat   int
	}{
		{"0.1.0", 0, 1, 0},
		{"1.0.0", 1, 0, 0},
		{"1.2.3", 1, 2, 3},
		{"v2.0.0-beta.1", 2, 0, 0},
		{"10.20.30", 10, 20, 30},
		{"", 0, 0, 0},
	}
	for _, tt := range tests {
		p := parseSemver(tt.input)
		if p[0] != tt.maj || p[1] != tt.min || p[2] != tt.pat {
			t.Errorf("parseSemver(%q) = [%d,%d,%d], want [%d,%d,%d]",
				tt.input, p[0], p[1], p[2], tt.maj, tt.min, tt.pat)
		}
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		current, latest string
		want            bool
	}{
		{"0.1.0", "0.2.0", true},
		{"0.2.0", "0.1.0", false},
		{"1.0.0", "2.0.0", true},
		{"1.0.0", "1.0.0", false},
		{"1.0.0", "1.0.1", true},
		{"1.9.9", "2.0.0", true},
		{"2.0.0", "1.9.9", false},
	}
	for _, tt := range tests {
		got := isNewer(tt.current, tt.latest)
		if got != tt.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
		}
	}
}

func TestCheckForUpdateSkipsDev(t *testing.T) {
	msg := CheckForUpdate("dev")
	if msg != "" {
		t.Errorf("dev version doit retourner vide, got %q", msg)
	}
}

func TestCheckForUpdateSkipsEmpty(t *testing.T) {
	msg := CheckForUpdate("")
	if msg != "" {
		t.Errorf("version vide doit retourner vide, got %q", msg)
	}
}

func TestCheckForUpdateCached(t *testing.T) {
	// Write a recent cache entry to skip the actual check
	cache := cachePath()
	if err := os.MkdirAll(filepath.Dir(cache), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache, []byte(time.Now().Format(time.RFC3339)), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(cache)

	msg := CheckForUpdate("0.1.0")
	if msg != "" {
		t.Errorf("cache récent doit éviter la vérification, got %q", msg)
	}
}

func TestCheckForUpdateNewerAvailable(t *testing.T) {
	// Clear cache
	os.Remove(cachePath())

	// Mock GitHub API returning a newer version
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(GitHubRelease{TagName: "v99.0.0"}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()

	// Test the parsing logic directly via the helper
	latest, err := fetchLatestFromURL(server.URL)
	if err != nil {
		t.Fatalf("fetchLatestFromURL: %v", err)
	}
	if latest != "v99.0.0" {
		t.Errorf("latest = %q, want v99.0.0", latest)
	}

	// Test isNewer with the comparison
	if !isNewer("0.1.0", normalizeVersion(latest)) {
		t.Error("99.0.0 doit être plus récent que 0.1.0")
	}
}

func TestCheckForUpdateUpToDate(t *testing.T) {
	os.Remove(cachePath())

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(GitHubRelease{TagName: "v0.1.0"}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()

	latest, err := fetchLatestFromURL(server.URL)
	if err != nil {
		t.Fatalf("fetchLatestFromURL: %v", err)
	}
	if isNewer("0.1.0", normalizeVersion(latest)) {
		t.Error("même version ne doit pas signaler de mise à jour")
	}
}

func TestCheckForUpdateNetworkError(t *testing.T) {
	os.Remove(cachePath())

	// Server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := fetchLatestFromURL(server.URL)
	if err == nil {
		t.Error("erreur HTTP 500 doit être retournée")
	}
}

func TestCacheReadWrite(t *testing.T) {
	cache := cachePath()
	os.Remove(cache)
	defer os.Remove(cache)

	// No cache initially
	if _, ok := readCache(); ok {
		t.Error("pas de cache attendu initialement")
	}

	// Write cache
	writeCache(time.Now())
	ts, ok := readCache()
	if !ok {
		t.Fatal("cache doit être lisible après écriture")
	}
	if time.Since(ts) > time.Second {
		t.Errorf("cache trop ancien: %v", ts)
	}
}

func TestCacheExpired(t *testing.T) {
	cache := cachePath()
	if err := os.MkdirAll(filepath.Dir(cache), 0o755); err != nil {
		t.Fatal(err)
	}
	// Write a cache from 2 days ago
	old := time.Now().Add(-48 * time.Hour)
	if err := os.WriteFile(cache, []byte(old.Format(time.RFC3339)), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(cache)

	ts, ok := readCache()
	if !ok {
		t.Fatal("cache doit être lisible")
	}
	if time.Since(ts) < CacheDuration {
		t.Error("cache de 2 jours doit être considéré expiré")
	}
}

func TestParseSemverInvalid(t *testing.T) {
	p := parseSemver("not-a-version")
	if p[0] != 0 || p[1] != 0 || p[2] != 0 {
		t.Errorf("version invalide doit retourner [0,0,0], got %v", p)
	}
}

func TestSkipUpdate(t *testing.T) {
	cases := []struct {
		name  string
		skip  string
		ci    string
		optIn string
		want  bool
	}{
		{"aucun env", "", "", "", false},
		{"skip=1", "1", "", "", true},
		{"skip=true", "true", "", "", true},
		{"skip=0 reste en ligne", "0", "", "", false},
		{"skip=inconnu désactive", "oui", "", "", true},
		{"CI sans opt-in", "", "present", "", true},
		{"CI avec opt-in", "", "present", "1", false},
		{"CI + skip=1", "1", "present", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// "" means unset; only set the variable when it has a value.
			setOrUnset(t, SkipUpdateEnvVar, tc.skip)
			setOrUnset(t, "CI", tc.ci)
			setOrUnset(t, "LIORIAN_CLI_UPDATE", tc.optIn)
			if got := skipUpdate(); got != tc.want {
				t.Errorf("skipUpdate() = %v, want %v", got, tc.want)
			}
		})
	}
}

// setOrUnset assigns value to key across the test, or removes the variable when
// value is empty. The previous state is restored at cleanup.
func setOrUnset(t *testing.T, key, value string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if value == "" {
		_ = os.Unsetenv(key)
	} else {
		_ = os.Setenv(key, value)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

// fetchLatestFromURL is a test helper that fetches from a custom URL.
func fetchLatestFromURL(baseURL string) (string, error) {
	resp, err := http.Get(baseURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", &APIError{StatusCode: resp.StatusCode, Message: "HTTP error"}
	}
	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	return release.TagName, nil
}
