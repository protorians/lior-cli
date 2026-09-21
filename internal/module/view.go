package module

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
)

// ViewFileName is the file name suffix of a module view.
const ViewFileName = ".view.tsx"

// ViewComponentName returns the PascalCase React component name of a kebab-case
// view identifier (e.g. "user-profile" -> "UserProfile").
func ViewComponentName(name string) string {
	return pascalName(name)
}

// ViewSpec is the set of values used to scaffold a new presentation view inside
// an existing module.
type ViewSpec struct {
	// Module is the module folder name (its domain) under library/modules/.
	Module string
	// Name is the kebab-case identifier of the view (e.g. user-profile).
	Name string
	// Label is the human-readable title displayed by the view (optional).
	// Empty falls back to the Title Case form of Name.
	Label string
	// Description is the subtitle displayed by the view (optional).
	Description string
}

// EffectiveLabel returns the view title, falling back to the Title Case form of
// the view identifier.
func (s ViewSpec) EffectiveLabel() string {
	if l := strings.TrimSpace(s.Label); l != "" {
		return l
	}
	return displayName(s.Name)
}

// ViewCreator scaffolds a new view into an existing module's presentation
// layer.
type ViewCreator struct {
	// Root is the project root containing library/modules/.
	Root string
	// MockupDir optionally points to a reference view file to copy and rename.
	// When empty, the embedded view mockup is used.
	MockupDir string
}

// ViewResult summarises a view creation.
type ViewResult struct {
	// Module is the module folder name (domain) the view was scaffolded into.
	Module string
	// Name is the kebab-case view identifier.
	Name string
	// Path is the created `<name>.view.tsx` file.
	Path string
}

// viewExistsError reports that a view with the same name already exists.
type viewExistsError struct {
	view string
	path string
}

func (e *viewExistsError) Error() string {
	return i18n.Tf("view.error.exists", e.view, e.path)
}

// IsViewExistsError reports whether err is a "view already exists" error.
func IsViewExistsError(err error) bool {
	var ee *viewExistsError
	return errors.As(err, &ee)
}

// viewMockupSource returns the custom view mockup file (ViewCreator field
// first, then environment), or "" to use the embedded one.
func (c *ViewCreator) viewMockupSource() string {
	if c.MockupDir != "" && pkg.FileExists(c.MockupDir) {
		return c.MockupDir
	}
	if p := os.Getenv(EnvViewMockup); p != "" && pkg.FileExists(p) {
		return p
	}
	return ""
}

// Create scaffolds a `<name>.view.tsx` inside
// `library/modules/<module>/presentation/views/` from the reference view
// mockup, renaming the component (`HelloWorldView` → `<Name>View`) and its
// title/description.
func (c *ViewCreator) Create(spec ViewSpec) (*ViewResult, error) {
	spec.Module = strings.TrimSpace(spec.Module)
	spec.Name = strings.TrimSpace(spec.Name)
	spec.Label = strings.TrimSpace(spec.Label)
	spec.Description = strings.TrimSpace(spec.Description)

	if spec.Module == "" {
		return nil, errors.New(i18n.T("view.error.no_module"))
	}
	if err := ValidateName(spec.Name); err != nil {
		return nil, err
	}

	moduleDir := filepath.Join(c.Root, config.ExternalModulesDir, spec.Module)
	if !pkg.DirExists(moduleDir) {
		return nil, errors.New(i18n.Tf("view.error.module_missing", spec.Module))
	}

	viewsDir := filepath.Join(moduleDir, "presentation", "views")
	viewPath := filepath.Join(viewsDir, spec.Name+ViewFileName)
	if pkg.FileExists(viewPath) {
		return nil, &viewExistsError{view: spec.Name, path: viewPath}
	}

	body, err := c.viewMockupBody()
	if err != nil {
		return nil, err
	}
	body = rewriteViewBody(body, spec)

	if err := pkg.WriteString(viewPath, body); err != nil {
		return nil, err
	}

	return &ViewResult{Module: spec.Module, Name: spec.Name, Path: viewPath}, nil
}

// viewMockupBody reads the reference view mockup (custom then embedded).
func (c *ViewCreator) viewMockupBody() (string, error) {
	if src := c.viewMockupSource(); src != "" {
		data, err := os.ReadFile(src)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	data, err := embeddedTemplates.ReadFile(embeddedViewFile)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// rewriteViewBody renames the mockup component and its displayed title and
// description with the view spec. The mockup module name (hello-world) is
// replaced with the view identifier so shared components keep consistent names.
func rewriteViewBody(body string, spec ViewSpec) string {
	repls := []moduleRepl{
		{old: "Hello World", new: spec.EffectiveLabel()},
		{old: "HelloWorld", new: pascalName(spec.Name)},
		{old: "helloWorld", new: camelName(spec.Name)},
		{old: "hello-world", new: spec.Name},
		{old: "HELLO_WORLD", new: upperSnake(spec.Name)},
		{old: "helloworld", new: lowerName(spec.Name)},
		{old: "Module d'exemple pour l'onboarding", new: spec.Description},
	}
	return applyReplacements(body, repls)
}
