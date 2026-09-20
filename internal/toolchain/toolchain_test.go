package toolchain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/pkg"
)

func writePackageJSON(t *testing.T, root string, scripts map[string]string) {
	t.Helper()
	var b strings.Builder
	b.WriteString(`{"scripts":{`)
	first := true
	for name, body := range scripts {
		if !first {
			b.WriteString(",")
		}
		first = false
		b.WriteString(`"` + name + `":` + body)
	}
	b.WriteString(`}}`)
	path := filepath.Join(root, "package.json")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeBun installs a fake `bun` in a fresh bin dir prepended to PATH. The
// fixture exits 7 for `run fail-*`, 1 for `run build`, and 0 otherwise.
func fakeBun(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	script := `#!/bin/sh
echo "[bun] $*"
case "$2" in
  fail-main|fail-hook) exit 7 ;;
  build) exit 1 ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(bin, "bun"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// hasStepID reports whether a recorded step carries the given ID.
func hasStepID(steps []Step, id string) bool {
	for _, s := range steps {
		if s.ID == id {
			return true
		}
	}
	return false
}

func TestResolveDefaultMapping(t *testing.T) {
	root := t.TempDir()
	writePackageJSON(t, root, map[string]string{"dev": `"echo dev"`, "lint": `"echo lint"`})
	r := New(root, "bun", config.ToolchainConfig{})

	c, err := r.Resolve(Dev, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(c.Args, " "); got != "bun run dev" {
		t.Errorf("Args = %q, want bun run dev", got)
	}
	if c.Dir != root {
		t.Errorf("Dir = %q, want %q", c.Dir, root)
	}

	c, err = r.Resolve(Check, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(c.Args, " "); got != "bun run lint" {
		t.Errorf("Args = %q, want bun run lint (default check script)", got)
	}
}

func TestResolveConfigOverride(t *testing.T) {
	root := t.TempDir()
	writePackageJSON(t, root, map[string]string{"typecheck": `"echo tsc"`})
	cfg := config.ToolchainConfig{Commands: map[string]string{Check: "typecheck"}}
	r := New(root, "bun", cfg)

	c, err := r.Resolve(Check, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(c.Args, " "); got != "bun run typecheck" {
		t.Errorf("Args = %q, want bun run typecheck", got)
	}
}

func TestResolveMissingScript(t *testing.T) {
	root := t.TempDir()
	writePackageJSON(t, root, map[string]string{})
	r := New(root, "bun", config.ToolchainConfig{})

	_, err := r.Resolve(Build, nil)
	if err == nil {
		t.Fatal("expected an error for a missing script")
	}
	pe, ok := err.(*pkg.Error)
	if !ok {
		t.Fatalf("err type = %T, want *pkg.Error", err)
	}
	if pe.ExitCode() != pkg.ExitError {
		t.Errorf("ExitCode = %d, want %d", pe.ExitCode(), pkg.ExitError)
	}
	if !strings.Contains(pe.Message, "build") {
		t.Errorf("Message = %q, want it to name the build script", pe.Message)
	}
}

func TestResolveNoPackageManager(t *testing.T) {
	r := New(t.TempDir(), "", config.ToolchainConfig{})
	_, err := r.Resolve(Dev, nil)
	pe, ok := err.(*pkg.Error)
	if !ok {
		t.Fatalf("err type = %T, want *pkg.Error", err)
	}
	if pe.ExitCode() != pkg.ExitError {
		t.Errorf("ExitCode = %d, want %d", pe.ExitCode(), pkg.ExitError)
	}
}

func TestResolvePassthroughArgs(t *testing.T) {
	root := t.TempDir()
	writePackageJSON(t, root, map[string]string{"dev": `"echo dev"`})
	extra := []string{"--port", "3000"}

	bun := New(root, "bun", config.ToolchainConfig{})
	c, err := bun.Resolve(Dev, extra)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(c.Args, " "); got != "bun run dev --port 3000" {
		t.Errorf("bun Args = %q, want passthrough without separator", got)
	}

	npm := New(root, "npm", config.ToolchainConfig{})
	c, err = npm.Resolve(Dev, extra)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(c.Args, " "); got != "npm run dev -- --port 3000" {
		t.Errorf("npm Args = %q, want a -- separator before forwarded args", got)
	}
}

func TestResolveUnknownCommand(t *testing.T) {
	r := New(t.TempDir(), "bun", config.ToolchainConfig{})
	if _, err := r.Resolve("deploy", nil); err == nil {
		t.Fatal("expected an error for an unknown command")
	}
}

func TestExecuteSuccessWithHooks(t *testing.T) {
	fakeBun(t)
	root := t.TempDir()
	writePackageJSON(t, root, map[string]string{
		"pre":  `"echo pre"`,
		"dev":  `"echo dev"`,
		"post": `"echo post"`,
	})
	cfg := config.ToolchainConfig{
		Before: map[string][]string{Dev: {"pre"}},
		After:  map[string][]string{Dev: {"post"}},
	}
	r := New(root, "bun", cfg)

	var steps []Step
	r.Reporter = func(s Step) { steps = append(steps, s) }
	if err := r.Execute(context.Background(), Dev, nil); err != nil {
		t.Fatal(err)
	}
	if !hasStepID(steps, "before:dev:0") {
		t.Error("expected a before hook step (before:dev:0)")
	}
	if !hasStepID(steps, "cmd:dev") {
		t.Error("expected a command step (cmd:dev)")
	}
	if !hasStepID(steps, "after:dev:0") {
		t.Error("expected an after hook step (after:dev:0)")
	}
}

func TestExecuteBeforeHookAbortsCommand(t *testing.T) {
	fakeBun(t)
	root := t.TempDir()
	writePackageJSON(t, root, map[string]string{
		"fail-hook": `"echo boom"`,
		"dev":       `"echo dev"`,
	})
	cfg := config.ToolchainConfig{Before: map[string][]string{Dev: {"fail-hook"}}}
	r := New(root, "bun", cfg)

	var steps []Step
	r.Reporter = func(s Step) { steps = append(steps, s) }
	err := r.Execute(context.Background(), Dev, nil)
	pe, ok := err.(*pkg.Error)
	if !ok {
		t.Fatalf("err type = %T, want *pkg.Error", err)
	}
	if pe.ExitCode() != 7 {
		t.Errorf("ExitCode = %d, want 7 (hook exit code)", pe.ExitCode())
	}
	if hasStepID(steps, "cmd:dev") {
		t.Error("the command must not run after a failing before hook")
	}
}

func TestExecuteAfterRunsEvenOnCommandFailure(t *testing.T) {
	fakeBun(t)
	root := t.TempDir()
	writePackageJSON(t, root, map[string]string{
		"build": `"echo build"`,
		"post":  `"echo post"`,
	})
	cfg := config.ToolchainConfig{After: map[string][]string{Build: {"post"}}}
	r := New(root, "bun", cfg)

	var steps []Step
	r.Reporter = func(s Step) { steps = append(steps, s) }
	err := r.Execute(context.Background(), Build, nil)
	pe, ok := err.(*pkg.Error)
	if !ok {
		t.Fatalf("err type = %T, want *pkg.Error (build failed)", err)
	}
	if pe.ExitCode() != 1 {
		t.Errorf("ExitCode = %d, want 1 (fixture build failure)", pe.ExitCode())
	}
	if !hasStepID(steps, "cmd:build") {
		t.Error("expected a command step (cmd:build)")
	}
	if !hasStepID(steps, "after:build:0") {
		t.Error("after hooks must run even when the command exits non-zero")
	}
}

func TestDefaultScriptTable(t *testing.T) {
	want := map[string]string{Dev: "dev", Build: "build", Start: "start", Check: "lint"}
	for name, script := range want {
		if got := DefaultScript[name]; got != script {
			t.Errorf("DefaultScript[%s] = %q, want %q", name, got, script)
		}
	}
}
