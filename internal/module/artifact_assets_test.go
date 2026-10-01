package module

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalDomainPrefixFollowsType(t *testing.T) {
	cases := map[string]string{
		"SERVICE":         "service",
		"SYSTEM":          "system",
		"WIDGET":          "widget",
		"THEME":           "theme",
		"CONFIGURATION":   "config",
		"WEB_APP_LOCAL":   "mod",
		"WEB_APP_REMOTE":  "mod",
		"REMOTE_FRONTEND": "mod",
		"EXTERNAL":        "mod",
		"":                "mod",
		"unknown":         "mod",
	}
	for moduleType, want := range cases {
		if got := CanonicalDomainPrefix(moduleType); got != want {
			t.Fatalf("CanonicalDomainPrefix(%q) = %q, want %q", moduleType, got, want)
		}
	}
}

func TestCanonicalDomainBuildsIdentity(t *testing.T) {
	got := CanonicalDomain("WEB_APP_LOCAL", "Mon Orga", "accounting")
	if got != "mod.mon-orga.accounting" {
		t.Fatalf("CanonicalDomain = %q", got)
	}
	if got := CanonicalDomain("SERVICE", "acme", "crm"); got != "service.acme.crm" {
		t.Fatalf("CanonicalDomain(service) = %q", got)
	}
}

func TestCanonicalizeDomainRewritesPrefixByType(t *testing.T) {
	// com.acme.billing declared WEB_APP_LOCAL → mod.acme.billing.
	domain, rewritten, err := CanonicalizeDomain("com.acme.billing", "WEB_APP_LOCAL")
	if err != nil {
		t.Fatal(err)
	}
	if !rewritten || domain != "mod.acme.billing" {
		t.Fatalf("domain = %q rewritten = %v", domain, rewritten)
	}
	// A SYSTEM type gets the system prefix.
	if domain, _ = domainOf(t, "mod.acme.identity", "SYSTEM"); domain != "system.acme.identity" {
		t.Fatalf("system domain = %q", domain)
	}
	// Already canonical → untouched.
	domain, rewritten, err = CanonicalizeDomain("mod.liorian.accounting", "WEB_APP_LOCAL")
	if err != nil || rewritten || domain != "mod.liorian.accounting" {
		t.Fatalf("canonical domain altered: %q %v %v", domain, rewritten, err)
	}
	// Not exactly three labels → refused.
	if _, _, err := CanonicalizeDomain("mod.accounting", "WEB_APP_LOCAL"); err == nil {
		t.Fatal("expected a two-label domain to be refused")
	}
	if _, _, err := CanonicalizeDomain("a.b.c.d", "WEB_APP_LOCAL"); err == nil {
		t.Fatal("expected a four-label domain to be refused")
	}
}

func domainOf(t *testing.T, domain, moduleType string) (string, bool) {
	t.Helper()
	got, rewritten, err := CanonicalizeDomain(domain, moduleType)
	if err != nil {
		t.Fatal(err)
	}
	return got, rewritten
}

func TestCheckArchiveEntryAllowsMediaAndRefusesExecutables(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	// Médias et code : admis.
	for _, name := range []string{"logo.png", "demo.mp4", "ring.mp3", "font.woff2", "main.ts", "index.html", "data.json"} {
		path := write(name, "payload")
		if err := checkExecutableContent(path); err != nil {
			t.Fatalf("%s should not be flagged as executable content: %v", name, err)
		}
		if filepath.Ext(name) != "" {
			if err := checkArchiveEntry(path, "src/"+name); err != nil {
				t.Fatalf("%s should be allowed: %v", name, err)
			}
		}
	}

	// Exécutables par extension : refusés.
	for _, name := range []string{"tool.sh", "tool.bash", "install.exe", "run.py", "lib.so", "app.jar"} {
		path := write(name, "#!/bin/sh\n")
		if err := checkArchiveEntry(path, "src/"+name); err == nil {
			t.Fatalf("%s should be refused", name)
		}
	}

	// Exécutables par contenu : ELF, Mach-O, PE, shebang — quelle que soit
	// l'extension.
	elf := write("payload.bin", "\x7fELF\x02\x01")
	if err := checkArchiveEntry(elf, "src/payload.bin"); err == nil {
		t.Fatal("ELF content should be refused")
	}
	macho := write("noext", "\xcf\xfa\xed\xfe....")
	if err := checkArchiveEntry(macho, "src/noext"); err == nil {
		t.Fatal("Mach-O content should be refused")
	}
	shebang := write("script", "#!/bin/bash\nrm -rf /")
	if err := checkArchiveEntry(shebang, "src/script"); err == nil {
		t.Fatal("shebang content should be refused")
	}
	zipPayload := write("README", "PK\x03\x04-stuffed")
	if err := checkExecutableContent(zipPayload); err == nil {
		t.Fatal("embedded zip should be refused")
	}

	// Fichier sans extension : texte admis, binaire refusé.
	text := write("README", "# Hello\n")
	if err := checkArchiveEntry(text, "src/README"); err != nil {
		t.Fatalf("extensionless text should be allowed: %v", err)
	}
	binary := write("LICENSE", "\x00\x01\x02binary")
	if err := checkArchiveEntry(binary, "src/LICENSE"); err == nil {
		t.Fatal("extensionless binary should be refused")
	}

	// Extension hors allowlist : refusée.
	unknown := write("notes.rtf", "text")
	if err := checkArchiveEntry(unknown, "src/notes.rtf"); err == nil {
		t.Fatal("unknown extension should be refused")
	}
}
