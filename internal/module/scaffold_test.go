package module

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/protorians/liorian-cli/internal/pkg"
)

// writeScaffoldFixture builds a minimal hello-world-style module mockup plus a
// page mockup, mirroring the reference mockups used by `liorian create module`.
func writeScaffoldFixture(t *testing.T) (mockupDir, pageMockup string) {
	t.Helper()
	base := t.TempDir()
	mockupDir = filepath.Join(base, "mockups", "hello-world")
	pageMockup = filepath.Join(base, "page-mockup.tsx")

	mustWrite := func(path, data string) {
		if err := pkg.WriteString(path, data); err != nil {
			t.Fatalf("WriteString(%s): %v", path, err)
		}
	}

	mustWrite(filepath.Join(mockupDir, "manifest.json"), `{
  "schemaVersion": 1,
  "id": "hello-world",
  "domain": "mod.liorian.helloworld",
  "key": "HELLO_WORLD",
  "name": "Hello World",
  "description": "Module d'exemple pour l'onboarding des développeurs",
  "version": "1.0.0",
  "entry": "index.tsx",
  "uri": "/hello-world",
  "permissions": ["hello-world.read", "hello-world.write"],
  "routines": ["helloWorldAnalyticsRoutine"],
  "menu": {
    "items": [
      { "label": "Salutations", "icon": "WandSparklesIcon", "url": "/hello-world" }
    ]
  }
}
`)

	mustWrite(filepath.Join(mockupDir, "index.tsx"), `import {ModuleDeclarationInterface} from "@liorian/sdk/domain/entities/module.interface";
import {HelloWorldWidget} from "./presentation/widgets/hello-world.widget";

const helloWorldModule: ModuleDeclarationInterface = {
    identifier: 'mod.liorian.helloworld',
    key: 'HELLO_WORLD',
    name: 'Hello World',
    description: 'Module d\'exemple pour l\'onboarding des développeurs',
    uri: '/hello-world',
    widgets: {
        analytics: HelloWorldWidget
    },
}

export default helloWorldModule
`)

	mustWrite(filepath.Join(mockupDir, "application", "service", "hello-world-api-service.ts"), `import {HelloWorldInterface} from "../../domain/hello-world.interface";

export class HelloWorldApiService {
    static async getAll() {
        return await this.get('/hello-world/');
    }
}
`)

	mustWrite(filepath.Join(mockupDir, "presentation", "views", "hello-world.view.tsx"), `"use client"
import {HelloWorldWidget} from "../widgets/hello-world.widget";

export function HelloWorldView() {
    return <div>Hello World</div>;
}
`)

	mustWrite(filepath.Join(mockupDir, "presentation", "widgets", "hello-world.widget.tsx"), `"use client"
export function HelloWorldWidget() {
    const key = ['hello-world', 'widget'];
    return null;
}
`)

	mustWrite(pageMockup, `import {HelloWorldView} from "@/external_modules/hello-world/presentation/views/hello-world.view";

export default function HelloWorldPage() {
    return <HelloWorldView/>;
}
`)

	return mockupDir, pageMockup
}

func TestCreateFromMockupRenamesComponents(t *testing.T) {
	mockupDir, pageMockup := writeScaffoldFixture(t)
	root := t.TempDir()
	creator := &Creator{Root: root, MockupDir: mockupDir, PageMockup: pageMockup}

	res, err := creator.Create(ModuleSpec{Domain: "com.example.blog-manager", ID: "blog-manager", Description: "Gestion de blog d'articles"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	moduleDir := filepath.Join(root, "external_modules", "com.example.blog-manager")

	// Files renamed with the new module name.
	for _, rel := range []string{
		"application/service/blog-manager-api-service.ts",
		"presentation/views/blog-manager.view.tsx",
		"presentation/widgets/blog-manager.widget.tsx",
		"README.md",
	} {
		if !pkg.FileExists(filepath.Join(moduleDir, filepath.FromSlash(rel))) {
			t.Errorf("renamed file missing: %s", rel)
		}
	}
	for _, rel := range []string{
		"application/service/hello-world-api-service.ts",
		"presentation/views/hello-world.view.tsx",
		"presentation/widgets/hello-world.widget.tsx",
	} {
		if pkg.PathExists(filepath.Join(moduleDir, filepath.FromSlash(rel))) {
			t.Errorf("original filename still present: %s", rel)
		}
	}

	// Declared URI on the module → page scaffolded in src/app.
	if res.Page == "" {
		t.Fatal("Page vide : la déclaration du module déclare une uri")
	}
	wantPage := filepath.Join(root, "src", "app", "blog-manager", "page.tsx")
	if res.Page != wantPage {
		t.Errorf("Page = %q, want %q", res.Page, wantPage)
	}
	if !pkg.FileExists(wantPage) {
		t.Fatalf("page non générée: %s", wantPage)
	}

	// Component/identifier names replaced across the scaffold.
	indexPath := filepath.Join(moduleDir, "index.tsx")
	assertFileContains(t, indexPath,
		"blogManagerModule",
		"com.example.blog-manager",
		"key: 'BLOG_MANAGER'",
		"name: 'Blog Manager'",
		"uri: '/blog-manager'",
		`description: 'Gestion de blog d\'articles',`,
		`from "./presentation/widgets/blog-manager.widget"`,
	)

	viewPath := filepath.Join(moduleDir, "presentation", "views", "blog-manager.view.tsx")
	assertFileContains(t, viewPath,
		"function BlogManagerView()",
		`from "../widgets/blog-manager.widget"`,
	)

	widgetPath := filepath.Join(moduleDir, "presentation", "widgets", "blog-manager.widget.tsx")
	assertFileContains(t, widgetPath,
		"function BlogManagerWidget()",
		"['blog-manager', 'widget']",
	)

	servicePath := filepath.Join(moduleDir, "application", "service", "blog-manager-api-service.ts")
	assertFileContains(t, servicePath,
		"class BlogManagerApiService",
		"BlogManagerInterface",
		"'/blog-manager/'",
	)

	// Manifest identity: fresh token + new module info.
	manifest, err := LoadManifest(filepath.Join(moduleDir, "manifest.json"))
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if !pkg.IsUUID(manifest.Token) {
		t.Errorf("token non UUID: %q", manifest.Token)
	}
	if manifest.ID != "blog-manager" {
		t.Errorf("id = %q, want blog-manager", manifest.ID)
	}
	if manifest.Domain != "com.example.blog-manager" {
		t.Errorf("domain = %q, want com.example.blog-manager", manifest.Domain)
	}
	if manifest.Name != "Blog Manager" {
		t.Errorf("name = %q, want Blog Manager", manifest.Name)
	}
	if manifest.Description != "Gestion de blog d'articles" {
		t.Errorf("description = %q", manifest.Description)
	}

	assertFileContains(t, filepath.Join(moduleDir, "manifest.json"),
		`"permissions": ["blog-manager.read", "blog-manager.write"]`,
		`"routines": ["blogManagerAnalyticsRoutine"]`,
	)

	// Page content renamed.
	assertFileContains(t, wantPage,
		`import {BlogManagerView} from "@/external_modules/com.example.blog-manager/presentation/views/blog-manager.view";`,
		"function BlogManagerPage()",
		"<BlogManagerView/>",
	)
}

func TestCreateFromMockupPatchesDefaultURI(t *testing.T) {
	mockupDir, pageMockup := writeScaffoldFixture(t)

	// Remove the uri from the declaration.
	indexPath := filepath.Join(mockupDir, "index.tsx")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(data), "    uri: '/hello-world',", "", 1)
	if err := os.WriteFile(indexPath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	creator := &Creator{Root: root, MockupDir: mockupDir, PageMockup: pageMockup}
	res, err := creator.Create(ModuleSpec{Domain: "com.example.blog-manager", ID: "blog-manager"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// The uri is always patched to the effective page url (default: id), so
	// the page is scaffolded at src/app/blog-manager/.
	wantPage := filepath.Join(root, "src", "app", "blog-manager", "page.tsx")
	if res.Page != wantPage {
		t.Errorf("Page = %q, want %q", res.Page, wantPage)
	}
	if !pkg.FileExists(wantPage) {
		t.Fatalf("page non générée: %s", wantPage)
	}
	assertFileContains(t, filepath.Join(root, "external_modules", "com.example.blog-manager", "index.tsx"),
		"uri: '/blog-manager'",
	)
	manifest, err := LoadManifest(filepath.Join(root, "external_modules", "com.example.blog-manager", "manifest.json"))
	if err != nil {
		t.Fatalf("manifest invalide sans description: %v", err)
	}
	if manifest.URI != "/blog-manager" {
		t.Errorf("uri = %q, want /blog-manager", manifest.URI)
	}
	if manifest.Menu.Items[0].URL != "/blog-manager" {
		t.Errorf("menu url = %q, want /blog-manager", manifest.Menu.Items[0].URL)
	}
}

func TestCreateUsesEnvModuleMockup(t *testing.T) {
	mockupDir, pageMockup := writeScaffoldFixture(t)
	t.Setenv(EnvModuleMockup, mockupDir)
	t.Setenv(EnvPageMockup, pageMockup)

	root := t.TempDir()
	creator := &Creator{Root: root}
	res, err := creator.Create(ModuleSpec{Domain: "com.example.blog-manager", ID: "blog-manager"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// The custom mockup (not the embedded one) must have been scaffolded.
	moduleDir := filepath.Join(root, "external_modules", "com.example.blog-manager")
	assertFileContains(t, filepath.Join(moduleDir, "index.tsx"),
		"blogManagerModule",
		"com.example.blog-manager",
		"key: 'BLOG_MANAGER'",
		"uri: '/blog-manager'",
	)
	if res.Page == "" {
		t.Fatal("une page doit être scaffoldée depuis le mockup de page personnalisé")
	}
	assertFileContains(t, res.Page,
		"function BlogManagerPage()",
	)

	// An invalid custom mockup path falls back to the embedded templates.
	t.Setenv(EnvModuleMockup, filepath.Join(t.TempDir(), "absent"))
	root2 := t.TempDir()
	if _, err := (&Creator{Root: root2}).Create(ModuleSpec{Domain: "com.example.blog-manager", ID: "blog-manager"}); err != nil {
		t.Fatalf("Create avec mockup env invalide: %v", err)
	}
	moduleDir2 := filepath.Join(root2, "external_modules", "com.example.blog-manager")
	if !pkg.FileExists(filepath.Join(moduleDir2, "package.json")) {
		t.Error("le fallback doit utiliser le mockup embarqué")
	}
}

func TestCreateFromEmbeddedMockup(t *testing.T) {
	root := t.TempDir()
	creator := &Creator{Root: root}

	res, err := creator.Create(ModuleSpec{Domain: "com.example.blog-manager", ID: "blog-manager", Description: "Gestion de blog et d'articles"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !pkg.IsUUID(res.Token) {
		t.Errorf("token non UUID: %q", res.Token)
	}

	moduleDir := filepath.Join(root, "external_modules", "com.example.blog-manager")
	for _, rel := range []string{
		"manifest.json",
		"index.tsx",
		"package.json",
		"README.md",
		"application/service/blog-manager-api-service.ts",
		"domain/blog-manager.interface.ts",
		"domain/enums/blog-manager-status.enum.ts",
		"infrastructure/routines/blog-manager-analytics.routine.ts",
		"presentation/views/blog-manager.view.tsx",
		"presentation/widgets/blog-manager.widget.tsx",
		"presentation/components/create-blog-manager-dialog.tsx",
		"presentation/providers/blog-manager-header.provider.tsx",
	} {
		if !pkg.FileExists(filepath.Join(moduleDir, filepath.FromSlash(rel))) {
			t.Errorf("fichier embarqué manquant: %s", rel)
		}
	}

	assertFileContains(t, filepath.Join(moduleDir, "index.tsx"),
		"blogManagerModule",
		"com.example.blog-manager",
		"key: 'BLOG_MANAGER'",
		`description: 'Gestion de blog et d\'articles',`,
		"uri: '/blog-manager'",
	)
	assertFileContains(t, filepath.Join(moduleDir, "manifest.json"),
		`"routines": ["blogManagerAnalyticsRoutine"]`,
		`"providers": ["layout"]`,
	)

	// The declaration declares a uri → the embedded page is scaffolded.
	wantPage := filepath.Join(root, "src", "app", "blog-manager", "page.tsx")
	if res.Page != wantPage {
		t.Errorf("Page = %q, want %q", res.Page, wantPage)
	}
	assertFileContains(t, wantPage,
		`import {BlogManagerView} from "@/external_modules/com.example.blog-manager/presentation/views/blog-manager.view";`,
		"function BlogManagerPage()",
	)
}

func TestCreateFromEmbeddedMockupManifestIsCanonical(t *testing.T) {
	root := t.TempDir()
	creator := &Creator{Root: root}
	if _, err := creator.Create(ModuleSpec{Domain: "com.example.blog-manager", ID: "blog-manager"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	moduleDir := filepath.Join(root, "external_modules", "com.example.blog-manager")
	manifestPath := filepath.Join(moduleDir, "manifest.json")
	assertFileContains(t, manifestPath,
		`"$schema"`,
		`"optionalRequirements": {}`,
		`"type": "EXTERNAL"`,
		`"category": "SYSTEM"`,
	)
	rawManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawManifest), "\n  \"url\"") {
		t.Errorf("le manifeste ne doit pas porter de champ `url` racine non canonique:\n%s", rawManifest)
	}

	// The manifest stays the single source of truth: the declaration must not
	// carry requirements/dependencies/devDependencies.
	indexPath := filepath.Join(moduleDir, "index.tsx")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"requirements:", "optionalRequirements:", "dependencies:", "devDependencies:"} {
		if strings.Contains(string(data), forbidden) {
			t.Errorf("index.tsx ne doit pas déclarer %q:\n%s", forbidden, data)
		}
	}
	assertFileContains(t, indexPath, "type: 'EXTERNAL'", "category: 'SYSTEM'")
}

func TestCreateRespectsTypeAndCategoryFlags(t *testing.T) {
	root := t.TempDir()
	creator := &Creator{Root: root}
	if _, err := creator.Create(ModuleSpec{
		Domain:   "com.example.blog-manager",
		ID:       "blog-manager",
		Type:     "INTERNAL",
		Category: "DATA",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	moduleDir := filepath.Join(root, "external_modules", "com.example.blog-manager")
	assertFileContains(t, filepath.Join(moduleDir, "manifest.json"),
		`"type": "INTERNAL"`,
		`"category": "DATA"`,
	)
	assertFileContains(t, filepath.Join(moduleDir, "index.tsx"),
		"type: 'INTERNAL'",
		"category: 'DATA'",
	)

	if _, err := (&Creator{Root: t.TempDir()}).Create(ModuleSpec{
		Domain: "com.example.other", ID: "other", Category: "NOPE",
	}); err == nil {
		t.Error("une catégorie invalide doit être refusée")
	}
	if _, err := (&Creator{Root: t.TempDir()}).Create(ModuleSpec{
		Domain: "com.example.other", ID: "other", Type: "NOPE",
	}); err == nil {
		t.Error("un type invalide doit être refusé")
	}
}

func TestCreateFromMockupEmptyDescriptionStaysEmpty(t *testing.T) {
	mockupDir, _ := writeScaffoldFixture(t)
	root := t.TempDir()
	creator := &Creator{Root: root, MockupDir: mockupDir}

	if _, err := creator.Create(ModuleSpec{Domain: "com.example.blog-manager", ID: "blog-manager"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	manifest, err := LoadManifest(filepath.Join(root, "external_modules", "com.example.blog-manager", "manifest.json"))
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if manifest.Description != "" {
		t.Errorf("description = %q, attendu vide (pour merge par un link)", manifest.Description)
	}
}

func assertFileContains(t *testing.T, path string, fragments ...string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lecture %s: %v", path, err)
	}
	content := string(data)
	for _, frag := range fragments {
		if !strings.Contains(content, frag) {
			t.Errorf("%s doit contenir %q\n---\n%s", path, frag, content)
		}
	}
}
