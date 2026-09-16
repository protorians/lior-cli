package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/protorians/sentient-cli/internal/i18n"
)

// ErrCancelled is returned by RunWithSteps when the developer aborts the
// operation (Ctrl+C or Esc, or an interrupting signal in a non-interactive
// context). Callers can match it with errors.Is to print a confirmation.
var ErrCancelled = errors.New("operation cancelled")

// Status classifies an execution step. The same vocabulary drives the live
// step trace and the end-of-run summary (severity breakdown), across every
// command that reports progress step by step.
type Status string

const (
	StatusRunning    Status = "RUNNING"
	StatusSuccess    Status = "SUCCESS"
	StatusNotice     Status = "NOTICE"
	StatusWarning    Status = "WARNING"
	StatusError      Status = "ERROR"
	StatusDeprecated Status = "DEPRECATED"
)

// statusOrder is the canonical severity order used by the summary.
var statusOrder = []Status{StatusSuccess, StatusNotice, StatusWarning, StatusError, StatusDeprecated}

// Step is a single observable stage reported by a long-running command.
//
// A step with a non-empty ID can be reported several times: the live view
// updates it in place, which lets a stage move from RUNNING to a terminal
// status while streaming its recent Output lines (a live sub-task tail).
type Step struct {
	ID     string
	Label  string
	Status Status
	Detail string
	Output []string
}

// Mark returns the glyph used to prefix a status.
func (st Status) Mark() string {
	switch st {
	case StatusRunning:
		return "…"
	case StatusSuccess:
		return "✓"
	case StatusWarning:
		return "⚠"
	case StatusError:
		return "✗"
	case StatusDeprecated:
		return "⊘"
	default:
		return "◆"
	}
}

func (s *Styles) stepStyle(status Status) lipgloss.Style {
	switch status {
	case StatusSuccess:
		return s.Success
	case StatusWarning, StatusDeprecated:
		return s.Warning
	case StatusError:
		return s.Error
	default:
		return s.Info
	}
}

// isTerminal reports whether a status is final (counted in the summary).
func (st Status) isTerminal() bool {
	return st != StatusRunning
}

// StepLine renders one step as "<mark> label — detail".
func (s *Styles) StepLine(step Step) string {
	line := s.stepStyle(step.Status).Render(step.Status.Mark() + " " + step.Label)
	if step.Detail != "" {
		line += s.Muted.Render(" — " + step.Detail)
	}
	return line
}

// Summarize counts terminal steps by status (RUNNING steps are not counted).
func Summarize(steps []Step) map[Status]int {
	counts := make(map[Status]int, len(statusOrder))
	for _, step := range steps {
		if step.Status.isTerminal() {
			counts[step.Status]++
		}
	}
	return counts
}

// SummaryBlock renders the severity breakdown of an execution: success,
// notice, warning, error and deprecated counts form the closing recap.
func (s *Styles) SummaryBlock(counts map[Status]int) string {
	lines := make([]string, 0, len(statusOrder))
	for _, st := range statusOrder {
		n := counts[st]
		// Success always shows; warning and error stay visible even at zero so
		// the recap is explicit. Notice and deprecated only surface when used.
		if n == 0 && st != StatusSuccess && st != StatusWarning && st != StatusError {
			continue
		}
		lines = append(lines, "  "+s.stepStyle(st).Render(st.Mark()+" "+i18n.Tf(summaryKey(st), n)))
	}
	if len(lines) == 0 {
		lines = append(lines, "  "+s.Muted.Render(i18n.T("tui.summary.none")))
	}
	body := s.SubHeader.Render(i18n.T("tui.summary.title")) + "\n" + strings.Join(lines, "\n")
	return s.NeutralPanel(body)
}

func summaryKey(st Status) string {
	switch st {
	case StatusSuccess:
		return "tui.summary.success"
	case StatusNotice:
		return "tui.summary.notice"
	case StatusWarning:
		return "tui.summary.warning"
	case StatusError:
		return "tui.summary.error"
	case StatusDeprecated:
		return "tui.summary.deprecated"
	default:
		return "tui.summary.notice"
	}
}

// stepReport wraps a step inside the shared Bubble Tea channel.
type stepReport struct{ step Step }

// stepsTask is the interactive model behind RunWithSteps.
type stepsTask[T any] struct {
	spinner   spinner.Model
	label     string
	steps     []Step
	ch        chan any
	cancel    context.CancelFunc
	done      bool
	cancelled bool
	err       error
	value     T
}

func (m stepsTask[T]) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, waitStepMsg(m.ch))
}

func waitStepMsg(ch chan any) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

// upsertStep updates a step in place when it carries a known ID, and appends
// it otherwise — the mechanism behind a live sub-task moving from RUNNING to
// its terminal status while streaming output.
func upsertStep(steps []Step, step Step) []Step {
	if step.ID == "" {
		return append(steps, step)
	}
	for i := range steps {
		if steps[i].ID == step.ID {
			steps[i] = step
			return steps
		}
	}
	return append(steps, step)
}

func (m stepsTask[T]) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "esc" {
			if m.cancel != nil {
				m.cancel()
			}
			m.cancelled = true
			m.done = true
			return m, tea.Quit
		}
		return m, nil
	case stepReport:
		m.steps = upsertStep(m.steps, msg.step)
		return m, waitStepMsg(m.ch)
	case resultMsg[T]:
		m.done = true
		m.err = msg.err
		m.value = msg.value
		return m, tea.Quit
	}
	return m, nil
}

func (m stepsTask[T]) View() string {
	s := NewStyles()
	var b strings.Builder

	var prefix string
	switch {
	case m.cancelled:
		prefix = s.Warning.Render("⊘")
	case m.done && m.err != nil:
		prefix = s.Error.Render("✗")
	case m.done:
		prefix = s.Success.Render("✓")
	default:
		prefix = s.Accent.Render(m.spinner.View())
	}
	b.WriteString(prefix + " " + s.Value.Render(m.label) + "\n")
	for _, step := range m.steps {
		if step.Status == StatusRunning {
			b.WriteString("  " + s.Accent.Render(m.spinner.View()) + " " + s.Value.Render(step.Label))
			if step.Detail != "" {
				b.WriteString(s.Muted.Render(" — " + step.Detail))
			}
			b.WriteString("\n")
			for _, line := range step.Output {
				b.WriteString("      " + s.Muted.Render(line) + "\n")
			}
			continue
		}
		b.WriteString("  " + s.StepLine(step) + "\n")
	}
	if m.cancelled {
		b.WriteString("  " + s.Warning.Render(i18n.T("tui.cancelled")) + "\n")
	}
	return b.String()
}

// plainStepPrinter renders steps for non-interactive output. It tracks how
// many output lines of each identified step were already printed so a running
// sub-task streams its tail without repeating previous lines.
type plainStepPrinter struct {
	styles *Styles
	seen   map[string]int
}

func (p *plainStepPrinter) print(step Step) {
	if step.ID != "" {
		if printed, ok := p.seen[step.ID]; ok {
			for _, line := range step.Output[min(printed, len(step.Output)):] {
				fmt.Fprintln(os.Stderr, "  "+p.styles.Muted.Render(line))
			}
			p.seen[step.ID] = len(step.Output)
			if step.Status.isTerminal() {
				fmt.Fprintln(os.Stderr, "  "+p.styles.StepLine(step))
			}
			return
		}
		p.seen[step.ID] = len(step.Output)
	}
	fmt.Fprintln(os.Stderr, "  "+p.styles.StepLine(step))
	for _, line := range step.Output {
		fmt.Fprintln(os.Stderr, "  "+p.styles.Muted.Render(line))
	}
}

// RunWithSteps displays the reported execution steps live while fn runs in the
// background, then prints a final ✓ / ✗ line. fn receives a context (cancelled
// when the developer aborts with Ctrl+C / Esc) and a report function it must
// call as each step completes. When the operation is cancelled it returns
// ErrCancelled so the caller can print a confirmation.
// In a non-interactive context it degrades to plain step lines on stderr and
// runs fn synchronously, still reacting to an interrupting signal.
func RunWithSteps[T any](label string, fn func(ctx context.Context, report func(Step)) (T, error)) (T, error) {
	var zero T

	if !IsInteractive() {
		msg := label
		if !strings.HasSuffix(msg, "…") && !strings.HasSuffix(msg, ".") {
			msg += " …"
		}
		fmt.Fprintln(os.Stderr, msg)

		printer := &plainStepPrinter{styles: NewStyles(), seen: map[string]int{}}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		value, err := fn(ctx, printer.print)
		if ctx.Err() != nil {
			return zero, ErrCancelled
		}
		return value, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := make(chan any, 64)
	go func() {
		v, err := fn(ctx, func(step Step) {
			select {
			case ch <- stepReport{step: step}:
			case <-ctx.Done():
			}
		})
		select {
		case ch <- resultMsg[T]{value: v, err: err}:
		case <-ctx.Done():
		}
	}()

	m := stepsTask[T]{
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		label:   label,
		ch:      ch,
		cancel:  cancel,
	}
	m.spinner.Style = NewStyles().Accent

	p := tea.NewProgram(m)
	final, err := p.Run()
	if errors.Is(err, tea.ErrInterrupted) {
		cancel()
		return zero, ErrCancelled
	}
	if err != nil {
		return zero, err
	}
	fm, ok := final.(stepsTask[T])
	if !ok {
		return zero, errors.New(i18n.T("tui.error.spinner_unexpected"))
	}
	if fm.cancelled {
		return zero, ErrCancelled
	}
	return fm.value, fm.err
}
