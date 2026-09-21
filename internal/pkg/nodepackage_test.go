package pkg

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDependencyArgs(t *testing.T) {
	cases := []struct {
		pm   string
		want []string
	}{
		{"bun", []string{"add", "react@latest"}},
		{"pnpm", []string{"add", "react@latest"}},
		{"yarn", []string{"add", "react@latest"}},
		{"npm", []string{"install", "react@latest"}},
	}
	for _, tc := range cases {
		got := DependencyArgs(tc.pm, "react@latest")
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("DependencyArgs(%q) = %v, want %v", tc.pm, got, tc.want)
		}
	}
	if got := DependencyArgs("unknown", "react@latest"); got != nil {
		t.Errorf("DependencyArgs(unknown) = %v, want nil", got)
	}
}

func TestLoadNodePackageDependencyNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "package.json")
	content := `{
  "dependencies": {"react": "latest", "next": "^16.0.0"},
  "devDependencies": {"typescript": "^6.0.3", "@types/react": "latest"}
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	np := LoadNodePackage(path)
	if got, want := np.DependencyNames(), []string{"@types/react", "next", "react", "typescript"}; !reflect.DeepEqual(got, want) {
		t.Errorf("DependencyNames() = %v, want %v", got, want)
	}
	if got, want := np.RuntimeDependencyNames(), []string{"next", "react"}; !reflect.DeepEqual(got, want) {
		t.Errorf("RuntimeDependencyNames() = %v, want %v", got, want)
	}
	if got, want := np.LatestDependencies(), []string{"@types/react", "react"}; !reflect.DeepEqual(got, want) {
		t.Errorf("LatestDependencies() = %v, want %v", got, want)
	}
}

func TestLoadNodePackageMissingFile(t *testing.T) {
	np := LoadNodePackage(filepath.Join(t.TempDir(), "absent.json"))
	if np == nil {
		t.Fatal("LoadNodePackage must never return nil")
	}
	if len(np.DependencyNames()) != 0 {
		t.Errorf("a missing package.json must yield no dependency, got %v", np.DependencyNames())
	}
}

func TestForceInstallLatest(t *testing.T) {
	dir := t.TempDir()
	content := `{"dependencies":{"react":"latest","next":"^16.0.0"},"devDependencies":{"typescript":"latest"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := t.TempDir()
	marker := filepath.Join(bin, "bun.log")
	script := "#!/bin/sh\nprintf '%s %s' \"$*\" \"$PWD\" > \"" + marker + "\"\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "bun"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	forced, err := ForceInstallLatest(dir, "bun")
	if err != nil {
		t.Fatalf("ForceInstallLatest: %v", err)
	}
	if want := []string{"react", "typescript"}; !reflect.DeepEqual(forced, want) {
		t.Errorf("ForceInstallLatest = %v, want %v", forced, want)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("the fake bun must have been called: %v", err)
	}
	got := string(data)
	for _, want := range []string{"add", "react@latest", "typescript@latest", dir} {
		if !strings.Contains(got, want) {
			t.Errorf("bun invocation %q must contain %q", got, want)
		}
	}
}

func TestForceInstallLatestNothingToDo(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"dependencies":{"react":"^19.0.0"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	forced, err := ForceInstallLatest(dir, "bun")
	if err != nil {
		t.Fatalf("ForceInstallLatest: %v", err)
	}
	if len(forced) != 0 {
		t.Errorf("ForceInstallLatest = %v, want none", forced)
	}
}
