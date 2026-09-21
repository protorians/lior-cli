package cmd

import (
	"path/filepath"
	"testing"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/module"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/spf13/cobra"
)

// createModuleForViewTest scaffolds a module with the embedded mockup so a view
// can be created into it.
func createModuleForViewTest(t *testing.T, root, domain string) {
	t.Helper()
	if _, err := (&module.Creator{Root: root}).Create(module.ModuleSpec{Domain: domain, ID: "blog-manager"}); err != nil {
		t.Fatalf("Create module: %v", err)
	}
}

func TestRunCreateViewCreatesView(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	createModuleForViewTest(t, root, "com.example.blog-manager")

	createViewName = "user-profile"
	createViewLabel = "User Profile"
	createViewDescription = "Profil utilisateur"
	defer func() { createViewName, createViewLabel, createViewDescription = "", "", "" }()

	if err := runCreateView(&cobra.Command{}, []string{"com.example.blog-manager"}); err != nil {
		t.Fatalf("runCreateView: %v", err)
	}

	view := filepath.Join(root, config.ExternalModulesDir, "com.example.blog-manager",
		"presentation", "views", "user-profile.view.tsx")
	if !pkg.FileExists(view) {
		t.Fatalf("view not created: %s", view)
	}
}

func TestRunCreateViewRejectsDuplicate(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	createModuleForViewTest(t, root, "com.example.blog-manager")

	createViewName = "user-profile"
	defer func() { createViewName = "" }()

	if err := runCreateView(&cobra.Command{}, []string{"com.example.blog-manager"}); err != nil {
		t.Fatalf("first runCreateView: %v", err)
	}
	if err := runCreateView(&cobra.Command{}, []string{"com.example.blog-manager"}); err == nil {
		t.Fatal("creating an existing view must fail")
	}
}

func TestRunCreateViewUnknownModule(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	createModuleForViewTest(t, root, "com.example.blog-manager")

	createViewName = "user-profile"
	defer func() { createViewName = "" }()

	if err := runCreateView(&cobra.Command{}, []string{"com.example.ghost"}); err == nil {
		t.Fatal("an unknown module must be rejected")
	}
}

func TestCreateViewCommandFlagsRegistered(t *testing.T) {
	for _, name := range []string{"mockup", "name", "label", "description"} {
		if f := createViewCmd.Flag(name); f == nil {
			t.Errorf("createViewCmd must expose the --%s flag", name)
		}
	}
	// `create view` is a subcommand of `create`.
	found := false
	for _, sub := range createCmd.Commands() {
		if sub == createViewCmd {
			found = true
		}
	}
	if !found {
		t.Error("createViewCmd must be registered under createCmd")
	}
}
