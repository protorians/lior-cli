// Package socle décrit le dépôt d'un socle Liora vu par la CLI : sa
// topologie (schéma d'URL, ports, serveur de bibliothèque), son état de
// développement (`.env`, certificats mkcert) et le contrat de liaison qu'un
// module en développement doit respecter.
//
// La boucle de développement d'un module produit rarement une erreur visible :
// un registre d'installation vide, un schéma divergent ou un certificat
// absent se traduisent par une page qui affiche « Module introuvable ou non
// installé » — ou par une iframe que le navigateur refuse d'afficher, sans
// message. Ce package expose l'état sous forme de contrôles nommés afin que
// `liora doctor` puisse nommer la cause au lieu de laisser le développeur
// chercher.
//
// Le contrat de liaison est celui posé par `internal/artifactbind` — la même
// implémentation que `liora artifact bind:socle` et `liora doctor --fix` : un
// seul code écrit la bibliothèque et le `.env.local`, donc le diagnostic et la
// commande ne peuvent pas diverger.
package socle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Clés d'environnement qui décrivent le transport d'un socle. Elles sont
// contractuelles : `NEXT_PUBLIC_LIBRARY_MODULES_URL` est lue par le socle au
// démarrage pour peupler son registre d'installation.
const (
	KeyAppHost           = "NEXT_PUBLIC_APP_HOST"
	KeyLibraryModulesURL = "NEXT_PUBLIC_LIBRARY_MODULES_URL"
	KeyDevModulesURL     = "NEXT_PUBLIC_DEV_MODULES_URL"
	KeyDevModules        = "NEXT_PUBLIC_DEV_MODULES"
	// KeyDevModulesMulti active la résolution multi-modules du transport :
	// l'URL dev est alors préfixée par le slug de l'identifiant
	// (`<DEV_MODULES_URL>/<slug>/index.html`) au lieu de la racine du
	// dev-server mono-module. Posée par le dev-server multi-modules.
	KeyDevModulesMulti = "NEXT_PUBLIC_DEV_MODULES_MULTI"
	KeyLibraryPort     = "LIBRARY_PORT"
)

// DefaultLibraryPort est le port du serveur de bibliothèque dédié
// (`scripts/serve-library.mjs`).
const DefaultLibraryPort = 5011

// EnvLocalFile est le fichier d'environnement local du socle — non versionné,
// contrairement à `.env`.
const EnvLocalFile = ".env.local"

// DevLinkFile est le lien de développement écrit dans le module par
// `artifact bind:socle`.
const DevLinkFile = ".liorian/dev.json"

// Profile est l'état déduit du dépôt d'un socle. Tous les champs sont
// renseignés sans jamais échouer : un socle inhabituel produit un profil
// partial, jamais une erreur — le diagnostic doit pouvoir s'afficher quand
// rien ne va.
type Profile struct {
	// Dir est le dossier du socle.
	Dir string
	// Name est le `package.json#name`, ou le nom du dossier à défaut.
	Name string
	// Scheme est le schéma sous lequel le développeur visite le socle.
	Scheme string
	// DevServer indique que le socle est servi par `next dev`, donc que
	// `/library/**` n'est pas servi par lui.
	DevServer bool
	// AppPort est le port du socle lorsqu'il est dédié.
	AppPort int
	// LibraryPort est le port du serveur de bibliothèque, 0 si le socle
	// n'en démarre pas.
	LibraryPort int
	// CertificatesDir est le répertoire des certificats mkcert.
	CertificatesDir string
	// HasCertificates indique que le couple certificat/clé est présent.
	HasCertificates bool
	// LibraryURL est la racine de transport à inscrire pour un socle en
	// développement, vide lorsque le socle sert lui-même `/library`.
	LibraryURL string
	// EnvKeys sont les variables transport effectivement déclarées.
	EnvKeys map[string]string
	// EnvLocalKeys sont celles de `.env.local` (le câblage de `bind:socle`).
	EnvLocalKeys map[string]string
	// EnvSample est le gabarit d'environnement livré avec le socle, vide
	// s'il n'en fournit pas.
	EnvSample string
}

// DevLink est le lien de développement écrit par `artifact bind:socle` à la
// racine du module.
type DevLink struct {
	SocleDir    string `json:"socleDir"`
	SocleScheme string `json:"socleScheme"`
	LibraryPort int    `json:"libraryPort"`
	DevHost     string `json:"devHost"`
	DevPort     int    `json:"devPort"`
	BoundAt     string `json:"boundAt"`
}

// IsSocle rapport whether dir porte les marqueurs d'un socle : `library/`,
// `serve.mjs`, ou un `package.json` dont le nom contient « socle ». Le nom du
// dossier n'est pas retenu — un checkout mal nommé n'est pas un socle.
func IsSocle(dir string) bool {
	if IsDir(filepath.Join(dir, "library")) || FileExists(filepath.Join(dir, "serve.mjs")) {
		return true
	}
	return strings.Contains(strings.ToLower(declaredPackageName(dir)), "socle")
}

// declaredPackageName returns the `name` declared by a package.json, or an empty
// string when there is none.
//
// It is deliberately distinct from PackageName, which falls back to the
// directory name for display: a fallback here would make any temporary folder
// called `not-a-socle` pass for a socle.
func declaredPackageName(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return ""
	}
	var parsed struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return ""
	}
	return parsed.Name
}

// FindSocle cherche un socle dans dir, puis dans ses parents jusqu'à la racine
// du système de fichiers. La recherche s'arrête au premier socle trouvé : le
// plus proche est le plus pertinent (un module vit à côté de son socle).
func FindSocle(dir string) string {
	current, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		if IsSocle(current) {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

// ReadProfile inspecte un dossier de socle et en déduit son état de
// développement.
func ReadProfile(dir string) Profile {
	scripts := PackageScripts(dir)
	env := readEnvFile(filepath.Join(dir, ".env"))
	envLocal := readEnvFile(filepath.Join(dir, EnvLocalFile))

	// `.env.local` écrase `.env` pour les variables NEXT_PUBLIC_*, mais les
	// deux sont lus pour qu'une clé présente dans l'un reste visible.
	merged := map[string]string{}
	for key, value := range env {
		merged[key] = value
	}
	for key, value := range envLocal {
		merged[key] = value
	}

	devScript := resolveDevScript(scripts)
	libraryScript := scripts["dev:library"]
	profile := Profile{
		Dir:             dir,
		Name:            PackageName(dir),
		Scheme:          resolveScheme(merged[KeyAppHost], devScript, scripts["start"]),
		DevServer:       regexp.MustCompile(`\bnext\s+dev\b`).MatchString(devScript),
		AppPort:         resolveAppPort(devScript, merged[KeyAppHost]),
		LibraryPort:     resolveLibraryPort(dir, libraryScript, merged[KeyLibraryPort]),
		CertificatesDir: filepath.Join(dir, "certificates"),
		EnvKeys:         merged,
		EnvLocalKeys:    envLocal,
		EnvSample:       findEnvSample(dir),
	}
	profile.HasCertificates = FileExists(filepath.Join(profile.CertificatesDir, "localhost.pem")) &&
		FileExists(filepath.Join(profile.CertificatesDir, "localhost-key.pem"))

	host := HostOf(merged[KeyAppHost])
	if host == "" {
		host = "localhost"
	}
	// Sous `serve.mjs` la bibliothèque est servie sur la même origine que le
	// socle : `/library` relatif reste le bon réglage.
	if profile.DevServer && profile.LibraryPort > 0 {
		profile.LibraryURL = profile.Scheme + "://" + host + ":" +
			strconv.Itoa(profile.LibraryPort) + "/library"
	}
	return profile
}

// ReadDevLink lit le lien de développement d'un module. Il indique le socle
// visé par `artifact bind:socle` et, par là, le schéma sous lequel le
// dev-server du module doit être servi.
func ReadDevLink(moduleDir string) (DevLink, bool) {
	var link DevLink
	data, err := os.ReadFile(filepath.Join(moduleDir, DevLinkFile))
	if err != nil {
		return link, false
	}
	if err := json.Unmarshal(data, &link); err != nil || link.SocleDir == "" {
		return DevLink{}, false
	}
	return link, true
}

// resolveDevScript choisit le script qui décrit le lancement de l'application
// du socle : `dev:app` d'abord (le contenu historique de `dev`, déplacé quand
// `dev` est devenu `liora socle dev`), puis `dev:socle`, puis `dev`. Un
// script qui délègue à `liora socle` est écarté : décrire le serveur central
// par le script qu'il exécute lui-même masquerait `next dev` (le socle
// semblerait sans serveur de dev) et ferait boucler le profil.
func resolveDevScript(scripts map[string]string) string {
	if script := runnableDevScript(scripts["dev:app"]); script != "" {
		return script
	}
	return strings.Join(nonEmpty(
		runnableDevScript(scripts["dev:socle"]),
		runnableDevScript(scripts["dev"])), " && ")
}

// runnableDevScript écarte un script absent ou qui délègue à la CLI.
func runnableDevScript(script string) string {
	script = strings.TrimSpace(script)
	if script == "" || strings.Contains(script, "liora socle") {
		return ""
	}
	return script
}

// AppScriptName nomme le script npm qui lance l'application du socle —
// `liora socle dev` l'exécute pour héberger le front à côté du serveur
// central. Ordre : `dev:app`, puis `dev:socle`, puis `dev` ; un script qui
// délègue à `liora socle` est écarté (boucle). Vide si aucun n'est
// exécutable : le socle est alors serveur central seul.
func AppScriptName(scripts map[string]string) string {
	for _, name := range []string{"dev:app", "dev:socle", "dev"} {
		if runnableDevScript(scripts[name]) != "" {
			return name
		}
	}
	return ""
}

// resolveScheme déduit le schéma du socle : l'hôte d'application déclaré
// d'abord (c'est l'URL que le développeur visite), puis le script de dev
// (`--experimental-https`), puis le serveur de production (`serve.mjs` est en
// HTTP derrière un proxy TLS). Sans signal, `https` — la posture canonique du
// socle en développement.
func resolveScheme(appHost, devScript, startScript string) string {
	if scheme := SchemeOf(appHost); scheme != "" {
		return scheme
	}
	if strings.Contains(devScript, "--experimental-https") {
		return "https"
	}
	if strings.Contains(startScript, "serve.mjs") {
		return "http"
	}
	return "https"
}

// resolveAppPort lit le port dédié du socle : `next dev -p 5010`, sinon le port
// de l'hôte d'application.
func resolveAppPort(devScript, appHost string) int {
	if match := regexp.MustCompile(`\bnext\s+dev\b[^&|]*?-p\s+(\d{2,5})`).FindStringSubmatch(devScript); match != nil {
		if port, err := strconv.Atoi(match[1]); err == nil {
			return port
		}
	}
	if port := PortOf(appHost); port > 0 {
		return port
	}
	return 0
}

// resolveLibraryPort lit le port du serveur de bibliothèque : `LIBRARY_PORT`,
// puis le port explicite du script, puis le défaut déclaré par
// `scripts/serve-library.mjs`. Zéro si le socle n'en démarre pas.
func resolveLibraryPort(socleDir, libraryScript, declared string) int {
	if match := regexp.MustCompile(`--port[= ](\d{2,5})`).FindStringSubmatch(libraryScript); match != nil {
		if port, err := strconv.Atoi(match[1]); err == nil {
			return port
		}
	}
	if port, err := strconv.Atoi(strings.TrimSpace(declared)); err == nil && port > 0 {
		return port
	}
	source := ""
	if data, err := os.ReadFile(filepath.Join(socleDir, "scripts", "serve-library.mjs")); err == nil {
		source = string(data)
	}
	if match := regexp.MustCompile(`LIBRARY_PORT\s*\|\|\s*(\d{2,5})`).FindStringSubmatch(source); match != nil {
		if port, err := strconv.Atoi(match[1]); err == nil {
			return port
		}
	}
	// `bun run dev:library &` n'est pas vérifié : la seule présence du
	// script suffit à exiger l'URL.
	if strings.Contains(libraryScript, "serve-library") || source != "" {
		return DefaultLibraryPort
	}
	return 0
}

// SchemeOf renvoie le schéma d'une URL ("http", "https"), ou une chaîne vide.
func SchemeOf(url string) string {
	switch {
	case strings.HasPrefix(strings.TrimSpace(url), "https://"):
		return "https"
	case strings.HasPrefix(strings.TrimSpace(url), "http://"):
		return "http"
	default:
		return ""
	}
}

// HostOf renvoie l'hôte d'une URL, sans son port ni son chemin. Une chaîne qui
// n'est pas une URL (pas de `://`) renvoie une chaîne vide : mieux vaut aucune
// information qu'un nom d'hôte fantôme tel que `localhost:5010/du texte`.
func HostOf(url string) string {
	trimmed := strings.TrimSpace(url)
	index := strings.Index(trimmed, "://")
	if index < 0 {
		return ""
	}
	rest := trimmed[index+3:]
	if index := strings.IndexAny(rest, "/?#"); index >= 0 {
		rest = rest[:index]
	}
	// Port après le dernier `:` : un IPv6 littéral contient déjà des `:`.
	if index := strings.LastIndex(rest, ":"); index > 0 && !strings.Contains(rest[index:], "]") {
		rest = rest[:index]
	}
	return rest
}

// PortOf renvoie le port d'une URL, zéro s'il est absent ou invalide.
func PortOf(url string) int {
	trimmed := strings.TrimSpace(url)
	index0 := strings.Index(trimmed, "://")
	if index0 < 0 {
		return 0
	}
	rest := trimmed[index0+3:]
	if index := strings.IndexAny(rest, "/?#"); index >= 0 {
		rest = rest[:index]
	}
	index := strings.LastIndex(rest, ":")
	if index < 0 || strings.Contains(rest[index:], "]") {
		return 0
	}
	port, err := strconv.Atoi(rest[index+1:])
	if err != nil {
		return 0
	}
	return port
}

// readEnvFile lit un fichier dotenv sans interpréter les valeurs : seules des
// clés de transport (URL, ports) sont lues ici, jamais un secret.
func readEnvFile(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	out := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))
		index := strings.Index(body, "=")
		if index <= 0 {
			continue
		}
		key := strings.TrimSpace(body[:index])
		value := strings.TrimSpace(body[index+1:])
		if len(value) >= 2 {
			first, last := value[0], value[len(value)-1]
			if (first == '"' || first == '\'') && last == first {
				value = value[1 : len(value)-1]
			}
		}
		if key != "" {
			out[key] = value
		}
	}
	return out
}

// PackageScripts returns the `scripts` of a package.json, an empty map when
// the manifest is absent or illisible.
func PackageScripts(dir string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return out
	}
	var parsed struct {
		Scripts map[string]any `json:"scripts"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return out
	}
	for name, value := range parsed.Scripts {
		if script, ok := value.(string); ok {
			out[name] = script
		}
	}
	return out
}

// PackageName returns the name declared by a package.json, or the directory
// name when there is none.
func PackageName(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return filepath.Base(dir)
	}
	var parsed struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil || parsed.Name == "" {
		return filepath.Base(dir)
	}
	return parsed.Name
}

// EnvSampleCandidates sont les noms de gabarit d'environnement sondés.
var EnvSampleCandidates = []string{
	".env-sample", ".env.sample", ".env.example", ".env.dist", ".env.template",
}

// findEnvSample renvoie le premier gabarit d'environnement présent dans dir.
func findEnvSample(dir string) string {
	for _, name := range EnvSampleCandidates {
		path := filepath.Join(dir, name)
		if FileExists(path) {
			return path
		}
	}
	return ""
}

// FileExists reports whether path is an existing non-directory file.
func FileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// IsDir reports whether path is an existing directory.
func IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func nonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}
