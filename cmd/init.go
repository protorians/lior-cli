package cmd

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/tui"
	"github.com/spf13/cobra"
)

// templateRepo is the repository whose release zip is downloaded by
// `liorian init` (spec FR-002). `LIORIAN_CLI_TEMPLATE_REPO` overrides it:
// it accepts a GitHub repository URL, a direct zip download URL or a local
// directory (useful for tests and mirrors).
const defaultTemplateRepo = "https://github.com/protorians/liorian-socle"

func templateRepo() string {
	if v := os.Getenv("LIORIAN_CLI_TEMPLATE_REPO"); v != "" {
		return v
	}
	return defaultTemplateRepo
}

// packageManagers is the detection + install order for `liorian init`.
var packageManagers = []struct {
	name       string
	installCmd []string
}{
	{name: "bun", installCmd: []string{"bun", "install"}},
	{name: "pnpm", installCmd: []string{"pnpm", "install"}},
	{name: "yarn", installCmd: []string{"yarn", "install"}},
	{name: "npm", installCmd: []string{"npm", "install"}},
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new Liorian project",
	Long: `Initializes a new Liorian project by downloading the release
archive of the template protorians/liorian-socle (ZIP) and installing
the dependencies.

The package manager is detected automatically (bun, pnpm, yarn, npm)
and offered to the user.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInit(cmd, args)
	},
}

var initChannel string
var initAutoEnv bool

func init() {
	initCmd.Flags().StringVar(&initChannel, "channel", "", "release channel (alpha, beta, rc, stable)")
	initCmd.Flags().BoolVar(&initAutoEnv, "auto-env", false, "auto-configure the .env without prompting")
	i18nHelp(initCmd, "cmd.init.short", "cmd.init.long")
	i18nFlag(initCmd, "channel", "init.flag.channel")
	i18nFlag(initCmd, "auto-env", "init.flag.auto_env")
}

func runInit(cmd *cobra.Command, args []string) error {
	projectName := ""
	if len(args) > 0 {
		projectName = args[0]
	}

	// Step 1 — project name
	if projectName == "" {
		if !tui.IsInteractive() {
			projectName = "liorian-socle"
		} else if envAutoConfigured() {
			// --auto-env (or LIORIAN_CLI_ENV_AUTO) means fully scripted setup:
			// derive the project name from the current directory without asking.
			projectName = defaultProjectName()
		} else {
			defaultName := defaultProjectName()
			name, err := tui.AskText(i18n.T("init.prompt.name"), defaultName)
			if err != nil {
				return err
			}
			projectName = strings.TrimSpace(name)
			if projectName == "" {
				projectName = defaultName
			}
		}
	}

	targetDir := projectName
	cwd, _ := os.Getwd()
	absTarget, _ := filepath.Abs(targetDir)
	isCWD := absTarget == cwd

	// Step 2 — release channel (spec FR-002): default to the latest stable
	// release and ignore alpha/beta/rc tags unless a channel is requested.
	if initChannel == "" {
		initChannel = string(pkg.ChannelStable)
	}
	if !pkg.IsValidChannel(initChannel) {
		return pkg.NewError(i18n.T("cat.project"),
			i18n.Tf("init.error.channel_invalid", initChannel, strings.Join(pkg.ValidChannels, ", ")), pkg.ExitError)
	}
	debugf("release channel: %s", initChannel)

	// Show the destination path before doing anything (init dest display fix).
	fmt.Println(tui.NewStyles().Info.Render(i18n.Tf("init.dest", absTarget)))

	mergeClone := false
	info, err := os.Stat(targetDir)
	if err == nil {
		if !info.IsDir() {
			return pkg.NewError(i18n.T("cat.project"), i18n.Tf("init.error.not_dir", targetDir), pkg.ExitError)
		}
		if !dirIsEmpty(targetDir) {
			// The destination already contains files: never clear, delete or
			// merge it without the user's approval.
			action, err := confirmExistingDir(targetDir, isCWD)
			if err != nil {
				return err
			}
			switch action {
			case destActionClear:
				if isCWD {
					if err := clearDirContents(targetDir); err != nil {
						return pkg.NewError(i18n.T("cat.project"), i18n.T("init.error.clear"), pkg.ExitError)
					}
				} else if err := os.RemoveAll(targetDir); err != nil {
					return pkg.NewError(i18n.T("cat.project"), i18n.T("init.error.remove"), pkg.ExitError)
				}
			case destActionMerge:
				mergeClone = true
			default:
				return pkg.NewErrorWithFix(i18n.T("cat.project"), i18n.Tf("init.error.exists", targetDir),
					i18n.T("init.error.exists.fix"), pkg.ExitError)
			}
		}
	}

	// Step 3 — package manager detection (spec FR-001)
	available, _ := tui.RunWithSpinner(i18n.T("init.spinner.pm"), func() ([]string, error) {
		found := make([]string, 0, len(packageManagers))
		for _, pm := range packageManagers {
			if pkg.HasCommand(pm.name) {
				found = append(found, pm.name)
			}
		}
		return found, nil
	})
	if len(available) == 0 {
		return pkg.NewErrorWithFix(
			i18n.T("cat.package_manager"),
			i18n.T("init.error.pm_none"),
			i18n.T("init.error.pm_none.fix"),
			pkg.ExitError,
		)
	}

	pmName := available[0] // bun is first; recommended default
	if len(available) > 1 && tui.IsInteractive() && !envAutoConfigured() {
		var items []string
		for i, name := range available {
			if i == 0 {
				items = append(items, name+i18n.T("init.hint.recommended"))
			} else {
				items = append(items, name)
			}
		}
		selected, err := tui.Select(i18n.T("init.prompt.pm"), items)
		if err != nil {
			return err
		}
		pmName = strings.TrimSuffix(selected, i18n.T("init.hint.recommended"))
	}
	debugf("selected package manager: %s", pmName)

	var installCmd []string
	for _, pm := range packageManagers {
		if pm.name == pmName {
			installCmd = pm.installCmd
			break
		}
	}
	if installCmd == nil {
		return pkg.NewError(i18n.T("cat.package_manager"), i18n.Tf("init.error.pm_unknown", pmName), pkg.ExitError)
	}

	// Step 4 — release channel download (spec FR-002). When the
	// destination already contained files and the user approved a merge,
	// download into a temporary sibling directory then copy the template over
	// the existing content. A progress bar shows the ZIP download level.
	cloneLabel := i18n.T("init.spinner.clone")
	if mergeClone {
		cloneLabel = i18n.T("init.spinner.merge")
	}

	// Resolve the release metadata (version, channel, branch, commit) up front
	// so the download line names exactly which build is installed, then reuse
	// the resolved release to avoid a second GitHub API round-trip. The metadata
	// is rendered as a muted line below the progress bar.
	var resolvedRelease *pkg.ReleaseInfo
	detail := ""
	if owner, name := githubOwnerRepo(templateRepo()); owner != "" && name != "" {
		info, err := pkg.ResolveRelease(owner, name, initChannel)
		if err != nil {
			debugf("resolving release metadata: %v", err)
		} else {
			resolvedRelease = &info
			detail = releaseDetail(info)
		}
	}

	if _, err := tui.RunWithProgressDetail(cloneLabel, detail, func(report tui.ReportFunc) (struct{}, error) {
		return struct{}{}, fetchTemplate(templateRepo(), initChannel, targetDir, resolvedRelease, report)
	}); err != nil {
		return pkg.NewErrorWithFix(i18n.T("cat.network"), err.Error(),
			i18n.T("init.error.clone.fix"), pkg.ExitNetwork)
	}

	// Step 5 — install dependencies. Dependencies pinned to the explicit
	// `latest` specifier in package.json are frequently ignored by a plain
	// install, so they are forced afterwards.
	if _, err := tui.RunWithSpinner(i18n.T("init.spinner.install"), func() (struct{}, error) {
		return struct{}{}, runInstall(targetDir, installCmd)
	}); err != nil {
		warn(i18n.Tf("init.warn.install", err.Error()))
	} else if _, err := pkg.ForceInstallLatest(targetDir, pmName); err != nil {
		warn(i18n.Tf("init.warn.install_latest", err.Error()))
	}

	// Step 6 — write lorian.config.json
	cfg := config.Default()
	cfg.Project.Name = projectName
	cfg.Project.PackageManager = pmName
	if err := cfg.Save(filepath.Join(targetDir, config.ConfigFileName)); err != nil {
		debugf("writing config file: %v", err)
	}

	// Step 7 — generate .env from the template sample. An existing .env is
	// never overwritten. Known variables are synthesised while the spinner
	// runs; the remaining ones are then offered as editable placeholders.
	// Unless auto-configuration is requested (--auto-env / LIORIAN_CLI_ENV_AUTO)
	// or the run is non-interactive, the developer is asked whether to accept
	// every suggestion at once; a "no" falls back to per-variable prompting,
	// where Esc accepts the remaining suggestions. Prompting happens outside
	// the spinner (a Bubbletea prompt cannot own the terminal while the
	// spinner program runs).
	var envCreated bool
	if envGenerationNeeded(targetDir) {
		plan, err := tui.RunWithSpinner(i18n.T("init.spinner.env"), func() (*envPlan, error) {
			return planEnvFromSample(targetDir, projectName)
		})
		if err != nil {
			warn(i18n.Tf("init.warn.env", err.Error()))
		} else if plan != nil {
			auto := envAutoConfigured()
			skip := false
			if !auto && tui.IsInteractive() {
				yes, err := tui.Confirm(i18n.T("init.prompt.env_auto"), true)
				if err != nil {
					warn(i18n.Tf("init.warn.env", err.Error()))
					skip = true
				} else {
					auto = yes
				}
			}
			if !skip {
				if err := plan.completeEnv(auto); err != nil {
					warn(i18n.Tf("init.warn.env", err.Error()))
				} else {
					envCreated = true
				}
			}
		}
	}

	// Step 8 — summary
	s := tui.NewStyles()
	summary := []string{
		s.Value.Render(strings.TrimSpace(i18n.Tf("init.summary.pm", pmName))),
	}
	if envCreated {
		summary = append(summary, s.Value.Render(strings.TrimSpace(i18n.Tf("init.summary.env", pkg.EnvFileName))))
	}

	fmt.Println()
	fmt.Println(s.SummaryCard(
		s.Success.Render(i18n.T("init.success")),
		summary...,
	))
	fmt.Println()
	fmt.Println(s.StepsList(i18n.T("init.next"),
		s.Info.Render("cd "+targetDir),
		s.Info.Render("liorian connect"),
		s.Info.Render("liorian create module"),
	))
	fmt.Println()
	return nil
}

func defaultProjectName() string {
	if cwd, err := os.Getwd(); err == nil {
		if base := filepath.Base(cwd); base != "" && base != "/" && base != "." {
			return base
		}
	}
	return "liorian-socle"
}

// destAction describes how to reuse an existing, non-empty destination.
type destAction int

const (
	destActionAbort destAction = iota
	destActionClear
	destActionMerge
)

// dirIsEmpty reports whether path exists and contains no entries.
func dirIsEmpty(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) == 0
}

// clearDirContents removes every entry inside dir while keeping dir itself
// (required when dir is the process working directory).
func clearDirContents(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// confirmExistingDir asks for approval before reusing a destination that
// already contains files. Interactive runs let the user Cancel, clear/delete
// the destination, or merge the template into it. Non-interactive runs fall
// back to LIORIAN_CLI_YES (approve the clear) or refuse.
func confirmExistingDir(dir string, isCWD bool) (destAction, error) {
	if !tui.IsInteractive() {
		if os.Getenv(tui.ConfirmYesEnv) != "" {
			return destActionClear, nil
		}
		return destActionAbort, pkg.NewErrorWithFix(i18n.T("cat.project"),
			i18n.Tf("init.error.exists", dir), i18n.T("init.error.exists.fix"), pkg.ExitError)
	}

	choices := []string{
		i18n.T("init.choice.cancel"),
		i18n.T("init.choice.merge"),
	}
	if isCWD {
		choices = append(choices, i18n.T("init.choice.clear"))
	} else {
		choices = append(choices, i18n.T("init.choice.delete"))
	}

	selected, err := tui.Select(i18n.Tf("init.prompt.existing", dir), choices)
	if err != nil {
		return destActionAbort, err
	}
	switch selected {
	case i18n.T("init.choice.clear"), i18n.T("init.choice.delete"):
		return destActionClear, nil
	case i18n.T("init.choice.merge"):
		return destActionMerge, nil
	default:
		return destActionAbort, nil
	}
}

// releaseDetail builds the muted metadata line rendered below the download
// progress bar: version, channel, branch and commit. Missing branch/commit
// fields are reported with a localized placeholder.
func releaseDetail(info pkg.ReleaseInfo) string {
	branch := info.Branch
	if branch == "" {
		branch = i18n.T("init.release.unknown")
	}
	commit := info.Commit
	if commit == "" {
		commit = i18n.T("init.release.unknown")
	}
	return i18n.Tf("init.release.meta", info.Version, info.Channel, branch, commit)
}

// fetchTemplate populates dest with the template source for the given
// channel. The repo argument accepts, in order of precedence:
//
//   - a local directory (tests / mirrors): its contents are copied directly;
//   - a GitHub repository URL: the latest release zip for the channel is
//     resolved through the GitHub API and extracted;
//   - any other value: treated as a direct ZIP download URL.
//
// resolved, when non-nil, is a release already resolved by the caller (avoids a
// second GitHub API round-trip). report forwards download progress (bytes
// done/total) to the caller; it must be safe to call from the network I/O
// goroutine.
func fetchTemplate(repo, channel, dest string, resolved *pkg.ReleaseInfo, report func(done, total int64)) error {
	if pkg.DirExists(repo) {
		return pkg.CopyDir(repo, dest)
	}
	if owner, name := githubOwnerRepo(repo); owner != "" && name != "" {
		if resolved != nil && resolved.Valid() {
			return pkg.DownloadReleaseZip(*resolved, dest, report)
		}
		return pkg.FetchReleaseZip(owner, name, channel, "", dest, report)
	}
	return pkg.FetchReleaseZip("", "", channel, repo, dest, report)
}

// githubOwnerRepo parses a GitHub repository reference such as
// `https://github.com/{owner}/{repo}`, `github.com/{owner}/{repo}` or the
// `{owner}/{repo}` shorthand (with or without a trailing `.git`) into its owner
// and repository name. Returns empty strings when not a GitHub reference.
func githubOwnerRepo(repo string) (owner, name string) {
	repo = strings.TrimSuffix(strings.TrimSpace(repo), "/")
	repo = strings.TrimSuffix(repo, ".git")
	parsed, err := url.Parse(repo)
	if err != nil {
		return "", ""
	}
	if parsed.Scheme != "" {
		if !strings.EqualFold(parsed.Host, "github.com") {
			return "", ""
		}
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if parsed.Scheme == "" && len(parts) == 3 && strings.EqualFold(parts[0], "github.com") {
		parts = parts[1:]
	}
	if len(parts) != 2 {
		return "", ""
	}
	owner, name = parts[0], parts[1]
	if owner == "" || name == "" {
		return "", ""
	}
	return owner, name
}

// mergeTemplateInto downloads the template into a temporary sibling directory
// then copies it over dest, keeping the files already present in dest
// (conflicts are overwritten by the template). report forwards download
// progress when the template is fetched over the network.
func mergeTemplateInto(repo, channel, dest string, report func(done, total int64)) error {
	absDest, err := filepath.Abs(dest)
	if err != nil {
		return fmt.Errorf("failed to resolve destination %s: %w", dest, err)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(absDest), ".lorian-init-*")
	if err != nil {
		return fmt.Errorf("failed to create a temporary directory: %w", err)
	}
	defer os.RemoveAll(tmp)
	if err := fetchTemplate(repo, channel, tmp, nil, report); err != nil {
		return err
	}
	return pkg.CopyDir(tmp, dest)
}

func runInstall(dir string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("invalid install command")
	}
	return pkg.StreamCommandIn(dir, args[0], args[1:]...)
}
