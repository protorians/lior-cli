package module

import (
	"errors"
	"path/filepath"

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
