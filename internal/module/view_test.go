package module

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/pkg"
)

// createTestModule scaffolds a module so a view can be created into it.
func createTestModule(t *testing.T, root, domain string) {
	t.Helper()
	if _, err := (&Creator{Root: root}).Create(ModuleSpec{Domain: domain, ID: "blog-manager"}); err != nil {
		t.Fatalf("Create module: %v", err)
	}
}

func TestViewCreateFromEmbeddedMockup(t *testing.T) {
	root := t.TempDir()
	createTestModule(t, root, "com.example.blog-manager")

	creator := &ViewCreator{Root: root}
	res, err := creator.Create(ViewSpec{
		Module:      "com.example.blog-manager",
		Name:        "user-profile",
		Label:       "User Profile",
		Description: "Affiche le profil de l'utilisateur",
	})
	if err != nil {
		t.Fatalf("Create view: %v", err)
	}

	wantPath := filepath.Join(root, config.ExternalModulesDir, "com.example.blog-manager",
		"presentation", "views", "user-profile.view.tsx")
	if res.Path != wantPath {
		t.Errorf("Path = %q, want %q", res.Path, wantPath)
	}
	if res.Module != "com.example.blog-manager" {
		t.Errorf("Module = %q", res.Module)
	}
	if res.Name != "user-profile" {
		t.Errorf("Name = %q", res.Name)
	}
	if !pkg.FileExists(wantPath) {
		t.Fatalf("view not created: %s", wantPath)
	}

	assertFileContains(t, wantPath,
		"export function UserProfileView()",
		`label="User Profile"`,
		`description="Affiche le profil de l'utilisateur"`,
	)
}

func TestViewCreateDefaultsLabelToIdentifier(t *testing.T) {
	root := t.TempDir()
	createTestModule(t, root, "com.example.blog-manager")

	creator := &ViewCreator{Root: root}
	res, err := creator.Create(ViewSpec{Module: "com.example.blog-manager", Name: "user-profile"})
	if err != nil {
		t.Fatalf("Create view: %v", err)
	}
	assertFileContains(t, res.Path, `label="User Profile"`)
}

func TestViewCreateCustomMockup(t *testing.T) {
	root := t.TempDir()
	createTestModule(t, root, "com.example.blog-manager")

	mockup := filepath.Join(root, "my-view.tsx")
	if err := os.WriteFile(mockup, []byte(`"use client"
import {View} from "@liorian/sdk/presentation/themes/katon/view";

export function HelloWorldView() {
    return <View>Hello World</View>;
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	creator := &ViewCreator{Root: root, MockupDir: mockup}
	res, err := creator.Create(ViewSpec{Module: "com.example.blog-manager", Name: "custom-view"})
	if err != nil {
		t.Fatalf("Create view: %v", err)
	}
	assertFileContains(t, res.Path, "export function CustomViewView()")
}

func TestViewCreateEnvMockup(t *testing.T) {
	mockup := filepath.Join(t.TempDir(), "env-view.tsx")
	if err := os.WriteFile(mockup, []byte("export function HelloWorldView() { return null }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvViewMockup, mockup)

	root := t.TempDir()
	createTestModule(t, root, "com.example.blog-manager")

	res, err := (&ViewCreator{Root: root}).Create(ViewSpec{Module: "com.example.blog-manager", Name: "env-view"})
	if err != nil {
		t.Fatalf("Create view: %v", err)
	}
	assertFileContains(t, res.Path, "export function EnvViewView()")
}

func TestViewCreateRejectsExistingView(t *testing.T) {
	root := t.TempDir()
	createTestModule(t, root, "com.example.blog-manager")

	creator := &ViewCreator{Root: root}
	if _, err := creator.Create(ViewSpec{Module: "com.example.blog-manager", Name: "user-profile"}); err != nil {
		t.Fatalf("first Create view: %v", err)
	}
	_, err := creator.Create(ViewSpec{Module: "com.example.blog-manager", Name: "user-profile"})
	if err == nil {
		t.Fatal("creating an existing view must fail")
	}
	if !IsViewExistsError(err) {
		t.Errorf("error must be a view-exists error, got: %v", err)
	}
}

func TestViewCreateRejectsMissingModuleAndBadName(t *testing.T) {
	root := t.TempDir()
	createTestModule(t, root, "com.example.blog-manager")

	creator := &ViewCreator{Root: root}
	if _, err := creator.Create(ViewSpec{Module: "com.example.ghost", Name: "user-profile"}); err == nil {
		t.Error("a missing module must be rejected")
	}
	if _, err := creator.Create(ViewSpec{Module: "com.example.blog-manager", Name: "Bad Name"}); err == nil {
		t.Error("an invalid view name must be rejected")
	}
	if _, err := creator.Create(ViewSpec{Module: "", Name: "user-profile"}); err == nil {
		t.Error("an empty module must be rejected")
	}
}

func TestViewComponentName(t *testing.T) {
	cases := map[string]string{
		"user-profile": "UserProfile",
		"view":         "View",
		"a-b-c":        "ABC",
	}
	for in, want := range cases {
		if got := ViewComponentName(in); got != want {
			t.Errorf("ViewComponentName(%q) = %q, want %q", in, got, want)
		}
	}
}
