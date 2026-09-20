package pkg

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jetbrains/lior-cli/internal/i18n"
)

// GitHubRelease is the minimal representation of a GitHub release for version
// comparison (NFR-006: auto-update detection).
type GitHubRelease struct {
	TagName string `json:"tag_name"`
}

// UpdateCheckURL is the GitHub API endpoint for the latest release.
const UpdateCheckURL = "https://api.github.com/repos/jetbrains/lior-cli/releases/latest"

// UpdateCheckURLEnv overrides the update-check endpoint (tests, mirrors,
// self-hosted release servers). It must return the JSON shape of a GitHub
// release (`{"tag_name": "v…"}`).
const UpdateCheckURLEnv = "LIORIAN_CLI_UPDATE_URL"

// CacheDuration controls how often the update check runs (once per day).
const CacheDuration = 24 * time.Hour

// cachePath returns the path to the update check cache file.
func cachePath() string {
	dir := filepath.Join(os.TempDir(), "lorian-cli")
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, ".update-check")
}

// SkipUpdateEnvVar disables the network update check when set to a truthy
// value (e.g. `1`, `true`). It lets CI/CD runs stay off the network.
const SkipUpdateEnvVar = "LIORIAN_CLI_SKIP_UPDATE"

// CheckForUpdate queries the release API and returns a notification message
// when a newer version is available. Returns empty string when the CLI is
// up-to-date or when the check should be skipped (cached, offline,
// `LIORIAN_CLI_SKIP_UPDATE` set, running in CI, etc.).
//
// S-015 / NFR-006: the check is informational only — it never downloads or
// installs the new version (spec: "notification, pas de mise à jour forcée").
func CheckForUpdate(currentVersion string) string {
	if currentVersion == "" || currentVersion == "dev" {
		return ""
	}

	// NFR-006 / CI: allow disabling the network call entirely.
	if skipUpdate() {
		return ""
	}

	// Respect cache: skip if checked less than CacheDuration ago.
	if cached, ok := readCache(); ok {
		if time.Since(cached) < CacheDuration {
			return ""
		}
	}

	latestVersion, err := fetchLatestVersion()
	if err != nil {
		return ""
	}

	writeCache(time.Now())

	currentClean := normalizeVersion(currentVersion)
	latestClean := normalizeVersion(latestVersion)

	// Nothing to report when the latest release is not strictly newer than
	// the running version (equal, or the running version is ahead).
	if !isNewer(currentClean, latestClean) {
		return ""
	}

	return i18n.Tf("update.available", currentClean, latestClean)
}

// skipUpdate reports whether the network check is disabled by configuration.
// A CLI-invokable env var is honoured first (only a truthy value disables,
// so `LIORIAN_CLI_SKIP_UPDATE=0` stays online); otherwise the presence of a
// CI environment is taken as a signal to stay offline unless an explicit opt-in.
func skipUpdate() bool {
	if v := os.Getenv(SkipUpdateEnvVar); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
		// Unknown value: treat the presence of the variable as a disable.
		return true
	}
	if _, ok := os.LookupEnv("CI"); ok {
		return os.Getenv("LIORIAN_CLI_UPDATE") == ""
	}
	return false
}

// updateCheckURL returns the endpoint to query for the latest release: the
// `LIORIAN_CLI_UPDATE_URL` override when set, the GitHub API otherwise.
func updateCheckURL() string {
	if url := strings.TrimSpace(os.Getenv(UpdateCheckURLEnv)); url != "" {
		return url
	}
	return UpdateCheckURL
}

// fetchLatestVersion retrieves the latest release tag.
func fetchLatestVersion() (string, error) {
	return fetchLatestFromURL(updateCheckURL())
}

// fetchLatestFromURL retrieves the latest release tag from a custom release
// endpoint. The response is expected to have a GitHub-shaped payload
// (`{"tag_name": "v…"}`).
func fetchLatestFromURL(url string) (string, error) {
	if strings.TrimSpace(url) == "" {
		return "", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", UserAgent())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("update API returned %d", resp.StatusCode)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	return release.TagName, nil
}

// normalizeVersion strips a leading "v" and whitespace.
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	return v
}

// isNewer returns true when latest is strictly newer than current using
// simple semver comparison (major.minor.patch).
func isNewer(current, latest string) bool {
	cur := parseSemver(current)
	lat := parseSemver(latest)

	if lat[0] != cur[0] {
		return lat[0] > cur[0]
	}
	if lat[1] != cur[1] {
		return lat[1] > cur[1]
	}
	return lat[2] > cur[2]
}

// parseSemver splits a version string into [major, minor, patch].
func parseSemver(v string) [3]int {
	var parts [3]int
	v = strings.TrimPrefix(v, "v")
	// Strip any pre-release or build metadata
	if idx := strings.IndexAny(v, "-+"); idx != -1 {
		v = v[:idx]
	}
	segments := strings.SplitN(v, ".", 3)
	for i, s := range segments {
		if i >= 3 {
			break
		}
		parts[i], _ = strconv.Atoi(s)
	}
	return parts
}

func readCache() (time.Time, bool) {
	data, err := os.ReadFile(cachePath())
	if err != nil {
		return time.Time{}, false
	}
	ts := strings.TrimSpace(string(data))
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func writeCache(t time.Time) {
	_ = os.WriteFile(cachePath(), []byte(t.Format(time.RFC3339)), 0o644)
}
