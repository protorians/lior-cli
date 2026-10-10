package module

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
)

// ModuleHelpersFileName is the optional assistive-help file a module may carry
// at its root (spec `assistive-help`, `docs/specs/modules/module-types/features/
// assistive-help.spec.md`). The socle auto-discovers it and the packer embeds it
// under `src/module.helpers.json`; it is never referenced from the signed
// manifest. Absent, the module simply offers no help.
const ModuleHelpersFileName = "module.helpers.json"

// moduleHelpersMaxContent is the maximum length of an anchor/step `content`,
// mirroring `MAX_CONTENT_LENGTH` of `assistive-help.types.ts` (and the
// `module.helpers.schema.json` `content` definition).
const moduleHelpersMaxContent = 2000

// moduleHelpersIdentifierRE matches the kebab-case identifier of an anchor or a
// tour (`^[a-z0-9]+(?:-[a-z0-9]+)*$`).
var moduleHelpersIdentifierRE = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// moduleHelpersTriggers/Sides/Aligns are the closed value sets of the schema.
var (
	moduleHelpersTriggers = map[string]bool{"hover": true, "click": true, "always": true}
	moduleHelpersSides    = map[string]bool{"top": true, "bottom": true, "left": true, "right": true}
	moduleHelpersAligns   = map[string]bool{"start": true, "center": true, "end": true}
)

// ModuleHelpersFile is the parsed content of module.helpers.json (version 1).
// It mirrors `AssistiveHelpModuleFileInterface` of the SDK.
type ModuleHelpersFile struct {
	Version int                   `json:"version"`
	Tours   []ModuleHelpersTour   `json:"tours,omitempty"`
	Anchors []ModuleHelpersAnchor `json:"anchors,omitempty"`
}

// ModuleHelpersTour is a guided, step-by-step tour.
type ModuleHelpersTour struct {
	ID          string              `json:"id"`
	Title       string              `json:"title"`
	Description string              `json:"description,omitempty"`
	Steps       []ModuleHelpersStep `json:"steps"`
	Beacon      *bool               `json:"beacon,omitempty"`
	Persist     *bool               `json:"persist,omitempty"`
}

// ModuleHelpersStep is one step of a tour. `content` is mandatory; `target` is
// optional (an absent target centres the step).
type ModuleHelpersStep struct {
	Target  string `json:"target,omitempty"`
	Title   string `json:"title,omitempty"`
	Content string `json:"content"`
	Side    string `json:"side,omitempty"`
	Align   string `json:"align,omitempty"`
	Section string `json:"section,omitempty"`
}

// ModuleHelpersAnchor is a description anchored to an element of the host
// document. `id`, `target` and `content` are mandatory.
type ModuleHelpersAnchor struct {
	ID      string `json:"id"`
	Target  string `json:"target"`
	Title   string `json:"title,omitempty"`
	Content string `json:"content"`
	Trigger string `json:"trigger,omitempty"`
	Side    string `json:"side,omitempty"`
}

// ModuleHelpersPath resolves the module.helpers.json of a module directory,
// trying the workspace source layout (module root) first, then the
// distribution layout (`src/`, where the packer stores the sources). It returns
// "" when the module carries no such file.
func ModuleHelpersPath(moduleDir string) string {
	for _, rel := range []string{ModuleHelpersFileName, filepath.Join("src", ModuleHelpersFileName)} {
		p := filepath.Join(moduleDir, rel)
		if pkg.FileExists(p) {
			return p
		}
	}
	return ""
}

// LoadModuleHelpersFile reads and parses a module.helpers.json. A malformed
// document (invalid JSON, or a value of the wrong type) is refused — the whole
// file is rejected rather than partially read (fail-closed).
func LoadModuleHelpersFile(path string) (*ModuleHelpersFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New(i18n.Tf("module.helpers.error.unreadable", err.Error()))
	}
	var file ModuleHelpersFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, errors.New(i18n.Tf("module.helpers.error.unreadable", err.Error()))
	}
	return &file, nil
}

// ValidateModuleHelpersFile applies the fail-closed contract of the assistive
// help standard, mirroring `validateModuleHelpersFile` of the SDK: `version`
// must be 1, every declared tour/anchor must be compliant, and identifiers must
// be unique across tours and anchors. It returns the list of problems (empty
// when the file is valid).
func ValidateModuleHelpersFile(file *ModuleHelpersFile) []string {
	if file == nil {
		return []string{i18n.T("module.helpers.error.object")}
	}
	var errs []string
	if file.Version != 1 {
		errs = append(errs, i18n.Tf("module.helpers.error.version", file.Version))
	}
	for i := range file.Tours {
		validateHelpersTour(&file.Tours[i], fmt.Sprintf("tours[%d]", i), &errs)
	}
	for i := range file.Anchors {
		validateHelpersAnchor(&file.Anchors[i], fmt.Sprintf("anchors[%d]", i), &errs)
	}

	// Uniqueness across tours and anchors: two homonymous entries would make the
	// help menu ambiguous and the resolution non-deterministic.
	ids := make([]string, 0, len(file.Tours)+len(file.Anchors))
	for _, tour := range file.Tours {
		if tour.ID != "" {
			ids = append(ids, tour.ID)
		}
	}
	for _, anchor := range file.Anchors {
		if anchor.ID != "" {
			ids = append(ids, anchor.ID)
		}
	}
	counts := map[string]int{}
	for _, id := range ids {
		counts[id]++
	}
	reported := map[string]bool{}
	for _, id := range ids {
		if counts[id] > 1 && !reported[id] {
			reported[id] = true
			errs = append(errs, i18n.Tf("module.helpers.error.duplicate", id))
		}
	}
	return errs
}

// validateHelpersTour checks one tour and its steps.
func validateHelpersTour(tour *ModuleHelpersTour, context string, errs *[]string) {
	if !moduleHelpersIdentifierRE.MatchString(tour.ID) {
		*errs = append(*errs, i18n.Tf("module.helpers.error.identifier", context, "id"))
	}
	if strings.TrimSpace(tour.Title) == "" {
		*errs = append(*errs, i18n.Tf("module.helpers.error.required", context, "title"))
	}
	if len(tour.Steps) == 0 {
		*errs = append(*errs, i18n.Tf("module.helpers.error.steps", context))
		return
	}
	for i := range tour.Steps {
		validateHelpersStep(&tour.Steps[i], fmt.Sprintf("%s.steps[%d]", context, i), errs)
	}
}

// validateHelpersStep checks one step. `content` is mandatory; the optional
// `target` may not be blank when present; `side`/`align` are closed sets.
func validateHelpersStep(step *ModuleHelpersStep, context string, errs *[]string) {
	if strings.TrimSpace(step.Content) == "" {
		*errs = append(*errs, i18n.Tf("module.helpers.error.required", context, "content"))
	} else if len(step.Content) > moduleHelpersMaxContent {
		*errs = append(*errs, i18n.Tf("module.helpers.error.content_length", context, moduleHelpersMaxContent))
	}
	if step.Side != "" && !moduleHelpersSides[step.Side] {
		*errs = append(*errs, i18n.Tf("module.helpers.error.enum", context, "side", step.Side))
	}
	if step.Align != "" && !moduleHelpersAligns[step.Align] {
		*errs = append(*errs, i18n.Tf("module.helpers.error.enum", context, "align", step.Align))
	}
}

// validateHelpersAnchor checks one anchor.
func validateHelpersAnchor(anchor *ModuleHelpersAnchor, context string, errs *[]string) {
	if !moduleHelpersIdentifierRE.MatchString(anchor.ID) {
		*errs = append(*errs, i18n.Tf("module.helpers.error.identifier", context, "id"))
	}
	if strings.TrimSpace(anchor.Target) == "" {
		*errs = append(*errs, i18n.Tf("module.helpers.error.required", context, "target"))
	}
	if strings.TrimSpace(anchor.Content) == "" {
		*errs = append(*errs, i18n.Tf("module.helpers.error.required", context, "content"))
	} else if len(anchor.Content) > moduleHelpersMaxContent {
		*errs = append(*errs, i18n.Tf("module.helpers.error.content_length", context, moduleHelpersMaxContent))
	}
	if anchor.Trigger != "" && !moduleHelpersTriggers[anchor.Trigger] {
		*errs = append(*errs, i18n.Tf("module.helpers.error.enum", context, "trigger", anchor.Trigger))
	}
	if anchor.Side != "" && !moduleHelpersSides[anchor.Side] {
		*errs = append(*errs, i18n.Tf("module.helpers.error.enum", context, "side", anchor.Side))
	}
}
