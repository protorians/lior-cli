package artifactkit

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/protorians/lior-cli/internal/pkg"
)

// project writes a minimal Liora workspace: a package.json, a manifest.json
// under modules/<id>, and the returned project root.
func project(t *testing.T, moduleID string) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "lorian.config.json"), `{"project":{"packageManager":"npm"}}`)
	mustWrite(t, filepath.Join(moduleDirPath(root, moduleID), "manifest.json"), `{"id":"acme.crm"}`)
	return root
}

func moduleDirPath(root, moduleID string) string {
	return filepath.Join(root, "modules", moduleID)
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestModuleDirFromCwd(t *testing.T) {
	root := project(t, "acme.crm")
	moduleDir := moduleDirPath(root, "acme.crm")

	// The module directory itself.
	if got := ModuleDirFromCwd(root, moduleDir); got != moduleDir {
		t.Fatalf("from the module dir: got %q, want %q", got, moduleDir)
	}
	// A nested directory: the walk climbs to the module that owns it.
	nested := filepath.Join(moduleDir, "presentation", "views")
	if got := ModuleDirFromCwd(root, nested); got != moduleDir {
		t.Fatalf("from a nested dir: got %q, want %q", got, moduleDir)
	}
	// The project root holds no module of its own, even when it has children.
	if got := ModuleDirFromCwd(root, root); got != "" {
		t.Fatalf("from the project root: got %q, want an empty result", got)
	}
}

func TestModuleDirFromCwdInstalledVersion(t *testing.T) {
	root := project(t, "acme.crm")
	// Multi-version installation (D11): library/modules/<id>/<version>/.
	installed := filepath.Join(root, "library", "modules", "acme.crm", "1.4.0")
	mustWrite(t, filepath.Join(installed, "manifest.json"), `{"id":"acme.crm"}`)
	if got := ModuleDirFromCwd(root, installed); got != installed {
		t.Fatalf("installed version: got %q, want %q", got, installed)
	}
	// The version root (`library/modules/<id>`) carries no manifest of its own,
	// and the walk only climbs: a version is never guessed. Callers that want
	// one ask for it explicitly (`config.ResolveInstalledModuleDir`).
	versionRoot := filepath.Join(root, "library", "modules", "acme.crm")
	if got := ModuleDirFromCwd(root, versionRoot); got != "" {
		t.Fatalf("version root: got %q, want an empty result", got)
	}
}

func TestModuleDirFromCwdOutsideProject(t *testing.T) {
	root := project(t, "acme.crm")
	// A sibling directory holding a manifest.json must not be adopted: the
	// walk stops at the project root.
	outside := t.TempDir()
	mustWrite(t, filepath.Join(outside, "manifest.json"), `{"id":"stranger"}`)
	if got := ModuleDirFromCwd(root, outside); got != "" {
		t.Fatalf("outside the project: got %q, want an empty result", got)
	}
}

func TestModuleDirFromArg(t *testing.T) {
	root := project(t, "acme.crm")
	moduleDir := moduleDirPath(root, "acme.crm")

	if got := ModuleDirFromArg([]string{moduleDir}); got != moduleDir {
		t.Fatalf("explicit dir: got %q, want %q", got, moduleDir)
	}
	// Flags are skipped, and a flag value that happens to be a directory is
	// only adopted when it really holds a manifest.
	flags := []string{"--port", "5178", moduleDir}
	if got := ModuleDirFromArg(flags); got != moduleDir {
		t.Fatalf("after flags: got %q, want %q", got, moduleDir)
	}
	// A directory without a manifest is not a module target.
	empty := t.TempDir()
	if got := ModuleDirFromArg([]string{empty}); got != "" {
		t.Fatalf("dir without manifest: got %q, want an empty result", got)
	}
}

func TestDeclares(t *testing.T) {
	root := project(t, "acme.crm")
	moduleDir := moduleDirPath(root, "acme.crm")
	manifest := filepath.Join(moduleDir, "package.json")

	if Declares(moduleDir) {
		t.Fatal("a module without package.json must declare nothing")
	}
	mustWrite(t, manifest, `{"dependencies":{"@liorian/sdk":"^1.0.0"}}`)
	if Declares(moduleDir) {
		t.Fatal("an unrelated dependency set must not declare the artifact-kit")
	}
	mustWrite(t, manifest, `{"dependencies":{"`+PackageName+`":"^0.4.1"}}`)
	if !Declares(moduleDir) {
		t.Fatal("a runtime dependency must be a declaration")
	}
	mustWrite(t, manifest, `{"devDependencies":{"`+PackageName+`":"^0.4.1"}}`)
	if !Declares(moduleDir) {
		t.Fatal("a dev dependency must be a declaration")
	}
}

func TestEnsureIsANoOpWhenDeclared(t *testing.T) {
	root := project(t, "acme.crm")
	moduleDir := moduleDirPath(root, "acme.crm")
	mustWrite(t, filepath.Join(moduleDir, "package.json"),
		`{"dependencies":{"`+PackageName+`":"^0.4.1"}}`)

	installed, err := Ensure(moduleDir, "npm")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if installed {
		t.Fatal("a declared module must not be reinstalled")
	}
}

func TestEnsureFailsClosedWithoutPackageManager(t *testing.T) {
	root := project(t, "acme.crm")
	moduleDir := moduleDirPath(root, "acme.crm")

	// Fail-closed: an unavailable toolchain is an error, never a silent pass
	// that would leave the developer with a module that cannot build.
	installed, err := Ensure(moduleDir, "")
	if err == nil {
		t.Fatal("expected an error without a package manager")
	}
	if installed {
		t.Fatal("no installation ran")
	}
}

func TestEnsureRejectsAnUnsupportedPackageManager(t *testing.T) {
	root := project(t, "acme.crm")
	moduleDir := moduleDirPath(root, "acme.crm")

	if _, err := Ensure(moduleDir, "cargo"); err == nil {
		t.Fatal("expected an error for an unsupported package manager")
	}
}

func TestResolveWalksUpToTheHoistedBinary(t *testing.T) {
	root := project(t, "acme.crm")
	moduleDir := moduleDirPath(root, "acme.crm")
	// A workspace hoists the binary to its root node_modules.
	binary := filepath.Join(root, "node_modules", ".bin", binaryName())
	mustWrite(t, binary, "#!/bin/sh\n")

	got, err := Resolve(root, moduleDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != binary {
		t.Fatalf("got %q, want %q", got, binary)
	}
}

func TestResolvePrefersTheModuleBinary(t *testing.T) {
	root := project(t, "acme.crm")
	moduleDir := moduleDirPath(root, "acme.crm")
	hoisted := filepath.Join(root, "node_modules", ".bin", binaryName())
	local := filepath.Join(moduleDir, "node_modules", ".bin", binaryName())
	mustWrite(t, hoisted, "#!/bin/sh\n")
	mustWrite(t, local, "#!/bin/sh\n")

	got, err := Resolve(root, moduleDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != local {
		t.Fatalf("got %q, want the module binary %q", got, local)
	}
}

func TestResolveFailsClosedWithoutBinary(t *testing.T) {
	root := project(t, "acme.crm")
	moduleDir := moduleDirPath(root, "acme.crm")

	_, err := Resolve(root, moduleDir)
	if err == nil {
		t.Fatal("expected an error when the CLI is not installed")
	}
	// The error must be categorized (the CLI renders `✗ Category : message`).
	if _, ok := err.(*pkg.Error); !ok {
		t.Fatalf("expected a categorized *pkg.Error, got %T", err)
	}
}

func TestBinaryName(t *testing.T) {
	want := BinaryName
	if runtime.GOOS == "windows" {
		want += ".cmd"
	}
	if binaryName() != want {
		t.Fatalf("got %q, want %q", binaryName(), want)
	}
}
