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
	// line 0 is the "⏵ Bash" header; line 1 is the railed command
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

	a.Update(drag(x, 0, tea.MouseActionPress)) // the "⏵ Bash" header row
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
	if a.cur().tl.HasSelection() || a.dragging {
		t.Fatal("selection only starts inside the conversation pane")
	}
}

func TestDragInTheDiffPaneDoesNotSelectTheTimeline(t *testing.T) {
	a := selApp(t)
	if !a.lay.ShowDiff {
		t.Skip("needs the diff pane")
	}
	x := a.lay.SidebarW + a.lay.TimelineW + 3
	a.Update(drag(x, 2, tea.MouseActionPress))
	a.Update(drag(x+4, 3, tea.MouseActionMotion))
	if a.dragging || a.cur().tl.HasSelection() {
		t.Fatal("the diff pane is not the conversation pane")
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
