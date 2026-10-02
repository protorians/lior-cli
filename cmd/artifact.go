package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/protorians/lior-cli/internal/artifactbind"
	"github.com/protorians/lior-cli/internal/artifactdev"
	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/module"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/runner"
	"github.com/protorians/lior-cli/internal/socle"
	"github.com/protorians/lior-cli/internal/tui"
	"github.com/spf13/cobra"
)

// artifactCmd est la chaîne de développement d'un module Liora, native : le
// build (esbuild), le dev-server (HMR par SSE), le pack, le typecheck, le
// test et la liaison au socle vivent dans le binaire `liora` — la chaîne de
// build n'a plus aucune dépendance Node.
var artifactCmd = &cobra.Command{
	Use:   "artifact <action>",
	Short: i18n.T("cmd.artifact.short"),
	Long:  i18n.T("cmd.artifact.long"),
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var (
	artifactPort      int
	artifactHost      string
	artifactHTTPS     bool
	artifactHTTP      bool
	artifactHTTPSFlag bool
	artifactStrict    bool
	artifactSocle     string
	artifactOut       string
	artifactNoBuild   bool
	artifactDevAll    bool
)

func init() {
	artifactCmd.AddCommand(
		artifactBuildCmd,
		artifactDevCmd,
		artifactPackCmd,
		artifactTypecheckCmd,
		artifactTestCmd,
		artifactBindSocleCmd,
		artifactUnbindSocleCmd,
	)

	artifactDevCmd.Flags().IntVar(&artifactPort, "port", 0, i18n.T("artifact.flag.port"))
	artifactDevCmd.Flags().StringVar(&artifactHost, "host", "", i18n.T("artifact.flag.host"))
	artifactDevCmd.Flags().BoolVar(&artifactHTTPS, "https", false, i18n.T("artifact.flag.https"))
	artifactDevCmd.Flags().BoolVar(&artifactHTTP, "http", false, i18n.T("artifact.flag.http"))
	artifactDevCmd.Flags().BoolVar(&artifactStrict, "strict-port", false, i18n.T("artifact.flag.strict_port"))
	artifactDevCmd.Flags().StringVar(&artifactSocle, "socle", "", i18n.T("artifact.flag.socle"))
	artifactDevCmd.Flags().BoolVar(&artifactDevAll, "all", false, i18n.T("artifact.flag.all"))
	artifactPackCmd.Flags().StringVar(&artifactOut, "out", "", i18n.T("pack.flag.out"))
	artifactPackCmd.Flags().BoolVar(&artifactNoBuild, "no-build", false, i18n.T("artifact.flag.no_build"))

	for _, command := range []*cobra.Command{
		artifactBuildCmd, artifactDevCmd, artifactPackCmd,
		artifactTypecheckCmd, artifactTestCmd, artifactBindSocleCmd, artifactUnbindSocleCmd,
	} {
		i18nHelp(command, "cmd.artifact_"+artifactHelpKey(command)+"_short", "cmd.artifact_"+artifactHelpKey(command)+"_long")
	}
	for name, key := range map[string]string{
		"port": "artifact.flag.port", "host": "artifact.flag.host", "https": "artifact.flag.https",
		"http": "artifact.flag.http", "strict-port": "artifact.flag.strict_port", "socle": "artifact.flag.socle",
		"no-build": "artifact.flag.no_build", "all": "artifact.flag.all",
	} {
		i18nFlag(artifactDevCmd, name, key)
	}
	i18nFlag(artifactPackCmd, "out", "pack.flag.out")
	i18nFlag(artifactPackCmd, "no-build", "artifact.flag.no_build")
}

// artifactHelpKey dérive la clé i18n du nom de sous-commande.
func artifactHelpKey(command *cobra.Command) string {
	switch command {
	case artifactBindSocleCmd:
		return "bind_socle"
	case artifactUnbindSocleCmd:
		return "unbind_socle"
	default:
		return strings.ReplaceAll(command.Name(), "-", "_")
	}
}

var artifactBuildCmd = &cobra.Command{
	Use:  "build [module]",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := requireProjectRoot()
		if err != nil {
			return err
		}
		moduleDir, err := artifactModuleDir(root, args)
		if err != nil {
			return err
		}
		result, err := artifactdev.Build(artifactdev.BuildOptions{ModuleDir: moduleDir}, func(message string) {
			fmt.Println(message)
		})
		if err != nil {
			return pkg.NewError(i18n.T("cat.pack"), err.Error(), pkg.ExitBuild)
		}
		s := tui.NewStyles()
		fmt.Println(s.SummaryCard(
			s.Success.Render(i18n.T("artifact.build.done")),
			s.KeyValue(i18n.T("label.bundle"), s.Info.Render(result.BundlePath)+
				fmt.Sprintf(" (%.0f Ko)", float64(result.BundleBytes)/1024)),
			s.KeyValue(i18n.T("label.document"), s.Info.Render(result.DocumentPath)),
		))
		return nil
	},
}

var artifactDevCmd = &cobra.Command{
	Use:  "dev [modules…]",
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := requireProjectRoot()
		if err != nil {
			return err
		}
		dirs, multi, err := artifactDevModuleDirs(root, args, artifactDevAll)
		if err != nil {
			return err
		}
		if !multi && len(dirs) == 0 {
			moduleDir, err := artifactModuleDir(root, args)
			if err != nil {
				return err
			}
			dirs = []string{moduleDir}
		}

		dev := artifactdev.DevOptions{Port: artifactPort, Host: artifactHost, StrictPort: artifactStrict}
		switch {
		case artifactHTTPS:
			dev.HTTPS = &artifactHTTPSFlagTrue
		case artifactHTTP:
			dev.HTTPS = &artifactHTTPSFlagFalse
		}

		// Orchestration un-terminal : `liora artifact dev --socle ../socle`
		// démarre aussi le socle (next dev + serveur de bibliothèque) et le
		// stoppe avec le dev-server. La boucle de développement tient alors
		// dans un seul terminal, HMR compris.
		var socleProcess *exec.Cmd
		if strings.TrimSpace(artifactSocle) != "" {
			socleProcess, err = startSocleDev(artifactSocle)
			if err != nil {
				return err
			}
			defer func() {
				runner.InterruptGroup(socleProcess)
				stopProcessGroup(socleProcess)
			}()
		}

		s := tui.NewStyles()
		log := func(message string) { fmt.Println(message) }

		// Plusieurs modules : un seul serveur, un seul port, chaque module
		// servi sous son slug (`/<slug>/`) — le socle résout le module par le
		// chemin, comme pour `/m/<slug>` côté iframe.
		if multi {
			server, err := artifactdev.StartMulti(artifactdev.MultiOptions{ModuleDirs: dirs, Dev: dev}, log)
			if err != nil {
				return pkg.NewError(i18n.T("cat.toolchain"), err.Error(), pkg.ExitError)
			}
			for _, hosted := range server.Modules {
				fmt.Println(s.Info.Render(i18n.T("artifact.dev.hint") + " " + server.URL + "/" + hosted.Slug + "/"))
			}
			fmt.Println()
			waitForInterrupt()
			server.Stop()
			return nil
		}

		server, err := artifactdev.Start(artifactdev.BuildOptions{ModuleDir: dirs[0], Dev: dev}, log)
		if err != nil {
			return pkg.NewError(i18n.T("cat.toolchain"), err.Error(), pkg.ExitError)
		}

		fmt.Println(s.Info.Render(i18n.T("artifact.dev.hint") + " " + server.URL + "/m"))
		fmt.Println()

		waitForInterrupt()
		server.Stop()
		return nil
	},
}

// waitForInterrupt bloque jusqu'au signal d'arrêt (Ctrl-C, SIGTERM).
func waitForInterrupt() {
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM)
	<-interrupts
}

// artifactDevModuleDirs résout les modules que `artifact dev` héberge :
// --all balaye l'arbre source du workspace (`modules/<id>`), sinon chaque
// argument nomme un module. Multi dès que plusieurs modules sont désignés ;
// un seul module nommé garde le serveur mono-module classique.
func artifactDevModuleDirs(root string, args []string, all bool) ([]string, bool, error) {
	if all {
		dirs := workspaceModuleDirs(root)
		if len(dirs) == 0 {
			return nil, false, pkg.NewError(i18n.T("cat.module"),
				i18n.Tf("artifact.error.workspace_empty", filepath.Join(root, config.WorkspaceModulesDir)),
				pkg.ExitModuleNotFound)
		}
		return dirs, true, nil
	}
	var named []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if dir := moduleDirFromArg([]string{arg}); dir != "" {
			named = append(named, dir)
			continue
		}
		if dir := absoluteModuleDir(root, arg); dir != "" {
			named = append(named, dir)
			continue
		}
		return nil, false, pkg.NewError(i18n.T("cat.module"),
			i18n.Tf("artifact.error.module_absent", arg), pkg.ExitModuleNotFound)
	}
	switch len(named) {
	case 0:
		return nil, false, nil
	case 1:
		return named, false, nil
	default:
		return named, true, nil
	}
}

// workspaceModuleDirs liste les modules de l'arbre source du workspace
// (`modules/<id>` portant un manifest.json).
func workspaceModuleDirs(root string) []string {
	entries, err := os.ReadDir(filepath.Join(root, config.WorkspaceModulesDir))
	if err != nil {
		return nil
	}
	dirs := []string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, config.WorkspaceModulesDir, entry.Name())
		if pkg.FileExists(filepath.Join(dir, config.ManifestFileName)) {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

var (
	artifactHTTPSFlagTrue  = true
	artifactHTTPSFlagFalse = false
)

var artifactPackCmd = &cobra.Command{
	Use:  "pack [module]",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := requireProjectRoot()
		if err != nil {
			return err
		}
		moduleDir, err := artifactModuleDir(root, args)
		if err != nil {
			return err
		}

		if !artifactNoBuild {
			if _, err := artifactdev.Build(artifactdev.BuildOptions{ModuleDir: moduleDir}, func(message string) {
				fmt.Println(message)
			}); err != nil {
				return pkg.NewError(i18n.T("cat.pack"), err.Error(), pkg.ExitBuild)
			}
		}

		packer := &module.Packer{Root: root, Out: strings.TrimSpace(artifactOut)}
		result, err := packer.PackPath(moduleDir)
		if err != nil {
			return pkg.NewError(i18n.T("cat.pack"), err.Error(), pkg.ExitBuild)
		}

		s := tui.NewStyles()
		fmt.Println(s.SummaryCard(
			s.Success.Render(i18n.T("pack.success")),
			s.KeyValue(i18n.T("label.module"), result.Module+" v"+result.Version),
			s.KeyValue(i18n.T("label.file"), s.Info.Render(result.Path)),
			s.KeyValue(i18n.T("label.size"), humanSize(result.Size)),
			s.KeyValue(i18n.T("label.checksum"), s.Value.Render(shortDigest(result.Checksum))),
		))
		fmt.Println()
		fmt.Println(s.Info.Render(i18n.T("artifact.pack.sign_hint")))
		return nil
	},
}

var artifactTypecheckCmd = &cobra.Command{
	Use:  "typecheck [module]",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := requireProjectRoot()
		if err != nil {
			return err
		}
		moduleDir, err := artifactModuleDir(root, args)
		if err != nil {
			return err
		}
		if err := module.RunTypecheck(moduleDir); err != nil {
			return pkg.NewError(i18n.T("cat.pack"), err.Error(), pkg.ExitBuild)
		}
		s := tui.NewStyles()
		fmt.Println(s.Success.Render(i18n.T("artifact.typecheck.done")))
		return nil
	},
}

var artifactTestCmd = &cobra.Command{
	Use:  "test [module]",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := requireProjectRoot()
		if err != nil {
			return err
		}
		moduleDir, err := artifactModuleDir(root, args)
		if err != nil {
			return err
		}
		np := pkg.LoadNodePackage(filepath.Join(moduleDir, "package.json"))
		if !np.HasScript("test") {
			return pkg.NewError(i18n.T("cat.test"), i18n.T("artifact.test.no_script"), pkg.ExitError)
		}
		pm := pkg.DetectPackageManager()
		if pm == "" {
			return pkg.NewError(i18n.T("cat.test"), i18n.T("artifact.error.pm_missing"), pkg.ExitError)
		}
		testCmd := exec.Command(pm, "run", "test")
		testCmd.Dir = moduleDir
		testCmd.Stdout = os.Stdout
		testCmd.Stderr = os.Stderr
		testCmd.Stdin = os.Stdin
		if err := testCmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				os.Exit(exitErr.ExitCode())
			}
			return pkg.NewError(i18n.T("cat.test"), err.Error(), pkg.ExitError)
		}
		return nil
	},
}

var artifactBindSocleCmd = &cobra.Command{
	Use:  "bind:socle <socle> [module]",
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runArtifactSocleBind(args, true)
	},
}

var artifactUnbindSocleCmd = &cobra.Command{
	Use:  "unbind:socle <socle> [module]",
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runArtifactSocleBind(args, false)
	},
}

// runArtifactSocleBind résout le socle (premier argument, absolutisé depuis
// le répertoire d'invocation) et le module, puis pose ou retire la liaison.
func runArtifactSocleBind(args []string, bind bool) error {
	root, err := requireProjectRoot()
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return pkg.NewError(i18n.T("cat.project"), i18n.T("modules.error.cwd"), pkg.ExitError)
	}

	socleDir := args[0]
	if !filepath.IsAbs(socleDir) {
		socleDir = filepath.Join(cwd, socleDir)
	}
	socleDir = filepath.Clean(socleDir)

	var moduleArg string
	if next := firstNonFlagIndex(args[1:]) + 1; next > 0 {
		moduleArg = args[next]
	}
	moduleDir, err := socleBindModuleDir(root, cwd, moduleArg)
	if err != nil {
		return err
	}

	options := artifactbind.Options{SocleDir: socleDir, ModuleDir: moduleDir, Log: func(message string) {
		fmt.Println(message)
	}}
	s := tui.NewStyles()
	if bind {
		result, err := artifactbind.Bind(options)
		if err != nil {
			return pkg.NewError(i18n.T("cat.link"), err.Error(), pkg.ExitError)
		}
		fmt.Println(s.SummaryCard(
			s.Success.Render(i18n.T("artifact.bind.done")),
			s.KeyValue(i18n.T("label.module"), result.Identifier+" v"+result.Version),
			s.KeyValue(i18n.T("label.mode"), string(result.Mode)),
			s.KeyValue(i18n.T("label.dev_url"), s.Info.Render(result.DevURL)),
			s.KeyValue(i18n.T("label.env"), s.Info.Render(result.EnvPath)),
		))
		fmt.Println()
		fmt.Println(s.StepsList(i18n.T("artifact.bind.next"),
			s.Info.Render("liora artifact dev"),
			s.Info.Render("liora doctor --socle "+displayPath(cwd, result.SocleDir)),
		))
		return nil
	}
	result, err := artifactbind.Unbind(options)
	if err != nil {
		return pkg.NewError(i18n.T("cat.link"), err.Error(), pkg.ExitError)
	}
	fmt.Println(s.SummaryCard(
		s.Success.Render(i18n.T("artifact.unbind.done")),
		s.KeyValue(i18n.T("label.module"), result.Identifier),
		s.KeyValue(i18n.T("label.removed"), map[bool]string{true: result.RemovedDir, false: "—"}[result.RemovedDir != ""]),
	))
	return nil
}

// socleBindModuleDir résout le module qu'une action de liaison vise : celui
// nommé par l'argument, sinon celui du répertoire courant, sinon celui
// sélectionné à la racine.
func socleBindModuleDir(root, cwd, moduleArg string) (string, error) {
	if moduleArg != "" {
		if dir := moduleDirFromArg([]string{moduleArg}); dir != "" {
			return dir, nil
		}
		if dir := absoluteModuleDir(root, moduleArg); dir != "" {
			return dir, nil
		}
		name, err := resolveModule(root, []string{moduleArg})
		if err != nil {
			return "", err
		}
		if dir := absoluteModuleDir(root, name); dir != "" {
			return dir, nil
		}
		return "", pkg.NewError(i18n.T("cat.module"),
			i18n.Tf("artifact.error.module_absent", moduleArg), pkg.ExitModuleNotFound)
	}

	if dir := moduleDirFromCwd(root, cwd); dir != "" {
		return dir, nil
	}
	if pkg.FileExists(filepath.Join(cwd, config.ManifestFileName)) {
		return cwd, nil
	}
	name, err := resolveModule(root, nil)
	if err != nil {
		return "", err
	}
	if dir := absoluteModuleDir(root, name); dir != "" {
		return dir, nil
	}
	return "", pkg.NewError(i18n.T("cat.module"),
		i18n.Tf("artifact.error.module_absent", name), pkg.ExitModuleNotFound)
}

// absoluteModuleDir résout une référence de module en dossier absolu.
func absoluteModuleDir(root, reference string) string {
	dir := config.ResolveModuleDir(root, reference)
	if dir == "" {
		return ""
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// startSocleDev démarre le script `dev` du socle dans son propre groupe de
// processus : `bun run dev` lance next dev ET le serveur de bibliothèque —
// les deux terminaux socle de la boucle tiennent dans celui-ci.
func startSocleDev(socleDir string) (*exec.Cmd, error) {
	socleDir, err := filepath.Abs(socleDir)
	if err != nil {
		return nil, err
	}
	if !socle.IsSocle(socleDir) {
		return nil, pkg.NewError(i18n.T("cat.toolchain"),
			i18n.Tf("artifact.error.socle_invalid", socleDir), pkg.ExitError)
	}
	pm := pkg.DetectPackageManager()
	if pm == "" {
		return nil, pkg.NewError(i18n.T("cat.toolchain"), i18n.T("artifact.error.pm_missing"), pkg.ExitError)
	}
	socleProcess := exec.Command(pm, "run", "dev")
	socleProcess.Dir = socleDir
	socleProcess.Stdout = os.Stdout
	socleProcess.Stderr = os.Stderr
	socleProcess.Stdin = os.Stdin
	if err := runner.Spawn(socleProcess); err != nil {
		return nil, pkg.NewError(i18n.T("cat.toolchain"),
			i18n.Tf("artifact.error.socle_start", err.Error()), pkg.ExitError)
	}
	s := tui.NewStyles()
	fmt.Println(s.Info.Render(i18n.Tf("artifact.socle.started", socleDir, pm)))
	return socleProcess, nil
}

// stopProcessGroup attend la fin du processus du socle après l'interruption,
// en forçant l'arrêt du groupe au-delà d'un délai de grâce.
func stopProcessGroup(socleProcess *exec.Cmd) {
	if socleProcess == nil || socleProcess.Process == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		_ = socleProcess.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		runner.KillGroup(socleProcess)
	}
}

// firstNonFlagIndex renvoie l'index du premier argument qui n'est pas une
// option, -1 quand il n'y en a pas.
func firstNonFlagIndex(args []string) int {
	for i, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			return i
		}
	}
	return -1
}
