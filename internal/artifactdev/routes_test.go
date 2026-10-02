package artifactdev

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRouteModule écrit un module avec un arbre de routes complet : racine,
// statique (métadonnées menu), dynamique, 404.
func writeRouteModule(t *testing.T, manifestExtra string) string {
	t.Helper()
	moduleDir := writeModule(t)
	routesDir := filepath.Join(moduleDir, "presentation", "routes")
	for path, content := range map[string]string{
		"index.tsx": "export default function Root() { return null; }\n",
		"repertoire.tsx": "export default function Repertoire() { return null; }\n" +
			"export const route = {title: \"Répertoire\", icon: \"BookUserIcon\"};\n",
		"customers/[id].tsx": "export default function Customer() { return null; }\n",
		"not-found.tsx":      "export default function NotFound() { return null; }\n",
	} {
		full := filepath.Join(routesDir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifest := `{"id":"crm","name":"CRM","version":"1.0.0","entry":"main.tsx","uri":"/m/crm"` + manifestExtra + `}`
	if err := os.WriteFile(filepath.Join(moduleDir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return moduleDir
}

func TestBuildGeneratesRouteRegistryAndWrapperRouter(t *testing.T) {
	moduleDir := writeRouteModule(t, "")
	result, err := Build(BuildOptions{ModuleDir: moduleDir}, func(string) {})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	registry, err := os.ReadFile(filepath.Join(moduleDir, RoutesRegistryName))
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	content := string(registry)
	// Ordre de matching : racine, statique avant dynamique, `*` en dernier.
	// Les chemins d'import remontent depuis `.liorian/`.
	for _, expected := range []string{
		`import * as Route0 from "../presentation/routes/index"`,
		`{path: "", params: [], component: Route0.default`,
		`{path: "repertoire", params: []`,
		`{path: "customers/:id", params: ["id"]`,
		`{path: "*", params: []`,
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("registry missing %q:\n%s", expected, content)
		}
	}
	// Le fichier 404 est bien le dernier de la table.
	if strings.LastIndex(content, `"*"`) < strings.LastIndex(content, `"repertoire"`) {
		t.Fatalf("wildcard route not last:\n%s", content)
	}

	// Le wrapper one-shot est retiré après build (§7.7) : le contrat
	// wrapper ↔ registre se vérifie en le régénérant à côté du registre.
	_, cfg, err := Resolve(BuildOptions{ModuleDir: moduleDir})
	if err != nil {
		t.Fatal(err)
	}
	wrapperPath, err := WriteBootstrapWrapper(cfg)
	if err != nil {
		t.Fatal(err)
	}
	wrapper, err := os.ReadFile(wrapperPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(wrapper), `bootstrapModule(definition as any, {router: routes});`) {
		t.Fatalf("wrapper missing router option:\n%s", wrapper)
	}

	// Le bundle est produit : le registre importe de vrais fichiers de route.
	if _, err := os.Stat(result.BundlePath); err != nil {
		t.Fatalf("bundle: %v", err)
	}

	// Sans `router.mode: auto`, le manifeste n'est pas touché.
	raw, _ := os.ReadFile(filepath.Join(moduleDir, "manifest.json"))
	if strings.Contains(string(raw), "routes") {
		t.Fatalf("manifest synced without router.auto: %s", raw)
	}
}

func TestBuildWithoutRoutesDirectoryKeepsManualRouting(t *testing.T) {
	moduleDir := writeModule(t)
	if _, err := Build(BuildOptions{ModuleDir: moduleDir}, func(string) {}); err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := os.Stat(filepath.Join(moduleDir, RoutesRegistryName)); !os.IsNotExist(err) {
		t.Fatalf("registry generated without routes dir: %v", err)
	}
	_, cfg, err := Resolve(BuildOptions{ModuleDir: moduleDir})
	if err != nil {
		t.Fatal(err)
	}
	wrapperPath, err := WriteBootstrapWrapper(cfg)
	if err != nil {
		t.Fatal(err)
	}
	wrapper, _ := os.ReadFile(wrapperPath)
	if strings.Contains(string(wrapper), "routes") {
		t.Fatalf("wrapper references routes without registry:\n%s", wrapper)
	}
}

func TestBuildRouterAutoSyncsManifestMenu(t *testing.T) {
	moduleDir := writeRouteModule(t, `,"router":{"mode":"auto"}`)
	if _, err := Build(BuildOptions{ModuleDir: moduleDir}, func(string) {}); err != nil {
		t.Fatalf("build: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(moduleDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	routes, _ := manifest["routes"].([]any)
	if len(routes) != 3 { // racine, dynamique, statique — le `*` est exclu
		t.Fatalf("routes = %v", routes)
	}
	menu, _ := manifest["menu"].(map[string]any)
	items, _ := menu["items"].([]any)
	// Répertoire uniquement : la racine n'a pas de titre, la route dynamique
	// sans titre non plus — rien à dériver.
	if len(items) != 1 {
		t.Fatalf("menu items = %v", items)
	}
	first, _ := items[0].(map[string]any)
	if first["url"] != "/m/crm/repertoire" || first["label"] != "Répertoire" || first["icon"] != "BookUserIcon" {
		t.Fatalf("first item = %v", first)
	}

	// Idempotence : un second build ne réécrit pas le manifeste (mtime).
	infoBefore, _ := os.Stat(filepath.Join(moduleDir, "manifest.json"))
	if _, err := Build(BuildOptions{ModuleDir: moduleDir}, func(string) {}); err != nil {
		t.Fatalf("second build: %v", err)
	}
	infoAfter, _ := os.Stat(filepath.Join(moduleDir, "manifest.json"))
	if !infoBefore.ModTime().Equal(infoAfter.ModTime()) {
		t.Fatal("manifest rewritten by idempotent sync")
	}
}

func TestRouterAutoManualMenuEntriesWin(t *testing.T) {
	// Une entrée manuelle sur /m/crm/repertoire écrase le libellé dérivé.
	moduleDir := writeRouteModule(t, `,"router":{"mode":"auto","menu":{"items":[{"label":"Annuaire","icon":"XIcon","url":"/m/crm/repertoire"}]}}`)
	// (le manifeste de writeRouteModule ne contient pas de menu : on le
	// réécrit avec le menu manuel avant le build.)
	manifest := `{"id":"crm","name":"CRM","version":"1.0.0","entry":"main.tsx","uri":"/m/crm","router":{"mode":"auto"},` +
		`"menu":{"items":[{"label":"Annuaire","icon":"XIcon","url":"/m/crm/repertoire"}]}}`
	if err := os.WriteFile(filepath.Join(moduleDir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(BuildOptions{ModuleDir: moduleDir}, func(string) {}); err != nil {
		t.Fatalf("build: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(moduleDir, "manifest.json"))
	var updated map[string]any
	if err := json.Unmarshal(raw, &updated); err != nil {
		t.Fatal(err)
	}
	menu, _ := updated["menu"].(map[string]any)
	items, _ := menu["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("menu items = %v", items)
	}
	entry, _ := items[0].(map[string]any)
	if entry["label"] != "Annuaire" {
		t.Fatalf("manual menu entry lost: %v", entry)
	}
}

func TestScanRouteFilesOrdersStaticBeforeDynamic(t *testing.T) {
	moduleDir := writeRouteModule(t, `,"router":{"mode":"auto"}`)
	_, cfg, err := Resolve(BuildOptions{ModuleDir: moduleDir})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RoutesDir == "" {
		t.Fatal("routes dir not detected")
	}
	routes, err := ScanRouteFiles(cfg, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	patterns := []string{}
	for _, route := range routes {
		patterns = append(patterns, route.Pattern)
	}
	want := []string{"", "repertoire", "customers/:id", "*"}
	if strings.Join(patterns, "|") != strings.Join(want, "|") {
		t.Fatalf("patterns = %v, want %v", patterns, want)
	}
}
