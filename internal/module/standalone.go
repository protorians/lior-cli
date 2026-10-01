package module

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/pkg"
)

// Le dépôt autonome est la forme normale d'un module tiers : un dépôt git
// public, contenant un seul module, que n'importe qui peut cloner et
// construire — et que la CLI traite exactement comme un projet, parce que la
// racine du dépôt porte `liorian.config.json`.
//
// Cette forme n'exige **rien du socle** : le socle est un dépôt distinct
// (`protorians/liorian-socle`) que l'on relie au module le temps du
// développement (`artifact bind:socle`, ou `liora doctor --fix`). Confondre les
// deux — cloner le socle pour écrire un module tiers, ou commiter ses
// certificats et son `.env` — est la principale friction d'entrée du modèle
// actuel ; d'où ce mode explicite plutôt qu'un scaffolds silencieux.

const (
	// standaloneReadmeFile est le README du dépôt autonome : c'est la page
	// d'accueil du module sur sa forge, il est donc écrit en entier plutôt que
	// laissé au template du module.
	standaloneReadmeFile = "README.md"
	// standaloneIgnoreFile exclut du dépôt tout ce qui est propre à une
	// machine ou régénérable — les artefacts, les dépendances, les certificats
	// locaux et les secrets.
	standaloneIgnoreFile = ".gitignore"
)

// standaloneConfig is the minimal project manifest written at the repository
// root. It declares the project to the CLI without inventing settings the
// tooling does not read yet.
type standaloneConfig struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Private keeps a module out of any ambient discovery. Modules are
	// published explicitly (`liora publish`), never by being present.
	Private bool `json:"private"`
	// Module is the single module this repository develops.
	Module string `json:"module"`
	// Version is kept aligned with the module manifest at scaffold time.
	Version string `json:"version"`
}

// StandaloneResult summarises the creation of a standalone module repository.
type StandaloneResult struct {
	// Root is the repository root.
	Root string
	// Created lists the repository files written besides the module itself.
	Created []string
}

// StandaloneError reports a directory that cannot become a module repository —
// most often because it already holds an unrelated project, which must never be
// silently turned into one.
type StandaloneError struct {
	// Dir is the offending directory.
	Dir string
	// Reason explains, in one line, why it was refused.
	Reason string
}

func (e *StandaloneError) Error() string {
	return fmt.Sprintf("%s : %s", e.Dir, e.Reason)
}

// PrepareStandaloneRoot turns dir into the root of a module repository.
//
// The check is deliberately strict: an empty directory, or one that only holds
// files this command writes, is accepted. Anything else is refused rather than
// merged into, because a module repository has a shape that the rest of the
// tooling assumes — silently scaffolding next to an existing application would
// break `pack`, `publish` and `doctor` in ways that are hard to trace back.
func PrepareStandaloneRoot(dir string, spec ModuleSpec) error {
	if !pkg.DirExists(dir) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	} else if entries, err := os.ReadDir(dir); err != nil {
		return err
	} else {
		allowed := map[string]bool{
			standaloneReadmeFile:  true,
			standaloneIgnoreFile:  true,
			config.ConfigFileName: true,
		}
		for _, entry := range entries {
			if allowed[entry.Name()] {
				continue
			}
			if entry.Name() == ".git" || entry.Name() == ".github" {
				continue
			}
			if entry.Name() == config.WorkspaceModulesDir && entry.IsDir() && isEmptyDir(filepath.Join(dir, entry.Name())) {
				continue
			}
			return &StandaloneError{
				Dir:    dir,
				Reason: "le dossier n'est pas vide (utilisez un dossier dédié au module)",
			}
		}
	}

	contents, err := json.MarshalIndent(standaloneConfig{
		Name:        repoName(dir, spec),
		Description: spec.Description,
		Private:     true,
		Module:      config.WorkspaceModulesDir,
		Version:     spec.EffectiveVersion(),
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := pkg.WriteString(filepath.Join(dir, config.ConfigFileName), string(contents)+"\n"); err != nil {
		return err
	}
	return pkg.WriteString(filepath.Join(dir, standaloneIgnoreFile), standaloneIgnoreRules)
}

// WriteStandaloneReadme writes the repository README from the module spec. It
// documents the loop the developer actually has to follow — bind, dev, pack,
// publish — because that is the part a template cannot infer and a third-party
// developer has never seen before.
func WriteStandaloneReadme(root string, spec ModuleSpec, moduleDir string) (string, error) {
	path := filepath.Join(root, standaloneReadmeFile)
	relModule := relPath(root, moduleDir)
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", spec.AppName)
	if spec.Description != "" {
		b.WriteString(spec.Description + "\n\n")
	}
	fmt.Fprintf(&b, "Module Liora `%s` (domaine `%s`), développé dans son propre dépôt.\n\n", spec.ID, spec.Domain)

	b.WriteString("## Structure\n\n")
	fmt.Fprintf(&b, "- `%s/` — sources du module (`manifest.json`, `%s`, ...)\n", relModule, config.ModuleEntryFileName)
	b.WriteString("- `liorian.config.json` — racine du dépôt, reconnue par la CLI\n\n")

	b.WriteString("## Développement\n\n")
	b.WriteString("Le socle est un dépôt distinct ; on ne le clone pas pour écrire un module.\n")
	b.WriteString("On le lie au module le temps du développement :\n\n")
	b.WriteString("```bash\n")
	b.WriteString("git clone https://github.com/protorians/liorian-socle\n")
	b.WriteString("liora doctor --socle ../liorian-socle --fix   # socle : .env, certificats, registre\n")
	fmt.Fprintf(&b, "cd %s && liora artifact bind:socle ../../liorian-socle\n", relModule)
	b.WriteString("cd ../liorian-socle && bun install && bun run dev\n")
	fmt.Fprintf(&b, "cd ../%s && bun install && liora artifact dev\n", relModule)
	b.WriteString("```\n\n")
	b.WriteString("Trois terminaux : le socle (`https://localhost:5010`), le serveur de bibliothèque\n")
	b.WriteString("(`https://localhost:5011`, démarré par le script `dev` du socle) et le\n")
	b.WriteString("dev-server du module. Le module est alors servi par le socle en\n")
	fmt.Fprintf(&b, "`https://localhost:5010/m/%s`, rechargé à chaque compilation.\n\n", spec.ID)

	b.WriteString("`liora doctor` diagnostique cette boucle d'un geste : registre d'installation,\n")
	b.WriteString("certificats, schémas d'URL, liaison. `--fix` corrige ce qui peut l'être sans\n")
	b.WriteString("risque.\n\n")

	b.WriteString("## Publication\n\n")
	b.WriteString("```bash\n")
	b.WriteString("liora test && liora typecheck\n")
	b.WriteString("liora audit\n")
	b.WriteString("liora connect   # authentification auprès du store\n")
	b.WriteString("liora sign      # signature de l'artefact (clé publique épinglée par le socle)\n")
	b.WriteString("liora publish\n")
	b.WriteString("```\n\n")
	b.WriteString("L'artefact est une archive `.LiorArtifactPackage` contenant `manifest.json`,\n")
	b.WriteString("`src/**` et `artifact/**` — le confinement appliqué par le socle à\n")
	b.WriteString("l'installation.\n")

	if err := pkg.WriteString(path, b.String()); err != nil {
		return "", err
	}
	return path, nil
}

// standaloneIgnoreRules excludes from the repository everything that is
// generated, machine-specific, or secret.
//
// The two entries that matter most for a module repository:
//   - `.liorian/` — build output and the development link to a socle, which
//     describes one developer's machine (`socleDir` is an absolute path) and
//     must never travel to a forge;
//   - `*.LiorArtifactPackage` — archives are reproducible from the sources,
//     their presence in history is only weight.
const standaloneIgnoreRules = "# Dépendances\n" +
	"node_modules/\n" +
	"\n" +
	"# Build de l'artefact et lien de développement vers un socle\n" +
	"# (.liorian/dev.json décrit une machine : chemin absolu, ports, certificats)\n" +
	".liorian/\n" +
	"\n" +
	"# Archives d'artefact — reproductibles depuis les sources\n" +
	"*.LiorArtifactPackage\n" +
	"*.liozip\n" +
	"*.SenMod\n" +
	"*.smp\n" +
	"\n" +
	"# Environnement local\n" +
	".env\n" +
	".env.local\n" +
	"\n" +
	"# Certificats de développement (mkcert) — jamais versionnés\n" +
	"certificates/\n" +
	"\n" +
	"# Système et éditeurs\n" +
	".DS_Store\n" +
	"Thumbs.db\n" +
	"*.log\n"

// repoName dérive le nom du dépôt de l'identifiant du module, ou du dossier
// quand aucun identifiant n'est encore connu.
func repoName(dir string, spec ModuleSpec) string {
	if name := strings.TrimSpace(spec.ID); name != "" {
		return name
	}
	return filepath.Base(dir)
}

// isEmptyDir reports whether dir holds no entry at all. A missing directory
// counts as empty.
func isEmptyDir(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true
	}
	return len(entries) == 0
}

// relPath renders path relative to root for display, falling back to the
// original path when it escapes the root.
func relPath(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return relative
}
