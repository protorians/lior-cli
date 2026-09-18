package audit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/protorians/liorian-cli/internal/config"
	"github.com/protorians/liorian-cli/internal/i18n"
	"github.com/protorians/liorian-cli/internal/module"
	"github.com/protorians/liorian-cli/internal/pkg"
)

// jsxTagRE heuristically recognises JSX elements without a full TS/JSX parser:
// closing tags (`</div>`), custom (capitalised) components that are
// self-closing (`<Foo …/>`) or carry content/attributes (`<Foo …>`), and
// lowercase native elements carrying attributes (`<div className=…/>`). It
// deliberately ignores type arguments such as `Array<string>`, generic chains
// such as `this.get<FetchResponse<Item>>(...)`, and capitalised type
// references used inside generic chains (`<Item>>`).
var jsxTagRE = regexp.MustCompile(`(?:</[A-Z][A-Za-z0-9._-]*>|</[a-z][a-z0-9_-]*>|<[A-Z][A-Za-z0-9._-]*(?:\s[^<>]*?)?/>|<[A-Z][A-Za-z0-9._-]*(?:\s[^<>]*?)>|<[a-z][a-z0-9_-]*(?:\s+[a-zA-Z-]+=)[^<>]*?/?>)`)

// Auditor runs all conformance checks on a module.
type Auditor struct {
	Root string
}

// AuditResult aggregates audit findings for one or more modules.
type AuditResult struct {
	Modules []module.Result `json:"modules"`
}

// TotalErrors returns the total error count across all modules.
func (ar *AuditResult) TotalErrors() int {
	n := 0
	for _, m := range ar.Modules {
		n += m.ErrorCount()
	}
	return n
}

// TotalWarnings returns the total warning count across all modules.
func (ar *AuditResult) TotalWarnings() int {
	n := 0
	for _, m := range ar.Modules {
		n += m.WarningCount()
	}
	return n
}

// AuditModules audits one or all modules. When name is empty, all modules
// in external_modules/ are audited.
func (a *Auditor) AuditModules(name string) (*AuditResult, error) {
	if name != "" {
		return a.auditSingle(name)
	}
	return a.auditAll()
}

func (a *Auditor) auditSingle(name string) (*AuditResult, error) {
	moduleDir := filepath.Join(a.Root, config.ExternalModulesDir, name)
	if !pkg.DirExists(moduleDir) {
		return nil, errors.New(i18n.Tf("val.module_not_found", name, config.ExternalModulesDir))
	}
	res, err := a.auditModule(name)
	if err != nil {
		return nil, err
	}
	return &AuditResult{Modules: []module.Result{*res}}, nil
}

func (a *Auditor) auditAll() (*AuditResult, error) {
	dir := filepath.Join(a.Root, config.ExternalModulesDir)
	if !pkg.DirExists(dir) {
		return nil, errors.New(i18n.Tf("modules.error.dir", config.ExternalModulesDir))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", config.ExternalModulesDir, err)
	}

	result := &AuditResult{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !pkg.FileExists(filepath.Join(dir, e.Name(), config.ManifestFileName)) {
			continue
		}
		res, err := a.auditModule(e.Name())
		if err != nil {
			return nil, err
		}
		result.Modules = append(result.Modules, *res)
	}
	return result, nil
}

// auditModule runs the full audit pipeline on a single module.
func (a *Auditor) auditModule(name string) (*module.Result, error) {
	v := &module.Validator{Root: a.Root}
	res, err := v.ValidateModule(name)
	if err != nil {
		return nil, err
	}

	moduleDir := filepath.Join(a.Root, config.ExternalModulesDir, name)
	indexPath := filepath.Join(moduleDir, config.ModuleEntryFileName)

	a.auditArchitecture(moduleDir, indexPath, res)
	a.auditDependencies(name, res)
	a.auditAssets(name, res)

	return res, nil
}

// auditArchitecture checks Clean Architecture rules.
func (a *Auditor) auditArchitecture(moduleDir, indexPath string, res *module.Result) {
	// Check: components don't import services directly (legacy `components/`
	// layout). The canonical layout lets presentation/ use application/service,
	// so bare capitalised service imports there are legitimate.
	a.auditComponentsImportServices(moduleDir, "components", res)

	// Check: services don't contain JSX (legacy `services/` layout and the
	// canonical application/service/, which is plain data-access TS).
	for _, dir := range []string{"services", filepath.Join("application", "service")} {
		a.auditServicesHaveNoJSX(moduleDir, dir, res)
	}

	// Check: index.tsx declares a module (identifier + widgets). The canonical
	// declaration is declarative (ModuleDeclarationInterface: widgets, service,
	// routines, providers) — the legacy async `render` field no longer exists.
	if pkg.FileExists(indexPath) {
		data, err := os.ReadFile(indexPath)
		if err == nil {
			content := string(data)
			hasIdentifier := strings.Contains(content, "identifier:")
			hasWidgets := strings.Contains(content, "widgets:")
			addLevel(res, "index.tsx", "declaration", hasIdentifier && hasWidgets,
				"module declaration present (identifier, widgets)", module.LevelError)
		}
	}
}

// auditComponentsImportServices scans a component directory for direct service
// imports.
func (a *Auditor) auditComponentsImportServices(moduleDir, dir string, res *module.Result) {
	componentsDir := filepath.Join(moduleDir, dir)
	if !pkg.DirExists(componentsDir) {
		return
	}
	_ = filepath.Walk(componentsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(data)
		if strings.Contains(content, "from \"../services") || strings.Contains(content, "from '../services") ||
			strings.Contains(content, "from \"./services") || strings.Contains(content, "from './services") ||
			strings.Contains(content, "from \"../../application/service") || strings.Contains(content, "from '../../application/service") {
			rel, _ := filepath.Rel(moduleDir, path)
			res.Findings = append(res.Findings, module.Finding{
				Category: "Clean Architecture",
				Rule:     "components→services",
				Severity: module.LevelError,
				Message:  fmt.Sprintf("%s imports services directly", rel),
			})
		}
		return nil
	})
}

// auditServicesHaveNoJSX scans a service directory for JSX content.
func (a *Auditor) auditServicesHaveNoJSX(moduleDir, dir string, res *module.Result) {
	servicesDir := filepath.Join(moduleDir, dir)
	if !pkg.DirExists(servicesDir) {
		return
	}
	_ = filepath.Walk(servicesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".ts") && !strings.HasSuffix(path, ".tsx") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if jsxTagRE.Match(data) {
			rel, _ := filepath.Rel(moduleDir, path)
			res.Findings = append(res.Findings, module.Finding{
				Category: "Clean Architecture",
				Rule:     "services→JSX",
				Severity: module.LevelError,
				Message:  fmt.Sprintf("%s contains JSX in a service", rel),
			})
		}
		return nil
	})
}

// auditDependencies checks that listed requirements and npm deps exist.
func (a *Auditor) auditDependencies(name string, res *module.Result) {
	manifest, err := module.LoadManifest(config.ManifestPath(a.Root, name))
	if err != nil {
		return
	}

	// Check requirements (external_modules/ or internal src/modules/;
	// platform core modules are always satisfied).
	for req := range manifest.Requirements {
		addLevel(res, "requirements", req, module.RequirementSatisfied(a.Root, req),
			fmt.Sprintf("requirement %q exists", req), module.LevelError)
	}

	// Check that listed npm dependencies are installed (spec rule
	// « Toutes les dépendances npm sont installées »). The previous duplicate
	// check iterated a map — duplicate keys are impossible by construction, so
	// it could never trigger. Installed-ness is the real, verifiable rule.
	for dep := range manifest.Dependencies {
		depDir := filepath.Join(a.Root, "node_modules", filepath.FromSlash(dep))
		addLevel(res, "dependencies", dep, pkg.DirExists(depDir),
			fmt.Sprintf("dependency %q installed", dep), module.LevelError)
	}
}

// auditAssets checks that assets referenced by the module exist.
func (a *Auditor) auditAssets(name string, res *module.Result) {
	assetsDir := config.ModuleAssetsDir(a.Root, name)
	if pkg.DirExists(assetsDir) {
		// Assets directory exists — check for files
		hasContent := false
		_ = filepath.Walk(assetsDir, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				hasContent = true
				return filepath.SkipDir
			}
			return nil
		})
		addLevel(res, "assets", "files", hasContent,
			"assets contain files", module.LevelWarning)
	}
}

func addLevel(res *module.Result, category, rule string, ok bool, okMsg string, failSev string) {
	sev := module.LevelOK
	msg := okMsg
	if !ok {
		sev = failSev
		msg = rule + " not compliant"
	}
	res.Findings = append(res.Findings, module.Finding{
		Category: category,
		Rule:     rule,
		Severity: sev,
		Message:  msg,
	})
}
