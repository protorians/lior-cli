package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/module"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/tui"
)

// createRoot returns the project root the module will be created in.
//
// With `--standalone`, the current directory becomes the root of a module
// repository of its own: the socle is a separate public repository, and a
// third-party developer must be able to start from an empty directory. Without
// the flag, the existing project root is required — a module created inside an
// application that was not asked for would land in the wrong tree.
func createRoot() (string, error) {
	if !createStandalone {
		return requireProjectRoot()
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", pkg.NewError(i18n.T("cat.project"), i18n.T("modules.error.cwd"), pkg.ExitError)
	}
	// A standalone repository inside another project would shadow it for every
	// subsequent command: the walk-up from `modules/<id>/` stops at the
	// nearest root, and the developer would publish from the wrong tree.
	if root, err := config.FindProjectRoot(cwd); err == nil && !isStandaloneRoot(root) {
		return "", pkg.NewErrorWithFix(
			i18n.T("cat.project"),
			i18n.Tf("create.error.standalone_nested", root),
			i18n.T("create.error.standalone_nested.fix"),
			pkg.ExitError,
		)
	}
	return cwd, nil
}

// isStandaloneRoot reports whether root is a repository created by
// `--standalone` — a project root whose only module tree is `modules/` and
// whose `liorian.config.json` names it, as opposed to an application.
func isStandaloneRoot(root string) bool {
	if root == "" {
		return false
	}
	if pkg.DirExists(filepath.Join(root, config.InternalModulesDir)) {
		return false
	}
	if pkg.DirExists(filepath.Join(root, config.ExternalModulesDir)) {
		return false
	}
	if pkg.DirExists(filepath.Join(root, config.AppSrcDir)) {
		return false
	}
	_, err := config.FindProjectRoot(root)
	return err == nil
}

// runCreateStandalone scaffolds a module repository of its own in root: the
// repository files that make the CLI recognise it as a project, then the module
// itself under `modules/<id>/` — the same layout as a first-party module, so
// every existing command (`pack`, `sign`, `publish`, `artifact …`) works without
// a special case.
func runCreateStandalone(root string, spec module.ModuleSpec) error {
	// The scaffolded root is written before the module so that `create` leaves
	// a coherent repository behind even if the dependency installation fails —
	// a half-created repository is easier to repair than a missing one.
	if err := module.PrepareStandaloneRoot(root, spec); err != nil {
		var refusal *module.StandaloneError
		if errors.As(err, &refusal) {
			return pkg.NewErrorWithFix(
				i18n.T("cat.project"),
				refusal.Error(),
				i18n.T("create.error.standalone_not_empty.fix"),
				pkg.ExitError,
			)
		}
		return err
	}

	// `modules/` is created before the module: the creator reads the tree to
	// choose between `modules/<id>/` (source) and `library/modules/<domain>/`
	// (installation). A repository of its own develops sources, not installs.
	if err := os.MkdirAll(filepath.Join(root, config.WorkspaceModulesDir), 0o755); err != nil {
		return err
	}

	// The declared address is the runtime route, not a page segment: the socle
	// serves an isolated module under `/m/<id>`, and the manifest is what the
	// route resolver reads. A standalone module has no `src/app/` page to
	// scaffold, so its `uri` must carry the `/m/` prefix or the module would
	// declare an address the socle never mounts.
	if strings.TrimSpace(spec.URL) == "" && spec.ID != "" {
		spec.URL = strings.Trim(config.ModuleRoutePrefix, "/") + "/" + spec.ID
	}

	creator := &module.Creator{
		Root:      root,
		MockupDir: createMockup,
		// No route is scaffolded: `src/app/<url>/page.tsx` belongs to the socle,
		// which serves the module on `/m/<id>`. In a repository of its own it
		// would be a tree nothing ever builds.
		NoPage: true,
	}
	result, err := creator.Create(spec)
	if err != nil {
		return err
	}

	readme, err := module.WriteStandaloneReadme(root, spec, result.Dir)
	if err != nil {
		debugf("readme du dépôt autonome: %v", err)
	}

	depsPM := ""
	if !createSkipInstall && os.Getenv(createSkipInstallEnv) == "" {
		var installErr error
		depsPM, installErr = tui.RunWithSpinner(i18n.T("create.spinner.deps"), func() (string, error) {
			return creator.ResolveDependencies(filepath.Dir(result.Dir))
		})
		if installErr != nil {
			warn(i18n.Tf("create.warn.install", installErr.Error()))
			depsPM = ""
		} else if depsPM == "" {
			warn(i18n.T("create.warn.pm_none"))
		}
	}

	s := tui.NewStyles()
	panel := s.Success.Render(i18n.Tf("create.success.repo", root)) + "\n" +
		s.Success.Render(i18n.Tf("create.success.dir", relToRoot(root, result.Dir))) + "\n" +
		s.Success.Render(i18n.Tf("create.success.token", result.Token)) + "\n" +
		s.Success.Render(i18n.T("create.success.manifest")) + "\n" +
		s.Success.Render(i18n.T("create.success.entry"))
	if readme != "" {
		panel += "\n" + s.Success.Render(i18n.Tf("create.success.readme", relToRoot(root, readme)))
	}
	if depsPM != "" {
		panel += "\n" + s.Success.Render(i18n.Tf("create.success.deps", depsPM))
	}
	fmt.Println()
	fmt.Println(s.SuccessPanel(panel))
	// The three-terminal loop is the part a third-party developer cannot infer
	// from a template, so it is printed in full rather than summarised.
	fmt.Println(s.StepsList(i18n.T("create.standalone.next"),
		s.Info.Render("git init && git add -A && git commit -m \"module initial\""),
		s.Info.Render(fmt.Sprintf(
			"cd %s && liora artifact bind:socle ../../liorian-socle",
			relToRoot(root, result.Dir),
		)),
		s.Info.Render("cd ../../liorian-socle && bun install && bun run dev"),
		s.Info.Render(fmt.Sprintf(
			"cd %s && liora artifact dev",
			relToRoot(root, result.Dir),
		)),
	))
	fmt.Println()
	fmt.Println(s.Hint.Render(i18n.Tf("create.standalone.socle", spec.ID)))
	fmt.Println()
	return nil
}
