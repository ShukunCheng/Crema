package ui

import (
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/gitdiff"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func splitSample() gitdiff.DiffSet {
	return gitdiff.DiffSet{Files: []gitdiff.File{{
		Path: "main.go", Status: "modified", Additions: 2, Deletions: 1,
		Hunks: []gitdiff.Hunk{{
			Header: "@@ -14,4 +14,5 @@", OldStart: 14, NewStart: 14,
			Lines: []gitdiff.Line{
				{Kind: gitdiff.LineContext, Text: "func main() {"},
				{Kind: gitdiff.LineDel, Text: "\told line"},
				{Kind: gitdiff.LineAdd, Text: "\tnew line"},
				{Kind: gitdiff.LineAdd, Text: "\textra line"},
				{Kind: gitdiff.LineContext, Text: "}"},
			},
		}},
	}}}
}

// A replaced line shows its before and after on the same row; the extra
// insertion pairs with a blank, which is what makes it read as an insertion.
func TestPairHunkAlignsChangesAndFillsGaps(t *testing.T) {
	rows := pairHunk(splitSample().Files[0].Hunks[0])
	want := []splitRow{
		{oldNum: 14, oldText: "func main() {", hasOld: true,
			newNum: 14, newText: "func main() {", hasNew: true},
		{oldNum: 15, oldText: "\told line", hasOld: true,
			newNum: 15, newText: "\tnew line", hasNew: true, changed: true},
		{newNum: 16, newText: "\textra line", hasNew: true, changed: true},
		{oldNum: 16, oldText: "}", hasOld: true,
			newNum: 17, newText: "}", hasNew: true},
	}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d: %+v", len(rows), len(want), rows)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Fatalf("row %d = %+v, want %+v", i, rows[i], want[i])
		}
	}
}

// Deletions with no replacement leave the right-hand side empty.
func TestPairHunkLeavesTheNewSideEmptyForADeletion(t *testing.T) {
	rows := pairHunk(gitdiff.Hunk{
		OldStart: 1, NewStart: 1,
		Lines: []gitdiff.Line{
			{Kind: gitdiff.LineDel, Text: "gone"},
			{Kind: gitdiff.LineContext, Text: "kept"},
		},
	})
	if len(rows) != 2 {
		t.Fatalf("got %+v", rows)
	}
	if rows[0].newNum != 0 || rows[0].newText != "" {
		t.Fatalf("a deletion must leave the new side blank: %+v", rows[0])
	}
	if rows[1].oldNum != 2 || rows[1].newNum != 1 {
		t.Fatalf("numbering must carry on per side: %+v", rows[1])
	}
}

func TestSplitViewPutsBothSidesOnOneRow(t *testing.T) {
	rows := renderSplitRows(splitSample(), 100, allOpen(splitSample()))
	text := joinRows(rows)
	if !strings.Contains(text, "before") || !strings.Contains(text, "after") {
		t.Fatalf("the columns must be labelled:\n%s", text)
	}
	var paired string
	for _, r := range rows {
		if strings.Contains(r.text, "old line") {
			paired = r.text
		}
	}
	if paired == "" {
		t.Fatalf("no row for the replaced line:\n%s", text)
	}
	if !strings.Contains(paired, "new line") {
		t.Fatalf("the replacement belongs on the same row: %q", paired)
	}
	if !strings.Contains(paired, "15") {
		t.Fatalf("the line-number gutters are missing: %q", paired)
	}
}

// Two columns need room. Below that the split gives way to the unified view
// rather than shredding every line.
func TestSplitViewFallsBackWhenTooNarrow(t *testing.T) {
	narrow := joinRows(renderSplitRows(splitSample(), 30, allOpen(splitSample())))
	if !strings.Contains(narrow, "+\tnew line") && !strings.Contains(narrow, "+") {
		t.Fatalf("expected the unified view:\n%s", narrow)
	}
	if strings.Contains(narrow, "before") {
		t.Fatalf("expected no split columns:\n%s", narrow)
	}
}

// ctrl+t walks the diff through its three sizes, and the split view only
// appears at the full-screen stop.
func TestCtrlTCyclesHiddenSideAndFullScreen(t *testing.T) {
	a := testApp(t)
	a.resize(140, 30)
	a.cur().dp.SetDiff(splitSample())
	if a.diffView != DiffSide || !a.lay.ShowDiff || a.lay.FullDiff {
		t.Fatalf("crema starts with the diff beside the conversation: %+v", a.lay)
	}
	if a.cur().dp.full {
		t.Fatal("the pane view is unified")
	}

	a.Update(kmsg(tea.KeyCtrlT)) // → full screen
	if !a.lay.FullDiff || a.lay.TimelineW != 0 || a.lay.ShowSidebar {
		t.Fatalf("full screen means the diff has every column: %+v", a.lay)
	}
	if a.lay.DiffW != 140 {
		t.Fatalf("diff width = %d, want the whole terminal", a.lay.DiffW)
	}
	if !a.cur().dp.full {
		t.Fatal("full screen draws the split view")
	}
	if a.focus != focusDiff {
		t.Fatal("with nothing else on screen the diff takes the focus")
	}
	if !strings.Contains(a.View(), "before") {
		t.Fatal("the split columns are not on screen")
	}

	a.Update(kmsg(tea.KeyCtrlT)) // → hidden
	if a.lay.ShowDiff || a.lay.FullDiff {
		t.Fatalf("the third press hides it: %+v", a.lay)
	}
	if a.focus != focusInput {
		t.Fatal("focus comes back to the input when the diff goes away")
	}

	a.Update(kmsg(tea.KeyCtrlT)) // → beside the conversation again
	if !a.lay.ShowDiff || a.lay.FullDiff || a.cur().dp.full {
		t.Fatalf("back to the unified pane: %+v", a.lay)
	}
}

// The size buttons live on the diff's own header now, not three lines away in
// the status bar, and each goes straight to that size rather than cycling.
func TestTheHeaderButtonsResizeTheDiff(t *testing.T) {
	a := testApp(t)
	a.resize(140, 30)
	a.cur().dp.SetDiff(sampleDiff())
	head := stripSGR(a.View())
	for _, want := range []string{" side ", " full ", " off "} {
		if !strings.Contains(head, want) {
			t.Fatalf("the header should offer %q:\n%s", want, head)
		}
	}

	// The buttons sit at the right edge of the pane's content.
	left := a.lay.SidebarW + a.lay.TimelineW
	full := left + 1 + a.lay.DiffW - 2 - modeButtons() + lipgloss.Width(modeText("side")) + 1
	clickPR(a, full, 1)
	if !a.lay.FullDiff {
		t.Fatalf("clicking full should go full screen: %+v", a.lay)
	}

	off := a.lay.DiffW - 2 - lipgloss.Width(modeText("off")) + 1
	clickPR(a, off, 1)
	if a.lay.ShowDiff {
		t.Fatalf("clicking off should hide it: %+v", a.lay)
	}
	// And the status bar no longer carries one.
	if strings.Contains(stripSGR(a.View()), "◨") {
		t.Fatal("the status bar chip should be gone")
	}
}

// Files arrive folded in both views: the header stays, the body waits to be
// asked for.
func TestFilesArriveFoldedInBothViews(t *testing.T) {
	key := DiffFileKey(splitSample().Files[0])
	for _, view := range []struct {
		name string
		rows func(map[string]bool) []diffRow
	}{
		{"unified", func(open map[string]bool) []diffRow {
			return renderDiffRows(splitSample(), 100, open)
		}},
		{"split", func(open map[string]bool) []diffRow {
			return renderSplitRows(splitSample(), 100, open)
		}},
	} {
		t.Run(view.name, func(t *testing.T) {
			folded := joinRows(view.rows(nil)) // nothing opened yet
			if strings.Contains(folded, "old line") {
				t.Fatalf("a file must arrive folded:\n%s", folded)
			}
			if !strings.Contains(folded, "▸") || !strings.Contains(folded, "main.go") {
				t.Fatalf("the header stays, marked folded:\n%s", folded)
			}
			open := joinRows(view.rows(map[string]bool{key: true}))
			if !strings.Contains(open, "old line") {
				t.Fatalf("opening it must show the diff:\n%s", open)
			}
			if !strings.Contains(open, "▾") {
				t.Fatalf("an open file is marked open:\n%s", open)
			}
		})
	}
}

// Hiding the diff takes its header away with it, so the way back sits in the
// status bar until it returns.
func TestAHiddenDiffKeepsAWayBack(t *testing.T) {
	a := testApp(t)
	a.resize(140, 30)
	if start, _ := ShowDiffRange(a.w, DiffSide); start != 0 {
		t.Fatal("a visible diff carries its own buttons; the bar needs none")
	}

	a.diffView = DiffHidden
	a.resize(140, 30)
	view := stripSGR(a.View())
	if !strings.Contains(view, "[ diff ]") {
		t.Fatalf("no way to bring the diff back:\n%s", view)
	}

	start, end := ShowDiffRange(a.w, DiffHidden)
	if theme, _ := ThemeToggleRange(a.w); end != theme {
		t.Fatalf("the chip should sit just left of the theme one: %d vs %d", end, theme)
	}
	last := strings.Split(view, "\n")
	if got := []rune(last[len(last)-1])[start:end]; string(got) != chip("diff", showDiffWidth) {
		t.Fatalf("the chip is not where hit-testing looks: %q", string(got))
	}

	a.Update(click(start+1, a.h-1))
	if !a.lay.ShowDiff {
		t.Fatalf("clicking it must bring the diff back: %+v", a.lay)
	}
	// And once it is back, its own header takes over again.
	if !strings.Contains(stripSGR(a.View()), " side ") {
		t.Fatal("the pane's own buttons should be showing")
	}
	if strings.Contains(stripSGR(a.View()), "[ diff ]") {
		t.Fatal("and the status bar's stand-in should be gone")
	}
}
