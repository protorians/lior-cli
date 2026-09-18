package module

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/protorians/liorian-cli/internal/pkg"
)

// setupProject creates a project root with one module via the Creator.
func setupProject(t *testing.T) (root string, creator *Creator) {
	t.Helper()
	root = t.TempDir()
	creator = &Creator{Root: root}
	return root, creator
}

// specFor builds a ModuleSpec whose folder domain is derived from the
// identifier.
func specFor(id, desc string) ModuleSpec {
	return ModuleSpec{Domain: "com.example." + id, ID: id, Description: desc}
}

func TestCreateModuleStructure(t *testing.T) {
	root, creator := setupProject(t)
	res, err := creator.Create(specFor("blog-manager", "Gestion de blog"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if res.Token == "" {
		t.Error("Token vide")
	}
	if res.Domain != "com.example.blog-manager" || res.ID != "blog-manager" {
		t.Errorf("CreateResult identity = %s / %s", res.Domain, res.ID)
	}

	expected := []string{
		"manifest.json",
		"index.tsx",
		"package.json",
		"README.md",
		"application/service/blog-manager-api-service.ts",
		"domain/blog-manager.interface.ts",
		"domain/enums/blog-manager-status.enum.ts",
		"infrastructure/routines/blog-manager-analytics.routine.ts",
		"presentation/components/blog-manager-columns.tsx",
		"presentation/components/blog-manager-data-grid.tsx",
		"presentation/components/blog-manager-details-sheet.tsx",
		"presentation/components/create-blog-manager-dialog.tsx",
		"presentation/providers/blog-manager-header.provider.tsx",
		"presentation/views/blog-manager.view.tsx",
		"presentation/widgets/blog-manager.widget.tsx",
	}
	for _, rel := range expected {
		p := filepath.Join(root, "external_modules", "com.example.blog-manager", filepath.FromSlash(rel))
		if _, err := os.Stat(p); err != nil {
			t.Errorf("fichier attendu manquant : %s (%v)", p, err)
		}
	}

	// The declaration declares a uri → the matching page is scaffolded.
	page := filepath.Join(root, "src", "app", "blog-manager", "page.tsx")
	if !pkg.FileExists(page) {
		t.Errorf("page attendue manquante : %s", page)
	}

	// No mockup spelling must survive the rename.
	dir := filepath.Join(root, "external_modules", "com.example.blog-manager")
	if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(path, "hello-world") {
			t.Errorf("résidu du mockup dans le chemin : %s", path)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk de %s: %v", dir, err)
	}
}

func TestCreateModuleDescriptionInIndex(t *testing.T) {
	root, creator := setupProject(t)
	if _, err := creator.Create(ModuleSpec{Domain: "com.example.blog-manager", ID: "blog-manager", Description: "Gestion de blog et d'articles"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "external_modules", "com.example.blog-manager", "index.tsx"))
	if err != nil {
		t.Fatalf("lecture index.tsx: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `description: 'Gestion de blog et d\'articles',`) {
		t.Errorf("index.tsx doit contenir la description fournie:\n%s", content)
	}
}

func TestCreateModuleDuplicate(t *testing.T) {
	_ = t.TempDir()
	_, creator := setupProject(t)
	if _, err := creator.Create(specFor("blog", "")); err != nil {
		t.Fatalf("premier Create: %v", err)
	}
	if _, err := creator.Create(specFor("blog", "")); err == nil {
		t.Error("second Create doit échouer sur un module existant")
	}
}

func TestArchiveStructure(t *testing.T) {
	root, creator := setupProject(t)
	if _, err := creator.Create(specFor("blog-manager", "")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// assets dir (named after the module domain)
	assetPath := filepath.Join(root, "public", "assets", "com.example.blog-manager", "logo.svg")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assetPath, []byte("<svg/>"), 0o644); err != nil {
		t.Fatal(err)
	}

	// app src dir (spec FR-010), driven by the manifest uri (/blog-manager)
	appSrcPath := filepath.Join(root, "src", "app", "blog-manager", "page.tsx")
	if err := os.MkdirAll(filepath.Dir(appSrcPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(appSrcPath, []byte("<div/>"), 0o644); err != nil {
		t.Fatal(err)
	}

	packer := &Packer{Root: root}
	res, err := packer.Pack("com.example.blog-manager")
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	if res.Size == 0 {
		t.Error("archive vide")
	}

	zr, err := zip.OpenReader(res.Path)
	if err != nil {
		t.Fatalf("ouverture de l'archive: %v", err)
	}
	defer zr.Close()

	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
		if strings.HasSuffix(f.Name, "logo.svg") {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("lecture logo.svg: %v", err)
			}
			data, _ := io.ReadAll(rc)
			rc.Close()
			if string(data) != "<svg/>" {
				t.Errorf("contenu asset incorrect: %q", data)
			}
		}
	}

	joined := strings.Join(names, "\n")
	for _, want := range []string{
		"external_modules/com.example.blog-manager/manifest.json",
		"external_modules/com.example.blog-manager/index.tsx",
		"src/app/blog-manager/page.tsx",
		"public/assets/com.example.blog-manager/logo.svg",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("l'archive doit contenir %q (contenu: %s)", want, joined)
		}
	}
}

func TestPackRejectsInvalidModule(t *testing.T) {
	root := t.TempDir()
	packer := &Packer{Root: root}
	if _, err := packer.Pack("com.example.absent"); err == nil {
		t.Error("Pack d'un module inexistant doit échouer")
	}
}

func TestValidateModuleOnCleanModule(t *testing.T) {
	root, creator := setupProject(t)
	if _, err := creator.Create(specFor("blog-manager", "")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	v := &Validator{Root: root}
	res, err := v.ValidateModule("com.example.blog-manager")
	if err != nil {
		t.Fatalf("ValidateModule: %v", err)
	}
	if res.HasErrors() {
		t.Errorf("module propre doit être valide, erreurs: %v", res.Findings)
	}
}

func TestValidateModuleMissingToken(t *testing.T) {
	root, creator := setupProject(t)
	if _, err := creator.Create(specFor("blog-manager", "")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Invalider le token
	m, err := LoadManifest(filepath.Join(root, "external_modules", "com.example.blog-manager", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	m.Token = "pas-un-uuid"
	if err := m.Save(filepath.Join(root, "external_modules", "com.example.blog-manager", "manifest.json")); err != nil {
		t.Fatal(err)
	}

	v := &Validator{Root: root}
	res, err := v.ValidateModule("com.example.blog-manager")
	if err != nil {
		t.Fatalf("ValidateModule: %v", err)
	}
	if !res.HasErrors() {
		t.Error("token invalide doit produire une erreur")
	}
}

func TestValidateModuleWarnsOnMissingCanonicalFields(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "external_modules", "com.example.legacy")
	raw := `{
  "schemaVersion": 1,
  "id": "legacy",
  "domain": "com.example.legacy",
  "key": "LEGACY",
  "name": "Legacy",
  "description": "legacy module",
  "version": "1.0.0",
  "icon": "PuzzleIcon",
  "type": "EXTERNAL",
  "entry": "index.tsx",
  "uri": "/legacy",
  "token": "3f1a2b4c-5d6e-7f80-9a1b-2c3d4e5f6a7b",
  "permissions": ["legacy.read"],
  "managerCompatibility": {"min": "0.17.1", "max": "0.17.0"}
}
`
	if err := pkg.WriteString(filepath.Join(moduleDir, "manifest.json"), raw); err != nil {
		t.Fatal(err)
	}
	if err := pkg.WriteString(filepath.Join(moduleDir, "index.tsx"), "export default { identifier: 'x', widgets: {} };\n"); err != nil {
		t.Fatal(err)
	}

	v := &Validator{Root: root}
	res, err := v.ValidateModule("com.example.legacy")
	if err != nil {
		t.Fatalf("ValidateModule: %v", err)
	}
	warned := map[string]bool{}
	for _, f := range res.Findings {
		if f.Severity == LevelWarning {
			warned[f.Rule] = true
		}
	}
	for _, rule := range []string{
		"optionalRequirements", "platforms", "capabilities", "publisher",
		"apiCompatibility", "managerCompatibility",
	} {
		if !warned[rule] {
			t.Errorf("règle %q doit produire un WARNING, findings: %+v", rule, res.Findings)
		}
	}
}

func TestLinkedModulesFiltersUnlinked(t *testing.T) {
	root, creator := setupProject(t)
	if _, err := creator.Create(specFor("mod-a", "")); err != nil {
		t.Fatalf("Create mod-a: %v", err)
	}
	if _, err := creator.Create(specFor("mod-b", "")); err != nil {
		t.Fatalf("Create mod-b: %v", err)
	}

	linker := &Linker{Root: root}
	if _, err := linker.Link("com.example.mod-a", RemoteInfo{Token: "m_abc123def456"}); err != nil {
		t.Fatalf("Link: %v", err)
	}

	// Seul mod-a (token distant) doit être listé comme lié.
	linked, err := linker.LinkedModules()
	if err != nil {
		t.Fatalf("LinkedModules: %v", err)
	}
	if len(linked) != 1 || linked[0] != "com.example.mod-a" {
		t.Errorf("seul mod-a doit être lié, obtenu: %v", linked)
	}

	// Unlink rétablit un token UUID local → aucun module lié restant.
	if err := linker.Unlink("com.example.mod-a"); err != nil {
		t.Fatalf("Unlink: %v", err)
	}
	linked, err = linker.LinkedModules()
	if err != nil {
		t.Fatalf("LinkedModules: %v", err)
	}
	if len(linked) != 0 {
		t.Errorf("aucun module ne doit être lié après unlink, obtenu: %v", linked)
	}
}

func TestLinkMergesAbsentRemoteMetadata(t *testing.T) {
	root, creator := setupProject(t)
	if _, err := creator.Create(specFor("blog-manager", "")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	linker := &Linker{Root: root}
	result, err := linker.Link("com.example.blog-manager", RemoteInfo{
		Token:         "m_abc123def456",
		Name:          "Blog Manager",
		Description:   "Gestion de blog et d'articles",
		Version:       "1.2.0",
		PublisherID:   "dev_42",
		PublisherName: "Jane Doe",
	})
	if err != nil {
		t.Fatalf("Link: %v", err)
	}

	// Result carries the remote identity.
	if result.RemoteToken != "m_abc123def456" ||
		result.RemoteName != "Blog Manager" ||
		result.RemoteVersion != "1.2.0" {
		t.Errorf("LinkResult incohérent: %+v", result)
	}

	// Manifest enriched with remote metadata (fields absent after create).
	m, err := LoadManifest(filepath.Join(root, "external_modules", "com.example.blog-manager", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Token != "m_abc123def456" {
		t.Errorf("Token = %q, want m_abc123def456", m.Token)
	}
	if m.Name != "Blog Manager" {
		t.Errorf("Name = %q, want Blog Manager", m.Name)
	}
	if m.Description != "Gestion de blog et d'articles" {
		t.Errorf("Description = %q, want Gestion de blog et d'articles", m.Description)
	}
	// Publisher comes from the reference mockup (present locally), so the
	// remote publisher (dev_42) must NOT overwrite it.
	if m.Publisher.ID != "liorian" || m.Publisher.Name != "Liorian Workspace" {
		t.Errorf("Publisher = %+v, want liorian/Liorian Workspace (valeur locale préservée)", m.Publisher)
	}

	// Existing local metadata must NOT be overwritten.
	if _, err := linker.Link("com.example.blog-manager", RemoteInfo{
		Token:       "m_new_token",
		Name:        "Nom Distant",
		Description: "Description distante",
		Version:     "2.0.0",
	}); err != nil {
		t.Fatalf("second Link: %v", err)
	}
	m, err = LoadManifest(filepath.Join(root, "external_modules", "com.example.blog-manager", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Token != "m_new_token" {
		t.Errorf("Token = %q, want m_new_token (toujours remis à jour)", m.Token)
	}
	if m.Name != "Blog Manager" {
		t.Errorf("Name = %q, la valeur locale ne doit pas être écrasée", m.Name)
	}
	if m.Description != "Gestion de blog et d'articles" {
		t.Errorf("Description = %q, la valeur locale ne doit pas être écrasée", m.Description)
	}
}

func TestLinkedModulesStateFileIgnoresTokenFormat(t *testing.T) {
	root, creator := setupProject(t)
	if _, err := creator.Create(specFor("mod-a", "")); err != nil {
		t.Fatalf("Create mod-a: %v", err)
	}

	// A remote token that happens to match the UUID format must still be
	// recognised as "linked" once recorded in the state file.
	remoteToken := pkg.NewUUID()
	linker := &Linker{Root: root}
	if _, err := linker.Link("com.example.mod-a", RemoteInfo{Token: remoteToken}); err != nil {
		t.Fatalf("Link: %v", err)
	}

	linked, err := linker.LinkedModules()
	if err != nil {
		t.Fatalf("LinkedModules: %v", err)
	}
	if len(linked) != 1 || linked[0] != "com.example.mod-a" {
		t.Errorf("mod-a (état) doit être lié, obtenu: %v", linked)
	}

	if err := linker.Unlink("com.example.mod-a"); err != nil {
		t.Fatalf("Unlink: %v", err)
	}
	linked, err = linker.LinkedModules()
	if err != nil {
		t.Fatalf("LinkedModules: %v", err)
	}
	if len(linked) != 0 {
		t.Errorf("aucun module ne doit être lié après unlink, obtenu: %v", linked)
	}
}
