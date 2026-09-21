// Package repair automatically fixes the auto-repairable findings of a module
// audit (`liorian audit`) and reports developer instructions for the findings
// that require a manual code change.
//
// The engine reuses the audit pipeline: it runs the same checks, applies the
// safe metadata repairs (manifest fields, missing npm dependencies, malformed
// manifest JSON), then re-audits the module to report what is left. Every
// finding that cannot be fixed automatically is turned into a step-by-step
// instruction for the developer.
package repair

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	instructed := map[string]map[string]bool{}

	for _, mod := range initial.Modules {
		mr := r.repairMetadata(mod.Module, mod.Findings, pendingInstall)
		r.applyRename(mr, mod.Module, pendingInstall)
		result.Modules = append(result.Modules, *mr)
	}

	r.installDependencies(result, pendingInstall)

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
		keys := instructed[mod]
		if keys == nil {
			keys = map[string]bool{}
		}
		for _, f := range remaining {
			key := f.Category + "|" + f.Rule
			if keys[key] {
				continue
			}
			result.Modules[i].Instructions = append(result.Modules[i].Instructions, *instructionForFinding(f, mod))
			keys[key] = true
		}
		instructed[mod] = keys
	}

	return result, nil
}

// repairMetadata loads the module manifest, applies every auto-repairable
// finding and saves it. It flags the module when a dependency install is
// needed (handled globally by installDependencies).
func (r *Repairer) repairMetadata(name string, findings []module.Finding, pendingInstall map[string]bool) *ModuleResult {
	mr := &ModuleResult{Module: name}
	moduleDir := filepath.Join(r.Root, config.ExternalModulesDir, name)
	manifestPath := filepath.Join(moduleDir, config.ManifestFileName)

	m, normalized, err := loadManifestLenient(manifestPath)
	if err != nil {
		mr.Instructions = append(mr.Instructions, *instructionForFinding(module.Finding{
			Category: "manifest.json",
			Rule:     "lecture",
			Severity: module.LevelError,
			Message:  err.Error(),
		}, name))
		return mr
	}

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
		action, instruction, ch := r.applyFinding(m, moduleDir, name, f)
		if ch {
			changed = true
		}
		if instruction != nil {
			mr.Instructions = append(mr.Instructions, *instruction)
			continue
		}
		if !ch {
			// Another repair already satisfied this finding.
			continue
		}
		mr.Actions = append(mr.Actions, action)
	}

	if changed && !r.DryRun {
		if err := m.Save(manifestPath); err != nil {
			mr.Instructions = append(mr.Instructions, *instructionForFinding(module.Finding{
				Category: "manifest.json",
				Rule:     "save",
				Severity: module.LevelError,
				Message:  err.Error(),
			}, name))
			return mr
		}
	}

	applied := changed && !r.DryRun
	for i := range mr.Actions {
		mr.Actions[i].Applied = applied
	}
	return mr
}

// applyRename fixes the "domain directory" finding by renaming the module
// directory to its manifest domain (or vice versa). The new name is proposed in
// the prompt (tab fills it), applied directly when the developer accepts or when
// the run is non-interactive, and the module result — along with the pending
// dependency installs — is remapped to the new name.
func (r *Repairer) applyRename(mr *ModuleResult, name string, pendingInstall map[string]bool) {
	domain := manifestDomain(filepath.Join(r.Root, config.ExternalModulesDir, name))
	req, ok := planRename(name, domain)
	if !ok {
		return
	}
	mr.Actions = dropAction(mr.Actions, "manifest.json", "domain directory")
	mr.Instructions = dropInstruction(mr.Instructions, "manifest.json", "domain directory")

	if r.DryRun {
		mr.Actions = append(mr.Actions, Action{
			Category: "manifest.json",
			Rule:     "domain directory",
			Detail:   fmt.Sprintf("would rename the module directory to %q", req.to),
		})
		return
	}

	target := req.to
	if r.Rename != nil && !r.NoInteraction {
		value, accept := r.Rename(i18n.Tf("repair.rename.prompt", req.from), req.to)
		if !accept {
			mr.Instructions = append(mr.Instructions, *instructionForFinding(module.Finding{
				Category: "manifest.json", Rule: req.rule, Severity: module.LevelError,
				Message: i18n.T("repair.rename.detected"),
			}, req.from))
			return
		}
		if strings.TrimSpace(value) != "" {
			target = strings.TrimSpace(value)
		}
	}

	if err := renameModuleDir(r.Root, req.from, target); err != nil {
		mr.Instructions = append(mr.Instructions, *instructionForFinding(module.Finding{
			Category: "manifest.json", Rule: req.rule, Severity: module.LevelError,
			Message: err.Error(),
		}, req.from))
		return
	}

	if pendingInstall[name] {
		delete(pendingInstall, name)
		pendingInstall[target] = true
	}
	mr.Module = target
	mr.Actions = append(mr.Actions, Action{
		Category: "manifest.json",
		Rule:     "domain directory",
		Detail:   fmt.Sprintf("renamed the module directory to %q", target),
		Applied:  true,
	})
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

// manifestDomain loads a module manifest domain, "" when it cannot be read.
func manifestDomain(moduleDir string) string {
	m, err := module.LoadManifest(filepath.Join(moduleDir, config.ManifestFileName))
	if err != nil {
		return ""
	}
	return m.Domain
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
func (r *Repairer) applyFinding(m *module.Manifest, moduleDir, name string, f module.Finding) (Action, *Instruction, bool) {
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
			if pkg.FileExists(filepath.Join(moduleDir, config.ModuleEntryFileName)) {
				m.Entry = config.ModuleEntryFileName
				act.Detail = fmt.Sprintf("set entry to %q", m.Entry)
				return act, nil, true
			}
			return act, instructionForFinding(f, name), false
		case "permissions":
			m.Permissions = []string{}
			act.Detail = "normalized permissions to an array"
			return act, nil, true
		case "optionalRequirements":
			m.OptionalRequirements = map[string]string{}
			act.Detail = "added optionalRequirements"
			return act, nil, true
		case "platforms":
			m.Platforms = defaultPlatforms()
			act.Detail = "added default platforms (web)"
			return act, nil, true
		case "managerCompatibility":
			m.ManagerCompat = module.Compatibility{Min: "0.0.0"}
			act.Detail = "added managerCompatibility"
			return act, nil, true
		case "apiCompatibility":
			m.APICompat = module.Compatibility{Min: "0.0.0"}
			act.Detail = "added apiCompatibility"
			return act, nil, true
		case "capabilities":
			m.Capabilities = module.Capabilities{NeedsNetwork: true, RequiresAuthenticatedUser: true}
			act.Detail = "added capabilities"
			return act, nil, true
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

// installDependencies runs the detected package manager once to install the
// dependencies flagged by the audit, then records the action for each module
// that needed it.
func (r *Repairer) installDependencies(result *RepairResult, pending map[string]bool) {
	if len(pending) == 0 {
		return
	}
	pm := pkg.DetectPackageManager()

	ran := false
	var runErr error
	if !r.DryRun && !r.SkipInstall && pm != "" {
		runErr = pkg.StreamCommandIn(r.Root, pm, "install")
		ran = runErr == nil
	}

	for i := range result.Modules {
		name := result.Modules[i].Module
		if !pending[name] {
			continue
		}
		switch {
		case r.DryRun:
			result.Modules[i].Actions = append(result.Modules[i].Actions, Action{
				Category: "dependencies",
				Rule:     "install",
				Detail:   fmt.Sprintf("would run %s install", pmLabel(pm)),
			})
		case r.SkipInstall || pm == "":
			// Left to the re-audit pass, which reports the remaining
			// dependency findings as manual instructions.
		case ran:
			result.Modules[i].Actions = append(result.Modules[i].Actions, Action{
				Category: "dependencies",
				Rule:     "install",
				Detail:   fmt.Sprintf("ran %s install", pm),
				Applied:  true,
			})
		default:
			result.Modules[i].Instructions = append(result.Modules[i].Instructions, *instructionForFinding(module.Finding{
				Category: "dependencies",
				Rule:     "install",
				Severity: module.LevelError,
				Message:  runErr.Error(),
			}, name))
		}
	}
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

// loadManifestLenient decodes a manifest, normalizing the known array fields
// when they hold a non-array value. It reports whether a normalization was
// needed (so the caller can persist the cleaned manifest).
func loadManifestLenient(path string) (*module.Manifest, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, false, err
	}

	m := &module.Manifest{}
	if err := json.Unmarshal(data, m); err == nil {
		return m, false, nil
	}

	normalized := false
	for _, key := range manifestArrayFields {
		v, ok := raw[key]
		if !ok {
			continue
		}
		var arr []json.RawMessage
		if json.Unmarshal(v, &arr) != nil {
			raw[key] = json.RawMessage("[]")
			normalized = true
		}
	}

	rebuilt, err := json.Marshal(raw)
	if err != nil {
		return nil, false, err
	}
	if err := json.Unmarshal(rebuilt, m); err != nil {
		return nil, false, err
	}
	return m, normalized, nil
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

// instructionKeys indexes the manual instructions by "category|rule".
func instructionKeys(instructions []Instruction) map[string]bool {
	keys := map[string]bool{}
	for _, ins := range instructions {
		keys[ins.Category+"|"+ins.Rule] = true
	}
	return keys
}

// instructionForFinding maps an unfixable finding to a developer instruction.
func instructionForFinding(f module.Finding, name string) *Instruction {
	switch f.Category {
	case "manifest.json":
		switch f.Rule {
		case "publisher":
			return newInstruction("manifest.json", "publisher",
				"the publisher identity is missing",
				"Open library/modules/"+name+"/manifest.json.",
				"Set publisher.id (your developer identifier) and publisher.name.",
				"Re-run 'liorian repair "+name+"' to verify.")
		case "lecture":
			return newInstruction("manifest.json", "lecture",
				"manifest.json could not be read",
				"Open library/modules/"+name+"/manifest.json.",
				"Fix the JSON syntax or type error reported by the audit.",
				"Re-run 'liorian repair "+name+"'.")
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
				"Re-run 'liorian repair "+name+"' to verify.")
		case "domain directory":
			return newInstruction("manifest.json", "domain directory",
				"the module directory does not match the manifest domain",
				"Rename the module directory to its domain: 'git mv library/modules/"+name+" library/modules/<domain>'.",
				"Update the 'identifier' in index.tsx and every import referencing the old path.",
				"Re-run 'liorian repair'.")
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
			"Install or create the module "+f.Rule+" (e.g. 'liorian marketplace install "+f.Rule+"').",
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
		"Re-run 'liorian repair "+name+"' to verify.")
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

// isDomainName reports whether s is a valid reverse-DNS dotted domain.
func isDomainName(s string) bool {
	return domainNameRE.MatchString(s)
}
