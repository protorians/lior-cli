package moduletest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindTestPackage(t *testing.T) {
	tp, ok := FindTestPackage("vitest")
	if !ok || tp.Name != "vitest" || len(tp.Args) == 0 {
		t.Fatalf("FindTestPackage(vitest) = %+v, %v", tp, ok)
	}
	if _, ok := FindTestPackage("unknown"); ok {
		t.Error("un package hors catalogue ne doit pas être trouvé")
	}
}

func TestTestPackageNames(t *testing.T) {
	names := TestPackageNames()
	if len(names) != len(TestPackages) {
		t.Fatalf("attendu %d noms, reçu %d", len(TestPackages), len(names))
	}
	if names[0] != "vitest" {
		t.Errorf("premier package = %q, want vitest", names[0])
	}
}

func TestRunnerBinary(t *testing.T) {
	if got := RunnerBinary("vitest"); got != "vitest" {
		t.Errorf("RunnerBinary(vitest) = %q", got)
	}
	if got := RunnerBinary("@scope/my-runner"); got != "my-runner" {
		t.Errorf("RunnerBinary(@scope/my-runner) = %q, want my-runner", got)
	}
}

func TestFindRunnerBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	moduleDir := filepath.Join(root, "library", "modules", "com.test.mod")
	bin := filepath.Join(moduleDir, "node_modules", ".bin", "vitest")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if got := FindRunnerBinary(moduleDir, root, "vitest"); got != bin {
		t.Errorf("FindRunnerBinary = %q, want %q", got, bin)
	}
	if !InstalledRunner(moduleDir, root, "vitest") {
		t.Error("InstalledRunner doit être true pour un binaire présent")
	}
	if InstalledRunner(moduleDir, root, "jest") {
		t.Error("InstalledRunner doit être false pour un binaire absent")
	}
}
