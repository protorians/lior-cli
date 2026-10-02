// Moteur unocss : la CLI officielle `@unocss/cli` génère les utilitaires à
// partir d'un glob de sources, vers `module.css`. La configuration unocss du
// module (`uno.config.ts`) est chargée par l'outil lui-même. La source du
// SDK est ajoutée au glob — les classes utilitaires des composants katon du
// SDK doivent être compilées dans le bundle du module qui les emploie.
package style

import (
	"path/filepath"
)

const (
	unocssPackage = "@unocss/cli"
	unocssBinary  = "unocss"
)

type unocssEngine struct{}

func (unocssEngine) Name() string { return "unocss" }

func (unocssEngine) Build(ctx Context, options Options) error {
	_ = options
	outputPath := filepath.Join(ctx.ArtifactDir, ctx.OutCSS)
	patterns := []string{filepath.ToSlash(filepath.Join(ctx.ModuleDir, "**", "*.{tsx,ts,jsx,js,html}"))}
	if sdkSource := workspaceSDKSource(ctx.ModuleDir); sdkSource != "" {
		patterns = append(patterns, filepath.ToSlash(filepath.Join(sdkSource, "**", "*.{tsx,ts}")))
	}
	args := append(ResolveNodeBinary(ctx.ModuleDir, unocssPackage, unocssBinary), patterns...)
	args = append(args, "-o", outputPath, "--minify")
	return runNodeTool(ctx, "unocss", unocssPackage, args)
}
