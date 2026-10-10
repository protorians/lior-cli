// Serveur de développement central d'un socle — `liora socle dev` (et
// `bun run dev` du socle, qui délègue à cette commande).
//
// Un seul serveur héberge tous les modules inscrits dans
// `<socle>/.liorian/module.dev.json` : plus de dev-server par module, un seul
// terminal pour le socle, sa bibliothèque et ses modules. Le registre est
// surveillé (polling ~2 s) : lier ou retirer un module pendant la session
// redémarre le serveur central, sans relancer la commande.
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/protorians/lior-cli/internal/artifactdev"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/runner"
	"github.com/protorians/lior-cli/internal/socle"
	"github.com/protorians/lior-cli/internal/tui"
)

// centralDevPoll est l'intervalle de surveillance du registre des modules en
// dev — un polling simple, sans fsnotify : le fichier n'est écrit que par
// `bind:socle` / `unbind:socle`, sa fréquence ne justifie pas un watcher.
const centralDevPoll = 2 * time.Second

// centralDevOptions décrit le serveur de développement central d'un socle.
type centralDevOptions struct {
	// SocleDir est la racine du socle hébergeant (absolue).
	SocleDir string
	// Dev porte les réglages du serveur (port, hôte, TLS, strict).
	Dev artifactdev.DevOptions
	// StartApp démarre aussi l'application du socle (`<pm> run dev:app`).
	StartApp bool
	// ExtraDirs ajoute au registre les modules déjà résolus par la commande
	// appelante (`liora artifact dev <modules…>`) : un module lié à un
	// registre périmé reste servi au lieu de disparaître du lot.
	ExtraDirs []string
}

// centralLog journalise les lignes du serveur central : elles sont
// collectées pendant le démarrage (le panneau se lit après), puis relayées en
// direct une fois l'interface affichée.
type centralLog struct {
	mu      sync.Mutex
	pending []string
	live    bool
}

func (l *centralLog) printf(message string) {
	line := styleArtifactLine(message)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.live {
		fmt.Println(line)
		return
	}
	l.pending = append(l.pending, line)
}

// take rend les lignes collectées puis vide le tampon.
func (l *centralLog) take() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	lines := l.pending
	l.pending = nil
	return lines
}

// goLive bascule le journal en affichage direct (appelé après le panneau).
func (l *centralLog) goLive() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.live = true
}

// styleArtifactLine met en forme une ligne du dev-server : le préfixe
// `artifact: ` est retiré (la commande porte déjà le contexte), les
// avertissements passent en ambre, le reste en sourdine.
func styleArtifactLine(message string) string {
	s := tui.NewStyles()
	message = strings.TrimPrefix(strings.TrimSpace(message), "artifact: ")
	if strings.HasPrefix(message, "⚠") {
		return s.Warning.Render(message)
	}
	return s.Muted.Render(message)
}

// existingModuleDirs garde les entrées du registre dont le dossier existe
// encore : un module déplacé ou supprimé sans `unbind:socle` ne doit pas
// empêcher les autres d'être servis.
func existingModuleDirs(entries []socle.ModuleDevEntry) (dirs, missing []string) {
	for _, entry := range entries {
		if pkg.DirExists(entry.Dir) {
			dirs = append(dirs, entry.Dir)
			continue
		}
		name := strings.TrimSpace(entry.Identifier)
		if name == "" {
			name = entry.Dir
		}
		missing = append(missing, name)
	}
	return dirs, missing
}

// runCentralDev démarre (et maintient) le serveur de développement central du
// socle : lecture du registre, serveur multi-modules, application du socle,
// surveillance du registre — puis arrêt propre sur Ctrl-C ou sortie de
// l'application.
func runCentralDev(options centralDevOptions) error {
	s := tui.NewStyles()

	socleDir, err := filepath.Abs(options.SocleDir)
	if err != nil {
		return pkg.NewError(i18n.T("cat.toolchain"), err.Error(), pkg.ExitError)
	}
	if !socle.IsSocle(socleDir) {
		return pkg.NewErrorWithFix(i18n.T("cat.toolchain"),
			i18n.Tf("socle.dev.not_socle", socleDir),
			i18n.T("socle.dev.not_socle.fix"), pkg.ExitError)
	}

	registryPath := socle.ModuleDevPath(socleDir)
	entries, registryErr := socle.ReadModuleDev(socleDir)
	if registryErr != nil {
		// Un registre illisible n'arrête pas la session : le socle reste
		// servi, seul héberger des modules neuf devient impossible.
		warn(i18n.Tf("socle.dev.registry_unreadable", registryErr.Error()))
	}
	dirs, missing := existingModuleDirs(entries)
	if len(missing) > 0 {
		warn(i18n.Tf("socle.dev.registry_missing", strings.Join(missing, ", ")))
	}
	// Les modules déjà résolus par l'appelant rejoignent le lot : un module
	// lié à un registre périmé reste servi au lieu de disparaître.
	dirs = mergeModuleDirs(dirs, options.ExtraDirs)

	appScript := ""
	if options.StartApp {
		appScript = socle.AppScriptName(socle.PackageScripts(socleDir))
	}
	if len(dirs) == 0 && appScript == "" {
		return pkg.NewErrorWithFix(i18n.T("cat.toolchain"),
			i18n.Tf("socle.dev.empty", registryPath),
			i18n.T("socle.dev.empty.fix"), pkg.ExitError)
	}
	pm := pkg.DetectPackageManager()
	if appScript != "" && pm == "" {
		warn(i18n.T("artifact.error.pm_missing"))
		appScript = ""
	}

	// Démarrage du serveur central : les lignes du serveur sont collectées
	// sous le spinner, puis rendues dans un bloc de journal.
	logger := &centralLog{}
	var server *artifactdev.MultiDevServer
	if len(dirs) > 0 {
		server, err = tui.RunWithSpinner(i18n.Tf("socle.dev.spinner", len(dirs)), func() (*artifactdev.MultiDevServer, error) {
			return artifactdev.StartMulti(artifactdev.MultiOptions{
				ModuleDirs: dirs,
				Dev:        options.Dev,
				SocleDir:   socleDir,
			}, logger.printf)
		})
		if err != nil {
			if _, ok := err.(*pkg.Error); ok {
				return err
			}
			return pkg.NewError(i18n.T("cat.toolchain"), err.Error(), pkg.ExitError)
		}
	}

	fmt.Println()
	fmt.Println(s.NeutralPanel(strings.Join(centralPanelRows(s, socleDir, registryPath, server, entries, appScript, pm), "\n")))
	if startup := logger.take(); len(startup) > 0 {
		fmt.Println()
		fmt.Println(s.LogsBlock(i18n.T("socle.dev.logs"), startup))
	}
	fmt.Println()

	// Application du socle : `dev:app` porte le lancement historique (next dev
	// + serveur de bibliothèque), jamais `dev` — ce script délègue à cette
	// commande et la relancerait en boucle.
	var app *exec.Cmd
	appExited := make(chan error, 1)
	if appScript != "" {
		app = exec.Command(pm, "run", appScript)
		app.Dir = socleDir
		app.Stdout = os.Stdout
		app.Stderr = os.Stderr
		app.Stdin = os.Stdin
		if err := runner.Spawn(app); err != nil {
			if server != nil {
				server.Stop()
			}
			return pkg.NewError(i18n.T("cat.toolchain"),
				i18n.Tf("artifact.error.socle_start", err.Error()), pkg.ExitError)
		}
		go func() { appExited <- app.Wait() }()
		fmt.Println(s.Info.Render(i18n.Tf("socle.dev.app_started", pm, appScript)))
	}
	logger.goLive()

	// Une seule boucle d'attente : signal d'arrêt, sortie de l'application,
	// ou changement du registre (redémarrage du serveur central).
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interrupts)

	watch := time.NewTicker(centralDevPoll)
	defer watch.Stop()
	lastRegistry := readRegistryBytes(registryPath)

	shutdown := func() {
		stopCentralApp(app, appExited)
		app = nil
		if server != nil {
			server.Stop()
			server = nil
		}
	}
	defer shutdown()

	for {
		select {
		case <-interrupts:
			fmt.Println(s.Info.Render(i18n.T("socle.dev.stopped")))
			return nil
		case waitErr := <-appExited:
			app = nil
			if server != nil {
				server.Stop()
				server = nil
			}
			if waitErr == nil {
				return nil
			}
			if exitErr, ok := waitErr.(*exec.ExitError); ok && exitErr.ExitCode() == 0 {
				return nil
			}
			return pkg.NewError(i18n.T("cat.toolchain"),
				i18n.Tf("socle.dev.app_exited", waitErr.Error()), pkg.ExitError)
		case <-watch.C:
			current := readRegistryBytes(registryPath)
			if current == lastRegistry {
				continue
			}
			lastRegistry = current
			nextEntries, nextErr := socle.ReadModuleDev(socleDir)
			if nextErr != nil {
				warn(i18n.Tf("socle.dev.registry_unreadable", nextErr.Error()))
				continue
			}
			nextDirs, nextMissing := existingModuleDirs(nextEntries)
			if len(nextMissing) > 0 {
				warn(i18n.Tf("socle.dev.registry_missing", strings.Join(nextMissing, ", ")))
			}
			nextDirs = mergeModuleDirs(nextDirs, options.ExtraDirs)
			if server != nil {
				server.Stop()
				server = nil
			}
			if len(nextDirs) == 0 {
				fmt.Println(s.Warning.Render("⚠ " + i18n.T("socle.dev.no_modules")))
				continue
			}
			restarted, restartErr := artifactdev.StartMulti(artifactdev.MultiOptions{
				ModuleDirs: nextDirs,
				Dev:        options.Dev,
				SocleDir:   socleDir,
			}, logger.printf)
			if restartErr != nil {
				if _, ok := restartErr.(*pkg.Error); ok {
					return restartErr
				}
				return pkg.NewError(i18n.T("cat.toolchain"), restartErr.Error(), pkg.ExitError)
			}
			server = restarted
			fmt.Println(s.Info.Render(i18n.Tf("socle.dev.restarted", len(nextDirs), restarted.URL)))
		}
	}
}

// centralPanelRows décrit la session qui vient de démarrer : socle hébergeant,
// serveur central et ses modules, application du socle, registre.
func centralPanelRows(s *tui.Styles, socleDir, registryPath string, server *artifactdev.MultiDevServer,
	entries []socle.ModuleDevEntry, appScript, pm string) []string {
	rows := []string{
		s.KeyValue(i18n.T("label.socle"), s.Info.Render(socleDir)),
	}
	switch {
	case server != nil:
		rows = append(rows, s.KeyValue(i18n.T("socle.dev.row.server"), s.Info.Render(server.URL)))
		for _, hosted := range server.Modules {
			rows = append(rows, s.KeyValue(hosted.Identifier, s.Info.Render(server.URL+"/"+hosted.Slug+"/")))
			rows = append(rows, s.KeyValue("  "+i18n.T("label.path"), s.Info.Render("<module>")))
		}
	case len(entries) == 0:
		rows = append(rows, s.KeyValue(i18n.T("label.modules"), s.Muted.Render(i18n.T("socle.dev.modules_none"))))
	}
	if appScript != "" {
		rows = append(rows, s.KeyValue(i18n.T("socle.dev.row.app"), s.Value.Render(pm+" run "+appScript)))
	}
	rows = append(rows, s.KeyValue(i18n.T("label.registry"), s.Info.Render("<socle>")))
	return rows
}

// stopCentralApp interrompt le groupe de l'application du socle et attend sa
// fin — le goroutine de surveillance est le seul à en charger la `Wait`, avec
// un délai de grâce avant le kill forcé.
func stopCentralApp(app *exec.Cmd, exited <-chan error) {
	if app == nil {
		return
	}
	runner.InterruptGroup(app)
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		runner.KillGroup(app)
		<-exited
	}
}

// readRegistryBytes lit le registre brut : son contenu sert d'empreinte au
// changement (le fichier n'est écrit que par bind/unbind, un contenu identique
// ne dit rien de neuf). Un fichier absent est une empreinte comme une autre.
func readRegistryBytes(registryPath string) string {
	data, err := os.ReadFile(registryPath)
	if err != nil {
		return ""
	}
	return string(data)
}

// centralSocleDir résout le socle hébergeant les modules ciblés par
// `liora artifact dev`. `--socle` tranche (le lot ciblé complète le registre
// via `ExtraDirs`) ; sinon la liaison du premier module doit désigner un
// socle dont le registre héberge déjà au moins un module. Renvoie "" quand il
// n'y a rien à déléguer — `artifact dev` garde alors son serveur autonome.
func centralSocleDir(moduleDirs []string, flag string) string {
	if trimmed := strings.TrimSpace(flag); trimmed != "" {
		abs, err := filepath.Abs(trimmed)
		if err != nil {
			return ""
		}
		// Un dossier non socle n'est pas délégué : le repli signalera
		// `artifact.error.socle_invalid` en lançant l'application.
		if socle.IsSocle(abs) {
			return abs
		}
		return ""
	}
	if len(moduleDirs) == 0 {
		return ""
	}
	link, ok := socle.ReadDevLink(moduleDirs[0])
	if !ok || link.SocleDir == "" || !socle.IsSocle(link.SocleDir) {
		return ""
	}
	entries, err := socle.ReadModuleDev(link.SocleDir)
	if err != nil {
		return ""
	}
	dirs, _ := existingModuleDirs(entries)
	if len(dirs) == 0 {
		return ""
	}
	return link.SocleDir
}

// mergeModuleDirs ajoute des dossiers à un lot : absolutisés, dédupliqués,
// inexistants écartés.
func mergeModuleDirs(dirs, extra []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(dirs)+len(extra))
	for _, dir := range append(append([]string{}, dirs...), extra...) {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
		clean := filepath.Clean(dir)
		if seen[clean] || !pkg.DirExists(clean) {
			continue
		}
		seen[clean] = true
		out = append(out, clean)
	}
	return out
}
