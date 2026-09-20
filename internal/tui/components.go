package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/lipgloss"
	"github.com/jetbrains/lior-cli/internal/i18n"
)

// progressBarMinWidth and progressBarMaxWidth bound the progress bar so it
// stays readable on narrow terminals and never dominates wide ones.
const (
	progressBarMinWidth = 20
	progressBarMaxWidth = 50
)

// ProgressBar returns a themed progress bar of the given width. It is the
// single source of truth for the progress bar look across the CLI: an accent
// fill over a soft empty track, with the numeric percentage rendered by
// ProgressLine rather than by the bar itself.
func (s *Styles) ProgressBar(width int) progress.Model {
	if width <= 0 {
		width = progressBarMinWidth
	}
	m := progress.New(
		progress.WithSolidFill(s.palette.accent),
		progress.WithWidth(width),
		progress.WithoutPercentage(),
	)
	m.EmptyColor = s.palette.soft
	return m
}

// ProgressLine renders a labeled progress bar across two lines: the label
// first, then the bar itself followed by a right-aligned percentage.
//
//	Downloading the release
//	████████████░░░░░░░░░░░░  62%
func (s *Styles) ProgressLine(label, bar string, pct float64) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	return s.Value.Render(label) + "\n" +
		bar + " " + s.Muted.Render(fmt.Sprintf("%3.0f%%", pct*100))
}

// progressWidth returns the bar width that best fits the current terminal,
// clamped to a comfortable range.
func (s *Styles) progressWidth() int {
	w := s.contentWidth - 8
	if w < progressBarMinWidth {
		w = progressBarMinWidth
	}
	if w > progressBarMaxWidth {
		w = progressBarMaxWidth
	}
	return w
}

// SummaryCard wraps a success headline and its metadata block in a soft,
// rounded success frame — the visual signature of a completed command.
func (s *Styles) SummaryCard(headline string, lines ...string) string {
	body := strings.TrimRight(strings.Join(lines, "\n"), "\n")
	if body == "" {
		return s.SuccessPanel(headline)
	}
	return s.SuccessPanel(headline + "\n\n" + body)
}

// Wordmark renders the brand badge used at the top of the CLI help screens
// (copper "◉" + plum/cream name), Expo- and Tailwind-style.
func (s *Styles) Wordmark() string {
	badge := lipgloss.NewStyle().
		Background(lipgloss.Color(s.palette.accent)).
		Foreground(lipgloss.Color(s.palette.soft)).
		Bold(true).
		Padding(0, 1).
		SetString("◉").
		Render()
	name := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(s.palette.accent)).
		Render("liorian")
	return badge + " " + name
}

// StepsList renders a "next steps" block under a soft heading + rule.
func (s *Styles) StepsList(title string, steps ...string) string {
	var b strings.Builder
	b.WriteString(s.SubHeader.Render(title))
	b.WriteString("\n")
	b.WriteString(s.Rule(6, s.palette.border))
	b.WriteString("\n")
	for _, step := range steps {
		b.WriteString("  ")
		b.WriteString(s.bulletMark())
		b.WriteString(" ")
		b.WriteString(step)
		b.WriteString("\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// LogsBlock renders log lines inside a neutral frame with a small heading.
func (s *Styles) LogsBlock(title string, logs []string) string {
	var b strings.Builder
	b.WriteString(s.Hint.Render(title))
	b.WriteString("\n")
	for _, l := range logs {
		b.WriteString("  " + l + "\n")
	}
	return s.NeutralPanel(strings.TrimSuffix(b.String(), "\n"))
}

func (s *Styles) bulletMark() string {
	return s.Bullet.Render("•")
}

// NavBar renders the navigation hints shown below option lists
// (arrow keys, enter, esc), localized and tuned in the muted gray.
func (s *Styles) NavBar() string {
	item := lipgloss.NewStyle().Foreground(lipgloss.Color(s.palette.gray))
	sep := lipgloss.NewStyle().Foreground(lipgloss.Color(s.palette.border)).Render("·")
	return item.Render(i18n.T("tui.nav.updown")) + " " + sep + " " +
		item.Render(i18n.T("tui.nav.enter")) + " " + sep + " " +
		item.Render(i18n.T("tui.nav.esc"))
}
