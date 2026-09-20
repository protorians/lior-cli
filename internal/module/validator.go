package module

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jetbrains/lior-cli/internal/config"
	"github.com/jetbrains/lior-cli/internal/i18n"
	"github.com/jetbrains/lior-cli/internal/pkg"
)

// Severity levels for validation findings.
const (
	LevelError   = "ERROR"
	LevelWarning = "WARNING"
	LevelOK      = "OK"
)

// Finding is a single validation result.
type Finding struct {
	Category string `json:"category"`
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// Validator validates a module against the Liorian rules.
type Validator struct {
	Root string
}

// Result aggregates validation findings for one module.
type Result struct {
	Module   string    `json:"module"`
	Findings []Finding `json:"findings"`
}

// HasErrors reports whether any ERROR finding exists.
func (r *Result) HasErrors() bool {
	for _, f := range r.Findings {
		if f.Severity == LevelError {
			return true
		}
	}
	return false
}

// ErrorCount returns the number of ERROR findings.
func (r *Result) ErrorCount() int {
	return count(r.Findings, LevelError)
}

// WarningCount returns the number of WARNING findings.
func (r *Result) WarningCount() int {
	return count(r.Findings, LevelWarning)
}

func count(findings []Finding, level string) int {
	n := 0
	for _, f := range findings {
		if f.Severity == level {
			n++
		}
	}
	return n
}

// ValidateModule checks the core requirements of a single module.
// Used by `pack` and shared by the audit pipeline.
func (v *Validator) ValidateModule(name string) (*Result, error) {
	moduleDir := filepath.Join(v.Root, config.ExternalModulesDir, name)
	if !pkg.DirExists(moduleDir) {
		return nil, errors.New(i18n.Tf("val.module_not_found", name, moduleDir))
	}

	res := &Result{Module: name}

	manifestPath := filepath.Join(moduleDir, config.ManifestFileName)
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		res.Findings = append(res.Findings, Finding{
			Category: "manifest.json", Rule: "lecture",
			Severity: LevelError, Message: err.Error(),
		})
		return res, nil
	}

	// id
	add(res, "manifest.json", "id", manifest.ID != "", "id present")
	// name
	add(res, "manifest.json", "name", manifest.Name != "", "name present")
	// version (semver)
	add(res, "manifest.json", "version", isSemver(manifest.Version), "valid SemVer version")
	// token (UUID)
	add(res, "manifest.json", "token", pkg.IsUUID(manifest.Token), "valid UUID token")
	// entry exists
	entryPath := filepath.Join(moduleDir, manifest.Entry)
	add(res, "manifest.json", "entry", pkg.FileExists(entryPath), "entry file present")
	// domain format warning (spec §5.10: expected mod.liorian.<name>)
	addLevel(res, "manifest.json", "domain", isLiorianDomain(manifest.Domain),
		"domain in mod.liorian.<name> format", LevelWarning)
	// the module directory must be named after its domain
	addLevel(res, "manifest.json", "domain directory", manifest.Domain == name,
		"manifest domain matches the module directory (library/modules/<domain>)", LevelWarning)
	// permissions must be an array (spec rule, WARNING severity)
	addLevel(res, "manifest.json", "permissions", rawPermissionsIsArray(manifestPath),
		"permissions is an array", LevelWarning)
	// optionalRequirements present (schema-required, WARNING for legacy projects)
	addLevel(res, "manifest.json", "optionalRequirements", manifest.OptionalRequirements != nil,
		"optionalRequirements present", LevelWarning)
	// platforms present, with modes for every supported platform
	addLevel(res, "manifest.json", "platforms",
		rawHasKey(manifestPath, "platforms") && platformsHaveModes(manifest.Platforms),
		"platforms present with modes for supported platforms", LevelWarning)
	// compatibility windows present and complete (`0.17.x`, not `0.17.0`)
	addLevel(res, "manifest.json", "managerCompatibility", compatibilityComplete(manifest.ManagerCompat),
		"managerCompatibility range present and complete", LevelWarning)
	addLevel(res, "manifest.json", "apiCompatibility", compatibilityComplete(manifest.APICompat),
		"apiCompatibility range present and complete", LevelWarning)
	// capabilities present (schema-required)
	addLevel(res, "manifest.json", "capabilities", rawHasKey(manifestPath, "capabilities"),
		"capabilities present", LevelWarning)
	// category within the ModuleCategory enum (optional field)
	addLevel(res, "manifest.json", "category",
		manifest.Category == "" || ModuleCategories[strings.ToUpper(manifest.Category)],
		"category in the allowed enum", LevelWarning)
	// publisher present (schema-required)
	addLevel(res, "manifest.json", "publisher", manifest.Publisher.ID != "" && manifest.Publisher.Name != "",
		"publisher present", LevelWarning)
	// entry default export present in index.tsx
	indexPath := filepath.Join(moduleDir, config.ModuleEntryFileName)
	addLevel(res, "index.tsx", "export", pkg.FileExists(indexPath) && containsDefaultExport(indexPath),
		"index.tsx file with default export", LevelError)

	return res, nil
}

func add(res *Result, category, rule string, ok bool, okMsg string) {
	sev := LevelOK
	msg := okMsg
	if !ok {
		sev = LevelError
		msg = rule + " invalid"
	}
	res.Findings = append(res.Findings, Finding{Category: category, Rule: rule, Severity: sev, Message: msg})
}

func addLevel(res *Result, category, rule string, ok bool, okMsg string, warningSev string) {
	sev := LevelOK
	msg := okMsg
	if !ok {
		sev = warningSev
		msg = rule + " not compliant"
	}
	res.Findings = append(res.Findings, Finding{Category: category, Rule: rule, Severity: sev, Message: msg})
}

// semverRE matches a strict SemVer 2.0.0 version (leading `v` tolerated), as
// used by the manifest `version` field (based on semver.org grammar).
var semverRE = regexp.MustCompile(`^v?(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

// isLiorianDomain reports whether the domain matches the expected
// mod.liorian.<name> format generated by `liorian create module` (spec §5.10).
func isLiorianDomain(domain string) bool {
	if !strings.HasPrefix(domain, "mod.liorian.") {
		return false
	}
	suffix := strings.TrimPrefix(domain, "mod.liorian.")
	return suffix != "" && kebabNameRE.MatchString(suffix)
}

func isSemver(v string) bool {
	return semverRE.MatchString(v)
}

// platformsHaveModes reports whether every supported platform declares at
// least one execution mode (schema rule `modes` required when `supported`).
func platformsHaveModes(p Platforms) bool {
	for _, platform := range []Platform{p.Web, p.Desktop, p.Mobile} {
		if platform.Supported && len(platform.Modes) == 0 {
			return false
		}
	}
	return true
}

// exactVersionRE matches a fully-qualified SemVer (no `x` wildcard).
var exactVersionRE = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// compatibilityComplete reports whether a compatibility window declares a min
// and, when a max is present, uses a complete range (`0.17.x` rather than an
// incomplete `0.17.0`).
func compatibilityComplete(c Compatibility) bool {
	if strings.TrimSpace(c.Min) == "" {
		return false
	}
	if c.Max != "" && exactVersionRE.MatchString(c.Max) {
		return false
	}
	return true
}

// rawHasKey reports whether a JSON file declares a top-level key. The typed
// struct cannot distinguish an absent field from a zero value.
func rawHasKey(path, key string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return false
	}
	_, ok := raw[key]
	return ok
}

var defaultExportRE = regexp.MustCompile(`(?m)^\s*export\s+default\s+(?:async\s+)?(?:function[\s\w]*|\{(?:[^}]*\})?|\([^)]*\)\s*=>|class\s+\w+|[A-Za-z_$][\w$]*)`)

func containsDefaultExport(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return defaultExportRE.Match(data)
}

// rawPermissionsIsArray reports whether the `permissions` field of a manifest
// is an array. The typed struct (`[]string`) cannot represent a malformed
// manifest, so the raw JSON is inspected instead (spec §5.10, WARNING rule).
func rawPermissionsIsArray(manifestPath string) bool {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var raw map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return false
	}
	perm, ok := raw["permissions"]
	if !ok {
		return false
	}
	var arr []json.RawMessage
	return json.Unmarshal(perm, &arr) == nil
}
