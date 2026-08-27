package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// InputHeight is the rows the input box occupies with a one-line draft:
// border plus that row. It is the starting size; the box grows with the draft.
const InputHeight = 3

// maxInputRows caps that growth. Past it the box scrolls instead, so a pasted
// essay can't push the conversation off the screen.
const maxInputRows = 6

type Input struct {
	ta    textarea.Model
	width int
	rows  int // text rows currently shown, 1..maxInputRows
}

func NewInput(w int) *Input {
	ta := textarea.New()
	ta.Placeholder = "ask the agent…  (enter send · ↓ model & permissions · tab switch agent · esc cancel)"
	ta.Prompt = "❯ "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	// The text area is kept at full height for its whole life; View shows only
	// the rows in use. Resizing it as the draft grows would leave its viewport
	// scrolled to wherever the caret was, so the first row of a draft that had
	// just become two would drop out of sight.
	ta.SetHeight(maxInputRows)
	ta.SetWidth(max(1, w-2))
	// Enter is reserved for "send" by the app; newline is the modified ones.
	// ctrl+enter and shift+enter are bound for the terminals that name them,
	// and recovered from the OS for the Windows console, which reports both as
	// a plain enter — see the rewrite in keyPress.
	ta.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("ctrl+enter", "shift+enter", "alt+enter", "ctrl+j"),
		key.WithHelp("shift+enter", "newline"))
	// Delete-word is ctrl+backspace, not the textarea's alt+backspace. Most
	// terminals send ^H for it, so that is bound too — and taken off
	// delete-character, which claims ^H by default and would win the match.
	// ctrl+w is dropped as well: the app uses it to close an agent.
	ta.KeyMap.DeleteWordBackward = key.NewBinding(
		key.WithKeys("ctrl+backspace", "ctrl+h"), key.WithHelp("ctrl+backspace", "delete word"))
	ta.KeyMap.DeleteCharacterBackward = key.NewBinding(
		key.WithKeys("backspace"), key.WithHelp("backspace", "delete character"))
	// Jump a word with ctrl+arrow, which is what the rest of the desktop uses.
	// The text area's own alt+arrow keeps working for anyone used to it.
	ta.KeyMap.WordBackward = key.NewBinding(
		key.WithKeys("ctrl+left", "alt+left", "alt+b"), key.WithHelp("ctrl+←", "previous word"))
	ta.KeyMap.WordForward = key.NewBinding(
		key.WithKeys("ctrl+right", "alt+right", "alt+f"), key.WithHelp("ctrl+→", "next word"))
	ta.Focus()
	i := &Input{ta: ta, width: max(1, w), rows: 1}
	i.ApplyTheme()
	return i
}

// Height is the rows the box needs right now, border included. It follows the
// draft, so a multi-line message is shown whole instead of one line at a time.
func (i *Input) Height() int { return i.rows + 2 }

// syncHeight re-measures the draft and, when the whole of it fits in the box,
// makes sure the box is showing it from the top. Callers re-lay the frame when
// Height changes; this runs on every path that can change the text.
func (i *Input) syncHeight() {
	rows, first := i.measure()
	i.rows = min(max(rows, 1), maxInputRows)
	if rows <= maxInputRows {
		i.scrollToTop(first)
	}
}

// measure lays the draft out in a throwaway text area of the same width to
// find how many rows it fills and what its first row reads. bubbles wraps at
// render time, keeps its wrap function to itself, and its LineCount only
// counts the lines the user typed, so this is the way to an exact answer — and
// the real text area can't be asked, since it is scrolled to follow the caret.
// The throwaway has no prompt, so the padding under the text is blank and can
// be told apart from it; trailing newlines are counted from the text instead,
// since on screen they look exactly like that padding. It is one row taller
// than the box so an overlong draft is recognisable as overlong.
func (i *Input) measure() (rows int, first string) {
	v := i.ta.Value()
	if v == "" {
		return 1, "" // the placeholder is not a draft, however far it wraps
	}
	m := textarea.New()
	m.Prompt = ""
	m.ShowLineNumbers = false
	m.CharLimit = 0
	m.SetWidth(i.ta.Width()) // Width is the text's own columns, prompt excluded
	m.SetHeight(maxInputRows + 1)
	m.SetValue(v)
	lines := strings.Split(m.View(), "\n")
	for n, line := range lines {
		if strings.TrimSpace(ansi.Strip(line)) != "" {
			rows = n + 1
		}
	}
	return rows + trailingNewlines(v), ansi.Strip(lines[0])
}

// scrollToTop drags the text area's view back to the first row of the draft.
// bubbles scrolls to follow the caret and, when the draft shrinks again, only
// scrolls back far enough to keep the caret in sight — which would leave the
// first rows hidden in a box that is tall enough to show them. The offset is
// unexported, but the viewport answers mouse wheel events, and the caret is
// never more than a box-full down, so a few of them always reach the top.
func (i *Input) scrollToTop(want string) {
	up := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp}
	for n := 0; i.ta.Focused() && n < maxInputRows && i.firstRow() != want; n++ {
		i.ta, _ = i.ta.Update(up)
	}
}

// firstRow is the top row the text area is showing, prompt and color removed.
func (i *Input) firstRow() string {
	line, _, _ := strings.Cut(i.ta.View(), "\n")
	return strings.TrimPrefix(ansi.Strip(line), i.ta.Prompt)
}

func trailingNewlines(s string) int {
	n := 0
	for strings.HasSuffix(s, "\n") {
		s, n = s[:len(s)-1], n+1
	}
	return n
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
	i.syncHeight() // narrower columns wrap the draft onto more rows
}

func (i *Input) Value() string { return i.ta.Value() }

// SetValue replaces the draft and puts the caret at the end, so completing a
// command leaves the user ready to type its arguments.
func (i *Input) SetValue(s string) {
	i.ta.SetValue(s)
	i.ta.CursorEnd()
	i.syncHeight()
}

// Insert types s in at the caret, the way a paste arrives.
func (i *Input) Insert(s string) {
	i.ta.InsertString(s)
	i.syncHeight()
}

func (i *Input) Reset() {
	i.ta.Reset()
	i.syncHeight()
}

func (i *Input) Focus() tea.Cmd { return i.ta.Focus() }
func (i *Input) Blur()          { i.ta.Blur() }
func (i *Input) Focused() bool  { return i.ta.Focused() }

func (i *Input) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	i.ta, cmd = i.ta.Update(msg)
	i.syncHeight()
	return cmd
}

func (i *Input) View() string {
	c := T.Purple
	if !i.ta.Focused() {
		c = T.Muted
	}
	// The text area always renders maxInputRows; show the ones in use. Its
	// view starts at the top of the draft, so those are the first rows.
	rows := strings.Split(i.ta.View(), "\n")
	if len(rows) > i.rows {
		rows = rows[:i.rows]
	}
	// keepBG because the textarea nests styles we don't control.
	return pane(c).Width(max(1, i.width-2)).Height(i.rows).
		Render(keepBG(strings.Join(rows, "\n")))
}
