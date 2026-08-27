package ui

import (
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/gitdiff"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// searchPanel is a diff pane with the find box already up.
func searchPanel(t *testing.T, w, h int) *DiffPanel {
	t.Helper()
	d := NewDiffPanel(w, h)
	d.SetDiff(sampleDiff())
	d.StartSearch()
	return d
}

// typeQuery types a query one key at a time, the way a user does — the
// intermediate prefixes matter.
func typeQuery(d *DiffPanel, q string) {
	for _, r := range q {
		d.SearchKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// Files arrive folded, so a search that only looked at what is on screen would
// find nothing at all.
func TestSearchOpensTheFileTheMatchIsIn(t *testing.T) {
	d := searchPanel(t, 60, 14)
	if strings.Contains(stripSGR(joinRows(d.rows)), "+new") {
		t.Fatal("the fixture should start folded")
	}
	typeQuery(d, "new")
	if len(d.search.hits) != 1 {
		t.Fatalf("hits = %d, want the one line", len(d.search.hits))
	}
	if !strings.Contains(stripSGR(joinRows(d.rows)), "+new") {
		t.Fatalf("the file was not opened:\n%s", stripSGR(joinRows(d.rows)))
	}
}

// Typing "new" passes through "n", which matches nearly everything. What a
// prefix opened has to close again, or the pane fills up with files that don't
// match what was finally typed.
func TestNarrowingTheQueryClosesWhatNoLongerMatches(t *testing.T) {
	d := searchPanel(t, 60, 14)
	typeQuery(d, "n")
	wide := len(d.rows)
	typeQuery(d, "ew")
	if len(d.rows) >= wide {
		t.Fatalf("narrowing to %q left %d rows open, was %d", d.search.query, len(d.rows), wide)
	}
	if d.open[DiffFileKey(sampleDiff().Files[0])] {
		t.Fatal("staged.go has no \"new\" in it and should have closed again")
	}
}

// A file the user opened is theirs, not the search's.
func TestSearchDoesNotCloseAFileYouOpened(t *testing.T) {
	d := searchPanel(t, 60, 14)
	key := DiffFileKey(sampleDiff().Files[0]) // staged.go
	d.ToggleCollapse(key)
	typeQuery(d, "new") // does not match staged.go
	if !d.open[key] {
		t.Fatal("the search closed a file the user had opened")
	}
}

// The matches are painted, and the current one differently from the rest.
func TestMatchesArePaintedAndTheCurrentOneStandsOut(t *testing.T) {
	restoreTheme(t)
	SetMode(ModeDark)
	inColor(t)
	d := searchPanel(t, 60, 14)
	typeQuery(d, "o") // "old", "ctx"… several
	if len(d.search.hits) < 2 {
		t.Fatalf("want several hits, got %d", len(d.search.hits))
	}
	view := d.View()
	if !strings.Contains(view, "48;2;"+rgb(T.Pink)) {
		t.Fatal("the current match should be picked out")
	}
	if !strings.Contains(view, "48;2;"+rgb(T.Yellow)) {
		t.Fatal("the other matches should be marked too")
	}
}

// Moving wraps at both ends: a search that stops at the last match makes you
// retype it to get back to the first.
func TestMovingBetweenMatchesWraps(t *testing.T) {
	d := searchPanel(t, 60, 14)
	typeQuery(d, "o")
	n := len(d.search.hits)
	if n < 2 {
		t.Fatalf("want several hits, got %d", n)
	}
	d.MoveMatch(-1)
	if d.search.idx != n-1 {
		t.Fatalf("back from the first should wrap to the last, got %d of %d", d.search.idx, n)
	}
	d.MoveMatch(1)
	if d.search.idx != 0 {
		t.Fatalf("and forward again to the first, got %d", d.search.idx)
	}
}

// The box costs one row and the pane stays exactly as tall as it was told.
func TestTheFindBoxTakesARowFromTheDiffNotFromThePane(t *testing.T) {
	d := NewDiffPanel(60, 10)
	d.SetDiff(sampleDiff())
	before := len(strings.Split(d.View(), "\n"))
	d.StartSearch()
	after := strings.Split(d.View(), "\n")
	if len(after) != before {
		t.Fatalf("pane is %d rows with the box, %d without", len(after), before)
	}
	if !strings.Contains(stripSGR(after[len(after)-1]), "find") {
		t.Fatalf("the box should be the last row: %q", stripSGR(after[len(after)-1]))
	}
	for i, ln := range after {
		if w := lipgloss.Width(ln); w > 60 {
			t.Fatalf("row %d is %d wide", i, w)
		}
	}
}

// Closing puts the row back and drops the highlight, but leaves the files the
// search opened open — finding them is what was asked for.
func TestClosingTheSearchKeepsWhatItFound(t *testing.T) {
	d := searchPanel(t, 60, 14)
	typeQuery(d, "new")
	d.SearchKey(tea.KeyMsg{Type: tea.KeyEsc})
	if d.Searching() {
		t.Fatal("esc should close the box")
	}
	if len(d.search.hits) != 0 {
		t.Fatalf("the highlight should be gone: %d hits", len(d.search.hits))
	}
	if !strings.Contains(stripSGR(joinRows(d.rows)), "+new") {
		t.Fatal("the file it found should still be open")
	}
}

// Backspace and the word keys edit the query rather than the conversation.
func TestTheBoxEditsItsOwnQuery(t *testing.T) {
	d := searchPanel(t, 60, 14)
	typeQuery(d, "old")
	d.SearchKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if d.search.query != "ol" {
		t.Fatalf("query = %q", d.search.query)
	}
	d.SearchKey(tea.KeyMsg{Type: tea.KeyCtrlU})
	if d.search.query != "" {
		t.Fatalf("ctrl+u should empty it, got %q", d.search.query)
	}
	if len(d.search.hits) != 0 {
		t.Fatal("an empty query matches nothing, rather than everything")
	}
}

// Keys the box has no use for fall through, so the pane still scrolls.
func TestTheBoxPassesOnKeysItDoesNotWant(t *testing.T) {
	d := searchPanel(t, 60, 14)
	if d.SearchKey(tea.KeyMsg{Type: tea.KeyPgDown}) {
		t.Fatal("page down belongs to the pane")
	}
	if !d.SearchKey(tea.KeyMsg{Type: tea.KeySpace}) {
		t.Fatal("a space is part of the query")
	}
}

// Search reads whatever is rendered, so the side-by-side view searches too.
func TestSearchWorksInTheSplitView(t *testing.T) {
	d := NewDiffPanel(100, 14)
	d.SetView(DiffFull)
	d.SetDiff(sampleDiff())
	d.StartSearch()
	typeQuery(d, "new")
	if len(d.search.hits) == 0 {
		t.Fatalf("nothing found in the split view:\n%s", stripSGR(joinRows(d.rows)))
	}
}

// A path is as findable as a line: it is on screen either way.
func TestSearchFindsAFileByItsName(t *testing.T) {
	d := searchPanel(t, 60, 14)
	typeQuery(d, "notes.md")
	if len(d.search.hits) == 0 {
		t.Fatal("a file name should be findable")
	}
}

func TestFindHitsCountsEveryMatchOnALine(t *testing.T) {
	hits := findHits([]string{"one two one", "nothing", "ONE"}, "one")
	if len(hits) != 3 {
		t.Fatalf("hits = %+v", hits)
	}
	if hits[0] != (searchHit{0, 0, 3}) || hits[1] != (searchHit{0, 8, 11}) {
		t.Fatalf("columns are wrong: %+v", hits[:2])
	}
	if hits[2].line != 2 {
		t.Fatalf("the search should ignore case: %+v", hits[2])
	}
}

// A refresh while searching keeps the highlight on what is still there.
func TestABackgroundRefreshKeepsTheSearchAlive(t *testing.T) {
	d := searchPanel(t, 60, 14)
	typeQuery(d, "new")
	ds := sampleDiff()
	ds.Files = append(ds.Files, gitdiff.File{Path: "later.go", Status: "modified",
		Hunks: []gitdiff.Hunk{{Header: "@@ -1 +1 @@", Lines: []gitdiff.Line{
			{Kind: gitdiff.LineAdd, Text: "new arrival"}}}}})
	d.SetDiff(ds)
	if len(d.search.hits) != 2 {
		t.Fatalf("the new file should have joined the search: %d hits", len(d.search.hits))
	}
}

// ctrl+f is find: it shows the diff if it was hidden, because asking to search
// something invisible is asking to see it.
func TestCtrlFOpensTheDiffAndItsFindBox(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	s.dp.SetDiff(sampleDiff())
	a.diffView = DiffHidden
	a.resize(140, 40)

	a.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	if !a.lay.ShowDiff {
		t.Fatal("ctrl+f should show the diff")
	}
	if a.focus != focusDiff || !s.dp.Searching() {
		t.Fatalf("focus = %v, searching = %v", a.focus, s.dp.Searching())
	}
	if !strings.Contains(stripSGR(a.View()), "find") {
		t.Fatal("the box should be on screen")
	}
}

// What you type in the box is a search, not a message — the usual rule that
// typing anywhere goes to the input has to stand aside.
func TestTypingInTheFindBoxDoesNotWriteAMessage(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	s.dp.SetDiff(sampleDiff())
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	typeRunes(t, a, "new")

	if a.in.Value() != "" {
		t.Fatalf("the draft was written into: %q", a.in.Value())
	}
	if s.dp.search.query != "new" {
		t.Fatalf("query = %q", s.dp.search.query)
	}
	// esc closes the box rather than cancelling the turn.
	a.Update(kmsg(tea.KeyEsc))
	if s.dp.Searching() {
		t.Fatal("esc should have closed the box")
	}
}

// With the diff focused, / is the pager gesture for the same thing.
func TestSlashSearchesWhenTheDiffHasFocus(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	s.dp.SetDiff(sampleDiff())
	a.setFocus(focusDiff)
	typeRunes(t, a, "/")
	if !s.dp.Searching() {
		t.Fatal("/ should open the find box")
	}
	if a.in.Value() != "" {
		t.Fatalf("/ went into the draft: %q", a.in.Value())
	}
}

// But / in the input is still the command list.
func TestSlashInTheInputIsStillTheCommandList(t *testing.T) {
	a := testApp(t)
	typeRunes(t, a, "/")
	if a.comp == nil {
		t.Fatal("/ in the input opens the command list")
	}
	if a.cur().dp.Searching() {
		t.Fatal("and must not start a diff search")
	}
}

// runeKey is one character arriving from the keyboard.
func runeKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }
