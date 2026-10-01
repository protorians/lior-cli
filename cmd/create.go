package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/module"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/tui"
	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create module [name]",
	Short: "Create a new module",
	Long: `Creates a new module in library/modules/{domain}/ from the embedded
hello-world mockup, renamed with the given identifier (manifest.json,
index.tsx, package.json, application/, domain/, infrastructure/,
presentation/).

The interactive wizard builds the module identity in three steps. First the
session is mandatory: the organization behind ` + "`liora connect`" + ` owns the slug
that anchors the identity, and an organization without a slug is forced to
define one through the CLI. Then the module type is asked among the supported
distribution kinds — it decides the canonical prefix of the reverse domain.
Finally the reverse domain is offered pre-filled
` + "`<prefix(type)>.<organization-slug>.`" + ` — the developer only completes the
<module-id>, and the whole identity is kept for the rest of the process.

The kebab-case identifier is deduced from the last label of the domain
(mod.liorian.accounting -> accounting). The optional page url drives both the
scaffolded src/app/{url}/ page and the manifest.json uri of the deployed
module (default: the identifier). --publisher overrides the session slug for
CI runs and scripted replays.

The module's unique UUID token is generated automatically.

Usage : liora create module [name] [--domain mod.org.app] [--id hello-world]`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCreate(cmd, args)
	},
}

var (
	createMockup      string
	createPageMockup  string
	createDomain      string
	createPublisher   string
	createID          string
	createAppName     string
	createVersion     string
	createIcon        string
	createURL         string
	createDescription string
	createType        string
	createCategory    string
	createSkipInstall bool
	createStandalone  bool
)

// createSkipInstallEnv disables the dependency installation step of
// `create module` (used by the unit test suite and CI-constrained runs).
const createSkipInstallEnv = "LIORIAN_CLI_SKIP_INSTALL"

func init() {
	createCmd.Flags().StringVar(&createMockup, "mockup", "", i18n.T("create.flag.mockup"))
	createCmd.Flags().StringVar(&createPageMockup, "page-mockup", "", i18n.T("create.flag.page_mockup"))
	createCmd.Flags().StringVar(&createDomain, "domain", "", i18n.T("create.flag.domain"))
	// --publisher names the publishing organization slug of the canonical
	// domain (`mod.<publisher>.<id>`) when --domain is omitted.
	createCmd.Flags().StringVar(&createPublisher, "publisher", "", i18n.T("create.flag.publisher"))
	createCmd.Flags().StringVar(&createID, "id", "", i18n.T("create.flag.id"))
	createCmd.Flags().StringVar(&createAppName, "name", "", i18n.T("create.flag.name"))
	createCmd.Flags().StringVar(&createVersion, "version", "", i18n.T("create.flag.version"))
	createCmd.Flags().StringVar(&createIcon, "icon", "", i18n.T("create.flag.icon"))
	createCmd.Flags().StringVar(&createURL, "url", "", i18n.T("create.flag.url"))
	createCmd.Flags().StringVar(&createDescription, "description", "", i18n.T("create.flag.description"))
	createCmd.Flags().StringVar(&createType, "type", "", i18n.T("create.flag.type"))
	createCmd.Flags().StringVar(&createCategory, "category", "", i18n.T("create.flag.category"))
	// --skip-install disables the dependency installation step (spec §5.2
	// step 6); LIORIAN_CLI_SKIP_INSTALL=1 is the environment equivalent.
	createCmd.Flags().BoolVar(&createSkipInstall, "skip-install", false, i18n.T("create.flag.skip_install"))
	// --standalone creates the module in a repository of its own, in the
	// current directory — the normal shape of a third-party module, which must
	// not require cloning the socle.
	createCmd.Flags().BoolVar(&createStandalone, "standalone", false, i18n.T("create.flag.standalone"))
	i18nHelp(createCmd, "cmd.create.short", "cmd.create.long")
	for _, name := range []string{"mockup", "page-mockup", "domain", "publisher", "id", "name", "version", "icon", "url", "description", "type", "category", "skip-install", "standalone"} {
		i18nFlag(createCmd, name, "create.flag."+name)
	}
}

func runCreate(cmd *cobra.Command, args []string) error {
	// Accept both `create module <name>` and `create <name>`.
	if len(args) > 0 && args[0] == "module" {
		args = args[1:]
	}
	if len(args) > 1 {
		return pkg.NewError(i18n.T("cat.module"), i18n.T("create.error.too_many"), pkg.ExitError)
	}

	// Le dépôt autonome crée sa propre racine : exiger un projet existant
	// empêcherait un développeur tiers de démarrer son module sans cloner le
	// socle — un détour qui n'a rien à voir avec son travail.
	root, err := createRoot()
	if err != nil {
		return err
	}

	spec := module.ModuleSpec{
		Domain:      strings.TrimSpace(createDomain),
		ID:          strings.TrimSpace(createID),
		AppName:     strings.TrimSpace(createAppName),
		Description: strings.TrimSpace(createDescription),
		Version:     strings.TrimSpace(createVersion),
		Icon:        strings.TrimSpace(createIcon),
		URL:         strings.TrimSpace(createURL),
		Type:        strings.TrimSpace(createType),
		Category:    strings.TrimSpace(createCategory),
	}

	// The positional argument is a convenience shorthand for the identifier.
	if len(args) > 0 {
		if spec.ID != "" && spec.ID != args[0] {
			return pkg.NewError(i18n.T("cat.module"), i18n.T("create.error.id_conflict"), pkg.ExitError)
		}
		spec.ID = args[0]
	}

	if err := collectCreateSpec(&spec); err != nil {
		return err
	}

	if createStandalone {
		return runCreateStandalone(root, spec)
	}

	creator := &module.Creator{Root: root, MockupDir: createMockup, PageMockup: createPageMockup}
	if createMockup != "" && !module.IsModuleMockup(createMockup) {
		warn(i18n.Tf("create.warn.mockup_ignored", createMockup))
	}
	if createPageMockup != "" && !pkg.FileExists(createPageMockup) {
		warn(i18n.Tf("create.warn.page_mockup_ignored", createPageMockup))
	}
	result, err := creator.Create(spec)
	if err != nil {
		if module.IsExistsError(err) {
			return pkg.NewError(i18n.T("cat.module"), err.Error(), pkg.ExitError)
		}
		return err
	}

	// Block the creation when a module required by the manifest (requirements)
	// is not available locally — library/modules/ or internal src/modules/.
	if missing := creator.MissingRequirements(result.Manifest); len(missing) > 0 {
		if err := os.RemoveAll(result.Dir); err != nil {
			debugf("rollback of %s: %v", result.Dir, err)
		}
		if result.Page != "" {
			if err := os.Remove(result.Page); err != nil {
				debugf("rollback of %s: %v", result.Page, err)
			}
		}
		return pkg.NewErrorWithFix(i18n.T("cat.module"),
			i18n.Tf("create.error.requirements_missing", strings.Join(missing, ", ")),
			i18n.T("create.error.requirements_missing.fix"), pkg.ExitError)
	}

	// Resolve the dependencies declared in the module package.json with the
	// first available package manager (bun → pnpm → yarn → npm).
	depsPM := ""
	if !createSkipInstall && os.Getenv(createSkipInstallEnv) == "" {
		var err error
		depsPM, err = tui.RunWithSpinner(i18n.T("create.spinner.deps"), func() (string, error) {
			return creator.ResolveDependencies(result.Dir)
		})
		if err != nil {
			warn(i18n.Tf("create.warn.install", err.Error()))
			depsPM = ""
		} else if depsPM == "" {
			warn(i18n.T("create.warn.pm_none"))
		}
	}

	s := tui.NewStyles()
	panel := s.Success.Render(i18n.Tf("create.success.dir", relToRoot(root, result.Dir))) + "\n" +
		s.Success.Render(i18n.Tf("create.success.token", result.Token)) + "\n" +
		s.Success.Render(i18n.T("create.success.manifest")) + "\n" +
		s.Success.Render(i18n.T("create.success.entry")) + "\n" +
		s.Success.Render(i18n.T("create.success.requirements"))
	if result.Page != "" {
		panel += "\n" + s.Success.Render(i18n.Tf("create.success.page", relToRoot(root, result.Page)))
	}
	if depsPM != "" {
		panel += "\n" + s.Success.Render(i18n.Tf("create.success.deps", depsPM))
	}
	fmt.Println()
	fmt.Println(s.SuccessPanel(panel))
	fmt.Println(s.StepsList(i18n.T("init.next"),
		s.Info.Render("liora connect"),
		s.Info.Render("liora pack "+result.Name),
		s.Info.Render("liora publish"),
	))
	fmt.Println()
	return nil
}

// collectCreateSpec completes the module spec with the interactive prompts and
// validates each answer before moving on to the next step.
//
// The wizard walks in the order the identity is built:
//
//  1. `liora connect` — the session is mandatory: it carries the organization
//     whose slug anchors the module identity, and an organization without a
//     slug is forced to define one through the CLI before anything is created;
//  2. the module type, asked among the supported distribution kinds — it
//     decides the canonical prefix of the reverse domain (config, system,
//     service, widget, theme, sinon mod);
//  3. the reverse domain, pre-filled `<prefix(type)>.<organization-slug>.` —
//     the developer only completes the `<module-id>`, and the whole identity
//     (prefix, slug, identifier) is kept for the rest of the process.
//
// The kebab-case module identifier is deduced from the last label of the
// domain (mod.liorian.accounting -> accounting), or the domain is composed
// from the effective type, the organization slug and the identifier when it
// is omitted. --publisher overrides the session slug for CI runs and scripted
// replays.
func collectCreateSpec(spec *module.ModuleSpec) error {
	interactive := tui.IsInteractive()

	// Step 1 — the type, asked among the supported kinds: it decides the
	// canonical prefix of the reverse domain.
	if interactive && spec.Type == "" {
		choice, err := tui.Select(i18n.T("create.prompt.type"), createTypeChoices())
		if err != nil {
			return err
		}
		spec.Type = createTypeValue(choice)
	}

	// Step 2 — the organization slug, recovered from the `liora connect`
	// session. The domain is composed from it — never from a free-typed
	// publisher — and an organization without a slug defines one right here.
	publisher := ""
	if interactive || spec.Domain == "" {
		p, err := organizationSlugForCreate(interactive, createPublisher)
		if err != nil {
			return err
		}
		publisher = p
	}

	// Step 3 — the reverse domain, pre-filled `<prefix(type)>.<slug>.`: the
	// developer completes the module identifier and the answer is validated
	// before the wizard moves on.
	if interactive && spec.Domain == "" && spec.ID == "" {
		prefill := module.CanonicalDomainPrefix(spec.EffectiveType()) + "." + publisher + "."
		d, err := askValidatedPrefilled(i18n.T("create.prompt.domain"), prefill,
			func(v string) error {
				if v == "" {
					return errors.New(i18n.T("create.error.no_domain"))
				}
				if err := module.ValidateDomain(v); err != nil {
					return err
				}
				return module.ValidateName(lastDomainLabel(v))
			})
		if err != nil {
			return err
		}
		spec.Domain = d
	}
	// The identifier is deduced from the domain when it was not given
	// explicitly: mod.liorian.accounting -> accounting.
	if spec.ID == "" && spec.Domain != "" {
		spec.ID = lastDomainLabel(spec.Domain)
	}
	// The domain is composed from the canonical identity when it was omitted:
	// <prefix(type)>.<publisher>.<id>. The publisher slug is the one resolved
	// from the session (step 2).
	if spec.Domain == "" {
		if spec.ID == "" {
			return pkg.NewError(i18n.T("cat.module"), i18n.T("create.error.no_domain"), pkg.ExitError)
		}
		spec.Domain = module.CanonicalDomain(spec.EffectiveType(), publisher, spec.ID)
	}
	if interactive {
		if spec.AppName == "" {
			def := module.DisplayName(lastDomainLabel(spec.Domain))
			n, err := tui.AskText(i18n.T("create.prompt.app_name"), def)
			if err != nil {
				return err
			}
			spec.AppName = strings.TrimSpace(n)
			if spec.AppName == "" {
				spec.AppName = def
			}
		}
		if spec.Version == "" {
			v, err := askValidated(i18n.T("create.prompt.version"), "0.0.0", module.ValidateVersion)
			if err != nil {
				return err
			}
			spec.Version = v
		}
		if spec.Icon == "" {
			icon, err := askValidated(i18n.T("create.prompt.icon"), "PuzzleIcon", module.ValidateIcon)
			if err != nil {
				return err
			}
			spec.Icon = icon
		}
		if spec.URL == "" {
			u, err := tui.AskText(i18n.T("create.prompt.url"), moduleURLPlaceholder(spec.Domain))
			if err != nil {
				return err
			}
			spec.URL = strings.TrimSpace(u)
		}
		if spec.Description == "" {
			desc, err := tui.AskText(i18n.T("create.prompt.description"), "")
			if err != nil {
				return err
			}
			spec.Description = strings.TrimSpace(desc)
		}
	}

	if spec.Domain == "" {
		return pkg.NewError(i18n.T("cat.module"), i18n.T("create.error.no_domain"), pkg.ExitError)
	}
	if err := module.ValidateDomain(spec.Domain); err != nil {
		return pkg.NewError(i18n.T("cat.module"), err.Error(), pkg.ExitError)
	}
	if err := module.ValidateName(spec.ID); err != nil {
		return pkg.NewError(i18n.T("cat.module"), err.Error(), pkg.ExitError)
	}
	if err := module.ValidateVersion(spec.Version); err != nil {
		return pkg.NewError(i18n.T("cat.module"), err.Error(), pkg.ExitError)
	}
	if err := module.ValidateIcon(spec.Icon); err != nil {
		return pkg.NewError(i18n.T("cat.module"), err.Error(), pkg.ExitError)
	}
	if err := module.ValidateType(spec.Type); err != nil {
		return pkg.NewError(i18n.T("cat.module"), err.Error(), pkg.ExitError)
	}
	if err := module.ValidateCategory(spec.Category); err != nil {
		return pkg.NewError(i18n.T("cat.module"), err.Error(), pkg.ExitError)
	}
	// The domain is aligned on the canonical form of the effective type — the
	// prefix depends on it (mod|config|system|service|widget|theme) — so a
	// `com.organization.module` habit never produces an identity the store
	// would refuse. The publisher and identifier labels are preserved.
	canonical, rewritten, err := module.CanonicalizeDomain(spec.Domain, spec.EffectiveType())
	if err != nil {
		return pkg.NewError(i18n.T("cat.module"), err.Error(), pkg.ExitError)
	}
	if rewritten {
		warn(i18n.Tf("create.warn.domain_prefix", spec.Domain, canonical))
		spec.Domain = canonical
	}
	return nil
}

// createTypeChoice is one entry of the interactive type menu: the label shows
// the canonical prefix the type puts in the reverse domain, the value is the
// canonical `ModuleType` enum.
type createTypeChoice struct {
	label string
	value string
}

// createTypeChoices lists the supported module types, in the order a developer
// meets them: the web-application forms first (all sharing the `mod` prefix),
// then the platform kinds owning their own prefix. The legacy `INTERNAL` /
// `EXTERNAL` aliases are deprecated and never offered.
var createTypeChoicesList = []createTypeChoice{
	{"WEB_APP_LOCAL (mod.*)", "WEB_APP_LOCAL"},
	{"WEB_APP_REMOTE (mod.*)", "WEB_APP_REMOTE"},
	{"WEB_APP_CACHED (mod.*)", "WEB_APP_CACHED"},
	{"EXTERNAL_URL (mod.*)", "EXTERNAL_URL"},
	{"REMOTE_FRONTEND (mod.*)", "REMOTE_FRONTEND"},
	{"CONFIGURATION (config.*)", "CONFIGURATION"},
	{"SYSTEM (system.*)", "SYSTEM"},
	{"SERVICE (service.*)", "SERVICE"},
	{"WIDGET (widget.*)", "WIDGET"},
	{"THEME (theme.*)", "THEME"},
}

// createTypeChoices returns the labels shown by the interactive type menu.
func createTypeChoices() []string {
	labels := make([]string, 0, len(createTypeChoicesList))
	for _, choice := range createTypeChoicesList {
		labels = append(labels, choice.label)
	}
	return labels
}

// createTypeValue maps a menu label back to its canonical type value; an
// unknown label falls back to the bare label (the value itself).
func createTypeValue(label string) string {
	for _, choice := range createTypeChoicesList {
		if choice.label == label {
			return choice.value
		}
	}
	return strings.TrimSpace(label)
}

// askValidated prompts for a single value and re-asks until it validates, so
// an incorrect answer never lets the wizard move on to the next step.
func askValidated(title, placeholder string, validate func(string) error) (string, error) {
	for {
		value, err := tui.AskText(title, placeholder)
		if err != nil {
			return "", err
		}
		value = strings.TrimSpace(value)
		if validate == nil {
			return value, nil
		}
		if err := validate(value); err != nil {
			s := tui.NewStyles()
			fmt.Fprintln(os.Stderr, s.Error.Render("✗ "+err.Error()))
			continue
		}
		return value, nil
	}
}

// askValidatedPrefilled is askValidated over a pre-filled input: the answer
// starts at the given value (cursor at the end) and the developer completes
// the missing part.
func askValidatedPrefilled(title, prefill string, validate func(string) error) (string, error) {
	for {
		value, err := tui.AskTextPrefilled(title, prefill)
		if err != nil {
			return "", err
		}
		value = strings.TrimSpace(value)
		if err := validate(value); err != nil {
			s := tui.NewStyles()
			fmt.Fprintln(os.Stderr, s.Error.Render("✗ "+err.Error()))
			continue
		}
		return value, nil
	}
}

// lastDomainLabel returns the last dot-separated label of a reverse-DNS
// identifier (com.organization.blog-manager -> blog-manager).
func lastDomainLabel(domain string) string {
	if i := strings.LastIndex(domain, "."); i >= 0 {
		return domain[i+1:]
	}
	return domain
}

// moduleURLPlaceholder derives the suggested page URL from a reverse-DNS module
// identifier, dropping the TLD segment and turning the remaining labels into
// path segments (com.org.test -> /org/test).
func moduleURLPlaceholder(domain string) string {
	domain = strings.TrimSpace(domain)
	if i := strings.Index(domain, "."); i >= 0 {
		return "/" + strings.ReplaceAll(domain[i+1:], ".", "/")
	}
	return "/" + domain
}
