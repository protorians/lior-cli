// Package toolchain runs the application-level passthrough commands (`dev`,
// `build`, `start`, `check`). Those commands proxy the package.json scripts of
// the project (spec docs/specs/liora-toolchain.md): the CLI stays the single
// entry point of the application lifecycle and can run configurable pre/post
// actions — hooks — around the underlying command.
//
// The package deliberately never names the underlying application engine in
// user-facing strings: every invocation goes through a package.json script
// resolved with the project package manager.
package toolchain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/protorians/lior-cli/internal/config"
	"github.com/protorians/lior-cli/internal/i18n"
	"github.com/protorians/lior-cli/internal/pkg"
	"github.com/protorians/lior-cli/internal/runner"
	"github.com/protorians/lior-cli/internal/tui"
)

const (
	// outputTail is the number of recent output lines kept for a running step.
	outputTail = 8
	// outputLogCap bounds the output retained in the final log block.
	outputLogCap = 200
)

// Step is a single reported execution step (the tui step vocabulary).
type Step = tui.Step

// Command names supported by the passthrough layer.
const (
	Dev   = "dev"
	Build = "build"
	Start = "start"
	Check = "check"
)

// DefaultScript maps a passthrough command to the package.json script that
// backs it. "check" deliberately proxies the static-analysis script.
var DefaultScript = map[string]string{
	Dev:   "dev",
	Build: "build",
	Start: "start",
	Check: "lint",
}

// runLabel returns the localized label of the execution step for a command.
func runLabel(name string) string {
	switch name {
	case Dev:
		return i18n.T("toolchain.step.dev")
	case Build:
		return i18n.T("toolchain.step.build")
	case Start:
		return i18n.T("toolchain.step.start")
	case Check:
		return i18n.T("toolchain.step.check")
	}
	return i18n.T("toolchain.step.run")
}

// Command is a resolved passthrough invocation and the directory it belongs to.
type Command struct {
	Args []string
	Dir  string
}

// Detail returns the neutral command line shown to the user.
func (c *Command) Detail() string {
	return strings.Join(c.Args, " ")
}

// Runner proxies the application toolchain commands for a project.
type Runner struct {
	Root string
	// PM is the package manager binary used to run scripts (bun, pnpm, yarn,
	// npm). An empty value yields a categorized error at resolution time.
	PM string
	// Config is the project `toolchain` section (script overrides + hooks).
	Config config.ToolchainConfig
	// Reporter, when set, receives every execution step as it happens so the
	// caller can surface a live, step-by-step trace.
	Reporter func(Step)
}

// New builds a Runner for a project root and package manager.
func New(root, pm string, cfg config.ToolchainConfig) *Runner {
	return &Runner{Root: root, PM: pm, Config: cfg}
}

// report forwards a step to the live reporter.
func (r *Runner) report(step Step) {
	if r.Reporter != nil {
		r.Reporter(step)
	}
}

// Resolve determines the command line that backs a passthrough command: the
// configured script (toolchain.commands.<name>), else the default mapping,
// both resolved through the project package manager. The package.json script
// must exist at the project root.
func (r *Runner) Resolve(name string, extra []string) (*Command, error) {
	if pm := strings.TrimSpace(r.PM); pm == "" {
		return nil, pkg.NewErrorWithFix(
			i18n.T("cat.package_manager"),
			i18n.T("toolchain.error.pm_none"),
			i18n.T("toolchain.error.pm_none.fix"),
			pkg.ExitError,
		)
	}

	script := r.Config.Command(name)
	if script == "" {
		script = DefaultScript[name]
	}
	if script == "" {
		return nil, fmt.Errorf("toolchain: unknown command %q", name)
	}

	if !packageScript(filepath.Join(r.Root, "package.json"), script) {
		return nil, pkg.NewErrorWithFix(
			i18n.T("cat.toolchain"),
			i18n.Tf("toolchain.error.no_script", script),
			i18n.Tf("toolchain.error.no_script.fix", script),
			pkg.ExitError,
		)
	}

	args := []string{r.PM, "run", script}
	if len(extra) > 0 {
		// npm requires a "--" separator before the forwarded arguments.
		if r.PM == "npm" {
			args = append(args, "--")
		}
		args = append(args, extra...)
	}
	return &Command{Args: args, Dir: r.Root}, nil
}

// Execute runs the whole passthrough life cycle for a command name: resolve,
// pre-actions, the underlying command (streamed live), post-actions. It
// returns tui.ErrCancelled when the developer aborts, a categorized *pkg.Error
// on any failed step (carrying the underlying exit code), and nil on success.
func (r *Runner) Execute(ctx context.Context, name string, extra []string) error {
	c, err := r.Resolve(name, extra)
	if err != nil {
		r.report(Step{
			Label:  i18n.T("toolchain.step.resolve"),
			Status: tui.StatusError,
			Detail: err.Error(),
		})
		return err
	}
	r.report(Step{
		Label:  i18n.T("toolchain.step.resolve"),
		Status: tui.StatusSuccess,
		Detail: c.Detail(),
	})

	if err := r.runHooks(ctx, name, "before", c); err != nil {
		return err
	}

	resultErr := r.runCommand(ctx, name, c)

	if ctx.Err() != nil {
		return tui.ErrCancelled
	}

	if err := r.runHooks(ctx, name, "after", c); err != nil {
		return err
	}
	return resultErr
}

// runHooks executes the configured pre/post action scripts (package.json
// scripts run through the package manager) for a command. A failing hook
// aborts the phase and returns a categorized error carrying the hook's exit
// code. A cancelled context stops the phase silently.
func (r *Runner) runHooks(ctx context.Context, name, phase string, c *Command) error {
	var (
		hooks     []string
		label     string
		failedKey string
	)
	if phase == "before" {
		hooks, label, failedKey = r.Config.BeforeHooks(name), i18n.T("toolchain.step.before"), "toolchain.before_failed"
	} else {
		hooks, label, failedKey = r.Config.AfterHooks(name), i18n.T("toolchain.step.after"), "toolchain.after_failed"
	}

	for i, hook := range hooks {
		detail := strings.Join([]string{r.PM, "run", hook}, " ")
		stepID := phase + ":" + name + ":" + strconv.Itoa(i)
		r.report(Step{ID: stepID, Label: label, Status: tui.StatusRunning, Detail: detail})

		code, tail := r.stream(ctx, c.Dir, []string{r.PM, "run", hook}, stepID, label, detail)
		if ctx.Err() != nil {
			return tui.ErrCancelled
		}
		if code != 0 {
			r.report(Step{
				ID:     stepID,
				Label:  label,
				Status: tui.StatusError,
				Detail: i18n.Tf("toolchain.step.run.failed", code),
				Output: tail,
			})
			return pkg.NewError(i18n.T("cat.toolchain"), i18n.Tf(failedKey, hook, code), code)
		}
		r.report(Step{ID: stepID, Label: label, Status: tui.StatusSuccess, Detail: detail, Output: tail})
	}
	return nil
}

// runCommand executes the resolved passthrough command, streaming its output
// live under a running step, and reports a terminal success/error step. It
// returns a categorized error carrying the underlying exit code on failure.
func (r *Runner) runCommand(ctx context.Context, name string, c *Command) error {
	stepID := "cmd:" + name
	label := runLabel(name)
	detail := c.Detail()
	r.report(Step{ID: stepID, Label: label, Status: tui.StatusRunning, Detail: detail})

	code, tail := r.stream(ctx, c.Dir, c.Args, stepID, label, detail)
	if ctx.Err() != nil {
		return tui.ErrCancelled
	}
	if code == 0 {
		r.report(Step{ID: stepID, Label: label, Status: tui.StatusSuccess, Detail: detail, Output: tail})
		return nil
	}
	r.report(Step{
		ID:     stepID,
		Label:  label,
		Status: tui.StatusError,
		Detail: i18n.Tf("toolchain.step.run.failed", code),
		Output: tail,
	})
	return pkg.NewError(i18n.T("cat.toolchain"), i18n.Tf("toolchain.step.run.failed", code), code)
}

// stream runs a command without a window or cap (servers and one-shot alike
// end on their own or on cancellation), streaming every output line under the
// given step, and returns the underlying exit code plus the retained tail.
func (r *Runner) stream(ctx context.Context, dir string, args []string, stepID, label, detail string) (int, []string) {
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
		r.report(Step{ID: stepID, Label: label, Status: tui.StatusRunning, Detail: detail, Output: tail})
	}

	outcome := runner.Run(ctx, dir, args, 0, 0, onLine)

	mu.Lock()
	tail := lastN(lines, outputTail)
	mu.Unlock()

	if outcome.Err != nil {
		return exitCodeOf(outcome.Err), tail
	}
	return 0, tail
}

// exitCodeOf extracts the process exit code from an execution error, defaulting
// to a generic error code for start/other failures.
func exitCodeOf(err error) int {
	var ee interface{ ExitCode() int }
	if errors.As(err, &ee) {
		if code := ee.ExitCode(); code > 0 {
			return code
		}
	}
	return 1
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

// packageScript reports whether path's package.json declares the given script.
func packageScript(path, name string) bool {
	var nodePackage struct {
		Scripts map[string]string `json:"scripts"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if err := json.Unmarshal(data, &nodePackage); err != nil {
		return false
	}
	_, ok := nodePackage.Scripts[name]
	return ok
}
