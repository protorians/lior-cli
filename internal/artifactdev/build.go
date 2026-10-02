// Pipeline de build d'artefact (spec §4.4, §8) : bundle esbuild autonome
// `.liorian/artifact/module.js` + document hôte `.liorian/artifact/index.html`.
//
// Le bundler est l'API Go d'esbuild (esbuild est écrit en Go) : le bundle
// d'un module ne dépend plus d'une installation Node. Le layout de
// développement (`.liorian/artifact/`, D7) est propre au module ; l'archive
// `.LiorArtifactPackage` et l'arbre installé restent en `artifact/**`.
package artifactdev

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	esbuild "github.com/evanw/esbuild/pkg/api"

	"github.com/protorians/lior-cli/internal/artifactdev/style"
)

// EsbuildOptions calcule les options canoniques du bundle de module. Le point
// d'entrée est le wrapper d'amorçage (`.bootstrap-entry.ts`) : l'entrée du
// développeur garde la forme canonique — export `mount`/`unmount` — et le
// bootstrap SDK construit `ctx` depuis le pont postMessage avant d'appeler
// `mount` (§4.2).
func EsbuildOptions(cfg *Config, wrapperPath string, aliases map[string]string) esbuild.BuildOptions {
	minify := cfg.Minify
	return esbuild.BuildOptions{
		EntryPoints:       []string{wrapperPath},
		Outdir:            cfg.ArtifactDir,
		EntryNames:        "module",
		Bundle:            true,
		Platform:          esbuild.PlatformBrowser,
		Target:            esbuild.ES2020,
		Format:            esbuild.FormatESModule,
		Define:            map[string]string{"process.env.NODE_ENV": `"production"`},
		MinifyWhitespace:  minify,
		MinifyIdentifiers: minify,
		MinifySyntax:      minify,
		Splitting:         false,
		Sourcemap:         map[bool]esbuild.SourceMap{true: esbuild.SourceMapExternal, false: esbuild.SourceMapNone}[cfg.Sourcemap],
		JSX:               esbuild.JSXAutomatic,
		AbsWorkingDir:     cfg.ModuleDir,
		LogLevel:          esbuild.LogLevelSilent,
		Write:             true,
		Alias:             aliases,
	}
}

// BuildResult résume un build one-shot.
type BuildResult struct {
	BundlePath    string
	DocumentPath  string
	BundleBytes   int64
	DocumentBytes int64
}

// Build exécute le build complet d'un module : wrapper, bundle esbuild,
// document hôte templatisé, validation non-vide. Le wrapper est retiré à la
// fin (§7.7 : rien de superflu dans la source du module). Fail-closed : la
// première erreur remonte, même sémantique que le pack.
func Build(options BuildOptions, log func(string)) (*BuildResult, error) {
	if log == nil {
		log = func(string) {}
	}
	manifest, cfg, err := Resolve(options)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.ArtifactDir, 0o755); err != nil {
		return nil, err
	}

	// Registre du routeur fichier (§8.5) : généré avant le wrapper, qui
	// l'importe quand il existe. La table rendue sert aussi à la synchro du
	// manifeste en mode `router.auto`.
	routes, err := ensureRouteRegistry(cfg, log)
	if err != nil {
		return nil, err
	}

	wrapperPath, err := WriteBootstrapWrapper(cfg)
	if err != nil {
		return nil, err
	}
	result := esbuild.Build(EsbuildOptions(cfg, wrapperPath, ResolveWorkspaceAliases(cfg.ModuleDir)))
	_ = os.Remove(wrapperPath)
	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("artifact: échec du bundle esbuild — %s", joinMessages(result.Errors))
	}
	CleanupBuildResidue(cfg, false)

	// Pipeline de style : après le bundle esbuild (le moteur `native` n'a rien
	// à exécuter — le CSS des imports existe déjà), avant le rendu du document
	// hôte qui injecte le lien vers le CSS produit.
	if err := runStyleEngine(cfg, log); err != nil {
		return nil, err
	}

	// Synchro du manifeste en mode `router.auto` (§8.5) : table `routes` et
	// complétion de `menu.items`. Uniquement au build one-shot — le dev-server
	// ne touche jamais au manifeste (pas de rebuild en boucle).
	if err := syncManifestRoutes(cfg, manifest, routes, log); err != nil {
		return nil, err
	}

	templateData, err := loadArtifactConfigJSON(cfg.ModuleDir)
	if err != nil {
		return nil, err
	}
	document, err := RenderHostDocument(cfg, manifest, templateData)
	if err != nil {
		return nil, err
	}
	documentPath := filepath.Join(cfg.ArtifactDir, cfg.Document)
	if err := os.WriteFile(documentPath, []byte(document), 0o644); err != nil {
		return nil, err
	}

	if issues := ValidateArtifacts(cfg); len(issues) > 0 {
		return nil, fmt.Errorf("artifact: validation du build en échec — %s", strings.Join(issues, "; "))
	}

	bundlePath := filepath.Join(cfg.ArtifactDir, cfg.Bundle)
	bundleInfo, _ := os.Stat(bundlePath)
	documentInfo, _ := os.Stat(documentPath)
	return &BuildResult{
		BundlePath:    bundlePath,
		DocumentPath:  documentPath,
		BundleBytes:   bundleInfo.Size(),
		DocumentBytes: documentInfo.Size(),
	}, nil
}

// ResolveWorkspaceAliases résout les alias first-party du monorepo (§7.6).
// Le bundle d'un module ne peut pas importer `next/*` (D1), mais il doit
// résoudre ses dépendances first-party `@liorian/module-*` — distribuées en
// source TS sans `dist/` — comme `@liorian/sdk`, en mappant directement leur
// répertoire source. Hors monorepo, aucune racine n'existe et la résolution
// `node_modules` standard s'applique (nil).
func ResolveWorkspaceAliases(moduleDir string) map[string]string {
	aliases := map[string]string{}

	dir := moduleDir
	for {
		sdkSource := filepath.Join(dir, "packages", "sdk", "src")
		if isDir(sdkSource) && aliases["@liorian/sdk"] == "" {
			aliases["@liorian/sdk"] = sdkSource
		}
		for _, modulesRoot := range []string{
			filepath.Join(dir, "modules"),
			filepath.Join(dir, "apps", "liorian-socle", "library", "modules"),
		} {
			if !isDir(modulesRoot) {
				continue
			}
			entries, err := os.ReadDir(modulesRoot)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if !entry.IsDir() || aliases["@"+entry.Name()] != "" {
					continue
				}
				packageJSON := filepath.Join(modulesRoot, entry.Name(), "package.json")
				data, err := os.ReadFile(packageJSON)
				if err != nil {
					continue
				}
				var pkg struct {
					Name string `json:"name"`
				}
				if json.Unmarshal(data, &pkg) != nil || pkg.Name == "" {
					continue
				}
				if strings.HasPrefix(pkg.Name, "@liorian/module-") && aliases[pkg.Name] == "" {
					aliases[pkg.Name] = filepath.Join(modulesRoot, entry.Name())
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	if len(aliases) == 0 {
		return nil
	}
	return aliases
}

// WriteBootstrapWrapper écrit le wrapper d'amorçage à côté de l'entrée et
// renvoie son chemin. En mode watch, il est laissé en place : esbuild le
// surveille avec le graphe de l'entrée et le rebuild ne le réécrit pas.
// Quand le registre du routeur fichier existe (§8.5), il est passé au
// bootstrap — `ctx.router` alimente `<ModuleRouter/>`.
func WriteBootstrapWrapper(cfg *Config) (string, error) {
	entryBase := strings.TrimSuffix(filepath.Base(cfg.Entry), filepath.Ext(cfg.Entry))
	wrapperPath := filepath.Join(cfg.ModuleDir, WrapperName)
	lines := []string{
		`import {bootstrapModule} from "@liorian/sdk/infrastructure/module-runtime/module-bootstrap";`,
		fmt.Sprintf(`import * as definition from "./%s";`, entryBase),
	}
	bootstrap := `bootstrapModule(definition as any);`
	if fileExists(filepath.Join(cfg.ModuleDir, RoutesRegistryName)) {
		lines = append(lines, `import routes from "./.liorian/routes.registry";`)
		bootstrap = `bootstrapModule(definition as any, {router: routes});`
	}
	lines = append(lines, ``, bootstrap, ``)
	if err := os.WriteFile(wrapperPath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return "", err
	}
	return wrapperPath, nil
}

// CleanupBuildResidue supprime le wrapper généré et les sorties de chunk
// parasites (§7.7 : rien de superflu dans la source du module). En mode
// watch, keepWrapper garde le fichier pour que le watcher d'esbuild le
// retrouve.
func CleanupBuildResidue(cfg *Config, keepWrapper bool) {
	wrapperPath := filepath.Join(cfg.ModuleDir, WrapperName)
	if !keepWrapper {
		_ = os.Remove(wrapperPath)
	}
	entryBase := strings.TrimSuffix(filepath.Base(cfg.Entry), filepath.Ext(cfg.Entry))
	// Le `module.css` produit par les imports CSS du bundle est légitime et
	// n'est pas listé : seuls les chunks parasites de l'ancien nommage sont
	// retirés.
	strayNames := map[string]bool{
		entryBase + ".js":         true,
		entryBase + ".css":        true,
		".bootstrap-entry.js":     true,
		".bootstrap-entry.css":    true,
		".bootstrap-entry.js.map": true,
	}
	entries, err := os.ReadDir(cfg.ArtifactDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == cfg.Bundle || name == cfg.Document || name == cfg.Bundle+".map" {
			continue
		}
		if strayNames[name] {
			_ = os.Remove(filepath.Join(cfg.ArtifactDir, name))
		}
	}
}

// ValidateArtifacts vérifie que bundle et document hôte existent et ne sont
// pas vides (§4.4, règle 3 — seul le zéro octet est refusé).
func ValidateArtifacts(cfg *Config) []string {
	var issues []string
	for _, name := range []string{cfg.Bundle, cfg.Document} {
		path := filepath.Join(cfg.ArtifactDir, name)
		info, err := os.Stat(path)
		if err != nil {
			issues = append(issues, fmt.Sprintf("%s : absent du répertoire d'artefact", name))
			continue
		}
		if info.Size() == 0 {
			issues = append(issues, fmt.Sprintf("%s : vide (0 octet)", name))
		}
	}
	return issues
}

// placeholderRE matche `{{ chemin.doté }}` dans le template du document hôte.
var placeholderRE = regexp.MustCompile(`\{\{\s*([\w.$-]+)\s*\}\}`)

// runStyleEngine exécute le moteur de style déclaré par le module
// (`artifact.config.json`, bloc `style`) — `native` par défaut, qui n'exécute
// rien. Fail-closed : une erreur du moteur est une erreur de build, et un
// moteur outillé doit avoir produit le CSS attendu.
func runStyleEngine(cfg *Config, log func(string)) error {
	options, err := style.Load(cfg.ModuleDir)
	if err != nil {
		return err
	}
	engine, err := style.Resolve(options)
	if err != nil {
		return err
	}
	if err := engine.Build(style.Context{
		ModuleDir:   cfg.ModuleDir,
		ArtifactDir: cfg.ArtifactDir,
		OutCSS:      BundleCSS,
		Log:         log,
	}, options); err != nil {
		return err
	}
	if style.RequiresCSS(options) {
		info, err := os.Stat(filepath.Join(cfg.ArtifactDir, BundleCSS))
		if err != nil {
			return fmt.Errorf("style: le moteur %s n'a pas produit %s — vérifie `style.entry`",
				engine.Name(), BundleCSS)
		}
		if info.Size() == 0 {
			return fmt.Errorf("style: le moteur %s a produit un %s vide — vérifie `style.entry` et la détection de contenu",
				engine.Name(), BundleCSS)
		}
	}
	return nil
}

// InjectStylesheetLink insère le lien vers le CSS du bundle dans le document
// hôte quand ce CSS existe (imports CSS du bundle ou moteur de style) — le
// template du module n'a jamais eu à le référencer lui-même. Idempotent : un
// document qui référence déjà le CSS (template du développeur, injection
// précédente) est rendu tel quel.
func InjectStylesheetLink(cfg *Config, html string) string {
	if !fileExists(filepath.Join(cfg.ArtifactDir, BundleCSS)) {
		return html
	}
	if strings.Contains(html, BundleCSS) {
		return html
	}
	link := `<link rel="stylesheet" href="./` + BundleCSS + `" />`
	if strings.Contains(html, "</head>") {
		return strings.Replace(html, "</head>", link+"\n</head>", 1)
	}
	return link + "\n" + html
}

// RenderHostDocument rend le document hôte depuis le template du module et
// les données du manifeste (plus le contexte additionnel de la config). Un
// objet est sérialisé en JSON ; une clé inconnue reste telle quelle, visible.
func RenderHostDocument(cfg *Config, manifest *Manifest, templateData map[string]any) (string, error) {
	if !fileExists(cfg.HTMLTemplate) {
		return "", fmt.Errorf(
			"artifact: template du document hôte introuvable (%s) — crée un index.html à la racine du module",
			cfg.HTMLTemplate,
		)
	}
	template, err := os.ReadFile(cfg.HTMLTemplate)
	if err != nil {
		return "", err
	}
	data := map[string]any{"manifest": manifest.Extra}
	for key, value := range templateData {
		data[key] = value
	}
	rendered := placeholderRE.ReplaceAllStringFunc(string(template), func(match string) string {
		expression := placeholderRE.FindStringSubmatch(match)[1]
		return lookupTemplate(data, expression, match)
	})
	return InjectStylesheetLink(cfg, rendered), nil
}

// lookupTemplate résout un chemin doté dans les données de template ; clé
// inconnue ou objet non sérialisable → placeholder d'origine conservé.
func lookupTemplate(data map[string]any, expression, match string) string {
	var value any = data
	for _, key := range strings.Split(expression, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return match
		}
		value = object[key]
	}
	switch typed := value.(type) {
	case nil:
		return match
	case string:
		return typed
	case map[string]any, []any:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return match
		}
		return string(encoded)
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
