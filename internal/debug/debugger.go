package debug

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/module"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/runner"
	"github.com/protorians/lior-cli/internal/tui"
)

// Default execution windows (overridable via Debugger.Timeout).
const (
	// startWindow is how long a `debug`/`dev` script is allowed to run before
	// it is considered started and stopped (those scripts are dev servers /
	// watchers that never exit on their own).
	startWindow = 15 * time.Second
	// buildTimeout caps one-shot build commands so a stuck build cannot hang
	// the CLI forever.
	buildTimeout = 5 * time.Minute
	// outputTail is the number of recent output lines kept for a running step.
	outputTail = 8
	// outputLogCap bounds the output retained in the final log block.
	outputLogCap = 200
)

// Step is a single reported execution step (the tui step vocabulary: success,
// notice, warning, error, deprecated).
type Step = tui.Step

// DebugResult holds the outcome of a debug run for a single module.
type DebugResult struct {
	Module string
	Status string
	Errors int
	Logs   []string
	// Steps is the ordered trace of the stages executed for this module.
	Steps []Step
}

// Debugger runs debug builds for modules.
type Debugger struct {
	Root string
	// Reporter, when set, receives every execution step as it happens so the
	// caller can surface a live, step-by-step trace.
	Reporter func(Step)
	// Timeout, when > 0, overrides the default execution windows: it is the
	// window after which a `debug`/`dev` script is considered started, and the
	// hard cap for a one-shot build command.
	Timeout time.Duration
}

// startWindow returns the effective window for a start script.
func (d *Debugger) startWindow() time.Duration {
	if d.Timeout > 0 {
		return d.Timeout
	}
	return startWindow
}

// buildTimeout returns the effective cap for a one-shot build command.
func (d *Debugger) buildTimeout() time.Duration {
	if d.Timeout > 0 {
		return d.Timeout
	}
	return buildTimeout
}

// emit records a step on the result and forwards it to the live reporter.
func (d *Debugger) emit(result *DebugResult, step Step) {
	result.Steps = append(result.Steps, step)
	d.report(step)
}

// report forwards a step to the live reporter without counting it in the
// end-of-run summary (used for organizational lines such as module headers).
func (d *Debugger) report(step Step) {
	if d.Reporter != nil {
		d.Reporter(step)
	}
}

// record counts a step in the end-of-run summary without rendering it live
// (used to break the validation aggregate down into its findings).
func (d *Debugger) record(result *DebugResult, step Step) {
	result.Steps = append(result.Steps, step)
}

// DebugModule runs a debug build of a single module.
func (d *Debugger) DebugModule(name string) (*DebugResult, error) {
	return d.DebugModuleCtx(context.Background(), name)
}

// DebugModuleCtx runs a debug build of a single module, cancelling the
// underlying build command when ctx is done.
func (d *Debugger) DebugModuleCtx(ctx context.Context, name string) (*DebugResult, error) {
	moduleDir := filepath.Join(d.Root, config.ExternalModulesDir, name)
	if !pkg.DirExists(moduleDir) {
		return nil, errors.New(i18n.Tf("val.module_not_found", name, config.ExternalModulesDir))
	}

	result := &DebugResult{Module: name}

	// Step 1 — validate the module against the Liorian rules.
	v := &module.Validator{Root: d.Root}
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
	// Live trace: a single line summarizing the whole validation stage.
	d.report(Step{
		Label:  i18n.T("debug.step.validate"),
		Status: valStatus,
		Detail: i18n.Tf("debug.step.validate.detail", res.ErrorCount(), res.WarningCount()),
	})

	// Recap: each failing/warning rule contributes its own severity so the
	// summary stays accurate, while the rules are also surfaced as log lines.
	findings := 0
	for _, f := range res.Findings {
		switch f.Severity {
		case module.LevelError:
			d.record(result, Step{Label: f.Message, Status: tui.StatusError})
		case module.LevelWarning:
			d.record(result, Step{Label: f.Message, Status: tui.StatusWarning})
		default:
			continue
		}
		findings++
		result.Logs = append(result.Logs, fmt.Sprintf("[%s] %s: %s", f.Severity, f.Rule, f.Message))
	}
	if findings == 0 {
		d.record(result, Step{Label: i18n.T("debug.step.validate"), Status: tui.StatusSuccess})
	}

	if res.HasErrors() {
		result.Status = "ERROR"
		result.Errors = res.ErrorCount()
		return result, nil
	}

	// Step 2 — detect the package manager (bun → pnpm → yarn → npm).
	pm := d.detectPackageManager()
	if pm == "" {
		result.Status = "WARNING"
		d.emit(result, Step{
			Label:  i18n.T("debug.step.package_manager"),
			Status: tui.StatusWarning,
			Detail: i18n.T("debug.step.package_manager.none"),
		})
		result.Logs = append(result.Logs, i18n.T("debug.npm_none"))
		return result, nil
	}
	d.emit(result, Step{
		Label:  i18n.T("debug.step.package_manager"),
		Status: tui.StatusSuccess,
		Detail: i18n.Tf("debug.step.package_manager.detail", pm),
	})

	// Step 3 — resolve the build command. Look for a debug or build script,
	// preferring the module's own package.json over the project root one.
	build := d.findBuildCommand(pm, moduleDir)
	resolveStatus := tui.StatusSuccess
	if build == nil {
		// Fallback to a real bundle when a bundler (esbuild/tsup) is
		// resolvable — beyond the package.json scripts (spec §5.9 "Exécuter
		// le build du module"): this compiles the module entry to real
		// output under `dist/` instead of a type-check.
		build = d.findBundlerBuildCommand(moduleDir)
		if build != nil {
			resolveStatus = tui.StatusNotice
		}
	}
	if build == nil {
		// Fallback to a real TypeScript type-check when the module contains
		// TS/TSX sources and a tsconfig + tsc are resolvable.
		build = d.findTypeCheckCommand(moduleDir)
		if build != nil {
			resolveStatus = tui.StatusNotice
		}
	}
	if build == nil {
		// No build script, bundler or type-check: the validation above is the
		// only thing executed — this must not be reported as a successful build
		// (previously a false "OK" hid the absence of any real compilation).
		result.Status = "WARNING"
		d.emit(result, Step{
			Label:  i18n.T("debug.step.resolve"),
			Status: tui.StatusWarning,
			Detail: i18n.T("debug.step.resolve.none"),
		})
		result.Logs = append(result.Logs, i18n.T("debug.no_build_script"))
		return result, nil
	}
	d.emit(result, Step{
		Label:  i18n.T("debug.step.resolve"),
		Status: resolveStatus,
		Detail: strings.Join(build.cmd, " "),
	})

	// Step 4 — run the resolved build command, streaming its output live so the
	// sub-task in progress (and any failure) is visible in real time.
	buildStepID := "build:" + name
	baseDetail := strings.Join(build.cmd, " ")
	if build.realBuild {
		baseDetail = i18n.Tf("debug.bundler", build.label, build.outDir)
	}
	d.report(Step{
		ID:     buildStepID,
		Label:  i18n.T("debug.step.build"),
		Status: tui.StatusRunning,
		Detail: baseDetail,
	})

	var (
		mu    sync.Mutex
		lines []string
	)
	onLine := func(line string) {
		mu.Lock()
		lines = append(lines, line)
		if len(lines) > outputLogCap {
			lines = lines[len(lines)-outputLogCap:]
		}
		tail := lastN(lines, outputTail)
		mu.Unlock()
		d.report(Step{
			ID:     buildStepID,
			Label:  i18n.T("debug.step.build"),
			Status: tui.StatusRunning,
			Detail: baseDetail,
			Output: tail,
		})
	}

	window := time.Duration(0)
	if build.startScript {
		window = d.startWindow()
	}
	outcome := runner.Run(ctx, build.dir, build.cmd, window, d.buildTimeout(), onLine)

	mu.Lock()
	output := strings.Join(lines, "\n")
	tail := lastN(lines, outputTail)
	mu.Unlock()

	if ctx.Err() != nil {
		result.Status = "CANCELLED"
		d.emit(result, Step{
			ID:     buildStepID,
			Label:  i18n.T("debug.step.cancelled"),
			Status: tui.StatusWarning,
			Output: tail,
		})
		return result, tui.ErrCancelled
	}

	switch {
	case outcome.Started:
		// A dev/watch script is not expected to exit: reaching the window
		// proves it started, so stop it and report a benign notice.
		result.Status = "OK"
		d.emit(result, Step{
			ID:     buildStepID,
			Label:  i18n.T("debug.step.build"),
			Status: tui.StatusNotice,
			Detail: i18n.Tf("debug.step.build.started", window),
			Output: tail,
		})
		if output != "" {
			result.Logs = append(result.Logs, output)
		}
	case outcome.TimedOut:
		reason := i18n.Tf("debug.step.build.timeout", d.buildTimeout())
		result.Status = "ERROR"
		result.Errors++
		d.emit(result, Step{
			ID:     buildStepID,
			Label:  i18n.T("debug.step.build"),
			Status: tui.StatusError,
			Detail: reason,
			Output: tail,
		})
		result.Logs = append(result.Logs, i18n.Tf("debug.build_error", reason))
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
		d.emit(result, Step{
			ID:     buildStepID,
			Label:  i18n.T("debug.step.build"),
			Status: tui.StatusError,
			Detail: detail,
			Output: tail,
		})
		result.Logs = append(result.Logs, i18n.Tf("debug.build_error", outcome.Err.Error()))
		if output != "" {
			result.Logs = append(result.Logs, output)
		}
	default:
		result.Status = "OK"
		d.emit(result, Step{
			ID:     buildStepID,
			Label:  i18n.T("debug.step.build"),
			Status: tui.StatusSuccess,
			Detail: baseDetail,
			Output: tail,
		})
		if build.realBuild {
			result.Logs = append(result.Logs, baseDetail)
		}
		if output != "" {
			result.Logs = append(result.Logs, output)
		}
	}

	return result, nil
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

// firstLine returns the first non-empty line of s, used to keep a build error
// step's detail short (the full output stays in the logs).
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// DebugAll runs debug on all modules in external_modules/.
func (d *Debugger) DebugAll() ([]*DebugResult, error) {
	return d.DebugAllCtx(context.Background())
}

// DebugAllCtx runs debug on all modules in external_modules/, stopping as soon
// as ctx is done.
func (d *Debugger) DebugAllCtx(ctx context.Context) ([]*DebugResult, error) {
	dir := filepath.Join(d.Root, config.ExternalModulesDir)
	if !pkg.DirExists(dir) {
		return nil, errors.New(i18n.Tf("modules.error.dir", config.ExternalModulesDir))
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", config.ExternalModulesDir, err)
	}

	var results []*DebugResult
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
		// Mark the start of each module's step trace when several are debugged.
		d.report(Step{
			Label:  i18n.Tf("debug.step.module", e.Name()),
			Status: tui.StatusNotice,
		})
		r, err := d.DebugModuleCtx(ctx, e.Name())
		if err != nil {
			return results, err
		}
		results = append(results, r)
	}
	return results, nil
}

// buildCommand is a build script or bundler command to run and the directory
// it belongs to. label and outDir (optional) are purely informational: label
// is shown in the OK log and outDir is the emitted bundle directory.
type buildCommand struct {
	cmd    []string
	dir    string
	label  string
	outDir string
	// realBuild marks a bundler or compiler invocation (true output), as
	// opposed to a package-manager `run` passthrough.
	realBuild bool
	// startScript marks a `debug`/`dev` script: a dev server or watcher that
	// does not exit on its own and is stopped once its start window elapses.
	startScript bool
}

func (d *Debugger) detectPackageManager() string {
	for _, pm := range []string{"bun", "pnpm", "yarn", "npm"} {
		if _, err := exec.LookPath(pm); err == nil {
			return pm
		}
	}
	return ""
}

// findBuildCommand searches for a debug/dev/build script, first in the module's
// own package.json (if any), then at the project root. The scripts map is
// parsed as JSON so a substring collision (e.g. `"build:prod"`) does not
// trigger a match.
func (d *Debugger) findBuildCommand(pm, moduleDir string) *buildCommand {
	scripts := []string{"debug", "dev", "build"}
	candidates := []string{
		filepath.Join(moduleDir, "package.json"),
		filepath.Join(d.Root, "package.json"),
	}
	for _, candidate := range candidates {
		dir := filepath.Dir(candidate)
		for _, script := range scripts {
			if _, ok := packageScript(candidate, script); ok {
				return &buildCommand{
					cmd:         []string{pm, "run", script},
					dir:         dir,
					startScript: script == "debug" || script == "dev",
				}
			}
		}
	}
	return nil
}

// packageScript returns the value of a package.json script if present.
func packageScript(path, name string) (string, bool) {
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

// bundlerName is a known real bundler executable prefixed by its CLI entry.
var bundlerCLI = map[string]string{
	"esbuild": "esbuild",
	"tsup":    "tsup",
}

// findBundlerBuildCommand resolves a real bundler (esbuild/tsup) — module
// node_modules, project root node_modules, then PATH — and drives a true
// bundle of the module entry into `dist/`, producing actual output instead of
// a plain type-check. Returns nil when no bundler is resolvable (spec §5.9,
// event-driven build beyond the package.json scripts).
func (d *Debugger) findBundlerBuildCommand(moduleDir string) *buildCommand {
	entry := filepath.Join(moduleDir, "index.tsx")
	if !pkg.FileExists(entry) {
		return nil
	}

	outDir := filepath.Join(moduleDir, "dist")
	if !pkg.DirExists(outDir) {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return nil
		}
	}

	for _, bin := range []string{
		filepath.Join(moduleDir, "node_modules", ".bin", "esbuild"),
		filepath.Join(moduleDir, "node_modules", ".bin", "tsup"),
		filepath.Join(d.Root, "node_modules", ".bin", "esbuild"),
		filepath.Join(d.Root, "node_modules", ".bin", "tsup"),
	} {
		if pkg.FileExists(bin) {
			cli, ok := bundlerCLI[filepath.Base(bin)]
			if !ok {
				continue
			}
			return bundlerCommand(cli, bin, entry, outDir)
		}
	}
	for _, cli := range []string{"esbuild", "tsup"} {
		if pkg.HasCommand(cli) {
			return bundlerCommand(cli, cli, entry, outDir)
		}
	}
	return nil
}

// bundlerCommand builds the CLI invocation for a resolved bundler. esbuild and
// tsup share the same entry → bundle → outdir shape, only the output flag
// differs.
func bundlerCommand(cli, execPath, entry, outDir string) *buildCommand {
	cmd := []string{execPath, entry, "--bundle"}
	flag := "--out-dir"
	if cli == "esbuild" {
		flag = "--outdir"
	}
	cmd = append(cmd, flag, outDir)
	return &buildCommand{cmd: cmd, dir: filepath.Dir(entry), label: cli, outDir: outDir, realBuild: true}
}

// findTypeCheckCommand locates a real TypeScript type-check for the module:
// a resolvable `tsc` binary (root node_modules/.bin, module node_modules/.bin
// or PATH) plus a tsconfig to drive it (module-level preferred, root as
// fallback). Without a tsconfig the compiler cannot know the project layout,
// so no type-check is attempted.
func (d *Debugger) findTypeCheckCommand(moduleDir string) *buildCommand {
	if !moduleHasTS(moduleDir) {
		return nil
	}

	// tsconfig resolution: the module's own tsconfig, if any, else the root one.
	configDir := ""
	for _, candidate := range []string{
		filepath.Join(moduleDir, "tsconfig.json"),
		filepath.Join(d.Root, "tsconfig.json"),
	} {
		if pkg.FileExists(candidate) {
			configDir = filepath.Dir(candidate)
			break
		}
	}
	if configDir == "" {
		return nil
	}

	// tsc binary resolution: package-local, then PATH.
	for _, bin := range []string{
		filepath.Join(d.Root, "node_modules", ".bin", "tsc"),
		filepath.Join(moduleDir, "node_modules", ".bin", "tsc"),
	} {
		if pkg.FileExists(bin) {
			return &buildCommand{cmd: []string{bin, "--noEmit"}, dir: configDir}
		}
	}
	if pkg.HasCommand("tsc") {
		return &buildCommand{cmd: []string{"tsc", "--noEmit"}, dir: configDir}
	}
	return nil
}

// moduleHasTS reports whether the module directory contains any TypeScript
// source file (.ts or .tsx), excluding node_modules.
func moduleHasTS(moduleDir string) bool {
	found := false
	_ = filepath.WalkDir(moduleDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "node_modules" || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext == ".ts" || ext == ".tsx" {
			found = true
		}
		return nil
	})
	return found
}

// FormatDebugLogs returns formatted log lines with timestamps.
func FormatDebugLogs(moduleName string, logs []string) []string {
	result := make([]string, 0, len(logs))
	for _, log := range logs {
		t := time.Now().Format("15:04:05")
		result = append(result, fmt.Sprintf("[%s] %s: %s", t, moduleName, log))
	}
	return result
}
