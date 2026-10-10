package module

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/protorians/lior-cli/internal/config"
)

// writeModernModule scaffolds a conformant isolated-runtime module under
// <root>/modules/<id>/ (D5): manifest (main.tsx, userScope, artifact),
// tsconfig, mount/unmount entry and a built payload under the development
// layout `.liorian/artifact/` (D7).
func writeModernModule(t *testing.T, root, id string) string {
	t.Helper()
	dir := config.WorkspaceModuleDir(root, id)
	files := map[string]string{
		"manifest.json": `{
  "schemaVersion": 1,
  "id": "` + id + `",
  "domain": "com.example.` + id + `",
  "key": "TEST",
  "name": "Test Module",
  "description": "A module used by the isolated-runtime tests",
  "version": "1.0.0",
  "icon": "PuzzleIcon",
  "type": "WEB_APP_LOCAL",
  "external": true,
  "entry": "main.tsx",
  "uri": "/` + id + `",
  "token": "3f1a2b4c-5d6e-7f80-9a1b-2c3d4e5f6a7b",
  "userScope": [],
  "backends": [],
  "artifact": {"dir": "artifact", "bundle": "module.js", "document": "index.html"},
  "platforms": {"web": {"supported": true, "modes": ["web"]}},
  "permissions": [],
  "requirements": {},
  "optionalRequirements": {},
  "capabilities": ["core:default"]
}`,
		"tsconfig.json":                "{}\n",
		"package.json":                 "{\"name\":\"@liorian/module-" + id + "\",\"scripts\":{\"typecheck\":\"true\"}}\n",
		"main.tsx":                     "export function mount(el: HTMLElement): void {}\n\nexport function unmount(): void {}\n",
		".liorian/artifact/module.js":  "console.log(\"bundle\");\n",
		".liorian/artifact/index.html": "<!doctype html><html><body></body></html>\n",
	}
	for rel, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func patchModernModule(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPackModernArchiveLayout(t *testing.T) {
	root := t.TempDir()
	writeModernModule(t, root, "blog-manager")

	packer := &Packer{Root: root}
	res, err := packer.Pack("blog-manager")
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}

	zr, err := zip.OpenReader(res.Path)
	if err != nil {
		t.Fatalf("ouverture de l'archive: %v", err)
	}
	defer zr.Close()

	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	joined := strings.Join(names, "\n")

	// D4/§4.4: manifest at the archive root, sources under src/, the built
	// payload under artifact/ (development layout .liorian/artifact →
	// distribution prefix artifact/).
	for _, want := range []string{
		"manifest.json",
		"src/main.tsx",
		"src/tsconfig.json",
		"artifact/module.js",
		"artifact/index.html",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("l'archive doit contenir %q (contenu: %s)", want, joined)
		}
	}
	for _, forbidden := range []string{"node_modules", "artifact/artifact"} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("l'archive ne doit pas contenir %q", forbidden)
		}
	}
}

func TestPackModernRefusesNextImport(t *testing.T) {
	root := t.TempDir()
	dir := writeModernModule(t, root, "crm")
	patchModernModule(t, dir, "presentation/views/crm.view.tsx",
		"import Link from \"next/link\";\n\nexport function CrmView() {\n  return <Link href=\"/\">home</Link>;\n}\n")

	packer := &Packer{Root: root}
	_, err := packer.Pack("crm")
	if err == nil {
		t.Fatal("Pack doit refuser un import next/* (D1)")
	}
	if !strings.Contains(err.Error(), "validation") {
		t.Errorf("error %q should mention the validation", err)
	}
}

// D16 is enforced on the module's **own source**. The bundle embeds the SDK
// runtime, whose ApiService legitimately calls fetch, so a bundle hit carries no
// information about the module: refusing on it would make every module
// unpackable, accepting it silently would drop the rule. The source is where
// the decision can be made, so the source is where the pack refuses.
func TestPackModernRefusesFetchInSource(t *testing.T) {
	root := t.TempDir()
	dir := writeModernModule(t, root, "crm")
	patchModernModule(t, dir, "presentation/views/crm.view.tsx",
		"export function CrmView() {\n  const r = await fetch(\"https://evil.example\");\n  return null;\n}\n")

	packer := &Packer{Root: root}
	_, err := packer.Pack("crm")
	if err == nil {
		t.Fatal("Pack doit refuser fetch() dans les sources du module (D16)")
	}
	if !strings.Contains(err.Error(), "validation") {
		t.Errorf("error %q should mention the validation", err)
	}
}

// A module that talks to the network through the SDK passes D16 even though the
// built bundle contains fetch (the SDK embeds it).
func TestPackModernAcceptsSDKNetworkUsage(t *testing.T) {
	root := t.TempDir()
	dir := writeModernModule(t, root, "crm")
	patchModernModule(t, dir, ".liorian/artifact/module.js",
		"const r = await fetch(\"/api/contacts\");\n")

	packer := &Packer{Root: root}
	if _, err := packer.Pack("crm"); err != nil {
		t.Fatalf("Pack doit accepter le fetch embarqué du SDK : %v", err)
	}
}

func TestPackModernRefusesAtAliasImport(t *testing.T) {
	root := t.TempDir()
	dir := writeModernModule(t, root, "crm")
	patchModernModule(t, dir, "main.tsx",
		"import { thing } from \"@/core/thing\";\n\nexport function mount(): void {}\nexport function unmount(): void {}\n")

	packer := &Packer{Root: root}
	if _, err := packer.Pack("crm"); err == nil {
		t.Fatal("Pack doit refuser un import @/ (§7.7)")
	}
}

func TestValidateModernRequiresUserScope(t *testing.T) {
	root := t.TempDir()
	dir := writeModernModule(t, root, "crm")
	// Remove userScope from the manifest (D15: refused at pack).
	manifest := filepath.Join(dir, "manifest.json")
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	patched := strings.Replace(string(data), "\"userScope\": [],", "", 1)
	if err := os.WriteFile(manifest, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := (&Validator{}).ValidateModuleDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasErrors() {
		t.Fatal("un module moderne sans userScope doit être refusé (D15)")
	}
}

func TestValidateModernRequiresArtifact(t *testing.T) {
	root := t.TempDir()
	dir := writeModernModule(t, root, "crm")
	if err := os.Remove(filepath.Join(dir, ".liorian", "artifact", "module.js")); err != nil {
		t.Fatal(err)
	}

	res, err := (&Validator{}).ValidateModuleDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasErrors() {
		t.Fatal("un module moderne sans artifact/module.js doit être refusé (§4.4 règle 3)")
	}
}

func TestValidateModernRequiresMountUnmount(t *testing.T) {
	root := t.TempDir()
	dir := writeModernModule(t, root, "crm")
	patchModernModule(t, dir, "main.tsx", "export default {}\n")

	res, err := (&Validator{}).ValidateModuleDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasErrors() {
		t.Fatal("une entrée sans mount/unmount doit être refusée (§4.2)")
	}
}

func TestValidateBackendDeclarations(t *testing.T) {
	valid := []BackendDeclaration{
		{Key: "erp-prod", URL: "https://erp.acme.com/api", Scopes: []string{"User:Get"}},
		{Key: "dev-local", URL: "http://127.0.0.1:8080"},
		{Key: "dev-localhost", URL: "http://localhost:3000/api"},
	}
	for _, b := range valid {
		if err := ValidateBackendDeclaration(b); err != nil {
			t.Errorf("ValidateBackendDeclaration(%+v) = %v, want nil", b, err)
		}
	}

	invalid := []struct {
		b    BackendDeclaration
		why  string
		want string
	}{
		{BackendDeclaration{Key: "ERP", URL: "https://erp.acme.com"}, "key non kebab", "kebab-case"},
		{BackendDeclaration{Key: "erp-prod", URL: "http://erp.acme.com"}, "http non loopback", "https expected"},
		{BackendDeclaration{Key: "erp-prod", URL: ""}, "url vide", "https expected"},
		{BackendDeclaration{Key: "erp-prod", URL: "https://user:pass@erp.acme.com"}, "userinfo", "userinfo"},
		{BackendDeclaration{Key: "erp-prod", URL: "https://erp.acme.com/api/../admin"}, "traversée", "dot-segment"},
		{BackendDeclaration{Key: "erp-prod", URL: "https://erp.acme.com", Scopes: []string{"user.read"}}, "scope hors grammaire", "Role:Verbe"},
	}
	for _, c := range invalid {
		err := ValidateBackendDeclaration(c.b)
		if err == nil {
			t.Errorf("ValidateBackendDeclaration(%+v) = nil, want une erreur (%s)", c.b, c.why)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("erreur %q devrait mentionner %q (%s)", err, c.want, c.why)
		}
	}
}

func TestValidateUserScopeEntries(t *testing.T) {
	for _, ok := range []string{"User:Get", "Admin:Delete", "Viewer:Post"} {
		if err := ValidateUserScopeEntry(ok); err != nil {
			t.Errorf("ValidateUserScopeEntry(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"user.read", "User", "User:Get:All", ""} {
		if err := ValidateUserScopeEntry(bad); err == nil {
			t.Errorf("ValidateUserScopeEntry(%q) = nil, want une erreur", bad)
		}
	}
}

// The `access` gate accepts a bare role name and the `<Role>:<Niveau>` couple
// (§6.5), with a numeric level bounded to 0–99.99.
func TestValidateAccessEntries(t *testing.T) {
	for _, ok := range []string{"Admin", "Root", "Root:80", "Manager:70", "Admin:99.99", "Editor:60"} {
		if err := ValidateAccessEntry(ok); err != nil {
			t.Errorf("ValidateAccessEntry(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "1Admin", "Admin:Wizzard", "Admin:", ":80", "Admin:100", "Admin:1.234", "Admin::80"} {
		if err := ValidateAccessEntry(bad); err == nil {
			t.Errorf("ValidateAccessEntry(%q) = nil, want une erreur", bad)
		}
	}
}

// The assistive-help standard (`module.helpers.json`) is validated fail-closed:
// a valid file passes, a malformed one reports every problem.
func TestValidateModuleHelpersFile(t *testing.T) {
	valid := &ModuleHelpersFile{
		Version: 1,
		Anchors: []ModuleHelpersAnchor{{ID: "creer", Target: "[data-help=\"x\"]", Content: "Ouvre le formulaire."}},
		Tours:   []ModuleHelpersTour{{ID: "decouverte", Title: "Tour", Steps: []ModuleHelpersStep{{Content: "Étape"}}}},
	}
	if errs := ValidateModuleHelpersFile(valid); len(errs) != 0 {
		t.Fatalf("fichier d'aide valide rejeté : %v", errs)
	}

	broken := &ModuleHelpersFile{
		Version: 2,
		Anchors: []ModuleHelpersAnchor{{ID: "Bad Id", Content: ""}},
		Tours: []ModuleHelpersTour{
			{ID: "tour", Steps: []ModuleHelpersStep{}},                             // title missing, steps empty
			{ID: "tour", Title: "dup", Steps: []ModuleHelpersStep{{Content: "x"}}}, // duplicate id
		},
	}
	if errs := ValidateModuleHelpersFile(broken); len(errs) == 0 {
		t.Fatal("fichier d'aide invalide accepté")
	}
}

// A malformed module.helpers.json blocks the pack — the socle would render it
// silently otherwise.
func TestValidateModernRefusesInvalidHelpers(t *testing.T) {
	root := t.TempDir()
	dir := writeModernModule(t, root, "crm")
	patchModernModule(t, dir, ModuleHelpersFileName,
		`{"version":1,"anchors":[{"id":"Bad Id","target":"","content":""}]}`)

	res, err := (&Validator{}).ValidateModuleDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasErrors() {
		t.Fatal("un module.helpers.json invalide doit être refusé (fail-closed)")
	}
}

// The optional module.helpers.json is embedded under `src/module.helpers.json`
// (spec `assistive-help` §2), like artifact.config.json.
func TestPackModernEmbedsModuleHelpers(t *testing.T) {
	root := t.TempDir()
	dir := writeModernModule(t, root, "crm")
	patchModernModule(t, dir, ModuleHelpersFileName,
		`{"version":1,"anchors":[{"id":"creer","target":"[data-help=\"x\"]","content":"Ouvre le formulaire."}]}`)

	res, err := (&Packer{Root: root}).Pack("crm")
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	zr, err := zip.OpenReader(res.Path)
	if err != nil {
		t.Fatalf("ouverture de l'archive: %v", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == "src/"+ModuleHelpersFileName {
			return
		}
	}
	t.Errorf("l'archive doit embarquer src/%s", ModuleHelpersFileName)
}

func TestModernPackPrefersWorkspaceOverLegacy(t *testing.T) {
	root := t.TempDir()
	writeModernModule(t, root, "blog-manager")

	// A same-named legacy module in library/modules must not shadow the
	// workspace source (D5).
	legacy := config.ModuleDir(root, "blog-manager")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := (&Packer{Root: root}).Pack("blog-manager")
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	zr, err := zip.OpenReader(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, config.ExternalModulesDir+"/") {
			t.Errorf("l'archive moderne ne doit pas contenir le layout legacy: %s", f.Name)
		}
	}
}
