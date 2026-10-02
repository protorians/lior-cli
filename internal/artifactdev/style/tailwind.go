// Moteur tailwind (tailwindcss v4) : la CLI officielle `@tailwindcss/cli`
// compile le CSS d'entrée du module vers `module.css`. La détection de
// contenu de v4 est automatique depuis la racine du module ; la source du SDK
// y est ajoutée par une entrée `@source` générée — les classes utilitaires
// des composants katon du SDK doivent être compilées dans le bundle du
// module qui les emploie.
//
// Le preset shadcn partage ce moteur : shadcn/ui est du tailwind plus des
// variables CSS déclarées dans le fichier d'entrée — aucun pipeline
// supplémentaire.
package style

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	tailwindPackage = "@tailwindcss/cli"
	tailwindBinary  = "tailwindcss"
)

type tailwindEngine struct{}

func (tailwindEngine) Name() string { return "tailwind" }

func (tailwindEngine) Build(ctx Context, options Options) error {
	entry := strings.TrimSpace(options.Entry)
	if entry == "" {
		return fmt.Errorf(
			"style: le moteur tailwind exige `style.entry` dans artifact.config.json — " +
				"le chemin du CSS d'entrée (ex. \"styles/tailwind.css\", avec `@import \"tailwindcss\"`)")
	}
	entryPath := entry
	if !filepath.IsAbs(entryPath) {
		entryPath = filepath.Join(ctx.ModuleDir, filepath.FromSlash(entry))
	}
	if _, err := os.Stat(entryPath); err != nil {
		return fmt.Errorf("style: entrée tailwind introuvable (%s)", entryPath)
	}

	// Zone de scan SDK : un CSS d'entrée temporaire ajoute la source du SDK
	// sans toucher au fichier du développeur. Le chemin `@source` est relatif
	// au CSS d'entrée (résolu par tailwind), donc recalculé depuis `.liorian/`.
	inputPath := entryPath
	if sdkSource := workspaceSDKSource(ctx.ModuleDir); sdkSource != "" {
		generated := filepath.Join(ctx.ModuleDir, ".liorian", "tailwind.input.css")
		relative, err := filepath.Rel(filepath.Dir(generated), sdkSource)
		if err == nil {
			source := fmt.Sprintf("\n@source \"%s\";\n", filepath.ToSlash(relative))
			if err := os.MkdirAll(filepath.Dir(generated), 0o755); err != nil {
				return err
			}
			original, err := os.ReadFile(entryPath)
			if err != nil {
				return err
			}
			if err := os.WriteFile(generated, append(original, []byte(source)...), 0o644); err != nil {
				return err
			}
			inputPath = generated
		}
	}

	outputPath := filepath.Join(ctx.ArtifactDir, ctx.OutCSS)
	args := append(ResolveNodeBinary(ctx.ModuleDir, tailwindPackage, tailwindBinary),
		"-i", inputPath, "-o", outputPath, "--minify")
	return runNodeTool(ctx, "tailwind", tailwindPackage, args)
}
