package ui

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// InputHeight is the total rows the input box occupies (border + one row).
const InputHeight = 3

type Input struct {
	ta    textarea.Model
	width int
}

func NewInput(w int) *Input {
	ta := textarea.New()
	ta.Placeholder = "ask the agent…  (enter send · ↓ model & permissions · tab switch agent · esc cancel)"
	ta.Prompt = "❯ "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetHeight(1)
	ta.SetWidth(max(1, w-2))
	// Enter is reserved for "send" by the app; newline moves to alt+enter/ctrl+j.
	ta.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("alt+enter", "ctrl+j"), key.WithHelp("alt+enter", "newline"))
	ta.Focus()
	i := &Input{ta: ta, width: max(1, w)}
	i.ApplyTheme()
	return i
}

// ApplyTheme re-reads the active palette; call it after a theme change.
func (i *Input) ApplyTheme() {
	for _, s := range []*textarea.Style{&i.ta.FocusedStyle, &i.ta.BlurredStyle} {
		s.Base = base()
		// The textarea styles the row holding the cursor with CursorLine
		// instead of Text, and the box is one row tall, so everything typed
		// goes through it. Without a foreground it falls back to the
		// terminal's own, which is unreadable on the theme background.
		s.CursorLine = fg(T.Fg)
		s.Text = fg(T.Fg)
		s.Placeholder = fg(T.Muted)
		s.EndOfBuffer = base()
		s.Prompt = fg(T.Muted)
	}
	i.ta.FocusedStyle.Prompt = fg(T.Pink)
	i.ta.BlurredStyle.Text = fg(T.Muted)
	i.ta.BlurredStyle.CursorLine = fg(T.Muted)
	// The caret is drawn by reversing its style, so give it theme colors too;
	// reversing an unset style yields the terminal's defaults.
	i.ta.Cursor.Style = fg(T.Fg)
	i.ta.Cursor.TextStyle = fg(T.Fg)
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
	// keepBG because the textarea nests styles we don't control.
	return pane(c).Width(max(1, i.width-2)).Render(keepBG(i.ta.View()))
}
