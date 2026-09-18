package cmd

import (
	"os"
	"strings"

	"github.com/protorians/liorian-cli/internal/config"
	"github.com/protorians/liorian-cli/internal/i18n"
	"github.com/spf13/cobra"
)

// i18n annotation keys storing the translation key of a command's help text.
const (
	i18nShortKey = "liorian.i18n.short"
	i18nLongKey  = "liorian.i18n.long"
)

// i18nHelp marks the Short and Long help texts of a command with their
// translation keys and initialises them with the current language (English at
// startup). applyLanguage later refreshes them when the UI language changes.
func i18nHelp(cmd *cobra.Command, shortKey, longKey string) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	if shortKey != "" {
		cmd.Annotations[i18nShortKey] = shortKey
		cmd.Short = i18n.T(shortKey)
	}
	if longKey != "" {
		cmd.Annotations[i18nLongKey] = longKey
		cmd.Long = i18n.T(longKey)
	}
}

// flagKeyEntry records a flag whose usage string must be re-rendered when the
// language changes.
type flagKeyEntry struct {
	cmd  *cobra.Command
	name string
	key  string
}

var flagKeys []flagKeyEntry

// i18nFlag sets the i18n key of a flag's usage string.
func i18nFlag(cmd *cobra.Command, name, key string) {
	flagKeys = append(flagKeys, flagKeyEntry{cmd: cmd, name: name, key: key})
	if f := cmd.Flag(name); f != nil {
		f.Usage = i18n.T(key)
	}
}

// ResolveLanguage picks the UI language in order of precedence:
//
//  1. the `--lang` flag,
//  2. the `LIORIAN_CLI_LANG` environment variable,
//  3. the `"cli".lang` key of `liorian.config.json`,
//  4. the OS locale (LC_ALL / LC_MESSAGES / LANG).
//
// It applies the selection and returns the effective catalog code.
func ResolveLanguage(args []string) string {
	if v := langFromArgs(args); v != "" {
		return i18n.Use(v)
	}
	if v := os.Getenv("LIORIAN_CLI_LANG"); v != "" {
		return i18n.Use(v)
	}
	if lang, ok := configLang(); ok {
		return i18n.Use(lang)
	}
	return i18n.Use(i18n.LangEnvOverride()())
}

// langFromArgs scans the raw arguments for `--lang <code>` or `--lang=<code>`
// before Cobra has parsed the flag set (Execute runs it to localise the help
// screens, which skip PersistentPreRun).
func langFromArgs(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "--lang" {
			if i+1 < len(args) {
				return strings.TrimSpace(args[i+1])
			}
			continue
		}
		if v, ok := strings.CutPrefix(args[i], "--lang="); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// configLang reads the `"cli".lang` key from the project config when the
// current directory sits inside a Liorian project.
func configLang() (string, bool) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	root, err := config.FindProjectRoot(cwd)
	if err != nil {
		return "", false
	}
	cfg, err := config.Load(config.ConfigPath(root))
	if err != nil {
		return "", false
	}
	if lang := strings.TrimSpace(cfg.Cli.Lang); lang != "" {
		return lang, true
	}
	return "", false
}

// langApplier is an indirection pointer to applyLanguage, breaking the
// rootCmd ↔ applyLanguage initialization cycle (the rootCmd literal closes
// over applyLanguage, which in turn reads rootCmd).
var langApplier = func() {}

func init() {
	langApplier = applyLanguage
}

// applyLanguage re-renders every i18n-tagged help text (command Short/Long)
// and flag usage in the active language. Called whenever the language is (re)
// selected, so `--help` screens and runtime errors follow the UI locale.
func applyLanguage() {
	for _, e := range flagKeys {
		if f := e.cmd.Flag(e.name); f != nil {
			f.Usage = i18n.T(e.key)
		}
	}
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if k, ok := c.Annotations[i18nShortKey]; ok && k != "" {
			c.Short = i18n.T(k)
		}
		if k, ok := c.Annotations[i18nLongKey]; ok && k != "" {
			c.Long = i18n.T(k)
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
}
