package pkg

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEnvPreservesCommentsAndOrder(t *testing.T) {
	sample := "# header\n\nNEXT_PUBLIC_APP_NAME=\"Sentient\"\nNEXT_PUBLIC_VAPID_PUBLIC_KEY=\n# footer\nOTHER=value\n"
	f := ParseEnv([]byte(sample))

	keys := f.Keys()
	want := []string{"NEXT_PUBLIC_APP_NAME", "NEXT_PUBLIC_VAPID_PUBLIC_KEY", "OTHER"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("Keys() = %v, want %v", keys, want)
	}
	if v, ok := f.Get("NEXT_PUBLIC_APP_NAME"); !ok || v != "Sentient" {
		t.Errorf("Get(NEXT_PUBLIC_APP_NAME) = %q, %v; want %q true", v, ok, "Sentient")
	}
	if v, ok := f.Get("NEXT_PUBLIC_VAPID_PUBLIC_KEY"); !ok || v != "" {
		t.Errorf("Get(VAPID_PUBLIC_KEY) = %q, %v; want empty true", v, ok)
	}
	if _, ok := f.Get("MISSING"); ok {
		t.Error("Get(MISSING) must report the variable as undefined")
	}

	rendered := f.Render()
	for _, want := range []string{"# header", "# footer", `NEXT_PUBLIC_APP_NAME="Sentient"`, "NEXT_PUBLIC_VAPID_PUBLIC_KEY="} {
		if !strings.Contains(rendered, want) {
			t.Errorf("Render() = %q, must contain %q", rendered, want)
		}
	}
}

func TestEnvFileSetUpdatesAndAppends(t *testing.T) {
	f := ParseEnv([]byte("A=1\n"))
	f.Set("A", "2")
	f.Set("B", "hello world")
	out := f.Render()
	if !strings.Contains(out, "A=2") {
		t.Errorf("Set must update an existing key, got %q", out)
	}
	if !strings.Contains(out, `B="hello world"`) {
		t.Errorf("Set must append and quote a spaced value, got %q", out)
	}
}

func TestFindEnvSample(t *testing.T) {
	dir := t.TempDir()
	if got := FindEnvSample(dir); got != "" {
		t.Errorf("FindEnvSample(empty) = %q, want empty", got)
	}
	path := filepath.Join(dir, ".env.example")
	if err := os.WriteFile(path, []byte("A=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := FindEnvSample(dir); got != path {
		t.Errorf("FindEnvSample() = %q, want %q", got, path)
	}
}

func TestNewEncryptionKey(t *testing.T) {
	key, err := NewEncryptionKey()
	if err != nil {
		t.Fatalf("NewEncryptionKey: %v", err)
	}
	if len(key) != 64 {
		t.Errorf("NewEncryptionKey() length = %d, want 64", len(key))
	}
	other, err := NewEncryptionKey()
	if err != nil {
		t.Fatal(err)
	}
	if key == other {
		t.Error("two generated keys must differ")
	}
}

func TestNewVAPIDKeys(t *testing.T) {
	public, private, err := NewVAPIDKeys()
	if err != nil {
		t.Fatalf("NewVAPIDKeys: %v", err)
	}
	pub, err := base64.RawURLEncoding.DecodeString(public)
	if err != nil {
		t.Fatalf("public key is not base64url: %v", err)
	}
	if len(pub) != 65 || pub[0] != 0x04 {
		t.Errorf("public key = %d bytes (first %#x), want 65 bytes starting with 0x04", len(pub), pub[0])
	}
	priv, err := base64.RawURLEncoding.DecodeString(private)
	if err != nil {
		t.Fatalf("private key is not base64url: %v", err)
	}
	if len(priv) != 32 {
		t.Errorf("private key = %d bytes, want 32", len(priv))
	}
}
