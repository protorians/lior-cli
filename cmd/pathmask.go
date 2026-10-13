// Masquage des chemins machine dans les panneaux TUI : le chemin absolu est
// affiché avec le préfixe du socle remplacé par `<socle>` et le préfixe du
// module par `<module>` — seule la racine machine est masquée, la suite du
// chemin reste lisible (`<socle>/.liorian/module.dev.json`,
// `<module>/.liorian/artifact/bundle.mjs`).
package cmd

import (
	"path/filepath"
	"strings"
)

// Jetons d'affichage des racines masquées.
const (
	socleToken  = "<socle>"
	moduleToken = "<module>"
)

// pathMask couple une racine de chemin à son jeton d'affichage.
type pathMask struct {
	dir   string
	token string
}

// maskPath substitue au préfixe du chemin affiché le premier masque qui le
// couvre. Une racine vide ou un chemin hors de la racine est ignoré : le
// chemin est alors rendu tel quel. Un chemin exactement égal à la racine
// reste entier : `<socle>` (ou `<module>`) seul ne dit rien de l'emplacement
// machine du socle ou du module. L'appelant ordonne les masques du plus
// profond au plus large (un module installé dans le socle s'affiche par son
// propre jeton, le reste du socle par le sien).
func maskPath(path string, masks ...pathMask) string {
	if strings.TrimSpace(path) == "" {
		return path
	}
	for _, mask := range masks {
		if strings.TrimSpace(mask.dir) == "" {
			continue
		}
		rel, err := filepath.Rel(mask.dir, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		// Le chemin de la racine elle-même reste entier : `<socle>` seul ne
		// dit rien de l'emplacement machine du socle.
		if rel == "." {
			return path
		}
		return mask.token + string(filepath.Separator) + rel
	}
	return path
}

// maskSoclePath rend un chemin du socle avec son préfixe masqué.
func maskSoclePath(socleDir, path string) string {
	return maskPath(path, pathMask{dir: socleDir, token: socleToken})
}

// maskModulePath rend un chemin affiché en masquant d'abord le module (le
// préfixe le plus profond), puis le socle — le registre et l'installation
// bibliothèque relèvent du socle, les artefacts du module. Un chemin ni sous
// le module ni sous le socle est rendu tel quel.
func maskModulePath(socleDir, moduleDir, path string) string {
	return maskPath(path,
		pathMask{dir: moduleDir, token: moduleToken},
		pathMask{dir: socleDir, token: socleToken})
}

// maskPathTexts remplace, dans des textes déjà formatés (détails i18n, titre),
// chaque occurrence du module puis du socle par son jeton — les rapports
// sérialisés gardent les chemins bruts, seul l'écran est masqué. Le module est
// traité en premier : une installation vécue à la fois sous le socle
// (`library/modules/…`) et sous le module s'affiche par son jeton le plus
// profond. Un socle distinct mais textuellement voisin (`liorian-socle-bis`)
// n'est pas masqué : la comparaison reste lisible.
func maskPathTexts(text, socleDir, moduleDir string) string {
	for _, bound := range []pathMask{
		{moduleDir, moduleToken},
		{socleDir, socleToken},
	} {
		if strings.TrimSpace(bound.dir) == "" {
			continue
		}
		// Un texte qui n'est que la racine reste entier : `<socle>` (ou
		// `<module>`) seul ne dit rien de l'emplacement machine.
		if text == bound.dir {
			return text
		}
		text = strings.ReplaceAll(text, bound.dir, bound.token)
	}
	return text
}
