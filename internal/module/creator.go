package module

import (
	"errors"
	"path/filepath"
	"sort"

	"github.com/protorians/sentient-cli/internal/config"
	"github.com/protorians/sentient-cli/internal/i18n"
	"github.com/protorians/sentient-cli/internal/pkg"
)

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
	Name     string
	Dir      string
	Token    string
	Manifest *Manifest
	// Page is the optional `src/app/<name>/page.tsx` path scaffolded from the
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

// ModuleExists reports whether a module `name` is available locally, either as
// an external module in `external_modules/` or as an internal module in
// `src/modules/`.
func ModuleExists(root, name string) bool {
	if pkg.DirExists(config.ModuleDir(root, name)) {
		return true
	}
	return pkg.DirExists(filepath.Join(root, config.InternalModulesDir, name))
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

// Create generates a module named `name` inside `external_modules/` by copying
// the reference mockup (custom or embedded) and renaming its components and
// information with the module name. When the module declaration declares a
// `uri`/`url`, a matching `src/app/<uri>/page.tsx` is scaffolded from the page
// mockup.
func (c *Creator) Create(name, description string) (*CreateResult, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}

	moduleDir := filepath.Join(c.Root, config.ExternalModulesDir, name)
	if pkg.DirExists(moduleDir) {
		return nil, &moduleExistsError{module: name, dir: moduleDir}
	}

	var err error
	if src := c.moduleMockupSource(); src != "" {
		err = scaffoldFromMockup(src, moduleDir, name, description)
	} else {
		err = scaffoldEmbeddedModule(moduleDir, name, description)
	}
	if err != nil {
		return nil, err
	}

	page := ""
	if src := c.pageMockupSource(); src != "" {
		page = scaffoldPage(c.Root, moduleDir, name, src)
	} else {
		page = scaffoldEmbeddedPage(c.Root, moduleDir, name)
	}

	manifest, err := LoadManifest(filepath.Join(moduleDir, config.ManifestFileName))
	if err != nil {
		return nil, err
	}

	return &CreateResult{
		Name:     name,
		Dir:      moduleDir,
		Token:    manifest.Token,
		Manifest: manifest,
		Page:     page,
	}, nil
}
