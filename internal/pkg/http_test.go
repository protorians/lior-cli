package pkg

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestClient401TokenRefresh verifies that a request rejected with HTTP 401 is
// retried once with a fresh token when TokenRefreshFunc is configured
// (SEC-003 token rotation in the HTTP call path).
func TestClient401TokenRefresh(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		auth := r.Header.Get("Authorization")
		switch {
		case auth == "Bearer stale-token":
			// First attempt with the stale token: reject with 401.
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message":"invalid token","statusCode":401}`))
		case auth == "Bearer fresh-token":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"message":"ok","data":{"ok":true},"statusCode":200}`))
		default:
			t.Errorf("unexpected authorization header: %q", auth)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL)
	client.Token = "stale-token"
	refreshed := false
	client.TokenRefreshFunc = func() (string, error) {
		refreshed = true
		return "fresh-token", nil
	}

	var out struct {
		OK bool `json:"ok"`
	}
	if err := client.Do(context.Background(), "GET", "/api/test", nil, &out); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if !refreshed {
		t.Error("TokenRefreshFunc was not invoked on 401")
	}
	if !out.OK {
		t.Error("retried request did not decode the response body")
	}
	if calls != 2 {
		t.Errorf("expected 2 HTTP calls (401 + retry), got %d", calls)
	}
	if client.Token != "fresh-token" {
		t.Errorf("client token not updated after refresh: %q", client.Token)
	}
}

// TestClient401NoRefresh confirms the 401 is surfaced when no refresh function
// is configured (no infinite loop, original error preserved).
func TestClient401NoRefresh(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"invalid token","statusCode":401}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	client.Token = "stale-token"

	err := client.Do(context.Background(), "GET", "/api/test", nil, nil)
	if err == nil {
		t.Fatal("expected an error for the 401 response")
	}
	if !strings.Contains(err.Error(), "invalid token") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestClient401RefreshFails verifies that a failed refresh surfaces the
// refresh error and never retries the doomed request.
func TestClient401RefreshFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"expired session","statusCode":401}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	client.Token = "stale-token"
	client.TokenRefreshFunc = func() (string, error) {
		return "", &APIError{StatusCode: http.StatusUnauthorized, Message: "refresh failed"}
	}

	err := client.Do(context.Background(), "GET", "/api/test", nil, nil)
	if err == nil {
		t.Fatal("expected an error when the token refresh fails")
	}
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Message != "refresh failed" {
		t.Errorf("expected the refresh error to surface, got %v", err)
	}
}

// TestRaitonEnvelopeDecode guard the Raiton envelope decoding used everywhere.
func TestRaitonEnvelopeDecode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message":"ok","data":{"items":[{"token":"m_1","name":"demo"}]},"statusCode":200}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	var out struct {
		Items []struct {
			Token string `json:"token"`
			Name  string `json:"name"`
		} `json:"items"`
	}
	if err := client.Do(context.Background(), "GET", "/api/test", nil, &out); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if len(out.Items) != 1 || out.Items[0].Token != "m_1" || out.Items[0].Name != "demo" {
		t.Errorf("unexpected decoded payload: %+v", out.Items)
	}
}

// TestRaitonEnvelopeNullData confirms a `data: null` envelope yields no error
// (used by endpoints without a payload).
func TestRaitonEnvelopeNullData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message":"ok","data":null,"statusCode":200}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	if err := client.Do(context.Background(), "GET", "/api/test", nil, &json.RawMessage{}); err != nil {
		t.Fatalf("Do: %v", err)
	}
}