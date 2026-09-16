// Package moduletest runs the tests of Sentient modules (spec §6.x, future
// scope moved to scope: `sentients test <module>`).
package moduletest

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
	// Steps is the ordered trace of the stages executed for this module.
	Steps []Step
}

// Tester runs tests for modules.
type Tester struct {
	Root string
	// Reporter, when set, receives every execution step as it happens so the
	// caller can surface a live, step-by-step trace.
	Reporter func(Step)
	// Timeout, when > 0, overrides the hard cap for a test run.
	Timeout time.Duration
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

	// Step 2 — detect the package manager (bun → pnpm → yarn → npm).
	pm := t.detectPackageManager()
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
	t.emit(result, Step{
		Label:  i18n.T("test.step.package_manager"),
		Status: tui.StatusSuccess,
		Detail: i18n.Tf("test.step.package_manager.detail", pm),
	})

	// Step 3 — resolve the test command. The module's own package.json `test`
	// script wins; the project root one is the fallback. Without a script, a
	// real test runner (vitest / jest / bun test) is used only when the module
	// actually contains test files — otherwise validation is the only step.
	tc := t.findTestCommand(pm, moduleDir)
	if tc == nil {
		result.Status = "WARNING"
		t.emit(result, Step{
			Label:  i18n.T("test.step.resolve"),
			Status: tui.StatusWarning,
			Detail: i18n.T("test.step.resolve.none"),
		})
		result.Logs = append(result.Logs, i18n.T("test.no_test_script"))
		return result, nil
	}
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

// detectPackageManager returns the first resolvable package manager.
func (t *Tester) detectPackageManager() string {
	for _, pm := range []string{"bun", "pnpm", "yarn", "npm"} {
		if _, err := exec.LookPath(pm); err == nil {
			return pm
		}
	}
	return ""
}

// testCommand is a resolved test command and the directory it belongs to.
type testCommand struct {
	cmd    []string
	dir    string
	detail string
}

func (t *Tester) findTestCommand(pm, moduleDir string) *testCommand {
	// A `test` script in the module's own package.json, then the project root.
	for _, candidate := range []string{
		filepath.Join(moduleDir, "package.json"),
		filepath.Join(t.Root, "package.json"),
	} {
		if _, ok := readPackageScript(candidate, "test"); ok {
			cmd := []string{pm, "run", "test"}
			return &testCommand{cmd: cmd, dir: filepath.Dir(candidate), detail: strings.Join(cmd, " ")}
		}
	}

	// No package.json script: a real runner is only usable when the module
	// contains test files — otherwise there is nothing to run.
	if !moduleHasTests(moduleDir) {
		return nil
	}
	bin := t.findTestRunner(pm, moduleDir)
	if bin == "" {
		return nil
	}
	cmd := []string{bin, "run"}
	if bin == "bun" {
		cmd = []string{bin, "test"}
	}
	return &testCommand{
		cmd:    cmd,
		dir:    moduleDir,
		detail: i18n.Tf("test.step.resolve.detail", filepath.Base(bin)),
	}
}

// findTestRunner resolves a real test runner for the module: a package-local
// vitest/jest binary (module or root node_modules), then PATH, falling back to
// `bun test` when the package manager is bun itself.
func (t *Tester) findTestRunner(pm, moduleDir string) string {
	for _, bin := range []string{
		filepath.Join(moduleDir, "node_modules", ".bin", "vitest"),
		filepath.Join(moduleDir, "node_modules", ".bin", "jest"),
		filepath.Join(t.Root, "node_modules", ".bin", "vitest"),
		filepath.Join(t.Root, "node_modules", ".bin", "jest"),
	} {
		if pkg.FileExists(bin) {
			return bin
		}
	}
	for _, bin := range []string{"vitest", "jest"} {
		if pkg.HasCommand(bin) {
			return bin
		}
	}
	if pm == "bun" && pkg.HasCommand("bun") {
		return "bun"
	}
	return ""
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

// moduleHasTests reports whether the module directory contains any test file
// (*.test.* / *.spec.* or a __tests__ directory), excluding node_modules.
func moduleHasTests(moduleDir string) bool {
	found := false
	_ = filepath.WalkDir(moduleDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "node_modules" || d.Name() == ".git" {
				return filepath.SkipDir
			}
			if d.Name() == "__tests__" {
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
