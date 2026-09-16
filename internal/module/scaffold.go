package module

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/protorians/sentient-cli/internal/config"
	"github.com/protorians/sentient-cli/internal/pkg"
)

// Embedded reference mockups shipped inside the CLI so `sentients create
// module` works on any machine with no external checkout.
//
//go:embed mockups
var embeddedTemplates embed.FS

// Paths of the embedded reference mockups.
const (
	embeddedModulePrefix = "mockups/hello-world"
	embeddedPageFile     = "mockups/page.tsx"
)

// Environment variables overriding the embedded reference mockups.
const (
	// EnvModuleMockup points to a custom directory of a reference module to
	// copy (a hello-world style module whose components are renamed).
	EnvModuleMockup = "SENTIENT_MODULE_MOCKUP"
	// EnvPageMockup points to a custom `src/app/<name>/page.tsx` template used
	// when the module declaration declares a `uri`/`url`.
	EnvPageMockup = "SENTIENT_PAGE_MOCKUP"
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
		pkg.FileExists(filepath.Join(dir, config.ModuleEntryFileName))
}

// scaffoldFromMockup copies a reference module directory into `moduleDir` and
// rewrites every component/identifier carrying the mockup module name with the
// module spec (identifier for the naming).
func scaffoldFromMockup(mockup, moduleDir string, spec ModuleSpec) error {
	if err := pkg.CopyDir(mockup, moduleDir); err != nil {
		return fmt.Errorf("failed to copy module mockup: %w", err)
	}

	if err := renameAndRewriteTree(moduleDir, moduleReplacements(spec.ID)); err != nil {
		return err
	}
	return finishScaffold(moduleDir, spec)
}

// scaffoldEmbeddedModule writes the embedded reference mockup into `moduleDir`,
// renaming components and identifiers with the module spec and forcing the new
// module identity onto the metadata files.
func scaffoldEmbeddedModule(moduleDir string, spec ModuleSpec) error {
	repls := moduleReplacements(spec.ID)
	if err := fs.WalkDir(embeddedTemplates, embeddedModulePrefix, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(embeddedModulePrefix, path)
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

// moduleReplacements maps every spelling of the mockup module name used in the
// hello-world reference module onto its counterpart for `id` (the kebab-case
// module identifier).
func moduleReplacements(id string) []moduleRepl {
	return []moduleRepl{
		{old: "Hello World", new: displayName(id)},
		{old: "HelloWorld", new: pascalName(id)},
		{old: "helloWorld", new: camelName(id)},
		{old: "hello-world", new: id},
		{old: "HELLO_WORLD", new: upperSnake(id)},
		{old: "helloworld", new: lowerName(id)},
	}
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
	text = patchJSONField(text, "uri", "/"+spec.URL)
	text = patchJSONField(text, "url", "/"+spec.URL)

	return pkg.WriteString(manifestPath, text)
}

// patchDeclarationIdentity rewrites the module declaration (index.tsx) identity
// and menu fields of the scaffolded module.
func patchDeclarationIdentity(moduleDir string, spec ModuleSpec) error {
	indexPath := filepath.Join(moduleDir, config.ModuleEntryFileName)
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
	text = patchJSField(text, "uri", "/"+spec.URL)
	text = patchJSField(text, "url", "/"+spec.URL)
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

// scaffoldPage generates `src/app/<url>/page.tsx` from a page mockup file.
// It returns the created page path ("" when the mockup is unavailable).
func scaffoldPage(root, moduleDir string, spec ModuleSpec, pageMockup string) string {
	uri := declaredURI(filepath.Join(moduleDir, config.ModuleEntryFileName))
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
	uri := declaredURI(filepath.Join(moduleDir, config.ModuleEntryFileName))
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
// (`@/external_modules/<domain>/...`).
func rewritePageBody(body string, spec ModuleSpec) string {
	body = applyReplacements(body, moduleReplacements(spec.ID))
	return strings.ReplaceAll(body, "external_modules/"+spec.ID+"/", "external_modules/"+spec.Domain+"/")
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
func mockupReadmeTemplate(spec ModuleSpec) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# %s\n\n", spec.AppName))
	if desc := spec.Description; desc != "" {
		b.WriteString(desc + "\n\n")
	}
	b.WriteString(fmt.Sprintf("Sentient module `%s` (`%s`).\n\n", spec.Domain, spec.ID))
	b.WriteString("## Structure\n\n")
	b.WriteString("- `manifest.json` — module metadata\n")
	b.WriteString("- `index.tsx` — module declaration (identifier, widgets, service, routines, uri)\n")
	b.WriteString("- `package.json` — module dependencies\n")
	b.WriteString("- `application/` — service layer\n")
	b.WriteString("- `domain/` — interfaces and enums\n")
	b.WriteString("- `infrastructure/` — routines\n")
	b.WriteString("- `presentation/` — views, widgets, components, providers\n")
	return b.String()
}
