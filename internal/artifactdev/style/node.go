// Outillage Node des moteurs de style. La CLI reste native (esbuild API Go),
// mais les moteurs tailwind/unocss n'existent qu'en outillage Node : ils sont
// exécutés comme outils — binaire résolu dans `node_modules/.bin` (du module,
// puis de chaque parent jusqu'à la racine du workspace), repli `npx
// --no-install` qui échoue plutôt que de télécharger à l'insu du développeur.
// Tout échec est fail-closed : une erreur de moteur est une erreur de build,
// jamais un CSS périmé servi en silence.
package style

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// execCommand est le point d'exécution des outils Node — une variable pour
// les tests (fake runner), une invocation réelle sinon.
var execCommand = exec.Command

// ResolveNodeBinary résout la commande d'un outil Node : le binaire
// `node_modules/.bin/<binary>` le plus proche du module, sinon
// `npx --no-install <package>` (échec explicite si absent — pas de
// téléchargement inattendu).
func ResolveNodeBinary(moduleDir, packageName, binary string) []string {
	dir := moduleDir
	for {
		candidate := filepath.Join(dir, "node_modules", ".bin", binary)
		if isExecutableFile(candidate) {
			return []string{candidate}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return []string{"npx", "--no-install", packageName}
}

// runNodeTool exécute un outil Node depuis la racine du module et rapporte
// les sorties au log. Un code de sortie non nul remonte en erreur avec le
// tail de stderr — la matière dont un développeur a besoin pour corriger.
func runNodeTool(ctx Context, name string, installHint string, args []string) error {
	if ctx.Log != nil {
		ctx.Log(fmt.Sprintf("style: moteur %s — %s", name, strings.Join(args, " ")))
	}
	output := bytes.NewBuffer(nil)
	runner := execCommand(args[0], args[1:]...)
	runner.Dir = ctx.ModuleDir
	runner.Stdout = output
	runner.Stderr = output
	if err := runner.Run(); err != nil {
		stderr := output.String()
		if len(stderr) > 2000 {
			stderr = stderr[len(stderr)-2000:]
		}
		return fmt.Errorf(
			"style: moteur %s en échec — %v\n%s\n\nInstalle l'outil dans le module (npm/bun install -D %s) "+
				"ou à la racine du workspace, puis relance.",
			name, err, strings.TrimSpace(stderr), installHint)
	}
	if ctx.Log != nil && output.Len() > 0 {
		ctx.Log(strings.TrimRight(output.String(), "\n"))
	}
	return nil
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

// workspaceSDKSource relève la source du SDK `@liorian/sdk` quand le module
// vit dans un workspace Liorian (`packages/sdk/src`) — chemin absolu, chaîne
// vide hors workspace. Les moteurs de style y ajoutent leur zone de scan :
// les classes utilitaires des composants katon du SDK doivent être compilées
// dans le bundle du module qui les emploie.
func workspaceSDKSource(moduleDir string) string {
	dir := moduleDir
	for {
		candidate := filepath.Join(dir, "packages", "sdk", "src")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
