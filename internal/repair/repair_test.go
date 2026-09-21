package repair

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/module"
	"github.com/protorians/lior-cli/internal/pkg"
)

// createTestModule scaffolds a valid module and stubs its declared
// dependencies in node_modules/ so it audits cleanly.
func createTestModule(t *testing.T, root, id string) {
	t.Helper()
	creator := &module.Creator{Root: root}
	if _, err := creator.Create(module.ModuleSpec{Domain: "mod.liorian." + id, ID: id, Description: "Test module"}); err != nil {
		t.Fatalf("Creator.Create(%q): %v", id, err)
	}
	nodePkg := pkg.LoadNodePackage(filepath.Join(root, config.ExternalModulesDir, "mod.liorian."+id, "package.json"))
	for _, dep := range nodePkg.DependencyNames() {
		if err := os.MkdirAll(filepath.Join(root, "node_modules", filepath.FromSlash(dep)), 0o755); err != nil {
			t.Fatalf("création de node_modules/%s: %v", dep, err)
		}
	}
}

func setupProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, config.ExternalModulesDir), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRepairCleanModule(t *testing.T) {
	root := setupProject(t)
	createTestModule(t, root, "clean-module")

	r := &Repairer{Root: root, SkipInstall: true}
	result, err := r.RepairModules("mod.liorian.clean-module")
	if err != nil {
		t.Fatalf("RepairModules: %v", err)
	}
	if len(result.Modules) != 1 {
		t.Fatalf("attendu 1 module, reçu %d", len(result.Modules))
	}
	m := result.Modules[0]
	if len(m.Actions) != 0 {
		t.Errorf("module propre ne doit nécessiter aucune correction: %+v", m.Actions)
	}
	if len(m.Instructions) != 0 {
		t.Errorf("module propre ne doit nécessiter aucune instruction: %+v", m.Instructions)
	}
	if got := result.TotalRemainingErrors(); got != 0 {
		t.Errorf("TotalRemainingErrors = %d, want 0", got)
	}
}

func TestRepairInvalidManifestMetadata(t *testing.T) {
	root := setupProject(t)
	createTestModule(t, root, "broken-meta")

	manifestPath := filepath.Join(root, config.ExternalModulesDir, "mod.liorian.broken-meta", config.ManifestFileName)
	m, err := module.LoadManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	m.Version = "not-semver"
	m.Token = ""
	if err := m.Save(manifestPath); err != nil {
		t.Fatal(err)
	}

	r := &Repairer{Root: root, SkipInstall: true}
	result, err := r.RepairModules("mod.liorian.broken-meta")
	if err != nil {
		t.Fatalf("RepairModules: %v", err)
	}
	mr := result.Modules[0]
	if result.TotalApplied() < 2 {
		t.Errorf("au moins 2 corrections attendues (version, token), reçu %d: %+v", result.TotalApplied(), mr.Actions)
	}
	if got := result.TotalRemainingErrors(); got != 0 {
		t.Errorf("TotalRemainingErrors = %d, want 0 (findings: %+v)", got, mr.Remaining)
	}

	// The repaired manifest must carry a valid SemVer version and UUID token.
	fixed, err := module.LoadManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if fixed.Version != "0.0.0" {
		t.Errorf("Version = %q, want 0.0.0", fixed.Version)
	}
	if fixed.Token == "" {
		t.Error("Token doit être régénéré")
	}
}

func TestRepairDryRunDoesNotWrite(t *testing.T) {
	root := setupProject(t)
	createTestModule(t, root, "dry-run")

	manifestPath := filepath.Join(root, config.ExternalModulesDir, "mod.liorian.dry-run", config.ManifestFileName)
	m, err := module.LoadManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	m.Version = "bad"
	if err := m.Save(manifestPath); err != nil {
		t.Fatal(err)
	}

	r := &Repairer{Root: root, SkipInstall: true, DryRun: true}
	result, err := r.RepairModules("mod.liorian.dry-run")
	if err != nil {
		t.Fatalf("RepairModules: %v", err)
	}
	if result.TotalApplied() != 0 {
		t.Errorf("dry-run ne doit appliquer aucune correction, reçu %d", result.TotalApplied())
	}
	fixed, err := module.LoadManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if fixed.Version != "bad" {
		t.Errorf("dry-run ne doit pas écrire le manifeste, Version = %q", fixed.Version)
	}
}

func TestRepairManualInstructionsForComplexFinding(t *testing.T) {
	root := setupProject(t)
	createTestModule(t, root, "no-export")

	indexPath := filepath.Join(root, config.ExternalModulesDir, "mod.liorian.no-export", "index.tsx")
	if err := os.WriteFile(indexPath, []byte("export const x = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &Repairer{Root: root, SkipInstall: true}
	result, err := r.RepairModules("mod.liorian.no-export")
	if err != nil {
		t.Fatalf("RepairModules: %v", err)
	}
	mr := result.Modules[0]
	if len(mr.Instructions) == 0 {
		t.Fatalf("des instructions manuelles sont attendues pour l'export manquant")
	}
	if result.TotalRemainingErrors() == 0 {
		t.Error("l'export manquant doit rester une erreur bloquante")
	}
	found := false
	for _, ins := range mr.Instructions {
		if ins.Rule == "export" {
			found = true
		}
	}
	if !found {
		t.Errorf("instruction pour la règle 'export' attendue: %+v", mr.Instructions)
	}
}

func TestRepairAllModules(t *testing.T) {
	root := setupProject(t)
	createTestModule(t, root, "mod-alpha")
	createTestModule(t, root, "mod-beta")

	r := &Repairer{Root: root, SkipInstall: true}
	result, err := r.RepairModules("")
	if err != nil {
		t.Fatalf("RepairModules(all): %v", err)
	}
	if len(result.Modules) != 2 {
		t.Errorf("attendu 2 modules, reçu %d", len(result.Modules))
	}
}

// makeRenameCandidate scaffolds a valid module, then moves its directory to
// the legacy <dir> form and rewrites its manifest domain to <domain>, leaving
// the manifest domain and the directory mismatched.
func makeRenameCandidate(t *testing.T, root, dir, domain string) {
	t.Helper()
	id := "rename-candidate"
	createTestModule(t, root, id)
	created := filepath.Join(root, config.ExternalModulesDir, "mod.liorian."+id)
	target := filepath.Join(root, config.ExternalModulesDir, dir)
	if err := os.Rename(created, target); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(target, config.ManifestFileName)
	m, err := module.LoadManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	m.Domain = domain
	if err := m.Save(manifestPath); err != nil {
		t.Fatal(err)
	}
}

func TestRepairRenamesModuleDirectory(t *testing.T) {
	root := setupProject(t)
	makeRenameCandidate(t, root, "mod.liorian.legacy", "com.example.legacy")

	r := &Repairer{Root: root, SkipInstall: true, NoInteraction: true}
	result, err := r.RepairModules("")
	if err != nil {
		t.Fatalf("RepairModules: %v", err)
	}
	if len(result.Modules) != 1 {
		t.Fatalf("attendu 1 module, reçu %d", len(result.Modules))
	}
	if result.Modules[0].Module != "com.example.legacy" {
		t.Errorf("Module = %q, want com.example.legacy", result.Modules[0].Module)
	}
	newDir := filepath.Join(root, config.ExternalModulesDir, "com.example.legacy")
	if _, err := os.Stat(newDir); err != nil {
		t.Errorf("le dossier renommé doit exister: %v", err)
	}
	oldDir := filepath.Join(root, config.ExternalModulesDir, "mod.liorian.legacy")
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Errorf("l'ancien dossier doit avoir disparu, err = %v", err)
	}
}

func TestRepairRenamePromptSuggestion(t *testing.T) {
	root := setupProject(t)
	makeRenameCandidate(t, root, "mod.liorian.legacy", "com.example.legacy")

	var gotMessage, gotSuggestion string
	r := &Repairer{
		Root: root, SkipInstall: true,
		Rename: func(message, suggestion string) (string, bool) {
			gotMessage, gotSuggestion = message, suggestion
			return suggestion, true
		},
	}
	if _, err := r.RepairModules(""); err != nil {
		t.Fatalf("RepairModules: %v", err)
	}
	if gotSuggestion != "com.example.legacy" {
		t.Errorf("suggestion = %q, want com.example.legacy", gotSuggestion)
	}
	if gotMessage == "" {
		t.Error("le message du prompt ne doit pas être vide")
	}
}

func TestRepairRenameDeclinedReportsInstruction(t *testing.T) {
	root := setupProject(t)
	makeRenameCandidate(t, root, "mod.liorian.legacy", "com.example.legacy")

	r := &Repairer{
		Root: root, SkipInstall: true,
		Rename: func(message, suggestion string) (string, bool) { return "", false },
	}
	result, err := r.RepairModules("")
	if err != nil {
		t.Fatalf("RepairModules: %v", err)
	}
	if result.Modules[0].Module != "mod.liorian.legacy" {
		t.Errorf("le refus ne doit pas renommer, Module = %q", result.Modules[0].Module)
	}
	if _, err := os.Stat(filepath.Join(root, config.ExternalModulesDir, "mod.liorian.legacy")); err != nil {
		t.Errorf("le dossier doit rester en place: %v", err)
	}
	found := false
	for _, ins := range result.Modules[0].Instructions {
		if ins.Rule == "domain directory" {
			found = true
		}
	}
	if !found {
		t.Errorf("une instruction 'domain directory' doit être rapportée: %+v", result.Modules[0].Instructions)
	}
}

func TestRepairDomainAcceptsReverseDNS(t *testing.T) {
	root := setupProject(t)
	createTestModule(t, root, "reverse-dns")

	manifestPath := filepath.Join(root, config.ExternalModulesDir, "mod.liorian.reverse-dns", config.ManifestFileName)
	m, err := module.LoadManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	// A non-mod.liorian reverse-DNS domain matches the directory: the audit
	// must not warn on the domain rule.
	dir := filepath.Join(root, config.ExternalModulesDir, "com.example.reverse-dns")
	if err := os.Rename(filepath.Join(root, config.ExternalModulesDir, "mod.liorian.reverse-dns"), dir); err != nil {
		t.Fatal(err)
	}
	m.Domain = "com.example.reverse-dns"
	if err := m.Save(filepath.Join(dir, config.ManifestFileName)); err != nil {
		t.Fatal(err)
	}

	r := &Repairer{Root: root, SkipInstall: true, NoInteraction: true}
	result, err := r.RepairModules("com.example.reverse-dns")
	if err != nil {
		t.Fatalf("RepairModules: %v", err)
	}
	for _, f := range result.Modules[0].Remaining {
		if f.Rule == "domain" && f.Severity == module.LevelWarning {
			t.Errorf("un domaine reverse-DNS valide ne doit pas produire de WARNING: %+v", f)
		}
	}
}
