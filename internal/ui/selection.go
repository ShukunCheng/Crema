package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// selPoint is a position in the timeline's rendered content: a line index and
// a display column.
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

func (t *Timeline) hasRange() bool {
	return (t.selecting || t.selected) && t.anchor != t.cursor
}

// BeginSelect starts a drag at a content line and column.
func (t *Timeline) BeginSelect(line, col int) {
	t.anchor = selPoint{line, max(0, col)}
	t.cursor = t.anchor
	t.selecting, t.selected = true, false
	t.sync()
}

// ExtendSelect moves the loose end of an in-progress drag.
func (t *Timeline) ExtendSelect(line, col int) {
	if !t.selecting {
		return
	}
	t.cursor = selPoint{line, max(0, col)}
	t.sync()
}

// EndSelect finishes a drag and reports the selected text, empty when the drag
// never left its starting cell (that's a click, not a selection).
func (t *Timeline) EndSelect() string {
	if !t.selecting {
		return ""
	}
	t.selecting = false
	if t.anchor == t.cursor {
		t.selected = false
		t.sync()
		return ""
	}
	t.selected = true
	t.sync()
	return t.SelectedText()
}

// ClearSelection drops any highlight.
func (t *Timeline) ClearSelection() {
	if !t.selecting && !t.selected {
		return
	}
	t.selecting, t.selected = false, false
	t.anchor, t.cursor = selPoint{}, selPoint{}
	t.sync()
}

func (t *Timeline) HasSelection() bool { return t.selected && t.anchor != t.cursor }

// SelectedText is the plain text under the highlight, with the tool-block rail
// stripped — the rail is decoration, and nobody wants "│ " pasted into a shell.
func (t *Timeline) SelectedText() string {
	if !t.hasRange() {
		return ""
	}
	lines := t.contentLines()
	a, b := ordered(t.anchor, t.cursor)
	var out []string
	for i := a.line; i <= b.line && i < len(lines); i++ {
		if i < 0 {
			continue
		}
		start, end := t.rangeOn(lines[i], i, a, b)
		seg := ansi.Strip(ansi.Cut(lines[i], start, end))
		out = append(out, strings.TrimRight(stripRail(seg), " "))
	}
	return strings.Join(out, "\n")
}

// stripRail removes a leading tool-block guide from a copied line.
func stripRail(s string) string {
	return strings.TrimPrefix(s, "│ ")
}

// rangeOn is the selected column span of one line: full width for the lines in
// the middle of a multi-line drag, clipped at the ends.
func (t *Timeline) rangeOn(line string, i int, a, b selPoint) (int, int) {
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

func (t *Timeline) contentLines() []string {
	return strings.Split(strings.TrimRight(t.Content(), "\n"), "\n")
}

// highlighted is the timeline content with the selection painted over it. The
// selected span is stripped of its own colors first: a highlight that keeps the
// underlying foreground is unreadable against the selection background.
func (t *Timeline) highlighted() string {
	if !t.hasRange() {
		return t.Content()
	}
	sel := lipgloss.NewStyle().Foreground(T.Bg).Background(T.Pink)
	lines := t.contentLines()
	a, b := ordered(t.anchor, t.cursor)
	for i := max(0, a.line); i <= b.line && i < len(lines); i++ {
		ln := lines[i]
		w := lipgloss.Width(ln)
		start, end := t.rangeOn(ln, i, a, b)
		if start >= end || start >= w {
			continue
		}
		lines[i] = ansi.Cut(ln, 0, start) +
			sel.Render(ansi.Strip(ansi.Cut(ln, start, end))) +
			base().Render(ansi.Strip(ansi.Cut(ln, end, w)))
	}
	return strings.Join(lines, "\n") + "\n"
}
