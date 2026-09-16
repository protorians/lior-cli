package module

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/protorians/sentient-cli/internal/i18n"
	"github.com/protorians/sentient-cli/internal/pkg"
)

// Manifest is the metadata file of a Sentient module (`manifest.json`).
//
// The canonical schema (`@sentients/sdk/schemas/module.schema.json`) admits
// additional properties: unknown top-level fields are preserved in Extra and
// re-emitted on Marshal so a round-trip never loses forward-compatible data.
type Manifest struct {
	Schema               string            `json:"$schema,omitempty"`
	SchemaVersion        int               `json:"schemaVersion"`
	ID                   string            `json:"id"`
	Domain               string            `json:"domain"`
	Key                  string            `json:"key"`
	Name                 string            `json:"name"`
	Description          string            `json:"description"`
	Version              string            `json:"version"`
	Icon                 string            `json:"icon"`
	Logo                 *string           `json:"logo,omitempty"`
	Banner               *string           `json:"banner,omitempty"`
	Type                 string            `json:"type"`
	Entry                string            `json:"entry"`
	URI                  string            `json:"uri"`
	Category             string            `json:"category,omitempty"`
	Token                string            `json:"token"`
	Publisher            Publisher         `json:"publisher"`
	Platforms            Platforms         `json:"platforms"`
	ManagerCompat        Compatibility     `json:"managerCompatibility"`
	APICompat            Compatibility     `json:"apiCompatibility"`
	Permissions          []string          `json:"permissions"`
	APIScopes            []string          `json:"apiScopes"`
	Capabilities         Capabilities      `json:"capabilities"`
	IsEnabled            bool              `json:"isEnabled"`
	IsDefault            bool              `json:"isDefault"`
	Requirements         map[string]any    `json:"requirements"`
	OptionalRequirements map[string]string `json:"optionalRequirements"`
	Dependencies         map[string]string `json:"dependencies"`
	DevDependencies      map[string]string `json:"devDependencies,omitempty"`
	Widgets              []string          `json:"widgets"`
	Routines             []string          `json:"routines"`
	Providers            []string          `json:"providers,omitempty"`
	ConfigSettings       []ConfigSetting   `json:"configSettings,omitempty"`
	Menu                 Menu              `json:"menu"`

	// Extra preserves unknown top-level fields (schema additionalProperties).
	Extra map[string]json.RawMessage `json:"-"`
}

// Publisher describes the developer publishing the module.
type Publisher struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url,omitempty"`
	Email       string `json:"email,omitempty"`
	Description string `json:"description,omitempty"`
	Avatar      string `json:"avatar,omitempty"`
}

// Platforms declares which platforms the module supports.
type Platforms struct {
	Web     Platform `json:"web"`
	Desktop Platform `json:"desktop"`
	Mobile  Platform `json:"mobile"`
}

// Platform describes support for a single platform.
type Platform struct {
	Supported    bool     `json:"supported"`
	Modes        []string `json:"modes,omitempty"`
	OS           []string `json:"os,omitempty"`
	IOSSupported *bool    `json:"iosSupported,omitempty"`
	MinOSVersion string   `json:"minOsVersion,omitempty"`
}

// Compatibility expresses a semver window. A complete `max` range is required
// by the schema (e.g. `0.17.x` rather than `0.17.0`); `strict` turns an out-of-
// range version into a hard failure rather than a warning.
type Compatibility struct {
	Min    string `json:"min"`
	Max    string `json:"max,omitempty"`
	Strict bool   `json:"strict,omitempty"`
}

// Capabilities declares module capabilities.
type Capabilities struct {
	NeedsNetwork              bool `json:"needsNetwork"`
	SupportsOffline           bool `json:"supportsOffline"`
	RequiresOrganization      bool `json:"requiresOrganization"`
	RequiresAuthenticatedUser bool `json:"requiresAuthenticatedUser"`
	RequiresAdmin             bool `json:"requiresAdmin,omitempty"`
	SupportsRealtime          bool `json:"supportsRealtime,omitempty"`
	ProcessesLocalData        bool `json:"processesLocalData,omitempty"`
}

// Menu holds menu entries declared by the module.
type Menu struct {
	Items    []MenuItem `json:"items"`
	Dropdown string     `json:"dropdown,omitempty"`
}

// MenuItem is a single entry of the module menu. The module declaration and
// the reference mockups use the `url` field; the legacy CLI-generated manifests
// used `uri`, still read for compatibility.
type MenuItem struct {
	ID          string     `json:"id,omitempty"`
	Label       string     `json:"label"`
	Description string     `json:"description,omitempty"`
	Icon        string     `json:"icon,omitempty"`
	URL         string     `json:"url,omitempty"`
	URI         string     `json:"uri,omitempty"`
	Target      string     `json:"target,omitempty"`
	Keywords    []string   `json:"keywords,omitempty"`
	Items       []MenuItem `json:"items,omitempty"`
	Separator   bool       `json:"separator,omitempty"`
}

// ConfigSetting is a configurable module parameter surfaced by the store.
type ConfigSetting struct {
	Key          string         `json:"key"`
	Label        string         `json:"label"`
	Description  string         `json:"description,omitempty"`
	Type         string         `json:"type,omitempty"`
	Required     bool           `json:"required,omitempty"`
	Placeholder  string         `json:"placeholder,omitempty"`
	DefaultValue string         `json:"defaultValue,omitempty"`
	Options      []ConfigOption `json:"options,omitempty"`
}

// ConfigOption is one choice of a SELECT config setting.
type ConfigOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// manifestKnownKeys lists the JSON tags owned by Manifest, used to split parsed
// top-level fields between the typed struct and the Extra extension bag.
var manifestKnownKeys = func() map[string]bool {
	keys := map[string]bool{}
	t := reflect.TypeOf(Manifest{})
	for i := 0; i < t.NumField(); i++ {
		name := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			keys[name] = true
		}
	}
	return keys
}()

// UnmarshalJSON decodes a manifest and preserves unknown top-level fields.
func (m *Manifest) UnmarshalJSON(data []byte) error {
	type alias Manifest
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	extra := map[string]json.RawMessage{}
	for k, v := range raw {
		if !manifestKnownKeys[k] {
			extra[k] = v
		}
	}
	*m = Manifest(a)
	if len(extra) > 0 {
		m.Extra = extra
	}
	return nil
}

// MarshalJSON serializes the manifest, re-emitting the preserved extension
// fields after the canonical ones.
func (m Manifest) MarshalJSON() ([]byte, error) {
	type alias Manifest
	base, err := json.Marshal(alias(m))
	if err != nil {
		return nil, err
	}
	if len(m.Extra) == 0 {
		return base, nil
	}
	keys := make([]string, 0, len(m.Extra))
	for k := range m.Extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b bytes.Buffer
	trimmed := bytes.TrimSuffix(base, []byte("}"))
	b.Write(trimmed)
	for i, k := range keys {
		if len(trimmed) > 1 || i > 0 {
			b.WriteByte(',')
		}
		keyJSON, _ := json.Marshal(k)
		b.Write(keyJSON)
		b.WriteByte(':')
		b.Write(m.Extra[k])
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// NewManifest builds a fresh manifest for a module.
func NewManifest(name, description string) Manifest {
	upperKey := upperSnake(name)
	return Manifest{
		SchemaVersion: 1,
		ID:            name,
		Domain:        "mod.sentients." + name,
		Key:           upperKey,
		Name:          displayName(name),
		Description:   description,
		Version:       "0.1.0",
		Icon:          "PuzzleIcon",
		Type:          "EXTERNAL",
		Entry:         "index.tsx",
		URI:           "/" + name,
		Category:      "SYSTEM",
		Token:         pkg.NewUUID(),
		Publisher:     Publisher{ID: "", Name: ""},
		Platforms: Platforms{
			Web:     Platform{Supported: true, Modes: []string{"web"}},
			Desktop: Platform{Supported: false},
			Mobile:  Platform{Supported: false},
		},
		ManagerCompat: Compatibility{Min: "0.0.0"},
		APICompat:     Compatibility{Min: "0.0.0"},
		Permissions:   []string{},
		APIScopes:     []string{},
		Capabilities: Capabilities{
			NeedsNetwork:              true,
			SupportsOffline:           false,
			RequiresOrganization:      false,
			RequiresAuthenticatedUser: true,
		},
		IsEnabled:            true,
		IsDefault:            false,
		Requirements:         map[string]any{},
		OptionalRequirements: map[string]string{},
		Dependencies:         map[string]string{"@sentients/sdk": "workspace:*"},
		DevDependencies:      map[string]string{},
		Widgets:              []string{},
		Routines:             []string{},
		Providers:            []string{},
		ConfigSettings:       []ConfigSetting{},
		Menu:                 Menu{Items: []MenuItem{}},
	}
}

// LoadManifest reads and decodes a manifest.json file.
func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to decode JSON of %s: %w", path, err)
	}
	return &m, nil
}

// Save writes the manifest to path with pretty-printed JSON.
func (m *Manifest) Save(path string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize manifest: %w", err)
	}
	data = append(data, '\n')
	return pkg.WriteFile(path, data)
}

var (
	kebabNameRE = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

	// domainRE matches a reverse-DNS dotted module domain
	// (e.g. com.organization.domain): at least two lowercase labels of
	// alphanumerics and hyphens (no leading/trailing hyphen, label ≤ 63 chars).
	domainRE = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)

	// iconRE matches a lucide component name (PascalCase).
	iconRE = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
)

// ValidateName checks a module identifier: kebab-case, 3–64 chars.
func ValidateName(name string) error {
	if len(name) < 3 || len(name) > 64 {
		return errors.New(i18n.T("module.error.name_length"))
	}
	if !kebabNameRE.MatchString(name) {
		return errors.New(i18n.T("module.error.name_kebab"))
	}
	return nil
}

// ValidateDomain checks a module domain: a reverse-DNS dotted form like
// `com.organization.domain`.
func ValidateDomain(domain string) error {
	if !domainRE.MatchString(domain) {
		return errors.New(i18n.T("module.error.domain"))
	}
	return nil
}

// ValidateVersion checks an optional SemVer version. An empty version is
// accepted (the creation default 0.0.0 applies).
func ValidateVersion(version string) error {
	if version == "" || isSemver(version) {
		return nil
	}
	return errors.New(i18n.T("module.error.version"))
}

// ValidateIcon checks an optional lucide icon component name (PascalCase).
func ValidateIcon(icon string) error {
	if icon == "" || iconRE.MatchString(icon) {
		return nil
	}
	return errors.New(i18n.T("module.error.icon"))
}

// ModuleTypes is the set of distribution types accepted by the schema.
var ModuleTypes = map[string]bool{"INTERNAL": true, "EXTERNAL": true}

// ModuleCategories is the set of store categories accepted by the schema.
var ModuleCategories = map[string]bool{
	"ADMINISTRATION": true,
	"COMMERCIAL":     true,
	"FINANCE":        true,
	"OPERATIONS":     true,
	"COMMUNICATION":  true,
	"DATA":           true,
	"AUTOMATION":     true,
	"SYSTEM":         true,
}

// ValidateType checks an optional module distribution type. An empty type is
// accepted (the creation default EXTERNAL applies).
func ValidateType(moduleType string) error {
	if moduleType == "" || ModuleTypes[strings.ToUpper(strings.TrimSpace(moduleType))] {
		return nil
	}
	return errors.New(i18n.T("module.error.type"))
}

// ValidateCategory checks an optional module category. An empty category is
// accepted (the creation default SYSTEM applies).
func ValidateCategory(category string) error {
	if category == "" || ModuleCategories[strings.ToUpper(strings.TrimSpace(category))] {
		return nil
	}
	return errors.New(i18n.T("module.error.category"))
}

// DisplayName returns the Title Case display name of a kebab-case identifier.
func DisplayName(name string) string {
	return displayName(name)
}

func upperSnake(name string) string {
	out := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == '-' {
			out = append(out, '_')
		} else {
			if c >= 'a' && c <= 'z' {
				c = c - 'a' + 'A'
			}
			out = append(out, c)
		}
	}
	return string(out)
}

// displayName converts a kebab-case name to a Title Case display name.
func displayName(name string) string {
	out := make([]byte, 0, len(name))
	start := true
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == '-' {
			out = append(out, ' ')
			start = true
			continue
		}
		if start {
			if c >= 'a' && c <= 'z' {
				c = c - 'a' + 'A'
			}
			start = false
		}
		out = append(out, c)
	}
	return string(out)
}

// pascalName converts a kebab-case name to PascalCase ("blog-manager" → "BlogManager").
func pascalName(name string) string {
	words := strings.Split(name, "-")
	for i, w := range words {
		words[i] = capitalize(w)
	}
	return strings.Join(words, "")
}

// camelName converts a kebab-case name to lower camelCase ("blog-manager" → "blogManager").
func camelName(name string) string {
	words := strings.Split(name, "-")
	out := words[0]
	for _, w := range words[1:] {
		out += capitalize(w)
	}
	return out
}

// lowerName concatenates a kebab-case name without separators ("blog-manager" → "blogmanager").
func lowerName(name string) string {
	return strings.ReplaceAll(name, "-", "")
}

func capitalize(s string) string {
	b := []byte(s)
	if len(b) > 0 && b[0] >= 'a' && b[0] <= 'z' {
		b[0] = b[0] - 'a' + 'A'
	}
	return string(b)
}
