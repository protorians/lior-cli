package cmd

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"text/template"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/protorians/lior-cli/internal/appconfig"
	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/tui"
	"github.com/spf13/cobra"
)

var (
	flagVerbose bool
	flagNoColor bool
	flagColor   bool
	flagLang    string
)

var rootCmd = &cobra.Command{
	Use:   "liorian",
	Short: "Lior CLI — Development tool for Liorian modules",
	Long: `Lior CLI is the one development tool to create, maintain
and publish modules in the Liorian ecosystem.

Full lifecycle: init → create → develop → debug → audit → pack → sign → publish.
`,
	Example: `  liorian init
  liorian create module
  liorian connect
  liorian pack com.example.blog-manager
  liorian sign com.example.blog-manager
  liorian publish com.example.blog-manager
  liorian audit`,
	SilenceUsage:  true,
	SilenceErrors: true,
	// TraverseChildren parses the root (global) flags before descending into a
	// subcommand. Combined with DisableFlagParsing on the toolchain commands,
	// this keeps `liorian --no-color dev` valid while forwarding everything
	// after `dev` verbatim to the backing script.
	TraverseChildren: true,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return nil
		}
		msg := fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath())
		if cmd.DisableSuggestions {
			return errors.New(msg)
		}
		if cmd.SuggestionsMinimumDistance <= 0 {
			cmd.SuggestionsMinimumDistance = 2
		}
		if suggestions := cmd.SuggestionsFor(args[0]); len(suggestions) > 0 {
			var sb strings.Builder
			sb.WriteString(msg)
			sb.WriteString("\n\nDid you mean this?\n")
			for _, s := range suggestions {
				fmt.Fprintf(&sb, "\t%v\n", s)
			}
			return errors.New(sb.String())
		}
		return errors.New(msg)
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// The --lang flag is parsed after Execute resolved the language, so it
		// has the last word for command runs.
		if flagLang != "" {
			i18n.Use(flagLang)
			langApplier()
		}
		// Persist --lang / --no-color / --verbose so the choice applies to
		// every subsequent run inside the project.
		persistCliSettings(cmd)
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	i18nHelp(rootCmd, "cmd.root.short", "cmd.root.long")
	rootCmd.Example = i18n.T("cmd.root.example")

	rootCmd.PersistentFlags().BoolVar(&flagVerbose, "verbose", false, "enable verbose logs")
	rootCmd.PersistentFlags().BoolVar(&flagNoColor, "no-color", false, "disable colors")
	rootCmd.PersistentFlags().BoolVar(&flagColor, "color", false, "force colors")
	rootCmd.PersistentFlags().StringVar(&flagLang, "lang", "", "language / UI locale (fr-FR, en-US, …)")
	i18nFlag(rootCmd, "verbose", "flag.verbose")
	i18nFlag(rootCmd, "no-color", "flag.no_color")
	i18nFlag(rootCmd, "color", "flag.color")
	i18nFlag(rootCmd, "lang", "flag.lang")

	rootCmd.SetVersionTemplate("liorian {{.Version}}\n")

	// Themed help: section labels are tinted when colors are enabled, and
	// degrade to plain text under --no-color (so E2E regexes stay stable).
	setupHelpTemplates(rootCmd)

	rootCmd.AddCommand(
		initCmd,
		createCmd,
		connectCmd,
		authCmd,
		disconnectCmd,
		packCmd,
		signCmd,
		publishCmd,
		linkCmd,
		unlinkCmd,
		debugCmd,
		auditCmd,
		repairCmd,
		testCmd,
		marketplaceCmd,
		moduleCmd,
		devCmd,
		buildCmd,
		startCmd,
		checkCmd,
	)
}

// setupHelpTemplates installs a lightly themed usage/help template on root and
// adds the template helpers used by those templates. Subcommands inherit the
// root templates automatically.
func setupHelpTemplates(root *cobra.Command) {
	cobra.AddTemplateFuncs(template.FuncMap{
		"hl":  func(s string) string { return tui.NewStyles().Accent.Render(s) },
		"dim": func(s string) string { return tui.NewStyles().Muted.Render(s) },
		// wordmark prints the brand badge; it degrades to plain text under
		// --no-color (Ascii profile), keeping E2E regexes stable.
		"wordmark": func() string { return tui.NewStyles().Wordmark() },
	})

	const usageTmpl = `Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

{{hl "Aliases:"}}
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

{{hl "Examples:"}}
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

{{hl "Available Commands:"}}{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

{{hl "Additional Commands:"}}{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

{{hl "Flags:"}}
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

{{hl "Global Flags:"}}
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

{{hl "Additional help topics:"}}{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

{{dim (printf "Use \"%s [command] --help\" for more information about a command." .CommandPath)}}{{end}}`

	root.SetUsageTemplate(usageTmpl)

	// Help template: brand wordmark on top of the main screen, then the
	// description and the (shared) usage template.
	root.SetHelpTemplate(`{{if eq .Name "liorian"}}{{wordmark}}{{println}}{{println}}{{end}}{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}

{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}`)
}

// Execute runs the CLI. The version/branch/commit/date build info comes from
// `app.config.json` (aligned in main.go) or ldflags (see .goreleaser.yaml);
// appConfig is the `app.config.json` registry embedded in the binary (see
// main.go).
func Execute(version, branch, commit, date string, appConfig []byte) {
	// Parameterise the CLI with the embedded workspace registry: API base
	// URLs and timeouts. A local `app.config.json` overrides it at call time.
	appconfig.SetEmbedded(appConfig)

	// rootCmd.Version is read by Cobra at execution time, so it is computed
	// here (not in init) to pick up the values injected via ldflags.
	rootCmd.Version = fmt.Sprintf("v%s (%s/%s) %s (%s)", version, runtime.GOOS, runtime.GOARCH, branch, commit)

	// Set the CLI version for the User-Agent header.
	pkg.SetCLIVersion(version)

	// NFR-007: resolve and apply the UI language (flag > env > config > OS
	// locale) before any output, including help screens.
	_ = ResolveLanguage(os.Args[1:])
	applyLanguage()

	// Colors: honour the effective `cli.noColor` config unless the raw
	// `--color` / `--no-color[=true|false]` flags override it (flag > config).
	// The raw arguments are scanned here because Cobra parses flags only inside
	// rootCmd.Execute(), i.e. after the first help/version output.
	if noColor, set := colorPreferenceFromArgs(os.Args[1:]); set {
		if noColor {
			lipgloss.SetColorProfile(termenv.Ascii)
		} else {
			lipgloss.SetColorProfile(forcedColorProfile())
		}
	} else if configNoColor() {
		lipgloss.SetColorProfile(termenv.Ascii)
	}

	// NFR-006: check for updates (non-blocking, cached daily)
	if updateMsg := pkg.CheckForUpdate(version); updateMsg != "" {
		s := tui.NewStyles()
		fmt.Fprintln(os.Stderr, s.Info.Render(updateMsg))
	}

	if err := rootCmd.Execute(); err != nil {
		printCmdError(err)
		os.Exit(pkg.ExitCodeFor(err))
	}
}

// forcedColorProfile returns the richest color profile the terminal supports,
// bypassing the TTY detection (and NO_COLOR) that normally disables colors when
// stdout is redirected. It backs the `--color` flag, which re-enables colors.
func forcedColorProfile() termenv.Profile {
	out := termenv.NewOutput(os.Stdout, termenv.WithTTY(true))
	if p := out.ColorProfile(); p != termenv.Ascii {
		return p
	}
	return termenv.ANSI
}

// printCmdError renders an error in a soft, framed error card.
func printCmdError(err error) {
	s := tui.NewStyles()
	if e, ok := err.(*pkg.Error); ok {
		var b strings.Builder
		b.WriteString(s.Error.Render("✗ "+e.Category) + "\n")
		b.WriteString("  " + s.Error.Render(e.Message) + "\n")
		if e.Fix != "" {
			b.WriteString(s.Hint.Render("  → " + e.Fix))
		}
		fmt.Fprintln(os.Stderr, s.ErrorPanel(strings.TrimSuffix(b.String(), "\n")))
		return
	}
	fmt.Fprintln(os.Stderr, s.ErrorPanel(s.Error.Render("✗ Error: "+err.Error())))
}

// debugConfigFlags caches whether the project's lorian.config.json `debug`
// section opts into verbose logging (spec §6.1, NFR-005): `debug.verbose:
// true` or `debug.logLevel: "debug"`. Resolved once per process from the
// current project root.
var (
	debugConfigOnce    sync.Once
	debugConfigVerbose bool
)

// debugConfigVerboseEnabled reports whether the project configuration requests
// verbose logs. It never fails: without a project or a config, it returns
// false (the `--verbose` flag and LIORIAN_CLI_DEBUG remain the main switches).
func debugConfigVerboseEnabled() bool {
	debugConfigOnce.Do(func() {
		cwd, err := os.Getwd()
		if err != nil {
			return
		}
		root, err := config.FindProjectRoot(cwd)
		if err != nil {
			return
		}
		cfg, err := config.Load(config.ConfigPath(root))
		if err != nil {
			return
		}
		debugConfigVerbose = cfg.Debug.Verbose || strings.EqualFold(cfg.Debug.LogLevel, "debug")
	})
	return debugConfigVerbose
}

// debugf logs a verbose line to stderr when --verbose (or the debug env var,
// or the project's `debug` config) is enabled (spec NFR-005).
func debugf(format string, args ...any) {
	if !flagVerbose && os.Getenv("LIORIAN_CLI_DEBUG") == "" && !debugConfigVerboseEnabled() {
		return
	}
	fmt.Fprintf(os.Stderr, "[liorian] "+format+"\n", args...)
}

// warn prints a warning to stderr.
func warn(message string) {
	s := tui.NewStyles()
	fmt.Fprintln(os.Stderr, s.Warning.Render("⚠ "+message))
}

// persistCliSettings records the `--lang`, `--color` / `--no-color` and
// `--verbose` root flags in the project's lorian.config.json so the choice
// survives across runs. Only flags the user explicitly passed are written (each one keeps its
// own precedence over the config on later runs). Without a project root, the
// settings cannot be persisted and the call is a silent no-op.
func persistCliSettings(root *cobra.Command) {
	langSet := root.Flags().Changed("lang")
	noColorSet := root.Flags().Changed("no-color")
	colorSet := root.Flags().Changed("color")
	verboseSet := root.Flags().Changed("verbose")
	if !langSet && !noColorSet && !colorSet && !verboseSet {
		return
	}

	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	projectRoot, err := config.FindProjectRoot(cwd)
	if err != nil {
		return
	}

	cfg, err := config.Load(config.ConfigPath(projectRoot))
	if err != nil {
		debugf("loading config for CLI settings persistence: %v", err)
		return
	}

	changed := false
	if langSet && flagLang != "" && cfg.Cli.Lang != flagLang {
		cfg.Cli.Lang = flagLang
		changed = true
	}
	if noColorSet || colorSet {
		// `--no-color` wins when both flags are passed.
		want := cfg.Cli.NoColor
		switch {
		case noColorSet:
			want = flagNoColor
		case colorSet:
			want = !flagColor
		}
		if cfg.Cli.NoColor != want {
			cfg.Cli.NoColor = want
			changed = true
		}
	}
	if verboseSet && cfg.Debug.Verbose != flagVerbose {
		cfg.Debug.Verbose = flagVerbose
		changed = true
	}
	if !changed {
		return
	}
	if err := cfg.Save(config.ConfigPath(projectRoot)); err != nil {
		debugf("saving config after CLI settings: %v", err)
	}
}
