package tui

import (
	"errors"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jetbrains/lior-cli/internal/i18n"
)

// inputModel is a Bubble Tea model wrapping a text input.
type inputModel struct {
	title  string
	input  textinput.Model
	secret bool
	result string
	cancel bool
}

func (m inputModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m inputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// textinput ignores every keystroke while unfocused, and Init/View receive
	// copies of the model (their mutations are discarded), so focus must be
	// applied here, on the model that will be returned.
	if !m.input.Focused() {
		m.input.Focus()
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			m.result = m.input.Value()
			return m, tea.Quit
		case "ctrl+c", "esc":
			m.cancel = true
			return m, tea.Quit
		case "tab":
			// Tab accepts the placeholder as the answer: a developer who agrees
			// with the suggestion fills the field in one keystroke instead of
			// retyping it.
			if !m.secret && m.input.Value() == "" && m.input.Placeholder != "" {
				m.input.SetValue(m.input.Placeholder)
				m.input.CursorEnd()
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m inputModel) View() string {
	if m.secret {
		m.input.EchoMode = textinput.EchoPassword
		m.input.EchoCharacter = '•'
	}
	s := NewStyles()
	line := s.questionMark() + " " + s.Question.Render(m.title) + " : " + m.input.View()
	if !m.secret && m.input.Placeholder != "" {
		line += " " + s.Hint.Render("["+i18n.T("tui.input.tab")+"]")
	}
	return line + "\n"
}

// askInput runs an interactive text input prompt.
func askInput(title, placeholder string, secret bool) (string, bool, error) {
	if !IsInteractive() {
		return "", true, RequireInteractive(i18n.T("tui.input"))
	}
	s := NewStyles()
	input := textinput.New()
	input.Placeholder = placeholder
	input.CharLimit = 256
	input.Width = 48
	input.Prompt = ""
	input.PromptStyle = s.Accent
	input.PlaceholderStyle = s.Hint
	input.TextStyle = s.Value
	input.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(s.palette.accent))

	m := inputModel{title: title, input: input, secret: secret}
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return "", false, err
	}
	fm, ok := final.(inputModel)
	if !ok {
		return "", false, errors.New(i18n.T("tui.error.input_unexpected"))
	}
	if fm.cancel {
		return "", true, nil
	}
	return fm.result, false, nil
}

// AskText collects one line of visible text.
func AskText(title, placeholder string) (string, error) {
	value, cancelled, err := askInput(title, placeholder, false)
	if err != nil {
		return "", err
	}
	if cancelled {
		return "", errors.New(i18n.T("tui.error.cancelled"))
	}
	return value, nil
}

// AskSecret collects a masked secret.
func AskSecret(title string) (string, error) {
	value, cancelled, err := askInput(title, "••••••••", true)
	if err != nil {
		return "", err
	}
	if cancelled {
		return "", errors.New(i18n.T("tui.error.cancelled"))
	}
	return value, nil
}

// selectItem adapts a label to the bubbles list item interface.
type selectItem struct {
	label string
}

func (i selectItem) Title() string       { return i.label }
func (i selectItem) Description() string { return "" }
func (i selectItem) FilterValue() string { return i.label }

// selectModel is a Bubble Tea model wrapping a bubbles list.
type selectModel struct {
	title  string
	list   list.Model
	result string
	cancel bool
}

func (m selectModel) Init() tea.Cmd {
	return nil
}

func (m selectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if it, ok := m.list.SelectedItem().(selectItem); ok {
				m.result = it.label
			}
			return m, tea.Quit
		case "ctrl+c", "esc":
			m.cancel = true
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		// The list is rendered between the question title and the nav bar;
		// reserve those lines so the options never overflow the terminal and
		// get clipped away.
		m.list.SetSize(msg.Width, msg.Height-selectChromeHeight(m.title))
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m selectModel) View() string {
	s := NewStyles()
	var b strings.Builder
	if m.title != "" {
		b.WriteString(s.questionMark())
		b.WriteString(" ")
		b.WriteString(s.Question.Render(m.title))
		b.WriteString("\n\n")
	}
	b.WriteString(m.list.View())
	b.WriteString("\n")
	b.WriteString(s.NavBar())
	b.WriteString("\n")
	return b.String()
}

// newSelectModel builds the themed list model behind Select. The question is
// rendered as the screen title by selectModel.View, so the bubbles list is
// stripped of its generic "List" heading and pagination row.
func newSelectModel(title string, items []string) selectModel {
	raw := make([]list.Item, 0, len(items))
	for _, label := range items {
		raw = append(raw, selectItem{label: label})
	}

	maxWidth := 0
	for _, label := range items {
		if w := lipgloss.Width(label); w > maxWidth {
			maxWidth = w
		}
	}
	delegate := list.NewDefaultDelegate()
	delegate.SetHeight(1)
	delegate.SetSpacing(0)
	delegate.ShowDescription = false
	delegate.Styles = list.NewDefaultItemStyles()
	s := NewStyles()
	delegate.Styles.SelectedTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(s.palette.accent)).
		Background(lipgloss.Color(s.palette.soft)).
		Bold(true).
		PaddingLeft(1)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedTitle
	delegate.Styles.NormalTitle = lipgloss.NewStyle().PaddingLeft(1).Foreground(lipgloss.Color(s.palette.text))
	delegate.Styles.NormalDesc = delegate.Styles.NormalTitle
	delegate.Styles.DimmedTitle = lipgloss.NewStyle().PaddingLeft(1).Foreground(lipgloss.Color(s.palette.muted))
	delegate.Styles.DimmedDesc = delegate.Styles.DimmedTitle

	l := list.New(raw, delegate, min(maxWidth+12, 80), len(items)+2)
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowTitle(false)
	l.SetShowPagination(false)
	return selectModel{title: title, list: l}
}

// Select presents a menu of options and returns the selected label.
// An empty item list is an error.
func Select(title string, items []string) (string, error) {
	if !IsInteractive() {
		return "", RequireInteractive(i18n.T("tui.selection"))
	}
	if len(items) == 0 {
		return "", errors.New(i18n.T("tui.error.no_options"))
	}

	m := newSelectModel(title, items)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return "", err
	}
	fm, ok := final.(selectModel)
	if !ok {
		return "", errors.New(i18n.T("tui.error.selection_unexpected"))
	}
	if fm.cancel {
		return "", errors.New(i18n.T("tui.error.cancelled"))
	}
	return fm.result, nil
}

// confirmModel is a minimal yes/no prompt.
type confirmModel struct {
	title  string
	defYes bool
	result bool
	cancel bool
}

func (m confirmModel) Init() tea.Cmd {
	return nil
}

func (m confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y":
			m.result = true
			return m, tea.Quit
		case "n":
			m.result = false
			return m, tea.Quit
		case "enter":
			m.result = m.defYes
			return m, tea.Quit
		case "ctrl+c", "esc":
			m.cancel = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m confirmModel) View() string {
	dflt := i18n.T("tui.confirm.yes")
	if !m.defYes {
		dflt = i18n.T("tui.confirm.no")
	}
	s := NewStyles()
	yes := i18n.T("tui.confirm.yes")
	no := i18n.T("tui.confirm.no")
	hints := "(" + s.Info.Render(strings.ToLower(yes)) + "/" +
		s.Info.Render(strings.ToLower(no)) + ")"
	return s.questionMark() + " " + s.Question.Render(m.title) + " " + s.Hint.Render(hints) +
		" [" + s.Focus.Render(dflt) + "] :\n"
}

// questionMark renders the interactive badge used by every prompt.
func (s *Styles) questionMark() string {
	return lipgloss.NewStyle().
		Background(lipgloss.Color(s.palette.accent)).
		Foreground(lipgloss.Color(s.palette.soft)).
		Bold(true).
		Padding(0, 1).
		SetString("?").
		Render()
}

// ConfirmYesEnv forces confirmations to succeed in non-interactive runs
// (`LIORIAN_CLI_YES` non-empty) — the CI pattern mentioned in spec §5.13.1.
const ConfirmYesEnv = "LIORIAN_CLI_YES"

// Confirm asks a yes/no question. defYes is the answer given by pressing
// strictly <enter>. In non-interactive runs the answer comes from
// `LIORIAN_CLI_YES` (truthy → yes, empty → explicit error).
func Confirm(title string, defYes bool) (bool, error) {
	if !IsInteractive() {
		if os.Getenv(ConfirmYesEnv) != "" {
			return true, nil
		}
		return false, RequireInteractive(i18n.T("tui.confirmation"))
	}
	m := confirmModel{title: title, defYes: defYes}
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return false, err
	}
	fm, ok := final.(confirmModel)
	if !ok {
		return false, errors.New(i18n.T("tui.error.confirm_unexpected"))
	}
	if fm.cancel {
		return false, errors.New(i18n.T("tui.error.cancelled"))
	}
	return fm.result, nil
}

// selectChromeHeight is the number of lines selectModel.View renders around
// the list itself: the question title (plus its blank separator), the nav bar
// and the trailing newline that closes the view. Bubbletea drops the top
// lines of a view taller than the terminal, so the list viewport must leave
// room for this chrome or the question scrolls off the screen.
func selectChromeHeight(title string) int {
	h := 2 // nav bar + trailing newline
	if title != "" {
		h += 2 // question line + blank separator
	}
	return h
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
