package runner

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestRunCapturesOutput verifies that both stdout and stderr lines reach the
// output callback as they are produced.
func TestRunCapturesOutput(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	out := Run(context.Background(), t.TempDir(),
		[]string{"sh", "-c", "echo first; echo second"},
		0, 5*time.Second, func(s string) {
			mu.Lock()
			lines = append(lines, s)
			mu.Unlock()
		})
	if out.Err != nil {
		t.Fatalf("outcome = %+v, want success", out)
	}
	got := strings.Join(lines, "\n")
	for _, want := range []string{"first", "second"} {
		if !strings.Contains(got, want) {
			t.Errorf("sortie non capturée %q : %v", want, lines)
		}
	}
}

// TestRunStartWindowStopsScript verifies that a process still running after
// the start window is stopped and reported as Started.
func TestRunStartWindowStopsScript(t *testing.T) {
	out := Run(context.Background(), t.TempDir(),
		[]string{"sh", "-c", "sleep 30"}, 500*time.Millisecond, 0, func(string) {})
	if !out.Started {
		t.Fatalf("outcome = %+v, want Started", out)
	}
}

// TestRunTimeoutMarksTimedOut verifies that a one-shot command killed after
// its hard cap is reported as TimedOut.
func TestRunTimeoutMarksTimedOut(t *testing.T) {
	out := Run(context.Background(), t.TempDir(),
		[]string{"sh", "-c", "sleep 30"}, 0, 300*time.Millisecond, func(string) {})
	if !out.TimedOut {
		t.Fatalf("outcome = %+v, want TimedOut", out)
	}
}

// TestRunReportsExitError returns the command exit error on failure.
func TestRunReportsExitError(t *testing.T) {
	out := Run(context.Background(), t.TempDir(),
		[]string{"sh", "-c", "exit 3"}, 0, 0, func(string) {})
	if out.Err == nil {
		t.Fatal("outcome = no error, want non-zero exit error")
	}
}
