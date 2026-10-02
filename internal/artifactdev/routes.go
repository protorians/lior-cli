// Router fichier d'un module (spec module-isolated-runtime §8.5) : un
// développeur ne devrait pas écrire une table de routes à la main pour
// obtenir ce qu'un arbre de fichiers décrit déjà. La convention :
//
//	presentation/routes/index.tsx            → racine du module
//	presentation/routes/repertoire.tsx       → /repertoire
//	presentation/routes/customers/[id].tsx   → /customers/:id
//	presentation/routes/parametres/index.tsx → /parametres
//	presentation/routes/not-found.tsx        → vue 404 (route `*`)
//
// Chaque fichier exporte sa vue par défaut et, s'il le souhaite,
// `export const route = {title, icon, order, menu}` — les métadonnées
// alimentent le menu du socle en mode `router.auto` du manifeste.
//
// La CLI scanne l'arbre, génère `.liorian/routes.registry.ts` (dossier
// ignoré du watcher — pas de boucle) que le wrapper d'amorçage passe à
// `bootstrapModule`, et, en mode `auto`, synchronise le manifeste. Le
// routage manuel reste la norme par défaut : sans dossier de routes (ou en
// `router.mode: "manual"`), rien n'est généré ni touché.
package artifactdev

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// DefaultRoutesDir est l'arbre de routes conventionnel d'un module.
const DefaultRoutesDir = "presentation/routes"

// RoutesRegistryName est le registre généré, relatif à la racine du module.
const RoutesRegistryName = ".liorian/routes.registry.ts"

// RouteMeta est l'extraction leniente de `export const route` d'un fichier —
// suffisant pour la synchro du menu ; la vérité reste lue au runtime par le
// registre généré.
type RouteMeta struct {
	Title string
	Icon  string
	Order int
	Menu  *bool
}

// RouteFile est une route découverte dans l'arbre de routes.
type RouteFile struct {
	// Pattern est le chemin relatif au module : `""` (racine),
	// `customers/:id`, `*` (vue 404).
	Pattern string
	// Segments est le pattern découpé (les `:param` inclus).
	Segments []string
	// Params est la liste ordonnée des paramètres dynamiques.
	Params []string
	// Source est le chemin du fichier, relatif à la racine du module.
	Source string
	// Meta est l'extraction leniente de `export const route`.
	Meta RouteMeta
}

var (
	routeMetaBlockRE = regexp.MustCompile(`export\s+const\s+route\s*=\s*\{([^}]*)\}`)
	routeTitleRE     = regexp.MustCompile(`title\s*:\s*"([^"]*)"`)
	routeIconRE      = regexp.MustCompile(`icon\s*:\s*"([^"]*)"`)
	routeOrderRE     = regexp.MustCompile(`order\s*:\s*(\d+)`)
	routeMenuRE      = regexp.MustCompile(`menu\s*:\s*(true|false)`)
)

// ScanRouteFiles relève les routes du module. Un fichier TypeScript qui
// n'exporte pas de vue par défaut est signalé puis ignoré — un utilitaire
// interne a sa place dans l'arbre, pas dans la table.
func ScanRouteFiles(cfg *Config, log func(string)) ([]RouteFile, error) {
	if cfg.RoutesDir == "" {
		return nil, nil
	}
	var routes []RouteFile
	err := filepath.Walk(cfg.RoutesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") || strings.HasPrefix(info.Name(), "_") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".tsx" && ext != ".ts" {
			return nil
		}
		base := strings.TrimSuffix(info.Name(), filepath.Ext(info.Name()))
		if base == "" || strings.HasPrefix(base, "_") || strings.HasPrefix(base, ".") ||
			strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".stories") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !strings.Contains(string(content), "export default") {
			log(fmt.Sprintf("artifact: %s ignoré du registre de routes (aucun export default)", path))
			return nil
		}

		rel, err := filepath.Rel(cfg.RoutesDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(strings.TrimSuffix(rel, filepath.Ext(path)))
		segments := strings.Split(rel, "/")
		if segments[len(segments)-1] == "index" {
			segments = segments[:len(segments)-1]
		}
		pattern := make([]string, 0, len(segments))
		var params []string
		for _, segment := range segments {
			if strings.HasPrefix(segment, "[") && strings.HasSuffix(segment, "]") {
				name := segment[1 : len(segment)-1]
				if name == "" {
					return fmt.Errorf("artifact: paramètre de route sans nom dans %s", path)
				}
				pattern = append(pattern, ":"+name)
				params = append(params, name)
				continue
			}
			pattern = append(pattern, segment)
		}
		joined := strings.Join(pattern, "/")
		if joined == "not-found" {
			joined = "*"
		}

		routes = append(routes, RouteFile{
			Pattern:  joined,
			Segments: pattern,
			Params:   params,
			Source:   filepath.ToSlash(filepath.Join(strings.TrimPrefix(cfg.RoutesDir, cfg.ModuleDir+string(filepath.Separator)), rel)),
			Meta:     extractRouteMeta(string(content)),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sortRouteFiles(routes)
	return routes, nil
}

// extractRouteMeta parse leniement le bloc `export const route = {…}` : un
// objet riche (expressions, imports) est hors périmètre — seules les valeurs
// littérales simples alimentent la synchro du manifeste.
func extractRouteMeta(content string) RouteMeta {
	block := routeMetaBlockRE.FindStringSubmatch(content)
	if block == nil {
		return RouteMeta{}
	}
	body := block[1]
	meta := RouteMeta{}
	if match := routeTitleRE.FindStringSubmatch(body); match != nil {
		meta.Title = match[1]
	}
	if match := routeIconRE.FindStringSubmatch(body); match != nil {
		meta.Icon = match[1]
	}
	if match := routeOrderRE.FindStringSubmatch(body); match != nil {
		var order int
		_, _ = fmt.Sscanf(match[1], "%d", &order)
		meta.Order = order
	}
	if match := routeMenuRE.FindStringSubmatch(body); match != nil {
		menu := match[1] == "true"
		meta.Menu = &menu
	}
	return meta
}

// sortRouteFiles ordonne la table pour le matching : racine, routes
// statiques avant dynamiques (une route `customers/nouveau` doit passer avant
// `customers/:id`), profondeur croissante, puis alphabétique. La route `*`
// ferme la marche.
func sortRouteFiles(routes []RouteFile) {
	sort.SliceStable(routes, func(i, j int) bool {
		a, b := routes[i], routes[j]
		if a.Pattern == "*" {
			return false
		}
		if b.Pattern == "*" {
			return true
		}
		if a.Pattern == "" {
			return true
		}
		if b.Pattern == "" {
			return false
		}
		if len(a.Params) != len(b.Params) {
			return len(a.Params) < len(b.Params)
		}
		if len(a.Segments) != len(b.Segments) {
			return len(a.Segments) < len(b.Segments)
		}
		return a.Pattern < b.Pattern
	})
}

// ensureRouteRegistry scanne l'arbre de routes et met le registre généré au
// niveau : écrit seulement quand le contenu change (pas de rebuild en
// boucle), retiré quand le module repasse en routage manuel. Rend la table
// (vide en routage manuel).
func ensureRouteRegistry(cfg *Config, log func(string)) ([]RouteFile, error) {
	registryPath := filepath.Join(cfg.ModuleDir, RoutesRegistryName)
	routes, err := ScanRouteFiles(cfg, log)
	if err != nil {
		return nil, err
	}
	if len(routes) == 0 {
		if err := os.Remove(registryPath); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		return nil, nil
	}
	content, err := renderRouteRegistry(routes)
	if err != nil {
		return nil, err
	}
	if existing, err := os.ReadFile(registryPath); err == nil && string(existing) == content {
		return routes, nil
	}
	if err := os.MkdirAll(filepath.Dir(registryPath), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(registryPath, []byte(content), 0o644); err != nil {
		return nil, err
	}
	log(fmt.Sprintf("artifact: registre de routes régénéré (%d routes) — %s", len(routes), RoutesRegistryName))
	return routes, nil
}

// renderRouteRegistry rend le registre TypeScript : import de chaque vue,
// table typée consommée par `bootstrapModule` via le wrapper. Les
// métadonnées restent lues au runtime (`Route0.route`) — la vérité est dans
// le fichier de route, pas dans la CLI.
func renderRouteRegistry(routes []RouteFile) (string, error) {
	var body strings.Builder
	body.WriteString("// Généré par `liora artifact build/dev` depuis l'arbre de routes du module —\n")
	body.WriteString("// ne pas éditer : toute modification est écrasée au build suivant (§8.5).\n")
	body.WriteString("import type {ModuleRouteRecordInterface} from \"@liorian/sdk/domain/entities/module-router.interface\";\n\n")
	for index, route := range routes {
		importPath := "../" + route.Source
		fmt.Fprintf(&body, "import * as Route%d from \"%s\";\n", index, importPath)
	}
	body.WriteString("\nconst routes: ModuleRouteRecordInterface[] = [\n")
	for index, route := range routes {
		fmt.Fprintf(&body, "\t{path: %q, params: %s, component: Route%d.default, meta: Route%d.route as any},\n",
			route.Pattern, formatParams(route.Params), index, index)
	}
	body.WriteString("];\n\nexport default routes;\n")
	return body.String(), nil
}

func formatParams(params []string) string {
	if len(params) == 0 {
		return "[]"
	}
	quoted := make([]string, 0, len(params))
	for _, param := range params {
		quoted = append(quoted, fmt.Sprintf("%q", param))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// syncManifestRoutes enrichit le manifeste en mode `router.auto` (§8.5) :
// table `routes` (chemin, titre, icône) et complétion de `menu.items` — les
// entrées déclarées à la main gagnent toujours sur les entrées dérivées.
// Idempotent : un manifeste déjà synchronisé n'est pas réécrit.
func syncManifestRoutes(cfg *Config, manifest *Manifest, routes []RouteFile, log func(string)) error {
	if manifest.Router == nil || manifest.Router.Mode != "auto" || len(routes) == 0 {
		return nil
	}
	uri, _ := manifest.Extra["uri"].(string)
	if uri == "" {
		uri = "/m/" + strings.TrimPrefix(manifest.ID, "")
	}

	menuItems, _ := manifest.Extra["menu"].(map[string]any)
	items, _ := menuItems["items"].([]any)
	manualURLs := map[string]bool{}
	for _, item := range items {
		if object, ok := item.(map[string]any); ok {
			if url, ok := object["url"].(string); ok {
				manualURLs[url] = true
			}
		}
	}

	derived := make([]any, 0, len(routes))
	for _, route := range routes {
		if route.Pattern == "*" {
			continue
		}
		if route.Meta.Menu != nil && !*route.Meta.Menu {
			continue
		}
		entry := map[string]any{"path": route.Pattern}
		if route.Meta.Title != "" {
			entry["title"] = route.Meta.Title
		}
		if route.Meta.Icon != "" {
			entry["icon"] = route.Meta.Icon
		}
		derived = append(derived, entry)

		if route.Meta.Title == "" || route.Meta.Menu != nil && !*route.Meta.Menu {
			continue
		}
		url := uri
		if route.Pattern != "" {
			url = strings.TrimSuffix(uri, "/") + "/" + route.Pattern
		}
		if manualURLs[url] {
			continue
		}
		items = append(items, map[string]any{
			"label": route.Meta.Title,
			"icon":  route.Meta.Icon,
			"url":   url,
		})
	}

	updated := deepCopyMap(manifest.Extra)
	updated["routes"] = derived
	if menuItems == nil {
		menuItems = map[string]any{}
	}
	if len(items) > 0 {
		menuItems["items"] = items
		updated["menu"] = menuItems
	}

	if manifestUnchanged(manifest.Extra, updated) {
		return nil
	}
	encoded, err := marshalManifestJSON(updated)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(cfg.ModuleDir, "manifest.json"), encoded, 0o644); err != nil {
		return err
	}
	log(fmt.Sprintf("artifact: manifeste synchronisé avec le registre de routes (router.auto) — %s", filepath.Join(cfg.ModuleDir, "manifest.json")))
	return nil
}

// manifestUnchanged compare deux manifestes sémantiquement (JSON canonique) :
// la réécriture ne doit jamais être déclenchée par le formatage.
func manifestUnchanged(current, updated map[string]any) bool {
	currentEncoded, errCurrent := marshalManifestJSON(current)
	updatedEncoded, errUpdated := marshalManifestJSON(updated)
	if errCurrent != nil || errUpdated != nil {
		return false
	}
	return string(currentEncoded) == string(updatedEncoded)
}

// marshalManifestJSON sérialise le manifeste en forme canonique lisible :
// clés triées, indentation deux espaces — la forme attendue par le checksum
// de pack et par la comparaison idempotente.
func marshalManifestJSON(manifest map[string]any) ([]byte, error) {
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func deepCopyMap(source map[string]any) map[string]any {
	encoded, err := json.Marshal(source)
	if err != nil {
		return map[string]any{}
	}
	var copy map[string]any
	if json.Unmarshal(encoded, &copy) != nil {
		return map[string]any{}
	}
	return copy
}
