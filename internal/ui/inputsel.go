package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The conversation and the diff both select text with the mouse, because crema
// holds the mouse itself — terminal mouse reporting is all-or-nothing, so the
// terminal's own selection is gone the moment crema asks for a single click.
// That left the message box as the one pane where a drag did nothing, which is
// the pane where it is most often wanted: what is in it is what you wrote.
//
// So it selects the same way, by the same gestures, and a selection there is a
// selection in a text box rather than a read-only pane — typing over it
// replaces it, and backspace deletes it.
//
// The text area keeps its buffer and its wrapping private, so the two things
// this needs — where a screen position falls in the draft, and where a draft
// offset falls on the screen — are worked out by walking its caret with the
// public API and reading LineInfo at each step. It is exact, it costs a walk
// of the draft's rows on a mouse press, and it never has to guess at the wrap.

// promptW is how many columns the "❯ " in front of every wrapped row takes.
// The selection is stored in text columns, past it, so what gets copied is
// what was typed rather than a column of prompts.
func (i *Input) promptW() int { return lipgloss.Width(i.ta.Prompt) }

// TextRows is how many rows of the draft the box is showing, which is what a
// screen position is checked against.
func (i *Input) TextRows() int { return i.rows }

// gridStop is one display row of the draft: which line of the value it belongs
// to, and the rune index within that line where the row starts.
type gridStop struct {
	line, start, length int
}

// grid walks the caret down the draft and records where each display row
// begins. The caret is put back where it was, so nothing the user can see
// moves. It is the only way to the text area's wrapping, which it does at
// render time and keeps to itself.
func (i *Input) grid() []gridStop {
	lines := strings.Split(i.ta.Value(), "\n")
	was := i.caretOffset()
	defer i.caretToOffset(was)

	i.caretToTop()
	var out []gridStop
	for n := 0; n < maxGridRows; n++ {
		li := i.ta.LineInfo()
		line := i.ta.Line()
		length := 0
		if line >= 0 && line < len(lines) {
			length = len([]rune(lines[line]))
		}
		out = append(out, gridStop{line: line, start: li.StartColumn, length: length})
		before := [2]int{line, li.StartColumn}
		i.ta.CursorDown()
		li = i.ta.LineInfo()
		if ([2]int{i.ta.Line(), li.StartColumn}) == before {
			break // the bottom: down moved nothing
		}
	}
	return out
}

// maxGridRows bounds the walk. A draft is a message, not a document, and a
// walk that never ended would freeze the frame on a paste of a whole file.
const maxGridRows = 4000

func (i *Input) caretToTop() {
	for n := 0; n < maxGridRows; n++ {
		if i.ta.Line() == 0 && i.ta.LineInfo().RowOffset == 0 {
			return
		}
		before := [2]int{i.ta.Line(), i.ta.LineInfo().StartColumn}
		i.ta.CursorUp()
		if ([2]int{i.ta.Line(), i.ta.LineInfo().StartColumn}) == before {
			return
		}
	}
}

// caretOffset is where the caret is, counted in runes from the start of the
// draft with each newline counting one.
func (i *Input) caretOffset() int {
	lines := strings.Split(i.ta.Value(), "\n")
	li := i.ta.LineInfo()
	off := 0
	for k := 0; k < i.ta.Line() && k < len(lines); k++ {
		off += len([]rune(lines[k])) + 1
	}
	return off + li.StartColumn + li.ColumnOffset
}

// caretToOffset puts the caret at a rune offset in the draft.
func (i *Input) caretToOffset(off int) {
	lines := strings.Split(i.ta.Value(), "\n")
	line, col := 0, max(0, off)
	for line < len(lines)-1 && col > len([]rune(lines[line])) {
		col -= len([]rune(lines[line])) + 1
		line++
	}
	i.caretToTop()
	for n := 0; n < maxGridRows && i.ta.Line() < line; n++ {
		before := i.ta.Line()
		i.ta.CursorDown()
		if i.ta.Line() == before && i.ta.LineInfo().RowOffset == 0 {
			break
		}
	}
	i.ta.SetCursor(min(col, len([]rune(lines[min(line, len(lines)-1)]))))
}

// offsetAt is where a position in the box's display grid falls in the draft.
func (i *Input) offsetAt(row, col int) int {
	g := i.grid()
	row = min(max(row+i.topRow(), 0), max(0, len(g)-1))
	if len(g) == 0 {
		return 0
	}
	st := g[row]
	lines := strings.Split(i.ta.Value(), "\n")
	off := 0
	for k := 0; k < st.line && k < len(lines); k++ {
		off += len([]rune(lines[k])) + 1
	}
	// A row ends where the next one on the same line begins; past that, the
	// click lands at the end of the row rather than wrapping onto the next.
	end := st.length
	if row+1 < len(g) && g[row+1].line == st.line {
		end = g[row+1].start
	}
	return off + min(st.start+max(0, col), end)
}

// topRow is the first row of the draft the box is showing. The box holds six
// rows and puts the draft's first row at the top whenever the whole of it
// fits, which is nearly always; past that it follows the caret, and the top is
// found by matching what is drawn there.
func (i *Input) topRow() int {
	if i.rows <= maxInputRows && i.rows == i.wrapRows() {
		return 0
	}
	first := i.firstRow()
	rows := i.draftRows()
	for n, r := range rows {
		if strings.TrimRight(r, " ") == strings.TrimRight(first, " ") {
			return n
		}
	}
	return 0
}

// wrapRows and draftRows lay the draft out the way measure does, to find how
// many rows it really takes and what each of them reads.
func (i *Input) wrapRows() int { n, _ := i.measure(); return n }

func (i *Input) draftRows() []string {
	rows := strings.Split(i.ta.View(), "\n")
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, strings.TrimPrefix(ansi.Strip(r), i.ta.Prompt))
	}
	return out
}

// rendered is the box's text rows with the prompt taken off the front, which
// is what the selection is measured and painted in.
func (i *Input) rendered() []string {
	rows := strings.Split(i.ta.View(), "\n")
	if len(rows) > i.rows {
		rows = rows[:i.rows]
	}
	out := make([]string, len(rows))
	for n, r := range rows {
		out[n] = ansi.Cut(r, i.promptW(), lipgloss.Width(r))
	}
	return out
}

// BeginSelect, ExtendSelect and EndSelect are the drag, in the box's own
// display grid: a row of the box and a column past the prompt.
func (i *Input) BeginSelect(row, col int) { i.sel.begin(row, col) }

func (i *Input) ExtendSelect(row, col int) { i.sel.extend(row, col) }

// ResumeSelect is shift+click in the box; see selection.resume.
func (i *Input) ResumeSelect(row, col int) bool { return i.sel.resume(row, col) }

// EndSelect finishes a drag and reports what it covers, empty when the drag
// never left its starting cell — that was a click, not a selection.
func (i *Input) EndSelect() string {
	if !i.sel.end() {
		return ""
	}
	return i.SelectedText()
}

func (i *Input) ClearSelection() bool { return i.sel.clear() }
func (i *Input) HasSelection() bool   { return i.sel.done() }

// SelectedText is taken out of the draft rather than off the rows it is drawn
// on, because a wrap is not a line break: reading it back from the screen put
// a newline in the middle of a sentence that never had one, and pasted it
// somewhere else. Cutting the draft between the selection's own two offsets
// gives exactly what was typed, and makes copying and deleting agree by
// construction rather than by care.
func (i *Input) SelectedText() string {
	if !i.sel.hasRange() {
		return ""
	}
	from, to := i.selRange()
	runes := []rune(i.ta.Value())
	if from >= to {
		return ""
	}
	return string(runes[from:to])
}

// selRange is the selection as two offsets into the draft, ordered and inside
// it.
func (i *Input) selRange() (int, int) {
	a, b := ordered(i.sel.anchor, i.sel.cursor)
	n := len([]rune(i.ta.Value()))
	from := min(max(i.offsetAt(a.line, a.col), 0), n)
	to := min(max(i.offsetAt(b.line, b.col), 0), n)
	return from, to
}

func (i *Input) selecting() bool { return i.sel.selecting }

// SelectWordAt highlights the word under a position, by the same rule
// ctrl+backspace deletes by — so a double-click in the box and a double-click
// in the conversation take the same thing.
func (i *Input) SelectWordAt(row, col int) bool {
	rows := i.rendered()
	if row < 0 || row >= len(rows) {
		return false
	}
	start, end := wordSpanAt(rows[row], col)
	return i.sel.selectSpan(row, start, end)
}

// SelectAll takes the whole draft, which is what ctrl+a means in a text box.
func (i *Input) SelectAll() bool {
	rows := i.rendered()
	if len(rows) == 0 || i.ta.Value() == "" {
		return false
	}
	last := len(rows) - 1
	i.sel.anchor = selPoint{0, 0}
	i.sel.cursor = selPoint{last, lipgloss.Width(rows[last])}
	i.sel.selecting, i.sel.active = false, true
	return true
}

// DeleteSelection takes the highlighted text out of the draft and leaves the
// caret where it was, which is what typing over a selection has to do first.
func (i *Input) DeleteSelection() bool {
	if !i.sel.hasRange() {
		return false
	}
	from, to := i.selRange()
	i.sel.clear()
	if from >= to {
		return false
	}
	runes := []rune(i.ta.Value())
	i.ta.SetValue(string(runes[:from]) + string(runes[to:]))
	i.caretToOffset(from)
	i.syncHeight()
	return true
}

// selectionKey handles a key while something is highlighted. Typing over a
// selection replaces it and backspace deletes it, the way a text box does;
// anything that only moves the caret drops the highlight and is passed on.
func (i *Input) selectionKey(k tea.KeyMsg) (handled bool) {
	if !i.sel.hasRange() {
		return false
	}
	switch {
	case isTyping(k), k.Type == tea.KeyBackspace, k.Type == tea.KeyDelete:
		i.DeleteSelection()
		// A backspace has now done its work by removing the selection; a
		// typed character still has to land.
		return k.Type == tea.KeyBackspace || k.Type == tea.KeyDelete
	case isDeleteWordBack(k):
		i.DeleteSelection()
		return true
	}
	i.sel.clear()
	return false
}

// highlighted is the box's rows with the selection painted over them, the
// prompt put back on the front of each.
func (i *Input) highlighted() []string {
	rows := strings.Split(i.ta.View(), "\n")
	if len(rows) > i.rows {
		rows = rows[:i.rows]
	}
	if !i.sel.hasRange() {
		return rows
	}
	pw := i.promptW()
	body := i.sel.paint(i.rendered())
	out := make([]string, len(rows))
	for n := range rows {
		prompt := ansi.Cut(rows[n], 0, pw)
		if n < len(body) {
			out[n] = prompt + body[n]
			continue
		}
		out[n] = rows[n]
	}
	return out
}

// Shift and an arrow select from the keyboard, the way every text box does:
// shift+←/→ a character at a time, ctrl+shift+←/→ a word at a time by the
// same rule ctrl+← jumps and ctrl+backspace deletes, shift+home/end to the
// ends of the row, shift+↑/↓ a row at a time, and ctrl+shift+home/end to the
// ends of the draft. Holding shift and pressing again keeps extending from
// where the selection started; a drag can be carried on the same way.

// shiftMoves pairs each selecting key with the plain move it extends by.
var shiftMoves = map[string]tea.KeyType{
	"shift+left":       tea.KeyLeft,
	"shift+right":      tea.KeyRight,
	"ctrl+shift+left":  tea.KeyCtrlLeft,
	"ctrl+shift+right": tea.KeyCtrlRight,
	"shift+home":       tea.KeyHome,
	"shift+end":        tea.KeyEnd,
	"shift+up":         tea.KeyUp,
	"shift+down":       tea.KeyDown,
	"ctrl+shift+home":  tea.KeyCtrlHome,
	"ctrl+shift+end":   tea.KeyCtrlEnd,
}

// IsSelectKey reports whether k is one of the keys that select in the box.
func IsSelectKey(k tea.KeyMsg) bool {
	_, ok := shiftMoves[k.String()]
	return ok
}

// extendSelection makes the plain move and highlights from where the
// selection was anchored to where the caret has gone.
func (i *Input) extendSelection(move tea.KeyType) {
	anchor := i.caretOffset()
	if i.sel.hasRange() {
		// Carry on from the selection there is, whether a drag or an
		// earlier shift+arrow made it: its anchor stays, its loose end is
		// where the caret moves from.
		anchor = i.offsetAt(i.sel.anchor.line, i.sel.anchor.col)
		i.caretToOffset(i.offsetAt(i.sel.cursor.line, i.sel.cursor.col))
	}
	i.sel.clear() // so the move below is a move, not the end of a selection
	i.Update(tea.KeyMsg{Type: move})
	caret := i.caretOffset()
	if caret == anchor {
		return // moved back onto its start: nothing is selected
	}
	i.sel.anchor, i.sel.cursor = i.pointOf(anchor), i.pointOf(caret)
	i.sel.selecting, i.sel.active = false, true
}

// pointOf is where a draft offset is drawn: a row of the box and a column
// past the prompt — the inverse of offsetAt. At a wrap the offset belongs to
// the start of the next row, which is where the text area puts the caret.
func (i *Input) pointOf(off int) selPoint {
	lines := strings.Split(i.ta.Value(), "\n")
	line, col := 0, max(0, off)
	for line < len(lines)-1 && col > len([]rune(lines[line])) {
		col -= len([]rune(lines[line])) + 1
		line++
	}
	g := i.grid()
	row := 0
	for r, st := range g {
		if st.line == line && st.start <= col {
			row = r
		}
	}
	if len(g) == 0 {
		return selPoint{0, col}
	}
	return selPoint{row - i.topRow(), col - g[row].start}
}
