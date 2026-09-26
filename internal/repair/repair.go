// Package repair automatically fixes the auto-repairable findings of a module
// audit (`liora audit`) and reports developer instructions for the findings
// that require a manual code change.
//
// The engine reuses the audit pipeline: it runs the same checks, applies the
// safe metadata repairs (manifest fields, missing npm dependencies, malformed
// manifest JSON), then re-audits the module to report what is left. Every
// finding that cannot be fixed automatically is turned into a step-by-step
// instruction for the developer.
package repair

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/protorians/lior-cli/internal/audit"
	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/module"
	"github.com/protorians/lior-cli/internal/pkg"
)

// Action is an automatic fix applied (or, in dry-run, that would be applied)
// to a module.
type Action struct {
	Category string `json:"category"`
	Rule     string `json:"rule"`
	Detail   string `json:"detail"`
	Applied  bool   `json:"applied"`
}

// Instruction is a manual action the developer must perform: the finding is
// too complex or too context-dependent to be repaired automatically.
type Instruction struct {
	Category string   `json:"category"`
	Rule     string   `json:"rule"`
	Message  string   `json:"message"`
	Steps    []string `json:"steps"`
}

// ModuleResult is the repair outcome of a single module.
type ModuleResult struct {
	Module       string           `json:"module"`
	Actions      []Action         `json:"actions"`
	Instructions []Instruction    `json:"instructions"`
	Remaining    []module.Finding `json:"remaining"`
}

// RepairResult aggregates the repair outcomes of one or more modules.
type RepairResult struct {
	Modules []ModuleResult `json:"modules"`
}

// TotalApplied returns the number of automatic fixes actually applied.
func (r *RepairResult) TotalApplied() int {
	n := 0
	for _, m := range r.Modules {
		for _, a := range m.Actions {
			if a.Applied {
				n++
			}
		}
	}
	return n
}

// TotalInstructions returns the number of manual actions reported.
func (r *RepairResult) TotalInstructions() int {
	n := 0
	for _, m := range r.Modules {
		n += len(m.Instructions)
	}
	return n
}

// TotalRemainingErrors returns the number of blocking findings left after the
// repair, across all modules.
func (r *RepairResult) TotalRemainingErrors() int {
	n := 0
	for _, m := range r.Modules {
		for _, f := range m.Remaining {
			if f.Severity == module.LevelError {
				n++
			}
		}
	}
	return n
}

// Repairer fixes the auto-repairable audit findings of a module.
type Repairer struct {
	// Root is the project root containing library/modules/.
	Root string
	// IncludeWarnings also repairs non-blocking (WARNING) findings.
	IncludeWarnings bool
	// DryRun reports the fixes without writing anything to disk.
	DryRun bool
	// SkipInstall prevents the automatic package-manager install that repairs
	// missing npm dependencies.
	SkipInstall bool
	// NoInteraction applies the suggested module directory name without
	// prompting the developer (used by --no-interaction and non-interactive
	// runs).
	NoInteraction bool
	// Rename asks the developer for the new module directory name, prefilled
	// with the suggested one. It returns false when the developer declines.
	// nil disables the prompt (non-interactive runs).
	Rename func(message, suggestion string) (string, bool)
}

// renameRequest describes a directory/folder mismatch that can only be fixed by
// renaming the module directory, which changes its identity and is therefore
// confirmed by the developer.
type renameRequest struct {
	from string
	to   string
	rule string
}

// planRename returns the rename request for a module whose directory does not
// match its manifest domain resolution, "" when no rename is warranted.
func planRename(name, domain string) (renameRequest, bool) {
	target := domainTargetDir(name, domain)
	if target == "" || target == name {
		return renameRequest{}, false
	}
	return renameRequest{from: name, to: target, rule: "domain directory"}, true
}

// domainTargetDir returns the canonical directory name for a module: the
// manifest domain when it is a valid dotted name (the directory mirrors the
// domain), else the module name itself ("" when no rename can help).
func domainTargetDir(name, domain string) string {
	if domain != "" && isDomainName(domain) {
		return domain
	}
	if domain == "" && isDomainName(name) {
		return name
	}
	return ""
}

// instructionSet accumulates the manual instructions of one module,
// deduplicated by "category|rule".
//
// A finding that cannot be fixed automatically is reported twice: once by the
// repair pass, which turns it into an instruction, and once by the re-audit
// pass, which sees it is still failing. Every instruction therefore goes
// through a set so the report lists each one exactly once.
type instructionSet struct {
	keys map[string]bool
}

func newInstructionSet() *instructionSet {
	return &instructionSet{keys: map[string]bool{}}
}

func instructionKey(category, rule string) string {
	return category + "|" + rule
}

// add appends ins to dst unless an instruction with the same category and rule
// is already recorded.
func (s *instructionSet) add(dst *[]Instruction, ins Instruction) {
	key := instructionKey(ins.Category, ins.Rule)
	if s.keys[key] {
		return
	}
	s.keys[key] = true
	*dst = append(*dst, ins)
}

// addFinding maps an unfixed finding to its instruction and records it.
func (s *instructionSet) addFinding(dst *[]Instruction, f module.Finding, name string) {
	s.add(dst, *instructionForFinding(f, name))
}

// drop forgets the instruction matching (category, rule) so a later finding on
// the same rule is reported again.
func (s *instructionSet) drop(category, rule string) {
	delete(s.keys, instructionKey(category, rule))
}

// RepairModules repairs one module (name non-empty) or all modules under
// library/modules/ (name empty).
func (r *Repairer) RepairModules(name string) (*RepairResult, error) {
	auditor := &audit.Auditor{Root: r.Root}
	initial, err := auditor.AuditModules(name)
	if err != nil {
		return nil, err
	}

	result := &RepairResult{}
	pendingInstall := map[string]bool{}

	// One instruction set per repaired module, kept index-aligned with
	// result.Modules so the install and re-audit passes record into the same
	// accumulator as the repair pass.
	sets := make([]*instructionSet, 0, len(initial.Modules))

	for _, mod := range initial.Modules {
		set := newInstructionSet()
		sets = append(sets, set)

		mr, domain := r.repairMetadata(mod.Module, mod.Findings, pendingInstall, set)
		r.applyRename(mr, mod.Module, domain, pendingInstall, set)
		result.Modules = append(result.Modules, *mr)
	}

	r.installDependencies(result, pendingInstall, sets)

	// Re-audit each module to report the remaining findings and turn any
	// still-unfixed finding into a developer instruction.
	for i := range result.Modules {
		mod := result.Modules[i].Module
		fresh, err := auditor.AuditModules(mod)
		if err != nil || len(fresh.Modules) != 1 {
			continue
		}
		remaining := remainingFindings(fresh.Modules[0].Findings, r.IncludeWarnings)
		result.Modules[i].Remaining = remaining
		for _, f := range remaining {
			sets[i].addFinding(&result.Modules[i].Instructions, f, mod)
		}
	}

	return result, nil
}

// repairMetadata loads the module manifest, applies every auto-repairable
// finding and saves it. It flags the module when a dependency install is
// needed (handled globally by installDependencies). It returns the module
// result and the manifest domain left by the repairs, so applyRename can act
// on the repaired value instead of re-reading a manifest that a dry run never
// wrote.
func (r *Repairer) repairMetadata(name string, findings []module.Finding, pendingInstall map[string]bool, set *instructionSet) (*ModuleResult, string) {
	mr := &ModuleResult{Module: name}
	moduleDir := filepath.Join(r.Root, config.ExternalModulesDir, name)
	manifestPath := filepath.Join(moduleDir, config.ManifestFileName)

	raw, mf, err := loadManifestLenient(manifestPath)
	if err != nil {
		set.addFinding(&mr.Instructions, module.Finding{
			Category: "manifest.json",
			Rule:     "lecture",
			Severity: module.LevelError,
			Message:  err.Error(),
		}, name)
		return mr, ""
	}
	m, normalized := raw, mf.normalized

	// Baseline of the manifest as it would be re-serialized, used to tell the
	// fields a repair actually touched from the fields that were only absent.
	before, _ := json.Marshal(m)

	changed := normalized
	if normalized {
		mr.Actions = append(mr.Actions, Action{
			Category: "manifest.json",
			Rule:     "lecture",
			Detail:   "normalized malformed manifest fields",
		})
	}

	for _, f := range findings {
		if f.Severity == module.LevelOK {
			continue
		}
		if f.Severity == module.LevelWarning && !r.IncludeWarnings {
			continue
		}
		if f.Category == "dependencies" {
			pendingInstall[name] = true
			continue
		}
		action, instruction, ch := r.applyFinding(m, moduleDir, name, f, mf)
		if ch {
			changed = true
		}
		if instruction != nil {
			set.add(&mr.Instructions, *instruction)
			continue
		}
		if !ch {
			// Another repair already satisfied this finding.
			continue
		}
		mr.Actions = append(mr.Actions, action)
	}

	if changed && !r.DryRun {
		after, _ := json.Marshal(m)
		if err := saveManifestSparse(m, manifestPath, mf, changedKeys(before, after)); err != nil {
			set.addFinding(&mr.Instructions, module.Finding{
				Category: "manifest.json",
				Rule:     "save",
				Severity: module.LevelError,
				Message:  err.Error(),
			}, name)
			return mr, m.Domain
		}
	}

	applied := changed && !r.DryRun
	for i := range mr.Actions {
		mr.Actions[i].Applied = applied
	}
	return mr, m.Domain
}

// applyRename fixes the "domain directory" finding by renaming the module
// directory to its manifest domain (or vice versa). The new name is proposed in
// the prompt (tab fills it), applied directly when the developer accepts or when
// the run is non-interactive, and the module result — along with the pending
// dependency installs — is remapped to the new name.
//
// domain is the manifest domain left by the repairs, so a dry run proposes the
// rename a real run would perform instead of the stale on-disk value.
func (r *Repairer) applyRename(mr *ModuleResult, name, domain string, pendingInstall map[string]bool, set *instructionSet) {
	req, ok := planRename(name, domain)
	if !ok {
		return
	}

	if r.DryRun {
		// Nothing is renamed, so the manual rename instruction stays in the
		// report next to the action that would resolve it.
		mr.Actions = append(mr.Actions, Action{
			Category: "manifest.json",
			Rule:     req.rule,
			Detail:   fmt.Sprintf("would rename the module directory to %q", req.to),
		})
		return
	}

	mr.Actions = dropAction(mr.Actions, "manifest.json", req.rule)
	mr.Instructions = dropInstruction(mr.Instructions, "manifest.json", req.rule)
	set.drop("manifest.json", req.rule)

	target := req.to
	if r.Rename != nil && !r.NoInteraction {
		value, accept := r.Rename(i18n.Tf("repair.rename.prompt", req.from), req.to)
		if !accept {
			set.addFinding(&mr.Instructions, module.Finding{
				Category: "manifest.json", Rule: req.rule, Severity: module.LevelError,
				Message: i18n.T("repair.rename.detected"),
			}, req.from)
			return
		}
		if strings.TrimSpace(value) != "" {
			target = strings.TrimSpace(value)
		}
	}

	if err := renameModuleDir(r.Root, req.from, target); err != nil {
		set.addFinding(&mr.Instructions, module.Finding{
			Category: "manifest.json", Rule: req.rule, Severity: module.LevelError,
			Message: err.Error(),
		}, req.from)
		return
	}

	if pendingInstall[name] {
		delete(pendingInstall, name)
		pendingInstall[target] = true
	}
	mr.Module = target
	// The retained instructions were written against the old directory: point
	// them at the new one so the developer is never sent to a path that is
	// gone.
	retargetInstructions(mr.Instructions, req.from, target)
	mr.Actions = append(mr.Actions, Action{
		Category: "manifest.json",
		Rule:     req.rule,
		Detail:   fmt.Sprintf("renamed the module directory to %q", target),
		Applied:  true,
	})
}

// retargetInstructions rewrites the module directory referenced by the manual
// instructions from one name to another.
func retargetInstructions(instructions []Instruction, from, to string) {
	if from == to || !isDomainName(from) && !isDomainName(to) {
		return
	}
	for i := range instructions {
		for j, step := range instructions[i].Steps {
			instructions[i].Steps[j] = strings.ReplaceAll(step,
				config.ExternalModulesDir+"/"+from, config.ExternalModulesDir+"/"+to)
		}
	}
}

// renameModuleDir renames a module directory, replacing an existing target
// (the stale directory left by an already-renamed module).
func renameModuleDir(root, from, to string) error {
	if from == to {
		return nil
	}
	modulesDir := filepath.Join(root, config.ExternalModulesDir)
	src := filepath.Join(modulesDir, from)
	dst := filepath.Join(modulesDir, to)
	if !pkg.DirExists(src) {
		return fmt.Errorf("module directory %s not found", from)
	}
	if pkg.DirExists(dst) {
		if err := os.RemoveAll(dst); err != nil {
			return err
		}
	}
	return os.Rename(src, dst)
}

// dropAction removes the action matching (category, rule).
func dropAction(actions []Action, category, rule string) []Action {
	out := actions[:0]
	for _, a := range actions {
		if a.Category == category && a.Rule == rule {
			continue
		}
		out = append(out, a)
	}
	return out
}

// dropInstruction removes the instruction matching (category, rule).
func dropInstruction(instructions []Instruction, category, rule string) []Instruction {
	out := instructions[:0]
	for _, ins := range instructions {
		if ins.Category == category && ins.Rule == rule {
			continue
		}
		out = append(out, ins)
	}
	return out
}

// applyFinding dispatches a single finding to its repair: it mutates the
// manifest in place (changed=true), or returns a manual instruction.
//
// mf carries the raw JSON of the manifest file so a repair can tell a
// malformed field from an absent one — the typed Manifest decodes both to the
// same zero value.
func (r *Repairer) applyFinding(m *module.Manifest, moduleDir, name string, f module.Finding, mf manifestFile) (Action, *Instruction, bool) {
	act := Action{Category: f.Category, Rule: f.Rule, Detail: f.Message}

	switch f.Category {
	case "manifest.json":
		switch f.Rule {
		case "id":
			m.ID = deriveID(name, m.Domain, m.Name)
			act.Detail = fmt.Sprintf("set id to %q", m.ID)
			return act, nil, true
		case "name":
			base := m.ID
			if base == "" {
				base = name
			}
			m.Name = module.DisplayName(base)
			if m.Name == "" {
				m.Name = "Module"
			}
			act.Detail = fmt.Sprintf("set name to %q", m.Name)
			return act, nil, true
		case "version":
			m.Version = "0.0.0"
			act.Detail = `set version to "0.0.0"`
			return act, nil, true
		case "token":
			m.Token = pkg.NewUUID()
			act.Detail = "generated a new UUID token"
			return act, nil, true
		case "entry":
			// A CONFIGURATION module carries its UI in the manifest itself, so
			// its entry is the declarative index.json — never the React entry
			// the other types resolve to.
			if isConfiguration(m) {
				return r.applyConfigurationEntry(m, moduleDir, name, act, f)
			}
			if pkg.FileExists(filepath.Join(moduleDir, config.ModuleEntryFileName)) {
				m.Entry = config.ModuleEntryFileName
				act.Detail = fmt.Sprintf("set entry to %q", m.Entry)
				return act, nil, true
			}
			return act, instructionForFinding(f, name), false
		case "permissions":
			return r.applyPermissions(m, name, act, f, mf)
		case "optionalRequirements":
			m.OptionalRequirements = map[string]string{}
			act.Detail = "added optionalRequirements"
			return act, nil, true
		case "platforms":
			return r.applyPlatforms(m, name, act, f, mf)
		case "managerCompatibility", "compatibility", "apiCompatibility":
			return r.applyCompatibility(m, act, f)
		case "capabilities":
			m.Capabilities = module.Capabilities{"core:default"}
			act.Detail = "added capabilities"
			return act, nil, true
		case "oauth.scopes":
			return r.applyOAuthScopes(m, act)
		case "type":
			return applyModuleType(m, name, act, f)
		case "canonical domain":
			if d, ok := canonicalDomainFrom(m.Domain); ok {
				m.Domain = d
				act.Detail = fmt.Sprintf("migrated domain to %q", d)
				return act, nil, true
			}
			return act, instructionForFinding(f, name), false
		case "dataModel":
			// The CRUD resources are a design decision, never a guess.
			return act, newInstruction("manifest.json", "dataModel",
				"the CONFIGURATION module declares no dataModel",
				"Open library/modules/"+name+"/manifest.json.",
				"Declare the CRUD resources in \"dataModel\" (resource, fields and their Role:Verbe permissions).",
				"Or set \"entry\" to \"index.tsx\" and pick a code type instead of CONFIGURATION.",
				"Re-run 'liora repair "+name+"' to verify."), false
		case "category":
			m.Category = "SYSTEM"
			act.Detail = `set category to "SYSTEM"`
			return act, nil, true
		case "domain":
			if d := domainTargetDir(name, ""); d != "" {
				m.Domain = d
				act.Detail = fmt.Sprintf("set domain to %q", d)
				return act, nil, true
			}
			return act, instructionForFinding(f, name), false
		case "domain directory":
			// The directory/domain mismatch is fixed by renaming the directory
			// (applyRename), never by silently rewriting the manifest domain.
			return act, instructionForFinding(f, name), false
		}
	}

	return act, instructionForFinding(f, name), false
}

// isConfiguration reports whether the manifest declares a CONFIGURATION module.
func isConfiguration(m *module.Manifest) bool {
	return strings.EqualFold(strings.TrimSpace(m.Type), typeConfiguration)
}

// applyConfigurationEntry points a CONFIGURATION module at its declarative
// index.json, the entry its UI is declared in.
func (r *Repairer) applyConfigurationEntry(m *module.Manifest, moduleDir, name string, act Action, f module.Finding) (Action, *Instruction, bool) {
	if strings.TrimSpace(m.Entry) == configurationEntryFile {
		return act, nil, false
	}
	if pkg.FileExists(filepath.Join(moduleDir, configurationEntryFile)) {
		m.Entry = configurationEntryFile
		act.Detail = fmt.Sprintf("set entry to %q", m.Entry)
		return act, nil, true
	}
	return act, newInstruction("manifest.json", "entry",
		"the CONFIGURATION module has no index.json entry",
		"Create library/modules/"+name+"/"+configurationEntryFile+" (the declarative module page).",
		"Set manifest.json \"entry\" to \""+configurationEntryFile+"\".",
		"Re-run 'liora repair "+name+"' to verify."), false
}

// applyPermissions repairs the `permissions` rule, which the audit raises under
// two distinct shapes that share one rule name: a malformed (non-array) value,
// and an array holding legacy non-canonical codes. Dispatching on the manifest
// state rather than on the finding keeps the two apart — a valid array is never
// emptied, only migrated where the canonical code is unambiguous.
func (r *Repairer) applyPermissions(m *module.Manifest, name string, act Action, f module.Finding, mf manifestFile) (Action, *Instruction, bool) {
	if !mf.isArray("permissions") {
		m.Permissions = []string{}
		act.Detail = "normalized permissions to an array"
		return act, nil, true
	}

	canonical := make([]string, 0, len(m.Permissions))
	ambiguous := []string{}
	migrated := 0
	for _, code := range m.Permissions {
		trimmed := strings.TrimSpace(code)
		if resolved, ok := migratePermissionCode(trimmed); ok {
			if resolved != trimmed {
				migrated++
			}
			canonical = append(canonical, resolved)
			continue
		}
		// Keep the entry: a legacy code the mapping cannot resolve losslessly
		// is the developer's to translate, not ours to drop.
		ambiguous = append(ambiguous, trimmed)
		canonical = append(canonical, trimmed)
	}

	if len(ambiguous) == 0 && migrated == 0 {
		// Every code is already canonical: the finding came from the audit
		// having read the file before this pass.
		return act, nil, false
	}

	m.Permissions = dedupeStrings(canonical)
	if len(ambiguous) == 0 {
		act.Detail = fmt.Sprintf("migrated %d permission(s) to canonical Role:Verbe codes", migrated)
		return act, nil, true
	}
	act.Detail = fmt.Sprintf("migrated %d permission(s) to canonical Role:Verbe codes", migrated)
	return act, newInstruction("manifest.json", "permissions",
		"some permission codes are not canonical Role:Verbe codes",
		"Open library/modules/"+name+"/manifest.json.",
		"Translate the remaining legacy codes to <Role>:<Verbe> ("+strings.Join(module.ModuleRoles, ", ")+" × Get, Post, Put, Delete).",
		"They were left untouched: a legacy verb like \"write\" maps to several canonical verbs."), true
}

// applyPlatforms completes the platform declaration without ever narrowing it:
// the `modes` of the platforms the module already claims are filled in, and the
// `supported` flags are never touched. A module serving desktop and mobile
// keeps doing so after a repair.
func (r *Repairer) applyPlatforms(m *module.Manifest, name string, act Action, f module.Finding, mf manifestFile) (Action, *Instruction, bool) {
	if !mf.has("platforms") {
		m.Platforms = defaultPlatforms()
		act.Detail = "added default platforms (web)"
		return act, nil, true
	}

	filled := 0
	for _, p := range []struct {
		platform *module.Platform
		mode     string
	}{
		{&m.Platforms.Web, "web"},
		{&m.Platforms.Desktop, "local-webview"},
		{&m.Platforms.Mobile, "local-webview"},
	} {
		if !p.platform.Supported || len(p.platform.Modes) > 0 {
			continue
		}
		p.platform.Modes = []string{p.mode}
		filled++
	}
	if filled == 0 {
		return act, nil, false
	}
	act.Detail = fmt.Sprintf("added the missing mode(s) to %d supported platform(s)", filled)
	return act, nil, true
}

// applyCompatibility fills the canonical `compatibility.{socle,api}` windows in
// a single pass, carrying the legacy windows over rather than resetting them.
// An absent legacy window yields an open lower bound instead of an invented
// upper bound, which could wrongly exclude a version the module supports.
func (r *Repairer) applyCompatibility(m *module.Manifest, act Action, f module.Finding) (Action, *Instruction, bool) {
	if m.Compatibility == nil {
		m.Compatibility = &module.ModuleCompatibility{}
	}
	socle := fillCompatibilityRange(&m.Compatibility.Socle, m.ManagerCompat)
	api := fillCompatibilityRange(&m.Compatibility.API, m.APICompat)
	if !socle && !api {
		return act, nil, false
	}

	added := []string{}
	if socle {
		added = append(added, "compatibility.socle")
	}
	if api {
		added = append(added, "compatibility.api")
	}
	act.Detail = "added " + strings.Join(added, " and ")
	return act, nil, true
}

// fillCompatibilityRange completes dst from its legacy counterpart, reporting
// whether it wrote anything.
func fillCompatibilityRange(dst *module.CompatibilityRange, legacy module.Compatibility) bool {
	if strings.TrimSpace(dst.Min) != "" {
		return false
	}
	*dst = module.CompatibilityRange{
		Min:    strings.TrimSpace(legacy.Min),
		Max:    strings.TrimSpace(legacy.Max),
		Strict: legacy.Strict,
	}
	if dst.Min == "" {
		dst.Min = openRangeMin
	}
	return true
}

// applyOAuthScopes brings the negotiated scopes back into the catalogue the
// OAuth server is authorized to grant. An empty (but declared) list satisfies
// the rule: a module requesting nothing is a valid module.
func (r *Repairer) applyOAuthScopes(m *module.Manifest, act Action) (Action, *Instruction, bool) {
	scopes := canonicalOAuthScopes(m.EffectiveOAuthScopes())
	if m.OAuth != nil && m.OAuth.Scopes != nil && sameStrings(m.OAuth.Scopes, scopes) {
		return act, nil, false
	}
	if m.OAuth == nil {
		m.OAuth = &module.ModuleOAuth{}
	}
	m.OAuth.Scopes = scopes
	act.Detail = fmt.Sprintf("set oauth.scopes to %v", scopes)
	return act, nil, true
}

// applyModuleType migrates a deprecated distribution type to its canonical
// ModuleType equivalent, and leaves a value outside the enum to the developer.
func applyModuleType(m *module.Manifest, name string, act Action, f module.Finding) (Action, *Instruction, bool) {
	current := strings.ToUpper(strings.TrimSpace(m.Type))
	if canonical, ok := legacyModuleTypes[current]; ok {
		m.Type = canonical
		act.Detail = fmt.Sprintf("migrated type %q to %q", current, canonical)
		return act, nil, true
	}
	if current == "" || module.ModuleTypes[current] {
		// Already canonical: nothing to migrate.
		return act, nil, false
	}
	return act, newInstruction("manifest.json", "type",
		fmt.Sprintf("type %q is outside the allowed enum", m.Type),
		"Open library/modules/"+name+"/manifest.json.",
		"Set \"type\" to one of: "+strings.Join(canonicalModuleTypes(), ", ")+".",
		"Re-run 'liora repair "+name+"' to verify."), false
}

// canonicalOAuthScopes keeps the scopes the OAuth server may grant, in their
// declared order and without duplicates.
func canonicalOAuthScopes(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] || !module.OAuthScopes[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// canonicalDomainFrom derives the canonical `mod.<éditeur>.<module>` domain
// from a legacy reverse-DNS one: the publisher label becomes the editor and the
// module label is kept (com.example.blog-manager → mod.example.blog-manager).
// It reports false when the domain is already canonical, is not a valid
// reverse-DNS name, or carries too few labels to name an editor.
func canonicalDomainFrom(domain string) (string, bool) {
	d := strings.TrimSpace(domain)
	if d == "" || module.IsCanonicalDomain(d) || !isDomainName(d) {
		return "", false
	}
	labels := strings.Split(d, ".")
	if len(labels) < 3 {
		return "", false
	}
	editor, moduleName := labels[1], labels[len(labels)-1]
	if editor == "" || moduleName == "" {
		return "", false
	}
	canonical := "mod." + editor + "." + moduleName
	if !isDomainName(canonical) {
		return "", false
	}
	return canonical, true
}

// legacyPermissionVerbs maps the legacy dotted `<id>.<action>` verbs to their
// canonical `Role:Verbe` counterpart. Only the unambiguous pairs are listed: a
// legacy `write` maps to three different canonical verbs, so it is reported to
// the developer instead of being guessed.
var legacyPermissionVerbs = map[string]string{
	"read":   "Get",
	"get":    "Get",
	"create": "Post",
	"add":    "Post",
	"update": "Put",
	"edit":   "Put",
	"delete": "Delete",
	"remove": "Delete",
}

// legacyModuleTypes maps the deprecated distribution types to their canonical
// ModuleType equivalent (see the Manifest doc: INTERNAL is a socle-bundled
// module, EXTERNAL a remotely-served web app).
var legacyModuleTypes = map[string]string{
	"INTERNAL": "SYSTEM",
	"EXTERNAL": "WEB_APP_REMOTE",
}

// canonicalModuleTypes lists the canonical distribution types, for the
// instructions that hand the choice back to the developer.
func canonicalModuleTypes() []string {
	types := make([]string, 0, len(module.ModuleTypes))
	for t := range module.ModuleTypes {
		if !module.LegacyModuleTypes[t] {
			types = append(types, t)
		}
	}
	sort.Strings(types)
	return types
}

// migratePermissionCode maps one manifest permission to its canonical
// `Role:Verbe` code, reporting false when the mapping would be a guess.
func migratePermissionCode(code string) (string, bool) {
	code = strings.TrimSpace(code)
	if module.IsPermissionCode(code) {
		return code, true
	}
	role, action, ok := strings.Cut(code, ".")
	if !ok {
		return "", false
	}
	verb, ok := legacyPermissionVerbs[strings.ToLower(strings.TrimSpace(action))]
	if !ok {
		return "", false
	}
	canonRole := canonicalRole(role)
	if canonRole == "" {
		return "", false
	}
	return canonRole + ":" + verb, true
}

// canonicalRole resolves a legacy role token against the ModuleRoles
// catalogue, tolerating case and separator differences (user_read → User).
func canonicalRole(role string) string {
	key := kebab(role)
	for _, r := range module.ModuleRoles {
		if strings.EqualFold(r, key) {
			return r
		}
	}
	return ""
}

// dedupeStrings returns the values in order, without duplicates.
func dedupeStrings(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// sameStrings reports whether two string slices hold the same values in the
// same order.
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// installDependencies runs the detected package manager to install the
// dependencies flagged by the audit, then records the action for each module
// that needed it.
//
// The install runs in the module's own directory, where the package.json the
// audit reads lives: running it at the project root cannot resolve the
// dependencies of a module that is not part of a workspace.
func (r *Repairer) installDependencies(result *RepairResult, pending map[string]bool, sets []*instructionSet) {
	if len(pending) == 0 {
		return
	}
	pm := pkg.DetectPackageManager()

	// Group the flagged modules by the directory their install must run in.
	dirs := []string{}
	for _, m := range result.Modules {
		if !pending[m.Module] {
			continue
		}
		if dir := r.installDir(m.Module); !containsString(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}

	ran := map[string]bool{}
	runErr := map[string]error{}
	if !r.DryRun && !r.SkipInstall && pm != "" {
		for _, dir := range dirs {
			err := pkg.StreamCommandIn(dir, pm, "install")
			ran[dir] = err == nil
			runErr[dir] = err
		}
	}

	for i := range result.Modules {
		name := result.Modules[i].Module
		if !pending[name] {
			continue
		}
		dir := r.installDir(name)
		switch {
		case r.DryRun:
			result.Modules[i].Actions = append(result.Modules[i].Actions, Action{
				Category: "dependencies",
				Rule:     "install",
				Detail:   fmt.Sprintf("would run %s install in %s", pmLabel(pm), moduleInstallLabel(r.Root, dir)),
			})
		case r.SkipInstall || pm == "":
			// Left to the re-audit pass, which reports the remaining
			// dependency findings as manual instructions.
		case ran[dir]:
			result.Modules[i].Actions = append(result.Modules[i].Actions, Action{
				Category: "dependencies",
				Rule:     "install",
				Detail:   fmt.Sprintf("ran %s install in %s", pm, moduleInstallLabel(r.Root, dir)),
				Applied:  true,
			})
		default:
			// A failed install is not a missing dependency: reporting it as one
			// would point the developer at a package.json that is fine.
			sets[i].add(&result.Modules[i].Instructions, *newInstruction("dependencies", "install",
				fmt.Sprintf("%s install failed", pm),
				fmt.Sprintf("Run it yourself in %s:", moduleInstallLabel(r.Root, dir)),
				pm+" install",
				"Fix the reported error, then re-run 'liora repair "+name+"'."))
		}
	}
}

// installDir returns the directory the package manager must run in for a
// module: its own directory when it declares a package.json, the project root
// otherwise.
func (r *Repairer) installDir(name string) string {
	dir := filepath.Join(r.Root, config.ExternalModulesDir, name)
	if pkg.FileExists(filepath.Join(dir, "package.json")) {
		return dir
	}
	return r.Root
}

// moduleInstallLabel renders an install directory relative to the project root
// so the report stays readable.
func moduleInstallLabel(root, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." {
		return "the project root"
	}
	return filepath.ToSlash(rel)
}

// containsString reports whether values holds target.
func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

func pmLabel(pm string) string {
	if pm == "" {
		return "the package manager"
	}
	return pm
}

// defaultPlatforms returns the reference platform support (web only).
func defaultPlatforms() module.Platforms {
	return module.Platforms{
		Web:     module.Platform{Supported: true, Modes: []string{"web"}},
		Desktop: module.Platform{Supported: false},
		Mobile:  module.Platform{Supported: false},
	}
}

// manifestArrayFields are the manifest fields that must be JSON arrays; a
// malformed value (object, string, …) is normalized to an empty array so the
// manifest can be decoded and re-saved.
var manifestArrayFields = []string{"permissions", "apiScopes", "widgets", "routines", "providers"}

// manifestFile is the raw JSON a manifest was read from: its decoded top-level
// fields and the key set they came from. The typed Manifest cannot answer
// "is this field declared, and does it hold the right shape?" — an absent
// field and a malformed one both decode to the same zero value — so the repairs
// that must not overwrite existing data read it from here.
type manifestFile struct {
	raw        map[string]json.RawMessage
	normalized bool
}

// has reports whether the file declares the top-level key.
func (mf manifestFile) has(key string) bool {
	_, ok := mf.raw[key]
	return ok
}

// isArray reports whether the file declares the key and holds a JSON array.
func (mf manifestFile) isArray(key string) bool {
	v, ok := mf.raw[key]
	if !ok {
		return false
	}
	var arr []json.RawMessage
	return json.Unmarshal(v, &arr) == nil
}

// loadManifestLenient decodes a manifest, normalizing the known array fields
// when they hold a non-array value. It reports whether a normalization was
// needed (so the caller can persist the cleaned manifest).
func loadManifestLenient(path string) (*module.Manifest, manifestFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, manifestFile{}, err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, manifestFile{}, err
	}
	mf := manifestFile{raw: raw}

	m := &module.Manifest{}
	if err := json.Unmarshal(data, m); err == nil {
		return m, mf, nil
	}

	for _, key := range manifestArrayFields {
		v, ok := raw[key]
		if !ok {
			continue
		}
		var arr []json.RawMessage
		if json.Unmarshal(v, &arr) != nil {
			raw[key] = json.RawMessage("[]")
			mf.normalized = true
		}
	}

	rebuilt, err := json.Marshal(raw)
	if err != nil {
		return nil, manifestFile{}, err
	}
	if err := json.Unmarshal(rebuilt, m); err != nil {
		return nil, manifestFile{}, err
	}
	return m, mf, nil
}

// manifestKeyOrder lists the canonical field order of a serialized manifest, so
// a sparse write keeps the field order of the struct and appends the preserved
// extension fields (schema additionalProperties) after it, sorted.
var manifestKeyOrder = func() []string {
	t := reflect.TypeOf(module.Manifest{})
	keys := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		name := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			keys = append(keys, name)
		}
	}
	return keys
}()

// saveManifestSparse writes the manifest, keeping only the fields the file
// already declared or that a repair actually changed.
//
// A plain re-serialization would inject the zero value of every absent field —
// an empty `publisher` block, `isEnabled: false`, `null` arrays — adding noise
// the developer never asked for. Worse, an injected empty `publisher` makes the
// audit raise the very warning repair is meant to resolve, and no repair can
// ever clear it.
func saveManifestSparse(m *module.Manifest, path string, mf manifestFile, changed map[string]bool) error {
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("failed to serialize manifest: %w", err)
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}

	// Split the serialized fields into the canonical order and the preserved
	// extension fields, keeping the value of each field that survives.
	kept := map[string]json.RawMessage{}
	keys := make([]string, 0, len(fields))
	for _, k := range manifestKeyOrder {
		v, ok := fields[k]
		if !ok {
			continue
		}
		delete(fields, k)
		if keepManifestKey(k, v, mf, changed) {
			keys = append(keys, k)
			kept[k] = v
		}
	}
	extra := make([]string, 0, len(fields))
	for k := range fields {
		extra = append(extra, k)
	}
	sort.Strings(extra)
	for _, k := range extra {
		if keepManifestKey(k, fields[k], mf, changed) {
			keys = append(keys, k)
			kept[k] = fields[k]
		}
	}

	// Assemble the object by hand to keep the canonical field order, then let
	// json.Indent restore the nested pretty-printing a plain Marshal would
	// flatten.
	var compact bytes.Buffer
	compact.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			compact.WriteByte(',')
		}
		keyJSON, err := json.Marshal(k)
		if err != nil {
			return err
		}
		compact.Write(keyJSON)
		compact.WriteByte(':')
		compact.Write(kept[k])
	}
	compact.WriteByte('}')

	var out bytes.Buffer
	if err := json.Indent(&out, compact.Bytes(), "", "  "); err != nil {
		return err
	}
	out.WriteByte('\n')
	return pkg.WriteFile(path, out.Bytes())
}

// keepManifestKey reports whether a serialized field belongs in the rewritten
// manifest: one the file already had, one a repair just changed, or one holding
// a value worth writing even though the file lacked it.
func keepManifestKey(key string, v json.RawMessage, mf manifestFile, changed map[string]bool) bool {
	if mf.has(key) || changed[key] {
		return true
	}
	return !isEmptyJSONValue(v)
}

// isEmptyJSONValue reports whether a serialized value carries no information:
// the zero value a repair would otherwise inject for an absent field. A
// container counts as empty when all of its members are, so a zero-valued
// nested struct (menu.items: null) is recognized too.
func isEmptyJSONValue(v json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(v))
	switch trimmed {
	case "null", "false", `""`, "{}", "[]":
		return true
	}
	// A numeric zero written for an absent field states nothing the decoded
	// manifest did not already imply (schemaVersion: 0 rather than 1).
	if f, err := strconv.ParseFloat(trimmed, 64); err == nil {
		return f == 0
	}

	var members map[string]json.RawMessage
	if json.Unmarshal(v, &members) == nil {
		if len(members) == 0 {
			return true
		}
		for _, mv := range members {
			if !isEmptyJSONValue(mv) {
				return false
			}
		}
		return true
	}

	var list []json.RawMessage
	if json.Unmarshal(v, &list) == nil {
		for _, ev := range list {
			if !isEmptyJSONValue(ev) {
				return false
			}
		}
		return true
	}
	return false
}

// changedKeys returns the top-level fields whose serialized value differs
// between two marshalled manifests — the fields the repairs actually touched.
func changedKeys(before, after []byte) map[string]bool {
	out := map[string]bool{}
	var b, a map[string]json.RawMessage
	if json.Unmarshal(before, &b) != nil || json.Unmarshal(after, &a) != nil {
		return out
	}
	for k, av := range a {
		if bv, ok := b[k]; !ok || string(bv) != string(av) {
			out[k] = true
		}
	}
	return out
}

// remainingFindings keeps the non-OK findings still in scope (errors always,
// warnings only when includeWarnings is set).
func remainingFindings(findings []module.Finding, includeWarnings bool) []module.Finding {
	out := []module.Finding{}
	for _, f := range findings {
		if f.Severity == module.LevelOK {
			continue
		}
		if f.Severity == module.LevelWarning && !includeWarnings {
			continue
		}
		out = append(out, f)
	}
	return out
}

// instructionForFinding maps an unfixable finding to a developer instruction.
func instructionForFinding(f module.Finding, name string) *Instruction {
	switch f.Category {
	case "manifest.json":
		switch f.Rule {
		case "publisher":
			// The publisher is optional in the canonical schema and completed
			// at publish time, so only a half-filled block is worth reporting.
			return newInstruction("manifest.json", "publisher",
				"the publisher block is incomplete",
				"Open library/modules/"+name+"/manifest.json.",
				"Set publisher.id (your developer identifier) and publisher.name, or remove the block entirely.",
				"Re-run 'liora repair "+name+"' to verify.")
		case "lecture":
			return newInstruction("manifest.json", "lecture",
				"manifest.json could not be read",
				"Open library/modules/"+name+"/manifest.json.",
				"Fix the JSON syntax or type error reported by the audit.",
				"Re-run 'liora repair "+name+"'.")
		case "entry":
			return newInstruction("manifest.json", "entry",
				"the module entry file is missing",
				"Create library/modules/"+name+"/index.tsx (the module declaration).",
				"Set manifest.json \"entry\" to \"index.tsx\".")
		case "domain":
			return newInstruction("manifest.json", "domain",
				"the domain is not a valid reverse-DNS dotted name",
				"Open library/modules/"+name+"/manifest.json.",
				"Set \"domain\" to a reverse-DNS name, e.g. com.organization."+name+".",
				"Re-run 'liora repair "+name+"' to verify.")
		case "domain directory":
			return newInstruction("manifest.json", "domain directory",
				"the module directory does not match the manifest domain",
				"Rename the module directory to its domain: 'git mv library/modules/"+name+" library/modules/<domain>'.",
				"Update the 'identifier' in index.tsx and every import referencing the old path.",
				"Re-run 'liora repair'.")
		}
	case "index.tsx":
		switch f.Rule {
		case "export":
			return newInstruction("index.tsx", "export",
				"index.tsx has no default export",
				"Open library/modules/"+name+"/index.tsx.",
				"Export the module declaration as default (e.g. 'export default myModule;').")
		case "declaration":
			return newInstruction("index.tsx", "declaration",
				"the module declaration is incomplete",
				"Open library/modules/"+name+"/index.tsx.",
				"Declare the module with 'identifier:' (the module domain) and 'widgets:'.")
		}
	case "Clean Architecture":
		switch f.Rule {
		case "components→services":
			file := trimSuffix(f.Message, " imports services directly")
			return newInstruction("Clean Architecture", f.Rule,
				f.Message,
				"Open library/modules/"+name+"/"+file+".",
				"Move the data access behind an application service.",
				"Consume the service through a hook or a provider instead of importing it directly.")
		case "services→JSX":
			file := trimSuffix(f.Message, " contains JSX in a service")
			return newInstruction("Clean Architecture", f.Rule,
				f.Message,
				"Open library/modules/"+name+"/"+file+".",
				"Move the JSX markup into a presentation component.",
				"Keep the service free of any markup.")
		}
	case "requirements":
		return newInstruction("requirements", f.Rule,
			fmt.Sprintf("required module %q is not available", f.Rule),
			"Install or create the module "+f.Rule+" (e.g. 'liora marketplace install "+f.Rule+"').",
			"It must exist under library/modules/ or src/modules/ (platform core modules are always satisfied).")
	case "dependencies":
		return newInstruction("dependencies", f.Rule,
			fmt.Sprintf("dependency %q is not installed", f.Rule),
			"Run the project package manager install at the project root.",
			"Verify that node_modules/"+f.Rule+" exists.")
	case "assets":
		return newInstruction("assets", f.Rule,
			"the assets directory is empty",
			"Add the module assets under public/assets/"+name+"/.",
			"Or remove the empty public/assets/"+name+"/ directory.")
	}
	return newInstruction(f.Category, f.Rule, f.Message,
		"Inspect the finding and fix it manually.",
		"Re-run 'liora repair "+name+"' to verify.")
}

func newInstruction(category, rule, message string, steps ...string) *Instruction {
	return &Instruction{Category: category, Rule: rule, Message: message, Steps: steps}
}

// deriveID builds a non-empty identifier from the module directory, domain or
// display name (the validator only requires the id to be present).
func deriveID(name, domain, display string) string {
	if id := domainSuffix(domain); id != "" {
		return id
	}
	if i := strings.LastIndex(name, "."); i >= 0 {
		if id := kebab(name[i+1:]); id != "" {
			return id
		}
	}
	if id := kebab(name); id != "" {
		return id
	}
	if id := kebab(display); id != "" {
		return id
	}
	return "module"
}

// domainSuffix returns the kebab-case last label of a dotted domain, or "".
func domainSuffix(domain string) string {
	if i := strings.LastIndex(domain, "."); i >= 0 {
		return kebab(domain[i+1:])
	}
	return ""
}

// kebab lowercases s and replaces every run of non-alphanumeric characters
// with a single hyphen.
func kebab(s string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		default:
			if !prevHyphen && b.Len() > 0 {
				b.WriteByte('-')
				prevHyphen = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func trimSuffix(s, suffix string) string {
	return strings.TrimSpace(strings.TrimSuffix(s, suffix))
}

var (
	kebabNameRE  = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	domainNameRE = regexp.MustCompile(
		`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)
)

const (
	// typeConfiguration is the distribution type of a module that declares its
	// UI in the manifest instead of shipping a React entry.
	typeConfiguration = "CONFIGURATION"
	// configurationEntryFile is the declarative page a CONFIGURATION module
	// points its entry at.
	configurationEntryFile = "index.json"
	// openRangeMin is the lower bound of a compatibility window with no known
	// floor: it excludes nothing, where an invented upper bound could wrongly
	// exclude a version the module does support.
	openRangeMin = "0.0.0"
)

// isDomainName reports whether s is a valid reverse-DNS dotted domain.
func isDomainName(s string) bool {
	return domainNameRE.MatchString(s)
}
