// Package artifactdev porte la chaîne de build et le dev-server d'un module
// Liora (spec docs/specs/applications/module-isolated-runtime.md, §4.4 et §8).
//
// Le portage est natif : le bundler est esbuild — écrit en Go — via son API
// Go, le dev-server est un serveur HTTP(S) de la bibliothèque standard qui
// sert `.liorian/artifact/` (D7) et pousse le rechargement du document hôte
// par SSE. La CLI `liora artifact` n'a donc plus aucune dépendance Node :
// tout le cycle de développement vit dans le binaire.
package artifactdev

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Défauts canoniques du runtime isolé (D6, D7).
const (
	DefaultArtifactDir = ".liorian/artifact"
	DefaultEntry       = "main.tsx"
	DefaultBundle      = "module.js"
	DefaultDocument    = "index.html"
	// DefaultDevPort est le port canonique du dev-server de module.
	DefaultDevPort = 5178
	// PortFallbackAttempts est le nombre de ports essayés au-delà du port
	// demandé quand celui-ci est occupé (EADDRINUSE). Le port 5178 survit
	// souvent à un terminal fermé sans SIGINT : un process orphelin le tient
	// sur [::1] pendant que rien n'écoute en IPv4 — démarrer sur le port
	// suivant vaut mieux qu'un échec brutal.
	PortFallbackAttempts = 20
	// WrapperName est le fichier d'amorçage généré à côté de l'entrée.
	WrapperName = ".bootstrap-entry.ts"
	// BundleCSS est le nom du CSS produit à côté du bundle : `EntryNames` vaut
	// toujours « module », esbuild nomme donc le CSS des imports `module.css`
	// quel que soit `manifest.artifact.bundle`. Tout le pipeline de style
	// (moteurs, injection du document hôte, service, pack) s'y réfère.
	BundleCSS = "module.css"
)

// Manifest est la projection du manifeste de module consommée par la chaîne
// de build. Le manifeste reste la déclaration canonique (schéma complet :
// packages/sdk/schemas/module.schema.json) — seuls les champs utiles au build
// sont lus ici, pour ne jamais rejeter un manifeste valide que la projection
// Go ne modéliserait pas.
type Manifest struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Domain  string `json:"domain"`
	Entry   string `json:"entry"`
	// Router porte la déclaration du routeur fichier (§8.5) : `mode` vaut
	// `auto` (synchro manifeste) ou `manual` (désactivé) ; `dir` surcharge
	// l'arbre de routes conventionnel (`presentation/routes`).
	Router *struct {
		Mode string `json:"mode"`
		Dir  string `json:"dir"`
	} `json:"router"`
	Artifact *struct {
		Dir      string `json:"dir"`
		Bundle   string `json:"bundle"`
		Document string `json:"document"`
	} `json:"artifact"`
	Extra map[string]any `json:"-"`
}

// LoadManifest lit le manifeste du module (source de vérité, racine du
// module) et conserve les champs inconnus pour le templating du document hôte.
func LoadManifest(moduleDir string) (*Manifest, error) {
	path := filepath.Join(moduleDir, "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("artifact: manifest.json introuvable à la racine du module (%s)", path)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("artifact: manifest.json illisible : %v", err)
	}
	if err := json.Unmarshal(data, &manifest.Extra); err != nil {
		manifest.Extra = map[string]any{}
	}
	return &manifest, nil
}

// BuildOptions spécialise la chaîne de build d'un module.
type BuildOptions struct {
	// ModuleDir est la racine du module (défaut : répertoire courant).
	ModuleDir string
	// Entry est l'entrée TypeScript (défaut : manifest.entry, sinon main.tsx).
	Entry string
	// ArtifactDir est le répertoire de sortie (défaut : .liorian/artifact).
	ArtifactDir string
	// Bundle est le nom du bundle produit (défaut : manifest.artifact.bundle,
	// sinon module.js).
	Bundle string
	// Document est le nom du document hôte (défaut : manifest.artifact.document,
	// sinon index.html).
	Document string
	// Minify désactive la minification quand faux (défaut : actif).
	Minify *bool
	// Sourcemap désactive la sourcemap externe quand faux (défaut : actif).
	Sourcemap *bool
	// Dev porte les réglages du dev-server.
	Dev DevOptions
}

// DevOptions porte les réglages du dev-server (`liora artifact dev`).
type DevOptions struct {
	Port  int
	Host  string
	HTTPS *bool
	Cert  string
	Key   string
	// StrictPort refuse de basculer sur le port suivant quand le port demandé
	// est occupé (défaut : bascule automatique, message explicite).
	StrictPort bool
}

// Config est la configuration effective, chemins absolutisés.
type Config struct {
	ModuleDir    string
	Entry        string
	EntryPath    string
	ArtifactDir  string
	Bundle       string
	Document     string
	HTMLTemplate string
	// RoutesDir est l'arbre de routes du routeur fichier (§8.5), absolutisé —
	// vide en routage manuel (aucun dossier, ou `router.mode: "manual"`).
	RoutesDir string
	Minify    bool
	Sourcemap bool
	Dev       DevOptions
}

// Resolve charge le manifeste et calcule la configuration effective.
func Resolve(options BuildOptions) (*Manifest, *Config, error) {
	moduleDir := options.ModuleDir
	if moduleDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, nil, err
		}
		moduleDir = cwd
	}
	moduleDir, err := filepath.Abs(moduleDir)
	if err != nil {
		return nil, nil, err
	}
	manifest, err := LoadManifest(moduleDir)
	if err != nil {
		return nil, nil, err
	}

	entry := firstNonEmpty(options.Entry, manifest.Entry, DefaultEntry)
	entryPath := filepath.Join(moduleDir, entry)
	if !fileExists(entryPath) {
		return nil, nil, fmt.Errorf("artifact: entrée introuvable (%s) — vérifie manifest.entry", entryPath)
	}

	// Le layout de développement est canonique (`.liorian/artifact/`, D7) :
	// `manifest.artifact.dir` décrit le répertoire de distribution dans
	// l'arbre installé, jamais la sortie du build.
	artifactDir := joinIfRelative(moduleDir, firstNonEmpty(options.ArtifactDir, DefaultArtifactDir))
	bundle := firstNonEmpty(options.Bundle, manifest.ArtifactBundle(), DefaultBundle)
	document := firstNonEmpty(options.Document, manifest.ArtifactDocument(), DefaultDocument)

	// Router fichier (§8.5) : actif si `router.mode` n'est pas `manual` et
	// qu'un arbre de routes existe — celui de `router.dir`, sinon l'arbre
	// conventionnel. Un dossier de routes suffit à activer la génération :
	// le développeur qui crée `presentation/routes/` opte pour le router.
	routesDir := ""
	if manifest.Router == nil || manifest.Router.Mode != "manual" {
		dir := DefaultRoutesDir
		if manifest.Router != nil && strings.TrimSpace(manifest.Router.Dir) != "" {
			dir = manifest.Router.Dir
		}
		if candidate := filepath.Join(moduleDir, filepath.FromSlash(dir)); isDir(candidate) {
			routesDir = candidate
		}
	}

	minify := options.Minify == nil || *options.Minify
	sourcemap := options.Sourcemap == nil || *options.Sourcemap

	if options.Dev.Port <= 0 {
		options.Dev.Port = envPort()
	}
	if options.Dev.Port <= 0 {
		options.Dev.Port = DefaultDevPort
	}
	if strings.TrimSpace(options.Dev.Host) == "" {
		options.Dev.Host = strings.TrimSpace(os.Getenv("LIORIAN_DEV_HOST"))
		if options.Dev.Host == "" {
			options.Dev.Host = "localhost"
		}
	}
	if options.Dev.Cert == "" {
		options.Dev.Cert = os.Getenv("LIORIAN_DEV_TLS_CERT")
	}
	if options.Dev.Key == "" {
		options.Dev.Key = os.Getenv("LIORIAN_DEV_TLS_KEY")
	}

	return manifest, &Config{
		ModuleDir:    moduleDir,
		Entry:        entry,
		EntryPath:    entryPath,
		ArtifactDir:  artifactDir,
		Bundle:       bundle,
		Document:     document,
		HTMLTemplate: filepath.Join(moduleDir, document),
		RoutesDir:    routesDir,
		Minify:       minify,
		Sourcemap:    sourcemap,
		Dev:          options.Dev,
	}, nil
}

// ArtifactDir rend le champ optionnel manifest.artifact.dir.
func (m *Manifest) ArtifactDir() string {
	if m.Artifact == nil {
		return ""
	}
	return m.Artifact.Dir
}

// ArtifactBundle rend le champ optionnel manifest.artifact.bundle.
func (m *Manifest) ArtifactBundle() string {
	if m.Artifact == nil {
		return ""
	}
	return m.Artifact.Bundle
}

// ArtifactDocument rend le champ optionnel manifest.artifact.document.
func (m *Manifest) ArtifactDocument() string {
	if m.Artifact == nil {
		return ""
	}
	return m.Artifact.Document
}

// envPort lit LIORIAN_DEV_PORT ; 0 quand absent ou invalide.
func envPort() int {
	raw := strings.TrimSpace(os.Getenv("LIORIAN_DEV_PORT"))
	if raw == "" {
		return 0
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port <= 0 {
		return 0
	}
	return port
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func joinIfRelative(base, candidate string) string {
	if filepath.IsAbs(candidate) {
		return candidate
	}
	return filepath.Join(base, candidate)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
