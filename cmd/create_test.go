package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/protorians/sentient-cli/internal/pkg"
	"github.com/spf13/cobra"
)

func TestRunCreateUsesMockupAndPageFlags(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	if err := os.Mkdir(filepath.Join(root, "external_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sentient.config.toml"), []byte("app=\"demo\"\n"), 0o644); err != nil {
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
  "domain": "mod.sentients.helloworld",
  "key": "HELLO_WORLD",
  "name": "Hello World",
  "description": "demo",
  "version": "1.0.0",
  "entry": "index.tsx",
  "uri": "/hello-world"
}
`)
	write("index.tsx", `const helloWorldModule = {
    identifier: 'mod.sentients.helloworld',
    name: 'Hello World',
    uri: '/hello-world',
}
export default helloWorldModule
`)
	write("marker.txt", "from-custom-mockup\n")

	if err := os.WriteFile(pageMockup, []byte(`import {HelloWorldView} from "@/external_modules/hello-world/presentation/views/hello-world.view";
export default function HelloWorldPage() {
    return <HelloWorldView/>;
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	createMockup = mockupDir
	createPageMockup = pageMockup
	defer func() { createMockup, createPageMockup = "", "" }()

	if err := runCreate(&cobra.Command{}, []string{"blog-manager"}); err != nil {
		t.Fatalf("runCreate: %v", err)
	}

	moduleDir := filepath.Join(root, "external_modules", "blog-manager")
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

	if err := os.Mkdir(filepath.Join(root, "external_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sentient.config.toml"), []byte("app=\"demo\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	createMockup = filepath.Join(root, "not-a-mockup")
	defer func() { createMockup = "" }()

	if err := runCreate(&cobra.Command{}, []string{"blog-manager"}); err != nil {
		t.Fatalf("runCreate: %v", err)
	}

	if !pkg.FileExists(filepath.Join(root, "external_modules", "blog-manager", "package.json")) {
		t.Error("an unusable --mockup must fall back to the embedded mockup (package.json missing)")
	}
}

func TestCreateCommandFlagsRegistered(t *testing.T) {
	for _, name := range []string{"mockup", "page-mockup"} {
		if f := createCmd.Flag(name); f == nil {
			t.Errorf("createCmd must expose the --%s flag", name)
		}
	}
}
