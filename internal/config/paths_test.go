package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeModule crée un module minimal sous `<root>/<dir>/<name>/`.
func writeModule(t *testing.T, root, dir, name string) string {
	t.Helper()
	moduleDir := filepath.Join(root, dir, name)
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, ManifestFileName), []byte(`{"id":"`+name+`"}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return moduleDir
}

func TestIsWorkspaceRoot(t *testing.T) {
	root := t.TempDir()

	if IsWorkspaceRoot(root) {
		t.Error("un répertoire vide ne doit pas être une racine de workspace")
	}

	// Un dossier `modules/` vide ne suffit pas : le signal doit être la présence
	// d'un module, sinon FindProjectRoot s'arrêterait sur n'importe quel dossier.
	if err := os.MkdirAll(filepath.Join(root, WorkspaceModulesDir), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if IsWorkspaceRoot(root) {
		t.Error("modules/ vide ne doit pas suffire à valider une racine de workspace")
	}

	writeModule(t, root, WorkspaceModulesDir, "crm")

	if !IsWorkspaceRoot(root) {
		t.Error("modules/crm/manifest.json doit valider la racine de workspace")
	}
}

func TestIsWorkspaceRootIgnoreUnFichierManifest(t *testing.T) {
	root := t.TempDir()
	// Un `manifest.json` à plat n'est pas un module.
	if err := os.MkdirAll(filepath.Join(root, WorkspaceModulesDir), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, WorkspaceModulesDir, ManifestFileName), []byte("{}"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if IsWorkspaceRoot(root) {
		t.Error("un manifest.json à plat dans modules/ ne doit pas valider la racine")
	}
}

func TestIsWorkspaceRootRejèteLaDestinationDInstallation(t *testing.T) {
	// `library/modules/<id>/manifest.json` est la destination d'installation
	// (D11) : le répertoire `library` qui la porte ne doit pas valider comme
	// racine de workspace, sinon FindProjectRoot remonte trop haut depuis un
	// module installé et renvoie `<projet>/library` au lieu de `<projet>`.
	root := t.TempDir()
	project := filepath.Join(root, "apps", "socle")
	writeModule(t, project, ExternalModulesDir, "billing")

	if IsWorkspaceRoot(filepath.Join(project, "library")) {
		t.Error("<projet>/library ne doit pas valider comme racine de workspace")
	}
	if !IsProjectRoot(project) {
		t.Error("le projet porteur de library/modules doit rester une racine de projet")
	}

	// Le même module installé ne doit pas faire remonter FindProjectRoot
	// au-dessus du projet qui l'héberge.
	nested := filepath.Join(project, ExternalModulesDir, "billing", "presentation")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	got, err := FindProjectRoot(nested)
	if err != nil {
		t.Fatalf("FindProjectRoot: %v", err)
	}
	if got != project {
		t.Errorf("FindProjectRoot() = %q, want %q", got, project)
	}
}

func TestIsProjectRootReconnaîtChaqueMarqueur(t *testing.T) {
	tests := []struct {
		nom      string
		preparer func(t *testing.T, root string)
		want     bool
	}{
		{
			nom:      "liorian.config.toml",
			preparer: func(t *testing.T, root string) { writeFile(t, filepath.Join(root, LorianConfigName), "") },
			want:     true,
		},
		{
			nom:      "liorian.config.json",
			preparer: func(t *testing.T, root string) { writeFile(t, filepath.Join(root, ConfigFileName), "{}") },
			want:     true,
		},
		{
			nom: "library/modules",
			preparer: func(t *testing.T, root string) {
				if err := os.MkdirAll(filepath.Join(root, ExternalModulesDir), 0o755); err != nil {
					t.Fatalf("MkdirAll: %v", err)
				}
			},
			want: true,
		},
		{
			nom:      "racine de workspace (modules/<id>/manifest.json)",
			preparer: func(t *testing.T, root string) { writeModule(t, root, WorkspaceModulesDir, "crm") },
			want:     true,
		},
		{
			nom:      "aucun marqueur",
			preparer: func(t *testing.T, root string) { writeFile(t, filepath.Join(root, "README.md"), "") },
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.nom, func(t *testing.T) {
			root := t.TempDir()
			tt.preparer(t, root)
			if got := IsProjectRoot(root); got != tt.want {
				t.Errorf("IsProjectRoot() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindProjectRootMonteDepuisUnModuleSource(t *testing.T) {
	root := t.TempDir()
	moduleDir := writeModule(t, root, WorkspaceModulesDir, "crm")
	nested := filepath.Join(moduleDir, "presentation", "views")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	got, err := FindProjectRoot(nested)
	if err != nil {
		t.Fatalf("FindProjectRoot: %v", err)
	}
	if got != root {
		t.Errorf("FindProjectRoot() = %q, want %q", got, root)
	}
}

func TestFindProjectRootPréfèreLeProjetLePlusProche(t *testing.T) {
	// Un projet imbriqué (le socle) doit l'emporter sur la racine du workspace :
	// les deux sont des racines valides, la plus proche gagne.
	workspace := t.TempDir()
	writeModule(t, workspace, WorkspaceModulesDir, "crm")

	project := filepath.Join(workspace, "apps", "socle")
	writeFile(t, filepath.Join(project, ConfigFileName), "{}")

	moduleDir := writeModule(t, project, ExternalModulesDir, "billing")
	nested := filepath.Join(moduleDir, "presentation")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	got, err := FindProjectRoot(nested)
	if err != nil {
		t.Fatalf("FindProjectRoot: %v", err)
	}
	if got != project {
		t.Errorf("FindProjectRoot() = %q, want %q", got, project)
	}
}

func TestFindProjectRootErreurHorsProjet(t *testing.T) {
	// Racine temporaire sans aucun marqueur, mais dont les parents (le dossier
	// temporaire du système) n'en ont pas non plus.
	if _, err := FindProjectRoot(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("FindProjectRoot doit échouer hors de tout projet")
	}
}

func TestListWorkspaceModules(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, WorkspaceModulesDir, "stock-management")
	writeModule(t, root, WorkspaceModulesDir, "crm")
	// Ni un fichier, ni un dossier sans manifeste : pas des modules.
	writeFile(t, filepath.Join(root, WorkspaceModulesDir, "README.md"), "")
	if err := os.MkdirAll(filepath.Join(root, WorkspaceModulesDir, "vide"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	got, err := ListWorkspaceModules(root)
	if err != nil {
		t.Fatalf("ListWorkspaceModules: %v", err)
	}
	want := []string{"crm", "stock-management"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListWorkspaceModules() = %v, want %v", got, want)
	}
}

func TestListWorkspaceModulesAbsente(t *testing.T) {
	got, err := ListWorkspaceModules(t.TempDir())
	if err != nil {
		t.Fatalf("ListWorkspaceModules: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListWorkspaceModules() = %v, want vide", got)
	}
}

func TestWorkspaceModulePaths(t *testing.T) {
	root := "/racine"

	if got, want := WorkspaceModulesDirPath(root), filepath.Join(root, WorkspaceModulesDir); got != want {
		t.Errorf("WorkspaceModulesDirPath() = %q, want %q", got, want)
	}
	if got, want := WorkspaceModuleDir(root, "crm"), filepath.Join(root, WorkspaceModulesDir, "crm"); got != want {
		t.Errorf("WorkspaceModuleDir() = %q, want %q", got, want)
	}
	if got, want := WorkspaceModuleEntryPath(root, "crm"), filepath.Join(root, WorkspaceModulesDir, "crm", ModuleEntryFileName); got != want {
		t.Errorf("WorkspaceModuleEntryPath() = %q, want %q", got, want)
	}
	// Source et destination d'installation ne doivent pas se confondre (D11).
	if WorkspaceModuleDir(root, "crm") == ModuleDir(root, "crm") {
		t.Error("modules/<id> (source) et library/modules/<id> (destination) doivent rester distincts")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestCurrentPointerRoundTrip(t *testing.T) {
	root := t.TempDir()

	if got := ReadCurrentPointer(root, "crm"); got != "" {
		t.Errorf("ReadCurrentPointer sans pointeur = %q, want \"\"", got)
	}
	if err := WriteCurrentPointer(root, "crm", "1.2.0"); err != nil {
		t.Fatalf("WriteCurrentPointer: %v", err)
	}
	if got := ReadCurrentPointer(root, "crm"); got != "1.2.0" {
		t.Errorf("ReadCurrentPointer = %q, want 1.2.0", got)
	}
}

func TestResolveInstalledModuleDir(t *testing.T) {
	root := t.TempDir()

	// Sans pointeur ni installation : le répertoire plat (legacy) est renvoyé.
	if got, want := ResolveInstalledModuleDir(root, "crm"), ModuleDir(root, "crm"); got != want {
		t.Errorf("ResolveInstalledModuleDir = %q, want %q", got, want)
	}

	// Multi-version : le pointeur décide (D11, §4.4).
	if err := WriteCurrentPointer(root, "crm", "1.2.0"); err != nil {
		t.Fatal(err)
	}
	versionDir := InstalledModuleVersionDir(root, "crm", "1.2.0")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, want := ResolveInstalledModuleDir(root, "crm"), versionDir; got != want {
		t.Errorf("ResolveInstalledModuleDir = %q, want %q", got, want)
	}

	// Un pointeur cassé (version absente) retombe sur le répertoire plat.
	if err := WriteCurrentPointer(root, "crm", "9.9.9"); err != nil {
		t.Fatal(err)
	}
	if got, want := ResolveInstalledModuleDir(root, "crm"), ModuleDir(root, "crm"); got != want {
		t.Errorf("ResolveInstalledModuleDir (pointeur cassé) = %q, want %q", got, want)
	}
}
