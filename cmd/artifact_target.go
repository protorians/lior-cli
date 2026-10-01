// Résolution du module cible des commandes `liora artifact <action>` : le
// module est celui désigné par un argument (un dossier portant un
// manifest.json), à défaut celui qui contient le répertoire courant, à
// défaut celui sélectionné à la racine du projet.
package cmd

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
)

// moduleDirFromCwd remonte depuis cwd jusqu'à la racine du projet à la
// recherche d'un module : arbre source (`modules/<id>`, D5) ou arbre
// d'installation (`library/modules/<id>`, D11).
func moduleDirFromCwd(root, cwd string) string {
	if root == "" || cwd == "" {
		return ""
	}
	for dir := cwd; isInsideRoot(root, dir); dir = filepath.Dir(dir) {
		if isModuleDirUnderRoot(root, dir) {
			return dir
		}
	}
	return ""
}

// moduleDirFromArg renvoie le dossier de module que les arguments désignent :
// le premier argument nommant un dossier existant qui porte un manifest.json.
// Chaîne vide quand aucun n'en porte.
func moduleDirFromArg(args []string) string {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if !pkg.DirExists(arg) || !pkg.FileExists(filepath.Join(arg, config.ManifestFileName)) {
			continue
		}
		if abs, err := filepath.Abs(arg); err == nil {
			return abs
		}
		return arg
	}
	return ""
}

// isInsideRoot rapporte si dir est root ou vit sous root.
func isInsideRoot(root, dir string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absDir)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// isModuleDirUnderRoot rapporte si dir est un module du projet root : arbre
// source (`modules/<id>`, D5) ou installation (`library/modules/<id>` et ses
// dossiers de version, D11). Un dossier hors du projet, et la racine elle-même,
// ne sont jamais des modules.
func isModuleDirUnderRoot(root, dir string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absDir)
	if err != nil || rel == "." {
		return false
	}
	if !pkg.FileExists(filepath.Join(absDir, config.ManifestFileName)) {
		return false
	}
	segments := strings.Split(filepath.ToSlash(rel), "/")
	// modules/<id>
	if len(segments) == 2 && segments[0] == config.WorkspaceModulesDir {
		return true
	}
	// library/modules/<id>[/<version>]
	installed := strings.SplitN(config.ExternalModulesDir, "/", 2)
	if len(segments) > len(installed) && slices.Equal(segments[:len(installed)], installed) {
		return true
	}
	return false
}

// artifactModuleDir résout le module qu'une action artifact vise : celui
// nommé par les arguments, sinon celui du répertoire courant (y compris un
// dossier de module autonome désigné comme répertoire de travail direct),
// sinon le module sélectionné à la racine.
func artifactModuleDir(root string, args []string) (string, error) {
	if dir := moduleDirFromArg(args); dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err == nil {
		if dir := moduleDirFromCwd(root, cwd); dir != "" {
			return dir, nil
		}
		// Un dépôt autonome est sa propre racine : son module vit sous
		// `modules/<id>`, déjà couvert ci-dessus. Un répertoire courant qui
		// porte un manifest.json est traité comme le module lui-même.
		if pkg.FileExists(filepath.Join(cwd, config.ManifestFileName)) {
			return cwd, nil
		}
	}
	name, err := resolveModule(root, nil)
	if err != nil {
		return "", err
	}
	dir := config.ResolveModuleDir(root, name)
	if dir == "" {
		return "", pkg.NewError(i18n.T("cat.module"),
			i18n.Tf("artifact.error.module_absent", name), pkg.ExitModuleNotFound)
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs, nil
	}
	return dir, nil
}

// displayPath renders target relative to base when it reads better in a
// command line (it stays inside the project), else absolute.
func displayPath(base, target string) string {
	if rel, err := filepath.Rel(base, target); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return target
}
