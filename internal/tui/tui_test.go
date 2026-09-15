package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func sendKeyMsg(t *testing.T, m tea.Model, msg tea.Msg) tea.Model {
	t.Helper()
	next, _ := m.Update(msg)
	return next
}

func enterKey() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyEnter}
}

func escKey() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyEsc}
}

func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func TestInputModelAcceptsTypedChars(t *testing.T) {
	m := inputModel{title: "Nom", input: textinput.New()}

	for _, r := range "blog" {
		m = sendKeyMsg(t, m, runeKey(r)).(inputModel)
	}
	if got := m.input.Value(); got != "blog" {
		t.Errorf("value = %q, want %q (the field must accept typed input)", got, "blog")
	}
}

func TestInputModelEnterReturnsValue(t *testing.T) {
	m := inputModel{title: "Nom", input: textinput.New()}
	m.input.SetValue("blog-manager")

	next := sendKeyMsg(t, m, enterKey())
	fm, ok := next.(inputModel)
	if !ok {
		t.Fatalf("type = %T", next)
	}
	if fm.result != "blog-manager" {
		t.Errorf("result = %q, want blog-manager", fm.result)
	}
}

func TestInputModelCancelsOnEsc(t *testing.T) {
	m := inputModel{title: "Nom", input: textinput.New()}
	m.input.SetValue("blog")

	next := sendKeyMsg(t, m, escKey())
	fm, ok := next.(inputModel)
	if !ok {
		t.Fatalf("type = %T", next)
	}
	if !fm.cancel {
		t.Error("esc should cancel the prompt")
	}
}

func TestConfirmModelYesAndNo(t *testing.T) {
	yes := sendKeyMsg(t, confirmModel{title: "Confirmer", defYes: false}, runeKey('y'))
	if y := yes.(confirmModel); !y.result {
		t.Error("y key should confirm")
	}

	no := sendKeyMsg(t, confirmModel{title: "Confirmer", defYes: true}, runeKey('n'))
	if n := no.(confirmModel); n.result {
		t.Error("n key should reject")
	}
}

func TestConfirmModelEnterUsesDefault(t *testing.T) {
	m := confirmModel{title: "Confirmer", defYes: true}
	next := sendKeyMsg(t, m, enterKey())
	if !next.(confirmModel).result {
		t.Error("enter should keep the default (true)")
	}
}

func TestSelectModelEnterReturnsSelection(t *testing.T) {
	items := []list.Item{
		selectItem{label: "alpha"},
		selectItem{label: "beta"},
		selectItem{label: "gamma"},
	}
	l := list.New(items, list.NewDefaultDelegate(), 24, 5)
	m := selectModel{title: "Sélection", list: l}

	next := sendKeyMsg(t, m, enterKey())
	fm, ok := next.(selectModel)
	if !ok {
		t.Fatalf("type = %T", next)
	}
	if fm.result != "alpha" {
		t.Errorf("result = %q, want alpha (first item)", fm.result)
	}
}

func TestSelectModelDownThenEnter(t *testing.T) {
	items := []list.Item{
		selectItem{label: "alpha"},
		selectItem{label: "beta"},
	}
	l := list.New(items, list.NewDefaultDelegate(), 24, 5)
	m := selectModel{title: "Sélection", list: l}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(tea.KeyMsg{Type: tea.KeyDown})
	if cmd != nil {
		_ = cmd
	}

	next, _ := m.Update(enterKey())
	fm := next.(selectModel)
	if fm.result != "beta" {
		t.Errorf("result = %q, want beta after ↓", fm.result)
	}
}

func TestRunWithSpinnerNonInteractive(t *testing.T) {
	// In a non-interactive environment the spinner degrades to a blocking
	// synchronous call.
	value, err := RunWithSpinner("Task", func() (int, error) {
		return 42, nil
	})
	if err != nil {
		t.Fatalf("RunWithSpinner: %v", err)
	}
	if value != 42 {
		t.Errorf("value = %d, want 42", value)
	}

	_, err = RunWithSpinner("Task", func() (int, error) {
		return 0, errors.New("boom")
	})
	if err == nil || err.Error() != "boom" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunWithProgressNonInteractive(t *testing.T) {
	// Without a terminal the progress bar degrades to a blocking synchronous
	// call; the report callback must be safe to call but is a no-op.
	value, err := RunWithProgress("Download", func(report ReportFunc) (int, error) {
		report(512, 1024)
		report(1024, 1024)
		return 7, nil
	})
	if err != nil {
		t.Fatalf("RunWithProgress: %v", err)
	}
	if value != 7 {
		t.Errorf("value = %d, want 7", value)
	}

	_, err = RunWithProgress("Download", func(report ReportFunc) (int, error) {
		return 0, errors.New("boom")
	})
	if err == nil || err.Error() != "boom" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProgressTaskView(t *testing.T) {
	pg := progress.New(progress.WithSolidFill("#c1a875"))
	pg.Width = 10
	m := progressTask[int]{
		progress: pg,
		label:    "Downloading template",
		pct:      0.5,
	}
	view := m.View()
	if !strings.Contains(view, "50%") {
		t.Errorf("mid-progress view must show 50%%, got: %q", view)
	}

	m.done = true
	m.err = errors.New("boom")
	if v := m.View(); !strings.Contains(v, "✗") {
		t.Errorf("error view must show ✗, got: %q", v)
	}
}
