package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jetbrains/lior-cli/internal/i18n"
)

// resultMsg carries the outcome of the background task.
type resultMsg[T any] struct {
	value T
	err   error
}

// spinnerTask is the interactive model behind RunWithSpinner.
type spinnerTask[T any] struct {
	spinner spinner.Model
	label   string
	ch      chan resultMsg[T]
	done    bool
	err     error
	value   T
}

func (m spinnerTask[T]) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, func() tea.Msg {
		return <-m.ch
	})
}

func (m spinnerTask[T]) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case resultMsg[T]:
		m.done = true
		m.err = msg.err
		m.value = msg.value
		return m, tea.Quit
	}
	return m, nil
}

func (m spinnerTask[T]) View() string {
	s := NewStyles()
	if m.done {
		if m.err != nil {
			return s.Error.Render("✗ ") + m.label + "\n"
		}
		return s.Success.Render("✓ ") + m.label + "\n"
	}
	return s.Accent.Render(m.spinner.View()) + " " + s.Value.Render(m.label) + " …\n"
}

// RunWithSpinner displays an animated spinner while fn runs in the
// background, then prints a final ✓ / ✗ line. It returns fn's result.
// In a non-interactive context it degrades to a simple progress line on
// stderr and runs fn synchronously.
func RunWithSpinner[T any](label string, fn func() (T, error)) (T, error) {
	var zero T
	if !IsInteractive() {
		msg := label
		if !strings.HasSuffix(msg, "…") && !strings.HasSuffix(msg, ".") {
			msg += " …"
		}
		fmt.Fprintln(os.Stderr, msg)
		return fn()
	}

	ch := make(chan resultMsg[T], 1)
	go func() {
		v, err := fn()
		ch <- resultMsg[T]{value: v, err: err}
	}()

	m := spinnerTask[T]{
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		label:   label,
		ch:      ch,
	}
	m.spinner.Style = NewStyles().Accent

	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return zero, err
	}
	fm, ok := final.(spinnerTask[T])
	if !ok {
		return zero, errors.New(i18n.T("tui.error.spinner_unexpected"))
	}
	return fm.value, fm.err
}
