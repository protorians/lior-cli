package catalog

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/pkg"
)

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// signArchive signs the archive with a freshly generated Ed25519 key.
func signArchive(data []byte) (sigB64, pubB64 string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data)),
		base64.StdEncoding.EncodeToString(pub), nil
}

// baseEntries returns a conformant isolated-runtime module file set (§4.4):
// the manifest at the archive root, the sources under src/ and the built
// payload under artifact/. badManifest points the entry to a missing file so
// validation fails. The manifest follows the canonical contract
// (compatibility, oauth, capabilities, Role:Verbe permissions, D15 userScope).
func baseEntries(name, page string, badManifest bool) map[string]string {
	manifest := map[string]any{
		"schemaVersion": 1,
		"id":            name,
		"domain":        name,
		"key":           strings.ToUpper(strings.ReplaceAll(name, "-", "_")),
		"name":          "Test Module",
		"description":   "A module used by the install tests",
		"version":       "1.2.3",
		"icon":          "PuzzleIcon",
		"type":          "WEB_APP_LOCAL",
		"external":      true,
		"entry":         "entry.tsx",
		"uri":           "/" + page,
		"category":      "SYSTEM",
		"token":         pkg.NewUUID(),
		"userScope":     []string{},
		"backends":      []string{},
		"artifact":      map[string]any{"dir": "artifact", "bundle": "module.js", "document": "index.html"},
		"platforms": map[string]any{
			"web": map[string]any{"supported": true, "modes": []string{"web"}},
		},
		"compatibility": map[string]any{
			"socle": map[string]any{"min": "0.17.1", "max": "0.17.x"},
			"api":   map[string]any{"min": "0.27.0", "max": "0.27.x"},
		},
		"permissions":          []string{"User:Get"},
		"oauth":                map[string]any{"scopes": []string{"openid"}},
		"optionalRequirements": map[string]string{},
		"requirements":         map[string]any{},
		"capabilities":         []string{"core:default"},
	}
	if badManifest {
		manifest["entry"] = "missing.tsx"
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		panic(err)
	}
	return map[string]string{
		config.ManifestFileName:          string(raw) + "\n",
		"src/tsconfig.json":              "{}\n",
		"src/entry.tsx":                  "export function mount(el: HTMLElement): void {}\n\nexport function unmount(): void {}\n",
		"src/presentation/demo.view.tsx": "export function DemoView() {\n  return <div>Demo</div>;\n}\n",
		"artifact/module.js":             "console.log(\"demo\");\n",
		"artifact/index.html":            "<!doctype html><html><body></body></html>\n",
	}
}

// modernArchiveOrder lists the archive entries in a deterministic order.
func modernArchiveOrder(page string) []string {
	return []string{
		config.ManifestFileName,
		"src/tsconfig.json",
		"src/entry.tsx",
		"src/presentation/demo.view.tsx",
		"artifact/module.js",
		"artifact/index.html",
	}
}

// archiveOpts tweaks the archive build for failure cases.
type archiveOpts struct {
	traverse    string // a path-traversal entry that must be dropped
	badManifest bool   // manifest entry points to a missing file
	noManifest  bool   // drop the manifest (unusable archive)
}

// buildArchive zips an ordered entry map into .LiorArtifactPackage bytes.
func buildArchive(t *testing.T, name, page string, opts archiveOpts) []byte {
	t.Helper()
	entries := baseEntries(name, page, opts.badManifest)
	if opts.traverse != "" {
		entries[opts.traverse] = "evil"
	}
	if opts.noManifest {
		delete(entries, config.ManifestFileName)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	// deterministic order keeps test archives reproducible
	for _, rel := range modernArchiveOrder(page) {
		content, ok := entries[rel]
		if !ok {
			continue
		}
		fw, err := zw.Create(rel)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if opts.traverse != "" {
		if content, ok := entries[opts.traverse]; ok {
			fw, err := zw.Create(opts.traverse)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fw.Write([]byte(content)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// catalogModuleFile carries a module and its artifact for the mock server.
type catalogModuleFile struct {
	name      string
	data      []byte
	checksum  string
	signature string
	publicKey string
}

func (m catalogModuleFile) catalogEntry(artifactURL string) CatalogModule {
	return CatalogModule{
		ID: "mod_" + m.name, Slug: m.name, Domain: m.name, Name: "Test Module",
		Version: "1.2.3", PrimaryCategory: "SYSTEM",
		Publisher: struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}{ID: "pub_1", Name: "Test Publisher"},
		ArtifactURL:        artifactURL,
		ArtifactChecksum:   m.checksum,
		Signature:          m.signature,
		SignaturePublicKey: m.publicKey,
	}
}

// installServer serves the catalog GetModule endpoint and module artifacts.
type installServer struct {
	URL  string
	mods []catalogModuleFile
	srv  *httptest.Server
}

func newInstallServer(t *testing.T, mods []catalogModuleFile) *installServer {
	t.Helper()
	s := &installServer{mods: mods}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/catalog/modules/", func(w http.ResponseWriter, r *http.Request) {
		ref := strings.TrimPrefix(r.URL.Path, "/api/catalog/modules/")
		for _, m := range mods {
			if m.name == ref {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(raiton(t, m.catalogEntry(s.srv.URL+"/artifacts/"+m.name+".LiorArtifactPackage")))
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"module not found","statusCode":404,"data":null}`))
	})
	mux.HandleFunc("/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/artifacts/")
		name = strings.TrimSuffix(name, ".LiorArtifactPackage")
		name = strings.TrimSuffix(name, config.ArchiveExt)
		for _, m := range mods {
			if m.name == name {
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = w.Write(m.data)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	})

	s.srv = httptest.NewServer(mux)
	s.URL = s.srv.URL
	t.Cleanup(s.srv.Close)
	return s
}

func (s *installServer) install(root, name string, force bool) (*InstallResult, error) {
	return (&Installer{Root: root, Force: force, Client: &Client{HTTP: pkg.NewClient(s.URL)}}).
		Install(context.Background(), name)
}

func (s *installServer) installAllowUnsigned(root, name string, force bool) (*InstallResult, error) {
	return (&Installer{Root: root, Force: force, AllowUnsigned: true, Client: &Client{HTTP: pkg.NewClient(s.URL)}}).
		Install(context.Background(), name)
}

const (
	testName = "com.example.blog-manager"
	testPage = "blog-manager"
)

func TestInstallHappyPath(t *testing.T) {
	data := buildArchive(t, testName, testPage, archiveOpts{})
	sig, pub, err := signArchive(data)
	if err != nil {
		t.Fatal(err)
	}
	srv := newInstallServer(t, []catalogModuleFile{{
		name: testName, data: data, checksum: sha256Hex(data), signature: sig, publicKey: pub,
	}})
	root := t.TempDir()

	res, err := srv.install(root, testName, false)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if res.Module != testName {
		t.Errorf("Module = %q, want %q", res.Module, testName)
	}
	if res.Version != "1.2.3" {
		t.Errorf("Version = %q, want 1.2.3", res.Version)
	}
	if res.SignatureStatus != SignatureVerified {
		t.Errorf("SignatureStatus = %q, want verified", res.SignatureStatus)
	}
	if res.Files != 6 {
		t.Errorf("Files = %d, want 6", res.Files)
	}

	// D11: the archive lands multi-version under library/modules/<id>/<version>/
	// and the current pointer moves to the installed version.
	versionDir := config.InstalledModuleVersionDir(root, testName, "1.2.3")
	for _, rel := range []string{
		config.ManifestFileName,
		"src/entry.tsx",
		"src/presentation/demo.view.tsx",
		"artifact/module.js",
		"artifact/index.html",
	} {
		if !pkg.FileExists(filepath.Join(versionDir, rel)) {
			t.Errorf("expected %s to be installed", filepath.Join(config.ExternalModulesDir, testName, "1.2.3", rel))
		}
	}
	if got := config.ReadCurrentPointer(root, testName); got != "1.2.3" {
		t.Errorf("current pointer = %q, want 1.2.3", got)
	}
}

// TestInstallUnsignedRefused pins the fail-closed rule (SEC-001, ADR-010): an
// artefact without a publisher signature is refused.
func TestInstallUnsignedRefused(t *testing.T) {
	data := buildArchive(t, testName, testPage, archiveOpts{})
	srv := newInstallServer(t, []catalogModuleFile{{name: testName, data: data, checksum: sha256Hex(data)}})
	root := t.TempDir()

	_, err := srv.install(root, testName, false)
	if err == nil {
		t.Fatal("expected a signature error for an unsigned artefact")
	}
	if pkg.ExitCodeFor(err) != pkg.ExitSigning {
		t.Errorf("ExitCode = %d, want %d", pkg.ExitCodeFor(err), pkg.ExitSigning)
	}
	if pkg.PathExists(filepath.Join(root, config.ExternalModulesDir)) {
		t.Error("nothing should be installed after a refused signature")
	}
}

// TestInstallUnsignedAllowedExplicitly covers the explicit dev escape hatch
// (--allow-unsigned): the install proceeds with an unsigned warning.
func TestInstallUnsignedAllowedExplicitly(t *testing.T) {
	data := buildArchive(t, testName, testPage, archiveOpts{})
	srv := newInstallServer(t, []catalogModuleFile{{name: testName, data: data, checksum: sha256Hex(data)}})
	root := t.TempDir()

	res, err := srv.installAllowUnsigned(root, testName, false)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if res.SignatureStatus != SignatureUnsigned {
		t.Errorf("SignatureStatus = %q, want unsigned", res.SignatureStatus)
	}
	if len(res.Warnings) == 0 {
		t.Error("expected an unsigned warning")
	}
}

func TestInstallAlreadyInstalled(t *testing.T) {
	data := buildArchive(t, testName, testPage, archiveOpts{})
	srv := newInstallServer(t, []catalogModuleFile{{name: testName, data: data, checksum: sha256Hex(data)}})
	root := t.TempDir()

	// A flat legacy installation (manifest.json directly in
	// library/modules/<name>) is only replaced on demand.
	moduleDir := config.ModuleDir(root, testName)
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(moduleDir, "marker.txt")
	if err := os.WriteFile(marker, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, config.ManifestFileName), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := srv.install(root, testName, false)
	if err == nil {
		t.Fatal("expected an error for an already-installed module")
	}
	if pkg.ExitCodeFor(err) != pkg.ExitModuleNotFound {
		t.Errorf("ExitCode = %d, want %d", pkg.ExitCodeFor(err), pkg.ExitModuleNotFound)
	}
	if !strings.Contains(err.Error(), "already") {
		t.Errorf("error %q should mention the already-installed module", err)
	}
	if !pkg.FileExists(marker) {
		t.Error("marker should survive a refused install")
	}
}

// A multi-version installation root (one directory per version + current
// pointer) never blocks a new install: the version lands alongside.
func TestInstallMultiVersionCoexists(t *testing.T) {
	data := buildArchive(t, testName, testPage, archiveOpts{})
	sig, pub, err := signArchive(data)
	if err != nil {
		t.Fatal(err)
	}
	srv := newInstallServer(t, []catalogModuleFile{{
		name: testName, data: data, checksum: sha256Hex(data), signature: sig, publicKey: pub,
	}})
	root := t.TempDir()

	if err := os.MkdirAll(config.InstalledModuleVersionDir(root, testName, "1.0.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteCurrentPointer(root, testName, "1.0.0"); err != nil {
		t.Fatal(err)
	}

	if _, err := srv.install(root, testName, false); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if got := config.ReadCurrentPointer(root, testName); got != "1.2.3" {
		t.Errorf("current pointer = %q, want 1.2.3", got)
	}
	if !pkg.DirExists(config.InstalledModuleVersionDir(root, testName, "1.0.0")) {
		t.Error("the previous version directory must survive the new install")
	}
}

func TestInstallForceReplaces(t *testing.T) {
	data := buildArchive(t, testName, testPage, archiveOpts{})
	sig, pub, err := signArchive(data)
	if err != nil {
		t.Fatal(err)
	}
	srv := newInstallServer(t, []catalogModuleFile{{
		name: testName, data: data, checksum: sha256Hex(data), signature: sig, publicKey: pub,
	}})
	root := t.TempDir()

	if _, err := srv.install(root, testName, false); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	// A stale file inside the version directory must be wiped by --force.
	versionDir := config.InstalledModuleVersionDir(root, testName, "1.2.3")
	marker := filepath.Join(versionDir, "marker.txt")
	if err := os.WriteFile(marker, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := srv.install(root, testName, true); err != nil {
		t.Fatalf("Install(--force) error = %v", err)
	}
	if pkg.FileExists(marker) {
		t.Error("marker should have been replaced by the force install")
	}
	if !pkg.FileExists(filepath.Join(versionDir, config.ManifestFileName)) {
		t.Error("manifest should be installed after a force install")
	}
}

func TestInstallChecksumMismatch(t *testing.T) {
	data := buildArchive(t, testName, testPage, archiveOpts{})
	srv := newInstallServer(t, []catalogModuleFile{{
		name: testName, data: data, checksum: strings.Repeat("0", 64),
	}})
	root := t.TempDir()

	_, err := srv.install(root, testName, false)
	if err == nil {
		t.Fatal("expected a checksum error")
	}
	if !strings.Contains(err.Error(), "SHA-256") {
		t.Errorf("error %q should mention the checksum", err)
	}
	if pkg.ExitCodeFor(err) != pkg.ExitError {
		t.Errorf("ExitCode = %d, want %d", pkg.ExitCodeFor(err), pkg.ExitError)
	}
	if pkg.PathExists(filepath.Join(root, config.ExternalModulesDir)) {
		t.Error("nothing should be installed after a checksum mismatch")
	}
}

func TestInstallSignatureVerified(t *testing.T) {
	data := buildArchive(t, testName, testPage, archiveOpts{})
	sig, pub, err := signArchive(data)
	if err != nil {
		t.Fatal(err)
	}
	srv := newInstallServer(t, []catalogModuleFile{{
		name: testName, data: data, checksum: sha256Hex(data), signature: sig, publicKey: pub,
	}})
	root := t.TempDir()

	res, err := srv.install(root, testName, false)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if res.SignatureStatus != SignatureVerified {
		t.Errorf("SignatureStatus = %q, want verified", res.SignatureStatus)
	}
}

func TestInstallSignatureWithoutKey(t *testing.T) {
	data := buildArchive(t, testName, testPage, archiveOpts{})
	sig, _, err := signArchive(data)
	if err != nil {
		t.Fatal(err)
	}
	srv := newInstallServer(t, []catalogModuleFile{{
		name: testName, data: data, checksum: sha256Hex(data), signature: sig,
	}})
	root := t.TempDir()

	// Fail-closed: a signature without a publisher public key cannot be
	// verified, so the install is refused (no unverified warning path).
	_, err = srv.install(root, testName, false)
	if err == nil {
		t.Fatal("expected a signature error (no public key)")
	}
	if pkg.ExitCodeFor(err) != pkg.ExitSigning {
		t.Errorf("ExitCode = %d, want %d", pkg.ExitCodeFor(err), pkg.ExitSigning)
	}
}

func TestInstallSignatureInvalid(t *testing.T) {
	data := buildArchive(t, testName, testPage, archiveOpts{})
	_, pub, err := signArchive(data)
	if err != nil {
		t.Fatal(err)
	}
	// a signature over different bytes with a DIFFERENT key
	sig, _, err := signArchive(buildArchive(t, testName, testPage, archiveOpts{badManifest: true}))
	if err != nil {
		t.Fatal(err)
	}
	srv := newInstallServer(t, []catalogModuleFile{{
		name: testName, data: data, checksum: sha256Hex(data), signature: sig, publicKey: pub,
	}})
	root := t.TempDir()

	_, err = srv.install(root, testName, false)
	if err == nil {
		t.Fatal("expected a signature error")
	}
	if pkg.ExitCodeFor(err) != pkg.ExitSigning {
		t.Errorf("ExitCode = %d, want %d", pkg.ExitCodeFor(err), pkg.ExitSigning)
	}
	if pkg.PathExists(filepath.Join(root, config.ExternalModulesDir)) {
		t.Error("nothing should be installed after a bad signature")
	}
}

func TestInstallModuleNotFound(t *testing.T) {
	srv := newInstallServer(t, nil)
	root := t.TempDir()

	_, err := srv.install(root, "com.unknown.module", false)
	if err == nil {
		t.Fatal("expected a not-found error")
	}
	if pkg.ExitCodeFor(err) != pkg.ExitModuleNotFound {
		t.Errorf("ExitCode = %d, want %d", pkg.ExitCodeFor(err), pkg.ExitModuleNotFound)
	}
}

func TestInstallTraversalRefused(t *testing.T) {
	data := buildArchive(t, testName, testPage, archiveOpts{traverse: "../evil.txt"})
	sig, pub, err := signArchive(data)
	if err != nil {
		t.Fatal(err)
	}
	srv := newInstallServer(t, []catalogModuleFile{{
		name: testName, data: data, checksum: sha256Hex(data), signature: sig, publicKey: pub,
	}})
	root := t.TempDir()

	// Fail-closed archive audit (§7.2): traversal entries are refused, the
	// whole install aborts and nothing is extracted.
	_, err = srv.install(root, testName, false)
	if err == nil {
		t.Fatal("expected a traversal error")
	}
	if pkg.PathExists(filepath.Join(root, config.ExternalModulesDir)) {
		t.Error("nothing should be installed after a refused archive entry")
	}
	if pkg.FileExists(filepath.Join(root, "evil.txt")) {
		t.Error("traversal entry escaped the module layout")
	}
}

func TestInstallInvalidArchive(t *testing.T) {
	garbage := []byte("not a zip")
	sig, pub, err := signArchive(garbage)
	if err != nil {
		t.Fatal(err)
	}
	srv := newInstallServer(t, []catalogModuleFile{{
		name: testName, data: garbage, checksum: sha256Hex(garbage), signature: sig, publicKey: pub,
	}})
	root := t.TempDir()

	_, err = srv.install(root, testName, false)
	if err == nil {
		t.Fatal("expected an unpack error")
	}
	if !strings.Contains(err.Error(), config.ArchiveFormatLabel) {
		t.Errorf("error %q should mention the archive", err)
	}
}

func TestInstallNoManifest(t *testing.T) {
	data := buildArchive(t, testName, testPage, archiveOpts{noManifest: true})
	sig, pub, err := signArchive(data)
	if err != nil {
		t.Fatal(err)
	}
	srv := newInstallServer(t, []catalogModuleFile{{
		name: testName, data: data, checksum: sha256Hex(data), signature: sig, publicKey: pub,
	}})
	root := t.TempDir()

	_, err = srv.install(root, testName, false)
	if err == nil {
		t.Fatal("expected a manifest error")
	}
}

func TestInstallValidationFails(t *testing.T) {
	data := buildArchive(t, testName, testPage, archiveOpts{badManifest: true})
	sig, pub, err := signArchive(data)
	if err != nil {
		t.Fatal(err)
	}
	srv := newInstallServer(t, []catalogModuleFile{{
		name: testName, data: data, checksum: sha256Hex(data), signature: sig, publicKey: pub,
	}})
	root := t.TempDir()

	_, err = srv.install(root, testName, false)
	if err == nil {
		t.Fatal("expected a validation error")
	}
	if !strings.Contains(err.Error(), "validation") {
		t.Errorf("error %q should mention the validation", err)
	}
	if pkg.PathExists(filepath.Join(root, config.ExternalModulesDir)) {
		t.Error("nothing should be installed after a validation failure")
	}
}

func TestUnpackRejectsAbsoluteEntry(t *testing.T) {
	entries := baseEntries(testName, testPage, false)
	entries["/etc/passwd"] = "evil"

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, rel := range []string{
		filepath.Join(config.ExternalModulesDir, testName, config.ManifestFileName),
		"/etc/passwd",
	} {
		fw, err := zw.Create(rel)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(entries[rel])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	// Fail-closed: the absolute entry aborts the whole unpack.
	if err := unpack(buf.Bytes(), t.TempDir(), true, &InstallResult{}); err == nil {
		t.Fatal("expected an error for the absolute entry")
	} else if !strings.Contains(err.Error(), "absolute") {
		t.Errorf("error %q should mention the absolute path", err)
	}
}

func containsEntry(t *testing.T, data []byte) bool {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name == "/etc/passwd" {
			return true
		}
	}
	return false
}

// TestEntryAudit pins the fail-closed archive audit contract (spec §7.2):
// traversal, absolute paths and executables are refused with an error;
// entries inside the module layout (including the canonical root manifest)
// pass; anything else is skipped.
func TestEntryAudit(t *testing.T) {
	for _, raw := range []string{"../evil.txt", "../../etc/passwd", "/etc/passwd", "C:/win/evil.txt"} {
		f := &zip.File{FileHeader: zip.FileHeader{Name: raw}}
		if _, err := auditEntry(f); err == nil {
			t.Errorf("auditEntry(%q) = nil, want an error", raw)
		}
	}
	for _, raw := range []string{"tool.exe", "lib/native.so", "run.sh"} {
		f := &zip.File{FileHeader: zip.FileHeader{Name: raw}}
		if _, err := auditEntry(f); err == nil {
			t.Errorf("auditEntry(%q) = nil, want an error (executable)", raw)
		}
	}
	for _, raw := range []string{"src/app/blog/page.tsx", "public/assets/x/a.txt"} {
		f := &zip.File{FileHeader: zip.FileHeader{Name: raw}}
		if _, err := auditEntry(f); err == nil {
			t.Errorf("auditEntry(%q) = nil, want an error (legacy layout refused)", raw)
		}
	}
	for raw, want := range map[string]string{
		config.ExternalModulesDir + "/com.example.x/manifest.json": config.ExternalModulesDir + "/com.example.x/manifest.json",
		"artifact/module.js": "artifact/module.js",
		"src/entry.tsx":      "src/entry.tsx",
		"manifest.json":      "manifest.json",
	} {
		f := &zip.File{FileHeader: zip.FileHeader{Name: raw}}
		got, err := auditEntry(f)
		if err != nil {
			t.Errorf("auditEntry(%q) error = %v", raw, err)
		} else if got != want {
			t.Errorf("auditEntry(%q) = %q, want %q", raw, got, want)
		}
	}
	for _, raw := range []string{"random/notes.txt", "README.md"} {
		f := &zip.File{FileHeader: zip.FileHeader{Name: raw}}
		got, err := auditEntry(f)
		if err != nil {
			t.Errorf("auditEntry(%q) error = %v", raw, err)
		} else if got != "" {
			t.Errorf("auditEntry(%q) = %q, want skipped", raw, got)
		}
	}
}

// TestEntrySanitization pins the sanitizer contract: entries are reduced to a
// clean, project-relative path and anything outside the module layout is
// rejected by allowedEntry (the two checks together defeat path traversal).
func TestEntrySanitization(t *testing.T) {
	cases := []struct {
		raw     string
		want    string
		allowed bool
	}{
		{config.ExternalModulesDir + "/com.example.x/manifest.json", config.ExternalModulesDir + "/com.example.x/manifest.json", true},
		{"artifact/module.js", "artifact/module.js", true},
		{"src/entry.tsx", "src/entry.tsx", true},
		{"../evil.txt", "evil.txt", false},
		{"../../etc/passwd", "etc/passwd", false},
		{"/etc/passwd", "etc/passwd", false},
		{"src/../../x", "x", false},
	}
	for _, c := range cases {
		got := sanitizeEntry(c.raw)
		if got == ".." || strings.HasPrefix(got, "../") || strings.Contains(got, "/../") {
			t.Errorf("sanitizeEntry(%q) = %q is not clean", c.raw, got)
		}
		if got != c.want {
			t.Errorf("sanitizeEntry(%q) = %q, want %q", c.raw, got, c.want)
		}
		if got != "" && allowedEntry(got) != c.allowed {
			t.Errorf("allowedEntry(%q) = %v, want %v", got, allowedEntry(got), c.allowed)
		}
	}
}

func TestWithinDir(t *testing.T) {
	base := "/tmp/liorian-base"
	if !withinDir(base, base+"/"+config.ExternalModulesDir+"/x/manifest.json") {
		t.Error("expected a descendant to be within base")
	}
	if withinDir(base, base+"-elsewhere/evil.txt") {
		t.Error("expected a sibling prefix to NOT be within base")
	}
	if withinDir(base, base+"/../evil.txt") {
		t.Error("expected a parent escape to NOT be within base")
	}
}
