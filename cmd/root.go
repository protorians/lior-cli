package cmd

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"text/template"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/protorians/sentient-cli/internal/appconfig"
	"github.com/protorians/sentient-cli/internal/i18n"
	"github.com/protorians/sentient-cli/internal/pkg"
	"github.com/protorians/sentient-cli/internal/tui"
	"github.com/spf13/cobra"
)

var (
	flagVerbose bool
	flagNoColor bool
	flagLang    string
)

var rootCmd = &cobra.Command{
	Use:   "sentients",
	Short: "Sentient CLI — Development tool for Sentient modules",
	Long: `Sentient CLI is the one development tool to create, maintain
and publish modules in the Sentient ecosystem.

Full lifecycle: init → create → develop → debug → audit → pack → sign → publish.
`,
	Example: `  sentients init
  sentients create module
  sentients connect
  sentients pack com.example.blog-manager
  sentients sign com.example.blog-manager
  sentients publish com.example.blog-manager
  sentients audit`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// The --lang flag is parsed after Execute resolved the language, so it
		// has the last word for command runs.
		if flagLang != "" {
			i18n.Use(flagLang)
			langApplier()
		}
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
	rootCmd.PersistentFlags().StringVar(&flagLang, "lang", "", "language / UI locale (fr-FR, en-US, …)")
	i18nFlag(rootCmd, "verbose", "flag.verbose")
	i18nFlag(rootCmd, "no-color", "flag.no_color")
	i18nFlag(rootCmd, "lang", "flag.lang")

	rootCmd.SetVersionTemplate("sentients {{.Version}}\n")

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
		testCmd,
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
	root.SetHelpTemplate(`{{if eq .Name "sentients"}}{{wordmark}}{{println}}{{println}}{{end}}{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}

{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}`)
}

// Execute runs the CLI. The version/commit/date build info comes from
// ldflags (see .goreleaser.yaml); appConfig is the `app.config.json` registry
// embedded in the binary (see main.go).
func Execute(version, commit, date string, appConfig []byte) {
	// Parameterise the CLI with the embedded workspace registry: API base
	// URLs and timeouts. A local `app.config.json` overrides it at call time.
	appconfig.SetEmbedded(appConfig)

	// rootCmd.Version is read by Cobra at execution time, so it is computed
	// here (not in init) to pick up the values injected via ldflags.
	rootCmd.Version = fmt.Sprintf("v%s (%s/%s) %s", version, runtime.GOOS, runtime.GOARCH, commit)

	// Set the CLI version for the User-Agent header.
	pkg.SetCLIVersion(version)

	// NFR-007: resolve and apply the UI language (flag > env > config > OS
	// locale) before any output, including help screens.
	_ = ResolveLanguage(os.Args[1:])
	applyLanguage()

	if flagNoColor {
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

// debugf logs a verbose line to stderr when --verbose (or the debug env var)
// is enabled (spec NFR-005).
func debugf(format string, args ...any) {
	if !flagVerbose && os.Getenv("SENTIENT_CLI_DEBUG") == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "[sentients] "+format+"\n", args...)
}

// warn prints a warning to stderr.
func warn(message string) {
	s := tui.NewStyles()
	fmt.Fprintln(os.Stderr, s.Warning.Render("⚠ "+message))
}
