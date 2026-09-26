package pkg

import (
	"strings"
	"testing"
)

// TestCanonicalJSONSortedKeys pins the byte-level contract shared with the
// server-side `canonicalJson` (module-artifact-crypto.util): keys sorted
// recursively, no whitespace.
func TestCanonicalJSONSortedKeys(t *testing.T) {
	value := map[string]any{
		"version":  "1.2.3",
		"checksum": "abc",
		"nested":   map[string]any{"z": 1, "a": true},
		"list":     []any{"b", "a"},
	}
	got, err := CanonicalJSON(value)
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	want := `{"checksum":"abc","list":["b","a"],"nested":{"a":true,"z":1},"version":"1.2.3"}`
	if string(got) != want {
		t.Fatalf("canonical form mismatch:\n got: %s\nwant: %s", got, want)
	}
}

// TestCanonicalJSONNoHTMLEscaping guards byte-identity with JSON.stringify:
// <, > and & must stay literal, unlike Go's default json.Marshal.
func TestCanonicalJSONNoHTMLEscaping(t *testing.T) {
	got, err := CanonicalJSON(map[string]any{"url": "https://acme.dev?a=1&b=<x>"})
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	if !strings.Contains(string(got), `a=1&b=<x>`) {
		t.Fatalf("HTML characters must not be escaped, got: %s", got)
	}
}

func TestCanonicalJSONNilAndNumbers(t *testing.T) {
	got, err := CanonicalJSON(map[string]any{"a": nil, "b": 1})
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	if string(got) != `{"a":null,"b":1}` {
		t.Fatalf("unexpected canonical form: %s", got)
	}
}
