package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// reportWidth returns the printable width shared by the report headings and
// rules, falling back to the default when the terminal size is unknown.
func (s *Styles) reportWidth() int {
	if s.contentWidth > 0 {
		return s.contentWidth
	}
	return defaultContentWidth
}

// ReportHeading renders a report section title with a right-aligned verdict
// chip over a full-width rule. It is the shared header of the audit and repair
// reports, so both commands present their per-module result the same way.
//
//	Audit: mod.liorian.blog-manager                       ✗ failed
//	──────────────────────────────────────────────────────────────
func (s *Styles) ReportHeading(title, verdict string, severity Status) string {
	rule := s.Rule(s.reportWidth(), s.palette.border)
	if verdict == "" {
		return s.Header.Render(title) + "\n" + rule
	}
	chip := s.StatusChip(verdict, severity)
	gap := s.reportWidth() - lipgloss.Width(title) - lipgloss.Width(chip)
	if gap < 1 {
		gap = 1
	}
	return s.Header.Render(title) + strings.Repeat(" ", gap) + chip + "\n" + rule
}

// StatusChip renders a short, severity-tagged label (mark + text), e.g.
// "✓ passed" or "✗ failed".
func (s *Styles) StatusChip(label string, severity Status) string {
	return s.stepStyle(severity).Render(severity.Mark() + " " + label)
}

// CountsLine renders the compact pass/warning/error tally shown under a report
// section, joining only the non-zero buckets with a muted separator.
//
//	✓ 27 check(s) passed · ⚠ 1 warning(s) · ✗ 2 error(s)
func (s *Styles) CountsLine(parts ...string) string {
	sep := s.Muted.Render(" · ")
	return strings.Join(parts, sep)
}
