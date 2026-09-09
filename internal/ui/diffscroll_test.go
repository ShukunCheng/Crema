package ui

import (
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/gitdiff"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// wideDiff is one file with one very long line, which is what a real diff of
// anything with a long call or a long string in it looks like in a narrow pane.
func wideDiff() gitdiff.DiffSet {
	long := "func handle(ctx context.Context, req *Request) (*Response, error) { return nil, ERRORHERE }"
	return gitdiff.DiffSet{Files: []gitdiff.File{{
		Path: "a.go", Status: "modified", Additions: 1,
		Hunks: []gitdiff.Hunk{{
			Header: "@@ -1,2 +1,2 @@",
			Lines: []gitdiff.Line{
				{Kind: gitdiff.LineAdd, Text: long},
				{Kind: gitdiff.LineContext, Text: "short"},
			},
		}},
	}}}
}

func openWide(t *testing.T, w, h int) *DiffPanel {
	t.Helper()
	d := NewDiffPanel(w, h)
	ds := wideDiff()
	d.SetDiff(ds)
	d.ToggleCollapse(DiffFileKey(ds.Files[0])) // files arrive folded
	return d
}

// A long line used to be cut at the pane's width and marked with "›", which
// hides the end — where the arguments and the string literal are. Now the body
// slides, and the end can be reached.
func TestTheDiffScrollsSideways(t *testing.T) {
	d := openWide(t, 40, 20)
	if d.contentW <= d.bodyWidth() {
		t.Fatalf("contentW = %d, want wider than the pane's %d", d.contentW, d.bodyWidth())
	}
	before := stripSGR(d.View())
	if !strings.Contains(before, "func handle") {
		t.Fatalf("the start of the line should be on screen:\n%s", before)
	}
	if strings.Contains(before, "ERRORHERE") {
		t.Fatal("and the end should not be, yet")
	}
	if !d.ScrollX(d.contentW) { // all the way right
		t.Fatal("it should scroll")
	}
	after := stripSGR(d.View())
	if !strings.Contains(after, "ERRORHERE") {
		t.Fatalf("the end of the line should be reachable:\n%s", after)
	}
	if d.ScrollX(hscrollStep) {
		t.Fatal("and it should stop at the last column rather than run on into padding")
	}
}

// The bar says how much of the width is on screen and where, and it is only
// there when there is something to scroll to.
func TestTheHorizontalBarAppearsOnlyWhenItIsNeeded(t *testing.T) {
	d := openWide(t, 40, 20)
	view := stripSGR(d.View())
	if !strings.Contains(view, "▌") {
		t.Fatalf("a thumb should be on the bar:\n%s", view)
	}
	if !strings.Contains(view, "of "+itoa(d.contentW)) {
		t.Fatalf("the bar should name the columns on show:\n%s", view)
	}
	// The bar takes its row off the body rather than off the frame.
	tall := NewDiffPanel(40, 20)
	tall.SetDiff(gitdiff.DiffSet{})
	if tall.hbarRows() != 0 {
		t.Fatal("nothing to scroll, so no bar")
	}
	if got, want := len(strings.Split(d.View(), "\n")), len(strings.Split(tall.View(), "\n")); got != want {
		t.Fatalf("the frame is %d rows with the bar and %d without; it should not change", got, want)
	}
}

// A pane wide enough for the content does not scroll, and asking it to does
// nothing rather than sliding the code off the left.
func TestAWidePaneDoesNotScroll(t *testing.T) {
	d := openWide(t, 200, 20)
	if d.hbarRows() != 0 {
		t.Fatalf("the whole line fits in 200 columns, so there is no bar: contentW=%d", d.contentW)
	}
	if d.ScrollX(hscrollStep) || d.XOff() != 0 {
		t.Fatalf("and nowhere to go, xoff = %d", d.XOff())
	}
}

// Shift and the wheel is how the rest of the desktop scrolls sideways.
func TestShiftWheelScrollsSideways(t *testing.T) {
	d := openWide(t, 40, 20)
	down := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown, Shift: true}
	up := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp, Shift: true}
	d.Update(down)
	if d.XOff() != hscrollStep {
		t.Fatalf("shift+wheel down should go right, xoff = %d", d.XOff())
	}
	d.Update(up)
	if d.XOff() != 0 {
		t.Fatalf("shift+wheel up should come back, xoff = %d", d.XOff())
	}
	// Without shift the wheel still scrolls up and down, and not sideways.
	d.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if d.XOff() != 0 {
		t.Fatalf("a plain wheel must not move it sideways, xoff = %d", d.XOff())
	}
	// A terminal that reports a sideways wheel needs no shift.
	d.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelRight})
	if d.XOff() != hscrollStep {
		t.Fatalf("a sideways wheel should move it, xoff = %d", d.XOff())
	}
}

// ←/→ move it too, for anyone who has the diff focused and no mouse.
func TestArrowsScrollTheDiffSideways(t *testing.T) {
	d := openWide(t, 40, 20)
	d.Update(tea.KeyMsg{Type: tea.KeyRight})
	if d.XOff() != hscrollStep {
		t.Fatalf("→ should go right, xoff = %d", d.XOff())
	}
	d.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if d.XOff() != 0 {
		t.Fatalf("← should come back, xoff = %d", d.XOff())
	}
	// home is the top of the diff and its left edge both.
	d.ScrollX(hscrollStep * 2)
	d.Update(tea.KeyMsg{Type: tea.KeyHome})
	if d.XOff() != 0 {
		t.Fatalf("home should also come back to the left, xoff = %d", d.XOff())
	}
}

// A selection in a scrolled diff takes the text under the pointer, not the
// text that used to be there.
func TestSelectingInAScrolledDiffTakesWhatIsOnScreen(t *testing.T) {
	a := testApp(t)
	a.diffView = DiffSide
	a.resize(160, 30)
	s := a.cur()
	ds := wideDiff()
	s.dp.SetDiff(ds)
	s.dp.ToggleCollapse(DiffFileKey(ds.Files[0]))
	if s.dp.maxXOff() == 0 {
		t.Skip("the pane is wide enough here; nothing to scroll")
	}
	s.dp.ScrollX(hscrollStep)
	_, col := a.diffPoint(s, a.lay.SidebarW+a.lay.TimelineW+1, 3)
	if col < hscrollStep {
		t.Fatalf("a screen column should map past the offset, got %d", col)
	}
}

// Rows are drawn at the content's width, but one pathological line — a
// minified bundle, a base64 blob — must not pad every row on the screen out to
// its length.
func TestAnAbsurdlyLongLineIsBounded(t *testing.T) {
	ds := gitdiff.DiffSet{Files: []gitdiff.File{{
		Path: "bundle.js", Status: "modified",
		Hunks: []gitdiff.Hunk{{Header: "@@ -1 +1 @@", Lines: []gitdiff.Line{
			{Kind: gitdiff.LineAdd, Text: strings.Repeat("x", 500_000)},
		}}},
	}}}
	d := NewDiffPanel(80, 20)
	d.SetDiff(ds)
	d.ToggleCollapse(DiffFileKey(ds.Files[0]))
	if d.contentW > maxDiffWidth {
		t.Fatalf("contentW = %d, want it bounded at %d", d.contentW, maxDiffWidth)
	}
}

// The bar takes its row off the body, not off the frame. Every row of the
// frame is still exactly the terminal's width and there are still exactly as
// many of them — a pane that miscounts by a line pushes the status bar, with
// its buttons on it, off the bottom of the screen.
func TestTheBarDoesNotPushTheFrameOutOfShape(t *testing.T) {
	for _, w := range []int{130, 160, 200} {
		a := testApp(t)
		a.diffView = DiffSide
		a.resize(w, 30)
		s := a.cur()
		ds := wideDiff()
		s.dp.SetDiff(ds)
		s.dp.ToggleCollapse(DiffFileKey(ds.Files[0]))
		a.resize(w, 30)
		if s.dp.hbarRows() == 0 {
			t.Fatalf("w=%d: this test needs the bar to be there", w)
		}
		lines := strings.Split(a.View(), "\n")
		if len(lines) != 30 {
			t.Fatalf("w=%d: frame is %d rows, want 30", w, len(lines))
		}
		for i, ln := range lines {
			if got := lipgloss.Width(stripSGR(ln)); got != w {
				t.Fatalf("w=%d: row %d is %d columns: %q", w, i, got, stripSGR(ln))
			}
		}
	}
}
