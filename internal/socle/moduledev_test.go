package socle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadModuleDevAbsentFileIsNotAnError(t *testing.T) {
	entries, err := ReadModuleDev(t.TempDir())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if entries != nil {
		t.Fatalf("entries = %v, want nil", entries)
	}
}

func TestReadModuleDevRejectsCorruptFile(t *testing.T) {
	socleDir := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(ModuleDevPath(socleDir)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ModuleDevPath(socleDir), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadModuleDev(socleDir); err == nil {
		t.Fatal("corrupt registry should fail")
	}
}

func TestUpsertModuleDevSortsAndDeduplicates(t *testing.T) {
	socleDir := t.TempDir()
	first := filepath.Join(t.TempDir(), "blogging")
	second := filepath.Join(t.TempDir(), "accounting")
	moved := filepath.Join(t.TempDir(), "blogging-moved")

	if _, err := UpsertModuleDev(socleDir, ModuleDevEntry{Identifier: "mod.blogging", Dir: first}); err != nil {
		t.Fatalf("upsert 1: %v", err)
	}
	if _, err := UpsertModuleDev(socleDir, ModuleDevEntry{Identifier: "mod.accounting", Dir: second}); err != nil {
		t.Fatalf("upsert 2: %v", err)
	}
	// Même identifiant, dossier neuf : l'entrée fantôme ne doit pas survivre.
	if _, err := UpsertModuleDev(socleDir, ModuleDevEntry{Identifier: "mod.blogging", Dir: moved}); err != nil {
		t.Fatalf("upsert 3: %v", err)
	}

	entries, err := ReadModuleDev(socleDir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2 (%v)", len(entries), entries)
	}
	if entries[0].Identifier != "mod.accounting" || entries[1].Identifier != "mod.blogging" {
		t.Fatalf("entries not sorted: %v", entries)
	}
	if entries[1].Dir != moved {
		t.Fatalf("dir = %q, want %q", entries[1].Dir, moved)
	}
	if entries[0].BoundAt == "" {
		t.Fatal("boundAt should be defaulted")
	}
	// Chemins absolus, écriture stable.
	if !filepath.IsAbs(entries[0].Dir) {
		t.Fatalf("dir %q is not absolute", entries[0].Dir)
	}
	raw := string(mustReadFile(t, ModuleDevPath(socleDir)))
	if !strings.Contains(raw, "\"modules\"") || !strings.HasSuffix(raw, "}\n") {
		t.Fatalf("unexpected file content:\n%s", raw)
	}
}

func TestRemoveModuleDevByIdentifierAndDir(t *testing.T) {
	socleDir := t.TempDir()
	dir := filepath.Join(t.TempDir(), "accounting")
	if _, err := UpsertModuleDev(socleDir, ModuleDevEntry{Identifier: "mod.accounting", Dir: dir}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Dossier résolu : l'entrée part.
	changed, err := RemoveModuleDev(socleDir, dir, "")
	if err != nil || !changed {
		t.Fatalf("remove by dir = (%v, %v), want (true, nil)", changed, err)
	}
	// Deuxième retrait : no-op idempotent.
	changed, err = RemoveModuleDev(socleDir, dir, "mod.accounting")
	if err != nil || changed {
		t.Fatalf("second remove = (%v, %v), want (false, nil)", changed, err)
	}

	// Identifiant seul : le dossier a bougé, l'entrée part quand même.
	if _, err := UpsertModuleDev(socleDir, ModuleDevEntry{Identifier: "mod.accounting", Dir: dir}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	changed, err = RemoveModuleDev(socleDir, filepath.Join(t.TempDir(), "elsewhere"), "mod.accounting")
	if err != nil || !changed {
		t.Fatalf("remove by identifier = (%v, %v), want (true, nil)", changed, err)
	}
	entries, err := ReadModuleDev(socleDir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %v, want empty", entries)
	}
}

func TestWriteModuleDevSkipsEmptyDirs(t *testing.T) {
	socleDir := t.TempDir()
	if err := WriteModuleDev(socleDir, []ModuleDevEntry{
		{Identifier: "kept", Dir: filepath.Join(t.TempDir(), "kept")},
		{Identifier: "void", Dir: "  "},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, err := ReadModuleDev(socleDir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entries) != 1 || entries[0].Identifier != "kept" {
		t.Fatalf("entries = %v, want only %q", entries, "kept")
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
