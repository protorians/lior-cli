package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// tablePad is the number of spaces kept on each side of a cell.
const tablePad = 1

// Table renders a rounded, banded, align-left table.
type Table struct {
	Headers []string
	Rows    [][]string
	styles  *Styles
}

// NewTable allocates a table.
func NewTable(headers []string) *Table {
	return &Table{Headers: headers, styles: NewStyles()}
}

// AddRow appends a row (length must match len(headers)).
func (t *Table) AddRow(cells ...string) {
	if len(cells) != len(t.Headers) {
		return
	}
	row := make([]string, len(cells))
	copy(row, cells)
	t.Rows = append(t.Rows, row)
}

// Render produces the printable table.
func (t *Table) Render() string {
	n := len(t.Headers)
	widths := make([]int, n)
	for i, h := range t.Headers {
		widths[i] = lipgloss.Width(h)
	}
	for _, row := range t.Rows {
		for i, cell := range row {
			if i < n && lipgloss.Width(cell) > widths[i] {
				widths[i] = lipgloss.Width(cell)
			}
		}
	}

	total := n + 1
	for i := 0; i < n; i++ {
		total += widths[i] + 2*tablePad
	}

	var b strings.Builder
	b.WriteString("╭" + strings.Repeat("─", total-2) + "╮\n")
	b.WriteString(t.renderRow(t.Headers, widths, true, false))
	b.WriteString("├" + strings.Repeat("─", total-2) + "┤\n")
	for r, row := range t.Rows {
		b.WriteString(t.renderRow(row, widths, false, r%2 == 1))
	}
	b.WriteString("╰" + strings.Repeat("─", total-2) + "╯\n")
	return b.String()
}

func (t *Table) renderRow(cells []string, widths []int, header, alt bool) string {
	var b strings.Builder
	b.WriteString("│")
	accent := lipgloss.Color(t.styles.palette.accent)
	soft := lipgloss.Color(t.styles.palette.soft)

	for i, cell := range cells {
		if i >= len(widths) {
			break
		}
		inner := lipgloss.NewStyle().
			Width(widths[i]+2*tablePad).
			Padding(0, tablePad).
			Render(cell)
		var style lipgloss.Style
		switch {
		case header:
			style = lipgloss.NewStyle().
				Bold(true).
				Foreground(accent).
				Background(soft)
		case alt:
			style = lipgloss.NewStyle().
				Foreground(accent).
				Background(soft)
		default:
			style = lipgloss.NewStyle().Foreground(lipgloss.Color(t.styles.palette.muted))
		}
		b.WriteString(style.Render(inner))
	}
	b.WriteString("│\n")
	return b.String()
}
