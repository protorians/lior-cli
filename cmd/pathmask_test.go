// Convention `<socle>` / `<module>` : les chemins affichés dans les panneaux
// substituent le préfixe machine du socle et du module, le reste du chemin
// reste lisible.
package cmd

import (
	"path/filepath"
	"testing"
)

func TestMaskSoclePath(t *testing.T) {
	socle := "/dev/liorian-socle"
	cases := []struct {
		name, path, want string
	}{
		{"registre", "/dev/liorian-socle/.liorian/module.dev.json", "<socle>/.liorian/module.dev.json"},
		{"bibliothèque", "/dev/liorian-socle/library/modules/mod.acme.crm", "<socle>/library/modules/mod.acme.crm"},
		{"la racine du socle reste entière", "/dev/liorian-socle", "/dev/liorian-socle"},
		{"hors socle", "/dev/modules/mod.super/lib.ts", "/dev/modules/mod.super/lib.ts"},
		{"préfixe partagé sans frontière", "/dev/liorian-socle-bis/x", "/dev/liorian-socle-bis/x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := maskSoclePath(socle, c.path); got != c.want {
				t.Errorf("maskSoclePath(%s) = %q, voulu %q", c.path, got, c.want)
			}
		})
	}
}

func TestMaskModulePath(t *testing.T) {
	socle := "/dev/liorian-socle"
	module := "/dev/mod.acme.crm"
	cases := []struct {
		name, path, moduleDir, want string
	}{
		{"artefact du module", "/dev/mod.acme.crm/.liorian/artifact/bundle.mjs", module, "<module>/.liorian/artifact/bundle.mjs"},
		{"la racine du module reste entière", module, module, module},
		{"module installé dans le socle — module d'abord", socle + "/library/modules/mod.acme.crm", socle + "/library/modules/mod.acme.crm", socle + "/library/modules/mod.acme.crm"},
		{"socle sans module connu", socle + "/.liorian/module.dev.json", "", "<socle>/.liorian/module.dev.json"},
		{"hors des deux", "/dev/out/lib.ts", module, "/dev/out/lib.ts"},
		{"préfixe partagé sans frontière", module + "-bis/x", module, module + "-bis/x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := maskModulePath(socle, c.moduleDir, c.path); got != c.want {
				t.Errorf("maskModulePath(%s) = %q, voulu %q", c.path, got, c.want)
			}
		})
	}
}

func TestMaskPathTexts(t *testing.T) {
	socle := "/dev/liorian-socle"
	module := "/dev/mod.acme.crm"
	cases := []struct {
		name, text, want string
	}{
		{
			"premier socle de la liaison",
			"socle lié introuvable : /dev/liorian-socle",
			"socle lié introuvable : <socle>",
		},
		{
			"deux socles comparés restent distincts",
			"lié à /dev/liorian-socle, diagnostic sur /dev/other-socle",
			"lié à <socle>, diagnostic sur /dev/other-socle",
		},
		{
			"module installé sous le socle — chaque préfixe remplacé",
			"/dev/mod.acme.crm/.liorian/artifact · /dev/liorian-socle/library/…",
			"<module>/.liorian/artifact · <socle>/library/…",
		},
		{
			"le texte égal à la racine du socle reste entier",
			"/dev/liorian-socle",
			"/dev/liorian-socle",
		},
		{
			"le texte égal à la racine du module reste entier",
			module,
			module,
		},
		{
			"hors perimètre",
			"aucun chemin reconnu dans /dev/out",
			"aucun chemin reconnu dans /dev/out",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := maskPathTexts(c.text, socle, module); got != c.want {
				t.Errorf("maskPathTexts(%q) = %q, voulu %q", c.text, got, c.want)
			}
		})
	}
}

func TestMaskPathEmpty(t *testing.T) {
	if got := maskSoclePath("/dev/liorian-socle", ""); got != "" {
		t.Errorf("path vide : %q, voulu vide", got)
	}
	if got := maskPath("/dev/lib.ts", pathMask{dir: "", token: socleToken}); got != filepath.Clean("/dev/lib.ts") {
		t.Errorf("racine vide : %q, voulu inchangé", got)
	}
}
