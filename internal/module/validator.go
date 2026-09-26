package module

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
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

// Validator validates a module against the Liora rules.
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

// OKCount returns the number of OK findings.
func (r *Result) OKCount() int {
	return count(r.Findings, LevelOK)
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
	// domain format warning (spec §5.10): any reverse-DNS dotted domain is
	// accepted, e.g. com.organization.domain (the former mod.liorian.<name>
	// prefix is no longer required).
	addLevel(res, "manifest.json", "domain", isDomainName(manifest.Domain),
		"domain in reverse-DNS format", LevelWarning)
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
	// canonical compatibility (`compatibility.{socle,api}`) preferred; the
	// legacy `managerCompatibility` / `apiCompatibility` windows are still
	// accepted (migration warning).
	if hasCanonicalCompatibility(manifestPath) {
		addLevel(res, "manifest.json", "compatibility", compatibilityRangeComplete(manifest.EffectiveSocle()) && compatibilityRangeComplete(manifest.EffectiveAPI()),
			"compatibility range present and complete", LevelWarning)
	} else {
		addLevel(res, "manifest.json", "managerCompatibility", compatibilityComplete(manifest.ManagerCompat),
			"managerCompatibility range present and complete (legacy: migrate to compatibility.socle)", LevelWarning)
		addLevel(res, "manifest.json", "apiCompatibility", compatibilityComplete(manifest.APICompat),
			"apiCompatibility range present and complete (legacy: migrate to compatibility.api)", LevelWarning)
	}
	// canonical OAuth scopes preferred; legacy `apiScopes` still accepted.
	if rawHasKey(manifestPath, "oauth") {
		addLevel(res, "manifest.json", "oauth.scopes", oauthScopesValid(manifest.EffectiveOAuthScopes()),
			"oauth.scopes within the authorized catalogue", LevelWarning)
	} else if rawHasKey(manifestPath, "apiScopes") {
		addLevel(res, "manifest.json", "apiScopes",
			true, "apiScopes present (legacy: migrate to oauth.scopes)", LevelWarning)
	} else {
		addLevel(res, "manifest.json", "oauth.scopes", false,
			"oauth.scopes present", LevelWarning)
	}
	// capabilities present (canonical Tauri permission ids; the legacy
	// boolean object is normalized on load).
	addLevel(res, "manifest.json", "capabilities", rawHasKey(manifestPath, "capabilities"),
		"capabilities present", LevelWarning)
	// permissions: canonical `Role:Verbe` codes; legacy dotted scopes trigger
	// a migration warning (the installation review requires canonical codes).
	addLevel(res, "manifest.json", "permissions", permissionsCanonical(manifest.Permissions),
		"permissions use canonical Role:Verbe codes", LevelWarning)
	// distribution type: canonical enum; legacy INTERNAL/EXTERNAL accepted
	// with a migration warning.
	addLevel(res, "manifest.json", "type",
		manifest.Type == "" || ModuleTypes[strings.ToUpper(manifest.Type)],
		"type in the allowed enum", LevelWarning)
	if IsLegacyModuleType(manifest.Type) {
		addLevel(res, "manifest.json", "type", false,
			"type uses the canonical ModuleType enum (legacy INTERNAL/EXTERNAL)", LevelWarning)
	}
	// category within the ModuleCategory enum (optional field)
	addLevel(res, "manifest.json", "category",
		manifest.Category == "" || ModuleCategories[strings.ToUpper(manifest.Category)],
		"category in the allowed enum", LevelWarning)
	// publisher: optional in the canonical schema (completed at publish
	// time); a half-filled publisher block is a warning.
	if rawHasKey(manifestPath, "publisher") {
		addLevel(res, "manifest.json", "publisher", manifest.Publisher.ID != "" && manifest.Publisher.Name != "",
			"publisher complete (id and name)", LevelWarning)
	}
	// domain: canonical `mod.<éditeur>.<module>` preferred (spec
	// module-installation §4.3); any reverse-DNS form stays accepted.
	addLevel(res, "manifest.json", "canonical domain", IsCanonicalDomain(manifest.Domain),
		"domain in canonical mod.<éditeur>.<module> form", LevelWarning)
	// CONFIGURATION modules carry their UI in the manifest itself
	// (`entry: index.json` + `dataModel`/`declarative`): no React entry needed.
	if strings.EqualFold(strings.TrimSpace(manifest.Type), "CONFIGURATION") {
		addLevel(res, "manifest.json", "entry", manifest.Entry == "index.json",
			"CONFIGURATION entry is index.json", LevelWarning)
		addLevel(res, "manifest.json", "dataModel",
			manifest.DataModel != nil,
			"CONFIGURATION dataModel present", LevelWarning)
	} else {
		// entry default export present in index.tsx
		indexPath := filepath.Join(moduleDir, config.ModuleEntryFileName)
		addLevel(res, "index.tsx", "export", pkg.FileExists(indexPath) && containsDefaultExport(indexPath),
			"index.tsx file with default export", LevelError)
	}

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

func isSemver(v string) bool {
	return semverRE.MatchString(v)
}

// isDomainName reports whether s is a valid reverse-DNS dotted domain
// (e.g. com.organization.domain), the accepted manifest domain form.
func isDomainName(s string) bool {
	return domainRE.MatchString(s)
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

// compatibilityRangeComplete is the canonical-form counterpart of
// compatibilityComplete (see ModuleCompatibility).
func compatibilityRangeComplete(c CompatibilityRange) bool {
	if strings.TrimSpace(c.Min) == "" {
		return false
	}
	if c.Max != "" && exactVersionRE.MatchString(c.Max) {
		return false
	}
	return true
}

// hasCanonicalCompatibility reports whether a manifest declares the canonical
// `compatibility` object (raw JSON inspection: the typed struct cannot tell an
// absent object from a zero value).
func hasCanonicalCompatibility(manifestPath string) bool {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return false
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return false
	}
	_, ok := raw["compatibility"]
	return ok
}

// OAuthScopes is the catalogue of OAuth scopes the server may grant
// (`docs/modules/module-manifest.md` §6.5).
var OAuthScopes = map[string]bool{
	"openid": true, "profile": true, "email": true,
	"organizations": true, "roles": true, "permissions": true,
}

// oauthScopesValid reports whether every scope belongs to the catalogue
// authorized by the OAuth server.
func oauthScopesValid(scopes []string) bool {
	if scopes == nil {
		return false
	}
	for _, s := range scopes {
		if !OAuthScopes[strings.TrimSpace(s)] {
			return false
		}
	}
	return true
}

// permissionsCanonical reports whether every permission entry uses the
// canonical `Role:Verbe` form. An empty list is accepted here (publish-time
// review covers the security semantics); only malformed entries warn.
func permissionsCanonical(permissions []string) bool {
	for _, p := range permissions {
		if !IsPermissionCode(p) {
			return false
		}
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
