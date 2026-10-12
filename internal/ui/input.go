package ui

import (
	"strings"
	"unicode"

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
	// sel is the drag in the box, in its own display grid. Crema holds the
	// mouse, so the terminal's own selection is gone; see inputsel.go.
	sel selection
}

func NewInput(w int) *Input {
	ta := textarea.New()
	ta.Placeholder = "ask the agent…  (enter send · ↓ model & permissions · ctrl+↑↓ switch agent · esc cancel)"
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
	// Delete-word is handled in Update rather than here, because the text
	// area's own rule is "back to the nearest space" and that is not what a
	// word is. Its binding is emptied so there is no second, shadowed answer
	// to the same key. ctrl+h matters because most terminals send it for
	// ctrl+backspace, and it is taken off delete-character, which claims ^H
	// by default and would otherwise win the match. ctrl+w is left out too:
	// the app uses it to close an agent.
	ta.KeyMap.DeleteWordBackward = key.NewBinding(key.WithDisabled())
	ta.KeyMap.DeleteCharacterBackward = key.NewBinding(
		key.WithKeys("backspace"), key.WithHelp("backspace", "delete character"))
	// Jump a word with ctrl+arrow, which is what the rest of the desktop uses.
	// Handled in Update for the same reason delete-word is: the text area's own
	// rule is "to the nearest space", which walks a whole branch name in one
	// press. Its bindings are emptied so there is no second, shadowed answer.
	ta.KeyMap.WordBackward = key.NewBinding(key.WithDisabled())
	ta.KeyMap.WordForward = key.NewBinding(key.WithDisabled())
	// ctrl+a selects the draft, which is what it means in a text box
	// everywhere else on the desktop. The text area binds it to line-start,
	// emacs-fashion, alongside home — so home keeps that job and ctrl+a is
	// taken off it rather than answering to two things at once.
	ta.KeyMap.LineStart = key.NewBinding(
		key.WithKeys("home"), key.WithHelp("home", "line start"))
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
	i.sel.clear()
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
	i.sel.clear()
	i.ta.Reset()
	i.syncHeight()
}

func (i *Input) Focus() tea.Cmd { return i.ta.Focus() }
func (i *Input) Blur()          { i.ta.Blur() }
func (i *Input) Focused() bool  { return i.ta.Focused() }

func (i *Input) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyMsg); ok {
		if k.Type == tea.KeyCtrlA {
			i.SelectAll()
			return nil
		}
		if move, ok := shiftMoves[k.String()]; ok {
			i.extendSelection(move)
			return nil
		}
		// Typing over a highlight replaces it, backspace deletes it, and
		// anything else drops it and carries on.
		if i.selectionKey(k) {
			i.syncHeight()
			return nil
		}
		switch {
		case isDeleteWordBack(k):
			i.deleteWordBack()
			i.syncHeight()
			return nil
		case isWordLeft(k):
			i.moveWord(-1)
			return nil
		case isWordRight(k):
			i.moveWord(1)
			return nil
		}
	}
	var cmd tea.Cmd
	i.ta, cmd = i.ta.Update(msg)
	i.syncHeight()
	return cmd
}

// isDeleteWordBack recognises the delete-word key however this terminal spells
// it. Most send ^H for ctrl+backspace; the Windows console sends a plain
// backspace and throws the control state away, and keyPress recovers it into
// ^H before the key gets here.
func isDeleteWordBack(k tea.KeyMsg) bool {
	switch k.String() {
	case "ctrl+h", "ctrl+backspace":
		return true
	}
	return false
}

// isWordLeft and isWordRight are the jump-a-word keys. ctrl+arrow is what the
// rest of the desktop uses; alt+arrow is the terminal spelling the text area
// came with, and alt+b / alt+f the emacs one, both kept for anyone used to
// them.
func isWordLeft(k tea.KeyMsg) bool {
	switch k.String() {
	case "ctrl+left", "alt+left", "alt+b":
		return true
	}
	return false
}

func isWordRight(k tea.KeyMsg) bool {
	switch k.String() {
	case "ctrl+right", "alt+right", "alt+f":
		return true
	}
	return false
}

// moveWord walks the caret one word left (dir -1) or right (dir 1), by the
// same rule ctrl+backspace deletes by — punctuation is a boundary, so
// ctrl+← through `fix/1234-order-workspace-issue` stops at every part of it
// rather than stepping over the whole thing. Jumping and deleting agreeing
// about where a word ends is the point: otherwise ctrl+← then ctrl+backspace
// takes something other than what you just walked past.
//
// The move is made of that many plain arrow presses, for the same reason the
// delete is made of backspaces: the caret and the wrapping stay the text
// area's own business.
func (i *Input) moveWord(dir int) {
	n, key := 0, tea.KeyLeft
	if dir < 0 {
		n = wordBackLen(i.beforeCursor())
	} else {
		n, key = wordFwdLen(i.afterCursor()), tea.KeyRight
	}
	if n == 0 {
		// Nothing left on this row in that direction: the thing beyond it is
		// the line break, and one arrow steps over it the way it would.
		n = 1
	}
	for ; n > 0; n-- {
		i.ta, _ = i.ta.Update(tea.KeyMsg{Type: key})
	}
}

// wordFwdLen is how far ctrl+→ moves: over any whitespace in the way, and
// then one run — of word runes, or of the punctuation between them. The
// mirror of wordBackLen, so a jump out and back lands where it started.
func wordFwdLen(rs []rune) int {
	n := 0
	for n < len(rs) && unicode.IsSpace(rs[n]) {
		n++
	}
	if n == len(rs) {
		return n // whitespace all the way to the end
	}
	word := isWordRune(rs[n])
	for n < len(rs) {
		r := rs[n]
		if unicode.IsSpace(r) || isWordRune(r) != word {
			break
		}
		n++
	}
	return n
}

// deleteWordBack removes the word before the caret.
//
// The text area's own delete-word goes back to the nearest space, which on a
// branch name is the whole thing: ctrl+backspace at the end of
// `fix/1234-order-workspace-issue` took all of it, when what was wanted was
// `issue`. Punctuation is a boundary as much as a space is — that is what
// every editor and every browser text box does — so a word here is a run of
// letters, digits and underscores, or a run of the punctuation between two of
// them, whichever the caret is sitting behind.
//
// It is done by handing the text area that many plain backspaces rather than
// by rewriting its value, so the caret, the wrapping and the undo state stay
// its own business.
func (i *Input) deleteWordBack() {
	before := i.beforeCursor()
	n := wordBackLen(before)
	if n == 0 {
		// Nothing before the caret on this row: the thing behind it is the
		// line break, and one backspace joins the rows the way it would.
		n = 1
	}
	for ; n > 0; n-- {
		i.ta, _ = i.ta.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
}

// beforeCursor and afterCursor are the text on the caret's own row, on either
// side of it. The text area keeps its buffer private, so the row comes from
// Line and the column from LineInfo, whose StartColumn and ColumnOffset add up
// to it whether the row is wrapped or not.
func (i *Input) beforeCursor() []rune {
	line, col := i.caret()
	return line[:col]
}

func (i *Input) afterCursor() []rune {
	line, col := i.caret()
	return line[col:]
}

func (i *Input) caret() ([]rune, int) {
	rows := strings.Split(i.ta.Value(), "\n")
	r := i.ta.Line()
	if r < 0 || r >= len(rows) {
		return nil, 0
	}
	li := i.ta.LineInfo()
	col := li.StartColumn + li.ColumnOffset
	line := []rune(rows[r])
	if col > len(line) {
		col = len(line)
	}
	if col < 0 {
		col = 0
	}
	return line, col
}

// wordBackLen is how many runes ctrl+backspace should take off the end of rs:
// any trailing whitespace, and then one run — of word runes, or of the
// punctuation that separates them, but never both.
func wordBackLen(rs []rune) int {
	n := 0
	for n < len(rs) && unicode.IsSpace(rs[len(rs)-1-n]) {
		n++
	}
	if n == len(rs) {
		return n // whitespace all the way back
	}
	word := isWordRune(rs[len(rs)-1-n])
	for n < len(rs) {
		r := rs[len(rs)-1-n]
		if unicode.IsSpace(r) || isWordRune(r) != word {
			break
		}
		n++
	}
	return n
}

// isWordRune is what counts as part of a word: letters, digits and the
// underscore. Everything else — the slashes and dashes in a branch name, the
// dots in a filename, the brackets in a call — is a boundary.
func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func (i *Input) View() string {
	c := T.Purple
	if !i.ta.Focused() {
		c = T.Muted
	}
	// The text area always renders maxInputRows; show the ones in use. Its
	// view starts at the top of the draft, so those are the first rows.
	rows := i.highlighted()
	// keepBG because the textarea nests styles we don't control.
	return pane(c).Width(max(1, i.width-2)).Height(i.rows).
		Render(keepBG(strings.Join(rows, "\n")))
}
