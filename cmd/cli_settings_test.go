package cmd

import (
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/jetbrains/lior-cli/internal/config"
	"github.com/jetbrains/lior-cli/internal/i18n"
)

func TestNoColorFromArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		val  bool
		set  bool
	}{
		{"no flag", []string{"publish"}, false, false},
		{"plain", []string{"--no-color"}, true, true},
		{"equals true", []string{"--no-color=true"}, true, true},
		{"equals false", []string{"--no-color=false"}, false, true},
		{"equals other value ignored", []string{"--no-color=banana"}, true, true},
		{"after command", []string{"pack", "--no-color"}, true, true},
		{"unrelated flag only", []string{"--verbose"}, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			val, set := noColorFromArgs(c.args)
			if val != c.val || set != c.set {
				t.Errorf("noColorFromArgs(%v) = (%v, %v), want (%v, %v)", c.args, val, set, c.val, c.set)
			}
		})
	}
}

func TestPersistCliSettingsWritesConfig(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile(config.ConfigFileName, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	flagLang = "fr-FR"
	flagNoColor = true
	flagVerbose = true
	if err := rootCmd.ParseFlags([]string{"--lang", "fr-FR", "--no-color", "--verbose"}); err != nil {
		t.Fatal(err)
	}
	defer resetRootFlags()

	persistCliSettings(rootCmd)

	cfg, err := config.Load(config.ConfigPath(root))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Cli.Lang != "fr-FR" {
		t.Errorf("Cli.Lang = %q, want fr-FR", cfg.Cli.Lang)
	}
	if !cfg.Cli.NoColor {
		t.Error("Cli.NoColor doit être true")
	}
	if !cfg.Debug.Verbose {
		t.Error("Debug.Verbose doit être true")
	}
}

// resetRootFlags restores the shared root persistent flags to their defaults
// so tests do not leak state into one another.
func resetRootFlags() {
	flagLang, flagNoColor, flagVerbose = "", false, false
	for _, name := range []string{"lang", "no-color", "verbose"} {
		f := rootCmd.PersistentFlags().Lookup(name)
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	}
}

func TestPersistCliSettingsNoFlagsNoWrite(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile(config.ConfigFileName, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	flagLang = ""
	flagNoColor = false
	flagVerbose = false

	persistCliSettings(rootCmd)

	cfg, err := config.Load(config.ConfigPath(root))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Cli.Lang != "" || cfg.Cli.NoColor || cfg.Debug.Verbose {
		t.Errorf("config doit rester inchangée, got %+v", cfg.Cli)
	}
}

func TestPersistCliSettingsNoProjectNoWrite(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	flagLang = "fr-FR"
	if err := rootCmd.ParseFlags([]string{"--lang", "fr-FR"}); err != nil {
		t.Fatal(err)
	}
	defer resetRootFlags()

	persistCliSettings(rootCmd)

	if _, err := os.Stat(config.ConfigFileName); !os.IsNotExist(err) {
		t.Errorf("aucun config ne doit être écrit hors projet: %v", err)
	}
}

func TestExecuteHonorsPersistedNoColor(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	cfg := config.Default()
	cfg.Cli.Lang = "fr-FR"
	cfg.Cli.NoColor = true
	if err := cfg.Save(config.ConfigPath(root)); err != nil {
		t.Fatal(err)
	}

	t.Setenv("LIORIAN_CLI_SKIP_UPDATE", "1")
	t.Setenv("LIORIAN_CLI_LANG", "")

	before := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(before)

	origArgs := os.Args
	os.Args = []string{"liorian"}
	defer func() { os.Args = origArgs }()

	Execute("dev", "none", "unknown", nil)

	if got := lipgloss.ColorProfile(); got != termenv.Ascii {
		t.Errorf("ColorProfile = %v, want termenv.Ascii (persisted noColor)", got)
	}
	if got := i18n.Language(); got != "fr-FR" {
		t.Errorf("Language = %q, want fr-FR (persisted lang)", got)
	}
	i18n.Use("en-US")
}

func TestExecutePersistsFlags(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile(config.ConfigFileName, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("LIORIAN_CLI_SKIP_UPDATE", "1")

	before := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(before)

	origArgs := os.Args
	os.Args = []string{"liorian", "--lang", "fr-FR", "--no-color", "--verbose"}
	defer func() {
		os.Args = origArgs
		flagLang, flagNoColor, flagVerbose = "", false, false
		i18n.Use("en-US")
	}()

	Execute("dev", "none", "unknown", nil)

	cfg, err := config.Load(config.ConfigPath(root))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Cli.Lang != "fr-FR" {
		t.Errorf("Cli.Lang = %q, want fr-FR persisted", cfg.Cli.Lang)
	}
	if !cfg.Cli.NoColor {
		t.Error("Cli.NoColor doit être persisté à true")
	}
	if !cfg.Debug.Verbose {
		t.Error("Debug.Verbose doit être persisté à true")
	}
}
