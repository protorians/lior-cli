// Package runner executes commands with streaming output, an optional start
// window (dev server / watcher) and a hard timeout, terminating the whole
// process group (the process and its children) when one of them elapses.
//
// It is the shared execution engine behind `debug` and `test`: both commands
// stream live stdout/stderr under a running step and must stop the tree
// (SIGINT then SIGKILL) on window, cap or cancellation.
package runner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

// OutputLine is the receiver for each stdout/stderr line as it is produced.
type OutputLine func(line string)

// Outcome is the result of a streamed command execution.
type Outcome struct {
	// Err is the raw execution error (nil on success).
	Err error
	// Started marks a start script (dev server / watcher) stopped after its
	// window while still running — a benign success for such a script.
	Started bool
	// TimedOut marks a one-shot command killed after its hard cap.
	TimedOut bool
}

// Run executes a command and streams every stdout/stderr line to onLine as it
// is produced, so the caller can surface real-time feedback.
//
//   - window > 0: a dev server / watcher start script — when the process is
//     still running after window it is stopped and reported as Started.
//   - timeout > 0: a hard cap for one-shot commands.
//
// The process (and, via SIGINT + WaitDelay, its children) is stopped when the
// window, the cap, or ctx terminates the run.
func Run(ctx context.Context, dir string, args []string, window, timeout time.Duration, onLine OutputLine) Outcome {
	if len(args) == 0 {
		return Outcome{Err: fmt.Errorf("empty command")}
	}

	runCtx := ctx
	if window > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, window)
		defer cancel()
	} else if timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, args[0], args[1:]...)
	cmd.Dir = dir
	// Own process group: signals reach the whole tree (npm/bun → the actual
	// dev server or watcher), not just the direct child.
	setProcessGroup(cmd)
	// exec calls Cancel when runCtx ends; we ask politely first, then the
	// forceStop helper below guarantees the pipes close.
	cmd.Cancel = func() error {
		interruptProcess(cmd)
		return nil
	}
	cmd.WaitDelay = 3 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Outcome{Err: err}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return Outcome{Err: err}
	}
	if err := cmd.Start(); err != nil {
		return Outcome{Err: err}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go scanLines(stdout, onLine, &wg)
	go scanLines(stderr, onLine, &wg)

	readDone := make(chan struct{})
	go func() { wg.Wait(); close(readDone) }()

	// stop terminates the process tree and closes the pipes so the scanners
	// always unblock, even when a grandchild inherited them.
	stop := func() {
		interruptProcess(cmd)
		select {
		case <-readDone:
		case <-time.After(2 * time.Second):
			killProcess(cmd)
			_ = stdout.Close()
			_ = stderr.Close()
			<-readDone
		}
	}

	var windowC, timeoutC <-chan time.Time
	if window > 0 {
		t := time.NewTimer(window)
		defer t.Stop()
		windowC = t.C
	}
	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		timeoutC = t.C
	}

	select {
	case <-readDone:
		// The process exited on its own; fall through to reaping.
	case <-windowC:
		stop()
	case <-timeoutC:
		stop()
	case <-ctx.Done():
		stop()
	}

	err = cmd.Wait()

	switch {
	case ctx.Err() != nil:
		return Outcome{Err: ctx.Err()}
	case window > 0 && errors.Is(runCtx.Err(), context.DeadlineExceeded):
		return Outcome{Started: true}
	case timeout > 0 && errors.Is(runCtx.Err(), context.DeadlineExceeded):
		return Outcome{TimedOut: true}
	default:
		return Outcome{Err: err}
	}
}

// scanLines forwards every line read from r to onLine until EOF.
func scanLines(r io.Reader, onLine OutputLine, wg *sync.WaitGroup) {
	defer wg.Done()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		onLine(sc.Text())
	}
}
