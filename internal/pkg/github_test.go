package pkg

import (
	"archive/zip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TemplateGitHubRelease(tag string, prerelease bool) ChannelRelease {
	return ChannelRelease{
		TagName:    tag,
		Prerelease: prerelease,
		ZipballURL: "https://example.invalid/zip/" + tag,
	}
}

func TestMatchChannel(t *testing.T) {
	cases := []struct {
		channel Channel
		tag     string
		pre     bool
		want    bool
	}{
		{ChannelStable, "v1.0.0", false, true},
		{ChannelStable, "v1.0.1", false, true},
		{ChannelStable, "v1.1.0-alpha.1", false, false},
		{ChannelStable, "v1.1.0-beta.1", false, false},
		{ChannelStable, "v1.1.0-rc.1", false, false},
		{ChannelStable, "v1.1.0-alpha.1", true, false},
		{ChannelAlpha, "v1.1.0-alpha.1", true, true},
		{ChannelAlpha, "v1.1.0-alpha.2", false, true},
		{ChannelAlpha, "v1.1.0-beta.1", false, false},
		{ChannelAlpha, "v1.0.0", false, false},
		{ChannelBeta, "v1.1.0-beta.1", true, true},
		{ChannelBeta, "v1.1.0-alpha.1", false, false},
		{ChannelBeta, "v1.0.0", false, false},
		{ChannelRC, "v1.1.0-rc.1", true, true},
		{ChannelRC, "v1.1.0-beta.1", false, false},
		{ChannelRC, "v1.0.0", false, false},
	}
	for _, c := range cases {
		got := matchChannel(c.channel, TemplateGitHubRelease(c.tag, c.pre))
		if got != c.want {
			t.Errorf("matchChannel(%q, %q, prerelease=%v) = %v, want %v", c.channel, c.tag, c.pre, got, c.want)
		}
	}
}

func TestIsValidChannel(t *testing.T) {
	for _, ch := range []string{"stable", "alpha", "beta", "rc"} {
		if !IsValidChannel(ch) {
			t.Errorf("%q must be a valid channel", ch)
		}
	}
	for _, ch := range []string{"", "nightly", "STABLE", "rc.1"} {
		if IsValidChannel(ch) {
			t.Errorf("%q must not be a valid channel", ch)
		}
	}
}

func TestResolveReleaseURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		releases := []ChannelRelease{
			TemplateGitHubRelease("v1.2.0-beta.1", true),
			TemplateGitHubRelease("v1.2.0-alpha.2", true),
			TemplateGitHubRelease("v1.1.0", false),
			TemplateGitHubRelease("v1.0.0", false),
		}
		_ = json.NewEncoder(w).Encode(releases)
	}))
	defer srv.Close()

	old := githubReleasesAPIURL
	githubReleasesAPIURL = func(owner, repo string) string {
		return srv.URL
	}
	defer func() { githubReleasesAPIURL = old }()

	got, err := resolveReleaseURL("jetbrains", "liorian-socle", string(ChannelStable))
	if err != nil {
		t.Fatalf("resolveReleaseURL(stable): %v", err)
	}
	if want := "https://example.invalid/zip/v1.1.0"; got != want {
		t.Errorf("resolveReleaseURL(stable) = %q, want %q", got, want)
	}

	got, err = resolveReleaseURL("jetbrains", "liorian-socle", string(ChannelBeta))
	if err != nil {
		t.Fatalf("resolveReleaseURL(beta): %v", err)
	}
	if want := "https://example.invalid/zip/v1.2.0-beta.1"; got != want {
		t.Errorf("resolveReleaseURL(beta) = %q, want %q", got, want)
	}

	if _, err := resolveReleaseURL("jetbrains", "liorian-socle", string(ChannelRC)); err == nil {
		t.Error("a missing channel must return an error")
	}
}

func writeTestZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "template.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return path
}

func TestDownloadAndExtractZip(t *testing.T) {
	zipPath := writeTestZip(t, map[string]string{
		"jetbrains-liorian-socle-abc123/package.json":             `{"name":"liorian-socle"}`,
		"jetbrains-liorian-socle-abc123/README.md":                "template\n",
		"jetbrains-liorian-socle-abc123/library/modules/.gitkeep": "",
	})
	zipData, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zipData)
	}))
	defer srv.Close()

	dest := t.TempDir()
	if err := downloadAndExtractZip(srv.URL, dest, nil); err != nil {
		t.Fatalf("downloadAndExtractZip: %v", err)
	}

	for _, wantFile := range []string{"package.json", "README.md", "library/modules/.gitkeep"} {
		if _, err := os.Stat(filepath.Join(dest, wantFile)); err != nil {
			t.Errorf("extracted file %s missing: %v", wantFile, err)
		}
	}
	if data, err := os.ReadFile(filepath.Join(dest, "README.md")); err != nil || string(data) != "template\n" {
		t.Errorf("README.md = %q, err=%v", data, err)
	}
}

func TestDownloadAndExtractZipReportsProgress(t *testing.T) {
	zipPath := writeTestZip(t, map[string]string{
		"jetbrains-liorian-socle-abc123/package.json": `{"name":"liorian-socle"}`,
	})
	zipData, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Length", strconv.Itoa(len(zipData)))
		_, _ = w.Write(zipData)
	}))
	defer srv.Close()

	dest := t.TempDir()
	var reports []int64
	var lastTotal int64
	if err := downloadAndExtractZip(srv.URL, dest, func(done, total int64) {
		reports = append(reports, done)
		lastTotal = total
	}); err != nil {
		t.Fatalf("downloadAndExtractZip: %v", err)
	}

	if len(reports) == 0 {
		t.Fatal("expected at least one progress report")
	}
	if reports[len(reports)-1] != int64(len(zipData)) {
		t.Errorf("final report = %d, want %d", reports[len(reports)-1], len(zipData))
	}
	if lastTotal != int64(len(zipData)) {
		t.Errorf("reported total = %d, want %d", lastTotal, len(zipData))
	}
}

func TestExtractZipNoTopLevelDir(t *testing.T) {
	zipPath := writeTestZip(t, map[string]string{
		"package.json": `{"name":"liorian-socle"}`,
	})
	dest := t.TempDir()
	if err := extractZip(zipPath, dest); err != nil {
		t.Fatalf("extractZip: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "package.json")); err != nil {
		t.Errorf("flat zip file missing: %v", err)
	}
}
