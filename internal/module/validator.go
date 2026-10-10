package module

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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
// Used by `pack` and shared by the audit pipeline. The module is resolved
// in whichever tree holds it (`modules/<name>` first, D5, then
// `library/modules/<name>`, D11).
func (v *Validator) ValidateModule(name string) (*Result, error) {
	if dir := config.ResolveModuleDir(v.Root, name); dir != "" {
		return v.validateModuleAt(dir, name)
	}
	return nil, errors.New(i18n.Tf("val.module_not_found", name,
		config.WorkspaceModulesDir+"|"+config.ExternalModulesDir))
}

// ValidateModuleDir validates the module rooted at moduleDir, whatever the
// layout (workspace source, installed multi-version tree, extracted archive
// in a temporary directory). The module identifier is read from the
// manifest.
func (v *Validator) ValidateModuleDir(moduleDir string) (*Result, error) {
	if !pkg.DirExists(moduleDir) {
		return nil, errors.New(i18n.Tf("val.module_not_found", filepath.Base(moduleDir), moduleDir))
	}
	manifest, err := LoadManifest(filepath.Join(moduleDir, config.ManifestFileName))
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(manifest.ID)
	if name == "" {
		name = filepath.Base(moduleDir)
	}
	return v.validateModuleAt(moduleDir, name)
}

// validateModuleAt runs the manifest and layout checks on one module
// directory. name may be empty (ValidateModuleDir) — the directory-name
// conformance warning is then skipped.
func (v *Validator) validateModuleAt(moduleDir, name string) (*Result, error) {
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
	// entry exists — at the module root for a workspace/legacy module, under
	// `src/` for an installed multi-version tree (§4.4).
	entryPath := filepath.Join(moduleDir, manifest.Entry)
	if !pkg.FileExists(entryPath) {
		if candidate := filepath.Join(moduleDir, "src", manifest.Entry); pkg.FileExists(candidate) {
			entryPath = candidate
		}
	}
	add(res, "manifest.json", "entry", pkg.FileExists(entryPath), "entry file present")
	// domain format warning (spec §5.10): any reverse-DNS dotted domain is
	// accepted, e.g. com.organization.domain (the former mod.liorian.<name>
	// prefix is no longer required).
	addLevel(res, "manifest.json", "domain", isDomainName(manifest.Domain),
		"domain in reverse-DNS format", LevelWarning)
	// the module directory must name its module
	if name != "" {
		addLevel(res, "manifest.json", "domain directory", directoryMatchesModule(filepath.Base(moduleDir), name, manifest.Domain),
			"module directory named after the module (library/modules/<domain> or modules/<id>)", LevelWarning)
	}
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
	// permissions: canonical bare permission domains (`Post`, `PostCategory`
	// — §6.5, the server composes `KEY:Domain` itself). The former
	// `<Role>:<Verbe>` couples never fed the namespaced RBAC and the schema
	// removed them — they trigger a migration warning.
	switch {
	case allPermissionDomains(manifest.Permissions):
		addLevel(res, "manifest.json", "permissions", true,
			"permissions use bare permission domains", LevelWarning)
	case allPermissionCodes(manifest.Permissions):
		addLevel(res, "manifest.json", "permissions", false,
			"permissions must use bare permission domains (legacy <Role>:<Verbe> couples — migrate to the module's real domains)", LevelWarning)
	default:
		addLevel(res, "manifest.json", "permissions", false,
			"permissions must use bare permission domains (malformed entries)", LevelWarning)
	}
	// access (§6.5): optional declarative gate — `<Role>` or `<Role>:<Niveau>`
	// entries (the module only shows/opens when one entry is satisfied).
	addLevel(res, "manifest.json", "access", accessEntriesValid(manifest.Access),
		"access entries are `<Role>` or `<Role>:<Niveau>`", LevelWarning)
	// themes (§6.10): token palettes of a THEME module — tokens only, the
	// whitelist (MODULE_THEME_TOKENS) and the contrast checks are enforced
	// server-side.
	addLevel(res, "manifest.json", "themes", themesValid(manifest.Themes),
		"themes entries declare an id and a token map", LevelWarning)
	// admin (§6.10): SYSTEM-module privileges. The first-party/Tauri/role
	// gating is server-side (fail-closed); a declaration on another type is
	// dead weight the socle ignores.
	if manifest.Admin != nil {
		addLevel(res, "manifest.json", "admin", ValidateAdminDeclaration(*manifest.Admin) == nil,
			"admin declares PascalCase roles and scopes", LevelWarning)
		addLevel(res, "manifest.json", "admin type",
			strings.EqualFold(strings.TrimSpace(manifest.Type), "SYSTEM"),
			"admin applies to SYSTEM modules", LevelWarning)
	}
	// remote (§6.10): the origin of a WEB_APP_REMOTE module — an HTTPS domain
	// origin (no literal IP), verified server-side before moderation.
	if manifest.Remote != nil {
		addLevel(res, "manifest.json", "remote", ValidateRemoteDeclaration(*manifest.Remote) == nil,
			"remote declares an HTTPS domain origin", LevelWarning)
		addLevel(res, "manifest.json", "remote type",
			strings.EqualFold(strings.TrimSpace(manifest.Type), "WEB_APP_REMOTE"),
			"remote applies to WEB_APP_REMOTE modules", LevelWarning)
	}
	// settings (§6.10): the parameter-menu entries of a CONFIGURATION module;
	// each entry resolves under /m/<slug>/.
	if manifest.Settings != nil {
		addLevel(res, "manifest.json", "settings", settingsEntriesValid(manifest.Settings.Entries),
			"settings entries carry a label", LevelWarning)
		addLevel(res, "manifest.json", "settings type",
			strings.EqualFold(strings.TrimSpace(manifest.Type), "CONFIGURATION"),
			"settings applies to CONFIGURATION modules", LevelWarning)
	}
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
	// time); a half-filled publisher block is a warning. A fully empty block
	// carries no half-typed field to correct — the publisher is completed when
	// the module is published — so it is not a finding.
	if rawHasKey(manifestPath, "publisher") {
		publisherEmpty := manifest.Publisher.ID == "" && manifest.Publisher.Name == ""
		publisherPartial := (manifest.Publisher.ID == "") != (manifest.Publisher.Name == "")
		if !publisherEmpty {
			addLevel(res, "manifest.json", "publisher", !publisherPartial,
				"publisher complete (id and name)", LevelWarning)
		}
	}
	// domain: canonical `mod.<éditeur>.<module>` preferred (spec
	// module-installation §4.3); any reverse-DNS form stays accepted.
	addLevel(res, "manifest.json", "canonical domain", IsCanonicalDomain(manifest.Domain),
		"domain in canonical mod.<organization-slug>.<module-identifier> form", LevelWarning)
	// backends (tier 1, D9): every declared backend must be schema-compliant
	// (§6.2). Applied to both layouts — a declaration is a declaration.
	for _, b := range manifest.Backends {
		add(res, "manifest.json", "backends", ValidateBackendDeclaration(b) == nil,
			"backend declaration compliant (key, https url, scopes)")
	}
	// userScope (D15): same grammar as `permissions`. Mandatory for
	// isolated-runtime modules (`[]` is the explicit "no user data"); a
	// legacy module without one is left alone — its migration happens in
	// the phase-8 move to modules/*.
	for _, s := range manifest.UserScope {
		add(res, "manifest.json", "userScope", ValidateUserScopeEntry(s) == nil,
			"userScope entry uses the Role:Verbe grammar")
	}
	if !rawHasKey(manifestPath, "userScope") && isModernModuleDir(moduleDir, manifest) {
		// D15: a module without userScope is refused at pack — `[]` is the
		// only way to declare "no user data".
		addLevel(res, "manifest.json", "userScope", false,
			"userScope present (mandatory for isolated-runtime modules)", LevelError)
	}
	// legal (D16, §6.11): optional — a module without it has no gate — but a
	// declaration that exists must be complete, or the server would refuse the
	// publication and lock the first user out of the module.
	add(res, "manifest.json", "legal", ValidateLegalDocuments(manifest.Legal) == nil,
		"legal declarations compliant (key, kind, version, content; TERMS and PRIVACY together)")
	// routines (§6.10): a compiled module names its `Routine` singletons; a
	// third-party SERVICE module declares descriptors executed by the socle
	// through ctx.api (job.kind "api", period bounded 30 s–1 h).
	routinesOK := true
	for _, r := range manifest.Routines {
		if ValidateRoutineDeclaration(r) != nil {
			routinesOK = false
			break
		}
	}
	addLevel(res, "manifest.json", "routines", routinesOK,
		"routines entries are singleton names or compliant declarative descriptors", LevelWarning)
	// declarative (§6.10): the dashboard widgets of a WIDGET module (or the
	// KPI-cards slot of a CONFIGURATION module). An interactive entry runs a
	// sandboxed artifact document — safe entry grammar, bounded minHeight and
	// a mandatory title; a native entry reads a dataModel resource.
	if manifest.Declarative != nil {
		widgetsOK := true
		for _, w := range manifest.Declarative.Widgets {
			if ValidateDeclarativeWidget(w) != nil {
				widgetsOK = false
				break
			}
		}
		addLevel(res, "manifest.json", "declarative.widgets", widgetsOK,
			"declarative widgets declare a resource or a compliant interactive block", LevelWarning)
	}
	// capabilities (§6.6): Tauri permission ids (`core:default`); the legacy
	// boolean-object flags (needsNetwork…) surface as non-conforming ids.
	addLevel(res, "manifest.json", "capabilities", capabilityIDsValid(manifest.Capabilities),
		"capabilities use Tauri permission ids", LevelWarning)

	// Aide assistée (`module.helpers.json`, spec `assistive-help`): optional
	// file at the module root, auto-discovered by the socle and embedded under
	// `src/module.helpers.json`. When present it must be compliant — a half-valid
	// help is misleading, so the whole file is refused (fail-closed, the same
	// verdict as the workspace `check:module-manifests`).
	if helpersPath := ModuleHelpersPath(moduleDir); helpersPath != "" {
		file, err := LoadModuleHelpersFile(helpersPath)
		switch {
		case err != nil:
			res.Findings = append(res.Findings, Finding{
				Category: ModuleHelpersFileName, Rule: "compliant",
				Severity: LevelError, Message: err.Error(),
			})
		default:
			if helpersErrs := ValidateModuleHelpersFile(file); len(helpersErrs) > 0 {
				res.Findings = append(res.Findings, Finding{
					Category: ModuleHelpersFileName, Rule: "compliant",
					Severity: LevelError,
					Message:  i18n.Tf("module.helpers.invalid", strings.Join(helpersErrs, " ; ")),
				})
			} else {
				add(res, ModuleHelpersFileName, "compliant", true,
					"module.helpers.json valid (assistive help)")
			}
		}
	}

	// CONFIGURATION modules carry their UI in the manifest itself
	// (`entry: index.json` + `dataModel`/`declarative`): no React entry needed.
	if strings.EqualFold(strings.TrimSpace(manifest.Type), "CONFIGURATION") {
		addLevel(res, "manifest.json", "entry", manifest.Entry == "index.json",
			"CONFIGURATION entry is index.json", LevelWarning)
		addLevel(res, "manifest.json", "dataModel",
			manifest.DataModel != nil,
			"CONFIGURATION dataModel present", LevelWarning)
		return res, nil
	}

	if isModernModuleDir(moduleDir, manifest) {
		v.validateModernModule(moduleDir, entryPath, manifest, res)
		return res, nil
	}

	// legacy declaration file (`index.tsx`, ModuleDeclarationInterface)
	indexPath := filepath.Join(moduleDir, config.LegacyDeclarationFileName)
	addLevel(res, config.LegacyDeclarationFileName, "export",
		pkg.FileExists(indexPath) && containsDefaultExport(indexPath),
		"index.tsx file with default export", LevelError)

	return res, nil
}

// isModernModuleDir reports whether a module directory follows the
// isolated-runtime layout: it declares an `artifact` section or already
// carries the built payload (development layout `.liorian/artifact/`, D7, or
// distribution layout `artifact/`). Legacy flat modules
// (library/modules/<name> with an index.tsx declaration) fall back to the
// legacy checks.
func isModernModuleDir(moduleDir string, m *Manifest) bool {
	if m.Artifact != nil {
		return true
	}
	return pkg.DirExists(filepath.Join(moduleDir, config.ModuleArtifactSourceDir)) ||
		pkg.DirExists(filepath.Join(moduleDir, config.ModuleArtifactDir))
}

// validateModernModule applies the pack rules of §4.4 to an
// isolated-runtime module. Every rule is blocking (LevelError) — the pack
// refuses an archive that fails any of them (D6, D15, D16).
func (v *Validator) validateModernModule(moduleDir, entryPath string, m *Manifest, res *Result) {
	// Rule 1: the entry is TypeScript — a .js/.mjs entry is refused (D6).
	entry := strings.TrimSpace(m.Entry)
	add(res, "manifest.json", "entry.ts", entry != "" &&
		(strings.HasSuffix(entry, ".ts") || strings.HasSuffix(entry, ".tsx")),
		"entry is a .ts/.tsx file")

	// Rule 2: a tsconfig.json is required (the `tsc --noEmit` pass itself is
	// enforced by the packer, which owns the toolchain). At the module root
	// for a workspace source tree, under src/ in an installed tree (the pack
	// stores the sources there).
	hasTsConfig := pkg.FileExists(filepath.Join(moduleDir, "tsconfig.json")) ||
		pkg.FileExists(filepath.Join(moduleDir, "src", "tsconfig.json"))
	add(res, "tsconfig.json", "present", hasTsConfig, "tsconfig.json present")

	// Rule 3: the artifact bundle and host document exist and are non-empty.
	// No size ceiling: only the zero-byte case is refused (the D7 rule 6
	// plafond was removed — artifact size is the developer's responsibility).
	artifactDir := filepath.Join(moduleDir, m.SourceArtifactDir(moduleDir))
	bundlePath := filepath.Join(artifactDir, m.EffectiveArtifactBundle())
	docPath := filepath.Join(artifactDir, m.EffectiveArtifactDocument())
	add(res, "artifact", "bundle", nonEmptyFile(bundlePath), "artifact bundle present and non-empty")
	add(res, "artifact", "document", nonEmptyFile(docPath), "artifact host document present and non-empty")

	// Rule 5 + §7.7: no `next/*` (execution constraint of D1) and no `@/`
	// (socle alias) in the module source graph.
	badNext, badAt := scanSourceImports(moduleDir)
	add(res, "isolation", "next/*", len(badNext) == 0,
		"no next/* import in the module source (D1)")
	for _, rel := range badNext {
		res.Findings = append(res.Findings, Finding{
			Category: "isolation", Rule: "next/*", Severity: LevelError,
			Message: fmt.Sprintf("%s imports next/* — use @liorian/sdk primitives and ctx.bridge instead", rel),
		})
	}
	add(res, "isolation", "@/ alias", len(badAt) == 0,
		"no @/ alias import in the module source (§7.7)")
	for _, rel := range badAt {
		res.Findings = append(res.Findings, Finding{
			Category: "isolation", Rule: "@/ alias", Severity: LevelError,
			Message: fmt.Sprintf("%s imports @/… — a module imports @liorian/sdk and its declared dependencies only", rel),
		})
	}

	// Rule 9 (D16): no fetch / XMLHttpRequest / WebSocket in the module's own
	// code — ApiService is the only network surface of a module (spec 4.5).
	sourceHits := scanSourceNetworkCalls(moduleDir)
	add(res, "source", "network calls", len(sourceHits) == 0,
		"no fetch/XHR/WebSocket in the module source (D16)")
	for _, hit := range sourceHits {
		res.Findings = append(res.Findings, Finding{
			Category: "source", Rule: "network calls", Severity: LevelError,
			Message: fmt.Sprintf("%s — the module must go through ApiService (D16)", hit),
		})
	}
	// The bundle is the module *and* the SDK runtime it embeds, and ApiService
	// legitimately calls fetch. A bundle hit therefore cannot attribute the
	// primitive to either side, so it is reported as an observation, not as a
	// verdict: the blocking D16 verdict is the source scan above, and when the
	// sources are clean the bundle hits belong to the embedded runtime.
	bundleHits := scanBundleNetworkCalls(bundlePath)
	if len(bundleHits) == 0 {
		add(res, "artifact", "network calls (bundle)", true,
			"no fetch/XHR/WebSocket in the bundle (D16)")
	} else if len(sourceHits) == 0 {
		res.Findings = append(res.Findings, Finding{
			Category: "artifact", Rule: "network calls (bundle)", Severity: LevelOK,
			Message: fmt.Sprintf("%s embeds %s — module source clean, hits attributed to the SDK runtime (D16)",
				m.EffectiveArtifactBundle(), strings.Join(bundleHits, ", ")),
		})
	}

	// Runtime contract: the entry exposes mount/unmount (§4.2).
	content, err := os.ReadFile(entryPath)
	mountOK := err == nil && mountExportRE.Match(content)
	unmountOK := err == nil && unmountExportRE.Match(content)
	add(res, "entry", "mount export", mountOK, "entry exports mount()")
	add(res, "entry", "unmount export", unmountOK, "entry exports unmount()")
}

// mountExportRE matches the `mount` export of the runtime contract (§4.2).
var mountExportRE = regexp.MustCompile(`export\s+(?:async\s+)?function\s+mount\b|export\s+(?:const|let|var)\s+mount\b`)

// unmountExportRE matches the `unmount` export of the runtime contract (§4.2).
var unmountExportRE = regexp.MustCompile(`export\s+(?:async\s+)?function\s+unmount\b|export\s+(?:const|let|var)\s+unmount\b`)

// importSpecifierRE matches static and dynamic import specifiers, plus
// require() calls, of a TS/TSX source file.
var importSpecifierRE = regexp.MustCompile(`(?:\bfrom\s*|\bimport\s*\(\s*|\brequire\s*\(\s*)['"]([^'"]+)['"]`)

// nextImportRE matches a `next/*` specifier (D1: forbidden in module bundles).
var nextImportRE = regexp.MustCompile(`^next/`)

// atImportRE matches a socle alias specifier (§7.7: forbidden in modules).
var atImportRE = regexp.MustCompile(`^@/`)

// excludedSourceDirs are never scanned for imports: build output, dependency
// trees and tooling state are not module source.
var excludedSourceDirs = map[string]bool{
	config.ModuleArtifactDir:       true,
	config.ModuleArtifactSourceDir: true,
	".liorian":                     true,
	"node_modules":                 true,
	"dist":                         true,
	".git":                         true,
}

// scanSourceImports walks the TypeScript source of a module and returns the
// relative paths of files importing `next/*` (first return value) and `@/…`
// (second return value).
func scanSourceImports(moduleDir string) (nextHits, atHits []string) {
	_ = filepath.Walk(moduleDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if excludedSourceDirs[info.Name()] || (info.Name() != filepath.Base(moduleDir) && strings.HasPrefix(info.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".ts") && !strings.HasSuffix(path, ".tsx") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(moduleDir, path)
		if err != nil {
			return nil
		}
		for _, m := range importSpecifierRE.FindAllSubmatch(data, -1) {
			specifier := string(m[1])
			if nextImportRE.MatchString(specifier) {
				nextHits = append(nextHits, filepath.ToSlash(rel))
				return nil
			}
			if atImportRE.MatchString(specifier) {
				atHits = append(atHits, filepath.ToSlash(rel))
				return nil
			}
		}
		return nil
	})
	return nextHits, atHits
}

// bundleNetworkCallRE matches the direct network primitives a bundle must
// never use (D16): fetch(), XMLHttpRequest and WebSocket construction.
var bundleNetworkCallRE = regexp.MustCompile(`\bfetch\s*\(|\bXMLHttpRequest\b|\bnew\s+WebSocket\b`)

// scanSourceNetworkCalls returns `file:line` for every direct use of a network
// primitive in the module's own TypeScript source (D16). The scan deliberately
// stops at the module boundary: `@liorian/sdk` is the network surface
// (ApiService), a third-party dependency is audited on its own, and the
// generated bundle mixes all of them.
func scanSourceNetworkCalls(moduleDir string) []string {
	var hits []string
	_ = filepath.Walk(moduleDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if excludedSourceDirs[info.Name()] || (info.Name() != filepath.Base(moduleDir) && strings.HasPrefix(info.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".ts") && !strings.HasSuffix(path, ".tsx") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(moduleDir, path)
		if err != nil {
			return nil
		}
		for index, line := range strings.Split(string(data), "\n") {
			if bundleNetworkCallRE.MatchString(line) {
				hits = append(hits, fmt.Sprintf("%s:%d", filepath.ToSlash(rel), index+1))
			}
		}
		return nil
	})
	return hits
}

// scanBundleNetworkCalls returns the distinct D16 violations found in a
// bundle file.
func scanBundleNetworkCalls(bundlePath string) []string {
	data, err := os.ReadFile(bundlePath)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var hits []string
	for _, m := range bundleNetworkCallRE.FindAllString(string(data), -1) {
		if !seen[m] {
			seen[m] = true
			hits = append(hits, strings.TrimSpace(m))
		}
	}
	return hits
}

// nonEmptyFile reports whether path exists and carries at least one byte.
func nonEmptyFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
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

// directoryMatchesModule reports whether a module directory names the module it
// holds. Three conventions coexist and are all legitimate (spec §7.7):
//
//   - the installed tree `library/modules/<domain>/` — the directory is the
//     full domain (`mod.liorian.crm`) ;
//   - the source tree `modules/<id>/` — the directory is the module identifier
//     (`crm`, `messenger`) ;
//   - a workspace module published under a legacy catalog identifier, where the
//     directory is the id and the domain is the historical name
//     (`modules/messenger/` with `mod.liorian.chating`).
//
// A directory matching none of them is a module installed under a folder that
// says nothing about it — worth a warning.
func directoryMatchesModule(dirName, id, domain string) bool {
	dirName = strings.TrimSpace(dirName)
	id = strings.TrimSpace(id)
	domain = strings.TrimSpace(domain)
	if dirName == "" {
		return false
	}
	if dirName == id || (id != "" && dirName == domain) {
		return true
	}
	if labels := strings.Split(domain, "."); domain != "" && len(labels) > 0 && dirName == labels[len(labels)-1] {
		return true
	}
	return false
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

// allPermissionDomains reports whether every entry of `permissions` is a bare
// PascalCase permission domain — the canonical grammar (§6.5). An empty list
// is valid: it declares a module that exposes no attributable permission.
func allPermissionDomains(permissions []string) bool {
	for _, p := range permissions {
		if !IsPermissionDomain(p) {
			return false
		}
	}
	return true
}

// allPermissionCodes reports whether every entry of `permissions` uses the
// former `<Role>:<Verbe>` grammar — the legacy form the schema removed.
func allPermissionCodes(permissions []string) bool {
	for _, p := range permissions {
		if !IsPermissionCode(p) {
			return false
		}
	}
	return len(permissions) > 0
}

// accessEntriesValid reports whether every `access` entry is a bare role name
// or a `<Role>:<Niveau>` couple (§6.5).
func accessEntriesValid(access []string) bool {
	for _, entry := range access {
		if !IsDeclaredAccessEntry(entry) {
			return false
		}
	}
	return true
}

// themesValid reports whether every `themes` entry carries an id and a token
// map (§6.10).
func themesValid(themes []ThemeDeclaration) bool {
	for _, t := range themes {
		if ValidateThemeDeclaration(t) != nil {
			return false
		}
	}
	return true
}

// settingsEntriesValid reports whether every `settings.entries` entry carries
// a label (§6.10).
func settingsEntriesValid(entries []SettingsEntry) bool {
	for _, e := range entries {
		if ValidateSettingsEntry(e) != nil {
			return false
		}
	}
	return true
}

// capabilityIDRE matches a Tauri capability/permission identifier
// (`core:default`, `notification:default`, `core:window:allow-start-dragging`).
var capabilityIDRE = regexp.MustCompile(`^[a-z][a-z0-9_]*:[a-z0-9_.:-]+$`)

// capabilityIDsValid reports whether every `capabilities` entry is a Tauri
// permission identifier (§6.6). The legacy boolean-object flags — normalized
// on load to plain names (`needsNetwork`) — do not conform.
func capabilityIDsValid(capabilities Capabilities) bool {
	for _, c := range capabilities {
		if !capabilityIDRE.MatchString(strings.TrimSpace(c)) {
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
