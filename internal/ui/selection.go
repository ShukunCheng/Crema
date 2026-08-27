package ui

import (
	"strings"

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
}

func (s *selection) extend(line, col int) bool {
	if !s.selecting {
		return false
	}
	s.cursor = selPoint{line, max(0, col)}
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

func (s *selection) clear() bool {
	if !s.selecting && !s.active {
		return false
	}
	s.selecting, s.active = false, false
	s.anchor, s.cursor = selPoint{}, selPoint{}
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
		start, end := s.spanOn(lines[i], i, a, b)
		seg := ansi.Strip(ansi.Cut(lines[i], start, end))
		out = append(out, strings.TrimRight(stripRail(seg), " "))
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
	return start, end
}

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
