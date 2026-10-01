// Package artifactbind matérialise la liaison d'un module en développement à
// un socle — `liora artifact bind:socle` / `unbind:socle` (spec
// docs/specs/applications/module-isolated-runtime.md, §4.4 / §8, D11).
//
// Le socle découvre ses modules locaux dans `<socle>/library/modules/<id>/…`
// (pointeur `current` + manifeste de version) et sert l'artefact sous
// `/library/**`. Développer un module hors du dossier du socle — il vit dans
// `modules/<id>/` du workspace et se pousse sur un dépôt public — impose de
// matérialiser ce layout par des liens symboliques : le module apparaît dans
// le registre du socle, l'artefact servi est celui que `liora artifact dev`
// réécrit en continu, et rien du module n'est dupliqué dans le dépôt du
// socle. Le HMR réel est porté par le dev-server : les clés
// `NEXT_PUBLIC_DEV_MODULES_URL` + `NEXT_PUBLIC_DEV_MODULES` sont écrites dans
// le `.env.local` du socle (non versionné), jamais dans un fichier suivi par
// git.
package artifactbind

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/protorians/lior-cli/internal/devlink"
	"github.com/protorians/lior-cli/internal/module"
	"github.com/protorians/lior-cli/internal/socle"
)

// Clés d'environnement du socle (§8).
const (
	// DevModulesURLKey est l'URL du dev-server d'un module en dev.
	DevModulesURLKey = "NEXT_PUBLIC_DEV_MODULES_URL"
	// DevModulesIDsKey liste les identifiants autorisés en dev (`*` ou liste).
	DevModulesIDsKey = "NEXT_PUBLIC_DEV_MODULES"
	// LibraryModulesURLKey est la racine de transport des URL de bibliothèque.
	// Sous `next dev`, `/library/**` répond 404 et, sans cette variable, le
	// registre d'installation reste vide — « Module introuvable ou non
	// installé » alors que la liaison est parfaite.
	LibraryModulesURLKey = "NEXT_PUBLIC_LIBRARY_MODULES_URL"
)

// EnvFile est le fichier d'environnement local du socle — non versionné.
const EnvFile = ".env.local"

// BindMarkerFile est le marqueur du bind, écrit dans le dossier de version.
const BindMarkerFile = ".liorian-bind.json"

// Mode est le mode de matérialisation effectivement retenu.
type Mode string

const (
	// ModeSymlink : les liens ont été posés.
	ModeSymlink Mode = "symlink"
	// ModeCopy : repli quand le système de fichiers refuse les liens.
	ModeCopy Mode = "copy"
)

// Options porte les paramètres d'une liaison.
type Options struct {
	// SocleDir est le dossier du socle (absolu ou relatif au répertoire courant).
	SocleDir string
	// ModuleDir est la racine du module (défaut : répertoire courant).
	ModuleDir string
	// Log reçoit le journal (respecte --quiet).
	Log func(string)
}

// Result résume une liaison posée.
type Result struct {
	SocleDir    string
	ModuleDir   string
	Identifier  string
	Version     string
	Mode        Mode
	VersionDir  string
	EnvPath     string
	DevURL      string
	LibraryURL  string
	DevLinkPath string
	Scheme      string
	Warnings    []string
}

// UnbindResult résume le retrait d'une liaison.
type UnbindResult struct {
	SocleDir       string
	ModuleDir      string
	Identifier     string
	RemovedDir     string
	EnvPath        string
	EnvUpdated     bool
	DevLinkRemoved bool
}

type bindMarker struct {
	Identifier      string `json:"identifier"`
	Version         string `json:"version"`
	ModuleDir       string `json:"moduleDir"`
	ArtifactSource  string `json:"artifactSource"`
	Mode            string `json:"mode"`
	PreviousCurrent string `json:"previousCurrent"`
	BoundAt         string `json:"boundAt"`
}

// Bind lie le module en développement au socle : layout de bibliothèque
// (`<id>/current` + `<id>/<version>/manifest.json` + `<id>/<version>/artifact`)
// en liens symboliques vers le module, câblage HMR dans le `.env.local` du
// socle. Idempotent : un second appel remplace la liaison existante.
func Bind(options Options) (*Result, error) {
	log := options.Log
	if log == nil {
		log = func(string) {}
	}
	socleDir, err := resolveSocleDir(options.SocleDir)
	if err != nil {
		return nil, err
	}
	moduleDir, err := filepath.Abs(firstNonEmpty(options.ModuleDir, "."))
	if err != nil {
		return nil, err
	}
	manifest, err := module.LoadManifest(filepath.Join(moduleDir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("artifact: %v", err)
	}
	identifier := strings.TrimSpace(manifest.ID)
	if identifier == "" {
		return nil, fmt.Errorf("artifact: manifest.id manquant dans %s",
			filepath.Join(moduleDir, "manifest.json"))
	}
	version := firstNonEmpty(manifest.Version, "0.0.0")

	// Layout de développement (D7) : `.liorian/artifact/`, avec repli sur
	// l'ancien `artifact/`. Le répertoire est créé au besoin pour que le lien
	// ne soit pas orphelin avant le premier dev-server.
	artifactSource := filepath.Join(moduleDir, ".liorian", "artifact")
	if !isDir(artifactSource) {
		artifactSource = filepath.Join(moduleDir, "artifact")
	}
	if err := os.MkdirAll(artifactSource, 0o755); err != nil {
		return nil, err
	}

	moduleRoot := filepath.Join(socleDir, "library", "modules", identifier)
	versionDir := filepath.Join(moduleRoot, version)
	currentPath := filepath.Join(moduleRoot, "current")

	// Pointeur d'installation réel éventuellement écrasé : `unbind` le
	// restaurera. Si un bind antérieur (autre version) avait déjà écrasé ce
	// pointeur, on remonte à l'origine réelle pour ne pas la perdre.
	rawCurrent := strings.TrimSpace(readFileIfExists(currentPath))
	previousCurrent := realCurrentPointer(moduleRoot, rawCurrent)
	// Les versions liées obsolètes (manifeste re-versionné) sont retirées :
	// la bibliothèque ne garde que la liaison courante.
	for _, binding := range boundBindings(moduleRoot, moduleDir) {
		if binding.version != version {
			_ = os.RemoveAll(filepath.Join(moduleRoot, binding.version))
		}
	}

	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		return nil, err
	}
	manifestMode := linkOrCopy(filepath.Join(moduleDir, "manifest.json"), filepath.Join(versionDir, "manifest.json"), false)
	artifactMode := linkOrCopy(artifactSource, filepath.Join(versionDir, "artifact"), true)
	mode := ModeSymlink
	if manifestMode != ModeSymlink || artifactMode != ModeSymlink {
		mode = ModeCopy
	}

	if err := os.WriteFile(currentPath, []byte(version+"\n"), 0o644); err != nil {
		return nil, err
	}
	// Le marqueur vit dans le dossier de version : `unbind` ne supprime que
	// les versions qu'il a liées, jamais une installation réelle voisine.
	marker, _ := json.MarshalIndent(bindMarker{
		Identifier:      identifier,
		Version:         version,
		ModuleDir:       moduleDir,
		ArtifactSource:  artifactSource,
		Mode:            string(mode),
		PreviousCurrent: previousCurrent,
		BoundAt:         time.Now().UTC().Format(time.RFC3339),
	}, "", "  ")
	if err := os.WriteFile(filepath.Join(versionDir, BindMarkerFile), append(marker, '\n'), 0o644); err != nil {
		return nil, err
	}

	envPath := filepath.Join(socleDir, EnvFile)
	profile := socle.ReadProfile(socleDir)
	warnings := collectBindWarnings(profile)

	// L'URL du dev-server est inscrite dans le schéma du socle : c'est la
	// seule valeur qui fonctionne dans les deux sens (mixed content dans
	// l'autre).
	devURL := ensureDevURL(envPath, fmt.Sprintf("%s://localhost:%d", profile.Scheme, devPort(moduleDir)))
	addDevModule(envPath, identifier)

	// La racine de transport de la bibliothèque est câblée par le même geste :
	// sans elle, le registre d'installation du socle reste vide sous `next
	// dev`. Seule l'absence de la clé est corrigée — une valeur posée à la
	// main (reverse proxy, docker) est préservée.
	if profile.LibraryURL != "" {
		ensureEnvKey(envPath, LibraryModulesURLKey, profile.LibraryURL)
	}

	// Le lien consigné dans le module permet au dev-server de servir le module
	// dans le même contexte que le socle, sans que le module connaisse la
	// topologie du socle.
	devLinkPath, err := devlink.Write(moduleDir, devlink.Link{
		SocleDir:    socleDir,
		SocleScheme: profile.Scheme,
		LibraryPort: profile.LibraryPort,
		DevHost:     "localhost",
		DevPort:     devPort(moduleDir),
		BoundAt:     time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return nil, err
	}

	log(fmt.Sprintf("artifact: %s@%s lié au socle (%s)", identifier, version, mode))
	log(fmt.Sprintf("artifact: bibliothèque — %s", versionDir))
	log(fmt.Sprintf("artifact: dev-server — %s (%s)", devURL, envPath))
	if profile.LibraryURL != "" {
		log(fmt.Sprintf("artifact: bibliothèque — %s", profile.LibraryURL))
	}
	log(fmt.Sprintf("artifact: lien de développement — %s", devLinkPath))
	for _, warning := range warnings {
		log("artifact: " + warning)
	}

	return &Result{
		SocleDir:    socleDir,
		ModuleDir:   moduleDir,
		Identifier:  identifier,
		Version:     version,
		Mode:        mode,
		VersionDir:  versionDir,
		EnvPath:     envPath,
		DevURL:      devURL,
		LibraryURL:  profile.LibraryURL,
		DevLinkPath: devLinkPath,
		Scheme:      profile.Scheme,
		Warnings:    warnings,
	}, nil
}

// Unbind retire la liaison d'un module : supprime l'entrée de bibliothèque
// créée par `bind:socle` (via son marqueur — une installation réelle n'est
// jamais touchée) et retire l'identifiant du `.env.local` du socle.
func Unbind(options Options) (*UnbindResult, error) {
	log := options.Log
	if log == nil {
		log = func(string) {}
	}
	socleDir, err := resolveSocleDir(options.SocleDir)
	if err != nil {
		return nil, err
	}
	moduleDir, err := filepath.Abs(firstNonEmpty(options.ModuleDir, "."))
	if err != nil {
		return nil, err
	}
	manifest, err := module.LoadManifest(filepath.Join(moduleDir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("artifact: %v", err)
	}
	identifier := strings.TrimSpace(manifest.ID)
	if identifier == "" {
		return nil, fmt.Errorf("artifact: manifest.id manquant dans %s",
			filepath.Join(moduleDir, "manifest.json"))
	}
	version := firstNonEmpty(manifest.Version, "0.0.0")

	moduleRoot := filepath.Join(socleDir, "library", "modules", identifier)
	envPath := filepath.Join(socleDir, EnvFile)
	envUpdated := removeDevModule(envPath, identifier)

	// Le lien de développement décrit une liaison qui n'existe plus : le
	// retirer empêche le dev-server de chercher un socle et un schéma périmés.
	devLinkPath := filepath.Join(moduleDir, devlink.File)
	devLinkRemoved := removeExisting(devLinkPath)
	if devLinkRemoved {
		// Le dossier `.liorian/` peut retenir d'autres fichiers (build,
		// cache) : os.Remove échoue sans dégâts s'il n'est pas vide.
		_ = os.Remove(filepath.Dir(devLinkPath))
	}

	result := &UnbindResult{
		SocleDir:       socleDir,
		ModuleDir:      moduleDir,
		Identifier:     identifier,
		EnvPath:        envPath,
		EnvUpdated:     envUpdated,
		DevLinkRemoved: devLinkRemoved,
	}

	bindings := boundBindings(moduleRoot, moduleDir)
	switch {
	case len(bindings) > 0:
		versions := make([]string, 0, len(bindings))
		for _, bound := range bindings {
			versions = append(versions, bound.version)
			_ = os.RemoveAll(filepath.Join(moduleRoot, bound.version))
		}
		// Le pointeur `current` désignait une version liée : on restaure le
		// pointeur réel d'origine, sinon on le retire (une installation
		// réelle voisine reste active, elle, quoi qu'il arrive).
		currentPath := filepath.Join(moduleRoot, "current")
		restore := ""
		for _, binding := range bindings {
			candidate := binding.previousCurrent
			if candidate == "" || contains(versions, candidate) || !isDir(filepath.Join(moduleRoot, candidate)) {
				continue
			}
			restore = candidate
			break
		}
		if restore != "" {
			_ = os.WriteFile(currentPath, []byte(restore+"\n"), 0o644)
		} else if pathExists(currentPath) {
			active := strings.TrimSpace(readFileIfExists(currentPath))
			if contains(versions, active) {
				_ = os.Remove(currentPath)
			}
		}
		_ = os.Remove(moduleRoot) // Non vide si d'autres installations subsistent.
		result.RemovedDir = moduleRoot
	case pathExists(moduleRoot):
		// Entrée non créée par `bind:socle` : on ne retire que les liens que
		// la liaison aurait posés, jamais une installation réelle.
		if removeOwnedLinks(moduleRoot, version) {
			result.RemovedDir = moduleRoot
		}
	}

	if result.RemovedDir != "" {
		log(fmt.Sprintf("artifact: liaison %s retirée (%s)", identifier, result.RemovedDir))
	} else {
		log(fmt.Sprintf("artifact: aucune liaison %s à retirer", identifier))
	}
	if envUpdated {
		log(fmt.Sprintf("artifact: %s retiré de %s", identifier, envPath))
	}
	if devLinkRemoved {
		log(fmt.Sprintf("artifact: lien de développement retiré (%s)", filepath.Join(moduleDir, devlink.File)))
	}
	return result, nil
}

// collectBindWarnings énumère les points de vigilance de la liaison, tous non
// bloquants : ils décrivent un état qui fonctionnera après une action du
// développeur (mkcert, bun run dev), pas un échec de la liaison elle-même.
func collectBindWarnings(profile socle.Profile) []string {
	var warnings []string
	if profile.Scheme == "https" && !profile.HasCertificates {
		warnings = append(warnings, fmt.Sprintf(
			"socle en HTTPS sans certificat (%s) — `mkcert -install && mkcert localhost` dans le socle, "+
				"sinon le dev-server refusera de démarrer (l'iframe serait bloquée en mixed content)",
			profile.CertificatesDir))
	}
	if profile.DevServer && profile.LibraryURL == "" {
		warnings = append(warnings,
			"socle servi par next dev sans serveur de bibliothèque détecté — "+
				"le registre d'installation restera vide (NEXT_PUBLIC_LIBRARY_MODULES_URL)")
	}
	return warnings
}

// resolveSocleDir valide le dossier du socle et le renvoie absolutisé.
func resolveSocleDir(socleDir string) (string, error) {
	dir, err := filepath.Abs(socleDir)
	if err != nil {
		return "", err
	}
	if !isDir(dir) {
		return "", fmt.Errorf("artifact: dossier du socle introuvable : %s", dir)
	}
	if !socle.IsSocle(dir) {
		return "", fmt.Errorf("artifact: %s ne ressemble pas à un socle Liora "+
			"(library/, serve.mjs ou package.json « socle » attendu)", dir)
	}
	return dir, nil
}

// devPort lit le port de dev attendu : la valeur du lien existant, sinon le
// port canonique (5178).
func devPort(moduleDir string) int {
	if link, ok := devlink.Read(moduleDir); ok && link.DevPort > 0 {
		return link.DevPort
	}
	return 5178
}

// linkOrCopy pose linkPath en lien symbolique vers source, ou en copie si le
// système de fichiers refuse le lien (Windows sans privilège, volume sans
// symlink).
func linkOrCopy(source, linkPath string, dir bool) Mode {
	_ = removeExisting(linkPath)
	target := relativeTarget(filepath.Dir(linkPath), source)
	if err := os.Symlink(target, linkPath); err != nil {
		if dir {
			if err := copyDir(source, linkPath); err != nil {
				return ModeCopy
			}
			return ModeCopy
		}
		if err := copyFile(source, linkPath); err != nil {
			return ModeCopy
		}
		return ModeCopy
	}
	return ModeSymlink
}

func relativeTarget(linkDir, source string) string {
	rel, err := filepath.Rel(linkDir, source)
	if err != nil || rel == "" {
		return source
	}
	return rel
}

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func copyDir(source, destination string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return copyFile(path, target)
	})
}

// removeExisting supprime une entrée existante — lien, fichier ou répertoire.
func removeExisting(path string) bool {
	if !pathExists(path) {
		return false
	}
	_ = os.RemoveAll(path)
	return true
}

type versionBinding struct {
	version         string
	previousCurrent string
}

// boundBindings énumère les versions d'un module liées par `bind:socle` :
// celles dont le marqueur désigne bien ce module (un marqueur étranger ou
// illisible est ignoré).
func boundBindings(moduleRoot, moduleDir string) []versionBinding {
	entries, err := os.ReadDir(moduleRoot)
	if err != nil {
		return nil
	}
	var bindings []versionBinding
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(moduleRoot, entry.Name(), BindMarkerFile))
		if err != nil {
			continue
		}
		var marker bindMarker
		if json.Unmarshal(data, &marker) != nil {
			continue
		}
		resolved, err := filepath.Abs(marker.ModuleDir)
		if err != nil || resolved != moduleDir {
			continue
		}
		bindings = append(bindings, versionBinding{
			version:         entry.Name(),
			previousCurrent: strings.TrimSpace(marker.PreviousCurrent),
		})
	}
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].version < bindings[j].version })
	return bindings
}

// realCurrentPointer remonte au pointeur d'installation réel : si value
// désigne une version liée par un bind antérieur, suit la chaîne de
// `previousCurrent` jusqu'à la première version non liée (borné, pour ne
// jamais boucler).
func realCurrentPointer(moduleRoot, value string) string {
	seen := map[string]bool{}
	current := value
	for current != "" && !seen[current] {
		seen[current] = true
		data, err := os.ReadFile(filepath.Join(moduleRoot, current, BindMarkerFile))
		if err != nil {
			break
		}
		var marker bindMarker
		if json.Unmarshal(data, &marker) != nil || marker.PreviousCurrent == "" {
			break
		}
		current = strings.TrimSpace(marker.PreviousCurrent)
	}
	return current
}

// removeOwnedLinks nettoie les liens posés pour une entrée sans marqueur.
func removeOwnedLinks(moduleRoot, version string) bool {
	touched := false
	currentPath := filepath.Join(moduleRoot, "current")
	if pathExists(currentPath) {
		_ = os.Remove(currentPath)
		touched = true
	}
	versionDir := filepath.Join(moduleRoot, version)
	for _, candidate := range []string{
		filepath.Join(versionDir, "manifest.json"),
		filepath.Join(versionDir, "artifact"),
	} {
		if info, err := os.Lstat(candidate); err == nil && info.Mode()&os.ModeSymlink != 0 {
			_ = os.Remove(candidate)
			touched = true
		}
	}
	_ = os.Remove(versionDir)
	_ = os.Remove(moduleRoot)
	return touched
}

// ensureDevURL inscrit NEXT_PUBLIC_DEV_MODULES_URL si absente ou vide, et
// corrige son schéma si elle ne correspond plus au socle : une valeur
// conservée malgré un socle passé en HTTPS produirait une iframe muette
// (mixed content), ce qui est pire qu'un réglage réécrit. Un hôte
// personnalisé (`host.docker.internal`, IP LAN) est en revanche préservé :
// c'est un choix d'infrastructure.
func ensureDevURL(envPath, devURL string) string {
	lines := readEnvLines(envPath)
	index := indexOfEnvKey(lines, DevModulesURLKey)
	current := ""
	if index >= 0 {
		current = strings.TrimSpace(envValue(lines[index], DevModulesURLKey))
	}
	if current == "" {
		entry := DevModulesURLKey + "=" + devURL
		if index >= 0 {
			lines[index] = entry
		} else {
			lines = append(lines, entry)
		}
		writeEnvLines(envPath, lines)
		return devURL
	}
	// Un socle HTTPS ne charge jamais une iframe HTTP : le schéma est imposé
	// quel que soit le réglage antérieur. L'hôte, lui, est un choix
	// d'infrastructure (docker, IP LAN) — il n'est jamais réécrit.
	aligned := alignDevURLScheme(current, devURL)
	if aligned == current {
		return current
	}
	lines[index] = DevModulesURLKey + "=" + aligned
	writeEnvLines(envPath, lines)
	return aligned
}

// alignDevURLScheme règle le schéma d'une URL de dev-server déjà écrite :
// HTTPS dès que le socle est en HTTPS, sinon on ne touche à rien (une iframe
// HTTPS dans une page HTTP est parfaitement valide).
func alignDevURLScheme(current, devURL string) string {
	socleScheme := schemeOf(devURL)
	if schemeOf(current) == socleScheme || socleScheme != "https" {
		return current
	}
	scheme := schemeOf(current)
	if scheme == "" {
		return current
	}
	return socleScheme + "://" + strings.TrimPrefix(current, scheme+"://")
}

// ensureEnvKey inscrit une clé de transport manquante dans le `.env.local` du
// socle. Une valeur déjà présente est toujours préservée : elle peut venir
// d'un reverse proxy ou d'un docker-compose, que la CLI ne doit pas écraser.
func ensureEnvKey(envPath, key, value string) {
	lines := readEnvLines(envPath)
	index := indexOfEnvKey(lines, key)
	if index >= 0 && strings.TrimSpace(envValue(lines[index], key)) != "" {
		return
	}
	entry := key + "=" + value
	if index >= 0 {
		lines[index] = entry
	} else {
		lines = append(lines, entry)
	}
	writeEnvLines(envPath, lines)
}

// addDevModule ajoute l'identifiant à NEXT_PUBLIC_DEV_MODULES (idempotent,
// respecte `*`).
func addDevModule(envPath, identifier string) {
	lines := readEnvLines(envPath)
	index := indexOfEnvKey(lines, DevModulesIDsKey)
	value := ""
	if index >= 0 {
		value = envValue(lines[index], DevModulesIDsKey)
	}
	ids := splitCSV(value)
	for _, id := range ids {
		if id == "*" || id == identifier {
			return
		}
	}
	ids = append(ids, identifier)
	entry := DevModulesIDsKey + "=" + strings.Join(ids, ",")
	if index >= 0 {
		lines[index] = entry
	} else {
		lines = append(lines, entry)
	}
	writeEnvLines(envPath, lines)
}

// removeDevModule retire l'identifiant de NEXT_PUBLIC_DEV_MODULES ; vrai si
// le fichier a changé. La liste devenue vide retire aussi l'URL de dev.
func removeDevModule(envPath, identifier string) bool {
	if !pathExists(envPath) {
		return false
	}
	lines := readEnvLines(envPath)
	index := indexOfEnvKey(lines, DevModulesIDsKey)
	if index < 0 {
		return false
	}
	ids := splitCSV(envValue(lines[index], DevModulesIDsKey))
	filtered := ids[:0]
	for _, id := range ids {
		if id != identifier {
			filtered = append(filtered, id)
		}
	}
	if len(filtered) == 0 {
		lines = append(lines[:index], lines[index+1:]...)
		if urlIndex := indexOfEnvKey(lines, DevModulesURLKey); urlIndex >= 0 {
			lines = append(lines[:urlIndex], lines[urlIndex+1:]...)
		}
	} else {
		lines[index] = DevModulesIDsKey + "=" + strings.Join(filtered, ",")
	}
	writeEnvLines(envPath, lines)
	return true
}

// readEnvLines lit un fichier `.env*` ligne à ligne, sans interpréter les
// valeurs (ni quotes, ni interpolation) : seules des URL et des ports sont
// écrits ici, jamais un secret.
func readEnvLines(envPath string) []string {
	content, err := os.ReadFile(envPath)
	if err != nil {
		return nil
	}
	text := strings.TrimRight(string(content), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func writeEnvLines(envPath string, lines []string) {
	if len(lines) == 0 {
		_ = os.WriteFile(envPath, nil, 0o644)
		return
	}
	_ = os.WriteFile(envPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func indexOfEnvKey(lines []string, key string) int {
	for index, line := range lines {
		if strings.HasPrefix(line, key+"=") {
			return index
		}
	}
	return -1
}

func envValue(line, key string) string {
	return strings.TrimPrefix(line, key+"=")
}

func splitCSV(value string) []string {
	var ids []string
	for _, candidate := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			ids = append(ids, trimmed)
		}
	}
	return ids
}

func schemeOf(url string) string {
	switch {
	case strings.HasPrefix(url, "https://"):
		return "https"
	case strings.HasPrefix(url, "http://"):
		return "http"
	default:
		return ""
	}
}

func readFileIfExists(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func contains(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
