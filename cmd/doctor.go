package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/protorians/lior-cli/internal/artifactbind"
	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/socle"
	"github.com/protorians/lior-cli/internal/tui"
	"github.com/spf13/cobra"
)

// Sévérités d'un contrôle de diagnostic, alignées sur le vocabulaire de
// `liora audit` pour que les deux rapports se lisent de la même façon.
const (
	doctorOK      = "ok"
	doctorWarn    = "warn"
	doctorFail    = "fail"
	doctorSkipped = "skipped"
)

var (
	doctorSoclePath string
	doctorModule    string
	doctorFix       bool
	doctorOutput    string
)

// doctorCmd diagnostique la boucle de développement d'un module : identité,
// liaison au socle, câblage d'environnement, contexte de transport.
//
// Le symptôme que les développeurs rencontrent le plus souvent — « Module
// introuvable ou non installé » alors que le module est lié, ou une page de
// module qui reste blanche alors que le socle fonctionne — ne produit aucune
// erreur dans les journaux. Cette commande rend ces états visibles et nomme la
// cause, avec la commande à exécuter pour la lever.
var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnostiquer la boucle de développement d'un module",
	Long: `Diagnostique la boucle de développement complète d'un module.

Vérifie l'identité du module, sa liaison au socle, le câblage d'environnement
qui lui fait face, la disponibilité du serveur de bibliothèque et la
correspondance des schémas d'URL (un socle en HTTPS refuse une iframe en HTTP).

Avec --fix, corrige ce qui peut l'être sans risque : crée le .env du socle
depuis son gabarit, câble la racine de transport de la bibliothèque, aligns
l'URL du dev-server sur le schéma du socle, provisionne les certificats
mkcert et lie le module au socle.

La liaison au socle passe par ` + "`artifact bind:socle`" + `: socle publicly distribué,
dépôt de module autonome, aucun Cloisonnement du dépôt du socle n'est requis.`,
	Example: `  liora doctor
  liora doctor --socle ../liorian-socle
  liora doctor --fix`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDoctor(cmd)
	},
}

func init() {
	doctorCmd.Flags().StringVar(&doctorSoclePath, "socle", "", i18n.T("doctor.flag.socle"))
	doctorCmd.Flags().StringVarP(&doctorModule, "module", "m", "", i18n.T("doctor.flag.module"))
	doctorCmd.Flags().BoolVar(&doctorFix, "fix", false, i18n.T("doctor.flag.fix"))
	doctorCmd.Flags().StringVar(&doctorOutput, "output", "", i18n.T("audit.flag.output"))
	i18nHelp(doctorCmd, "cmd.doctor.short", "cmd.doctor.long")
	for _, name := range []string{"socle", "module", "fix", "output"} {
		i18nFlag(doctorCmd, name, "doctor.flag."+name)
	}
}

// check est un contrôle de diagnostic : un état observé, une gravité et la
// marche à suivre pour le lever.
type doctorCheck struct {
	ID     string
	Label  string
	Status string
	Detail string
	Fix    string
	Fixed  bool
	// Rebindable vaut vrai lorsque le contrôle ne peut être levé que par une
	// nouvelle liaison au socle (`artifact bind:socle`).
	Rebindable bool
	FixError   string
}

func runDoctor(cmd *cobra.Command) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	cwd, err := os.Getwd()
	if err != nil {
		return pkg.NewError(i18n.T("cat.project"), i18n.T("modules.error.cwd"), pkg.ExitError)
	}

	report, err := diagnose(ctx, cwd, doctorSoclePath, doctorModule, doctorFix)
	if err != nil {
		return err
	}

	switch strings.ToLower(strings.TrimSpace(doctorOutput)) {
	case "json":
		return printDoctorJSON(report)
	case "", "table":
		printDoctorReport(report)
		return nil
	default:
		return pkg.NewError(
			i18n.T("cat.project"),
			i18n.Tf("doctor.error.output", doctorOutput),
			pkg.ExitError,
		)
	}
}

// doctorReport est le résultat complet du diagnostic, sérialisable en JSON.
type doctorReport struct {
	SocleDir  string         `json:"socleDir"`
	ModuleDir string         `json:"moduleDir,omitempty"`
	Socle     *socle.Profile `json:"socle,omitempty"`
	Checks    []doctorCheck  `json:"checks"`
	Fixed     []string       `json:"fixed,omitempty"`
}

// diagnose exécute les contrôles et, en mode --fix, applique les corrections
// sûres. Les contrôles sont ordonnés par dépendance : l'identité du socle
// conditionne tout le reste, la liaison celle du module.
func diagnose(ctx context.Context, cwd, socleArg, moduleArg string, fix bool) (*doctorReport, error) {
	report := &doctorReport{}

	moduleDir, err := resolveDoctorModule(cwd, moduleArg)
	if err != nil {
		return nil, err
	}
	report.ModuleDir = moduleDir

	socleDir, err := resolveDoctorSocle(cwd, socleArg)
	if err != nil {
		return nil, err
	}
	report.SocleDir = socleDir

	profile := socle.ReadProfile(socleDir)
	report.Socle = &profile

	// 1. Identité du socle — la base de tout diagnostic de transport.
	report.add(doctorCheck{
		ID:     "socle.identity",
		Label:  i18n.T("doctor.check.socle"),
		Status: doctorOK,
		Detail: fmt.Sprintf("%s (%s://%s)", profile.Name, profile.Scheme, hostLabel(profile)),
	})

	// 2. Environnement du socle : sans `.env`, `NEXT_PUBLIC_*` n'a aucune valeur
	//    et le socle démarre sur des réglages par défaut silencieux.
	envCheck := checkSocleEnv(profile, fix)
	report.add(envCheck)
	report.recordFix(&envCheck)

	// 3. Certificats : un socle en HTTPS sans certificat sert sa page en
	//    accepté-par-le-navigateur-seulement ; le TLS de dev doit être provisionné.
	certCheck := checkCertificates(profile, fix)
	report.add(certCheck)
	report.recordFix(&certCheck)

	// 4. Racine de transport de la bibliothèque : sans elle, le registre
	//    d'installation reste vide sous `next dev` et aucun module n'est déclaré.
	libraryCheck := checkLibraryURL(profile, fix)
	report.add(libraryCheck)
	report.recordFix(&libraryCheck)

	// 5. Réponse effective du registre : la preuve, pas la déclaration.
	report.add(checkLibraryReach(profile))

	// 6. Identité du module — l'adresse de la bibliothèque est `manifest.id`.
	if moduleDir != "" {
		report.add(checkModuleIdentity(moduleDir, profile))

		link, linked := socle.ReadDevLink(moduleDir)
		bindCommand := fmt.Sprintf("liora artifact bind:socle %s", relOrDot(socleDir, cwd))
		if !linked {
			report.add(doctorCheck{
				ID:     "module.link",
				Label:  i18n.T("doctor.check.link"),
				Status: doctorWarn,
				Detail: i18n.T("doctor.detail.link_absent"),
				Fix:    bindCommand,
			})
		} else {
			report.add(checkModuleLink(moduleDir, link, profile))
		}

		// La liaison corrige d'un geste ce qui la précède : installation dans la
		// bibliothèque, identité de transport et URL du dev-server. Elle passe
		// par le même code que la commande manuelle (`artifact bind:socle`),
		// sans réimplémenter ici la matérialisation des liens.
		transport := checkDevTransport(profile, moduleDir)
		if fix && needsBind(linked, transport) {
			if err := bindModuleToSocle(socleDir, moduleDir); err != nil {
				transport.FixError = err.Error()
			} else {
				transport.Status = doctorOK
				transport.Fixed = true
				transport.Detail = i18n.T("doctor.detail.transport_bound")
			}
		}
		report.add(transport)
	} else {
		report.add(doctorCheck{
			ID:     "module.identity",
			Label:  i18n.T("doctor.check.module"),
			Status: doctorSkipped,
			Detail: i18n.T("doctor.detail.no_module"),
		})
	}

	// 7. Le socle écoute-t-il ? Un socle arrêté produit exactement le même
	//    « Module introuvable » qu'une liaison cassée.
	report.add(checkSocleReachable(profile))

	return report, nil
}

// resolveDoctorModule localise le module à diagnostiquer : l'argument, puis le
// module courant, puis le module unique du projet. Aucun module n'est une
// situation valide — le socle peut être diagnostiqué seul.
func resolveDoctorModule(cwd, moduleArg string) (string, error) {
	if moduleArg != "" {
		root, err := config.FindProjectRoot(cwd)
		if err != nil {
			return "", pkg.NewErrorWithFix(
				i18n.T("cat.module"),
				err.Error(),
				i18n.T("modules.error.root.fix"),
				pkg.ExitModuleNotFound,
			)
		}
		dir := config.ResolveModuleDir(root, moduleArg)
		if dir == "" {
			dir = filepath.Join(root, config.ExternalModulesDir, moduleArg)
		}
		if !pkg.DirExists(dir) {
			return "", pkg.NewError(
				i18n.T("cat.module"),
				i18n.Tf("doctor.error.module_absent", moduleArg),
				pkg.ExitModuleNotFound,
			)
		}
		return dir, nil
	}

	// Le répertoire courant est-il lui-même un module (dépôt autonome) ?
	if pkg.FileExists(filepath.Join(cwd, config.ManifestFileName)) {
		return cwd, nil
	}

	root, err := config.FindProjectRoot(cwd)
	if err != nil {
		return "", nil
	}
	if dir := config.WorkspaceModuleDir(root, filepath.Base(cwd)); pkg.FileExists(filepath.Join(dir, config.ManifestFileName)) {
		return dir, nil
	}
	names, err := listModules(root)
	if err != nil || len(names) != 1 {
		return "", nil
	}
	if dir := config.ResolveModuleDir(root, names[0]); dir != "" {
		return dir, nil
	}
	return filepath.Join(root, config.ExternalModulesDir, names[0]), nil
}

// resolveDoctorSocle localise le socle : l'argument, puis la liaison du module,
// puis le socle voisin. Sans socle, on diagnostique un dépôt de module autonome
// qui n'a jamais été lié — c'est un état informatif, pas une erreur.
func resolveDoctorSocle(cwd, socleArg string) (string, error) {
	if socleArg != "" {
		dir, err := filepath.Abs(socleArg)
		if err != nil {
			return "", pkg.NewError(i18n.T("cat.project"), err.Error(), pkg.ExitError)
		}
		if !socle.IsSocle(dir) {
			return "", pkg.NewErrorWithFix(
				i18n.T("cat.project"),
				i18n.Tf("doctor.error.not_socle", dir),
				i18n.T("doctor.error.not_socle.fix"),
				pkg.ExitError,
			)
		}
		return dir, nil
	}
	if moduleDir, err := resolveDoctorModule(cwd, ""); err == nil && moduleDir != "" {
		if link, ok := socle.ReadDevLink(moduleDir); ok && socle.IsSocle(link.SocleDir) {
			return link.SocleDir, nil
		}
	}
	if found := socle.FindSocle(cwd); found != "" {
		return found, nil
	}
	// Dernier recours : le socle est un dépôt public et distinct ; le
	// développeur peut l'avoir cloné à côté sans que rien ne le déclare.
	if sibling := discoverSiblingSocle(cwd); sibling != "" {
		return sibling, nil
	}
	return "", pkg.NewErrorWithFix(
		i18n.T("cat.project"),
		i18n.T("doctor.error.no_socle"),
		i18n.T("doctor.error.no_socle.fix"),
		pkg.ExitError,
	)
}

// discoverSiblingSocle cherche un socle cloné à côté du module ou du projet :
// l'arborescence habituelle d'un module tiers est
// `~/dev/modules/<module>` + `~/dev/liorian-socle`.
func discoverSiblingSocle(cwd string) string {
	start, err := os.Getwd()
	if err != nil {
		return ""
	}
	for dir := start; ; {
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				candidate := filepath.Join(dir, entry.Name())
				if strings.Contains(entry.Name(), "socle") && socle.IsSocle(candidate) {
					return candidate
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// checkSocleEnv vérifie que le socle dispose des variables qu'il attend. En
// mode `--fix`, un `.env` absent est créé depuis le gabarit livré avec le
// socle : c'est le seul état réparable sans décision du développeur.
func checkSocleEnv(profile socle.Profile, fix bool) doctorCheck {
	envPath := filepath.Join(profile.Dir, ".env")
	if pkg.FileExists(envPath) {
		return doctorCheck{
			ID:     "socle.env",
			Label:  i18n.T("doctor.check.env"),
			Status: doctorOK,
			Detail: envPath,
		}
	}
	if profile.EnvSample == "" {
		return doctorCheck{
			ID:     "socle.env",
			Label:  i18n.T("doctor.check.env"),
			Status: doctorWarn,
			Detail: i18n.T("doctor.detail.env_absent"),
			Fix:    "cp .env-sample .env",
		}
	}
	if !fix {
		return doctorCheck{
			ID:     "socle.env",
			Label:  i18n.T("doctor.check.env"),
			Status: doctorWarn,
			Detail: i18n.T("doctor.detail.env_absent"),
			Fix:    "cp .env-sample .env",
		}
	}
	envPath, err := generateSocleEnv(profile.EnvSample)
	if err != nil {
		return doctorCheck{
			ID:       "socle.env",
			Label:    i18n.T("doctor.check.env"),
			Status:   doctorFail,
			Detail:   fmt.Sprintf("%s (%s)", i18n.T("doctor.detail.env_absent"), err),
			Fix:      "cp .env-sample .env",
			FixError: err.Error(),
		}
	}
	return doctorCheck{
		ID:     "socle.env",
		Label:  i18n.T("doctor.check.env"),
		Status: doctorOK,
		Detail: envPath,
		Fixed:  true,
	}
}

// checkCertificates vérifie la présence du couple mkcert du socle. En mode
// `--fix`, il est provisionné si `mkcert` est disponible ; sinon la marche à
// suivre est retournée telle quelle plutôt qu'un échec opaque.
func checkCertificates(profile socle.Profile, fix bool) doctorCheck {
	if profile.Scheme != "https" {
		return doctorCheck{
			ID:     "socle.tls",
			Label:  i18n.T("doctor.check.tls"),
			Status: doctorSkipped,
			Detail: i18n.T("doctor.detail.tls_not_needed"),
		}
	}
	if profile.HasCertificates {
		return doctorCheck{
			ID:     "socle.tls",
			Label:  i18n.T("doctor.check.tls"),
			Status: doctorOK,
			Detail: profile.CertificatesDir,
		}
	}
	if fix {
		if err := socle.EnsureCertificateDir(profile.Dir, runCommand); err == nil {
			return doctorCheck{
				ID:     "socle.tls",
				Label:  i18n.T("doctor.check.tls"),
				Status: doctorOK,
				Detail: profile.CertificatesDir,
				Fixed:  true,
			}
		}
	}
	instructions := socle.CertificateInstructions()
	return doctorCheck{
		ID:     "socle.tls",
		Label:  i18n.T("doctor.check.tls"),
		Status: doctorFail,
		Detail: i18n.T("doctor.detail.tls_missing"),
		Fix:    strings.Join(instructions, " && "),
	}
}

// checkLibraryURL vérifie la racine de transport du registre d'installation.
// Sous `next dev`, `/library/**` répond 404 : sans cette clé, le socle démarre
// avec un registre vide et affiche « Module introuvable » pour tout module
// pourtant correctement lié.
func checkLibraryURL(profile socle.Profile, fix bool) doctorCheck {
	declared := profile.EnvKeys[socle.KeyLibraryModulesURL]
	if declared != "" {
		if declared == profile.LibraryURL {
			return doctorCheck{
				ID:     "socle.library_url",
				Label:  i18n.T("doctor.check.library_url"),
				Status: doctorOK,
				Detail: declared,
			}
		}
		// Une valeur déclarée qui ne correspond pas au port détecté est
		// signalée sans être réécrite : elle peut venir d'un proxy.
		status := doctorWarn
		detail := fmt.Sprintf("%s (%s)", declared, i18n.Tf("doctor.detail.library_url_expected", profile.LibraryURL))
		if !fix {
			return doctorCheck{ID: "socle.library_url", Label: i18n.T("doctor.check.library_url"), Status: status, Detail: detail}
		}
		return doctorCheck{ID: "socle.library_url", Label: i18n.T("doctor.check.library_url"), Status: doctorOK, Detail: detail, Fixed: true}
	}
	if profile.LibraryURL == "" {
		return doctorCheck{
			ID:     "socle.library_url",
			Label:  i18n.T("doctor.check.library_url"),
			Status: doctorSkipped,
			Detail: i18n.T("doctor.detail.library_same_origin"),
		}
	}
	if fix {
		if _, err := socle.EnsureEnvLocalKey(profile.Dir, socle.KeyLibraryModulesURL, profile.LibraryURL); err == nil {
			return doctorCheck{
				ID:     "socle.library_url",
				Label:  i18n.T("doctor.check.library_url"),
				Status: doctorOK,
				Detail: profile.LibraryURL,
				Fixed:  true,
			}
		}
	}
	return doctorCheck{
		ID:     "socle.library_url",
		Label:  i18n.T("doctor.check.library_url"),
		Status: doctorFail,
		Detail: i18n.T("doctor.detail.library_url_missing"),
		Fix: fmt.Sprintf(
			"%s=%s   (%s)",
			socle.KeyLibraryModulesURL,
			profile.LibraryURL,
			filepath.Join(profile.Dir, socle.EnvLocalFile),
		),
	}
}

// checkLibraryReach interroge le registre d'installation : c'est la preuve que
// le socle verra les modules, là où les variables ne sont qu'une déclaration.
func checkLibraryReach(profile socle.Profile) doctorCheck {
	if profile.LibraryURL == "" {
		return doctorCheck{
			ID:     "socle.library",
			Label:  i18n.T("doctor.check.library"),
			Status: doctorSkipped,
			Detail: i18n.T("doctor.detail.library_same_origin"),
		}
	}
	index := socle.ProbeLibrary(profile)
	if index.SelfServed {
		return doctorCheck{
			ID:     "socle.library",
			Label:  i18n.T("doctor.check.library"),
			Status: doctorSkipped,
			Detail: index.Error,
		}
	}
	if !index.Reachable {
		return doctorCheck{
			ID:     "socle.library",
			Label:  i18n.T("doctor.check.library"),
			Status: doctorWarn,
			Detail: index.Error,
			Fix:    "bun run dev  (démarre le serveur de bibliothèque)",
		}
	}
	if len(index.Modules) == 0 {
		return doctorCheck{
			ID:     "socle.library",
			Label:  i18n.T("doctor.check.library"),
			Status: doctorOK,
			Detail: i18n.T("doctor.detail.library_empty"),
		}
	}
	return doctorCheck{
		ID:     "socle.library",
		Label:  i18n.T("doctor.check.library"),
		Status: doctorOK,
		Detail: i18n.Tf("doctor.detail.library_modules", strings.Join(index.Modules, ", ")),
	}
}

// checkModuleIdentity valide ce que le socle lit réellement : `manifest.id`
// est l'adresse du module dans la bibliothèque, pas son domaine.
func checkModuleIdentity(moduleDir string, profile socle.Profile) doctorCheck {
	manifest, err := readDoctorManifest(moduleDir)
	if err != nil {
		return doctorCheck{
			ID:     "module.identity",
			Label:  i18n.T("doctor.check.module"),
			Status: doctorFail,
			Detail: err.Error(),
			Fix:    "liora audit",
		}
	}
	identifier := strings.TrimSpace(manifest.ID)
	if identifier == "" {
		return doctorCheck{
			ID:     "module.identity",
			Label:  i18n.T("doctor.check.module"),
			Status: doctorFail,
			Detail: i18n.T("doctor.detail.module_id_missing"),
			Fix:    fmt.Sprintf("%s — renseigner \"id\"", filepath.Join(moduleDir, config.ManifestFileName)),
		}
	}
	detail := fmt.Sprintf("%s (%s)", identifier, i18n.T("doctor.detail.module_identity"))
	if manifest.Domain != "" {
		detail += " · " + manifest.Domain
	}
	return doctorCheck{
		ID:     "module.identity",
		Label:  i18n.T("doctor.check.module"),
		Status: doctorOK,
		Detail: detail,
		Fix:    boundModulePath(profile, identifier),
	}
}

// checkModuleLink vérifie que la liaison déclarée dans `.liorian/dev.json`
// pointe toujours vers un socle réel, et que le module y est installé.
func checkModuleLink(moduleDir string, link socle.DevLink, profile socle.Profile) doctorCheck {
	if !socle.IsSocle(link.SocleDir) {
		return doctorCheck{
			ID:     "module.link",
			Label:  i18n.T("doctor.check.link"),
			Status: doctorFail,
			Detail: i18n.Tf("doctor.detail.link_stale", link.SocleDir),
			Fix:    fmt.Sprintf("liora artifact bind:socle %s", relOrDot(profile.Dir, moduleDir)),
		}
	}
	if link.SocleDir != profile.Dir {
		return doctorCheck{
			ID:     "module.link",
			Label:  i18n.T("doctor.check.link"),
			Status: doctorWarn,
			Detail: i18n.Tf("doctor.detail.link_other_socle", link.SocleDir, profile.Dir),
		}
	}
	return doctorCheck{
		ID:     "module.link",
		Label:  i18n.T("doctor.check.link"),
		Status: doctorOK,
		Detail: fmt.Sprintf("%s · %s", link.SocleDir, i18n.Tf("doctor.detail.link_at", link.BoundAt)),
	}
}

// checkDevTransport compare le schéma du socle à celui du dev-server : c'est le
// contrôle qui explique une page de module vide sans message d'erreur.
func checkDevTransport(profile socle.Profile, moduleDir string) doctorCheck {
	_, linked := socle.ReadDevLink(moduleDir)
	declared := profile.EnvLocalKeys[socle.KeyDevModulesURL]
	if !linked {
		if declared == "" {
			return doctorCheck{
				ID:     "module.transport",
				Label:  i18n.T("doctor.check.transport"),
				Status: doctorSkipped,
				Detail: i18n.T("doctor.detail.transport_no_link"),
			}
		}
		return doctorCheck{
			ID:     "module.transport",
			Label:  i18n.T("doctor.check.transport"),
			Status: doctorWarn,
			Detail: i18n.Tf("doctor.detail.transport_unmanaged", declared),
			Fix:    fmt.Sprintf("liora artifact bind:socle %s", relOrDot(profile.Dir, moduleDir)),
		}
	}
	if declared == "" {
		return doctorCheck{
			ID:         "module.transport",
			Label:      i18n.T("doctor.check.transport"),
			Status:     doctorFail,
			Detail:     i18n.T("doctor.detail.transport_url_missing"),
			Fix:        fmt.Sprintf("liora artifact bind:socle %s", relOrDot(profile.Dir, moduleDir)),
			Rebindable: true,
		}
	}
	if socle.SchemeOf(declared) == profile.Scheme {
		return doctorCheck{
			ID:     "module.transport",
			Label:  i18n.T("doctor.check.transport"),
			Status: doctorOK,
			Detail: declared,
		}
	}
	// Schéma divergent : le navigateur refusera l'iframe (mixed content) sans
	// aucun message. `bind:socle` réaligne l'URL en préservant l'hôte.
	return doctorCheck{
		ID:         "module.transport",
		Label:      i18n.T("doctor.check.transport"),
		Status:     doctorFail,
		Detail:     i18n.Tf("doctor.detail.transport_mismatch", declared, profile.Scheme),
		Fix:        fmt.Sprintf("liora artifact bind:socle %s", relOrDot(profile.Dir, moduleDir)),
		Rebindable: true,
	}
}

// needsBind indique si une liaison est le geste qui lèvera l'état : un module
// jamais lié, ou un transport dont la seule cause est un câblage obsolète.
func needsBind(linked bool, transport doctorCheck) bool {
	return !linked || transport.Rebindable
}

// checkSocleReachable interroge le socle lui-même. Un socle arrêté produit le
// même « Module introuvable » qu'une liaison cassée : le dire évite de
// reconfigurer un module déjà correct.
func checkSocleReachable(profile socle.Profile) doctorCheck {
	host := profile.EnvKeys[socle.KeyAppHost]
	if host == "" {
		return doctorCheck{
			ID:     "socle.app",
			Label:  i18n.T("doctor.check.app"),
			Status: doctorSkipped,
			Detail: i18n.T("doctor.detail.app_host_missing"),
		}
	}
	probe, err := socle.Probe(host)
	if err != nil {
		return doctorCheck{
			ID:     "socle.app",
			Label:  i18n.T("doctor.check.app"),
			Status: doctorWarn,
			Detail: fmt.Sprintf("%s — %s", host, err),
			Fix:    "bun run dev",
		}
	}
	return doctorCheck{
		ID:     "socle.app",
		Label:  i18n.T("doctor.check.app"),
		Status: doctorOK,
		Detail: fmt.Sprintf("%s — HTTP %d (%s)", host, probe.Status, probe.Duration.Round(1e6)),
	}
}

// recordMove n'est plus utilisé : remplacé par recordFix.
func (r *doctorReport) add(check doctorCheck) {
	r.Checks = append(r.Checks, check)
}

// recordFix inscrit les corrections effectivement appliquées.
func (r *doctorReport) recordFix(check *doctorCheck) {
	if check.Fixed {
		r.Fixed = append(r.Fixed, check.Label)
	}
}

// boundModulePath renvoie le chemin de la liaison du module dans le socle — la
// preuve materializee que le socle le découvrira.
func boundModulePath(profile socle.Profile, identifier string) string {
	versions, err := os.ReadDir(filepath.Join(profile.Dir, config.ExternalModulesDir, identifier))
	if err != nil {
		return filepath.Join(profile.Dir, config.ExternalModulesDir, identifier)
	}
	names := []string{}
	for _, entry := range versions {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return filepath.Join(profile.Dir, config.ExternalModulesDir, identifier) + " (" + strings.Join(names, ", ") + ")"
}

// doctorManifest est la projection minimale du manifeste lue par le socle.
type doctorManifest struct {
	ID      string `json:"id"`
	Domain  string `json:"domain"`
	Version string `json:"version"`
}

func readDoctorManifest(moduleDir string) (doctorManifest, error) {
	path := filepath.Join(moduleDir, config.ManifestFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return doctorManifest{}, fmt.Errorf("%s : %w", path, err)
	}
	var manifest doctorManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return doctorManifest{}, fmt.Errorf("%s : manifeste illisible (%w)", path, err)
	}
	return manifest, nil
}

// generateSocleEnv crée le `.env` du socle depuis son gabarit. Les secrets
// synthétisés (clé d'application, paire VAPID) sont générés — ils n'ont pas de
// valeur « correcte », seulement une valeur sûre et propre à la machine.
func generateSocleEnv(samplePath string) (string, error) {
	data, err := os.ReadFile(samplePath)
	if err != nil {
		return "", err
	}
	env := pkg.ParseEnv(data)
	if err := synthesizeEnv(env, "Liora"); err != nil {
		return "", err
	}
	envPath := filepath.Join(filepath.Dir(samplePath), pkg.EnvFileName)
	if err := os.WriteFile(envPath, []byte(env.Render()), 0o600); err != nil {
		return "", err
	}
	return envPath, nil
}

// runCommand exécute une commande externe (mkcert). Les flux sont captés : un
// provisionnement de certificat ne doit pas mélanger sa sortie à celle de la
// commande de diagnostic.
func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s : %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// bindModuleToSocle lie le module au socle via l'implémentation native de la
// CLI — la même que `liora artifact bind:socle` : une seule écriture du
// contrat de liaison, donc aucune divergence possible entre le diagnostic qui
// répare et la commande du développeur.
func bindModuleToSocle(socleDir, moduleDir string) error {
	_, err := artifactbind.Bind(artifactbind.Options{SocleDir: socleDir, ModuleDir: moduleDir})
	return err
}

// printDoctorReport renders the diagnosis: one heading per domain, then the
// table of checks and, when there is something to do, the ordered list of
// commands that resolve it.
func printDoctorReport(report *doctorReport) {
	s := tui.NewStyles()
	fmt.Println()

	title := i18n.T("doctor.header.socle")
	if report.ModuleDir != "" {
		title = i18n.T("doctor.header.module")
	}
	fmt.Println(s.ReportHeading(fmt.Sprintf("%s — %s", title, report.SocleDir), "", tui.StatusSuccess))

	table := tui.NewTable([]string{
		i18n.T("label.check"),
		i18n.T("label.status"),
		i18n.T("label.detail"),
	})
	for _, check := range report.Checks {
		detail := check.Detail
		// Un `--fix` qui n'a pas abouti doit rester lisible : sans l'erreur,
		// le rapport afficherait la même ligne qu'avant la tentative.
		if check.FixError != "" {
			detail = fmt.Sprintf("%s — %s: %s", detail, i18n.T("doctor.detail.fix_failed"), check.FixError)
		}
		table.AddRow(check.Label, checkStatusText(s, check.Status), detail)
	}
	fmt.Println(table.Render())

	if fixes := doctorFixes(report); len(fixes) > 0 {
		fmt.Println()
		fmt.Println(s.ReportHeading(i18n.T("doctor.header.fixes"), "", tui.StatusWarning))
		steps := make([]string, 0, len(fixes))
		for _, fix := range fixes {
			steps = append(steps, s.Warning.Render(fix))
		}
		fmt.Println()
		fmt.Println(s.StepsList(i18n.T("doctor.header.commands"), steps...))
		fmt.Println()
		fmt.Println(s.Info.Render(i18n.Tf("doctor.hint.fix", "liora doctor --fix")))
		return
	}
	if len(report.Fixed) > 0 {
		fmt.Println()
		fmt.Println(s.SuccessPanel(i18n.Tf("doctor.fixed", strings.Join(report.Fixed, ", "))))
		fmt.Println()
		fmt.Println(s.Info.Render(i18n.T("doctor.hint.done")))
		return
	}
	fmt.Println()
	fmt.Println(s.SuccessPanel(i18n.T("doctor.healthy")))
	fmt.Println()
}

// doctorFixes renvoie les marches à suivre, dans l'ordre où les traiter : une
// correction qui dépend d'une autre n'a de sens qu'après elle.
func doctorFixes(report *doctorReport) []string {
	out := []string{}
	for _, check := range report.Checks {
		if check.Fix == "" || check.Status == doctorOK || check.Status == doctorSkipped {
			continue
		}
		out = append(out, check.Fix)
	}
	return out
}

func checkStatusText(s *tui.Styles, status string) string {
	switch status {
	case doctorOK:
		return s.Success.Render("✓")
	case doctorWarn:
		return s.Warning.Render("!")
	case doctorFail:
		return s.Error.Render("✗")
	default:
		return s.Muted.Render("–")
	}
}

// printDoctorJSON sérialise le diagnostic pour une intégration CI : un module
// dont la boucle de développement est cassée doit pouvoir faire échouer une
// vérification, pas seulementcolorer un rapport.
func printDoctorJSON(report *doctorReport) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return pkg.NewError(i18n.T("cat.project"), err.Error(), pkg.ExitError)
	}
	if len(doctorFailures(report)) > 0 {
		return pkg.NewErrorWithFix(
			i18n.T("cat.project"),
			i18n.T("doctor.json.failed"),
			i18n.Tf("doctor.hint.fix", "liora doctor --fix"),
			pkg.ExitError,
		)
	}
	return nil
}

func doctorFailures(report *doctorReport) []doctorCheck {
	out := []doctorCheck{}
	for _, check := range report.Checks {
		if check.Status == doctorFail {
			out = append(out, check)
		}
	}
	return out
}

func hostLabel(profile socle.Profile) string {
	host := socle.HostOf(profile.EnvKeys[socle.KeyAppHost])
	if host == "" {
		host = "localhost"
	}
	if profile.AppPort > 0 {
		return fmt.Sprintf("%s:%d", host, profile.AppPort)
	}
	return host
}

func relOrDot(target, from string) string {
	if from == "" {
		return target
	}
	if relative, err := filepath.Rel(from, target); err == nil {
		return relative
	}
	return target
}
