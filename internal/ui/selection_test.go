package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func drag(x, y int, action tea.MouseAction) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: action, Button: tea.MouseButtonLeft}
}

// selApp gives a timeline with known, wide-enough content to drag across.
func selApp(t *testing.T) *App {
	t.Helper()
	a := testApp(t)
	a.cur().tl.Restore([]Block{
		{Kind: BlockAssistant, Text: "first line of output"},
		{Kind: BlockAssistant, Text: "second line of output"},
		{Kind: BlockAssistant, Text: "third line of output"},
	})
	a.cur().tl.Update(kmsg(tea.KeyHome)) // scroll to the top so line 0 is row 0
	return a
}

// A double-click selects the word under the pointer, the way it does
// everywhere else. What counts as a word is the same rule ctrl+backspace uses
// in the input box: letters, digits and underscores, with punctuation as a
// boundary rather than part of the word.
func TestDoubleClickSelectsTheWord(t *testing.T) {
	a := selApp(t)
	x := a.lay.SidebarW
	// "second line of output" is content line 1; "line" starts at column 7.
	a.Update(drag(x+8, 1, tea.MouseActionPress))
	a.Update(drag(x+8, 1, tea.MouseActionRelease))
	a.Update(drag(x+8, 1, tea.MouseActionPress))
	a.Update(drag(x+8, 1, tea.MouseActionRelease))
	if got := a.cur().tl.SelectedText(); got != "line" {
		t.Fatalf("double-click selected %q, want %q", got, "line")
	}
	if !a.cur().tl.HasSelection() {
		t.Fatal("and it should be a real selection, copyable like any other")
	}
}

// A header is a button, and clicking a button twice quickly is a thing people
// do: folding a tool block open and shut again must not turn into a word
// selection halfway through.
func TestDoubleClickingAHeaderStillFoldsIt(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	s.tl.Append(Block{Kind: BlockTool, Name: "Bash", Text: "line one\nline two"})
	s.tl.Update(kmsg(tea.KeyHome))

	header := -1
	for i := 0; i < 40; i++ {
		if s.tl.HeaderBlockAt(i) == s.tl.Len()-1 {
			header = i
			break
		}
	}
	if header < 0 {
		t.Fatal("no header row found")
	}
	y := header - s.tl.YOffset()
	x := a.lay.SidebarW + 2
	for i := 0; i < 2; i++ { // two presses in a row, as fast as a double-click
		a.Update(drag(x, y, tea.MouseActionPress))
		a.Update(drag(x, y, tea.MouseActionRelease))
	}
	if !strings.Contains(s.tl.Content(), "line two") {
		t.Fatalf("the second click should have expanded it again:\n%s", s.tl.Content())
	}
	if got := s.tl.SelectedText(); got != "" {
		t.Fatalf("and it should not have selected a word, got %q", got)
	}
}

// Two presses far enough apart are two clicks, not a double-click.
func TestTwoSlowClicksAreNotADoubleClick(t *testing.T) {
	a := selApp(t)
	x := a.lay.SidebarW
	a.Update(drag(x+8, 1, tea.MouseActionPress))
	a.Update(drag(x+8, 1, tea.MouseActionRelease))
	a.lastPaneClick = a.lastPaneClick.Add(-2 * doubleClick)
	a.Update(drag(x+8, 1, tea.MouseActionPress))
	a.Update(drag(x+8, 1, tea.MouseActionRelease))
	if got := a.cur().tl.SelectedText(); got != "" {
		t.Fatalf("selection = %q, want none", got)
	}
}

// Word boundaries, on the shapes that actually turn up in agent output.
func TestWordSpanAt(t *testing.T) {
	for _, c := range []struct {
		line string
		col  int
		want string
	}{
		{"internal/ui/app.go", 13, "app"},   // a path segment, not the path
		{"internal/ui/app.go", 8, "/"},      // and the separator on its own
		{"internal/ui/app.go", 16, "go"},    // the extension
		{"fix/1234-order-issue", 4, "1234"}, // digits are word runes
		{"snake_case_name x", 3, "snake_case_name"},
		{"call(arg)", 5, "arg"},
		{"a === b", 3, "==="}, // a run of punctuation goes together
		{"one two", 3, ""},    // whitespace selects nothing
		{"short", 40, ""},     // and neither does the void past the end
		{"héllo wörld", 2, "héllo"},
		{"│ tool output here", 7, "output"}, // the tool rail is not part of it
	} {
		start, end := wordSpanAt(c.line, c.col)
		got := ""
		if start < end {
			got = string([]rune(c.line)[colToRune(c.line, start):colToRune(c.line, end)])
		}
		if got != c.want {
			t.Errorf("wordSpanAt(%q, %d) = %q, want %q", c.line, c.col, got, c.want)
		}
	}
}

// colToRune turns a display column back into a rune index, for the test to
// read the span it was given.
func colToRune(line string, col int) int {
	w := 0
	for i, r := range []rune(line) {
		if w >= col {
			return i
		}
		w += runeWidth(r)
	}
	return len([]rune(line))
}

func TestDragSelectsAcrossLines(t *testing.T) {
	a := selApp(t)
	tl := a.cur().tl
	tl.BeginSelect(0, 0)
	tl.ExtendSelect(1, 6)
	got := tl.SelectedText()
	if got != "first line of output\nsecond" {
		t.Fatalf("selection = %q", got)
	}
	if !strings.Contains(tl.Content(), "first line of output") {
		t.Fatal("Content() must stay the unhighlighted source of truth")
	}
}

func TestSelectingBackwardsGivesTheSameText(t *testing.T) {
	a := selApp(t)
	tl := a.cur().tl
	tl.BeginSelect(1, 6)
	tl.ExtendSelect(0, 0)
	if got := tl.SelectedText(); got != "first line of output\nsecond" {
		t.Fatalf("dragging up should select the same span, got %q", got)
	}
}

func TestSelectionWithinOneLine(t *testing.T) {
	a := selApp(t)
	tl := a.cur().tl
	tl.BeginSelect(0, 6)
	tl.ExtendSelect(0, 10)
	if got := tl.SelectedText(); got != "line" {
		t.Fatalf("partial-line selection = %q", got)
	}
}

func TestSelectionStripsTheToolRail(t *testing.T) {
	a := testApp(t)
	tl := a.cur().tl
	tl.Restore([]Block{{Kind: BlockTool, Name: "Bash", Text: "go test ./..."}})
	// line 0 is the "▶ Bash" header; line 1 is the railed command
	tl.BeginSelect(1, 0)
	tl.ExtendSelect(1, 100)
	got := tl.SelectedText()
	if strings.Contains(got, "│") {
		t.Fatalf("the rail is decoration and must not be copied: %q", got)
	}
	if got != "go test ./..." {
		t.Fatalf("selection = %q, want the bare command", got)
	}
}

func TestHighlightIsVisibleAndConfinedToTheSelection(t *testing.T) {
	restoreTheme(t)
	trueColor(t)
	a := selApp(t)
	tl := a.cur().tl
	plain := tl.View()
	tl.BeginSelect(0, 0)
	tl.ExtendSelect(0, 5)
	marked := tl.View()

	if marked == plain {
		t.Fatal("a selection must be visible")
	}
	lines := strings.Split(marked, "\n")
	if !strings.Contains(stripANSIFull(lines[0]), "first line of output") {
		t.Fatalf("the text itself must survive highlighting: %q", lines[0])
	}
	// only the first row changed
	plainLines := strings.Split(plain, "\n")
	for i := 1; i < len(lines) && i < len(plainLines); i++ {
		if lines[i] != plainLines[i] {
			t.Fatalf("row %d changed but is outside the selection", i)
		}
	}
}

// fakeClipboard captures copies instead of touching the real clipboard.
func fakeClipboard(t *testing.T) *string {
	t.Helper()
	var got string
	prev := copyToClipboard
	copyToClipboard = func(s string) error { got = s; return nil }
	t.Cleanup(func() { copyToClipboard = prev })
	return &got
}

func TestDraggingInTheTimelineCopiesToTheClipboard(t *testing.T) {
	clip := fakeClipboard(t)
	a := selApp(t)
	x := a.lay.SidebarW

	a.Update(drag(x, 0, tea.MouseActionPress))
	a.Update(drag(x+10, 1, tea.MouseActionMotion))
	a.Update(drag(x+6, 1, tea.MouseActionRelease))

	if *clip != "first line of output\nsecond" {
		t.Fatalf("clipboard got %q", *clip)
	}
	if !strings.Contains(a.note, "copied 2 lines") {
		t.Fatalf("the user must be told the copy happened: %q", a.note)
	}
	if !a.cur().tl.HasSelection() {
		t.Fatal("the highlight stays up so the user sees what was copied")
	}
}

// ctrl+c copies the highlight instead of quitting — reaching for it after
// selecting text must not take the session down.
func TestCtrlCCopiesTheSelectionAndDoesNotQuit(t *testing.T) {
	clip := fakeClipboard(t)
	a := selApp(t)
	x := a.lay.SidebarW

	a.Update(drag(x, 0, tea.MouseActionPress))
	a.Update(drag(x+10, 1, tea.MouseActionMotion))
	a.Update(drag(x+6, 1, tea.MouseActionRelease))
	*clip, a.note = "", ""

	_, cmd := a.Update(kmsg(tea.KeyCtrlC))
	if cmd != nil {
		t.Fatal("ctrl+c must not quit while text is selected")
	}
	if *clip != "first line of output\nsecond" {
		t.Fatalf("clipboard got %q", *clip)
	}
	if !strings.Contains(a.note, "copied 2 lines") {
		t.Fatalf("note = %q, want the copy reported", a.note)
	}
	if !a.cur().tl.HasSelection() {
		t.Fatal("the highlight stays up after an explicit copy")
	}
}

// With nothing selected ctrl+c still doesn't quit; it says where the exit is.
func TestCtrlCWithNoSelectionPointsAtTheExit(t *testing.T) {
	a := selApp(t)
	_, cmd := a.Update(kmsg(tea.KeyCtrlC))
	if cmd != nil {
		t.Fatal("ctrl+c must never quit")
	}
	if !strings.Contains(a.note, "ctrl+q") {
		t.Fatalf("note = %q, want it to name the quit key", a.note)
	}
	if _, cmd := a.Update(kmsg(tea.KeyCtrlQ)); cmd == nil {
		t.Fatal("ctrl+q quits")
	}
}

func TestACopyFailureIsReportedNotSwallowed(t *testing.T) {
	prev := copyToClipboard
	copyToClipboard = func(string) error { return errors.New("no clipboard here") }
	t.Cleanup(func() { copyToClipboard = prev })

	a := selApp(t)
	x := a.lay.SidebarW
	a.Update(drag(x, 0, tea.MouseActionPress))
	a.Update(drag(x+8, 0, tea.MouseActionMotion))
	a.Update(drag(x+8, 0, tea.MouseActionRelease))
	if !strings.Contains(a.note, "could not copy") {
		t.Fatalf("a failed copy must say so: %q", a.note)
	}
}

func TestDragOverAToolHeaderSelectsInsteadOfFolding(t *testing.T) {
	fakeClipboard(t)
	a := testApp(t)
	tl := a.cur().tl
	tl.Restore([]Block{{Kind: BlockTool, Name: "Bash", Text: "echo one\necho two"}})
	tl.Update(kmsg(tea.KeyHome))
	x := a.lay.SidebarW

	a.Update(drag(x, 0, tea.MouseActionPress)) // the "▶ Bash" header row
	a.Update(drag(x+5, 2, tea.MouseActionMotion))
	a.Update(drag(x+5, 2, tea.MouseActionRelease))

	if !strings.Contains(tl.Content(), "echo two") {
		t.Fatal("a drag across a header must select, not fold the block away")
	}
}

func TestClickWithoutMovingIsNotASelection(t *testing.T) {
	a := selApp(t)
	x := a.lay.SidebarW + 3
	a.Update(drag(x, 1, tea.MouseActionPress))
	a.Update(drag(x, 1, tea.MouseActionRelease))
	if a.cur().tl.HasSelection() {
		t.Fatal("a click must not leave a selection behind")
	}
	if strings.Contains(a.note, "copied") {
		t.Fatalf("nothing should have been copied: %q", a.note)
	}
}

func TestDragOutsideTheTimelineDoesNotSelect(t *testing.T) {
	a := selApp(t)
	// press in the sidebar, drag across the timeline
	a.Update(drag(2, 2, tea.MouseActionPress))
	a.Update(drag(a.lay.SidebarW+10, 3, tea.MouseActionMotion))
	if a.cur().tl.HasSelection() || a.dragging != dragNone {
		t.Fatal("selection only starts inside a pane that shows text")
	}
}

// A drag in the diff selects the diff, and leaves the conversation alone.
func TestDragInTheDiffPaneSelectsTheDiff(t *testing.T) {
	clip := fakeClipboard(t)
	a := selApp(t)
	if !a.lay.ShowDiff {
		t.Skip("needs the diff pane")
	}
	a.cur().dp.SetDiff(sampleDiff())

	x := a.lay.SidebarW + a.lay.TimelineW + 1 // just inside the border
	a.Update(drag(x, 1, tea.MouseActionPress))
	if a.dragging != dragDiff {
		t.Fatalf("dragging = %v, want the diff pane", a.dragging)
	}
	a.Update(drag(x+20, 2, tea.MouseActionMotion))
	a.Update(drag(x+20, 2, tea.MouseActionRelease))

	if !a.cur().dp.HasSelection() {
		t.Fatal("the diff should be holding a selection")
	}
	if a.cur().tl.HasSelection() {
		t.Fatal("the conversation must not be selected too")
	}
	if *clip == "" || !strings.Contains(a.note, "copied") {
		t.Fatalf("the drag should have copied: clip=%q note=%q", *clip, a.note)
	}
	// ctrl+c copies it again, from whichever pane holds it.
	*clip = ""
	a.Update(kmsg(tea.KeyCtrlC))
	if *clip == "" {
		t.Fatal("ctrl+c must copy the diff's selection")
	}
}

// Starting a drag in one pane drops the other's highlight, so only one is
// ever live and ctrl+c is never ambiguous.
func TestSelectingOnePaneClearsTheOther(t *testing.T) {
	a := selApp(t)
	if !a.lay.ShowDiff {
		t.Skip("needs the diff pane")
	}
	a.cur().dp.SetDiff(sampleDiff())

	tx := a.lay.SidebarW
	a.Update(drag(tx, 0, tea.MouseActionPress))
	a.Update(drag(tx+8, 1, tea.MouseActionMotion))
	a.Update(drag(tx+8, 1, tea.MouseActionRelease))
	if !a.cur().tl.HasSelection() {
		t.Fatal("expected a conversation selection to start with")
	}

	dx := a.lay.SidebarW + a.lay.TimelineW + 1
	a.Update(drag(dx, 1, tea.MouseActionPress))
	if a.cur().tl.HasSelection() {
		t.Fatal("starting in the diff must drop the conversation's highlight")
	}
}

// A click on a file header still opens it, now that presses go to the drag
// handler first.
func TestClickingADiffHeaderStillTogglesIt(t *testing.T) {
	a := selApp(t)
	if !a.lay.ShowDiff {
		t.Skip("needs the diff pane")
	}
	s := a.cur()
	s.dp.SetDiff(sampleDiff())
	if strings.Contains(joinRows(s.dp.rows), "package main") {
		t.Fatal("files arrive folded")
	}
	row := -1
	for i := range s.dp.rows {
		if s.dp.rows[i].header {
			row = i
			break
		}
	}
	x := a.lay.SidebarW + a.lay.TimelineW + 2
	y := row - s.dp.YOffset() + 2 // the border, then the pane's own header
	a.Update(drag(x, y, tea.MouseActionPress))
	a.Update(drag(x, y, tea.MouseActionRelease))
	if !strings.Contains(joinRows(s.dp.rows), "package main") {
		t.Fatal("clicking the header should have opened the file")
	}
}

func TestEscapeClearsTheSelectionBeforeCancelingTheTurn(t *testing.T) {
	a := selApp(t)
	s := a.cur()
	s.tl.BeginSelect(0, 0)
	s.tl.ExtendSelect(1, 4)
	s.tl.EndSelect()
	if !s.tl.HasSelection() {
		t.Fatal("expected a selection")
	}
	a.in.ta.SetValue("work")
	a.Update(kmsg(tea.KeyEnter)) // now busy
	a.Update(kmsg(tea.KeyEsc))
	if s.tl.HasSelection() {
		t.Fatal("esc should clear the selection first")
	}
	if !s.busy {
		t.Fatal("the first esc must not also cancel the turn")
	}
	a.Update(kmsg(tea.KeyEsc))
	if !strings.Contains(s.tl.Content(), "canceling") {
		t.Fatal("a second esc cancels as usual")
	}
}

func TestClickingAnotherPaneClearsTheSelection(t *testing.T) {
	a := selApp(t)
	s := a.cur()
	s.tl.BeginSelect(0, 0)
	s.tl.ExtendSelect(1, 4)
	s.tl.EndSelect()
	a.Update(drag(2, 2, tea.MouseActionPress)) // the sidebar
	if s.tl.HasSelection() {
		t.Fatal("clicking outside the conversation drops the highlight")
	}
}

func TestSelectionSurvivesNewOutputArriving(t *testing.T) {
	a := selApp(t)
	tl := a.cur().tl
	tl.BeginSelect(0, 0)
	tl.ExtendSelect(0, 5)
	tl.EndSelect()
	before := tl.SelectedText()
	tl.Append(Block{Kind: BlockAssistant, Text: "more output"})
	if got := tl.SelectedText(); got != before {
		t.Fatalf("appending must not move the selection: %q → %q", before, got)
	}
}
