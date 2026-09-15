package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/protorians/sentient-cli/internal/i18n"
)

// ReportFunc receives download progress updates (bytes done, bytes total).
// It is called from the background download goroutine.
type ReportFunc func(done, total int64)

// progressMsg carries a download progress update.
type progressMsg struct {
	done  int64
	total int64
}

// progressTask is the interactive model behind RunWithProgress.
type progressTask[T any] struct {
	progress progress.Model
	label    string
	progCh   chan progressMsg
	resCh    chan resultMsg[T]
	done     bool
	err      error
	value    T
	pct      float64
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
		pct := 0.0
		if msg.total > 0 {
			pct = float64(msg.done) / float64(msg.total)
			if pct > 1 {
				pct = 1
			}
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
	if m.done {
		if m.err != nil {
			return s.Error.Render("✗ ") + m.label + "\n"
		}
		return s.Success.Render("✓ ") + m.label + "\n"
	}
	return fmt.Sprintf("%s %s%3.0f%%\n",
		s.Accent.Render(m.label),
		m.progress.View(),
		m.pct*100,
	)
}

// RunWithProgress displays an animated progress bar while fn runs in the
// background. fn receives a report function it must call as its work
// progresses (done and total bytes). It returns fn's result.
// In a non-interactive context it degrades to a simple progress line on
// stderr and runs fn synchronously.
func RunWithProgress[T any](label string, fn func(report ReportFunc) (T, error)) (T, error) {
	var zero T
	if !IsInteractive() {
		msg := label
		if !strings.HasSuffix(msg, "…") && !strings.HasSuffix(msg, ".") {
			msg += " …"
		}
		fmt.Fprintln(os.Stderr, msg)
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

	m := progressTask[T]{
		progress: progress.New(progress.WithSolidFill("#c1a875")),
		label:    label,
		progCh:   progCh,
		resCh:    resCh,
	}
	m.progress.EmptyColor = "#725b2a"
	m.progress.Width = 40

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
