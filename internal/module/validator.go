package module

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"

	"github.com/protorians/sentient-cli/internal/config"
	"github.com/protorians/sentient-cli/internal/i18n"
	"github.com/protorians/sentient-cli/internal/pkg"
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

// Validator validates a module against the Sentient rules.
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
	// domain format warning (canonical format uses concatenated lower name)
	expectedDomain := "mod.sentients." + lowerName(name)
	addLevel(res, "manifest.json", "domain", manifest.Domain == expectedDomain,
		"domain in mod.sentients.<lowerName> format", LevelWarning)
	// permissions must be an array (spec rule, WARNING severity)
	addLevel(res, "manifest.json", "permissions", rawPermissionsIsArray(manifestPath),
		"permissions is an array", LevelWarning)
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

func isSemver(v string) bool {
	return semverRE.MatchString(v)
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
