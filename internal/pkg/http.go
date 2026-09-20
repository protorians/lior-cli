package pkg

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// DefaultHTTPTimeout is the default timeout for API calls; the effective
// value may be overridden per application by `app.config.json` (`api.timeout`).
const DefaultHTTPTimeout = 30 * time.Second

var cliVersion = "dev"

// SetCLIVersion sets the CLI version used in the User-Agent header.
func SetCLIVersion(version string) {
	if version != "" {
		cliVersion = version
	}
}

var (
	osPlatformOnce sync.Once
	osPlatformVal  string
)

// UserAgent returns the User-Agent string sent with every HTTP request.
// `Go-http-client/1.1` is the engine token Go's `net/http` transport uses by
// default; `Senteints/<version>` is the CLI (the version is injected via
// SetCLIVersion).
func UserAgent() string {
	return fmt.Sprintf("protorians/5.0 (%s) Go-http-client/1.1 Senteints/%s", osPlatformToken(), cliVersion)
}

// osPlatformToken returns a browser-style platform token describing the real
// OS running the CLI (family, architecture and version when available).
func osPlatformToken() string {
	osPlatformOnce.Do(func() {
		var b strings.Builder
		switch runtime.GOOS {
		case "darwin":
			b.WriteString("Macintosh; ")
			if runtime.GOARCH == "arm64" {
				b.WriteString("Mac OS X ")
			} else {
				b.WriteString("Intel Mac OS X ")
			}
			b.WriteString(darwinVersion())
		case "linux":
			b.WriteString("X11; Linux ")
			b.WriteString(normalizeArch(runtime.GOARCH))
			if v := linuxVersion(); v != "" {
				b.WriteString(" (")
				b.WriteString(v)
				b.WriteString(")")
			}
		case "windows":
			b.WriteString("Windows NT 10.0; Win64; ")
			if runtime.GOARCH == "arm64" {
				b.WriteString("ARM64")
			} else {
				b.WriteString("x64")
			}
		default:
			b.WriteString(runtime.GOOS)
			b.WriteString("; ")
			b.WriteString(runtime.GOARCH)
		}
		osPlatformVal = b.String()
	})
	return osPlatformVal
}

// darwinVersion returns the real macOS product version (e.g. "26.7").
func darwinVersion() string {
	out, err := exec.Command("sw_vers", "-productVersion").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// linuxVersion returns the real distro version from `/etc/os-release`.
func linuxVersion() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VERSION_ID=") {
			v := strings.TrimSpace(strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), `"`))
			if v != "" {
				return v
			}
		}
	}
	return ""
}

// normalizeArch maps Go architecture names to browser-style ones.
func normalizeArch(arch string) string {
	switch arch {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		return arch
	}
}

// RaitonResponse is the standard Raiton API envelope `{message, data, statusCode}`
// returned by every endpoint of `liorian-api-core` / `liorian-api-connect`
// (see `RaitonResponses(message, data, statusCode)` in the Raiton framework).
type RaitonResponse struct {
	Message    string          `json:"message"`
	Data       json.RawMessage `json:"data"`
	StatusCode int             `json:"statusCode"`
}

// Client is a thin JSON-aware HTTP client used to talk to the liorian APIs.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	Token   string
	// TokenRefreshFunc is an optional callback invoked when a request fails
	// with HTTP 401 (unauthorized). It must return a fresh bearer token.
	// When set, the client retries the failed request once with the new token.
	TokenRefreshFunc func() (string, error)
}

// NewClient builds a client for the given base URL with the default timeout.
func NewClient(baseURL string) *Client {
	return NewClientWithTimeout(baseURL, DefaultHTTPTimeout)
}

// NewClientWithTimeout builds a client for the given base URL and timeout.
func NewClientWithTimeout(baseURL string, timeout time.Duration) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{Timeout: timeout},
	}
}

// Do performs a request with the given method, path, body and decodes the
// `data` field of the Raiton envelope into out (when out is not nil).
//
// When the server responds with HTTP 401 and a TokenRefreshFunc is configured,
// the client refreshes the bearer token and retries the request once.
func (c *Client) Do(ctx context.Context, method, path string, body any, out any) error {
	return c.do(ctx, method, path, body, out, false)
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any, retried bool) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to serialize the request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to build the request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent())
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read the response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized && !retried && c.TokenRefreshFunc != nil {
		newToken, refreshErr := c.TokenRefreshFunc()
		if refreshErr == nil && newToken != "" {
			c.Token = newToken
			return c.do(ctx, method, path, body, out, true)
		}
		if refreshErr != nil {
			return refreshErr
		}
	}

	if resp.StatusCode >= 400 {
		if apiErr := parseAPIError(resp.StatusCode, data); apiErr != nil {
			return apiErr
		}
		return fmt.Errorf("HTTP %d response: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	if out != nil && len(data) > 0 {
		var envelope RaitonResponse
		if err := json.Unmarshal(data, &envelope); err != nil {
			return fmt.Errorf("failed to decode the response: %w", err)
		}
		if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
			return nil
		}
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("failed to decode the response: %w", err)
		}
	}
	return nil
}

// parseAPIError extracts an APIError from an HTTP error body. Errors follow
// the Raiton envelope (`{message, statusCode, data}`); legacy flat bodies
// `{code, message}` are still accepted.
func parseAPIError(statusCode int, data []byte) *APIError {
	var envelope RaitonResponse
	if json.Unmarshal(data, &envelope) == nil && envelope.Message != "" {
		code := ""
		var legacy struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &legacy) == nil {
			code = legacy.Code
		}
		return &APIError{StatusCode: statusCode, Code: code, Message: envelope.Message}
	}
	var apiErr APIError
	if json.Unmarshal(data, &apiErr) == nil && apiErr.Message != "" {
		apiErr.StatusCode = statusCode
		return &apiErr
	}
	return nil
}

// APIError represents an error returned by the liorian API.
type APIError struct {
	StatusCode int    `json:"-"`
	Code       string `json:"code,omitempty"`
	Message    string `json:"message,omitempty"`
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s (code=%s, HTTP %d)", e.Message, e.Code, e.StatusCode)
	}
	return fmt.Sprintf("%s (HTTP %d)", e.Message, e.StatusCode)
}

// IsNotFound reports whether the error is an HTTP 404.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}
