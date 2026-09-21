package pkg

import (
	"archive/zip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

	got, err := resolveRelease("protorians", "liorian-socle", string(ChannelStable))
	if err != nil {
		t.Fatalf("resolveRelease(stable): %v", err)
	}
	if want := "https://example.invalid/zip/v1.1.0"; got.ZipballURL != want {
		t.Errorf("resolveRelease(stable).ZipballURL = %q, want %q", got.ZipballURL, want)
	}

	got, err = resolveRelease("protorians", "liorian-socle", string(ChannelBeta))
	if err != nil {
		t.Fatalf("resolveRelease(beta): %v", err)
	}
	if want := "https://example.invalid/zip/v1.2.0-beta.1"; got.ZipballURL != want {
		t.Errorf("resolveRelease(beta).ZipballURL = %q, want %q", got.ZipballURL, want)
	}

	if _, err := resolveRelease("protorians", "liorian-socle", string(ChannelRC)); err == nil {
		t.Error("a missing channel must return an error")
	}
}

func TestResolveReleaseMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases"):
			_ = json.NewEncoder(w).Encode([]ChannelRelease{
				{TagName: "v1.1.0", TargetCommitish: "main", ZipballURL: "https://example.invalid/zip/v1.1.0"},
			})
		case strings.Contains(r.URL.Path, "/git/ref/tags/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"object": map[string]any{"sha": "deadbeef", "type": "commit"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	oldRel, oldRef := githubReleasesAPIURL, githubTagRefAPIURL
	githubReleasesAPIURL = func(owner, repo string) string {
		return srv.URL + "/repos/" + owner + "/" + repo + "/releases"
	}
	githubTagRefAPIURL = func(owner, repo, tag string) string {
		return srv.URL + "/repos/" + owner + "/" + repo + "/git/ref/tags/" + tag
	}
	defer func() { githubReleasesAPIURL, githubTagRefAPIURL = oldRel, oldRef }()

	info, err := ResolveRelease("protorians", "liorian-socle", string(ChannelStable))
	if err != nil {
		t.Fatalf("ResolveRelease: %v", err)
	}
	if info.Version != "v1.1.0" || info.Channel != "stable" || info.Branch != "main" || info.Commit != "deadbeef" {
		t.Errorf("unexpected release info: %+v", info)
	}
	if !info.Valid() {
		t.Error("a resolved release must carry a download URL")
	}
}

func TestResolveTagCommitDereferencesAnnotatedTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/git/ref/tags/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"object": map[string]any{"sha": "tagobj", "type": "tag"},
			})
		case strings.Contains(r.URL.Path, "/git/tags/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"object": map[string]any{"sha": "commit123"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	oldRef, oldObj := githubTagRefAPIURL, githubTagObjectAPIURL
	githubTagRefAPIURL = func(owner, repo, tag string) string {
		return srv.URL + "/repos/" + owner + "/" + repo + "/git/ref/tags/" + tag
	}
	githubTagObjectAPIURL = func(owner, repo, sha string) string {
		return srv.URL + "/repos/" + owner + "/" + repo + "/git/tags/" + sha
	}
	defer func() { githubTagRefAPIURL, githubTagObjectAPIURL = oldRef, oldObj }()

	if got := resolveTagCommit("protorians", "liorian-socle", "v1.0.0"); got != "commit123" {
		t.Errorf("resolveTagCommit = %q, want commit123", got)
	}
}

func TestResolveTagCommitBestEffortOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	oldRef := githubTagRefAPIURL
	githubTagRefAPIURL = func(owner, repo, tag string) string { return srv.URL }
	defer func() { githubTagRefAPIURL = oldRef }()

	if got := resolveTagCommit("protorians", "liorian-socle", "v1.0.0"); got != "" {
		t.Errorf("resolveTagCommit on API error = %q, want empty", got)
	}
}

func TestDownloadReleaseZipRequiresResolvedURL(t *testing.T) {
	if err := DownloadReleaseZip(ReleaseInfo{Version: "v1.0.0"}, t.TempDir(), nil); err == nil {
		t.Error("a release without a resolved URL must be refused")
	}
}

func TestGithubAPIErrorRateLimited(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{}}
	resp.Header.Set("X-RateLimit-Remaining", "0")
	resp.Header.Set("X-RateLimit-Reset", "1700000000")

	err := githubAPIError(resp)
	if err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("githubAPIError = %v, want a rate-limit error", err)
	}
	if !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Errorf("rate-limit error must hint at GITHUB_TOKEN, got: %v", err)
	}
}

func TestGithubAPIErrorGeneric(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{}}
	err := githubAPIError(resp)
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("githubAPIError = %v, want an HTTP 404 error", err)
	}
}

func TestGithubTokenFromEnv(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	if got := githubToken(); got != "" {
		t.Errorf("githubToken() = %q, want empty", got)
	}
	t.Setenv("GH_TOKEN", "ghp_example")
	if got := githubToken(); got != "ghp_example" {
		t.Errorf("githubToken() = %q, want ghp_example", got)
	}
}

func TestGithubAPIGetSendsToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	t.Setenv("GITHUB_TOKEN", "ghp_test")
	var out map[string]any
	if err := githubAPIGet(srv.URL, &out); err != nil {
		t.Fatalf("githubAPIGet: %v", err)
	}
	if gotAuth != "Bearer ghp_test" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer ghp_test")
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
		"protorians-liorian-socle-abc123/package.json":             `{"name":"liorian-socle"}`,
		"protorians-liorian-socle-abc123/README.md":                "template\n",
		"protorians-liorian-socle-abc123/library/modules/.gitkeep": "",
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
		"protorians-liorian-socle-abc123/package.json": `{"name":"liorian-socle"}`,
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

func TestDownloadAndExtractZipReportsProgressWithoutContentLength(t *testing.T) {
	zipPath := writeTestZip(t, map[string]string{
		"protorians-liorian-socle-abc123/package.json": `{"name":"liorian-socle"}`,
	})
	zipData, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Flushing before the body is complete forces a chunked response, so
		// no Content-Length is advertised.
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		half := len(zipData) / 2
		_, _ = w.Write(zipData[:half])
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		_, _ = w.Write(zipData[half:])
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
		t.Fatal("expected progress reports even without Content-Length")
	}
	if reports[len(reports)-1] != int64(len(zipData)) {
		t.Errorf("final report = %d, want %d", reports[len(reports)-1], len(zipData))
	}
	if lastTotal != -1 {
		t.Errorf("reported total = %d, want -1 (unknown)", lastTotal)
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
