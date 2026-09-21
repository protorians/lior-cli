package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/tui"
	"github.com/spf13/cobra"
)

// setupTemplateRepo creates a local template directory usable as the init
// template source (LIORIAN_CLI_TEMPLATE_REPO) so tests stay offline.
func setupTemplateRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := pkg.WriteFile(filepath.Join(dir, name), []byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// installFakeBun adds a no-op `bun` on the PATH so package manager detection and
// installation succeed deterministically.
func installFakeBun(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "bun"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestDirIsEmptyAndClearDirContents(t *testing.T) {
	dir := t.TempDir()
	if !dirIsEmpty(dir) {
		t.Error("a fresh directory must be considered empty")
	}
	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if dirIsEmpty(dir) {
		t.Error("a directory with content must not be considered empty")
	}
	if err := clearDirContents(dir); err != nil {
		t.Fatal(err)
	}
	if !dirIsEmpty(dir) {
		t.Error("the contents must have been cleared")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("the directory itself must be kept: %v", err)
	}
}

func TestConfirmExistingDirNonInteractive(t *testing.T) {
	t.Setenv("PATH", "/nonexistent")

	t.Setenv(tui.ConfirmYesEnv, "")
	action, err := confirmExistingDir("dir", false)
	if action != destActionAbort {
		t.Errorf("without LIORIAN_CLI_YES the action must abort, got %d", action)
	}
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("without LIORIAN_CLI_YES the call must refuse, got %v", err)
	}

	t.Setenv(tui.ConfirmYesEnv, "1")
	action, err = confirmExistingDir("dir", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != destActionClear {
		t.Errorf("with LIORIAN_CLI_YES the action must be clear, got %d", action)
	}
}

func TestRunInitInCWDUntouchedWithoutApproval(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile("existing.txt", []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(tui.ConfirmYesEnv, "")

	err := runInit(&cobra.Command{}, []string{"."})
	if err == nil {
		t.Fatal("initializing into a non-empty current directory must require approval")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected an 'already exists' error, got: %v", err)
	}
	if _, statErr := os.Stat("existing.txt"); statErr != nil {
		t.Errorf("existing content must remain untouched after refusal: %v", statErr)
	}
}

func TestRunInitEmptyDestinationProceedsToPackageManagerDetection(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("PATH", "/nonexistent")

	err := runInit(&cobra.Command{}, []string{"fresh-app"})
	if err == nil {
		t.Fatal("expected a package-manager error")
	}
	if !strings.Contains(err.Error(), "no package manager detected") {
		t.Errorf("expected the package-manager error, got: %v", err)
	}
	if _, statErr := os.Stat("fresh-app"); !os.IsNotExist(statErr) {
		t.Errorf("fresh-app must not have been created: %v", statErr)
	}
}

func TestRunInitClearsNonEmptyDirectoryWithApproval(t *testing.T) {
	repo := setupTemplateRepo(t, map[string]string{
		"package.json":             `{"name":"liorian-socle","scripts":{"dev":"vite"}}`,
		"library/modules/.gitkeep": "",
		"lorian.config.toml":       "app = \"template\"\n",
	})
	bin := installFakeBun(t)

	root := t.TempDir()
	t.Chdir(root)
	if err := os.MkdirAll("proj", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("proj/existing.txt", []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("LIORIAN_CLI_TEMPLATE_REPO", repo)
	t.Setenv(tui.ConfirmYesEnv, "1")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := runInit(&cobra.Command{}, []string{"proj"}); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	if _, err := os.Stat("proj/existing.txt"); !os.IsNotExist(err) {
		t.Errorf("existing.txt must have been removed: %v", err)
	}
	if _, err := os.Stat("proj/package.json"); err != nil {
		t.Errorf("the template must have been cloned: %v", err)
	}
	if _, err := os.Stat("proj/lorian.config.json"); err != nil {
		t.Errorf("the config file must have been written: %v", err)
	}
}

func TestRunInitInCWDWithExistingContentAndApproval(t *testing.T) {
	repo := setupTemplateRepo(t, map[string]string{
		"package.json": `{"name":"liorian-socle","scripts":{"dev":"vite"}}`,
	})
	bin := installFakeBun(t)

	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile("existing.txt", []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("LIORIAN_CLI_TEMPLATE_REPO", repo)
	t.Setenv(tui.ConfirmYesEnv, "1")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := runInit(&cobra.Command{}, []string{"."}); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	if _, err := os.Stat("existing.txt"); !os.IsNotExist(err) {
		t.Errorf("existing.txt must have been removed: %v", err)
	}
	if _, err := os.Stat("package.json"); err != nil {
		t.Errorf("the template must have been cloned into the cwd: %v", err)
	}
	if _, err := os.Stat("lorian.config.json"); err != nil {
		t.Errorf("the config file must have been written: %v", err)
	}
}

func TestRunInitForcesLatestDependencies(t *testing.T) {
	repo := setupTemplateRepo(t, map[string]string{
		"package.json":       `{"name":"liorian-socle","dependencies":{"@liorian/sdk":"latest","react":"^19.0.0"}}`,
		"lorian.config.toml": "app = \"template\"\n",
	})

	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"" + logPath + "\"\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "bun"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("LIORIAN_CLI_TEMPLATE_REPO", repo)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := runInit(&cobra.Command{}, []string{"latest-app"}); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("the fake bun must have been called: %v", err)
	}
	calls := string(data)
	if !strings.Contains(calls, "install") {
		t.Errorf("bun install must run, got:\n%s", calls)
	}
	if !strings.Contains(calls, "add @liorian/sdk@latest") {
		t.Errorf("the explicit latest dependency must be force-installed, got:\n%s", calls)
	}
	if strings.Contains(calls, "react@latest") {
		t.Errorf("a ranged dependency must not be forced, got:\n%s", calls)
	}
}

func TestMergeTemplateInto(t *testing.T) {
	repo := setupTemplateRepo(t, map[string]string{
		"package.json": `{"name":"liorian-socle"}`,
		"README.md":    "template\n",
	})

	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, "existing.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "README.md"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := mergeTemplateInto(repo, "stable", dest, nil); err != nil {
		t.Fatalf("mergeTemplateInto: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dest, "existing.txt")); err != nil {
		t.Errorf("pre-existing files must be kept: %v", err)
	}
	readme, err := os.ReadFile(filepath.Join(dest, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(readme) != "template\n" {
		t.Errorf("conflicting template files must win, got %q", readme)
	}
	if _, err := os.Stat(filepath.Join(dest, "package.json")); err != nil {
		t.Errorf("template files must be copied over: %v", err)
	}
}

func TestI18nInitDestKeysExist(t *testing.T) {
	for _, key := range []string{
		"init.dest",
		"init.prompt.existing",
		"init.choice.cancel",
		"init.choice.clear",
		"init.choice.delete",
		"init.choice.merge",
		"init.error.clear",
		"init.spinner.merge",
		"init.spinner.clone",
		"init.release.meta",
		"init.release.unknown",
		"init.flag.channel",
		"init.error.channel_invalid",
	} {
		if i18n.T(key) == key {
			t.Errorf("i18n key %q must resolve to a message", key)
		}
	}
}

func TestReleaseDetailShowsMetadata(t *testing.T) {
	i18n.Use("en-US")
	defer i18n.Use("en-US")

	detail := releaseDetail(pkg.ReleaseInfo{
		Version: "v0.23.0-alpha.1",
		Channel: "alpha",
		Branch:  "71892a5a3620f6187dd63d9320eb0247710c1acd",
		Commit:  "71892a5a3620f6187dd63d9320eb0247710c1acd",
	})
	for _, want := range []string{"release", "v0.23.0-alpha.1", "channel", "alpha", "branch", "commit", "71892a5a"} {
		if !strings.Contains(detail, want) {
			t.Errorf("releaseDetail() = %q, must contain %q", detail, want)
		}
	}
}

func TestReleaseDetailFallsBackOnMissingFields(t *testing.T) {
	i18n.Use("en-US")
	defer i18n.Use("en-US")

	detail := releaseDetail(pkg.ReleaseInfo{Version: "v0.23.0", Channel: "stable"})
	if !strings.Contains(detail, "unknown") {
		t.Errorf("releaseDetail() = %q, must flag missing branch/commit", detail)
	}
}

func TestRunInitInvalidChannel(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("PATH", "/nonexistent")

	initChannel = "nightly"
	defer func() { initChannel = "" }()

	err := runInit(&cobra.Command{}, nil)
	if err == nil {
		t.Fatal("an invalid channel must be refused")
	}
	if !strings.Contains(err.Error(), "unknown channel") {
		t.Errorf("expected the unknown-channel error, got: %v", err)
	}
}

func TestGithubOwnerRepo(t *testing.T) {
	cases := []struct {
		in    string
		owner string
		name  string
	}{
		{"https://github.com/protorians/liorian-socle", "protorians", "liorian-socle"},
		{"https://github.com/protorians/liorian-socle.git", "protorians", "liorian-socle"},
		{"https://github.com/protorians/liorian-socle/", "protorians", "liorian-socle"},
		{"github.com/protorians/liorian-socle", "protorians", "liorian-socle"},
		{"protorians/liorian-socle", "protorians", "liorian-socle"},
		{"not-a-github-url", "", ""},
		{"https://example.com/template.zip", "", ""},
		{"https://github.com/protorians", "", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		owner, name := githubOwnerRepo(c.in)
		if owner != c.owner || name != c.name {
			t.Errorf("githubOwnerRepo(%q) = (%q, %q), want (%q, %q)", c.in, owner, name, c.owner, c.name)
		}
	}
}
