package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jetbrains/lior-cli/internal/config"
	"github.com/jetbrains/lior-cli/internal/i18n"
	"github.com/jetbrains/lior-cli/internal/module"
	"github.com/jetbrains/lior-cli/internal/pkg"
	"github.com/spf13/cobra"
)

func TestRunCreateUsesMockupAndPageFlags(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv(createSkipInstallEnv, "1")

	if err := os.MkdirAll(filepath.Join(root, config.ExternalModulesDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lorian.config.toml"), []byte("app=\"demo\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mockupDir := filepath.Join(root, "my-mockup")
	pageMockup := filepath.Join(root, "my-page.tsx")

	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(mockupDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("manifest.json", `{
  "schemaVersion": 1,
  "id": "hello-world",
  "domain": "mod.liorian.helloworld",
  "key": "HELLO_WORLD",
  "name": "Hello World",
  "description": "demo",
  "version": "1.0.0",
  "entry": "index.tsx",
  "uri": "/hello-world"
}
`)
	write("index.tsx", `const helloWorldModule = {
    identifier: 'mod.liorian.helloworld',
    name: 'Hello World',
    uri: '/hello-world',
}
export default helloWorldModule
`)
	write("marker.txt", "from-custom-mockup\n")

	if err := os.WriteFile(pageMockup, []byte(`import {HelloWorldView} from "@/library/modules/hello-world/presentation/views/hello-world.view";
export default function HelloWorldPage() {
    return <HelloWorldView/>;
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	createMockup = mockupDir
	createPageMockup = pageMockup
	createDomain = "com.example.blog-manager"
	defer func() { createMockup, createPageMockup, createDomain = "", "", "" }()

	if err := runCreate(&cobra.Command{}, []string{"blog-manager"}); err != nil {
		t.Fatalf("runCreate: %v", err)
	}

	moduleDir := filepath.Join(root, config.ExternalModulesDir, "com.example.blog-manager")
	if !pkg.FileExists(filepath.Join(moduleDir, "marker.txt")) {
		t.Error("the custom --mockup directory must be scaffolded (marker.txt missing)")
	}

	page := filepath.Join(root, "src", "app", "blog-manager", "page.tsx")
	if !pkg.FileExists(page) {
		t.Errorf("page must be scaffolded from --page-mockup: %s", page)
	}
}

func TestRunCreateIgnoresUnusableMockupFlag(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv(createSkipInstallEnv, "1")

	if err := os.MkdirAll(filepath.Join(root, config.ExternalModulesDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lorian.config.toml"), []byte("app=\"demo\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	createMockup = filepath.Join(root, "not-a-mockup")
	createDomain = "com.example.blog-manager"
	defer func() { createMockup, createDomain = "", "" }()

	if err := runCreate(&cobra.Command{}, []string{"blog-manager"}); err != nil {
		t.Fatalf("runCreate: %v", err)
	}

	if !pkg.FileExists(filepath.Join(root, config.ExternalModulesDir, "com.example.blog-manager", "package.json")) {
		t.Error("an unusable --mockup must fall back to the embedded mockup (package.json missing)")
	}
}

// TestCollectCreateSpecDerivesIdentifierFromDomain verifies that the module
// identifier is deduced from the reverse-DNS domain by replacing the dots with
// hyphens when it is not supplied explicitly.
func TestCollectCreateSpecDerivesIdentifierFromDomain(t *testing.T) {
	spec := module.ModuleSpec{Domain: "com.example.blog-manager"}
	if err := collectCreateSpec(&spec); err != nil {
		t.Fatalf("collectCreateSpec: %v", err)
	}
	if spec.ID != "com-example-blog-manager" {
		t.Errorf("ID = %q, want %q", spec.ID, "com-example-blog-manager")
	}
}

// TestModuleURLPlaceholderDerivesURIFromDomain verifies that the interactive
// URL suggestion is a URI derived from the reverse-DNS identifier, dropping the
// TLD segment (com.org.test -> /org/test).
func TestModuleURLPlaceholderDerivesURIFromDomain(t *testing.T) {
	cases := map[string]string{
		"com.org.test":            "/org/test",
		"com.organization.domain": "/organization/domain",
		"mod.liorian.hello-world": "/liorian/hello-world",
		"hello-world":             "/hello-world",
	}
	for domain, want := range cases {
		if got := moduleURLPlaceholder(domain); got != want {
			t.Errorf("moduleURLPlaceholder(%q) = %q, want %q", domain, got, want)
		}
	}
}

func TestCreateCommandFlagsRegistered(t *testing.T) {
	for _, name := range []string{
		"domain", "id", "name", "version", "icon", "url", "description",
		"type", "category", "mockup", "page-mockup", "skip-install",
	} {
		if f := createCmd.Flag(name); f == nil {
			t.Errorf("createCmd must expose the --%s flag", name)
		}
	}
}

// TestRunCreateSkipInstallFlagSkipsInstallStep verifies that the
// `--skip-install` flag (spec §5.2 step 6) disables the dependency
// installation step even when LIORIAN_CLI_SKIP_INSTALL is unset: with an
// empty PATH (no package manager) the "no package manager detected" warning
// must NOT be emitted.
func TestRunCreateSkipInstallFlagSkipsInstallStep(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	// No package manager resolvable: a non-skipped run would warn pm_none.
	t.Setenv("PATH", t.TempDir())
	t.Setenv(createSkipInstallEnv, "")

	if err := os.MkdirAll(filepath.Join(root, config.ExternalModulesDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lorian.config.toml"), []byte("app=\"demo\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	createDomain = "com.example.blog-manager"
	createSkipInstall = true
	defer func() { createDomain, createSkipInstall = "", false }()

	stderr := captureStderr(func() {
		if err := runCreate(&cobra.Command{}, []string{"blog-manager"}); err != nil {
			t.Fatalf("runCreate: %v", err)
		}
	})

	if !pkg.FileExists(filepath.Join(root, config.ExternalModulesDir, "com.example.blog-manager", "index.tsx")) {
		t.Fatal("the module must have been created")
	}
	if strings.Contains(stderr, i18n.T("create.warn.pm_none")) ||
		strings.Contains(stderr, "failed to install dependencies") {
		t.Errorf("with --skip-install the dependency step must not run; got warnings on stderr:\n%s", stderr)
	}
}

// captureStderr runs fn with os.Stderr redirected to a pipe and returns the
// captured output.
func captureStderr(fn func()) string {
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	fn()

	w.Close()
	b, _ := io.ReadAll(r)
	return string(b)
}

// writeRequirementMockup writes a scaffoldable mockup whose manifest declares
// the given requirements.
func writeRequirementMockup(t *testing.T, mockupDir string, requirements string) {
	t.Helper()
	if err := os.MkdirAll(mockupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mockupDir, "manifest.json"), []byte(`{
  "schemaVersion": 1,
  "id": "hello-world",
  "key": "HELLO_WORLD",
  "name": "Hello World",
  "version": "1.0.0",
  "entry": "index.tsx",
  "uri": "/hello-world",
  "requirements": `+requirements+`
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mockupDir, "index.tsx"), []byte(`const m = { name: 'Hello World' };
export default m;
`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunCreateBlocksMissingRequirement(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv(createSkipInstallEnv, "1")

	if err := os.MkdirAll(filepath.Join(root, config.ExternalModulesDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lorian.config.toml"), []byte("app=\"demo\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mockupDir := filepath.Join(root, "my-mockup")
	writeRequirementMockup(t, mockupDir, `{"analytics": true}`)

	createMockup = mockupDir
	createDomain = "com.example.blog-manager"
	defer func() { createMockup, createDomain = "", "" }()

	err := runCreate(&cobra.Command{}, []string{"blog-manager"})
	if err == nil {
		t.Fatal("create must fail when a required module is missing")
	}
	if !strings.Contains(err.Error(), "analytics") || !strings.Contains(err.Error(), "library/modules/") {
		t.Errorf("error must mention the missing requirement and the lookup dirs, got: %v", err)
	}
	if pkg.DirExists(filepath.Join(root, config.ExternalModulesDir, "com.example.blog-manager")) {
		t.Error("the module dir must be rolled back when creation is blocked")
	}
}

func TestRunCreateAcceptsRequirementInInternalModules(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv(createSkipInstallEnv, "1")

	if err := os.MkdirAll(filepath.Join(root, config.ExternalModulesDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src", "modules", "analytics"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lorian.config.toml"), []byte("app=\"demo\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mockupDir := filepath.Join(root, "my-mockup")
	writeRequirementMockup(t, mockupDir, `{"analytics": true}`)

	createMockup = mockupDir
	createDomain = "com.example.blog-manager"
	defer func() { createMockup, createDomain = "", "" }()

	if err := runCreate(&cobra.Command{}, []string{"blog-manager"}); err != nil {
		t.Fatalf("runCreate with an internal required module must succeed: %v", err)
	}
	if !pkg.FileExists(filepath.Join(root, config.ExternalModulesDir, "com.example.blog-manager", "index.tsx")) {
		t.Error("the module must have been created")
	}
}
