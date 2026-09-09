package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Compacting is the one thing an agent does with nothing to show for itself
// while it happens, so it says so at the bottom with the honest numbers.
func TestTheCompactingRowSaysWhatIsHappening(t *testing.T) {
	a := testApp(t)
	a.resize(110, 26)
	s := a.cur()
	s.ctxTokens, s.ctxWindow = 814000, 1000000
	s.noteCompacting()
	s.busy, s.turnStart, s.turnOut = true, time.Now().Add(-23*time.Second), 1800

	view := stripSGR(a.View())
	for _, want := range []string{"Compacting conversation", "814k tokens", "23s", "↓ 1.8k written"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q:\n%s", want, lastLines(view, 3))
		}
	}
	if s.CompactingRows() != 1 {
		t.Fatalf("CompactingRows = %d", s.CompactingRows())
	}

	// And it goes when the compaction does.
	s.compacting, s.busy = false, false
	a.resize(a.w, a.h)
	if strings.Contains(stripSGR(a.View()), "Compacting conversation") {
		t.Fatal("the row should be gone")
	}
	if s.CompactingRows() != 0 {
		t.Fatal("and its row given back")
	}
}

// The bar sweeps rather than fills: nothing reports progress through a
// compaction, so a bar that crept along would be a guess dressed up as a
// measurement. What it must do is move.
func TestTheCompactingBarSweeps(t *testing.T) {
	block := func(d time.Duration) (start, width int) {
		at := 0
		for _, g := range sweep(d) {
			w := lipgloss.Width(g.text)
			if g.bg == T.Purple {
				return at, w
			}
			at += w
		}
		return -1, 0
	}
	// It enters from the left, crosses, and leaves on the right.
	if s, w := block(0); s != -1 && w > 0 && s != 0 {
		t.Fatalf("at the start it should be at the left edge or not yet in: %d,%d", s, w)
	}
	mid1, w1 := block(sweepPeriod / 3)
	mid2, w2 := block(sweepPeriod * 2 / 3)
	if w1 == 0 || w2 == 0 {
		t.Fatalf("the block should be visible mid-sweep: %d,%d", w1, w2)
	}
	if mid2 <= mid1 {
		t.Fatalf("it should move along: %d then %d", mid1, mid2)
	}
	// Every phase paints exactly the track's width, so the row cannot grow.
	for ms := 0; ms < 1300; ms += 50 {
		total := 0
		for _, g := range sweep(time.Duration(ms) * time.Millisecond) {
			total += lipgloss.Width(g.text)
		}
		if total != sweepCells {
			t.Fatalf("%dms painted %d cells, want %d", ms, total, sweepCells)
		}
	}
}

// The whole frame keeps its height while the row is up, at any width.
func TestTheCompactingRowFitsTheFrame(t *testing.T) {
	for _, w := range []int{80, 120, 200} {
		a := testApp(t)
		a.resize(w, 26)
		s := a.cur()
		s.ctxTokens = 500000
		s.noteCompacting()
		s.busy, s.turnStart = true, time.Now().Add(-5*time.Second)
		lines := strings.Split(a.View(), "\n")
		if len(lines) != 26 {
			t.Fatalf("width %d: %d rows, want 26", w, len(lines))
		}
		if got := lipgloss.Width(stripSGR(lines[len(lines)-1])); got != w {
			t.Fatalf("width %d: the row is %d columns", w, got)
		}
	}
}

// /compact lights the row, and remembers the size it is folding up so the
// line can say what is being spent.
func TestCompactingLightsTheRow(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	s.ctxTokens, s.ctxWindow = 900000, 1000000
	s.tl.Append(Block{Kind: BlockAssistant, Text: "something to summarise"})

	send(t, a, "/compact")
	if !s.compacting || s.compactFrom != 900000 {
		t.Fatalf("/compact: compacting=%v from=%d", s.compacting, s.compactFrom)
	}
	s.close()
}
