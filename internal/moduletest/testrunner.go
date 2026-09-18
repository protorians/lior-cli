// Package moduletest runs the tests of Sentient modules (spec §6.x, future
// scope moved to scope: `sentients test <module>`).
package moduletest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/protorians/sentient-cli/internal/config"
	"github.com/protorians/sentient-cli/internal/i18n"
	"github.com/protorians/sentient-cli/internal/module"
	"github.com/protorians/sentient-cli/internal/pkg"
	"github.com/protorians/sentient-cli/internal/runner"
	"github.com/protorians/sentient-cli/internal/tui"
)

const (
	// testTimeout caps a test run so a stuck suite cannot hang the CLI.
	testTimeout = 2 * time.Minute
	// installTimeout caps the installation of a test package.
	installTimeout = 5 * time.Minute
	// outputTail is the number of recent output lines kept for a running step.
	outputTail = 8
	// outputLogCap bounds the output retained in the final log block.
	outputLogCap = 200
)

// Step is a single reported execution step (the tui step vocabulary).
type Step = tui.Step

// TestResult holds the outcome of a test run for a single module.
type TestResult struct {
	Module string
	Status string
	Errors int
	Logs   []string
	// Command is the resolved test command (informational only).
	Command string
	// Runner is the test package used ("vitest", "builtin", "script", …) so
	// the caller can persist it in the project config.
	Runner string
	// PackageManager is the package manager used for this run, persisted as a
	// fallback when the project config does not pin one.
	PackageManager string
	// Steps is the ordered trace of the stages executed for this module.
	Steps []Step
}

// RunnerOption is one catalog entry shown to the developer when a test
// package must be chosen.
type RunnerOption struct {
	// Package is the catalog package name.
	Package string
	// Installed reports whether the package is already available.
	Installed bool
}

// RunnerChoice is the developer's test-package selection. An empty Package
// means the run should be skipped.
type RunnerChoice struct {
	Package string
	// Install requests the package be installed before running.
	Install bool
}

// Tester runs tests for modules.
type Tester struct {
	Root string
	// Reporter, when set, receives every execution step as it happens so the
	// caller can surface a live, step-by-step trace.
	Reporter func(Step)
	// Timeout, when > 0, overrides the hard cap for a test run.
	Timeout time.Duration
	// Config is the project `test` section (runner per module, package manager).
	Config config.TestConfig
	// ProjectPackageManager is the package manager chosen at init
	// (project.packageManager), used as a fallback for Config.PackageManager.
	ProjectPackageManager string
	// Runner overrides any configured runner (the --runner flag).
	Runner string
	// choices holds the interactive test-package selections per module. It is
	// populated by the caller (outside the step runner) via SetRunnerChoice so
	// no Bubble Tea prompt collides with the live step trace.
	choices map[string]RunnerChoice
}

// testTimeout returns the effective cap for a test run.
func (t *Tester) runTimeout() time.Duration {
	if t.Timeout > 0 {
		return t.Timeout
	}
	return testTimeout
}

// emit records a step on the result and forwards it to the live reporter.
func (t *Tester) emit(result *TestResult, step Step) {
	result.Steps = append(result.Steps, step)
	t.report(step)
}

// report forwards a step to the live reporter without counting it in the
// end-of-run summary (used for organizational lines such as module headers).
func (t *Tester) report(step Step) {
	if t.Reporter != nil {
		t.Reporter(step)
	}
}

// TestModule runs the tests of a single module.
func (t *Tester) TestModule(name string) (*TestResult, error) {
	return t.TestModuleCtx(context.Background(), name)
}

// TestModuleCtx runs the tests of a single module, cancelling the underlying
// command when ctx is done.
func (t *Tester) TestModuleCtx(ctx context.Context, name string) (*TestResult, error) {
	moduleDir := filepath.Join(t.Root, config.ExternalModulesDir, name)
	if !pkg.DirExists(moduleDir) {
		return nil, errors.New(i18n.Tf("val.module_not_found", name, config.ExternalModulesDir))
	}

	result := &TestResult{Module: name}

	// Step 1 — validate the module against the Sentient rules.
	v := &module.Validator{Root: t.Root}
	res, err := v.ValidateModule(name)
	if err != nil {
		return nil, err
	}

	valStatus := tui.StatusSuccess
	switch {
	case res.HasErrors():
		valStatus = tui.StatusError
	case res.WarningCount() > 0:
		valStatus = tui.StatusWarning
	}
	t.report(Step{
		Label:  i18n.T("test.step.validate"),
		Status: valStatus,
		Detail: i18n.Tf("test.step.validate.detail", res.ErrorCount(), res.WarningCount()),
	})

	// Recap: each failing/warning rule contributes its own severity so the
	// summary stays accurate, while the rules are also surfaced as log lines.
	findings := 0
	for _, f := range res.Findings {
		switch f.Severity {
		case module.LevelError:
			t.record(result, Step{Label: f.Message, Status: tui.StatusError})
		case module.LevelWarning:
			t.record(result, Step{Label: f.Message, Status: tui.StatusWarning})
		default:
			continue
		}
		findings++
		result.Logs = append(result.Logs, fmt.Sprintf("[%s] %s: %s", f.Severity, f.Rule, f.Message))
	}
	if findings == 0 {
		t.record(result, Step{Label: i18n.T("test.step.validate"), Status: tui.StatusSuccess})
	}

	if res.HasErrors() {
		result.Status = "ERROR"
		result.Errors = res.ErrorCount()
		return result, nil
	}

	// Step 2 — resolve the package manager. The one chosen at install
	// (project.packageManager) wins, then a test-specific override, then PATH
	// detection (bun → pnpm → yarn → npm).
	pm, fromConfig := t.effectivePackageManager()
	if pm == "" {
		result.Status = "WARNING"
		t.emit(result, Step{
			Label:  i18n.T("test.step.package_manager"),
			Status: tui.StatusWarning,
			Detail: i18n.T("test.step.package_manager.none"),
		})
		result.Logs = append(result.Logs, i18n.T("test.npm_none"))
		return result, nil
	}
	result.PackageManager = pm
	pmDetail := i18n.Tf("test.step.package_manager.detail", pm)
	if fromConfig {
		pmDetail = i18n.Tf("test.step.package_manager.config", pm)
	}
	t.emit(result, Step{
		Label:  i18n.T("test.step.package_manager"),
		Status: tui.StatusSuccess,
		Detail: pmDetail,
	})

	// Step 3 — resolve the test command. A configured/flagged test package
	// wins; otherwise the module's own package.json `test` script (then the
	// project root), an installed test package, or the package manager's
	// built-in runner. When nothing is available and the module contains test
	// files, the developer is offered the main test packages (and their
	// installation) in interactive mode.
	tc, err := t.resolveRunner(ctx, name, moduleDir, pm, result)
	if err != nil {
		return result, err
	}
	if tc == nil {
		result.Status = "WARNING"
		t.emit(result, Step{
			Label:  i18n.T("test.step.resolve"),
			Status: tui.StatusWarning,
			Detail: i18n.T("test.step.resolve.none"),
		})
		result.Logs = append(result.Logs, i18n.T("test.no_test_script"))
		result.Logs = append(result.Logs, i18n.Tf("test.hint.runners", strings.Join(TestPackageNames(), ", ")))
		return result, nil
	}

	// A module without any test file has nothing to run: skip it rather than
	// launching a resolved runner that would fail with "no test files found".
	if !moduleHasTests(moduleDir) {
		result.Status = "SKIPPED"
		t.emit(result, Step{
			Label:  i18n.T("test.step.no_tests"),
			Status: tui.StatusNotice,
			Detail: i18n.T("test.step.no_tests.detail"),
		})
		result.Logs = append(result.Logs, i18n.T("test.no_tests"))
		return result, nil
	}
	result.Runner = tc.runner
	result.Command = strings.Join(tc.cmd, " ")
	t.emit(result, Step{
		Label:  i18n.T("test.step.resolve"),
		Status: tui.StatusSuccess,
		Detail: tc.detail,
	})

	// Step 4 — run the resolved test command, streaming its output live.
	testStepID := "test:" + name
	t.report(Step{
		ID:     testStepID,
		Label:  i18n.T("test.step.run"),
		Status: tui.StatusRunning,
		Detail: tc.detail,
	})

	var (
		mu    sync.Mutex
		lines []string
	)
	onLine := func(line string) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, line)
		if len(lines) > outputLogCap {
			lines = lines[len(lines)-outputLogCap:]
		}
		t.report(Step{
			ID:     testStepID,
			Label:  i18n.T("test.step.run"),
			Status: tui.StatusRunning,
			Detail: tc.detail,
			Output: lastN(lines, outputTail),
		})
	}

	timeout := t.runTimeout()
	outcome := runner.Run(ctx, tc.dir, tc.cmd, 0, timeout, onLine)

	mu.Lock()
	output := strings.Join(lines, "\n")
	tail := lastN(lines, outputTail)
	mu.Unlock()

	if ctx.Err() != nil {
		result.Status = "CANCELLED"
		t.emit(result, Step{
			ID:     testStepID,
			Label:  i18n.T("test.step.cancelled"),
			Status: tui.StatusWarning,
			Output: tail,
		})
		return result, tui.ErrCancelled
	}

	switch {
	case outcome.TimedOut:
		reason := i18n.Tf("test.step.run.timeout", timeout)
		result.Status = "ERROR"
		result.Errors++
		t.emit(result, Step{
			ID:     testStepID,
			Label:  i18n.T("test.step.run"),
			Status: tui.StatusError,
			Detail: reason,
			Output: tail,
		})
		result.Logs = append(result.Logs, i18n.Tf("test.run_error", reason))
		if output != "" {
			result.Logs = append(result.Logs, output)
		}
	case outcome.Err != nil:
		result.Status = "ERROR"
		result.Errors++
		detail := outcome.Err.Error()
		if output != "" {
			detail = firstLine(output)
		}
		t.emit(result, Step{
			ID:     testStepID,
			Label:  i18n.T("test.step.run"),
			Status: tui.StatusError,
			Detail: detail,
			Output: tail,
		})
		result.Logs = append(result.Logs, i18n.Tf("test.run_error", outcome.Err.Error()))
		if output != "" {
			result.Logs = append(result.Logs, output)
		}
	default:
		result.Status = "OK"
		t.emit(result, Step{
			ID:     testStepID,
			Label:  i18n.T("test.step.run"),
			Status: tui.StatusSuccess,
			Detail: tc.detail,
			Output: tail,
		})
		if output != "" {
			result.Logs = append(result.Logs, output)
		}
	}

	return result, nil
}

// record counts a step in the end-of-run summary without rendering it live.
func (t *Tester) record(result *TestResult, step Step) {
	result.Steps = append(result.Steps, step)
}

// lastN returns a copy of the last n elements of s.
func lastN(s []string, n int) []string {
	if n <= 0 || len(s) == 0 {
		return nil
	}
	if len(s) > n {
		s = s[len(s)-n:]
	}
	out := make([]string, len(s))
	copy(out, s)
	return out
}

// firstLine returns the first non-empty line of s, used to keep a failing
// step's detail short (the full output stays in the logs).
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// FormatTestLogs returns formatted log lines with timestamps.
func FormatTestLogs(moduleName string, logs []string) []string {
	result := make([]string, 0, len(logs))
	for _, log := range logs {
		t := time.Now().Format("15:04:05")
		result = append(result, fmt.Sprintf("[%s] %s: %s", t, moduleName, log))
	}
	return result
}

// TestAll runs the tests of all modules in external_modules/.
func (t *Tester) TestAll() ([]*TestResult, error) {
	return t.TestAllCtx(context.Background())
}

// TestAllCtx runs the tests of all modules in external_modules/, stopping as
// soon as ctx is done.
func (t *Tester) TestAllCtx(ctx context.Context) ([]*TestResult, error) {
	dir := filepath.Join(t.Root, config.ExternalModulesDir)
	if !pkg.DirExists(dir) {
		return nil, errors.New(i18n.Tf("modules.error.dir", config.ExternalModulesDir))
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", config.ExternalModulesDir, err)
	}

	var results []*TestResult
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return results, tui.ErrCancelled
		}
		if !e.IsDir() {
			continue
		}
		if !pkg.FileExists(filepath.Join(dir, e.Name(), config.ManifestFileName)) {
			continue
		}
		t.report(Step{
			Label:  i18n.Tf("test.step.module", e.Name()),
			Status: tui.StatusNotice,
		})
		r, err := t.TestModuleCtx(ctx, e.Name())
		if err != nil {
			return results, err
		}
		results = append(results, r)
	}
	return results, nil
}

// effectivePackageManager returns the package manager to use and whether it
// comes from the project config (chosen at install) rather than PATH
// detection. A configured but unavailable manager falls back to detection so a
// moved project stays runnable.
func (t *Tester) effectivePackageManager() (string, bool) {
	for _, pm := range []string{t.Config.PackageManager, t.ProjectPackageManager} {
		pm = strings.TrimSpace(pm)
		if pm == "" {
			continue
		}
		if pkg.HasCommand(pm) {
			return pm, true
		}
		return pkg.DetectPackageManager(), false
	}
	return pkg.DetectPackageManager(), false
}

// testCommand is a resolved test command and the directory it belongs to.
type testCommand struct {
	cmd    []string
	dir    string
	detail string
	// runner is the resolved test package ("vitest", "script", "builtin", a
	// custom package name) so the caller can persist it.
	runner string
}

// scriptCommand resolves the package.json `test` script (module then root).
func (t *Tester) scriptCommand(pm, moduleDir string) *testCommand {
	for _, candidate := range []string{
		filepath.Join(moduleDir, "package.json"),
		filepath.Join(t.Root, "package.json"),
	} {
		if _, ok := readPackageScript(candidate, "test"); ok {
			cmd := []string{pm, "run", "test"}
			return &testCommand{cmd: cmd, dir: filepath.Dir(candidate), detail: strings.Join(cmd, " "), runner: ScriptRunner}
		}
	}
	return nil
}

// runnerCommand builds the command for a test package. BuiltinRunner maps to
// the package manager's built-in runner; anything else resolves the installed
// binary (module → root node_modules → PATH) and appends the catalog arguments.
func (t *Tester) runnerCommand(pm, moduleDir, name string) *testCommand {
	if name == BuiltinRunner {
		cmd := []string{pm, "test"}
		return &testCommand{cmd: cmd, dir: moduleDir, detail: strings.Join(cmd, " "), runner: BuiltinRunner}
	}
	bin := FindRunnerBinary(moduleDir, t.Root, name)
	if bin == "" {
		return nil
	}
	args := []string{bin}
	if tp, ok := FindTestPackage(name); ok {
		args = append(args, tp.Args...)
	}
	return &testCommand{
		cmd:    args,
		dir:    moduleDir,
		detail: i18n.Tf("test.step.resolve.detail", strings.Join(args, " ")),
		runner: name,
	}
}

// runnerOptions lists the catalog test packages and their installation state.
func (t *Tester) runnerOptions(moduleDir string) []RunnerOption {
	opts := make([]RunnerOption, 0, len(TestPackages))
	for _, tp := range TestPackages {
		opts = append(opts, RunnerOption{
			Package:   tp.Name,
			Installed: InstalledRunner(moduleDir, t.Root, tp.Name),
		})
	}
	return opts
}

// moduleDir returns the directory of a module.
func (t *Tester) moduleDir(module string) string {
	return filepath.Join(t.Root, config.ExternalModulesDir, module)
}

// RunnerOptions lists the catalog test packages offered to the developer and
// whether each is already installed for the module.
func (t *Tester) RunnerOptions(module string) []RunnerOption {
	return t.runnerOptions(t.moduleDir(module))
}

// NeedsRunnerChoice reports whether a module requires the developer to pick a
// test package: it contains test files but has no configured runner, no
// package.json `test` script and no installed test package.
func (t *Tester) NeedsRunnerChoice(module string) bool {
	if strings.TrimSpace(t.Runner) != "" || t.Config.RunnerFor(module) != "" {
		return false
	}
	pm, _ := t.effectivePackageManager()
	if pm == "" {
		return false
	}
	moduleDir := t.moduleDir(module)
	if t.scriptCommand(pm, moduleDir) != nil {
		return false
	}
	if pm == "bun" {
		return false
	}
	for _, tp := range TestPackages {
		if InstalledRunner(moduleDir, t.Root, tp.Name) {
			return false
		}
	}
	return moduleHasTests(moduleDir)
}

// SetRunnerChoice stores the interactive test-package selection for a module.
func (t *Tester) SetRunnerChoice(module string, choice RunnerChoice) {
	if t.choices == nil {
		t.choices = map[string]RunnerChoice{}
	}
	t.choices[module] = choice
}

// resolveRunner determines how to run a module's tests. It returns nil (with
// no error) when no runner is available and the developer did not choose one.
func (t *Tester) resolveRunner(ctx context.Context, module, moduleDir, pm string, result *TestResult) (*testCommand, error) {
	// 1. An explicit runner: the --runner flag, then the project config.
	configured := strings.TrimSpace(t.Runner)
	if configured == "" {
		configured = t.Config.RunnerFor(module)
	}
	if configured != "" {
		if configured == ScriptRunner {
			return t.scriptCommand(pm, moduleDir), nil
		}
		tc := t.runnerCommand(pm, moduleDir, configured)
		if tc == nil {
			ok, err := t.ensureInstalled(ctx, module, moduleDir, pm, configured, result)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, nil
			}
			tc = t.runnerCommand(pm, moduleDir, configured)
		}
		if tc == nil {
			t.emit(result, Step{
				Label:  i18n.T("test.step.runner"),
				Status: tui.StatusWarning,
				Detail: i18n.Tf("test.step.runner.missing_bin", RunnerBinary(configured)),
			})
			return nil, nil
		}
		t.emit(result, Step{
			Label:  i18n.T("test.step.runner"),
			Status: tui.StatusSuccess,
			Detail: i18n.Tf("test.step.runner.configured", configured),
		})
		return tc, nil
	}

	// 2. The module's (then the project's) package.json `test` script.
	if tc := t.scriptCommand(pm, moduleDir); tc != nil {
		return tc, nil
	}

	// 3. An already-installed catalog test package.
	for _, tp := range TestPackages {
		if !InstalledRunner(moduleDir, t.Root, tp.Name) {
			continue
		}
		if tc := t.runnerCommand(pm, moduleDir, tp.Name); tc != nil {
			t.emit(result, Step{
				Label:  i18n.T("test.step.runner"),
				Status: tui.StatusSuccess,
				Detail: i18n.Tf("test.step.runner.detected", tp.Name),
			})
			return tc, nil
		}
	}

	// 4. The package manager's built-in runner (e.g. `bun test`) when the
	// module actually contains test files.
	if moduleHasTests(moduleDir) && pm == "bun" {
		tc := t.runnerCommand(pm, moduleDir, BuiltinRunner)
		t.emit(result, Step{
			Label:  i18n.T("test.step.runner"),
			Status: tui.StatusSuccess,
			Detail: i18n.Tf("test.step.runner.builtin", pm),
		})
		return tc, nil
	}

	// 5. Nothing available: use the developer's interactive choice (made
	// before the run), which may install a test package within the package
	// manager's scope.
	if !moduleHasTests(moduleDir) {
		return nil, nil
	}
	choice := t.choices[module]
	name := strings.TrimSpace(choice.Package)
	if name == "" {
		return nil, nil
	}
	if choice.Install || !InstalledRunner(moduleDir, t.Root, name) {
		ok, err := t.ensureInstalled(ctx, module, moduleDir, pm, name, result)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
	}
	tc := t.runnerCommand(pm, moduleDir, name)
	if tc == nil {
		t.emit(result, Step{
			Label:  i18n.T("test.step.runner"),
			Status: tui.StatusWarning,
			Detail: i18n.Tf("test.step.runner.missing_bin", RunnerBinary(name)),
		})
		return nil, nil
	}
	t.emit(result, Step{
		Label:  i18n.T("test.step.runner"),
		Status: tui.StatusSuccess,
		Detail: i18n.Tf("test.step.runner.configured", name),
	})
	return tc, nil
}

// ensureInstalled installs a test package as a dev dependency of the module
// when it is not already available. It reports the installation as a step and
// returns whether the runner is usable.
func (t *Tester) ensureInstalled(ctx context.Context, module, moduleDir, pm, name string, result *TestResult) (bool, error) {
	if name == BuiltinRunner || InstalledRunner(moduleDir, t.Root, name) {
		return true, nil
	}
	args := pkg.DevDependencyArgs(pm, name)
	if args == nil {
		t.emit(result, Step{
			Label:  i18n.T("test.step.runner"),
			Status: tui.StatusError,
			Detail: i18n.Tf("test.step.runner.install_error", name, i18n.T("test.step.runner.unsupported_pm")),
		})
		return false, nil
	}

	stepID := "install:" + module
	installDetail := i18n.Tf("test.step.runner.installing", name)
	t.report(Step{ID: stepID, Label: i18n.T("test.step.runner"), Status: tui.StatusRunning, Detail: installDetail})

	var (
		mu    sync.Mutex
		lines []string
	)
	onLine := func(line string) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, line)
		if len(lines) > outputLogCap {
			lines = lines[len(lines)-outputLogCap:]
		}
		t.report(Step{
			ID:     stepID,
			Label:  i18n.T("test.step.runner"),
			Status: tui.StatusRunning,
			Detail: installDetail,
			Output: lastN(lines, outputTail),
		})
	}

	cmd := append([]string{pm}, args...)
	outcome := runner.Run(ctx, moduleDir, cmd, 0, installTimeout, onLine)
	if ctx.Err() != nil {
		return false, tui.ErrCancelled
	}

	mu.Lock()
	output := strings.Join(lines, "\n")
	tail := lastN(lines, outputTail)
	mu.Unlock()

	if outcome.Err != nil || outcome.TimedOut {
		reason := i18n.T("test.step.runner.install_timeout")
		if outcome.Err != nil {
			reason = outcome.Err.Error()
			if output != "" {
				reason = firstLine(output)
			}
		}
		t.emit(result, Step{
			ID:     stepID,
			Label:  i18n.T("test.step.runner"),
			Status: tui.StatusError,
			Detail: i18n.Tf("test.step.runner.install_error", name, reason),
			Output: tail,
		})
		result.Logs = append(result.Logs, i18n.Tf("test.step.runner.install_error", name, reason))
		if output != "" {
			result.Logs = append(result.Logs, output)
		}
		return false, nil
	}

	t.emit(result, Step{
		ID:     stepID,
		Label:  i18n.T("test.step.runner"),
		Status: tui.StatusSuccess,
		Detail: i18n.Tf("test.step.runner.installed", name),
		Output: tail,
	})
	return true, nil
}

// readPackageScript returns the value of a package.json script if present.
func readPackageScript(path, name string) (string, bool) {
	if !pkg.FileExists(path) {
		return "", false
	}
	var nodePackage struct {
		Scripts map[string]string `json:"scripts"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	if err := json.Unmarshal(data, &nodePackage); err != nil {
		return "", false
	}
	v, ok := nodePackage.Scripts[name]
	return v, ok
}

// testDirNames are the conventional directories that hold test files. Their
// presence alone marks a module as testable, even when the files they contain
// do not follow the *.test.* / *.spec.* naming convention.
var testDirNames = map[string]bool{
	"__tests__": true,
	"__test__":  true,
	"test":      true,
	"tests":     true,
	"spec":      true,
	"specs":     true,
}

// moduleHasTests reports whether the module directory contains any test file
// (*.test.* / *.spec.*) or test directory (__tests__/, test/, tests/, spec/,
// …), excluding node_modules and .git.
func moduleHasTests(moduleDir string) bool {
	found := false
	_ = filepath.WalkDir(moduleDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}
		if d.IsDir() {
			name := strings.ToLower(d.Name())
			if name == "node_modules" || name == ".git" {
				return filepath.SkipDir
			}
			// The walk root is the module itself, not a test directory.
			if path != moduleDir && testDirNames[name] {
				found = true
				return filepath.SkipDir
			}
			return nil
		}
		base := strings.ToLower(filepath.Base(path))
		if strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
			found = true
		}
		return nil
	})
	return found
}
