package audit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/module"
	"github.com/protorians/lior-cli/internal/pkg"
)

// createTestModule sets up a valid module under a project root. The default
// dependency (@liorian/sdk) is stubbed in node_modules/ so the module audits
// cleanly, mirroring an installed project.
func createTestModule(t *testing.T, root, id string) {
	t.Helper()
	creator := &module.Creator{Root: root}
	if _, err := creator.Create(module.ModuleSpec{Domain: "mod.liorian." + id, ID: id, Description: "Test module"}); err != nil {
		t.Fatalf("Creator.Create(%q): %v", id, err)
	}
	// Stub every dependency declared in the module package.json in
	// node_modules/ so the module audits cleanly, mirroring an installed
	// project.
	nodePkg := pkg.LoadNodePackage(filepath.Join(root, config.ExternalModulesDir, "mod.liorian."+id, "package.json"))
	for _, dep := range nodePkg.DependencyNames() {
		depDir := filepath.Join(root, "node_modules", filepath.FromSlash(dep))
		if err := os.MkdirAll(depDir, 0o755); err != nil {
			t.Fatalf("création de node_modules/%s: %v", dep, err)
		}
	}
}

func setupAuditProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	// Create the library/modules dir (Creator does this, but be explicit)
	if err := os.MkdirAll(filepath.Join(root, config.ExternalModulesDir), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestAuditCleanModule(t *testing.T) {
	root := setupAuditProject(t)
	createTestModule(t, root, "blog-manager")

	auditor := &Auditor{Root: root}
	result, err := auditor.AuditModules("mod.liorian.blog-manager")
	if err != nil {
		t.Fatalf("AuditModules: %v", err)
	}
	if len(result.Modules) != 1 {
		t.Fatalf("attendu 1 module, reçu %d", len(result.Modules))
	}
	if result.Modules[0].HasErrors() {
		t.Errorf("module propre ne doit pas avoir d'erreurs: %v", result.Modules[0].Findings)
	}
	if result.TotalErrors() != 0 {
		t.Errorf("TotalErrors = %d, want 0", result.TotalErrors())
	}
}

func TestAuditMissingModule(t *testing.T) {
	root := setupAuditProject(t)
	auditor := &Auditor{Root: root}
	_, err := auditor.AuditModules("mod.liorian.nonexistent")
	if err == nil {
		t.Error("AuditModules d'un module absent doit échouer")
	}
}

func TestAuditAllModules(t *testing.T) {
	root := setupAuditProject(t)
	createTestModule(t, root, "mod-alpha")
	createTestModule(t, root, "mod-beta")

	auditor := &Auditor{Root: root}
	result, err := auditor.AuditModules("")
	if err != nil {
		t.Fatalf("AuditModules(all): %v", err)
	}
	if len(result.Modules) != 2 {
		t.Errorf("attendu 2 modules, reçu %d", len(result.Modules))
	}
}

func TestAuditAllSkipsDirsWithoutManifest(t *testing.T) {
	root := setupAuditProject(t)
	createTestModule(t, root, "real-module")
	// Create a dir without manifest.json
	if err := os.MkdirAll(filepath.Join(root, config.ExternalModulesDir, "empty-dir"), 0o755); err != nil {
		t.Fatal(err)
	}

	auditor := &Auditor{Root: root}
	result, err := auditor.AuditModules("")
	if err != nil {
		t.Fatalf("AuditModules: %v", err)
	}
	if len(result.Modules) != 1 {
		t.Errorf("attendu 1 module (skip empty dirs), reçu %d", len(result.Modules))
	}
}

func TestAuditAllMissingDir(t *testing.T) {
	root := t.TempDir() // no library/modules
	auditor := &Auditor{Root: root}
	_, err := auditor.AuditModules("")
	if err == nil {
		t.Error("AuditModules sans library/modules/ doit échouer")
	}
}

func TestAuditArchitectureComponentsImportServices(t *testing.T) {
	root := setupAuditProject(t)
	createTestModule(t, root, "bad-module")

	// Write a component that imports from services
	compDir := filepath.Join(root, config.ExternalModulesDir, "mod.liorian.bad-module", "components")
	if err := os.MkdirAll(compDir, 0o755); err != nil {
		t.Fatal(err)
	}
	compFile := filepath.Join(compDir, "Widget.tsx")
	if err := os.WriteFile(compFile, []byte(`import { fetchData } from "../services/api";`), 0o644); err != nil {
		t.Fatal(err)
	}

	auditor := &Auditor{Root: root}
	result, err := auditor.AuditModules("mod.liorian.bad-module")
	if err != nil {
		t.Fatalf("AuditModules: %v", err)
	}

	found := false
	for _, f := range result.Modules[0].Findings {
		if f.Category == "Clean Architecture" && f.Rule == "components→services" {
			found = true
			break
		}
	}
	if !found {
		t.Error("audit doit détecter l'import components→services")
	}
}

func TestAuditArchitectureServicesJSX(t *testing.T) {
	root := setupAuditProject(t)
	createTestModule(t, root, "jsx-service")

	// Write a service file containing JSX
	svcDir := filepath.Join(root, config.ExternalModulesDir, "mod.liorian.jsx-service", "services")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	svcFile := filepath.Join(svcDir, "api.ts")
	if err := os.WriteFile(svcFile, []byte(`// @jsx react
const el = <div>hello</div>;
// @tsx
`), 0o644); err != nil {
		t.Fatal(err)
	}

	auditor := &Auditor{Root: root}
	result, err := auditor.AuditModules("mod.liorian.jsx-service")
	if err != nil {
		t.Fatalf("AuditModules: %v", err)
	}

	found := false
	for _, f := range result.Modules[0].Findings {
		if f.Category == "Clean Architecture" && f.Rule == "services→JSX" {
			found = true
			break
		}
	}
	if !found {
		t.Error("audit doit détecter le JSX dans les services")
	}
}

func TestAuditDependenciesInstalled(t *testing.T) {
	root := setupAuditProject(t)
	createTestModule(t, root, "installed")

	auditor := &Auditor{Root: root}
	result, err := auditor.AuditModules("mod.liorian.installed")
	if err != nil {
		t.Fatalf("AuditModules: %v", err)
	}

	for _, f := range result.Modules[0].Findings {
		if f.Category == "dependencies" && f.Rule == "@liorian/sdk" {
			if f.Severity != module.LevelOK {
				t.Errorf("dépendance installée doit être OK, reçu %s", f.Severity)
			}
			return
		}
	}
	t.Error("finding pour dependencies installées attendu")
}

func TestAuditDependenciesMissing(t *testing.T) {
	root := setupAuditProject(t)
	createTestModule(t, root, "mod-missing")

	// Remove the stubbed node_modules: the default dependency becomes missing.
	if err := os.RemoveAll(filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}

	auditor := &Auditor{Root: root}
	result, err := auditor.AuditModules("mod.liorian.mod-missing")
	if err != nil {
		t.Fatalf("AuditModules: %v", err)
	}

	for _, f := range result.Modules[0].Findings {
		if f.Category == "dependencies" && f.Rule == "@liorian/sdk" {
			if f.Severity != module.LevelError {
				t.Errorf("dépendance absente doit être ERROR, reçu %s", f.Severity)
			}
			return
		}
	}
	t.Error("finding pour dépendance manquante attendu")
}

func TestAuditRequirementsMissing(t *testing.T) {
	root := setupAuditProject(t)
	createTestModule(t, root, "dependent")

	// Add a requirement that doesn't exist
	manifestPath := filepath.Join(root, config.ExternalModulesDir, "mod.liorian.dependent", "manifest.json")
	manifest, err := module.LoadManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Requirements = map[string]any{"nonexistent-module": true}
	if err := manifest.Save(manifestPath); err != nil {
		t.Fatal(err)
	}

	auditor := &Auditor{Root: root}
	result, err := auditor.AuditModules("mod.liorian.dependent")
	if err != nil {
		t.Fatalf("AuditModules: %v", err)
	}

	found := false
	for _, f := range result.Modules[0].Findings {
		if f.Category == "requirements" && f.Severity == module.LevelError {
			found = true
			break
		}
	}
	if !found {
		t.Error("audit doit détecter une requirement manquante")
	}
}

func TestAuditRequirementsPresent(t *testing.T) {
	root := setupAuditProject(t)
	createTestModule(t, root, "dep-a")
	createTestModule(t, root, "dep-b")

	// dep-b requires dep-a
	manifestPath := filepath.Join(root, config.ExternalModulesDir, "mod.liorian.dep-b", "manifest.json")
	manifest, err := module.LoadManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Requirements = map[string]any{"dep-a": true}
	if err := manifest.Save(manifestPath); err != nil {
		t.Fatal(err)
	}

	auditor := &Auditor{Root: root}
	result, err := auditor.AuditModules("mod.liorian.dep-b")
	if err != nil {
		t.Fatalf("AuditModules: %v", err)
	}

	for _, f := range result.Modules[0].Findings {
		if f.Category == "requirements" && f.Rule == "dep-a" {
			if f.Severity != module.LevelOK {
				t.Errorf("requirement existante doit être OK, reçu %s", f.Severity)
			}
			return
		}
	}
	t.Error("finding pour dep-a requirement attendu")
}

func TestAuditAssetsEmpty(t *testing.T) {
	root := setupAuditProject(t)
	createTestModule(t, root, "with-assets")

	// Create empty assets directory
	assetsDir := config.ModuleAssetsDir(root, "mod.liorian.with-assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	auditor := &Auditor{Root: root}
	result, err := auditor.AuditModules("mod.liorian.with-assets")
	if err != nil {
		t.Fatalf("AuditModules: %v", err)
	}

	found := false
	for _, f := range result.Modules[0].Findings {
		if f.Category == "assets" {
			found = true
			if f.Severity != module.LevelWarning {
				t.Errorf("assets vides doit être WARNING, reçu %s", f.Severity)
			}
			break
		}
	}
	if !found {
		t.Error("finding pour assets attendu")
	}
}

func TestAuditAssetsWithContent(t *testing.T) {
	root := setupAuditProject(t)
	createTestModule(t, root, "with-content")

	assetsDir := config.ModuleAssetsDir(root, "mod.liorian.with-content")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := pkg.WriteFile(filepath.Join(assetsDir, "icon.png"), []byte("fake-png")); err != nil {
		t.Fatal(err)
	}

	auditor := &Auditor{Root: root}
	result, err := auditor.AuditModules("mod.liorian.with-content")
	if err != nil {
		t.Fatalf("AuditModules: %v", err)
	}

	for _, f := range result.Modules[0].Findings {
		if f.Category == "assets" {
			if f.Severity != module.LevelOK {
				t.Errorf("assets avec contenu doit être OK, reçu %s", f.Severity)
			}
			return
		}
	}
}

func TestAuditResultTotals(t *testing.T) {
	root := setupAuditProject(t)
	createTestModule(t, root, "mod-a")
	createTestModule(t, root, "mod-b")

	auditor := &Auditor{Root: root}
	result, err := auditor.AuditModules("")
	if err != nil {
		t.Fatalf("AuditModules: %v", err)
	}
	// Two clean modules: no errors, no warnings
	if result.TotalErrors() != 0 {
		t.Errorf("TotalErrors = %d, want 0", result.TotalErrors())
	}
	if result.TotalWarnings() != 0 {
		t.Errorf("TotalWarnings = %d, want 0", result.TotalWarnings())
	}
}

func TestAuditIndexMissingDeclaration(t *testing.T) {
	root := setupAuditProject(t)
	createTestModule(t, root, "no-declaration")

	// Overwrite index.tsx so it no longer declares a module (no identifier/widgets).
	indexPath := filepath.Join(root, config.ExternalModulesDir, "mod.liorian.no-declaration", "index.tsx")
	if err := os.WriteFile(indexPath, []byte(`export default { name: "test" };`), 0o644); err != nil {
		t.Fatal(err)
	}

	auditor := &Auditor{Root: root}
	result, err := auditor.AuditModules("mod.liorian.no-declaration")
	if err != nil {
		t.Fatalf("AuditModules: %v", err)
	}

	found := false
	for _, f := range result.Modules[0].Findings {
		if f.Category == "index.tsx" && f.Rule == "declaration" {
			found = true
			if f.Severity != module.LevelError {
				t.Errorf("déclaration manquante doit être ERROR, reçu %s", f.Severity)
			}
			break
		}
	}
	if !found {
		t.Error("finding pour déclaration de module attendu")
	}
}
