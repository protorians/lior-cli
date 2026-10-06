package module

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/pkg"
)

// importSpecifiersRE extracts the module specifiers of a source file — the
// `from "…"` clauses, kept on a single line so Go does not insert a `;` in the
// middle of a concatenated literal.
var importSpecifiersRE = regexp.MustCompile(`from "([^"]+)"`)

// writeScaffoldFixture builds a minimal hello-world-style module mockup plus a
// page mockup, mirroring the reference mockups used by `liora create module`.
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

	mustWrite(pageMockup, `import {HelloWorldView} from "@/library/modules/hello-world/presentation/views/hello-world.view";

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

	moduleDir := filepath.Join(root, config.ExternalModulesDir, "com.example.blog-manager")

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
		`import {BlogManagerView} from "@/library/modules/com.example.blog-manager/presentation/views/blog-manager.view";`,
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
	assertFileContains(t, filepath.Join(root, config.ExternalModulesDir, "com.example.blog-manager", "index.tsx"),
		"uri: '/blog-manager'",
	)
	manifest, err := LoadManifest(filepath.Join(root, config.ExternalModulesDir, "com.example.blog-manager", "manifest.json"))
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
	moduleDir := filepath.Join(root, config.ExternalModulesDir, "com.example.blog-manager")
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
	moduleDir2 := filepath.Join(root2, config.ExternalModulesDir, "com.example.blog-manager")
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

	moduleDir := filepath.Join(root, config.ExternalModulesDir, "com.example.blog-manager")
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
		`import {BlogManagerView} from "@/library/modules/com.example.blog-manager/presentation/views/blog-manager.view";`,
		"function BlogManagerPage()",
	)
}

// Un module créé sans `tsconfig.json` échoue à la règle D6-2 dès le premier
// pack en arbre isolé (`Validator` : `tsconfig.json present`, `LevelError`), et
// le typecheck du packer n'a rien à comprendre. Le scaffold doit donc embarquer
// un `tsconfig.json` autonome — sans `extends` : un module créé chez un
// développeur n'hérite d'aucun `tsconfig.base.json` de monorepo.
func TestCreateFromEmbeddedMockupShipsAConformingTsConfig(t *testing.T) {
	root := t.TempDir()
	creator := &Creator{Root: root}
	if _, err := creator.Create(ModuleSpec{Domain: "com.example.blog-manager", ID: "blog-manager"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	moduleDir := filepath.Join(root, config.ExternalModulesDir, "com.example.blog-manager")
	raw, err := os.ReadFile(filepath.Join(moduleDir, "tsconfig.json"))
	if err != nil {
		t.Fatalf("le scaffold doit embarquer un tsconfig.json (D6-2): %v", err)
	}
	var tsconfig struct {
		CompilerOptions map[string]any `json:"compilerOptions"`
		Extends         string         `json:"extends"`
		Include         []string       `json:"include"`
	}
	// strictement JSON : `tsc` accepte les commentaires, pas tous les lecteurs
	// de tsconfig de la chaîne (outillage d'archive, éditeurs, IDE).
	if err := json.Unmarshal(raw, &tsconfig); err != nil {
		t.Fatalf("tsconfig.json illisible: %v\n%s", err, raw)
	}
	if tsconfig.Extends != "" {
		t.Errorf("tsconfig.json ne doit pas hériter d'une base (%q) : un module créé hors monorepo n'en a pas", tsconfig.Extends)
	}
	// Contrat interne du SDK : le module consomme les sources de `@liorian/sdk`,
	// compilées avec ces options relâchées. Les hériter ici ferait échouer le
	// typecheck sur le code du SDK, pas sur celui du module.
	for option, want := range map[string]any{
		"verbatimModuleSyntax":     false,
		"noUncheckedIndexedAccess": false,
		"noImplicitOverride":       false,
		"moduleResolution":         "bundler",
		"jsx":                      "react-jsx",
		"noEmit":                   true,
		"strict":                   true,
		"types":                    []any{"node"},
	} {
		if got, ok := tsconfig.CompilerOptions[option]; !ok {
			t.Errorf("tsconfig.json sans l'option %q", option)
		} else if !reflect.DeepEqual(got, want) {
			t.Errorf("tsconfig.json %q = %v, want %v", option, got, want)
		}
	}
	if len(tsconfig.Include) == 0 {
		t.Error("tsconfig.json sans `include`: le typecheck ne couvre aucun fichier")
	}
	// Les sous-chemins du SDK ne se résolvent pas par le `exports` du package
	// (le glob `./infrastructure/*` cible un chemin sans extension que `tsc`
	// ne complète pas) : sans ce `paths` vers les sources, chaque import
	// `@liorian/sdk/...` du module échoue en TS2307 au premier pack.
	paths, _ := tsconfig.CompilerOptions["paths"].(map[string]any)
	if got, ok := paths["@liorian/sdk/*"]; !ok {
		t.Errorf("tsconfig.json sans le mapping %q — le typecheck échouerait sur chaque import du SDK", "@liorian/sdk/*")
	} else if !reflect.DeepEqual(got, []any{"../../packages/sdk/src/*"}) {
		t.Errorf("tsconfig.json paths[%q] = %v, want [\"../../packages/sdk/src/*\"]", "@liorian/sdk/*", got)
	}
}

// Le mockup embarqué ne doit importer que ce qu'un module peut résoudre seul.
// `@/core/...` est l'alias interne du socle (`apps/liorian-socle/tsconfig.json`) :
// un module créé n'a pas de `src/core`, et son `tsconfig.json` ne déclare pas
// cet alias — le typecheck échouait sur chaque vue scaffoldee. Les autres
// spécificateurs sont des paquets déclarés par le `package.json` du mockup.
// Chaque mockup de type (spec `module-types` §2) est parcouru.
func TestEmbeddedMockupImportsOnlyResolvableSpecifiers(t *testing.T) {
	prefixes := make([]string, 0, len(embeddedTypeMockups))
	seen := map[string]bool{}
	for _, prefix := range embeddedTypeMockups {
		if !seen[prefix] {
			seen[prefix] = true
			prefixes = append(prefixes, prefix)
		}
	}
	sort.Strings(prefixes)
	for _, prefix := range prefixes {
		err := fs.WalkDir(embeddedTemplates, prefix, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if !isTextFile(path) {
				return nil
			}
			data, err := embeddedTemplates.ReadFile(path)
			if err != nil {
				return err
			}
			for _, match := range importSpecifiersRE.FindAllStringSubmatch(string(data), -1) {
				if spec := match[1]; strings.HasPrefix(spec, "@/") {
					t.Errorf("%s importe %q : alias interne au socle, hors de portée d'un module", path, spec)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("parcours du mockup embarqué %s: %v", prefix, err)
		}
	}
}

// Chaque mockup embarqué porte son identifiant d'exemple dans le manifeste :
// le renommage lit cet `id` pour cartographier ses épellations (kebab,
// Pascal, camel, UPPER_SNAKE…) sur le nouveau module — n'importe quel mockup
// peut donc être écrit avec n'importe quel nom d'exemple. Chaque manifeste
// doit aussi déclarer le type auquel son mockup est dédié.
func TestEmbeddedTypeMockupsCarryTheirSampleIDAndType(t *testing.T) {
	for moduleType, prefix := range embeddedTypeMockups {
		raw, err := embeddedTemplates.ReadFile(prefix + "/" + config.ManifestFileName)
		if err != nil {
			t.Fatalf("mockup %s sans manifest.json: %v", prefix, err)
		}
		var manifest struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			t.Fatalf("manifest.json de %s illisible: %v", prefix, err)
		}
		if err := ValidateName(manifest.ID); err != nil {
			t.Errorf("mockup %s : identifiant d'exemple invalide %q: %v", prefix, manifest.ID, err)
		}
		if manifest.Type != moduleType {
			t.Errorf("mockup %s : type %q, want %q", prefix, manifest.Type, moduleType)
		}
		// Le contrat de scaffold exige une déclaration et un tsconfig
		// autonomes (IsModuleMockup, D6-2).
		for _, file := range []string{config.LegacyDeclarationFileName, "tsconfig.json", "package.json"} {
			if _, err := embeddedTemplates.ReadFile(prefix + "/" + file); err != nil {
				t.Errorf("mockup %s sans %s: %v", prefix, file, err)
			}
		}
	}
}

// La façade d'API d'un module est statique (`ApiService` est abstraite à
// instance) : un scaffold qui l'étendait ne compilait pas
// (« does not implement inherited abstract member assertAllowed »). Chaque
// service d'API embarqué dans les mockups de types est vérifié.
func TestEmbeddedMockupApiServiceUsesTheStaticModuleFacade(t *testing.T) {
	for _, prefix := range embeddedTypeMockups {
		err := fs.WalkDir(embeddedTemplates, prefix+"/application", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if !strings.HasSuffix(path, ".ts") {
				return nil
			}
			data, err := embeddedTemplates.ReadFile(path)
			if err != nil {
				return err
			}
			body := string(data)
			if !strings.Contains(body, "class ") {
				return nil
			}
			if !strings.Contains(body, `from "@liorian/sdk/infrastructure/module-runtime/module-api.service"`) {
				t.Errorf("le service du mockup %s (%s) doit étendre `ModuleApiService`:\n%s", prefix, path, body)
			}
			if strings.Contains(body, "extends ApiService {") {
				t.Errorf("le service du mockup %s (%s) ne doit pas étendre `ApiService` (abstraite à instance)", prefix, path)
			}
			return nil
		})
		// Le mockup n'embarque pas de couche application : rien à vérifier.
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("parcours de %s/application: %v", prefix, err)
		}
	}
}

// `ModuleDeclarationInterface` exige `external` : sans lui, le module créé ne
// compile pas et la régression n'apparaît qu'au premier pack.
func TestEmbeddedMockupDeclarationDeclaresExternal(t *testing.T) {
	source, err := embeddedTemplates.ReadFile(defaultEmbeddedMockup + "/" + config.LegacyDeclarationFileName)
	if err != nil {
		t.Fatalf("déclaration du mockup introuvable: %v", err)
	}
	if !strings.Contains(string(source), "external: false") {
		t.Errorf("la déclaration du mockup doit porter `external: false` (champ requis par ModuleDeclarationInterface):\n%s", source)
	}
}

// Le projet peut développer ses propres modules dans `<root>/modules/`
// (arbre source D5, couvert par les globs de workspace). Y créer le module
// sous son id : c'est l'arbre que `config.ResolveModuleDir` privilégie, et le
// seul où le `package.json` du module est effectivement installé — donc le seul
// où le typecheck D6-2 peut s'exécuter.
func TestCreateTargetsTheWorkspaceSourceTreeWhenItExists(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, config.WorkspaceModulesDir), 0o755); err != nil {
		t.Fatal(err)
	}
	creator := &Creator{Root: root}
	if _, err := creator.Create(ModuleSpec{Domain: "com.example.blog-manager", ID: "blog-manager"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	moduleDir := filepath.Join(root, config.WorkspaceModulesDir, "blog-manager")
	if !pkg.DirExists(moduleDir) {
		t.Fatalf("module non créé dans l'arbre source: %s", moduleDir)
	}
	if pkg.DirExists(filepath.Join(root, config.ExternalModulesDir, "com.example.blog-manager")) {
		t.Errorf("le module ne doit pas être créé dans l'arbre d'installation %q", config.ExternalModulesDir)
	}
	// Résolution côté packer : par id et par domaine, pour rester adressable
	// quelle que soit la forme employée ensuite.
	if got := config.ResolveModuleDir(root, "blog-manager"); got != moduleDir {
		t.Errorf("ResolveModuleDir(id) = %q, want %q", got, moduleDir)
	}
	if got := config.ResolveModuleDir(root, "com.example.blog-manager"); got != moduleDir {
		t.Errorf("ResolveModuleDir(domain) = %q, want %q", got, moduleDir)
	}
}

func TestCreateFromEmbeddedMockupManifestIsCanonical(t *testing.T) {
	root := t.TempDir()
	creator := &Creator{Root: root}
	if _, err := creator.Create(ModuleSpec{Domain: "com.example.blog-manager", ID: "blog-manager"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	moduleDir := filepath.Join(root, config.ExternalModulesDir, "com.example.blog-manager")
	manifestPath := filepath.Join(moduleDir, "manifest.json")
	assertFileContains(t, manifestPath,
		`"$schema"`,
		`"optionalRequirements": {}`,
		`"type": "WEB_APP_LOCAL"`,
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
	assertFileContains(t, indexPath, "type: 'WEB_APP_LOCAL'", "category: 'SYSTEM'")
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

	moduleDir := filepath.Join(root, config.ExternalModulesDir, "com.example.blog-manager")
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

	manifest, err := LoadManifest(filepath.Join(root, config.ExternalModulesDir, "com.example.blog-manager", "manifest.json"))
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if manifest.Description != "" {
		t.Errorf("description = %q, attendu vide (pour merge par un link)", manifest.Description)
	}
}

// Chaque type canonique scaffolde le mockup qui porte sa surface d'injection
// (spec `module-types` §2) : fichiers signatures, déclaration et manifeste.
// Les types sans surface applicative ne reçoivent aucune page socle.
func TestCreateScaffoldsTheMockupOfTheType(t *testing.T) {
	cases := []struct {
		moduleType  string
		files       []string // fichiers renommés attendus dans le module
		declaration []string // fragments attendus dans index.tsx
		manifest    []string // fragments attendus dans manifest.json
		page        bool     // page socle scaffoldée ?
	}{
		{
			moduleType: "WEB_APP_LOCAL",
			files: []string{
				"application/service/blog-manager-api-service.ts",
				"presentation/views/blog-manager.view.tsx",
				"presentation/widgets/blog-manager.widget.tsx",
				"presentation/providers/blog-manager-header.provider.tsx",
			},
			declaration: []string{"type: 'WEB_APP_LOCAL'", "blogManagerModule"},
			manifest:    []string{`"type": "WEB_APP_LOCAL"`},
			page:        true,
		},
		{
			moduleType: "CONFIGURATION",
			files: []string{
				"settings.tsx",
				"presentation/settings/blog-manager-form.tsx",
				"application/service/blog-manager-api-service.ts",
			},
			declaration: []string{"type: 'CONFIGURATION'", "blogManagerModule"},
			manifest: []string{
				`"type": "CONFIGURATION"`,
				`"declarative"`,
				`"dataModel"`,
				`"configSettings"`,
			},
		},
		{
			moduleType: "SERVICE",
			files: []string{
				"routines.tsx",
				"application/service/blog-manager-api-service.ts",
				"domain/blog-manager.interface.ts",
			},
			declaration: []string{"type: 'SERVICE'"},
			manifest:    []string{`"type": "SERVICE"`},
		},
		{
			moduleType: "WIDGET",
			files: []string{
				"presentation/widgets/blog-manager-kpi.widget.tsx",
				"presentation/widgets/blog-manager-activity.widget.tsx",
			},
			declaration: []string{"type: 'WIDGET'", "'blog-manager.kpi'", "'blog-manager.activity'"},
			manifest: []string{
				`"type": "WIDGET"`,
				`"widgets": ["blog-manager.kpi", "blog-manager.activity"]`,
			},
		},
		{
			moduleType:  "THEME",
			files:       []string{"styles/blog-manager-ocean.css"},
			declaration: []string{"type: 'THEME'"},
			manifest: []string{
				`"type": "THEME"`,
				`"themes": [`,
				`"dataTheme": "blog-manager-ocean"`,
				`"tokens"`,
				`"dark"`,
			},
		},
		{
			moduleType:  "WEB_APP_REMOTE",
			declaration: []string{"type: 'WEB_APP_REMOTE'"},
			manifest: []string{
				`"type": "WEB_APP_REMOTE"`,
				`"remote": {`,
				`"origin": "https://app.acme.example"`,
				`"wellKnown"`,
			},
		},
		{
			moduleType: "SYSTEM",
			files: []string{
				"presentation/views/blog-manager.view.tsx",
				"application/service/blog-manager-api-service.ts",
			},
			declaration: []string{"type: 'SYSTEM'", "blogManagerModule"},
			manifest: []string{
				`"type": "SYSTEM"`,
				`"admin": {`,
				`"roles": ["Root", "Admin"]`,
				`"supported": false`,
			},
			page: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.moduleType, func(t *testing.T) {
			root := t.TempDir()
			creator := &Creator{Root: root}
			res, err := creator.Create(ModuleSpec{
				Domain: CanonicalDomainPrefix(tc.moduleType) + ".example.blog-manager",
				ID:     "blog-manager",
				Type:   tc.moduleType,
			})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}

			moduleDir := filepath.Join(root, config.ExternalModulesDir, CanonicalDomainPrefix(tc.moduleType)+".example.blog-manager")
			for _, rel := range tc.files {
				if !pkg.FileExists(filepath.Join(moduleDir, filepath.FromSlash(rel))) {
					t.Errorf("fichier du mockup %s manquant: %s", tc.moduleType, rel)
				}
			}
			assertFileContains(t, filepath.Join(moduleDir, "index.tsx"), tc.declaration...)
			assertFileContains(t, filepath.Join(moduleDir, "manifest.json"), tc.manifest...)

			if tc.page && res.Page == "" {
				t.Errorf("type %s : une page socle doit être scaffoldée", tc.moduleType)
			}
			if !tc.page && res.Page != "" {
				t.Errorf("type %s : aucune page socle ne doit être scaffoldée (reçu %s)", tc.moduleType, res.Page)
			}
		})
	}
}

// Le mockup dédié au type est choisi par le type effectif : les alias legacy
// non canoniques retombent sur le mockup de référence hello-world
// (WEB_APP_LOCAL), et EmbeddedMockupName expose le mockup sélectionné.
func TestEmbeddedMockupSelectionFollowsTheType(t *testing.T) {
	if got := EmbeddedMockupName("WEB_APP_LOCAL"); got != "hello-world" {
		t.Errorf("EmbeddedMockupName(WEB_APP_LOCAL) = %q, want hello-world", got)
	}
	if got := EmbeddedMockupName("SERVICE"); got != "service" {
		t.Errorf("EmbeddedMockupName(SERVICE) = %q, want service", got)
	}
	// Alias legacy : repli sur le mockup de référence.
	if got := EmbeddedMockupName("REMOTE_FRONTEND"); got != "hello-world" {
		t.Errorf("EmbeddedMockupName(REMOTE_FRONTEND) = %q, want hello-world", got)
	}
	if got := EmbeddedMockupName(""); got != "hello-world" {
		t.Errorf("EmbeddedMockupName(\"\") = %q, want hello-world", got)
	}

	// Le type effectif décide, pas la valeur brute : un type vide scaffoldé
	// porte le type par défaut WEB_APP_LOCAL.
	root := t.TempDir()
	if _, err := (&Creator{Root: root}).Create(ModuleSpec{Domain: "mod.example.blog-manager", ID: "blog-manager"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	moduleDir := filepath.Join(root, config.ExternalModulesDir, "mod.example.blog-manager")
	if !pkg.FileExists(filepath.Join(moduleDir, "presentation", "views", "blog-manager.view.tsx")) {
		t.Error("le mockup hello-world doit être scaffoldé quand le type est vide")
	}
}

// ModuleTypeSupportsPage restreint la page socle aux types qui possèdent une
// surface applicative : les types d'injection (CONFIGURATION, SERVICE,
// WIDGET, THEME) et WEB_APP_REMOTE — hébergé sur l'origine distante — n'en ont
// pas.
func TestModuleTypeSupportsPage(t *testing.T) {
	for _, withPage := range []string{"WEB_APP_LOCAL", "SYSTEM", "WEB_APP_CACHED", "INTERNAL", ""} {
		if !ModuleTypeSupportsPage(withPage) {
			t.Errorf("ModuleTypeSupportsPage(%q) = false, want true", withPage)
		}
	}
	for _, withoutPage := range []string{"CONFIGURATION", "SERVICE", "WIDGET", "THEME", "WEB_APP_REMOTE"} {
		if ModuleTypeSupportsPage(withoutPage) {
			t.Errorf("ModuleTypeSupportsPage(%q) = true, want false", withoutPage)
		}
	}
}

// Le renommage lit l'identifiant d'exemple du mockup (`id` du manifeste) et
// cartographie toutes ses épellations sur le nouveau module : un mockup
// personnalisé peut donc être écrit avec n'importe quel nom d'exemple, pas
// seulement hello-world.
func TestCreateRenamesAnyMockupSampleID(t *testing.T) {
	base := t.TempDir()
	mockupDir := filepath.Join(base, "mockup")
	pageMockup := filepath.Join(base, "page.tsx")

	mustWrite := func(rel, data string) {
		t.Helper()
		p := filepath.Join(mockupDir, filepath.FromSlash(rel))
		if err := pkg.WriteString(p, data); err != nil {
			t.Fatalf("WriteString(%s): %v", p, err)
		}
	}
	mustWrite("manifest.json", `{
  "schemaVersion": 1,
  "id": "acme-notes",
  "domain": "mod.liorian.acme-notes",
  "key": "ACME_NOTES",
  "name": "Acme Notes",
  "version": "1.0.0",
  "entry": "index.tsx",
  "uri": "/acme-notes"
}
`)
	mustWrite("index.tsx", `import {ModuleDeclarationInterface} from "@liorian/sdk/domain/entities/module.interface";
import {AcmeNotesWidget} from "./presentation/widgets/acme-notes.widget";

const acmeNotesModule: ModuleDeclarationInterface = {
    identifier: 'mod.liorian.acme-notes',
    key: 'ACME_NOTES',
    name: 'Acme Notes',
    description: '',
    uri: '/acme-notes',
    widgets: {
        notes: AcmeNotesWidget
    },
}

export default acmeNotesModule
`)
	mustWrite("presentation/widgets/acme-notes.widget.tsx", `"use client"
export function AcmeNotesWidget() {
    const key = ['acme-notes', 'widget'];
    return null;
}
`)
	mustWrite("application/service/acme-notes-api-service.ts", `import {AcmeNotesInterface} from "../../domain/acme-notes.interface";

export class AcmeNotesApiService {
    static async getAll() {
        return await this.get('/acme-notes/');
    }
}
`)
	mustWrite("domain/acme-notes.interface.ts", `export interface AcmeNotesInterface {
    id: string;
}
`)
	if err := pkg.WriteString(pageMockup, `import {AcmeNotesView} from "@/library/modules/acme-notes/presentation/views/acme-notes.view";

export default function AcmeNotesPage() {
    return <AcmeNotesView/>;
}
`); err != nil {
		t.Fatalf("WriteString(page): %v", err)
	}

	root := t.TempDir()
	creator := &Creator{Root: root, MockupDir: mockupDir, PageMockup: pageMockup}
	if _, err := creator.Create(ModuleSpec{Domain: "com.example.blog-manager", ID: "blog-manager"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	moduleDir := filepath.Join(root, config.ExternalModulesDir, "com.example.blog-manager")
	for _, rel := range []string{
		"presentation/widgets/blog-manager.widget.tsx",
		"application/service/blog-manager-api-service.ts",
		"domain/blog-manager.interface.ts",
	} {
		if !pkg.FileExists(filepath.Join(moduleDir, filepath.FromSlash(rel))) {
			t.Errorf("fichier renommé manquant: %s", rel)
		}
	}
	assertFileContains(t, filepath.Join(moduleDir, "index.tsx"),
		"blogManagerModule",
		"identifier: 'com.example.blog-manager'",
		"key: 'BLOG_MANAGER'",
		"name: 'Blog Manager'",
		`from "./presentation/widgets/blog-manager.widget"`,
	)
	assertFileContains(t, filepath.Join(moduleDir, "manifest.json"),
		`"key": "BLOG_MANAGER"`,
	)
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
