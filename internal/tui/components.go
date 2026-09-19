package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/protorians/lior-cli/internal/i18n"
)

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
