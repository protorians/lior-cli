package module

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
)

// Manifest is the metadata file of a Liora module (`manifest.json`).
//
// The canonical schema (`@liorian/sdk/schemas/module.schema.json`) admits
// additional properties: unknown top-level fields are preserved in Extra and
// re-emitted on Marshal so a round-trip never loses forward-compatible data.
//
// Canonical reference: `docs/modules/module-manifest.md` (workspace) and the
// installation chain (`docs/specs/applications/module-installation.md` §7.5).
// Legacy workspace forms (`managerCompatibility` / `apiCompatibility`,
// `apiScopes`, boolean-object `capabilities`, `INTERNAL` / `EXTERNAL` types)
// are accepted on read and normalized; Marshal emits the canonical form.
type Manifest struct {
	Schema        string               `json:"$schema,omitempty"`
	SchemaVersion int                  `json:"schemaVersion"`
	ID            string               `json:"id"`
	Domain        string               `json:"domain"`
	Key           string               `json:"key"`
	Name          string               `json:"name"`
	Description   string               `json:"description"`
	Version       string               `json:"version"`
	Icon          string               `json:"icon"`
	Logo          *string              `json:"logo,omitempty"`
	Banner        *string              `json:"banner,omitempty"`
	Type          string               `json:"type"`
	External      bool                 `json:"external,omitempty"`
	Entry         string               `json:"entry"`
	URI           string               `json:"uri"`
	Category      string               `json:"category,omitempty"`
	Token         string               `json:"token"`
	Publisher     Publisher            `json:"publisher"`
	Platforms     Platforms            `json:"platforms"`
	Compatibility *ModuleCompatibility `json:"compatibility,omitempty"`
	ManagerCompat Compatibility        `json:"managerCompatibility,omitempty"`
	APICompat     Compatibility        `json:"apiCompatibility,omitempty"`
	Permissions   []string             `json:"permissions"`
	// UserScope declares the user-data permissions a module needs (D15,
	// spec module-isolated-runtime.md §6.10): the consent an end user must
	// grant before the module mounts. Mandatory — `[]` (no user data) is
	// the explicit way to declare a module that reads nothing. Same grammar
	// as `permissions` (`Role:Verbe`), validated at pack time.
	UserScope []string `json:"userScope,omitempty"`
	// Legal declares the legal documents an end user must accept before the
	// module mounts (D16, spec module-isolated-runtime.md §6.11): terms of use,
	// privacy policy, and optionally a licence contract.
	//
	// Optional at pack time — a module without `legal` has no legal obligation
	// and no gate. But once one document is declared, TERMS and PRIVACY are both
	// mandatory and LICENSE may be declared with `required: false`. The content
	// accepts three notations (section array, nested object, Markdown); the
	// server normalizes all three into one canonical form and checksums it, so
	// rewriting a document between notations does not force a re-acceptance.
	Legal []LegalDocument `json:"legal,omitempty"`
	// Backends is the tier-1 egress declaration (D9, §6.2): the third-party
	// backends a module may reach through the api-core gateway. The
	// operator's master list (tier 2) decides separately; the two must
	// match for an egress call to pass.
	Backends     []BackendDeclaration `json:"backends,omitempty"`
	OAuth        *ModuleOAuth         `json:"oauth,omitempty"`
	APIScopes    []string             `json:"apiScopes,omitempty"`
	Capabilities Capabilities         `json:"capabilities"`
	// Artifact declares the executable payload of the module (§4.4): the
	// build output directory and the bundle/document file names relative to
	// the module directory. Mandatory for isolated-runtime modules.
	Artifact             *ArtifactDeclaration `json:"artifact,omitempty"`
	IsEnabled            bool                 `json:"isEnabled"`
	IsDefault            bool                 `json:"isDefault"`
	Requirements         map[string]any       `json:"requirements"`
	OptionalRequirements map[string]string    `json:"optionalRequirements"`
	DataModel            []DataModelResource  `json:"dataModel,omitempty"`
	Declarative          *DeclarativeConfig   `json:"declarative,omitempty"`
	Widgets              []string             `json:"widgets"`
	Routines             []string             `json:"routines"`
	Providers            []string             `json:"providers,omitempty"`
	ConfigSettings       []ConfigSetting      `json:"configSettings,omitempty"`
	Menu                 Menu                 `json:"menu"`

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
//
// Legacy form (`managerCompatibility` / `apiCompatibility`). The canonical
// form is `compatibility: { socle: {...}, api: {...} }` (see
// ModuleCompatibility); both are accepted on read.
type Compatibility struct {
	Min    string `json:"min"`
	Max    string `json:"max,omitempty"`
	Strict bool   `json:"strict,omitempty"`
}

// CompatibilityRange is one side of the canonical `compatibility` object: a
// semver window (`min`, optional `max`) for the socle/manager or the API.
type CompatibilityRange struct {
	Min    string `json:"min"`
	Max    string `json:"max,omitempty"`
	Strict bool   `json:"strict,omitempty"`
}

// ModuleCompatibility is the canonical compatibility declaration
// (`docs/modules/module-manifest.md` §6.4): accepted socle (manager) and API
// version windows. Out-of-range versions mark the module `incompatible` at
// resolution time (spec `module-installation.md` §7.1, step 6).
type ModuleCompatibility struct {
	Socle CompatibilityRange `json:"socle"`
	API   CompatibilityRange `json:"api"`
}

// ModuleOAuth carries the OAuth scopes negotiated at access time. Scopes must
// stay within the catalogue authorized by the OAuth server (`openid`,
// `profile`, `email`, `organizations`, `roles`, `permissions`).
type ModuleOAuth struct {
	Scopes []string `json:"scopes"`
}

// BackendDeclaration is one entry of the tier-1 egress declaration
// (`manifest.backends[]`, spec module-isolated-runtime.md §6.2). The `key`
// must match an entry of the operator master list (tier 2, `ModuleEgressBackend`)
// for a call to pass — a module never auto-authorizes a backend.
type BackendDeclaration struct {
	// Key is the stable kebab-case identifier of the backend ("erp-prod").
	Key string `json:"key"`
	// URL is the origin + base path of the backend; https is mandatory
	// (loopback http accepted for development only).
	URL string `json:"url"`
	// Scopes lists declarative permissions (`Role:Verbe`, same grammar as
	// `permissions`).
	Scopes []string `json:"scopes,omitempty"`
	// Description is the human-readable purpose shown on the consent screen
	// (§6.10.5: the approval box lists the declared backends).
	Description string `json:"description,omitempty"`
}

// LegalDocumentKind enumerates the categories a module may declare (D16,
// spec module-isolated-runtime.md §6.11). TERMS and PRIVACY are mandatory
// once any document is declared; LICENSE is the only optional one.
const (
	LegalKindTerms   = "TERMS"
	LegalKindPrivacy = "PRIVACY"
	LegalKindLicense = "LICENSE"
)

// LegalDocument is one entry of `manifest.legal` (D16, §6.11).
//
// Content is deliberately kept as raw JSON rather than a typed union. The
// server owns the canonical form and the checksum (spec §6.11: the acceptance
// records `documentVersion` + `contentChecksum` over that canonical form), so
// a Go struct here would be a fourth implementation of the normalizer, free to
// drift from the one that decides. This type validates the *envelope* — the
// part whose mistakes make a module unpublishable — and hands the content
// through untouched, which also keeps `Marshal` byte-exact on re-emission.
type LegalDocument struct {
	// Key is the stable kebab-case identifier of the document ("terms"). It is
	// part of the acceptance key: renaming it forces a re-acceptance.
	Key string `json:"key"`
	// Kind is TERMS, PRIVACY or LICENSE.
	Kind string `json:"kind"`
	// Title is what the end user reads on the checkbox line; defaults to the
	// category label server-side. Part of the checksummed content identity.
	Title string `json:"title,omitempty"`
	// Version is the document revision, independent of the module version.
	// Mandatory: it is what triggers re-acceptance when a publisher revises
	// their terms.
	Version string `json:"version"`
	// Required defaults to true. `false` is admitted for LICENSE only.
	Required *bool `json:"required,omitempty"`
	// Content is the document body in one of three notations: a section array,
	// a nested object, or Markdown. Verified structurally by
	// ValidateLegalContent; normalized server-side.
	Content json.RawMessage `json:"content"`
}

// IsRequired reports whether the document blocks module use until accepted.
func (d LegalDocument) IsRequired() bool { return d.Required == nil || *d.Required }

// ArtifactDeclaration is the `artifact` section of the manifest (§4.4): where
// the build writes the executable iframe payload.
type ArtifactDeclaration struct {
	// Dir is the build output directory, relative to the module directory
	// (canonical: `artifact`).
	Dir string `json:"dir,omitempty"`
	// Bundle is the self-contained JS bundle file name (canonical:
	// `module.js`).
	Bundle string `json:"bundle,omitempty"`
	// Document is the iframe host document (canonical: `index.html`).
	Document string `json:"document,omitempty"`
}

// EffectiveArtifactDir returns the artifact directory of a manifest,
// defaulting to the canonical `artifact`.
func (m *Manifest) EffectiveArtifactDir() string {
	if m.Artifact != nil && strings.TrimSpace(m.Artifact.Dir) != "" {
		return m.Artifact.Dir
	}
	return config.ModuleArtifactDir
}

// SourceArtifactDir resolves the artifact directory of a module inside its
// development tree: the `.liorian/artifact` layout (D7) when present on disk,
// the manifest declaration when it exists, the distribution layout otherwise
// (legacy `<module>/artifact/`).
func (m *Manifest) SourceArtifactDir(moduleDir string) string {
	if pkg.DirExists(filepath.Join(moduleDir, config.ModuleArtifactSourceDir)) {
		return config.ModuleArtifactSourceDir
	}
	if dir := m.EffectiveArtifactDir(); pkg.DirExists(filepath.Join(moduleDir, dir)) {
		return dir
	}
	return m.EffectiveArtifactDir()
}

// EffectiveArtifactBundle returns the bundle file name of a manifest,
// defaulting to the canonical `module.js`.
func (m *Manifest) EffectiveArtifactBundle() string {
	if m.Artifact != nil && strings.TrimSpace(m.Artifact.Bundle) != "" {
		return m.Artifact.Bundle
	}
	return "module.js"
}

// EffectiveArtifactDocument returns the host document file name of a
// manifest, defaulting to the canonical `index.html`.
func (m *Manifest) EffectiveArtifactDocument() string {
	if m.Artifact != nil && strings.TrimSpace(m.Artifact.Document) != "" {
		return m.Artifact.Document
	}
	return "index.html"
}

// Capabilities declares the module capabilities as a list of Tauri permission
// identifiers (`core:default`, `notification:default`, `core:<plugin>:<perm>`).
//
// Canonical form (`docs/modules/module-manifest.md` §6.6). The legacy
// boolean-object form (`{"needsNetwork": true, ...}`) is accepted on read and
// normalized to the enabled flag names; Marshal always emits the canonical
// array (the legacy flags were removed from the schema).
type Capabilities []string

// UnmarshalJSON accepts the canonical string array and the legacy
// boolean-object form.
func (c *Capabilities) UnmarshalJSON(data []byte) error {
	var ids []string
	if err := json.Unmarshal(data, &ids); err == nil {
		*c = ids
		return nil
	}
	var legacy map[string]any
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	out := make([]string, 0, len(legacy))
	for k, v := range legacy {
		if b, ok := v.(bool); ok && !b {
			continue
		}
		out = append(out, k)
	}
	sort.Strings(out)
	*c = out
	return nil
}

// DataModelField is one field of a `dataModel` resource (spec
// `module-installation.md` §7.5): the shape of the auto-generated CRUD.
type DataModelField struct {
	Key      string   `json:"key"`
	Type     string   `json:"type,omitempty"`
	Label    string   `json:"label,omitempty"`
	Required bool     `json:"required,omitempty"`
	ReadOnly bool     `json:"readOnly,omitempty"`
	Options  []string `json:"options,omitempty"`
}

// DataModelPermissionSet maps CRUD operations to `Role:Verbe` permission
// codes required to call them.
type DataModelPermissionSet struct {
	Read   string `json:"read,omitempty"`
	Create string `json:"create,omitempty"`
	Update string `json:"update,omitempty"`
	Delete string `json:"delete,omitempty"`
}

// DataModelResource declares one CRUD resource of a `CONFIGURATION` module.
type DataModelResource struct {
	Resource    string                 `json:"resource"`
	Label       string                 `json:"label,omitempty"`
	Fields      []DataModelField       `json:"fields,omitempty"`
	Permissions DataModelPermissionSet `json:"permissions,omitempty"`
}

// DeclarativeColumn is one column of a `datagrid` block.
type DeclarativeColumn struct {
	Field    string `json:"field"`
	Label    string `json:"label,omitempty"`
	Type     string `json:"type,omitempty"`
	Sortable bool   `json:"sortable,omitempty"`
}

// DeclarativeBlock is one UI block of a `CONFIGURATION` module page
// (`kpi`, `datagrid`, `form`, `chart`, `settings`). Only the fields relevant
// to the block `kind` are set; unknown block fields are ignored by the CLI
// (the SDK engine owns the rendering).
type DeclarativeBlock struct {
	ID        string              `json:"id,omitempty"`
	Kind      string              `json:"kind"`
	Resource  string              `json:"resource,omitempty"`
	Mode      string              `json:"mode,omitempty"`
	Aggregate string              `json:"aggregate,omitempty"`
	Label     string              `json:"label,omitempty"`
	Title     string              `json:"title,omitempty"`
	Chart     string              `json:"chart,omitempty"`
	X         string              `json:"x,omitempty"`
	Y         string              `json:"y,omitempty"`
	Columns   []DeclarativeColumn `json:"columns,omitempty"`
	Actions   []string            `json:"actions,omitempty"`
	Fields    []DataModelField    `json:"fields,omitempty"`
}

// DeclarativePage is one page of the `declarative` section.
type DeclarativePage struct {
	ID     string             `json:"id"`
	Path   string             `json:"path,omitempty"`
	Title  string             `json:"title,omitempty"`
	Blocks []DeclarativeBlock `json:"blocks,omitempty"`
}

// DeclarativeConfig is the `declarative` section of a `CONFIGURATION`
// manifest: natively rendered pages plus dashboard `widgets`.
type DeclarativeConfig struct {
	Pages   []DeclarativePage  `json:"pages,omitempty"`
	Widgets []DeclarativeBlock `json:"widgets,omitempty"`
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
//
// The manifest follows the canonical contract (`docs/modules/module-manifest.md`,
// `schemaVersion: 1`): `compatibility.{socle,api}` windows, `oauth.scopes`,
// `capabilities` as Tauri permission ids, and `Role:Verbe` permissions.
func NewManifest(name, description string) Manifest {
	upperKey := upperSnake(name)
	return Manifest{
		SchemaVersion: 1,
		ID:            name,
		Domain:        "mod.liorian." + name,
		Key:           upperKey,
		Name:          displayName(name),
		Description:   description,
		Version:       "0.1.0",
		Icon:          "PuzzleIcon",
		Type:          "WEB_APP_LOCAL",
		External:      true,
		Entry:         config.ModuleEntryFileName,
		URI:           "/" + name,
		Category:      "SYSTEM",
		Token:         pkg.NewUUID(),
		Publisher:     Publisher{ID: "", Name: ""},
		Platforms: Platforms{
			Web:     Platform{Supported: true, Modes: []string{"web"}},
			Desktop: Platform{Supported: true, Modes: []string{"local-webview"}, OS: []string{"windows", "macos", "linux"}},
			Mobile:  Platform{Supported: true, Modes: []string{"local-webview"}, OS: []string{"android"}},
		},
		Compatibility: &ModuleCompatibility{
			Socle: CompatibilityRange{Min: "0.17.1", Max: "0.17.x"},
			API:   CompatibilityRange{Min: "0.27.0", Max: "0.27.x"},
		},
		Permissions: []string{"User:Get", "Editor:Post", "Editor:Put", "Admin:Delete"},
		// D15: userScope is mandatory — the scaffold starts with an empty
		// (but declared) scope, so the consent screen has something to show
		// and the pack validation passes.
		UserScope: []string{},
		// D9: no third-party backend by default — a first-party module only
		// reaches api-core through ctx.api.
		Backends: []BackendDeclaration{},
		OAuth:    &ModuleOAuth{Scopes: []string{"openid", "profile", "email", "organizations"}},
		Artifact: &ArtifactDeclaration{Dir: config.ModuleArtifactDir, Bundle: "module.js", Document: "index.html"},
		Capabilities: Capabilities{
			"core:default",
		},
		IsEnabled:            true,
		IsDefault:            false,
		Requirements:         map[string]any{},
		OptionalRequirements: map[string]string{},
		DataModel:            []DataModelResource{},
		Widgets:              []string{},
		Routines:             []string{},
		Providers:            []string{},
		ConfigSettings:       []ConfigSetting{},
		Menu:                 Menu{Items: []MenuItem{}},
	}
}

// EffectiveSocle returns the socle/manager compatibility window, preferring
// the canonical `compatibility.socle` over the legacy `managerCompatibility`.
func (m *Manifest) EffectiveSocle() CompatibilityRange {
	if m.Compatibility != nil && strings.TrimSpace(m.Compatibility.Socle.Min) != "" {
		return m.Compatibility.Socle
	}
	return CompatibilityRange{Min: m.ManagerCompat.Min, Max: m.ManagerCompat.Max, Strict: m.ManagerCompat.Strict}
}

// EffectiveAPI returns the API compatibility window, preferring the canonical
// `compatibility.api` over the legacy `apiCompatibility`.
func (m *Manifest) EffectiveAPI() CompatibilityRange {
	if m.Compatibility != nil && strings.TrimSpace(m.Compatibility.API.Min) != "" {
		return m.Compatibility.API
	}
	return CompatibilityRange{Min: m.APICompat.Min, Max: m.APICompat.Max, Strict: m.APICompat.Strict}
}

// EffectiveOAuthScopes returns the negotiated OAuth scopes, preferring the
// canonical `oauth.scopes` over the legacy `apiScopes`.
func (m *Manifest) EffectiveOAuthScopes() []string {
	if m.OAuth != nil && m.OAuth.Scopes != nil {
		return m.OAuth.Scopes
	}
	return m.APIScopes
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

// ModuleTypes is the set of distribution/execution types accepted by the
// canonical schema (`docs/modules/module-manifest.md` §6.2). `INTERNAL` and
// `EXTERNAL` are legacy aliases (deprecated, still accepted): `INTERNAL`
// denotes a socle-bundled module, `EXTERNAL` a remotely-served web app.
var ModuleTypes = map[string]bool{
	"CONFIGURATION":   true,
	"EXTERNAL_URL":    true,
	"WEB_APP_REMOTE":  true,
	"WEB_APP_CACHED":  true,
	"WEB_APP_LOCAL":   true,
	"REMOTE_FRONTEND": true,
	"SYSTEM":          true,
	"SERVICE":         true,
	"WIDGET":          true,
	"THEME":           true,
	"INTERNAL":        true,
	"EXTERNAL":        true,
}

// LegacyModuleTypes lists the pre-canonical distribution types. They remain
// accepted for existing projects but trigger a migration warning at audit
// time; new modules should use the canonical `ModuleType` enum.
var LegacyModuleTypes = map[string]bool{"INTERNAL": true, "EXTERNAL": true}

// IsLegacyModuleType reports whether t is a deprecated distribution type.
func IsLegacyModuleType(moduleType string) bool {
	return LegacyModuleTypes[strings.ToUpper(strings.TrimSpace(moduleType))]
}

// ModuleRoles is the catalogue of backend roles usable in `permissions`
// (`<ROLE>:<VERBE>`, `docs/modules/module-manifest.md` §6.5). The roles are a
// **subset** of the scope grammar: a scope is any PascalCase domain of the
// backend `PermissionsConfig` (`User`, `RestaurantDish`, `AccountingAccount`…),
// the roles being the domains the platform names explicitly.
var ModuleRoles = []string{
	"Root", "SuperAdmin", "Admin", "Manager", "Editor", "Moderator",
	"Analyst", "Commercial", "Operator", "Contributor", "Caissier",
	"User", "Guest", "Viewer",
}

// ModuleVerbs maps Raiton controller verbs to permission verbs.
var ModuleVerbs = []string{"Get", "Post", "Put", "Delete"}

// permissionCodeRE matches a canonical scope code: `<PascalCaseDomain>:<VERBE>`
// (D15, spec `module-isolated-runtime.md` §6.10.1). The domain is not a role
// list but any backend `PermissionsConfig` domain — the grammar the runtime
// itself enforces (`MODULE_SCOPE_RE`, `@liorian/api-resources/module-scope.util`,
// reused by `assertValidUserScope` at installation and by the module-consent
// token checks). A CLI narrower than the runtime would refuse archives the
// socle then accepts: the two must share one grammar.
var permissionCodeRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*:(Get|Post|Put|Delete)$`)

// IsPermissionCode reports whether s is a canonical `<DOMAIN>:<VERBE>` scope
// code — same grammar for `permissions` and `userScope`.
func IsPermissionCode(s string) bool {
	return permissionCodeRE.MatchString(strings.TrimSpace(s))
}

// ValidatePermissionCode checks one permission entry of a manifest.
func ValidatePermissionCode(code string) error {
	if IsPermissionCode(code) {
		return nil
	}
	return errors.New(i18n.Tf("module.error.permission", code))
}

// backendKeyRE matches a backend key: kebab-case (spec §6.2).
var backendKeyRE = kebabNameRE

// backendURLRE captures the pieces of a backend URL that carry SSRF risk:
// userinfo before the host, and dot segments in the path.
var (
	userinfoRE = regexp.MustCompile(`^[a-z][a-z0-9+.-]*://[^/@]+@`)
	dotSegRE   = regexp.MustCompile(`(?i)(^|/)\.\.(/|$)|%2e%2e`)
)

// ValidateBackendDeclaration checks one tier-1 egress declaration (§6.2):
// kebab-case key, https URL (loopback http accepted for development), no
// userinfo, no dot-segment traversal.
func ValidateBackendDeclaration(b BackendDeclaration) error {
	key := strings.TrimSpace(b.Key)
	if !backendKeyRE.MatchString(key) {
		return errors.New(i18n.Tf("module.error.backend_key", key))
	}
	url := strings.TrimSpace(b.URL)
	if url == "" {
		return errors.New(i18n.Tf("module.error.backend_url", key, "empty"))
	}
	if !strings.HasPrefix(url, "https://") && !isLoopbackURL(url) {
		return errors.New(i18n.Tf("module.error.backend_url", key, url))
	}
	if userinfoRE.MatchString(url) {
		return errors.New(i18n.Tf("module.error.backend_userinfo", key))
	}
	if dotSegRE.MatchString(url) {
		return errors.New(i18n.Tf("module.error.backend_traversal", key))
	}
	for _, scope := range b.Scopes {
		if !IsPermissionCode(scope) {
			return errors.New(i18n.Tf("module.error.backend_scope", key, scope))
		}
	}
	return nil
}

// isLoopbackURL reports whether u is an http:// URL aimed at the loopback
// interface — the only http exception, accepted for development backends
// (§6.7: loopback must still be aligned explicitly in the tier-2 master list).
func isLoopbackURL(u string) bool {
	prefixes := []string{
		"http://127.0.0.1", "http://localhost",
		"http://[::1]", "http://[::ffff:127.0.0.1]",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(u, p+"/") || strings.HasPrefix(u, p+":") || u == p {
			return true
		}
	}
	return false
}

// ValidateUserScopeEntry checks one `userScope` entry: the same `Role:Verbe`
// grammar as `permissions` (D15, §6.10.1). The pack validator rejects an
// out-of-grammar scope — it is the only place where a module can declare
// invalid qualifiers without the user seeing them.
func ValidateUserScopeEntry(scope string) error {
	if IsPermissionCode(scope) {
		return nil
	}
	return errors.New(i18n.Tf("module.error.user_scope", scope))
}

// canonicalDomainRE matches the canonical module domain
// (`mod.<organization-slug>.<module-identifier>`, spec `module-installation.md` §4.3).
// Exactly three labels: the organization slug (mandatory — every organization
// configures it before gaining access to the socle, Connect and the CLI) and
// the module identifier, both kebab-case.
var canonicalDomainRE = regexp.MustCompile(`^mod\.[a-z0-9]+(-[a-z0-9]+)*\.[a-z0-9]+(-[a-z0-9]+)*$`)

// IsCanonicalDomain reports whether domain follows the canonical
// `mod.<organization-slug>.<module-identifier>` form.
func IsCanonicalDomain(domain string) bool {
	return canonicalDomainRE.MatchString(strings.TrimSpace(domain))
}

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

// ValidateType checks an optional module distribution type against the
// canonical `ModuleType` enum (`CONFIGURATION`, `EXTERNAL_URL`,
// `WEB_APP_REMOTE`, `WEB_APP_CACHED`, `WEB_APP_LOCAL`, `REMOTE_FRONTEND`,
// `SYSTEM`, `SERVICE`, `WIDGET`, `THEME`). The legacy `INTERNAL` / `EXTERNAL`
// values are still accepted (migration warning at audit time). An empty type
// is accepted (the creation default applies).
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
