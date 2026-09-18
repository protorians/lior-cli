package pkg

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Channel represents a release channel for template downloads.
type Channel string

const (
	ChannelStable Channel = "stable"
	ChannelAlpha  Channel = "alpha"
	ChannelBeta   Channel = "beta"
	ChannelRC     Channel = "rc"
)

// ValidChannels lists all valid channel values.
var ValidChannels = []string{string(ChannelStable), string(ChannelAlpha), string(ChannelBeta), string(ChannelRC)}

// IsValidChannel reports whether ch is a valid release channel.
func IsValidChannel(ch string) bool {
	for _, v := range ValidChannels {
		if ch == v {
			return true
		}
	}
	return false
}

// ChannelRelease is the minimal representation of a GitHub release needed for
// channel-based template resolution.
type ChannelRelease struct {
	TagName    string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
	ZipballURL string `json:"zipball_url"`
}

// channelPatterns maps a channel to the regex that matches its tag suffix.
var channelPatterns = map[Channel]*regexp.Regexp{
	ChannelAlpha: regexp.MustCompile(`(?i)-alpha`),
	ChannelBeta:  regexp.MustCompile(`(?i)-beta`),
	ChannelRC:    regexp.MustCompile(`(?i)-rc`),
}

// FetchReleaseZip downloads the zip of the latest release matching the given
// channel from the specified GitHub repository and extracts it into dest.
// When overrideURL is non-empty it is used as the zip source instead of the
// GitHub API (useful for tests). onProgress, when non-nil, is called with the
// number of bytes downloaded and the total size as the zip streams in.
func FetchReleaseZip(owner, repo, channel, overrideURL, dest string, onProgress func(done, total int64)) error {
	zipURL := overrideURL
	if zipURL == "" {
		tag, err := resolveReleaseURL(owner, repo, channel)
		if err != nil {
			return err
		}
		zipURL = tag
	}
	return downloadAndExtractZip(zipURL, dest, onProgress)
}

// githubReleasesAPIURL builds the GitHub API endpoint listing a repository's
// releases. It is a variable so tests can point it at a local server.
var githubReleasesAPIURL = func(owner, repo string) string {
	return fmt.Sprintf("https://api.github.com/repos/%s/%s/releases", owner, repo)
}

// resolveReleaseURL queries the GitHub releases API and returns the zipball
// URL of the latest release matching the requested channel.
func resolveReleaseURL(owner, repo, channel string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	url := githubReleasesAPIURL(owner, repo)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to build GitHub API request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", UserAgent())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}

	var releases []ChannelRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", fmt.Errorf("failed to decode GitHub releases: %w", err)
	}

	ch := Channel(channel)
	for _, rel := range releases {
		if matchChannel(ch, rel) {
			return rel.ZipballURL, nil
		}
	}

	return "", fmt.Errorf("no release found for channel %q in %s/%s", channel, owner, repo)
}

// matchChannel reports whether a release belongs to the given channel.
func matchChannel(ch Channel, rel ChannelRelease) bool {
	tag := rel.TagName
	switch ch {
	case ChannelStable:
		return !rel.Prerelease && !channelPatterns[ChannelAlpha].MatchString(tag) &&
			!channelPatterns[ChannelBeta].MatchString(tag) &&
			!channelPatterns[ChannelRC].MatchString(tag)
	case ChannelAlpha, ChannelBeta, ChannelRC:
		return channelPatterns[ch].MatchString(tag)
	default:
		return false
	}
}

// downloadAndExtractZip downloads a zip archive from url and extracts its
// contents into dest. GitHub zip archives contain a single top-level
// directory; its contents are extracted directly into dest. onProgress, when
// non-nil, is called as the download progresses.
func downloadAndExtractZip(url, dest string, onProgress func(done, total int64)) error {
	tmpFile, err := os.CreateTemp("", "liorian-zip-*.zip")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to build download request: %w", err)
	}
	req.Header.Set("User-Agent", UserAgent())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	body := io.Reader(resp.Body)
	if onProgress != nil {
		body = &progressReader{
			r:        resp.Body,
			total:    resp.ContentLength,
			onReport: onProgress,
		}
	}

	if _, err := io.Copy(tmpFile, body); err != nil {
		return fmt.Errorf("failed to save zip: %w", err)
	}
	tmpFile.Close()

	return extractZip(tmpFile.Name(), dest)
}

// progressReader wraps an io.Reader and reports the number of bytes read to a
// callback so a progress bar can be rendered during a download.
type progressReader struct {
	r        io.Reader
	total    int64
	done     int64
	onReport func(done, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	if p.onReport != nil && n > 0 {
		p.onReport(p.done, p.total)
	}
	return n, err
}

// extractZip extracts a zip archive into dest. The GitHub convention of a
// single top-level directory is handled: files inside it are written directly
// into dest.
func extractZip(zipPath, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer r.Close()

	prefix := detectTopLevelPrefix(&r.Reader)

	for _, f := range r.File {
		rel := stripZipPrefix(f.Name, prefix)
		if rel == "" || filepath.IsAbs(rel) || isUnsafeRel(rel) {
			continue
		}

		target := filepath.Join(dest, rel)

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", target, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("failed to create parent directory: %w", err)
		}

		outFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			return fmt.Errorf("failed to create file %s: %w", target, err)
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return fmt.Errorf("failed to open zip entry %s: %w", f.Name, err)
		}

		if _, err := io.Copy(outFile, rc); err != nil {
			rc.Close()
			outFile.Close()
			return fmt.Errorf("failed to extract %s: %w", f.Name, err)
		}
		rc.Close()
		outFile.Close()
	}

	return nil
}

// stripZipPrefix removes the top-level directory prefix from a zip entry name,
// returning the cleaned relative path (or "" when the entry is the prefix
// itself).
func stripZipPrefix(name, prefix string) string {
	cleaned := filepath.Clean(name)
	if prefix != "" {
		cleaned = strings.TrimPrefix(cleaned, strings.TrimSuffix(filepath.Clean(prefix), "/"))
		cleaned = strings.TrimPrefix(cleaned, "/")
	}
	if cleaned == "" || cleaned == "." {
		return ""
	}
	return cleaned
}

// isUnsafeRel reports whether rel escapes dest (zip-slip protection).
func isUnsafeRel(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, "../") || strings.HasPrefix(rel, `..\`)
}

// detectTopLevelPrefix finds the common top-level directory in a zip archive
// (GitHub repos extract to `{owner}-{repo}-{hash}/`).
func detectTopLevelPrefix(r *zip.Reader) string {
	if len(r.File) == 0 {
		return ""
	}
	first := r.File[0].Name
	parts := strings.SplitN(first, "/", 2)
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "/"
}
