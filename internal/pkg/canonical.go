package pkg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// CanonicalJSON serializes a value as canonical JSON: object keys sorted
// recursively, no whitespace, no HTML escaping, UTF-8 output.
//
// This is the Go counterpart of `canonicalJson` in the shared
// `module-artifact-crypto.util` (api-resources) — the signature contract of
// the module publication chain (spec module-installation §7.1). Any divergence
// (key order, escaping) makes the artefact signature unverifiable by the
// server; the two implementations must stay byte-identical.
func CanonicalJSON(value any) ([]byte, error) {
	decoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize the value: %w", err)
	}
	// Re-parse with UseNumber so numbers keep their literal form (matching
	// JSON.stringify on the server side), then emit canonically.
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.UseNumber()
	var tree any
	if err := decoder.Decode(&tree); err != nil {
		return nil, fmt.Errorf("failed to decode the value: %w", err)
	}
	var out bytes.Buffer
	if err := encodeCanonical(&out, tree); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// encodeCanonical writes the canonical form of a decoded JSON tree: objects
// with sorted keys and no spaces, arrays in order, strings escaped exactly
// like JSON.stringify (control characters, but not <, >, &).
func encodeCanonical(out *bytes.Buffer, value any) error {
	switch v := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		if v {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case json.Number:
		out.WriteString(v.String())
	case string:
		encoded, err := canonicalString(v)
		if err != nil {
			return err
		}
		out.Write(encoded)
	case []any:
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := encodeCanonical(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			encodedKey, err := canonicalString(key)
			if err != nil {
				return err
			}
			out.Write(encodedKey)
			out.WriteByte(':')
			if err := encodeCanonical(out, v[key]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	default:
		return fmt.Errorf("unsupported canonical value of type %T", value)
	}
	return nil
}

// canonicalString escapes a string the way JSON.stringify does (no HTML
// escaping of <, >, & — Go's encoder escapes them by default, which would
// break byte-identity with the server-side canonical form).
func canonicalString(s string) ([]byte, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(s); err != nil {
		return nil, fmt.Errorf("failed to encode the string: %w", err)
	}
	return []byte(strings.TrimRight(out.String(), "\n")), nil
}
