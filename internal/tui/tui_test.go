package tui

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
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

func TestInputModelTabFillsPlaceholder(t *testing.T) {
	m := inputModel{title: "URL", input: textinput.New()}
	m.input.Placeholder = "/org/test"

	m = sendKeyMsg(t, m, tea.KeyMsg{Type: tea.KeyTab}).(inputModel)
	if got := m.input.Value(); got != "/org/test" {
		t.Errorf("tab must fill the placeholder, value = %q, want %q", got, "/org/test")
	}
}

func TestInputModelTabKeepsTypedValue(t *testing.T) {
	m := inputModel{title: "URL", input: textinput.New()}
	m.input.Placeholder = "/org/test"
	m.input.SetValue("/custom")

	m = sendKeyMsg(t, m, tea.KeyMsg{Type: tea.KeyTab}).(inputModel)
	if got := m.input.Value(); got != "/custom" {
		t.Errorf("tab must not overwrite a typed value, value = %q, want %q", got, "/custom")
	}
}

func TestInputModelTabIgnoresSecretPlaceholder(t *testing.T) {
	m := inputModel{title: "Token", input: textinput.New(), secret: true}
	m.input.Placeholder = "••••••••"

	m = sendKeyMsg(t, m, tea.KeyMsg{Type: tea.KeyTab}).(inputModel)
	if got := m.input.Value(); got != "" {
		t.Errorf("tab must not fill a secret placeholder, value = %q, want empty", got)
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

func TestSelectViewShowsQuestionAsTitle(t *testing.T) {
	m := newSelectModel("Choisir un gestionnaire de paquets", []string{"bun", "pnpm", "yarn"})
	view := m.View()
	if !strings.Contains(view, "Choisir un gestionnaire de paquets") {
		t.Errorf("the question must be shown as the screen title, got: %q", view)
	}
	if strings.Contains(view, "List") {
		t.Errorf("the bubbles list must not add its generic title, got: %q", view)
	}
}

func TestSelectViewFitsTerminal(t *testing.T) {
	items := make([]string, 20)
	for i := range items {
		items[i] = "module-" + strconv.Itoa(i)
	}
	m := newSelectModel("Sélectionnez le module", items)

	const height = 12
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: height})
	view := next.(selectModel).View()
	// Count lines the way bubbletea does: a trailing newline is a real line.
	lines := strings.Count(view, "\n") + 1
	if lines > height {
		t.Errorf("the select screen must fit the terminal: %d lines for height %d", lines, height)
	}
	if first := strings.SplitN(view, "\n", 2)[0]; !strings.Contains(first, "Sélectionnez le module") {
		t.Errorf("the question must stay visible on the first line, got: %q", first)
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

func TestRunWithStepsNonInteractive(t *testing.T) {
	// Without a terminal the runner degrades to a blocking synchronous call
	// and still forwards every reported step.
	var got []Step
	value, err := RunWithSteps("Task", func(ctx context.Context, report func(Step)) (int, error) {
		step := Step{Label: "Validation", Status: StatusSuccess}
		got = append(got, step)
		report(step)
		return 42, nil
	})
	if err != nil {
		t.Fatalf("RunWithSteps: %v", err)
	}
	if value != 42 || len(got) != 1 {
		t.Errorf("value = %d, steps = %d, want 42 and 1", value, len(got))
	}
}

func TestRunWithStepsNonInteractivePropagatesError(t *testing.T) {
	_, err := RunWithSteps("Task", func(ctx context.Context, report func(Step)) (int, error) {
		return 0, errors.New("boom")
	})
	if err == nil || err.Error() != "boom" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestStepsTaskCancelsOnCtrlC(t *testing.T) {
	cancelled := false
	m := stepsTask[int]{cancel: func() { cancelled = true }}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	fm, ok := next.(stepsTask[int])
	if !ok {
		t.Fatalf("type = %T", next)
	}
	if !fm.cancelled || !fm.done {
		t.Errorf("ctrl+c must mark the task cancelled and done (cancelled=%v done=%v)", fm.cancelled, fm.done)
	}
	if !cancelled {
		t.Error("ctrl+c must invoke the context cancel function")
	}
	if view := fm.View(); !strings.Contains(view, "⊘") {
		t.Errorf("cancelled view must show the cancel mark, got: %q", view)
	}
}

func TestStepsTaskCancelsOnEsc(t *testing.T) {
	cancelled := false
	m := stepsTask[int]{cancel: func() { cancelled = true }}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	fm := next.(stepsTask[int])
	if !fm.cancelled || !cancelled {
		t.Error("esc must cancel the operation")
	}
}

func TestUpsertStepUpdatesByID(t *testing.T) {
	steps := []Step{{ID: "build", Label: "Build", Status: StatusRunning}}
	steps = upsertStep(steps, Step{ID: "build", Label: "Build", Status: StatusSuccess, Detail: "done"})
	if len(steps) != 1 {
		t.Fatalf("identified step must be updated in place, got %d steps", len(steps))
	}
	if steps[0].Status != StatusSuccess || steps[0].Detail != "done" {
		t.Errorf("step not updated: %+v", steps[0])
	}

	steps = upsertStep(steps, Step{ID: "other", Label: "Other", Status: StatusNotice})
	if len(steps) != 2 {
		t.Fatalf("new id must append, got %d steps", len(steps))
	}
}

func TestSummarizeIgnoresRunning(t *testing.T) {
	counts := Summarize([]Step{
		{Status: StatusRunning},
		{Status: StatusSuccess},
	})
	if counts[StatusRunning] != 0 {
		t.Errorf("running steps must not be counted, got %d", counts[StatusRunning])
	}
	if counts[StatusSuccess] != 1 {
		t.Errorf("terminal steps must be counted, got %d", counts[StatusSuccess])
	}
}

func TestSummarizeCountsByStatus(t *testing.T) {
	steps := []Step{
		{Status: StatusSuccess},
		{Status: StatusSuccess},
		{Status: StatusNotice},
		{Status: StatusWarning},
		{Status: StatusError},
		{Status: StatusDeprecated},
	}
	counts := Summarize(steps)
	want := map[Status]int{
		StatusSuccess: 2, StatusNotice: 1, StatusWarning: 1,
		StatusError: 1, StatusDeprecated: 1,
	}
	for status, n := range want {
		if counts[status] != n {
			t.Errorf("counts[%s] = %d, want %d", status, counts[status], n)
		}
	}
}

func TestSummaryBlockShowsSeverities(t *testing.T) {
	s := NewStyles()
	block := s.SummaryBlock(map[Status]int{StatusSuccess: 3, StatusWarning: 1, StatusError: 1})
	for _, want := range []string{"3", "1", "✓", "⚠", "✗"} {
		if !strings.Contains(block, want) {
			t.Errorf("summary block missing %q: %q", want, block)
		}
	}
	if strings.Contains(block, "deprecated") {
		t.Errorf("zero-count deprecated must stay hidden: %q", block)
	}
}

func TestStatusMarks(t *testing.T) {
	cases := map[Status]string{
		StatusSuccess: "✓", StatusWarning: "⚠", StatusError: "✗",
		StatusDeprecated: "⊘", StatusNotice: "◆",
	}
	for status, mark := range cases {
		if got := status.Mark(); got != mark {
			t.Errorf("Status(%s).Mark() = %q, want %q", status, got, mark)
		}
	}
}

func TestProgressTaskView(t *testing.T) {
	pg := NewStyles().ProgressBar(10)
	m := progressTask[int]{
		progress: pg,
		label:    "Downloading template",
		pct:      0.5,
	}
	view := m.View()
	if !strings.Contains(view, "50%") {
		t.Errorf("mid-progress view must show 50%%, got: %q", view)
	}
	if n := strings.Count(view, "%"); n != 1 {
		t.Errorf("the percentage must be rendered once, got %d in: %q", n, view)
	}

	m.done = true
	m.err = errors.New("boom")
	if v := m.View(); !strings.Contains(v, "✗") {
		t.Errorf("error view must show ✗, got: %q", v)
	}
}

func TestProgressLineRendersLabelAndPercentOnce(t *testing.T) {
	s := NewStyles()
	line := s.ProgressLine("Downloading", "████░░░░", 0.5)
	if !strings.Contains(line, "Downloading") {
		t.Errorf("line must contain the label, got: %q", line)
	}
	if n := strings.Count(line, "%"); n != 1 {
		t.Errorf("percentage must appear exactly once, got %d in: %q", n, line)
	}
}

func TestProgressBarClampsWidth(t *testing.T) {
	s := NewStyles()
	if w := s.ProgressBar(0).Width; w != progressBarMinWidth {
		t.Errorf("zero width must fall back to the minimum, got %d", w)
	}
	if w := s.ProgressBar(12).Width; w != 12 {
		t.Errorf("explicit width must be honoured, got %d", w)
	}
}
