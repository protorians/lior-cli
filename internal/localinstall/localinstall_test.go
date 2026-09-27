package localinstall

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/protorians/lior-cli/internal/catalog"
	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/pkg"
)

// buildModernArchive zips a conformant isolated-runtime module (§4.4).
func buildModernArchive(t *testing.T) []byte {
	t.Helper()
	entries := map[string]string{
		"manifest.json": `{
  "schemaVersion": 1,
  "id": "com.example.blog-manager",
  "domain": "com.example.blog-manager",
  "key": "BLOG_MANAGER",
  "name": "Blog Manager",
  "description": "A module used by the local-install tests",
  "version": "1.2.0",
  "icon": "PuzzleIcon",
  "type": "WEB_APP_LOCAL",
  "external": true,
  "entry": "entry.tsx",
  "uri": "/blog-manager",
  "token": "3f1a2b4c-5d6e-7f80-9a1b-2c3d4e5f6a7b",
  "userScope": [],
  "backends": [],
  "artifact": {"dir": "artifact", "bundle": "module.js", "document": "index.html"},
  "platforms": {"web": {"supported": true, "modes": ["web"]}},
  "permissions": [],
  "requirements": {},
  "optionalRequirements": {},
  "capabilities": ["core:default"]
}`,
		"src/tsconfig.json":   "{}\n",
		"src/entry.tsx":       "export function mount(el: HTMLElement): void {}\n\nexport function unmount(): void {}\n",
		"artifact/module.js":  "console.log(\"bundle\");\n",
		"artifact/index.html": "<!doctype html><html><body></body></html>\n",
	}
	order := []string{
		"manifest.json",
		"src/tsconfig.json",
		"src/entry.tsx",
		"artifact/module.js",
		"artifact/index.html",
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, rel := range order {
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
	return buf.Bytes()
}

func TestLocalInstallHappyPath(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, ".lorian", "build", "blog-manager-1.2.0.liozip")
	data := buildModernArchive(t)
	if err := pkg.CreateDir(filepath.Dir(archive)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, data, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := (&Installer{Root: root}).Install(archive)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Module != "com.example.blog-manager" {
		t.Errorf("Module = %q", res.Module)
	}
	if res.Version != "1.2.0" {
		t.Errorf("Version = %q", res.Version)
	}
	if res.SignatureStatus != catalog.SignatureUnsigned {
		t.Errorf("SignatureStatus = %q, want unsigned (no sidecar)", res.SignatureStatus)
	}

	versionDir := config.InstalledModuleVersionDir(root, "com.example.blog-manager", "1.2.0")
	for _, rel := range []string{
		config.ManifestFileName,
		"src/entry.tsx",
		"artifact/module.js",
		"artifact/index.html",
	} {
		if !pkg.FileExists(filepath.Join(versionDir, rel)) {
			t.Errorf("expected %s to be installed", rel)
		}
	}
	if got := config.ReadCurrentPointer(root, "com.example.blog-manager"); got != "1.2.0" {
		t.Errorf("current pointer = %q, want 1.2.0", got)
	}
}

func TestLocalInstallMissingArchive(t *testing.T) {
	root := t.TempDir()
	_, err := (&Installer{Root: root}).Install(filepath.Join(root, "absent.liozip"))
	if err == nil {
		t.Fatal("expected an error for a missing archive")
	}
}

func TestLocalInstallRefusesLegacyLayout(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "legacy.liozip")

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, rel := range []string{"src/app/blog/page.tsx", "public/assets/blog/a.txt"} {
		fw, err := zw.Create(rel)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := (&Installer{Root: root}).Install(archive)
	if err == nil {
		t.Fatal("a legacy-layout archive must be refused")
	}
	if !strings.Contains(err.Error(), "legacy") && !strings.Contains(err.Error(), "layout") {
		t.Errorf("error %q should mention the legacy layout", err)
	}
	if pkg.PathExists(filepath.Join(root, config.ExternalModulesDir)) {
		t.Error("nothing should be installed after a refused archive")
	}
}
