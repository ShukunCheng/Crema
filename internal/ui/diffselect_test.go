package ui

import (
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/gitdiff"
)

func codeSample() gitdiff.DiffSet {
	return gitdiff.DiffSet{Files: []gitdiff.File{{
		Path: "main.go", Status: "modified", Additions: 2, Deletions: 1,
		Hunks: []gitdiff.Hunk{{
			Header: "@@ -14,4 +14,5 @@", OldStart: 14, NewStart: 14,
			Lines: []gitdiff.Line{
				{Kind: gitdiff.LineContext, Text: "func main() {"},
				{Kind: gitdiff.LineDel, Text: "    old line"},
				{Kind: gitdiff.LineAdd, Text: "    new line"},
				{Kind: gitdiff.LineAdd, Text: "    extra line"},
				{Kind: gitdiff.LineContext, Text: "}"},
			},
		}},
	}}}
}

// rowWith is the content line holding text, and the column it starts at.
func rowWith(t *testing.T, d *DiffPanel, text string) (int, int) {
	t.Helper()
	for i, l := range d.contentLines() {
		plain := stripSGR(l)
		if j := strings.Index(plain, text); j >= 0 {
			return i, len([]rune(plain[:j]))
		}
	}
	t.Fatalf("no line holds %q:\n%s", text, strings.Join(d.contentLines(), "\n"))
	return 0, 0
}

// In full screen the two sides sit next to each other, each with its line
// numbers. A drag through the new code used to take everything it crossed —
// the old side, both gutters — so pasting it gave both versions interleaved
// with numbers. It copies the code on the side the drag started on, and
// nothing else.
func TestADragInTheSplitDiffCopiesOneSidesCode(t *testing.T) {
	d := NewDiffPanel(120, 20)
	d.SetDiff(codeSample())
	d.SetView(DiffFull)

	top, _ := rowWith(t, d, "func main() {")
	bottom, _ := rowWith(t, d, "}")
	// Start in the right side's line-number gutter, drag to the far right of
	// the last line: the whole new version, without its numbers.
	l := d.contentLines()[top]
	divide := d.rows[top].divide
	if divide == 0 {
		t.Fatalf("the full-screen diff should be split:\n%s", stripSGR(l))
	}
	d.BeginSelect(top, divide+1)
	d.ExtendSelect(bottom, 10_000)
	got := d.EndSelect()
	want := "func main() {\n    new line\n    extra line\n}"
	if got != want {
		t.Fatalf("copied:\n%q\nwant the new side's code only:\n%q", got, want)
	}

	// And the left side gives the old version: the deleted line, and no gap
	// where the new side had a line the old one did not.
	d.BeginSelect(top, 0)
	d.ExtendSelect(bottom, divide-2)
	if got, want := d.EndSelect(), "func main() {\n    old line\n}"; got != want {
		t.Fatalf("copied:\n%q\nwant the old side's code only:\n%q", got, want)
	}
}

// The unified view has no gutter, but it has the +/- marker in front of each
// line, which is not code either.
func TestADragInTheUnifiedDiffLeavesTheMarkers(t *testing.T) {
	d := NewDiffPanel(80, 20)
	d.SetDiff(codeSample())
	d.ToggleCollapse(DiffFileKey(codeSample().Files[0]))
	from, _ := rowWith(t, d, "old line")
	to, _ := rowWith(t, d, "new line")
	d.BeginSelect(from, 0)
	d.ExtendSelect(to, 10_000)
	if got, want := d.EndSelect(), "    old line\n    new line"; got != want {
		t.Fatalf("copied %q, want %q", got, want)
	}
}

// A drag that starts on a header is not a drag through code, and selects the
// way it always has.
func TestADragFromAHeaderIsUnrestricted(t *testing.T) {
	d := NewDiffPanel(120, 20)
	d.SetDiff(codeSample())
	d.SetView(DiffFull)
	hunk, _ := rowWith(t, d, "@@ -14,4 +14,5 @@")
	d.BeginSelect(hunk, 0)
	d.ExtendSelect(hunk+1, 10_000)
	got := d.EndSelect()
	if !strings.Contains(got, "@@ -14,4 +14,5 @@") || !strings.Contains(got, "14") {
		t.Fatalf("from a header everything is fair game, got %q", got)
	}
}
