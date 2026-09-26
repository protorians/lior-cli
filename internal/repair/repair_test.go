package repair

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// writeManifest replaces a module manifest with a raw JSON body, the shape a
// legacy or hand-edited manifest has on disk.
func writeManifest(t *testing.T, root, name, body string) string {
	t.Helper()
	dir := filepath.Join(root, config.ExternalModulesDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, config.ManifestFileName)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// countInstructions returns how many times a rule is reported, so a duplicated
// instruction is caught.
func countInstructions(mr ModuleResult, rule string) int {
	n := 0
	for _, ins := range mr.Instructions {
		if ins.Rule == rule {
			n++
		}
	}
	return n
}

func findInstruction(mr ModuleResult, rule string) *Instruction {
	for i := range mr.Instructions {
		if mr.Instructions[i].Rule == rule {
			return &mr.Instructions[i]
		}
	}
	return nil
}

// A finding the repair cannot fix is reported by the repair pass and again by
// the re-audit pass: it must be listed once.
func TestRepairReportsEachInstructionOnce(t *testing.T) {
	root := setupProject(t)
	createTestModule(t, root, "dedup")

	// Break the entry so the audit raises index.tsx findings the repair has to
	// turn into instructions.
	entry := filepath.Join(root, config.ExternalModulesDir, "mod.liorian.dedup", config.ModuleEntryFileName)
	if err := os.WriteFile(entry, []byte("export const x = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &Repairer{Root: root, SkipInstall: true}
	result, err := r.RepairModules("mod.liorian.dedup")
	if err != nil {
		t.Fatalf("RepairModules: %v", err)
	}
	mr := result.Modules[0]

	for _, rule := range []string{"export", "declaration"} {
		if n := countInstructions(mr, rule); n != 1 {
			t.Errorf("instruction %q rapportée %d fois, want 1: %+v", rule, n, mr.Instructions)
		}
	}
	// The whole report must not grow a second copy of the same finding set.
	if len(mr.Instructions) != 2 {
		t.Errorf("Instructions = %d, want 2: %+v", len(mr.Instructions), mr.Instructions)
	}
}

// A valid permission array must never be emptied: only the codes the canonical
// mapping resolves are migrated, the ambiguous ones are kept as they are.
func TestRepairMigratesPermissionsWithoutLosingCodes(t *testing.T) {
	root := setupProject(t)
	path := writeManifest(t, root, "mod.liorian.perm", `{
  "id": "perm",
  "domain": "mod.liorian.perm",
  "name": "Perm",
  "version": "1.0.0",
  "entry": "index.tsx",
  "type": "WEB_APP_LOCAL",
  "token": "3f1a2b4c-5d6e-7f80-9a1b-2c3d4e5f6a7b",
  "permissions": ["user.read", "admin.delete", "editor.write"],
  "capabilities": ["core:default"]
}`)
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), config.ModuleEntryFileName), []byte("export default {};\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &Repairer{Root: root, SkipInstall: true, IncludeWarnings: true}
	if _, err := r.RepairModules("mod.liorian.perm"); err != nil {
		t.Fatalf("RepairModules: %v", err)
	}

	m, err := module.LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"User:Get", "Admin:Delete", "editor.write"}
	if len(m.Permissions) != len(want) {
		t.Fatalf("Permissions = %v, want %v", m.Permissions, want)
	}
	for i, code := range want {
		if m.Permissions[i] != code {
			t.Errorf("Permissions[%d] = %q, want %q (total %v)", i, m.Permissions[i], code, m.Permissions)
		}
	}

	// The unresolvable code is reported, once, with the reason it was kept.
	result, err := r.RepairModules("mod.liorian.perm")
	if err != nil {
		t.Fatalf("RepairModules: %v", err)
	}
	if n := countInstructions(result.Modules[0], "permissions"); n != 1 {
		t.Errorf("instruction 'permissions' rapportée %d fois, want 1", n)
	}
}

// The platform declaration must never be narrowed: a module serving desktop
// keeps doing so, and only the missing modes are filled in.
func TestRepairPlatformsPreservesSupportedFlags(t *testing.T) {
	root := setupProject(t)
	path := writeManifest(t, root, "mod.liorian.plat", `{
  "id": "plat",
  "domain": "mod.liorian.plat",
  "name": "Plat",
  "version": "1.0.0",
  "entry": "index.tsx",
  "type": "WEB_APP_LOCAL",
  "token": "3f1a2b4c-5d6e-7f80-9a1b-2c3d4e5f6a7b",
  "permissions": [],
  "capabilities": ["core:default"],
  "platforms": {
    "web": {"supported": true},
    "desktop": {"supported": true, "os": ["macos"]},
    "mobile": {"supported": false}
  }
}`)
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), config.ModuleEntryFileName), []byte("export default {};\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &Repairer{Root: root, SkipInstall: true, IncludeWarnings: true}
	if _, err := r.RepairModules("mod.liorian.plat"); err != nil {
		t.Fatalf("RepairModules: %v", err)
	}

	m, err := module.LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Platforms.Desktop.Supported {
		t.Error("le support desktop ne doit pas être perdu par un repair")
	}
	if len(m.Platforms.Desktop.Modes) == 0 {
		t.Error("le mode manquant de desktop doit être ajouté")
	}
	if len(m.Platforms.Desktop.OS) != 1 || m.Platforms.Desktop.OS[0] != "macos" {
		t.Errorf("Desktop.OS = %v, want [macos]", m.Platforms.Desktop.OS)
	}
	if m.Platforms.Mobile.Supported {
		t.Error("une plateforme non supportée ne doit pas devenir supportée")
	}
}

// A deprecated distribution type is migrated to its canonical equivalent.
func TestRepairMigratesLegacyModuleType(t *testing.T) {
	root := setupProject(t)
	path := writeManifest(t, root, "mod.liorian.legacy-type", `{
  "id": "legacy-type",
  "domain": "mod.liorian.legacy-type",
  "name": "Legacy Type",
  "version": "1.0.0",
  "entry": "index.tsx",
  "type": "INTERNAL",
  "token": "3f1a2b4c-5d6e-7f80-9a1b-2c3d4e5f6a7b",
  "permissions": [],
  "capabilities": ["core:default"]
}`)
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), config.ModuleEntryFileName), []byte("export default {};\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &Repairer{Root: root, SkipInstall: true, IncludeWarnings: true}
	result, err := r.RepairModules("mod.liorian.legacy-type")
	if err != nil {
		t.Fatalf("RepairModules: %v", err)
	}
	if n := countInstructions(result.Modules[0], "type"); n != 0 {
		t.Errorf("le type legacy doit être migré, %d instruction(s) restante(s)", n)
	}

	m, err := module.LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Type != "SYSTEM" {
		t.Errorf("Type = %q, want SYSTEM", m.Type)
	}
}

// A type outside the enum is left to the developer, with the allowed values.
func TestRepairReportsTypeOutsideEnum(t *testing.T) {
	root := setupProject(t)
	path := writeManifest(t, root, "mod.liorian.bad-type", `{
  "id": "bad-type",
  "domain": "mod.liorian.bad-type",
  "name": "Bad Type",
  "version": "1.0.0",
  "entry": "index.tsx",
  "type": "NOPE",
  "token": "3f1a2b4c-5d6e-7f80-9a1b-2c3d4e5f6a7b",
  "permissions": [],
  "capabilities": ["core:default"]
}`)
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), config.ModuleEntryFileName), []byte("export default {};\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &Repairer{Root: root, SkipInstall: true, IncludeWarnings: true}
	result, err := r.RepairModules("mod.liorian.bad-type")
	if err != nil {
		t.Fatalf("RepairModules: %v", err)
	}
	ins := findInstruction(result.Modules[0], "type")
	if ins == nil {
		t.Fatalf("une instruction 'type' est attendue: %+v", result.Modules[0].Instructions)
	}
	joined := strings.Join(ins.Steps, " ")
	if !strings.Contains(joined, "CONFIGURATION") {
		t.Errorf("l'instruction doit lister les types canoniques: %q", joined)
	}
}

// A CONFIGURATION module is repaired towards its declarative entry, never the
// React one, and its dataModel is reported with actionable guidance.
func TestRepairConfigurationModule(t *testing.T) {
	root := setupProject(t)
	dir := filepath.Join(root, config.ExternalModulesDir, "mod.acme.crm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, config.ManifestFileName)
	if err := os.WriteFile(path, []byte(`{
  "id": "crm",
  "domain": "mod.acme.crm",
  "name": "Crm",
  "version": "1.0.0",
  "type": "CONFIGURATION",
  "entry": "index.tsx",
  "token": "3f1a2b4c-5d6e-7f80-9a1b-2c3d4e5f6a7b",
  "permissions": [],
  "capabilities": ["core:default"]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &Repairer{Root: root, SkipInstall: true, IncludeWarnings: true}
	result, err := r.RepairModules("mod.acme.crm")
	if err != nil {
		t.Fatalf("RepairModules: %v", err)
	}

	m, err := module.LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Entry != "index.json" {
		t.Errorf("Entry = %q, want index.json (une CONFIGURATION n'a pas d'entrée React)", m.Entry)
	}

	ins := findInstruction(result.Modules[0], "dataModel")
	if ins == nil {
		t.Fatalf("une instruction 'dataModel' est attendue: %+v", result.Modules[0].Instructions)
	}
	if !strings.Contains(strings.Join(ins.Steps, " "), "Role:Verbe") {
		t.Errorf("l'instruction dataModel doit guider la déclaration: %+v", ins.Steps)
	}
	if n := countInstructions(result.Modules[0], "dataModel"); n != 1 {
		t.Errorf("instruction 'dataModel' rapportée %d fois, want 1", n)
	}
}

// The canonical compatibility object is completed in a single pass, both
// windows, carrying the legacy values over.
func TestRepairFillsBothCompatibilityWindows(t *testing.T) {
	root := setupProject(t)
	path := writeManifest(t, root, "mod.liorian.compat", `{
  "id": "compat",
  "domain": "mod.liorian.compat",
  "name": "Compat",
  "version": "1.0.0",
  "entry": "index.tsx",
  "type": "WEB_APP_LOCAL",
  "token": "3f1a2b4c-5d6e-7f80-9a1b-2c3d4e5f6a7b",
  "permissions": [],
  "capabilities": ["core:default"],
  "managerCompatibility": {"max": "0.17.x"},
  "apiCompatibility": {"min": ""}
}`)
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), config.ModuleEntryFileName), []byte("export default {};\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &Repairer{Root: root, SkipInstall: true, IncludeWarnings: true}
	if _, err := r.RepairModules("mod.liorian.compat"); err != nil {
		t.Fatalf("RepairModules: %v", err)
	}

	m, err := module.LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Compatibility == nil {
		t.Fatal("compatibility doit être ajouté")
	}
	// The legacy upper bound is carried over; the missing lower bound becomes an
	// open one rather than an invented version.
	if m.Compatibility.Socle.Min != openRangeMin || m.Compatibility.Socle.Max != "0.17.x" {
		t.Errorf("Socle = %+v, want min %q et max 0.17.x", m.Compatibility.Socle, openRangeMin)
	}
	if m.Compatibility.API.Min != openRangeMin || m.Compatibility.API.Max != "" {
		t.Errorf("API = %+v, want min %q sans max", m.Compatibility.API, openRangeMin)
	}

	// One pass is enough: nothing is left to repair.
	result, err := r.RepairModules("mod.liorian.compat")
	if err != nil {
		t.Fatalf("RepairModules: %v", err)
	}
	if len(result.Modules[0].Actions) != 0 {
		t.Errorf("la convergence doit tenir en une passe, corrections restantes: %+v", result.Modules[0].Actions)
	}
	if n := countInstructions(result.Modules[0], "compatibility"); n != 0 {
		t.Errorf("la fenêtre canonique doit être complète, %d instruction(s) restante(s)", n)
	}
}

// The repair must not inject the zero value of every absent field: an empty
// publisher block would make the audit raise a warning no repair can clear.
func TestRepairDoesNotInjectAbsentFields(t *testing.T) {
	root := setupProject(t)
	path := writeManifest(t, root, "mod.liorian.sparse", `{
  "id": "sparse",
  "domain": "mod.liorian.sparse",
  "name": "Sparse",
  "version": "not-semver",
  "entry": "index.tsx",
  "type": "WEB_APP_LOCAL",
  "capabilities": ["core:default"]
}`)
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), config.ModuleEntryFileName),
		[]byte("export default { identifier: 'mod.liorian.sparse', widgets: {} };\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &Repairer{Root: root, SkipInstall: true, IncludeWarnings: true}
	if _, err := r.RepairModules("mod.liorian.sparse"); err != nil {
		t.Fatalf("RepairModules: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("manifeste illisible après repair: %v\n%s", err, data)
	}
	for _, absent := range []string{"publisher", "isEnabled", "isDefault", "menu", "requirements", "widgets", "routines", "apiScopes", "managerCompatibility", "apiCompatibility"} {
		if v, ok := fields[absent]; ok {
			t.Errorf("le champ absent %q ne doit pas être injecté, obtenu %s", absent, v)
		}
	}
	// The fields the repair did fix are present.
	for _, fixed := range []string{"version", "token"} {
		if _, ok := fields[fixed]; !ok {
			t.Errorf("le champ corrigé %q doit être écrit", fixed)
		}
	}

	// A second pass must be a no-op: no warning the repair cannot clear.
	result, err := r.RepairModules("mod.liorian.sparse")
	if err != nil {
		t.Fatalf("RepairModules: %v", err)
	}
	if len(result.Modules[0].Actions) != 0 || len(result.Modules[0].Instructions) != 0 {
		t.Errorf("la seconde passe doit être neutre, got actions=%+v instructions=%+v",
			result.Modules[0].Actions, result.Modules[0].Instructions)
	}
}

// A legacy reverse-DNS domain is migrated to the canonical form, and the module
// directory follows it.
func TestRepairMigratesCanonicalDomainAndRenames(t *testing.T) {
	root := setupProject(t)
	dir := filepath.Join(root, config.ExternalModulesDir, "com.example.blog")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, config.ManifestFileName), []byte(`{
  "id": "blog",
  "domain": "com.example.blog",
  "name": "Blog",
  "version": "1.0.0",
  "entry": "index.tsx",
  "type": "WEB_APP_LOCAL",
  "token": "3f1a2b4c-5d6e-7f80-9a1b-2c3d4e5f6a7b",
  "permissions": [],
  "capabilities": ["core:default"]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, config.ModuleEntryFileName), []byte("export default {};\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &Repairer{Root: root, SkipInstall: true, IncludeWarnings: true, NoInteraction: true}
	result, err := r.RepairModules("com.example.blog")
	if err != nil {
		t.Fatalf("RepairModules: %v", err)
	}
	if got := result.Modules[0].Module; got != "mod.example.blog" {
		t.Errorf("Module = %q, want mod.example.blog", got)
	}
	moved := filepath.Join(root, config.ExternalModulesDir, "mod.example.blog")
	if !pkg.DirExists(moved) {
		t.Fatalf("le dossier doit être renommé en mod.example.blog")
	}
	m, err := module.LoadManifest(filepath.Join(moved, config.ManifestFileName))
	if err != nil {
		t.Fatal(err)
	}
	if m.Domain != "mod.example.blog" {
		t.Errorf("Domain = %q, want mod.example.blog", m.Domain)
	}
	// The instructions must not send the developer to the directory that is
	// now gone.
	for _, ins := range result.Modules[0].Instructions {
		for _, step := range ins.Steps {
			if strings.Contains(step, "com.example.blog/") {
				t.Errorf("l'instruction %q/%q pointe encore vers l'ancien dossier: %q", ins.Category, ins.Rule, step)
			}
		}
	}
}

// A dry run proposes the rename a real run would perform, without touching disk.
func TestRepairDryRunProposesDomainMigration(t *testing.T) {
	root := setupProject(t)
	dir := filepath.Join(root, config.ExternalModulesDir, "com.example.dry")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, config.ManifestFileName), []byte(`{
  "id": "dry",
  "domain": "com.example.dry",
  "name": "Dry",
  "version": "1.0.0",
  "entry": "index.tsx",
  "type": "WEB_APP_LOCAL",
  "token": "3f1a2b4c-5d6e-7f80-9a1b-2c3d4e5f6a7b",
  "permissions": [],
  "capabilities": ["core:default"]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, config.ModuleEntryFileName), []byte("export default {};\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &Repairer{Root: root, SkipInstall: true, IncludeWarnings: true, DryRun: true, NoInteraction: true}
	result, err := r.RepairModules("com.example.dry")
	if err != nil {
		t.Fatalf("RepairModules: %v", err)
	}

	var proposed bool
	for _, a := range result.Modules[0].Actions {
		if a.Rule == "domain directory" && strings.Contains(a.Detail, "mod.example.dry") {
			proposed = true
		}
		if a.Applied {
			t.Errorf("aucune correction ne doit être appliquée en dry run: %+v", a)
		}
	}
	if !proposed {
		t.Errorf("le dry run doit proposer le renommage canonique: %+v", result.Modules[0].Actions)
	}
	if !pkg.DirExists(dir) {
		t.Error("le dry run ne doit pas renommer le dossier")
	}
	if pkg.DirExists(filepath.Join(root, config.ExternalModulesDir, "mod.example.dry")) {
		t.Error("le dry run ne doit pas créer le dossier cible")
	}
}

// The install must run where the package.json the audit reads lives: the
// module's own directory, not the project root.
func TestRepairInstallDirPrefersModuleDirectory(t *testing.T) {
	root := setupProject(t)
	createTestModule(t, root, "install-dir")

	r := &Repairer{Root: root, SkipInstall: true}
	want := filepath.Join(root, config.ExternalModulesDir, "mod.liorian.install-dir")
	if got := r.installDir("mod.liorian.install-dir"); got != want {
		t.Errorf("installDir = %q, want %q", got, want)
	}

	// A module without its own package.json falls back to the project root.
	if err := os.Remove(filepath.Join(want, "package.json")); err != nil {
		t.Fatal(err)
	}
	if got := r.installDir("mod.liorian.install-dir"); got != root {
		t.Errorf("installDir sans package.json module = %q, want la racine %q", got, root)
	}
}

// A dependency that is not installed is reported as such — never as a
// "dependency install is not installed" pseudo-dependency.
func TestRepairNeverReportsAPseudoDependency(t *testing.T) {
	root := setupProject(t)
	createTestModule(t, root, "pseudo-dep")
	entries, err := os.ReadDir(filepath.Join(root, "node_modules"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(root, "node_modules", e.Name())); err != nil {
			t.Fatal(err)
		}
	}

	// SkipInstall leaves the install to the re-audit, which reports each real
	// dependency by name.
	r := &Repairer{Root: root, SkipInstall: true}
	result, err := r.RepairModules("mod.liorian.pseudo-dep")
	if err != nil {
		t.Fatalf("RepairModules: %v", err)
	}
	mr := result.Modules[0]

	var reported int
	for _, ins := range mr.Instructions {
		joined := strings.Join(ins.Steps, " ")
		if strings.Contains(joined, "node_modules/install") {
			t.Errorf("instruction %q/%q décrit un pseudo-dépendance: %q", ins.Category, ins.Rule, joined)
		}
		if ins.Category == "dependencies" && ins.Rule != "install" {
			reported++
		}
	}
	if reported == 0 {
		t.Errorf("les dépendances non installées doivent être rapportées: %+v", mr.Instructions)
	}
	seen := map[string]bool{}
	for _, ins := range mr.Instructions {
		key := ins.Category + "|" + ins.Rule
		if seen[key] {
			t.Errorf("instruction %q dupliquée: %+v", key, mr.Instructions)
		}
		seen[key] = true
	}
}
