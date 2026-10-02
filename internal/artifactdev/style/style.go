// Package style porte le pipeline de style d'un module Liora : chaque moteur
// (tailwindcss, unocss, …) est un adaptateur derrière un contrat unique, en
// développement (dev-server, watch) comme en production (`artifact build`,
// pack). La configuration vit dans `artifact.config.json` — une question de
// build, pas une déclaration signée du manifeste :
//
//	{ "style": { "engine": "tailwind", "entry": "styles/tailwind.css" } }
//
// Le moteur `native` (défaut) ne lance rien : esbuild traite les imports CSS
// du bundle et produit le CSS frère du bundle (`module.css`) ; l'injection du
// `<link>` dans le document hôte est du ressort d'artifactdev, commune à tous
// les moteurs. Les moteurs outillés par Node (tailwind, unocss) sont
// fail-closed : outil absent ou en échec remonte comme erreur de build, jamais
// un CSS périmé servi en silence.
package style

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Options est la configuration de style d'un module (bloc `style` de
// `artifact.config.json`).
type Options struct {
	// Engine est le nom du moteur : `native` (défaut), `tailwind`, `unocss`.
	Engine string `json:"engine"`
	// Entry est le fichier CSS d'entrée du moteur (relatif à la racine du
	// module) — ignoré par `native`, requis par les moteurs outillés.
	Entry string `json:"entry"`
	// Options sont les réglages additionnels propres au moteur.
	Options map[string]any `json:"options"`
}

// Context passe à un moteur les chemins effectifs du build. Les champs
// miroient la Config d'artifactdev, que ce package ne peut pas importer
// (cycle) : le contrat reste minimal et stable.
type Context struct {
	// ModuleDir est la racine du module.
	ModuleDir string
	// ArtifactDir est le répertoire de sortie (.liorian/artifact).
	ArtifactDir string
	// OutCSS est le nom du fichier CSS attendu, frère du bundle (module.css).
	OutCSS string
	// Log rapporte la progression (dev-server comme build).
	Log func(string)
}

// Engine est le contrat d'un moteur de style : produire le CSS du module dans
// le répertoire d'artefact, avant le rendu du document hôte.
type Engine interface {
	// Name rend le nom déclaré dans `style.engine`.
	Name() string
	// Build produit `<ArtifactDir>/<OutCSS>`. Un moteur sans travail (native)
	// ne fait rien — esbuild a déjà traité les imports CSS du bundle.
	Build(ctx Context, options Options) error
}

// Load lit le bloc `style` de `artifact.config.json`. Fichier absent ou bloc
// absent → options natives par défaut, sans erreur.
func Load(moduleDir string) (Options, error) {
	path := filepath.Join(moduleDir, "artifact.config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Options{}, nil
		}
		return Options{}, fmt.Errorf("style: artifact.config.json illisible : %v", err)
	}
	var config struct {
		Style *Options `json:"style"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return Options{}, fmt.Errorf("style: artifact.config.json illisible : %v", err)
	}
	if config.Style == nil {
		return Options{}, nil
	}
	return *config.Style, nil
}

// Resolve rend le moteur nommé par les options. Le défaut est `native` ; un
// moteur inconnu est une erreur fail-closed (le build ne doit pas continuer
// avec un pipeline que le développeur n'a pas demandé).
func Resolve(options Options) (Engine, error) {
	name := strings.TrimSpace(options.Engine)
	if name == "" {
		name = NativeName
	}
	engine, ok := engines[name]
	if !ok {
		return nil, fmt.Errorf("style: moteur inconnu %q — moteurs disponibles : %s",
			name, strings.Join(availableEngines(), ", "))
	}
	return engine, nil
}

// NativeName est le moteur par défaut : esbuild seul, zéro outillage.
const NativeName = "native"

// engines est le registre des moteurs — un moteur s'ajoute en une entrée.
// Les alias (shadcn, mui) sont des pipelines existants sous un nom connu :
// shadcn = tailwind + variables CSS dans le fichier d'entrée ; mui =
// CSS-in-JS runtime, zéro étape de build.
var engines = map[string]Engine{
	NativeName: nativeEngine{},
	"tailwind": tailwindEngine{},
	"shadcn":   tailwindEngine{},
	"unocss":   unocssEngine{},
	"mui":      nativeEngine{},
}

// RequiresCSS rapporte si le moteur déclaré doit produire `module.css` —
// utilisé par le pack pour refuser une archive sans le CSS promis. Les
// moteurs natives (native, mui) ne promettent rien au-delà des imports CSS
// d'esbuild ; un moteur inconnu est traité comme exigeant (fail-closed).
func RequiresCSS(options Options) bool {
	engine, err := Resolve(options)
	if err != nil {
		return true
	}
	_, isNative := engine.(nativeEngine)
	return !isNative
}

func availableEngines() []string {
	names := make([]string, 0, len(engines))
	for name := range engines {
		names = append(names, name)
	}
	return names
}

// nativeEngine est le moteur par défaut : les imports CSS du bundle sont
// traités par esbuild (CSS frère du bundle), rien à exécuter.
type nativeEngine struct{}

func (nativeEngine) Name() string { return NativeName }

func (nativeEngine) Build(Context, Options) error { return nil }
