package module

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jetbrains/lior-cli/internal/pkg"
)

func TestValidateName(t *testing.T) {
	valid := []string{"blog", "blog-manager", "a1b2", "my-module-name"}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", name, err)
		}
	}

	invalid := []string{"", "ab", "Blog", "blog manager", "blog_", "-blog", "blog-", "éé"}
	for _, name := range invalid {
		if err := ValidateName(name); err == nil {
			t.Errorf("ValidateName(%q) = nil, want error", name)
		}
	}
}

func TestValidateDomain(t *testing.T) {
	valid := []string{"com.example.app", "com.organization.domain", "io.liorian-sdk.a", "fr.dev.demo"}
	for _, d := range valid {
		if err := ValidateDomain(d); err != nil {
			t.Errorf("ValidateDomain(%q) = %v, want nil", d, err)
		}
	}

	invalid := []string{"", "blog-manager", "justonelabel", "com", "com.", ".com", "com..example", "com/example", "é.com"}
	for _, d := range invalid {
		if err := ValidateDomain(d); err == nil {
			t.Errorf("ValidateDomain(%q) = nil, want error", d)
		}
	}
}

func TestValidateVersion(t *testing.T) {
	valid := []string{"", "0.0.0", "1.2.3", "10.20.30", "2.0.0-beta.1", "1.0.0+build.5", "v1.0.0"}
	for _, v := range valid {
		if err := ValidateVersion(v); err != nil {
			t.Errorf("ValidateVersion(%q) = %v, want nil", v, err)
		}
	}

	invalid := []string{"1", "1.2", "1.0", "abc"}
	for _, v := range invalid {
		if err := ValidateVersion(v); err == nil {
			t.Errorf("ValidateVersion(%q) = nil, want error", v)
		}
	}
}

func TestValidateIcon(t *testing.T) {
	valid := []string{"", "Puzzle", "PuzzleIcon", "WandSparklesIcon"}
	for _, ic := range valid {
		if err := ValidateIcon(ic); err != nil {
			t.Errorf("ValidateIcon(%q) = %v, want nil", ic, err)
		}
	}

	invalid := []string{"puzzle", "puzzle-icon", "Puzzle Icon", "1st", "éIco"}
	for _, ic := range invalid {
		if err := ValidateIcon(ic); err == nil {
			t.Errorf("ValidateIcon(%q) = nil, want error", ic)
		}
	}
}

func TestDisplayName(t *testing.T) {
	cases := map[string]string{
		"":             "",
		"blog-manager": "Blog Manager",
		"myapp":        "Myapp",
	}
	for in, want := range cases {
		if got := DisplayName(in); got != want {
			t.Errorf("DisplayName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewManifest(t *testing.T) {
	m := NewManifest("blog-manager", "Gestion de blog")

	if m.ID != "blog-manager" {
		t.Errorf("ID = %q, want blog-manager", m.ID)
	}
	if m.Key != "BLOG_MANAGER" {
		t.Errorf("Key = %q, want BLOG_MANAGER", m.Key)
	}
	if m.Domain != "mod.liorian.blog-manager" {
		t.Errorf("Domain = %q, want mod.liorian.blog-manager", m.Domain)
	}
	if m.URI != "/blog-manager" {
		t.Errorf("URI = %q, want /blog-manager", m.URI)
	}
	if m.Version != "0.1.0" {
		t.Errorf("Version = %q, want 0.1.0", m.Version)
	}
	if m.Token == "" {
		t.Error("Token est vide, un UUID doit être généré")
	}
	if !pkg.IsUUID(m.Token) {
		t.Errorf("Token %q n'est pas un UUID valide", m.Token)
	}
	if m.Entry != "index.tsx" {
		t.Errorf("Entry = %q, want index.tsx", m.Entry)
	}
	if m.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", m.SchemaVersion)
	}
}

func TestManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/manifest.json"

	m := NewManifest("billing", "")
	if err := m.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if loaded.ID != m.ID || loaded.Key != m.Key || loaded.Token != m.Token {
		t.Errorf("round-trip mismatch: loaded=%+v want=%+v", loaded, m)
	}
}

// TestMockupManifestRoundTripNoLoss guarantees a LoadManifest → Save → Load
// cycle loses no canonical field (platform os/iosSupported, capabilities,
// configSettings, menu hierarchy, publisher, ...).
func TestMockupManifestRoundTripNoLoss(t *testing.T) {
	original, err := LoadManifest(filepath.Join("mockups", "hello-world", "manifest.json"))
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}

	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := original.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if !reflect.DeepEqual(original, reloaded) {
		t.Errorf("round-trip a perdu des champs:\noriginal = %+v\nreloaded = %+v", original, reloaded)
	}

	if original.Schema == "" {
		t.Error("$schema doit être conservé")
	}
	if original.OptionalRequirements == nil {
		t.Error("optionalRequirements doit être conservé (objet vide non-nil)")
	}
	if len(reloaded.Platforms.Desktop.OS) == 0 {
		t.Error("platforms.desktop.os perdu au round-trip")
	}
	if reloaded.Platforms.Mobile.IOSSupported == nil {
		t.Error("platforms.mobile.iosSupported perdu au round-trip")
	}
	if reloaded.Capabilities != original.Capabilities {
		t.Errorf("capabilities altérées: %+v", reloaded.Capabilities)
	}
	if !reflect.DeepEqual(reloaded.Menu, original.Menu) {
		t.Errorf("menu altéré: %+v", reloaded.Menu)
	}
}

// TestManifestPreservesUnknownFields checks that schema additionalProperties
// (unknown top-level fields) survive a Save cycle.
func TestManifestPreservesUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	raw := `{"schemaVersion":1,"id":"x","customField":{"a":1},"another":"y"}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.Extra["customField"] == nil || m.Extra["another"] == nil {
		t.Fatalf("champs inconnus non conservés: %+v", m.Extra)
	}
	if err := m.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("sortie invalide: %v\n%s", err, out)
	}
	for _, key := range []string{"customField", "another"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("la sortie doit conserver %q:\n%s", key, out)
		}
	}
}

func TestManifestDeclarationsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/manifest.json"

	m := NewManifest("billing", "")
	m.Widgets = []string{"analytics"}
	m.Routines = []string{"billingAnalyticsRoutine"}
	m.Providers = []string{"layout"}
	m.Dependencies = map[string]string{"@liorian/sdk": "workspace:*"}
	m.DevDependencies = map[string]string{"typescript": "^6.0.3"}
	m.Menu.Items = []MenuItem{
		{Label: "Factures", Icon: "FileIcon", URL: "/billing/invoices"},
	}
	if err := m.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(loaded.Widgets) != 1 || loaded.Widgets[0] != "analytics" {
		t.Errorf("Widgets non conformes: %+v", loaded.Widgets)
	}
	if len(loaded.Routines) != 1 || loaded.Routines[0] != "billingAnalyticsRoutine" {
		t.Errorf("Routines non conformes: %+v", loaded.Routines)
	}
	if len(loaded.Providers) != 1 || loaded.Providers[0] != "layout" {
		t.Errorf("Providers non conformes: %+v", loaded.Providers)
	}
	if len(loaded.Menu.Items) != 1 || loaded.Menu.Items[0].URL != "/billing/invoices" {
		t.Errorf("Menu non conforme: %+v", loaded.Menu)
	}
	if loaded.Dependencies["@liorian/sdk"] != "workspace:*" {
		t.Errorf("Dependencies non conformes: %+v", loaded.Dependencies)
	}
	if loaded.DevDependencies["typescript"] != "^6.0.3" {
		t.Errorf("DevDependencies non conformes: %+v", loaded.DevDependencies)
	}
}

func TestManifestReadsMockupStyleMenuURL(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/manifest.json"

	m := NewManifest("billing", "")
	m.Menu.Items = []MenuItem{
		{Label: "Factures", Icon: "FileIcon", URL: "/billing/invoices"},
	}
	if err := m.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// The reference mockup declares menu entries with a `url` field.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.ReplaceAll(string(data), `"uri"`, `"url"`)
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(loaded.Menu.Items) != 1 || loaded.Menu.Items[0].URL != "/billing/invoices" {
		t.Errorf("Menu non conforme: %+v", loaded.Menu)
	}
}

func TestNewManifestDeclarationsAreTypedEmptyArrays(t *testing.T) {
	m := NewManifest("blog", "")
	if m.Widgets == nil || m.Routines == nil || m.Menu.Items == nil {
		t.Fatal("widgets/routines/menu doivent être des tableaux vides non-nil")
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	raw := string(data)
	for _, field := range []string{`"widgets":[]`, `"routines":[]`, `"items":[]`} {
		if !strings.Contains(raw, field) {
			t.Errorf("le JSON ne contient pas %q: %s", field, raw)
		}
	}
}

func TestUpperSnake(t *testing.T) {
	cases := map[string]string{
		"blog":         "BLOG",
		"blog-manager": "BLOG_MANAGER",
		"a1b2":         "A1B2",
	}
	for in, want := range cases {
		if got := upperSnake(in); got != want {
			t.Errorf("upperSnake(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSemver(t *testing.T) {
	ok := []string{"0.1.0", "1.2.3", "1.2.3-beta.1", "v2.0.0", "2.0.0+meta", "10.2.300"}
	bad := []string{"", "1", "1.2", "abc", "1.2.x", "01.2.3", "1.2.3-", "1.2.3+", "1.2.3..4"}

	for _, v := range ok {
		if !isSemver(v) {
			t.Errorf("isSemver(%q) = false, want true", v)
		}
	}
	for _, v := range bad {
		if isSemver(v) {
			t.Errorf("isSemver(%q) = true, want false", v)
		}
	}
}

func TestBumpPatch(t *testing.T) {
	for version, want := range map[string]string{
		"0.1.0":       "0.1.1",
		"1.2.3":       "1.2.4",
		"v3.4.5":      "3.4.6",
		"2.0.0-beta":  "2.0.1",
		"2.0.0+build": "2.0.1",
	} {
		got, err := pkg.BumpPatch(version)
		if err != nil {
			t.Errorf("BumpPatch(%q): %v", version, err)
			continue
		}
		if got != want {
			t.Errorf("BumpPatch(%q) = %q, want %q", version, got, want)
		}
	}
	for _, bad := range []string{"", "abc", "1.2", "1.2.x"} {
		if _, err := pkg.BumpPatch(bad); err == nil {
			t.Errorf("BumpPatch(%q) doit échouer", bad)
		}
	}
}

func TestContainsDefaultExport(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/index.tsx"

	valid := []string{
		"export default declaration;",
		"export default function Foo() {}",
		"export default async () => {}",
		"export default () => {}",
		"export default class Foo {}",
		"export default {\n  render: async () => {},\n};\n",
		"  export default foo;",
	}
	invalid := []string{
		"",
		"export { default } from \"./mod\";",
		"const x = export;",
		"exports.default = {};",
	}

	for _, content := range valid {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if !containsDefaultExport(path) {
			t.Errorf("containsDefaultExport(%q) = false, want true", content)
		}
	}
	for _, content := range invalid {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if containsDefaultExport(path) {
			t.Errorf("containsDefaultExport(%q) = true, want false", content)
		}
	}
}

func TestRawPermissionsIsArray(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/manifest.json"

	cases := map[string]bool{
		`{"permissions": []}`:                     true,
		`{"permissions": ["read"]}`:               true,
		`{"permissions": "read"}`:                 false,
		`{"permissions": {}}`:                     false,
		`{"permissions": 1}`:                      false,
		`{"nopermissions": []}`:                   false,
		`{"schemaVersion":1,"permissions":[]}`:    true,
		`{"schemaVersion":1,"permissions":"all"}`: false,
	}
	for content, want := range cases {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := rawPermissionsIsArray(path); got != want {
			t.Errorf("rawPermissionsIsArray(%q) = %v, want %v", content, got, want)
		}
	}
}
