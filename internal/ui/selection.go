package ui

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// selPoint is a position in a pane's rendered content: a line index and a
// display column.
type selPoint struct{ line, col int }

func (p selPoint) before(q selPoint) bool {
	return p.line < q.line || (p.line == q.line && p.col < q.col)
}

// ordered returns the selection's endpoints top-to-bottom regardless of which
// direction the user dragged.
func ordered(a, b selPoint) (selPoint, selPoint) {
	if b.before(a) {
		return b, a
	}
	return a, b
}

// selection is a drag in progress or finished. It knows nothing about what it
// is selecting — the pane hands it the rendered lines — so the conversation
// and the diff both get the same behaviour from the same code.
type selection struct {
	anchor, cursor    selPoint
	selecting, active bool
	// within, when set, keeps the selection inside a column range on each
	// line, and off the lines that have none. The split diff uses it so a
	// drag in the new code copies the new code, not the old side beside it
	// or the line numbers in front of it.
	within func(line int) (lo, hi int, ok bool)
}

func (s *selection) hasRange() bool {
	return (s.selecting || s.active) && s.anchor != s.cursor
}

// done reports a finished selection, which is what may be copied.
func (s *selection) done() bool { return s.active && s.anchor != s.cursor }

func (s *selection) begin(line, col int) {
	s.anchor = selPoint{line, max(0, col)}
	s.cursor = s.anchor
	s.selecting, s.active = true, false
	s.within = nil // whoever wants a limit sets it after
}

func (s *selection) extend(line, col int) bool {
	if !s.selecting {
		return false
	}
	s.cursor = selPoint{line, max(0, col)}
	return true
}

// resume picks a finished selection back up as a drag, its start where it
// was and its loose end at line, col. It is shift+click: the highlight grows
// or shrinks to the click, the way it does in any editor, and the release
// finishes it like any other drag. It reports false when there is nothing to
// resume, so the press can be an ordinary one.
func (s *selection) resume(line, col int) bool {
	if !s.done() {
		return false
	}
	s.cursor = selPoint{line, max(0, col)}
	s.selecting, s.active = true, false
	return true
}

// end finishes a drag. It reports false when the drag never left its starting
// cell — that was a click, not a selection.
func (s *selection) end() bool {
	if !s.selecting {
		return false
	}
	s.selecting = false
	s.active = s.anchor != s.cursor
	return s.active
}

// selectSpan lays a finished selection over one line's column range, which is
// what a double-click leaves behind: no drag ever happened, but there is a
// highlight, and it may be copied like any other.
func (s *selection) selectSpan(line, start, end int) bool {
	if start >= end {
		return false
	}
	s.anchor, s.cursor = selPoint{line, start}, selPoint{line, end}
	s.selecting, s.active = false, true
	s.within = nil
	return true
}

func (s *selection) clear() bool {
	if !s.selecting && !s.active {
		return false
	}
	s.selecting, s.active = false, false
	s.anchor, s.cursor = selPoint{}, selPoint{}
	s.within = nil
	return true
}

// text is the plain text under the highlight, with the tool-block rail
// stripped — the rail is decoration, and nobody wants "│ " pasted into a shell.
func (s *selection) text(lines []string) string {
	if !s.hasRange() {
		return ""
	}
	a, b := ordered(s.anchor, s.cursor)
	var out []string
	for i := a.line; i <= b.line && i < len(lines); i++ {
		if i < 0 {
			continue
		}
		if s.within != nil {
			if _, _, ok := s.within(i); !ok {
				continue // a header, or the other side's line: not this code
			}
		}
		start, end := s.spanOn(lines[i], i, a, b)
		seg := ansi.Strip(ansi.Cut(lines[i], start, end))
		if s.within == nil {
			seg = stripRail(seg)
		}
		out = append(out, strings.TrimRight(seg, " "))
	}
	return strings.Join(out, "\n")
}

// paint returns the lines with the selection drawn over them. The selected
// span is stripped of its own colors first: a highlight that keeps the
// underlying foreground is unreadable against the selection background.
func (s *selection) paint(lines []string) []string {
	if !s.hasRange() {
		return lines
	}
	sel := lipgloss.NewStyle().Foreground(T.Bg).Background(T.Pink)
	a, b := ordered(s.anchor, s.cursor)
	for i := max(0, a.line); i <= b.line && i < len(lines); i++ {
		ln := lines[i]
		w := lipgloss.Width(ln)
		start, end := s.spanOn(ln, i, a, b)
		if start >= end || start >= w {
			continue
		}
		lines[i] = ansi.Cut(ln, 0, start) +
			sel.Render(ansi.Strip(ansi.Cut(ln, start, end))) +
			base().Render(ansi.Strip(ansi.Cut(ln, end, w)))
	}
	return lines
}

// spanOn is the selected column range of one line: full width for the lines in
// the middle of a multi-line drag, clipped at the ends.
func (s *selection) spanOn(line string, i int, a, b selPoint) (int, int) {
	w := lipgloss.Width(line)
	start, end := 0, w
	if i == a.line {
		start = min(a.col, w)
	}
	if i == b.line {
		end = min(b.col, w)
	}
	if end < start {
		start, end = end, start
	}
	if s.within != nil {
		lo, hi, ok := s.within(i)
		if !ok {
			return 0, 0
		}
		if i != a.line {
			start = lo
		}
		if i != b.line {
			end = hi
		}
		start, end = max(start, lo), min(end, hi)
		if end < start {
			return 0, 0
		}
	}
	return start, end
}

// wordSpanAt is the column range of the word under col on a rendered line —
// the columns, not the runes, because that is what the selection paints and
// cuts with and a wide character occupies two of them.
//
// A word is a run of letters, digits and underscores, or a run of the
// punctuation between two of them: the same rule ctrl+backspace uses in the
// input box, and the same one every editor and every terminal applies to a
// double-click. Double-clicking `app` in `internal/ui/app.go` gives `app`
// rather than the whole path, and a click on the dot gives the dot.
//
// It returns an empty range for whitespace and for a click past the end of the
// line, where there is no word to be had.
func wordSpanAt(line string, col int) (int, int) {
	runes := []rune(ansi.Strip(line))
	// Where each rune starts and ends in display columns.
	starts := make([]int, len(runes)+1)
	for i, r := range runes {
		starts[i+1] = starts[i] + runeWidth(r)
	}
	at := -1
	for i := range runes {
		if col >= starts[i] && col < starts[i+1] {
			at = i
			break
		}
	}
	if at < 0 || unicode.IsSpace(runes[at]) {
		return 0, 0
	}
	word := isWordRune(runes[at])
	lo, hi := at, at+1
	for lo > 0 {
		r := runes[lo-1]
		if unicode.IsSpace(r) || isWordRune(r) != word {
			break
		}
		lo--
	}
	for hi < len(runes) {
		r := runes[hi]
		if unicode.IsSpace(r) || isWordRune(r) != word {
			break
		}
		hi++
	}
	return starts[lo], starts[hi]
}

// runeWidth is a rune's width in terminal columns, which is what the selection
// measures in.
func runeWidth(r rune) int { return lipgloss.Width(string(r)) }

// stripRail removes a leading tool-block guide from a copied line.
func stripRail(s string) string {
	return strings.TrimPrefix(s, "│ ")
}

// The conversation's selection, which is the shared one bound to its content.

// BeginSelect starts a drag at a content line and column.
func (t *Timeline) BeginSelect(line, col int) {
	t.sel.begin(line, col)
	t.sync()
}

// ResumeSelect is shift+click on the conversation; see selection.resume.
func (t *Timeline) ResumeSelect(line, col int) bool {
	if !t.sel.resume(line, col) {
		return false
	}
	t.sync()
	return true
}

// ExtendSelect moves the loose end of an in-progress drag.
func (t *Timeline) ExtendSelect(line, col int) {
	if t.sel.extend(line, col) {
		t.sync()
	}
}

// EndSelect finishes a drag and reports the selected text, empty when the drag
// never left its starting cell (that's a click, not a selection).
func (t *Timeline) EndSelect() string {
	if !t.sel.end() {
		t.sync()
		return ""
	}
	t.sync()
	return t.SelectedText()
}

// ClearSelection drops any highlight.
func (t *Timeline) ClearSelection() {
	if t.sel.clear() {
		t.sync()
	}
}

func (t *Timeline) HasSelection() bool { return t.sel.done() }

// SelectWordAt highlights the word under a content position, which is what a
// double-click asks for. It reports false when there is no word there — over
// blank space, or past the end of the line — so the caller can leave the
// press to mean whatever it meant before.
func (t *Timeline) SelectWordAt(line, col int) bool {
	lines := t.contentLines()
	if line < 0 || line >= len(lines) {
		return false
	}
	start, end := wordSpanAt(lines[line], col)
	if !t.sel.selectSpan(line, start, end) {
		return false
	}
	t.sync()
	return true
}

// SelectedText is the plain text under the highlight.
func (t *Timeline) SelectedText() string { return t.sel.text(t.contentLines()) }

func (t *Timeline) contentLines() []string {
	return strings.Split(strings.TrimRight(t.Content(), "\n"), "\n")
}

// highlighted is the timeline content with the selection painted over it.
func (t *Timeline) highlighted() string {
	if !t.sel.hasRange() {
		return t.Content()
	}
	return strings.Join(t.sel.paint(t.contentLines()), "\n") + "\n"
}
