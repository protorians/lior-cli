package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/protorians/lior-cli/internal/pkg"
)

func TestGenerateEnvFromSample(t *testing.T) {
	dir := t.TempDir()
	sample := strings.Join([]string{
		"# Encryption",
		"NEXT_PUBLIC_ENCRYPTION_KEY=",
		"NEXT_PUBLIC_APP_NAME=\"Sentient\"",
		"NEXT_PUBLIC_APP_SLUG=app.sentient.socle",
		"NEXT_PUBLIC_VAPID_PUBLIC_KEY=",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, ".env-sample"), []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}

	created, err := generateEnvFromSample(dir, "My App")
	if err != nil {
		t.Fatalf("generateEnvFromSample: %v", err)
	}
	if !created {
		t.Fatal("a .env must have been created from the sample")
	}

	envPath := filepath.Join(dir, pkg.EnvFileName)
	info, err := os.Stat(envPath)
	if err != nil {
		t.Fatalf("the .env must exist: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf(".env permissions = %o, want 600", info.Mode().Perm())
	}

	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	env := pkg.ParseEnv(data)

	if v, _ := env.Get("NEXT_PUBLIC_ENCRYPTION_KEY"); len(v) != 64 {
		t.Errorf("encryption key = %q, want a 64-character hex key", v)
	}
	if v, _ := env.Get("NEXT_PUBLIC_APP_NAME"); v != "My App" {
		t.Errorf("app name = %q, want the project name", v)
	}
	if v, _ := env.Get("NEXT_PUBLIC_APP_SLUG"); v != "my-app" {
		t.Errorf("app slug = %q, want %q", v, "my-app")
	}
	if v, _ := env.Get("NEXT_PUBLIC_VAPID_PUBLIC_KEY"); v == "" {
		t.Error("the VAPID public key must be generated")
	}
	if v, _ := env.Get("VAPID_PRIVATE_KEY"); v == "" {
		t.Error("the VAPID private key must be generated alongside the public key")
	}
	if !strings.Contains(string(data), "# Encryption") {
		t.Errorf("the sample comments must be preserved, got:\n%s", data)
	}
}

func TestGenerateEnvFromSampleNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env-sample"), []byte("A=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(dir, pkg.EnvFileName)
	if err := os.WriteFile(existing, []byte("KEEP=me\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	created, err := generateEnvFromSample(dir, "proj")
	if err != nil {
		t.Fatalf("generateEnvFromSample: %v", err)
	}
	if created {
		t.Error("an existing .env must never be overwritten")
	}
	data, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "KEEP=me\n" {
		t.Errorf("the existing .env must be kept, got %q", data)
	}
}

func TestGenerateEnvFromSampleWithoutSample(t *testing.T) {
	dir := t.TempDir()
	created, err := generateEnvFromSample(dir, "proj")
	if err != nil {
		t.Fatalf("generateEnvFromSample: %v", err)
	}
	if created {
		t.Error("no .env must be created without a sample")
	}
	if _, err := os.Stat(filepath.Join(dir, pkg.EnvFileName)); !os.IsNotExist(err) {
		t.Errorf(".env must not exist: %v", err)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"My App":          "my-app",
		"  Liorian Socle": "liorian-socle",
		"café 42":         "caf-42",
		"":                "",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVapidPrivateKey(t *testing.T) {
	cases := map[string]string{
		"NEXT_PUBLIC_VAPID_PUBLIC_KEY": "VAPID_PRIVATE_KEY",
		"VAPID_PUBLIC_KEY":             "VAPID_PRIVATE_KEY",
	}
	for in, want := range cases {
		if got := vapidPrivateKey(in); got != want {
			t.Errorf("vapidPrivateKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEnvKeyLabel(t *testing.T) {
	cases := map[string]string{
		"NEXT_PUBLIC_APP_SLUG":          "App Slug",
		"NEXT_PUBLIC_APP_NAME":          "App Name",
		"NEXT_PUBLIC_API_TIMEOUT":       "Api Timeout",
		"APP_HOST":                      "App Host",
		"NEXT_PUBLIC_VAPID_PRIVATE_KEY": "Vapid Private Key",
	}
	for in, want := range cases {
		if got := envKeyLabel(in); got != want {
			t.Errorf("envKeyLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsGeneratedEnvKey(t *testing.T) {
	generated := []string{
		"NEXT_PUBLIC_ENCRYPTION_KEY",
		"ENCRYPTION_KEY",
		"APP_KEY",
		"NEXT_PUBLIC_VAPID_PUBLIC_KEY",
		"VAPID_PRIVATE_KEY",
	}
	for _, key := range generated {
		if !isGeneratedEnvKey(key) {
			t.Errorf("isGeneratedEnvKey(%q) = false, want true", key)
		}
	}
	configurable := []string{
		"NEXT_PUBLIC_APP_NAME",
		"NEXT_PUBLIC_APP_SLUG",
		"NEXT_PUBLIC_APP_HOST",
		"NEXT_PUBLIC_API_TIMEOUT",
	}
	for _, key := range configurable {
		if isGeneratedEnvKey(key) {
			t.Errorf("isGeneratedEnvKey(%q) = true, want false", key)
		}
	}
}

func TestEnvAutoConfigured(t *testing.T) {
	old := initAutoEnv
	defer func() { initAutoEnv = old }()

	initAutoEnv = false
	t.Setenv(envAutoEnv, "")
	if envAutoConfigured() {
		t.Error("auto-configuration must be off by default")
	}

	initAutoEnv = true
	if !envAutoConfigured() {
		t.Error("--auto-env must enable auto-configuration")
	}

	initAutoEnv = false
	t.Setenv(envAutoEnv, "1")
	if !envAutoConfigured() {
		t.Errorf("%s must enable auto-configuration", envAutoEnv)
	}
}
