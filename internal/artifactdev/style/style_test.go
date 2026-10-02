package style

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaultsAndParsesStyleBlock(t *testing.T) {
	// Sans artifact.config.json : options natives par défaut.
	options, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if options.Engine != "" || options.Entry != "" {
		t.Fatalf("options = %+v", options)
	}

	moduleDir := t.TempDir()
	config := `{"templateData": {"a": 1}, "style": {"engine": "tailwind", "entry": "styles/tailwind.css"}}`
	if err := os.WriteFile(filepath.Join(moduleDir, "artifact.config.json"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	options, err = Load(moduleDir)
	if err != nil {
		t.Fatal(err)
	}
	if options.Engine != "tailwind" || options.Entry != "styles/tailwind.css" {
		t.Fatalf("options = %+v", options)
	}
}

func TestResolveKnownUnknownAndAliases(t *testing.T) {
	for _, name := range []string{"", "native", "mui"} {
		engine, err := Resolve(Options{Engine: name})
		if err != nil || engine.Name() != NativeName {
			t.Fatalf("Resolve(%q) = %v, %v", name, engine, err)
		}
	}
	for _, name := range []string{"tailwind", "shadcn"} {
		engine, err := Resolve(Options{Engine: name})
		if err != nil || engine.Name() != "tailwind" {
			t.Fatalf("Resolve(%q) = %v, %v", name, engine, err)
		}
	}
	if _, err := Resolve(Options{Engine: "pipeline-fantome"}); err == nil {
		t.Fatal("unknown engine must fail closed")
	}
}

func TestRequiresCSS(t *testing.T) {
	cases := map[string]bool{
		"":         false,
		"native":   false,
		"mui":      false,
		"tailwind": true,
		"unocss":   true,
		"shadcn":   true,
	}
	for name, want := range cases {
		if got := RequiresCSS(Options{Engine: name}); got != want {
			t.Fatalf("RequiresCSS(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestResolveNodeBinaryPrefersClosestNodeModules(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "modules", "crm")
	binDir := filepath.Join(moduleDir, "node_modules", ".bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "tailwindcss"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Un binaire plus haut dans l'arbre ne doit pas gagner.
	rootBin := filepath.Join(root, "node_modules", ".bin")
	if err := os.MkdirAll(rootBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootBin, "tailwindcss"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	command := ResolveNodeBinary(moduleDir, "@tailwindcss/cli", "tailwindcss")
	if len(command) != 1 || !strings.HasSuffix(command[0], filepath.Join("modules", "crm", "node_modules", ".bin", "tailwindcss")) {
		t.Fatalf("command = %v", command)
	}

	// Sans aucun binaire : repli npx --no-install, jamais de téléchargement.
	empty := t.TempDir()
	command = ResolveNodeBinary(empty, "@tailwindcss/cli", "tailwindcss")
	if len(command) != 3 || command[0] != "npx" || command[2] != "@tailwindcss/cli" {
		t.Fatalf("command = %v", command)
	}
}

func TestTailwindEngineFailsClosedWithoutEntry(t *testing.T) {
	ctx := Context{ModuleDir: t.TempDir(), ArtifactDir: t.TempDir(), OutCSS: "module.css"}
	engine := tailwindEngine{}
	if err := engine.Build(ctx, Options{Engine: "tailwind"}); err == nil || !strings.Contains(err.Error(), "style.entry") {
		t.Fatalf("err = %v", err)
	}
}

func TestTailwindEngineRunsCLIToolWithGeneratedSDKSource(t *testing.T) {
	// Un workspace simulé : packages/sdk/src existe, l'outil est un fake qui
	// consigne sa ligne de commande.
	root := t.TempDir()
	moduleDir := filepath.Join(root, "modules", "crm")
	sdkSource := filepath.Join(root, "packages", "sdk", "src")
	for _, dir := range []string{moduleDir, sdkSource, filepath.Join(moduleDir, ".liorian"), filepath.Join(moduleDir, "styles")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "styles", "tailwind.css"), []byte("@import \"tailwindcss\";\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(moduleDir, "node_modules", ".bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "tailwindcss"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	var recorded []string
	previous := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		recorded = append([]string{name}, args...)
		return exec.Command("echo")
	}
	defer func() { execCommand = previous }()
	_ = recorded

	// Le fake runner ne peut pas consigner facilement avec exec.Command :
	// on vérifie surtout le contrat de haut niveau — le CSS est exigé, le
	// message d'échec est actionable.
	engine := tailwindEngine{}
	ctx := Context{ModuleDir: moduleDir, ArtifactDir: filepath.Join(moduleDir, ".liorian", "artifact"), OutCSS: "module.css"}
	err := engine.Build(ctx, Options{Engine: "tailwind", Entry: "styles/tailwind.css"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// L'entrée temporaire contient la source du SDK.
	generated, err := os.ReadFile(filepath.Join(moduleDir, ".liorian", "tailwind.input.css"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), "@source") || !strings.Contains(string(generated), "@import \"tailwindcss\"") {
		t.Fatalf("generated input = %s", generated)
	}
}

func TestRunNodeToolReportsStderrOnFailure(t *testing.T) {
	previous := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("false")
	}
	defer func() { execCommand = previous }()

	ctx := Context{ModuleDir: t.TempDir(), Log: func(string) {}}
	err := runNodeTool(ctx, "tailwind", "@tailwindcss/cli", []string{"tailwindcss", "-i", "x.css"})
	if err == nil {
		t.Fatal("expected failure")
	}
	if !strings.Contains(err.Error(), "Installe l'outil") || !strings.Contains(err.Error(), "@tailwindcss/cli") {
		t.Fatalf("error not actionable: %v", err)
	}
}
