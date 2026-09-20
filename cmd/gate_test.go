package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jetbrains/lior-cli/internal/config"
	"github.com/jetbrains/lior-cli/internal/pkg"
)

// gateIndex is a minimal index.tsx that validates and passes the audit rules
// (default export + declarative identifier/widgets).
const gateIndex = `export default function GateDemo() {
	const name = "demo";
	return <p>{name}</p>;
}

const moduleConfig = {
	identifier: "hello-world",
	widgets: [],
};
`

// gateManifest builds a valid manifest for the test module. deps is injected
// verbatim as the manifest `dependencies` object.
func gateManifest(deps string) string {
	return fmt.Sprintf(`{
  "id": "hello-world",
  "domain": "mod.liorian.hello-world",
  "name": "Hello World",
  "version": "0.0.0",
  "token": %q,
  "entry": "index.tsx",
  "uri": "/hello-world",
  "type": "EXTERNAL",
  "category": "SYSTEM",
  "publisher": {"id": "dev", "name": "Dev"},
  "permissions": [],
  "optionalRequirements": {},
  "requirements": {},
  "dependencies": %s,
  "devDependencies": {},
  "widgets": [],
  "routines": [],
  "platforms": {
    "web": {"supported": true, "modes": ["web"]},
    "desktop": {"supported": false},
    "mobile": {"supported": false}
  },
  "managerCompatibility": {"min": "0.0.0"},
  "apiCompatibility": {"min": "0.0.0"},
  "capabilities": {"needsNetwork": true, "supportsOffline": false, "requiresOrganization": false, "requiresAuthenticatedUser": true}
}`, pkg.NewUUID(), deps)
}

// writeGateModule writes a module directory with a manifest and index.tsx.
func writeGateModule(t *testing.T, root, name, manifest string) {
	t.Helper()
	dir := filepath.Join(root, config.ExternalModulesDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, config.ManifestFileName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, config.ModuleEntryFileName), []byte(gateIndex), 0o644); err != nil {
		t.Fatal(err)
	}
}

// hermeticPATH removes every package manager / bundler from PATH so the gates
// degrade to their no-command fallbacks (WARNING, never blocking) and the
// tests stay deterministic.
func hermeticPATH(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

func TestRunToolchainGateSkippedWithoutModules(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()

	for _, name := range []string{"dev", "build", "start"} {
		if err := runToolchainGate(root, cfg, name); err != nil {
			t.Errorf("runToolchainGate(%s) = %v, want nil without modules", name, err)
		}
	}
}

func TestRunToolchainGateNoChecksForCheck(t *testing.T) {
	hermeticPATH(t)
	root := t.TempDir()
	writeGateModule(t, root, "hello-world", gateManifest("{}"))
	cfg := config.Default()

	if err := runToolchainGate(root, cfg, "check"); err != nil {
		t.Errorf("runToolchainGate(check) = %v, want nil (no gate for check)", err)
	}
}

func TestRunToolchainGatePassesOnHealthyModules(t *testing.T) {
	hermeticPATH(t)
	root := t.TempDir()
	writeGateModule(t, root, "hello-world", gateManifest("{}"))
	cfg := config.Default()

	for _, name := range []string{"dev", "build", "start"} {
		if err := runToolchainGate(root, cfg, name); err != nil {
			t.Errorf("runToolchainGate(%s) = %v, want nil on healthy modules", name, err)
		}
	}
}

func TestRunToolchainGateBlocksOnInvalidModule(t *testing.T) {
	hermeticPATH(t)
	root := t.TempDir()
	broken := strings.Replace(gateManifest("{}"), `"version": "0.0.0"`, `"version": "broken"`, 1)
	writeGateModule(t, root, "hello-world", broken)
	cfg := config.Default()

	err := runToolchainGate(root, cfg, "dev")
	if err == nil {
		t.Fatal("runToolchainGate(dev) = nil, want an error on an invalid module")
	}
	pe, ok := err.(*pkg.Error)
	if !ok {
		t.Fatalf("err type = %T, want *pkg.Error", err)
	}
	if pe.ExitCode() != pkg.ExitBuild {
		t.Errorf("ExitCode = %d, want %d (debug gate failure)", pe.ExitCode(), pkg.ExitBuild)
	}
	if !strings.Contains(pe.Message, "debug") {
		t.Errorf("Message = %q, want it to name the debug check", pe.Message)
	}
}

func TestRunToolchainGateBlocksOnAuditFailure(t *testing.T) {
	hermeticPATH(t)
	root := t.TempDir()
	// The module passes debug and test, but lists a dependency that is not
	// installed: only the audit gate (build/start) can catch it.
	writeGateModule(t, root, "hello-world", gateManifest(`{"some-pkg": "1.0.0"}`))
	cfg := config.Default()

	err := runToolchainGate(root, cfg, "build")
	if err == nil {
		t.Fatal("runToolchainGate(build) = nil, want an audit failure")
	}
	pe, ok := err.(*pkg.Error)
	if !ok {
		t.Fatalf("err type = %T, want *pkg.Error", err)
	}
	if pe.ExitCode() != pkg.ExitError {
		t.Errorf("ExitCode = %d, want %d (audit gate failure)", pe.ExitCode(), pkg.ExitError)
	}
	if !strings.Contains(pe.Message, "audit") {
		t.Errorf("Message = %q, want it to name the audit check", pe.Message)
	}
}

func TestHasModules(t *testing.T) {
	root := t.TempDir()
	if hasModules(root) {
		t.Error("hasModules = true, want false without library/modules")
	}
	writeGateModule(t, root, "hello-world", gateManifest("{}"))
	if !hasModules(root) {
		t.Error("hasModules = false, want true with a module")
	}
}
