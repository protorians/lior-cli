package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// defaultContentWidth is the fallback interior width of panels when the
// terminal size cannot be measured (e.g. piped or scripted output).
const defaultContentWidth = 80

// Brand palette (spec §9.2): a quiet two-stop sage/olive identity tuned for
// dark and light terminals. The two brand stops are:
//
//	#c1a875 — sage, the primary accent (light stop, reads on dark)
//	#725b2a — deep olive, the secondary stop (dark ink, reads on light)
//
// foreground is adapted per mode: body text follows the terminal background
// (sage on dark, olive on light) and any ink sitting on a colored surface uses
// the opposite stop so contrast is preserved in both themes.
type palettes struct {
	success string
	error   string
	warning string
	info    string
	muted   string
	accent  string

	text   string
	border string
	soft   string // subtle surface tint used for banded rows and chips
	gray   string // neutral gray used for help bars and dimmed headers
}

var darkPalette = palettes{
	success: "#c1a875",
	error:   "#c1a875",
	warning: "#c1a875",
	info:    "#c1a875",
	muted:   "#c1a875",
	accent:  "#c1a875",
	text:    "#c1a875",
	border:  "#c1a875",
	soft:    "#725b2a",
	gray:    "#c1a875",
}

var lightPalette = palettes{
	success: "#725b2a",
	error:   "#725b2a",
	warning: "#725b2a",
	info:    "#725b2a",
	muted:   "#725b2a",
	accent:  "#725b2a",
	text:    "#725b2a",
	border:  "#725b2a",
	soft:    "#c1a875",
	gray:    "#725b2a",
}

// Styles holds the themed lipgloss styles used across the CLI.
type Styles struct {
	palette palettes

	// contentWidth is the maximum width of a panel's interior, derived from
	// the terminal size. Lines longer than this are wrapped so they never
	// overflow the panel's frame.
	contentWidth int

	Success lipgloss.Style
	Error   lipgloss.Style
	Warning lipgloss.Style
	Info    lipgloss.Style
	Muted   lipgloss.Style
	Accent  lipgloss.Style

	Title        lipgloss.Style
	Header       lipgloss.Style
	SubHeader    lipgloss.Style
	Key          lipgloss.Style
	Value        lipgloss.Style
	Hint         lipgloss.Style
	Bullet       lipgloss.Style
	Item         lipgloss.Style
	SelectedItem lipgloss.Style
	DimmedItem   lipgloss.Style
	Help         lipgloss.Style
	Divider      lipgloss.Style
	TableHeader  lipgloss.Style
	TableRow     lipgloss.Style
	Focus        lipgloss.Style
	ErrorBar     lipgloss.Style
	DimTitle     lipgloss.Style
}

// NewStyles builds the style set for the current terminal background.
func NewStyles() *Styles {
	var p palettes
	if lipgloss.HasDarkBackground() {
		p = darkPalette
	} else {
		p = lightPalette
	}

	accent := lipgloss.Color(p.accent)

	// Measure the terminal so framed panels can wrap long lines instead of
	// overflowing. Falls back to a fixed width when the size is unknown
	// (piped or scripted output).
	contentWidth := defaultContentWidth
	if w, _, err := term.GetSize(int(os.Stderr.Fd())); err == nil && w > 6 {
		contentWidth = w - 6 // room for the two border cells and the L/R padding
	}

	return &Styles{
		palette:      p,
		contentWidth: contentWidth,

		Success: lipgloss.NewStyle().Foreground(lipgloss.Color(p.success)).Bold(true),
		Error:   lipgloss.NewStyle().Foreground(lipgloss.Color(p.error)).Bold(true),
		Warning: lipgloss.NewStyle().Foreground(lipgloss.Color(p.warning)).Bold(true),
		Info:    lipgloss.NewStyle().Foreground(lipgloss.Color(p.info)),
		Muted:   lipgloss.NewStyle().Foreground(lipgloss.Color(p.muted)),
		Accent:  lipgloss.NewStyle().Foreground(accent).Bold(true),

		Title:     lipgloss.NewStyle().Bold(true).Foreground(accent).MarginBottom(1),
		Header:    lipgloss.NewStyle().Bold(true).Foreground(accent),
		SubHeader: lipgloss.NewStyle().Bold(true).Foreground(accent),
		Key:       lipgloss.NewStyle().Foreground(lipgloss.Color(p.muted)),
		Value:     lipgloss.NewStyle().Foreground(lipgloss.Color(p.text)),
		Hint:      lipgloss.NewStyle().Foreground(lipgloss.Color(p.muted)).Faint(true),
		Bullet:    lipgloss.NewStyle().Foreground(accent),
		Item:      lipgloss.NewStyle().PaddingLeft(2),
		SelectedItem: lipgloss.NewStyle().
			PaddingLeft(2).
			Foreground(accent).
			Bold(true),
		DimmedItem: lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color(p.muted)),
		Help:       lipgloss.NewStyle().Foreground(lipgloss.Color(p.muted)).MarginTop(1),
		Divider:    lipgloss.NewStyle().Foreground(lipgloss.Color(p.muted)).Faint(true),
		TableHeader: lipgloss.NewStyle().
			Bold(true).
			Foreground(accent).
			Background(lipgloss.Color(p.soft)).
			Padding(0, 1),
		TableRow: lipgloss.NewStyle().Foreground(lipgloss.Color(p.muted)).Padding(0, 1),
		Focus:    lipgloss.NewStyle().Bold(true).Foreground(accent),
		ErrorBar: lipgloss.NewStyle().
			Background(lipgloss.Color(p.error)).
			Foreground(lipgloss.Color(p.soft)).
			Padding(0, 1),
		DimTitle: lipgloss.NewStyle().Foreground(lipgloss.Color(p.gray)),
	}
}

// ColorSeverity returns a styled ✓ / ⚠ / ✗ prefix for a level.
func (s *Styles) ColorSeverity(severity string) lipgloss.Style {
	switch severity {
	case "OK":
		return s.Success
	case "WARNING":
		return s.Warning
	default:
		return s.Error
	}
}

// KeyValue renders a right-aligned-free "label : value" line, keeping the
// exact spacing contract used across the CLI and its E2E expectations.
func (s *Styles) KeyValue(key, value string) string {
	return "  " + s.Key.Render(key) + " : " + value
}

// Heading renders a section heading followed by a soft rule.
func (s *Styles) Heading(title string) string {
	return s.SubHeader.Render(title) + "\n" + s.Rule(len(title)+10, s.palette.border)
}

// Rule renders a horizontal rule in the given color.
func (s *Styles) Rule(width int, color string) string {
	if width <= 0 {
		width = 40
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(color)).
		Render(strings.Repeat("─", width))
}

// frame wraps content in a soft, rounded frame tinted with the given color.
// Lines wider than the terminal are wrapped so they stay inside the frame.
func (s *Styles) frame(color, content string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(color)).
		Padding(0, 2).
		Render(s.wrapContent(content))
}

// wrapContent wraps any line that is wider than the panel's content width so
// the frame never overflows the terminal. Lines that already fit are left
// untouched, keeping short panels compact.
func (s *Styles) wrapContent(content string) string {
	if s.contentWidth <= 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if lipgloss.Width(line) > s.contentWidth {
			lines[i] = lipgloss.NewStyle().Width(s.contentWidth).Render(line)
		}
	}
	return strings.Join(lines, "\n")
}

// SuccessPanel wraps content in a success-tinted rounded frame.
func (s *Styles) SuccessPanel(content string) string {
	return s.frame(s.palette.success, content)
}

// NeutralPanel wraps content in a subtle rounded frame.
func (s *Styles) NeutralPanel(content string) string {
	return s.frame(s.palette.border, content)
}

// ErrorPanel wraps content in an error-tinted rounded frame.
func (s *Styles) ErrorPanel(content string) string {
	return s.frame(s.palette.error, content)
}

// WarningPanel wraps content in a warning-tinted rounded frame.
func (s *Styles) WarningPanel(content string) string {
	return s.frame(s.palette.warning, content)
}

// InfoPanel wraps content in an info-tinted rounded frame.
func (s *Styles) InfoPanel(content string) string {
	return s.frame(s.palette.info, content)
}
