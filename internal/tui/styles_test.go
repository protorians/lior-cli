package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestWrapContentLeavesCompactLinesIntact(t *testing.T) {
	s := NewStyles()
	if s.contentWidth <= 0 {
		t.Fatalf("contentWidth = %d, want > 0", s.contentWidth)
	}
	in := "short line\nanother short line"
	if got := s.wrapContent(in); got != in {
		t.Errorf("wrapContent(%q) = %q, want unchanged for short lines", in, got)
	}
}

func TestWrapContentWrapsWideLines(t *testing.T) {
	s := NewStyles()
	// Force a narrow, known interior so the test does not depend on the
	// measured terminal size.
	s.contentWidth = 30

	in := "short line\n" + strings.Repeat("word ", 30)
	got := s.wrapContent(in)

	lines := strings.Split(got, "\n")
	if !strings.Contains(lines[0], "short line") {
		t.Errorf("leading short line not preserved: %q", got)
	}
	for _, line := range lines[1:] {
		if lipgloss.Width(line) > s.contentWidth {
			t.Errorf("wrapped line width %d > %d: %q", lipgloss.Width(line), s.contentWidth, line)
		}
	}
}
