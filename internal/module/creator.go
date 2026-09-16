package module

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/protorians/sentient-cli/internal/config"
	"github.com/protorians/sentient-cli/internal/i18n"
	"github.com/protorians/sentient-cli/internal/pkg"
)

// ModuleSpec is the set of identity and metadata values used to scaffold a new
// module.
type ModuleSpec struct {
	// Domain is the reverse-DNS dotted name of the module
	// (e.g. com.organization.domain). It names the module directory under
	// external_modules/ and public/assets/.
	Domain string
	// ID is the kebab-case identifier of the module (e.g. hello-world).
	ID string
	// AppName is the display name of the application (manifest `name`).
	AppName string
	// Description is the module description (optional).
	Description string
	// Version is the module version; empty defaults to 0.0.0.
	Version string
	// Icon is the lucide-react component name of the module icon (optional).
	Icon string
	// URL is the page path segment in src/app/<url>; empty defaults to the
	// module identifier (manifest `uri` is /<url>).
	URL string
}

// EffectiveVersion returns the module version, defaulting to 0.0.0.
func (s ModuleSpec) EffectiveVersion() string {
	if v := strings.TrimSpace(s.Version); v != "" {
		return v
	}
	return "0.0.0"
}

// EffectiveURL returns the module page URL segment. An empty URL falls back to
// the module identifier.
func (s ModuleSpec) EffectiveURL() string {
	if u := strings.Trim(strings.TrimSpace(s.URL), "/"); u != "" {
		return u
	}
	return s.ID
}

// EffectiveAppName returns the application display name, falling back to the
// Title Case form of the identifier.
func (s ModuleSpec) EffectiveAppName() string {
	if n := strings.TrimSpace(s.AppName); n != "" {
		return n
	}
	return displayName(s.ID)
}

// Creator builds new modules by scaffolding them from a reference mockup.
type Creator struct {
	// Root is the project root containing external_modules/.
	Root string
	// MockupDir optionally points to a reference module (e.g. the hello-world
	// example) to copy and rename. When empty, the embedded mockup is used.
	MockupDir string
	// PageMockup optionally points to a `src/app/<name>/page.tsx` template used
	// when the module declaration declares a `uri`/`url`. When empty, the
	// embedded page mockup is used.
	PageMockup string
}

// CreateResult summarises a module creation.
type CreateResult struct {
	// Name is the module address on disk: its domain (external_modules/<name>).
	Name string
	ID   string
	// Domain is the reverse-DNS dotted name of the module.
	Domain string
	Dir    string
	Token  string
	// Manifest is the scaffolded manifest.json.
	Manifest *Manifest
	// Page is the optional `src/app/<url>/page.tsx` path scaffolded from the
	// page mockup when the module declaration declares a `uri`/`url`.
	Page string
}

// moduleExistsError reports that a module with the same name already exists.
type moduleExistsError struct {
	module string
	dir    string
}

func (e *moduleExistsError) Error() string {
	return i18n.Tf("module.error.exists", e.module, e.dir)
}

// IsExistsError reports whether err is an "module already exists" error.
func IsExistsError(err error) bool {
	var ee *moduleExistsError
	return errors.As(err, &ee)
}

// platformCoreModules are requirements provided by the workspace core (not
// necessarily as local modules): their presence is governed by the platform,
// so the local-existence check skips them.
var platformCoreModules = map[string]bool{
	"organization": true,
	"identity":     true,
}

// ModuleExists reports whether a module `ref` is available locally, either as
// an external module in `external_modules/` (matched by its folder domain or
// by its manifest id) or as an internal module in `src/modules/`.
func ModuleExists(root, ref string) bool {
	if pkg.DirExists(config.ModuleDir(root, ref)) {
		return true
	}

	// A module may be referenced by its manifest id rather than its folder
	// (domain): scan the external modules.
	dir := filepath.Join(root, config.ExternalModulesDir)
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if mkt, err := LoadManifest(filepath.Join(dir, e.Name(), config.ManifestFileName)); err == nil && mkt.ID == ref {
				return true
			}
		}
	}
	return pkg.DirExists(filepath.Join(root, config.InternalModulesDir, ref))
}

// RequirementSatisfied reports whether a requirement is provided: platform
// core modules always count, otherwise the module must exist locally
// (external_modules/ or src/modules/).
func RequirementSatisfied(root, req string) bool {
	if platformCoreModules[req] {
		return true
	}
	return ModuleExists(root, req)
}

// MissingRequirements returns the requirements declared in `manifest` that are
// not satisfied locally by an external (external_modules/) or an internal
// (src/modules/) module. Platform core modules are always considered
// satisfied.
func (c *Creator) MissingRequirements(manifest *Manifest) []string {
	missing := []string{}
	for req := range manifest.Requirements {
		if !RequirementSatisfied(c.Root, req) {
			missing = append(missing, req)
		}
	}
	sort.Strings(missing)
	return missing
}

// ResolveDependencies installs the module dependencies (dependencies +
// devDependencies) declared in the manifest by running the first available
// package manager (bun → pnpm → yarn → npm) in the project root. It returns
// the package manager name used, or "" (with no error) when none is available.
func (c *Creator) ResolveDependencies() (string, error) {
	pm := pkg.DetectPackageManager()
	if pm == "" {
		return "", nil
	}
	return pm, pkg.StreamCommandIn(c.Root, pm, "install")
}

// Create generates a module from the provided spec inside
// `external_modules/<domain>` by copying the reference mockup (custom or
// embedded) and renaming its components and information with the spec
// (identifier for components, domain for the directory and the declaration
// identifier). A matching `src/app/<url>/page.tsx` is scaffolded from the page
// mockup.
func (c *Creator) Create(spec ModuleSpec) (*CreateResult, error) {
	spec.Domain = strings.TrimSpace(spec.Domain)
	spec.ID = strings.TrimSpace(spec.ID)
	if err := ValidateName(spec.ID); err != nil {
		return nil, err
	}
	if err := ValidateDomain(spec.Domain); err != nil {
		return nil, err
	}
	if err := ValidateVersion(spec.Version); err != nil {
		return nil, err
	}
	if err := ValidateIcon(spec.Icon); err != nil {
		return nil, err
	}
	spec.Version = spec.EffectiveVersion()
	spec.URL = spec.EffectiveURL()
	spec.AppName = spec.EffectiveAppName()

	moduleDir := filepath.Join(c.Root, config.ExternalModulesDir, spec.Domain)
	if pkg.DirExists(moduleDir) {
		return nil, &moduleExistsError{module: spec.Domain, dir: moduleDir}
	}

	var err error
	if src := c.moduleMockupSource(); src != "" {
		err = scaffoldFromMockup(src, moduleDir, spec)
	} else {
		err = scaffoldEmbeddedModule(moduleDir, spec)
	}
	if err != nil {
		return nil, err
	}

	page := ""
	if src := c.pageMockupSource(); src != "" {
		page = scaffoldPage(c.Root, moduleDir, spec, src)
	} else {
		page = scaffoldEmbeddedPage(c.Root, moduleDir, spec)
	}

	manifest, err := LoadManifest(filepath.Join(moduleDir, config.ManifestFileName))
	if err != nil {
		return nil, err
	}

	return &CreateResult{
		Name:     spec.Domain,
		ID:       spec.ID,
		Domain:   spec.Domain,
		Dir:      moduleDir,
		Token:    manifest.Token,
		Manifest: manifest,
		Page:     page,
	}, nil
}
