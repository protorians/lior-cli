package module

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/pkg"
)

// Embedded reference mockups shipped inside the CLI so `liora create
// module` works on any machine with no external checkout.
//
//go:embed mockups
var embeddedTemplates embed.FS

// Paths of the embedded reference mockups.
const (
	embeddedPageFile = "mockups/page.tsx"
	embeddedViewFile = "mockups/view.tsx"
	// embeddedPageSampleID is the sample module identifier the page mockup is
	// written with (the page template is shared by every module type, so it
	// keeps the reference hello-world naming and is renamed onto the new
	// module like any other mockup file).
	embeddedPageSampleID = "hello-world"
)

// embeddedTypeMockups maps every canonical module type onto the embedded
// reference mockup scaffolded for that type (spec `module-types` §2): each
// type owns a mockup shaped for its injection surface — settings entries for
// CONFIGURATION, routines.tsx for SERVICE, widget components for WIDGET,
// token palettes for THEME, a remote declaration for WEB_APP_REMOTE, the full
// local application for WEB_APP_LOCAL and the Tauri-only admin application for
// SYSTEM. The legacy aliases and an empty type fall back to the reference
// hello-world mockup (the WEB_APP_LOCAL shape).
var embeddedTypeMockups = map[string]string{
	"CONFIGURATION":  "mockups/configuration",
	"SERVICE":        "mockups/service",
	"WIDGET":         "mockups/widget",
	"THEME":          "mockups/theme",
	"WEB_APP_REMOTE": "mockups/web-app-remote",
	"WEB_APP_LOCAL":  "mockups/hello-world",
	"SYSTEM":         "mockups/system",
}

// defaultEmbeddedMockup is the mockup used when the type owns no dedicated
// one: the reference hello-world application (WEB_APP_LOCAL shape).
const defaultEmbeddedMockup = "mockups/hello-world"

// embeddedMockupPrefix returns the embedded mockup prefix scaffolded for a
// module type.
func embeddedMockupPrefix(moduleType string) string {
	if prefix, ok := embeddedTypeMockups[strings.ToUpper(strings.TrimSpace(moduleType))]; ok {
		return prefix
	}
	return defaultEmbeddedMockup
}

// EmbeddedMockupName returns the name of the embedded mockup scaffolded for a
// module type ("hello-world" for WEB_APP_LOCAL) — what the success panel of
// `liora create module` reports.
func EmbeddedMockupName(moduleType string) string {
	return filepath.Base(embeddedMockupPrefix(moduleType))
}

// ModuleTypeSupportsPage reports whether a module type owns a visible
// application surface and therefore gets a `src/app/<url>/page.tsx` route
// scaffolded. The injection-only types (CONFIGURATION, SERVICE, WIDGET,
// THEME) and WEB_APP_REMOTE — whose app is hosted on the registered remote
// origin, never in the socle — have no socle page: their `uri` stays the
// runtime address the socle resolves (`/m/<slug>/…` for installed modules,
// `settings.entries` for a CONFIGURATION).
func ModuleTypeSupportsPage(moduleType string) bool {
	switch strings.ToUpper(strings.TrimSpace(moduleType)) {
	case "CONFIGURATION", "SERVICE", "WIDGET", "THEME", "WEB_APP_REMOTE":
		return false
	}
	return true
}

// Environment variables overriding the embedded reference mockups.
const (
	// EnvModuleMockup points to a custom directory of a reference module to
	// copy (a hello-world style module whose components are renamed).
	EnvModuleMockup = "LIORIAN_MODULE_MOCKUP"
	// EnvPageMockup points to a custom `src/app/<name>/page.tsx` template used
	// when the module declaration declares a `uri`/`url`.
	EnvPageMockup = "LIORIAN_PAGE_MOCKUP"
	// EnvViewMockup points to a custom `<name>.view.tsx` template used by
	// `liora create view`.
	EnvViewMockup = "LIORIAN_VIEW_MOCKUP"
)

// moduleMockupSource returns the custom module mockup directory to scaffold
// from (Creator field first, then environment), or "" to use the embedded one.
func (c *Creator) moduleMockupSource() string {
	if c.MockupDir != "" && IsModuleMockup(c.MockupDir) {
		return c.MockupDir
	}
	if p := os.Getenv(EnvModuleMockup); p != "" && IsModuleMockup(p) {
		return p
	}
	return ""
}

// pageMockupSource returns the custom page mockup file (Creator field first,
// then environment), or "" to use the embedded one.
func (c *Creator) pageMockupSource() string {
	if c.PageMockup != "" && pkg.FileExists(c.PageMockup) {
		return c.PageMockup
	}
	if p := os.Getenv(EnvPageMockup); p != "" && pkg.FileExists(p) {
		return p
	}
	return ""
}

// IsModuleMockup reports whether dir looks like a scofoldable module mockup.
func IsModuleMockup(dir string) bool {
	return pkg.DirExists(dir) &&
		pkg.FileExists(filepath.Join(dir, config.ManifestFileName)) &&
		pkg.FileExists(filepath.Join(dir, config.LegacyDeclarationFileName))
}

// scaffoldFromMockup copies a reference module directory into `moduleDir` and
// rewrites every component/identifier carrying the mockup sample module name
// with the module spec (identifier for the naming).
func scaffoldFromMockup(mockup, moduleDir string, spec ModuleSpec) error {
	if err := pkg.CopyDir(mockup, moduleDir); err != nil {
		return fmt.Errorf("failed to copy module mockup: %w", err)
	}

	repls := mockupReplacements(mockupSampleOf(filepath.Join(mockup, config.ManifestFileName)), spec)
	if err := renameAndRewriteTree(moduleDir, repls); err != nil {
		return err
	}
	return finishScaffold(moduleDir, spec)
}

// scaffoldEmbeddedModule writes the embedded reference mockup of the module
// type into `moduleDir`, renaming components and identifiers with the module
// spec and forcing the new module identity onto the metadata files.
func scaffoldEmbeddedModule(moduleDir string, spec ModuleSpec) error {
	prefix := embeddedMockupPrefix(spec.EffectiveType())
	repls := mockupReplacements(embeddedSample(prefix), spec)
	if err := fs.WalkDir(embeddedTemplates, prefix, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(prefix, path)
		if err != nil {
			return err
		}
		data, err := embeddedTemplates.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read embedded mockup %s: %w", path, err)
		}
		if isTextFile(path) {
			data = []byte(applyReplacements(string(data), repls))
		}
		target := filepath.Join(moduleDir, filepath.FromSlash(applyReplacements(rel, repls)))
		if err := pkg.WriteFile(target, data); err != nil {
			return fmt.Errorf("failed to write %s: %w", target, err)
		}
		return nil
	}); err != nil {
		return err
	}
	return finishScaffold(moduleDir, spec)
}

// finishScaffold applies the shared post-copy work: manifest/declaration
// identity, descriptions and the README.
func finishScaffold(moduleDir string, spec ModuleSpec) error {
	if err := patchManifestIdentity(moduleDir, spec); err != nil {
		return err
	}
	if err := patchDeclarationIdentity(moduleDir, spec); err != nil {
		return err
	}
	if err := patchPackageDescription(moduleDir, spec); err != nil {
		return err
	}
	if err := pkg.WriteString(filepath.Join(moduleDir, "README.md"), mockupReadmeTemplate(spec)); err != nil {
		return fmt.Errorf("failed to write README.md: %w", err)
	}
	return nil
}

// moduleRepl is a single module-name token substitution.
type moduleRepl struct {
	old string
	new string
}

// sampleSpellings returns every spelling a mockup uses for one sample module
// identifier: Title Case, PascalCase, camelCase, kebab-case, UPPER_SNAKE and
// the bare concatenation ("hello-world" → "Hello World", "HelloWorld",
// "helloWorld", "hello-world", "HELLO_WORLD", "helloworld").
func sampleSpellings(id string) []string {
	return []string{
		displayName(id),
		pascalName(id),
		camelName(id),
		id,
		upperSnake(id),
		lowerName(id),
	}
}

// mockupSample is the sample identity a mockup is written with: the `id` of
// its manifest.json (drives the component/identifier renames) and its
// `domain` (drives the module references of the assistive-help file, e.g.
// `data-help="module:<domain>"`).
type mockupSample struct {
	ID     string
	Domain string
}

// mockupSampleOf extracts the sample identity a mockup is written with — the
// `id` and `domain` of its manifest.json. Every mockup (embedded or custom)
// carries one: the rename machinery maps its spellings onto the new module
// spec, so a mockup may be written with any sample name. An unreadable
// manifest falls back to the reference "hello-world" (the original mockup).
func mockupSampleOf(manifestPath string) mockupSample {
	return sampleFromManifest(func() ([]byte, error) {
		return os.ReadFile(manifestPath)
	})
}

// embeddedSample extracts the sample identity of an embedded mockup prefix.
func embeddedSample(prefix string) mockupSample {
	return sampleFromManifest(func() ([]byte, error) {
		return embeddedTemplates.ReadFile(prefix + "/" + config.ManifestFileName)
	})
}

// sampleFromManifest decodes a manifest.json payload into its sample identity
// (`id` and `domain`).
func sampleFromManifest(read func() ([]byte, error)) mockupSample {
	fallback := mockupSample{ID: "hello-world", Domain: "mod.liorian.hello-world"}
	data, err := read()
	if err != nil {
		return fallback
	}
	var manifest struct {
		ID     string `json:"id"`
		Domain string `json:"domain"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.ID == "" {
		return fallback
	}
	return mockupSample{ID: manifest.ID, Domain: manifest.Domain}
}

// mockupReplacements maps every spelling of the sample module name and the
// sample module domain a mockup was written with onto its counterpart for the
// module spec. The longest, most specific spellings go first so a
// case-sensitive token is never partially rewritten by a shorter one.
func mockupReplacements(sample mockupSample, spec ModuleSpec) []moduleRepl {
	sources := sampleSpellings(sample.ID)
	targets := sampleSpellings(spec.ID)
	// A single-word sample produces identical spellings for several kinds
	// ("acme" is at once its kebab, camel and lower form). Keep one target per
	// source spelling, preferring the kinds a module visibly carries: file
	// names and identifiers (kebab), component names (Pascal) and manifest
	// keys (UPPER_SNAKE). Display names are re-patched afterwards by
	// finishScaffold from the spec.
	var priority = [6]int{3, 1, 4, 2, 0, 5}
	repls := make([]moduleRepl, 0, len(sources)+1)
	seen := map[string]bool{}
	for _, kind := range priority {
		if sources[kind] == targets[kind] || seen[sources[kind]] {
			continue
		}
		seen[sources[kind]] = true
		repls = append(repls, moduleRepl{old: sources[kind], new: targets[kind]})
	}
	// The module domain is a first-class token: the assistive-help file anchors
	// its `data-help="module:<domain>"` targets on it, and the declaration
	// carries it as `identifier`. Rename it alongside the identifier spellings
	// — the domain is longer than the bare identifier (it contains it), so the
	// longest-first sort below rewrites it before any spelling could split it.
	if domain := strings.TrimSpace(sample.Domain); domain != "" && domain != spec.Domain {
		repls = append(repls, moduleRepl{old: domain, new: spec.Domain})
	}
	// Longest first: "Hello World" (Title) must be rewritten before "Hello"
	// could split it, and a camelCase token must never match inside the lower
	// concatenation once the latter was already replaced.
	sort.Slice(repls, func(i, j int) bool { return len(repls[i].old) > len(repls[j].old) })
	return repls
}

// applyReplacements applies substitution rules to a string.
func applyReplacements(s string, repls []moduleRepl) string {
	for _, r := range repls {
		s = strings.ReplaceAll(s, r.old, r.new)
	}
	return s
}

// renameAndRewriteTree walks `dir`, renaming files that embed the mockup module
// name and rewriting the text content of source files.
func renameAndRewriteTree(dir string, repls []moduleRepl) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		renamed := filepath.Join(filepath.Dir(path), applyReplacements(filepath.Base(path), repls))
		if renamed != path {
			if err := os.Rename(path, renamed); err != nil {
				return fmt.Errorf("failed to rename %s: %w", path, err)
			}
			path = renamed
			info, err = os.Stat(path)
			if err != nil {
				return err
			}
		}

		if !isTextFile(path) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rewritten := applyReplacements(string(data), repls)
		if rewritten == string(data) {
			return nil
		}
		return os.WriteFile(path, []byte(rewritten), info.Mode())
	})
}

var textExtensions = map[string]bool{
	".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true,
	".json": true, ".md": true, ".css": true, ".html": true,
	".toml": true, ".yml": true, ".yaml": true,
}

// isTextFile reports whether a file should be rewritten as text.
func isTextFile(path string) bool {
	return textExtensions[strings.ToLower(filepath.Ext(path))]
}

var (
	manifestIDRe      = regexp.MustCompile(`("id"\s*:\s*"[^"]*")`)
	jsonDescriptionRe = regexp.MustCompile(`("description"\s*:\s*)"[^"]*"`)
	manifestTokenRe   = regexp.MustCompile(`"token":`)
	declaredURIAttrRe = regexp.MustCompile(`(?m)^\s*(?:uri|url)\s*[:=]\s*['"]([^'"]+)['"]`)
)

// patchManifestIdentity ensures the scaffolded manifest.json carries the new
// module identity: a unique UUID token (the mockup has none) and the provided
// spec metadata (identifier, domain, key, application name, description,
// version, icon and url).
func patchManifestIdentity(moduleDir string, spec ModuleSpec) error {
	manifestPath := filepath.Join(moduleDir, config.ManifestFileName)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read scaffolded manifest: %w", err)
	}
	text := string(data)

	if !manifestTokenRe.MatchString(text) {
		m := manifestIDRe.FindString(text)
		if m == "" {
			return fmt.Errorf("scaffolded %s is missing an id field", config.ManifestFileName)
		}
		text = strings.Replace(text, m, m+",\n  \"token\": \""+pkg.NewUUID()+"\"", 1)
	}

	text = patchJSONField(text, "id", spec.ID)
	text = patchJSONField(text, "domain", spec.Domain)
	text = patchJSONField(text, "key", upperSnake(spec.ID))
	text = patchJSONField(text, "name", spec.AppName)
	text = patchJSONField(text, "description", spec.Description)
	text = patchJSONField(text, "version", spec.Version)
	if spec.Icon != "" {
		text = patchJSONField(text, "icon", spec.Icon)
	}
	text = patchJSONField(text, "type", spec.EffectiveType())
	text = patchJSONField(text, "category", spec.EffectiveCategory())
	text = patchJSONField(text, "uri", "/"+spec.URL)
	// Menu entries carry a nested `url`; sync them without ever injecting a
	// non-canonical top-level `url` field.
	text = patchJSONFieldExisting(text, "url", "/"+spec.URL)

	return pkg.WriteString(manifestPath, text)
}

// patchDeclarationIdentity rewrites the module declaration (index.tsx) identity
// and menu fields of the scaffolded module.
func patchDeclarationIdentity(moduleDir string, spec ModuleSpec) error {
	indexPath := filepath.Join(moduleDir, config.LegacyDeclarationFileName)
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return fmt.Errorf("failed to read scaffolded declaration: %w", err)
	}
	text := string(data)
	text = patchJSField(text, "identifier", spec.Domain)
	text = patchJSField(text, "key", upperSnake(spec.ID))
	text = patchJSField(text, "version", spec.Version)
	text = patchJSField(text, "name", spec.AppName)
	text = patchJSField(text, "description", spec.Description)
	if spec.Icon != "" {
		text = patchJSField(text, "icon", spec.Icon)
	}
	text = patchJSField(text, "type", spec.EffectiveType())
	text = patchJSField(text, "category", spec.EffectiveCategory())
	text = patchJSField(text, "uri", "/"+spec.URL)
	// Menu entries carry a nested `url`; sync the existing ones only.
	text = patchJSFieldExisting(text, "url", "/"+spec.URL)
	if err := os.WriteFile(indexPath, []byte(text), 0o644); err != nil {
		return fmt.Errorf("failed to update %s: %w", indexPath, err)
	}
	return nil
}

// patchPackageDescription updates the description of the scaffolded
// package.json.
func patchPackageDescription(moduleDir string, spec ModuleSpec) error {
	packagePath := filepath.Join(moduleDir, "package.json")
	if !pkg.FileExists(packagePath) {
		return nil
	}
	data, err := os.ReadFile(packagePath)
	if err != nil {
		return fmt.Errorf("failed to read scaffolded package.json: %w", err)
	}
	descLit, _ := json.Marshal(spec.Description)
	updated := jsonDescriptionRe.ReplaceAllString(string(data), "${1}"+strings.ReplaceAll(string(descLit), "$", "$$"))
	if err := os.WriteFile(packagePath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("failed to update package.json: %w", err)
	}
	return nil
}

// patchJSONField rewrites the value of a top-level JSON string field (fields at
// the standard two-space indent, i.e. not nested). The value is JSON-encoded
// before insertion. When the field is absent it is inserted as the new last
// top-level field (right before the final closing brace of the manifest).
func patchJSONField(text, field, value string) string {
	re := regexp.MustCompile(`(?m)^ {2}"` + regexp.QuoteMeta(field) + `"\s*:\s*"[^"]*"`)
	encoded, err := json.Marshal(value)
	if err != nil {
		return text
	}
	replacement := strings.ReplaceAll(string(encoded), "$", "$$")
	if re.MatchString(text) {
		return re.ReplaceAllString(text, `  "`+field+`": `+replacement)
	}
	lastBrace := regexp.MustCompile(`(?m)\n\}$`)
	return lastBrace.ReplaceAllString(text, ",\n  \""+field+"\": "+replacement+"\n}")
}

// patchJSONFieldExisting rewrites every occurrence of a JSON string field at
// any indentation (e.g. a nested menu-item `url`) without inserting a new
// top-level field when the field is absent.
func patchJSONFieldExisting(text, field, value string) string {
	re := regexp.MustCompile(`(?m)^(\s*)"` + regexp.QuoteMeta(field) + `"\s*:\s*"[^"]*"`)
	encoded, err := json.Marshal(value)
	if err != nil {
		return text
	}
	replacement := strings.ReplaceAll(string(encoded), "$", "$$")
	return re.ReplaceAllString(text, "${1}\""+field+"\": "+replacement)
}

// patchJSField rewrites a `field: 'value'` line of the declarative module
// declaration (index.tsx), preserving the leading indentation and the trailing
// comma. When the field is absent it is inserted as the last member of the
// declarative object (before the closing brace that precedes `export default`).
func patchJSField(text, field, value string) string {
	re := regexp.MustCompile(`(?m)^(\s*)(?:` + regexp.QuoteMeta(field) + `):.*$`)
	escaped := strings.ReplaceAll(value, "'", `\'`)
	escaped = strings.ReplaceAll(escaped, "$", "$$")
	if re.MatchString(text) {
		return re.ReplaceAllString(text, "${1}"+field+": '"+escaped+"',")
	}
	closing := regexp.MustCompile(`\n}\n\nexport default`)
	return closing.ReplaceAllString(text, "\n    "+field+": '"+escaped+"',\n}\n\nexport default")
}

// patchJSFieldExisting rewrites an existing `field: 'value'` line of the
// declarative module declaration without inserting a new member when the field
// is absent.
func patchJSFieldExisting(text, field, value string) string {
	re := regexp.MustCompile(`(?m)^(\s*)(?:` + regexp.QuoteMeta(field) + `):.*$`)
	if !re.MatchString(text) {
		return text
	}
	escaped := strings.ReplaceAll(value, "'", `\'`)
	escaped = strings.ReplaceAll(escaped, "$", "$$")
	return re.ReplaceAllString(text, "${1}"+field+": '"+escaped+"',")
}

// scaffoldPage generates `src/app/<url>/page.tsx` from a page mockup file.
// It returns the created page path ("" when the mockup is unavailable).
func scaffoldPage(root, moduleDir string, spec ModuleSpec, pageMockup string) string {
	uri := declaredURI(filepath.Join(moduleDir, config.LegacyDeclarationFileName))
	if uri == "" {
		return ""
	}

	data, err := os.ReadFile(pageMockup)
	if err != nil {
		return ""
	}
	return writeScaffoldedPage(root, spec, rewritePageBody(string(data), spec))
}

// scaffoldEmbeddedPage generates `src/app/<url>/page.tsx` from the embedded
// page mockup.
func scaffoldEmbeddedPage(root, moduleDir string, spec ModuleSpec) string {
	uri := declaredURI(filepath.Join(moduleDir, config.LegacyDeclarationFileName))
	if uri == "" {
		return ""
	}

	data, err := embeddedTemplates.ReadFile(embeddedPageFile)
	if err != nil {
		return ""
	}
	return writeScaffoldedPage(root, spec, rewritePageBody(string(data), spec))
}

// rewritePageBody renames the mockup components in a page body and rewrites the
// module import path to the module domain
// (`@/library/modules/<domain>/...`). The shared page mockup keeps the
// reference hello-world sample naming — the page template is common to every
// module type — and is renamed onto the module spec like any other mockup.
func rewritePageBody(body string, spec ModuleSpec) string {
	body = applyReplacements(body, mockupReplacements(mockupSample{ID: embeddedPageSampleID}, spec))
	return strings.ReplaceAll(body, config.ExternalModulesDir+"/"+spec.ID+"/", config.ExternalModulesDir+"/"+spec.Domain+"/")
}

// writeScaffoldedPage writes a page body at `src/app/<url>/page.tsx`.
func writeScaffoldedPage(root string, spec ModuleSpec, body string) string {
	pagePath := filepath.Join(root, config.AppSrcDir, spec.URL, "page.tsx")
	if err := pkg.WriteString(pagePath, body); err != nil {
		return ""
	}
	return pagePath
}

// declaredURI extracts the `uri`/`url` of the module declaration, "" when none
// is declared.
func declaredURI(indexPath string) string {
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return ""
	}
	m := declaredURIAttrRe.FindStringSubmatch(string(data))
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// mockupReadmeTemplate documents a module scaffolded from the reference mockup.
// The structure section and the type contracts adapt to the module type
// (spec `module-types` §2) — the mockup each type scaffolds from is the one
// shaped for its injection surface.
func mockupReadmeTemplate(spec ModuleSpec) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# %s\n\n", spec.AppName))
	if desc := spec.Description; desc != "" {
		b.WriteString(desc + "\n\n")
	}
	b.WriteString(fmt.Sprintf("Liora module `%s` (`%s`) — type **%s**.\n\n", spec.Domain, spec.ID, spec.EffectiveType()))
	b.WriteString(moduleTypeReadmeSection(spec.EffectiveType()))
	b.WriteString("## Structure\n\n")
	b.WriteString("- `manifest.json` — module metadata\n")
	b.WriteString("- `index.tsx` — module declaration (identifier, widgets, service, routines, uri)\n")
	b.WriteString("- `package.json` — module dependencies\n")
	b.WriteString("- `tsconfig.json` — standalone TypeScript configuration\n")
	for _, line := range mockupReadmeStructure(spec.EffectiveType()) {
		b.WriteString(line + "\n")
	}
	return b.String()
}

// moduleTypeReadmeSection documents the goal of a module type and its
// development contracts (first-party vs third-party), from spec
// `module-types` §2 and its feature specs.
func moduleTypeReadmeSection(moduleType string) string {
	switch strings.ToUpper(strings.TrimSpace(moduleType)) {
	case "CONFIGURATION":
		return "" +
			"Module **déclaratif de configuration/paramétrage** : ses entrées de réglages " +
			"s'injectent dans le dropdown du compte connecté et dans le hub `/settings` dès que " +
			"les vérifications du registre passent (V-1 → V-7).\n\n" +
			"- **First-party** : le fichier `settings.tsx` exporte un tableau d'entrées " +
			"`{label, description?, component}` rendu nativement par le socle.\n" +
			"- **Tiers** : aucune ligne de code — le manifeste déclare `settings.entries` " +
			"(routes `/m/<slug>/<path>`) et `configSettings` ; `entry: index.json`.\n\n"
	case "SERVICE":
		return "" +
			"Module **de service d'arrière-plan** : ses routines s'ajoutent à la liste des " +
			"routines (`HeaderRoutines`). Persistante par défaut, une routine devient non " +
			"persistante dès qu'elle déclare une condition de déclenchement (`trigger`).\n\n" +
			"- **First-party** : le fichier `routines.tsx` exporte un tableau de singletons " +
			"`Routine` (`persist: true` par défaut, `trigger: {url, modules}` pour restreindre).\n" +
			"- **Tiers** : descripteurs `routines[]` du manifeste (`job.kind: \"api\"`, " +
			"`intervalMs` borné 30 s–1 h), exécutés par le socle via `ctx.api`.\n\n"
	case "WIDGET":
		return "" +
			"Module **fournisseur de widgets** du tableau de bord : ses widgets s'ajoutent à la " +
			"liste des widgets quand les vérifications passent.\n\n" +
			"- **First-party** : composants React déclarés dans `index.tsx` " +
			"(`widgets: {\"<id>.kpi\": Composant}`).\n" +
			"- **Tiers** : descripteurs `declarative.widgets[]` du manifeste, rendus " +
			"nativement par le socle (aucun code tiers importé).\n\n"
	case "THEME":
		return "" +
			"Module **fournisseur de thème** : ses palettes de tokens s'ajoutent à la liste des " +
			"thèmes (`/settings/themes`). Un thème est uniquement un jeu de tokens — jamais de " +
			"code ; `scheme` absent vaut `light`, le bloc `dark` est optionnel.\n\n" +
			"- **Manifeste** : `themes[]` (`id`, `label`, `dataTheme`, `swatches`, `tokens`, " +
			"`dark?`) — les clés de `tokens` sont validées contre la whitelist " +
			"`MODULE_THEME_TOKENS`.\n" +
			"- **First-party** : peut en plus fournir une palette CSS compilée (`styles/`).\n\n"
	case "WEB_APP_REMOTE":
		return "" +
			"Application web **hébergée à distance**, chargée en iframe sandboxée depuis " +
			"l'origine enregistrée. Aucune page n'est scaffoldée dans le socle : l'application " +
			"vit sur le serveur distant.\n\n" +
			"- **Manifeste** : section `remote` (`origin`, `paths`, `backends`, `scopes`, " +
			"`wellKnown`).\n" +
			"- **Publication** : le serveur distant doit s'enregistrer, être vérifié " +
			"(well-known / DNS), validé (CSP, egress) puis approuvé par la modération — " +
			"aucune auto-approbation.\n" +
			"- **Runtime** : données uniquement via `ctx.api` (jeton de module), egress " +
			"limité aux backends déclarés, jamais `allow-same-origin`.\n\n"
	case "SYSTEM":
		return "" +
			"Module **first-party `WEB_APP_LOCAL` avec accès administrateur**, disponible " +
			"uniquement sous Tauri (`platforms.web.supported: false` — sur web l'état est " +
			"`platform_unsupported`). Masqué aux développeurs tiers.\n\n" +
			"- **Manifeste** : section `admin` (`roles`, `scopes`) — les privilèges ne sont " +
			"accordés que si le module est first-party, le runtime est Tauri, le rôle de " +
			"l'utilisateur est listé et les scopes sont accordés (fail-closed).\n\n"
	default:
		return "" +
			"Application web **autonome** (`WEB_APP_LOCAL`) : vue principale, composants, " +
			"widget, provider de layout, routine et service d'API. Publiable au Store sous " +
			"forme d'artefact signé, exécutée en iframe isolée une fois installée.\n\n"
	}
}

// mockupReadmeStructure lists the directories the scaffolded module carries,
// per module type.
func mockupReadmeStructure(moduleType string) []string {
	// The assistive-help file is scaffolded for every type that owns a helpers
	// injection surface (spec `assistive-help` §4): CONFIGURATION, WIDGET,
	// WEB_APP_REMOTE, WEB_APP_LOCAL, SYSTEM — never SERVICE (no UI) nor THEME
	// (tokens only).
	const helpersLine = "- `module.helpers.json` — aide assistée (balises + visites guidées)"
	common := []string{"- `application/` — service layer", "- `domain/` — interfaces and enums"}
	switch strings.ToUpper(strings.TrimSpace(moduleType)) {
	case "CONFIGURATION":
		return []string{
			"- `settings.tsx` — paramètres spécifiques du module (contrat first-party)",
			"- `application/` — service layer",
			"- `presentation/settings/` — composants de paramètres",
			helpersLine,
		}
	case "SERVICE":
		return []string{
			"- `routines.tsx` — routines du module (contrat first-party SERVICE)",
			"- `application/` — service layer",
			"- `domain/` — interfaces",
		}
	case "WIDGET":
		return []string{
			"- `application/` — service layer",
			"- `domain/` — interfaces",
			"- `presentation/widgets/` — widgets du tableau de bord",
			helpersLine,
		}
	case "THEME":
		return []string{
			"- `styles/` — palettes CSS compilées (first-party, optionnel)",
		}
	case "WEB_APP_REMOTE":
		return []string{
			"- `manifest.json` — déclare la section `remote` (origine, backends, scopes)",
			helpersLine,
		}
	case "SYSTEM":
		return append(common,
			"- `presentation/` — views and components (accès administrateur, Tauri only)",
			helpersLine,
		)
	default:
		return append(common,
			"- `infrastructure/` — routines",
			"- `presentation/` — views, widgets, components, providers",
			helpersLine,
		)
	}
}
