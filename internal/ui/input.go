package ui

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// InputHeight is the total rows the input box occupies (border + one row).
const InputHeight = 3

type Input struct {
	ta    textarea.Model
	width int
}

func NewInput(w int) *Input {
	ta := textarea.New()
	ta.Placeholder = "ask the agent…  (enter to send · alt+enter newline · tab switch agent · esc cancel)"
	ta.Prompt = "❯ "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetHeight(1)
	ta.SetWidth(max(1, w-2))
	// Enter is reserved for "send" by the app; newline moves to alt+enter/ctrl+j.
	ta.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("alt+enter", "ctrl+j"), key.WithHelp("alt+enter", "newline"))
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(T.Pink)
	ta.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(T.Muted)
	ta.Focus()
	return &Input{ta: ta, width: max(1, w)}
}

func (i *Input) SetWidth(w int) {
	i.width = max(1, w)
	i.ta.SetWidth(max(1, w-2))
}

func (i *Input) Value() string  { return i.ta.Value() }
func (i *Input) Reset()         { i.ta.Reset() }
func (i *Input) Focus() tea.Cmd { return i.ta.Focus() }
func (i *Input) Blur()          { i.ta.Blur() }
func (i *Input) Focused() bool  { return i.ta.Focused() }

func (i *Input) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	i.ta, cmd = i.ta.Update(msg)
	return cmd
}

func (i *Input) View() string {
	c := T.Purple
	if !i.ta.Focused() {
		c = T.Muted
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(c).
		Width(max(1, i.width-2)).
		Render(i.ta.View())
}
