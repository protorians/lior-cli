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
	if c.MockupDir != "" && isModuleMockup(c.MockupDir) {
		return c.MockupDir
	}
	if p := os.Getenv(EnvModuleMockup); p != "" && isModuleMockup(p) {
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

// isModuleMockup reports whether dir looks like a scofoldable module mockup.
func isModuleMockup(dir string) bool {
	return pkg.DirExists(dir) &&
		pkg.FileExists(filepath.Join(dir, config.ManifestFileName)) &&
		pkg.FileExists(filepath.Join(dir, config.ModuleEntryFileName))
}

// scaffoldFromMockup copies a reference module directory into `moduleDir` and
// rewrites every component/identifier carrying the mockup module name with
// `name`.
func scaffoldFromMockup(mockup, moduleDir, name, description string) error {
	if err := pkg.CopyDir(mockup, moduleDir); err != nil {
		return fmt.Errorf("failed to copy module mockup: %w", err)
	}

	if err := renameAndRewriteTree(moduleDir, moduleReplacements(name)); err != nil {
		return err
	}
	return finishScaffold(moduleDir, name, description)
}

// scaffoldEmbeddedModule writes the embedded reference mockup into `moduleDir`,
// renaming components and identifiers with `name` and forcing the new module
// identity onto the metadata files.
func scaffoldEmbeddedModule(moduleDir, name, description string) error {
	repls := moduleReplacements(name)
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
	return finishScaffold(moduleDir, name, description)
}

// finishScaffold applies the shared post-copy work: manifest identity, module
// descriptions and the README.
func finishScaffold(moduleDir, name, description string) error {
	if err := patchManifestIdentity(moduleDir, name, description); err != nil {
		return err
	}
	if err := patchDeclarationDescriptions(moduleDir, name, description); err != nil {
		return err
	}
	if err := pkg.WriteString(filepath.Join(moduleDir, "README.md"), mockupReadmeTemplate(name, description)); err != nil {
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
// hello-world reference module onto its counterpart for `name`.
func moduleReplacements(name string) []moduleRepl {
	return []moduleRepl{
		{old: "Hello World", new: displayName(name)},
		{old: "HelloWorld", new: pascalName(name)},
		{old: "helloWorld", new: camelName(name)},
		{old: "hello-world", new: name},
		{old: "HELLO_WORLD", new: upperSnake(name)},
		{old: "helloworld", new: lowerName(name)},
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

// effectiveDescription returns the description to write (trimmed). An empty
// description is kept empty so the metadata stays "absent" for a later link.
func effectiveDescription(name, description string) string {
	return description
}

var (
	manifestIDRe      = regexp.MustCompile(`("id"\s*:\s*"[^"]*")`)
	jsonDescriptionRe = regexp.MustCompile(`("description"\s*:\s*)"[^"]*"`)
	tsDescriptionLine = regexp.MustCompile(`(?m)^(\s*)(description:).*$`)
	manifestTokenRe   = regexp.MustCompile(`"token":`)
	declaredURIAttrRe = regexp.MustCompile(`(?m)^\s*(?:uri|url)\s*[:=]\s*['"]([^'"]+)['"]`)
)

// patchManifestIdentity ensures the scaffolded manifest.json carries the new
// module identity: a unique UUID token (the mockup has none) and the provided
// description (the mockup description is demo-specific).
func patchManifestIdentity(moduleDir, name, description string) error {
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

	if desc, err := json.Marshal(effectiveDescription(name, description)); err == nil {
		text = jsonDescriptionRe.ReplaceAllString(text, "${1}"+string(desc))
	}

	return pkg.WriteString(manifestPath, text)
}

// patchDeclarationDescriptions rewrites the demo description of the module
// declaration (index.tsx) and package.json with the provided description.
func patchDeclarationDescriptions(moduleDir, name, description string) error {
	desc := effectiveDescription(name, description)

	indexPath := filepath.Join(moduleDir, config.ModuleEntryFileName)
	if data, err := os.ReadFile(indexPath); err == nil {
		escaped := strings.ReplaceAll(desc, "'", `\'`)
		updated := tsDescriptionLine.ReplaceAllString(string(data), "${1}${2} '"+escaped+"',")
		if err := os.WriteFile(indexPath, []byte(updated), 0o644); err != nil {
			return fmt.Errorf("failed to update %s: %w", indexPath, err)
		}
	}

	packagePath := filepath.Join(moduleDir, "package.json")
	if pkg.FileExists(packagePath) {
		data, err := os.ReadFile(packagePath)
		if err != nil {
			return fmt.Errorf("failed to read scaffolded package.json: %w", err)
		}
		descLit, _ := json.Marshal(desc)
		updated := jsonDescriptionRe.ReplaceAllString(string(data), "${1}"+string(descLit))
		if err := os.WriteFile(packagePath, []byte(updated), 0o644); err != nil {
			return fmt.Errorf("failed to update package.json: %w", err)
		}
	}
	return nil
}

// scaffoldPage generates `src/app/<uri>/page.tsx` from a page mockup file when
// the module declaration declares a `uri`/`url`. It returns the created page
// path ("" when the declaration has no url or the mockup is unavailable).
func scaffoldPage(root, moduleDir, name, pageMockup string) string {
	uri := declaredURI(filepath.Join(moduleDir, config.ModuleEntryFileName))
	if uri == "" {
		return ""
	}

	data, err := os.ReadFile(pageMockup)
	if err != nil {
		return ""
	}
	return writeScaffoldedPage(root, name, uri, applyReplacements(string(data), moduleReplacements(name)))
}

// scaffoldEmbeddedPage generates `src/app/<uri>/page.tsx` from the embedded page
// mockup, when the module declaration declares a `uri`/`url`.
func scaffoldEmbeddedPage(root, moduleDir, name string) string {
	uri := declaredURI(filepath.Join(moduleDir, config.ModuleEntryFileName))
	if uri == "" {
		return ""
	}

	data, err := embeddedTemplates.ReadFile(embeddedPageFile)
	if err != nil {
		return ""
	}
	return writeScaffoldedPage(root, name, uri, applyReplacements(string(data), moduleReplacements(name)))
}

// writeScaffoldedPage writes a page body at `src/app/<uri>/page.tsx`.
func writeScaffoldedPage(root, name, uri, body string) string {
	pageDir := strings.Trim(uri, "/")
	if pageDir == "" {
		pageDir = name
	}
	pagePath := filepath.Join(root, config.AppSrcDir, pageDir, "page.tsx")
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
func mockupReadmeTemplate(name, description string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# %s\n\n", displayName(name)))
	if desc := effectiveDescription(name, description); desc != "" {
		b.WriteString(desc + "\n\n")
	}
	b.WriteString(fmt.Sprintf("Sentient module `%s`.\n\n", name))
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
