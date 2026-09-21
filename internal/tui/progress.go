package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/protorians/lior-cli/internal/i18n"
)

// ReportFunc receives download progress updates (bytes done, bytes total).
// It is called from the background download goroutine.
type ReportFunc func(done, total int64)

// indeterminateCeiling is the highest fraction the bar reaches when the server
// does not advertise a total size. The bar eases toward it and never wraps back
// to zero, so the progression only ever moves forward.
const indeterminateCeiling = 0.95

// indeterminateEasing is the fraction of the remaining distance to the ceiling
// the bar covers on each streamed chunk when no total size is known. Combined
// with indeterminateCeiling it yields an asymptotic, monotonic animation.
const indeterminateEasing = 0.03

// progressMsg carries a download progress update.
type progressMsg struct {
	done  int64
	total int64
}

// progressTask is the interactive model behind RunWithProgress.
type progressTask[T any] struct {
	progress progress.Model
	label    string
	detail   string
	progCh   chan progressMsg
	resCh    chan resultMsg[T]
	done     bool
	err      error
	value    T
	pct      float64

	// indeterminate is set when the server did not advertise a total size
	// (e.g. a chunked response): the bar then eases forward and reports the
	// downloaded byte count instead of a percentage.
	indeterminate bool
	sweep         float64
	bytes         int64
}

func (m progressTask[T]) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg { return <-m.progCh },
		func() tea.Msg { return <-m.resCh },
	)
}

func (m progressTask[T]) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case progress.FrameMsg:
		pm, cmd := m.progress.Update(msg)
		if pm, ok := pm.(progress.Model); ok {
			m.progress = pm
		}
		return m, cmd
	case progressMsg:
		m.bytes = msg.done
		// Without a total size (chunked response, mirror without
		// Content-Length) a percentage cannot be computed. Keep the bar
		// moving in step with the streamed bytes instead of pinning it at 0%.
		if msg.total <= 0 {
			m.indeterminate = true
			// Ease toward the ceiling instead of wrapping to zero: the bar
			// must only ever move forward, otherwise it reads as an abnormal
			// "percentage goes up then down" flicker.
			m.sweep += (indeterminateCeiling - m.sweep) * indeterminateEasing
			if m.sweep > indeterminateCeiling {
				m.sweep = indeterminateCeiling
			}
			return m, tea.Batch(
				m.progress.SetPercent(m.sweep),
				func() tea.Msg { return <-m.progCh },
			)
		}
		m.indeterminate = false
		pct := float64(msg.done) / float64(msg.total)
		if pct > 1 {
			pct = 1
		}
		m.pct = pct
		return m, tea.Batch(
			m.progress.SetPercent(pct),
			func() tea.Msg { return <-m.progCh },
		)
	case resultMsg[T]:
		m.done = true
		m.err = msg.err
		m.value = msg.value
		return m, tea.Batch(m.progress.SetPercent(1), tea.Quit)
	}
	return m, nil
}

func (m progressTask[T]) View() string {
	s := NewStyles()
	detail := ""
	if m.detail != "" {
		detail = "\n" + s.Muted.Render(m.detail)
	}
	if m.done {
		if m.err != nil {
			return s.Error.Render("✗ ") + m.label + detail + "\n"
		}
		return s.Success.Render("✓ ") + m.label + detail + "\n"
	}
	if m.indeterminate {
		return s.ProgressLineBytes(m.label, m.progress.View(), m.bytes) + detail + "\n"
	}
	return s.ProgressLine(m.label, m.progress.View(), m.pct) + detail + "\n"
}

// RunWithProgress displays an animated progress bar while fn runs in the
// background. fn receives a report function it must call as its work
// progresses (done and total bytes). It returns fn's result.
// In a non-interactive context it degrades to a simple progress line on
// stderr and runs fn synchronously.
func RunWithProgress[T any](label string, fn func(report ReportFunc) (T, error)) (T, error) {
	return RunWithProgressDetail(label, "", fn)
}

// RunWithProgressDetail behaves like RunWithProgress and additionally renders a
// muted detail line below the progress bar (e.g. the release metadata). In a
// non-interactive context the detail is printed on the line following the
// label.
func RunWithProgressDetail[T any](label, detail string, fn func(report ReportFunc) (T, error)) (T, error) {
	var zero T
	if !IsInteractive() {
		msg := label
		if !strings.HasSuffix(msg, "…") && !strings.HasSuffix(msg, ".") {
			msg += " …"
		}
		fmt.Fprintln(os.Stderr, msg)
		if detail != "" {
			fmt.Fprintln(os.Stderr, detail)
		}
		return fn(func(done, total int64) {})
	}

	progCh := make(chan progressMsg, 32)
	resCh := make(chan resultMsg[T], 1)
	go func() {
		v, err := fn(func(done, total int64) {
			progCh <- progressMsg{done: done, total: total}
		})
		resCh <- resultMsg[T]{value: v, err: err}
	}()

	s := NewStyles()
	m := progressTask[T]{
		progress: s.ProgressBar(s.progressWidth()),
		label:    label,
		detail:   detail,
		progCh:   progCh,
		resCh:    resCh,
	}

	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return zero, err
	}
	fm, ok := final.(progressTask[T])
	if !ok {
		return zero, errors.New(i18n.T("tui.error.spinner_unexpected"))
	}
	return fm.value, fm.err
}
