package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// inputAt is the screen position of a row and column of the draft, so a test
// drags where a person would point.
func inputAt(a *App, row, col int) (x, y int) {
	return 1 + a.in.promptW() + col, a.lay.PaneH + 1 + row
}

func dragInputFrom(a *App, r1, c1, r2, c2 int) {
	x1, y1 := inputAt(a, r1, c1)
	x2, y2 := inputAt(a, r2, c2)
	a.Update(drag(x1, y1, tea.MouseActionPress))
	a.Update(drag(x2, y2, tea.MouseActionMotion))
	a.Update(drag(x2, y2, tea.MouseActionRelease))
}

// Crema holds the mouse, so the terminal's own selection is gone and the box
// was the one pane where a drag did nothing — the pane holding the thing you
// just wrote.
func TestDraggingInTheInputSelects(t *testing.T) {
	a := testApp(t)
	a.resize(80, 24)
	typeRunes(t, a, "fix the parser")
	dragInputFrom(a, 0, 4, 0, 14)
	if got := a.in.SelectedText(); got != "the parser" {
		t.Fatalf("selection = %q, want %q", got, "the parser")
	}
	if !a.in.HasSelection() {
		t.Fatal("and it should be a real selection")
	}
	if strings.Contains(a.in.SelectedText(), "❯") {
		t.Fatal("the prompt is decoration, not text that was typed")
	}
}

// The highlight is drawn, and only over what it covers — the prompt in front
// of the row keeps its own colour.
func TestTheInputHighlightIsVisible(t *testing.T) {
	restoreTheme(t)
	trueColor(t)
	a := testApp(t)
	a.resize(80, 24)
	typeRunes(t, a, "fix the parser")
	plain := a.in.View()
	dragInputFrom(a, 0, 4, 0, 14)
	marked := a.in.View()
	if marked == plain {
		t.Fatal("a selection must be visible")
	}
	if !strings.Contains(stripSGR(marked), "fix the parser") {
		t.Fatalf("and the text must survive it: %q", stripSGR(marked))
	}
}

// A drag that ends where it began is a click: it places the caret rather than
// selecting nothing.
func TestAClickInTheInputPlacesTheCaret(t *testing.T) {
	a := testApp(t)
	a.resize(80, 24)
	typeRunes(t, a, "fix the parser")
	dragInputFrom(a, 0, 4, 0, 4)
	if a.in.HasSelection() {
		t.Fatal("a click is not a selection")
	}
	typeRunes(t, a, "X")
	if got := a.in.Value(); got != "fix Xthe parser" {
		t.Fatalf("the caret should have gone where it was clicked, got %q", got)
	}
}

// Typing over a highlight replaces it, the way a text box does — a selection
// you cannot type over is only half a selection.
func TestTypingOverASelectionReplacesIt(t *testing.T) {
	a := testApp(t)
	a.resize(80, 24)
	typeRunes(t, a, "fix the parser")
	dragInputFrom(a, 0, 4, 0, 14)
	typeRunes(t, a, "it")
	if got := a.in.Value(); got != "fix it" {
		t.Fatalf("draft = %q, want %q", got, "fix it")
	}
	if a.in.HasSelection() {
		t.Fatal("and the highlight goes with the text it covered")
	}
}

// Backspace over one deletes it and nothing else.
func TestBackspaceOverASelectionDeletesIt(t *testing.T) {
	a := testApp(t)
	a.resize(80, 24)
	typeRunes(t, a, "fix the parser")
	dragInputFrom(a, 0, 4, 0, 14)
	press(t, a, kmsg(tea.KeyBackspace))
	if got := a.in.Value(); got != "fix " {
		t.Fatalf("draft = %q, want %q", got, "fix ")
	}
	// And the next backspace takes one character, not a second selection.
	press(t, a, kmsg(tea.KeyBackspace))
	if got := a.in.Value(); got != "fix" {
		t.Fatalf("draft = %q, want %q", got, "fix")
	}
}

// A double-click takes a word, by the same rule as everywhere else in crema.
func TestDoubleClickInTheInputSelectsAWord(t *testing.T) {
	a := testApp(t)
	a.resize(80, 24)
	typeRunes(t, a, "on fix/12-order now")
	x, y := inputAt(a, 0, 12) // inside "order"
	for i := 0; i < 2; i++ {
		a.Update(drag(x, y, tea.MouseActionPress))
		a.Update(drag(x, y, tea.MouseActionRelease))
	}
	if got := a.in.SelectedText(); got != "order" {
		t.Fatalf("double-click selected %q, want %q — punctuation is a boundary", got, "order")
	}
}

// ctrl+a takes the draft, which is what it means in a text box; typing then
// replaces the lot.
func TestCtrlASelectsTheWholeDraft(t *testing.T) {
	a := testApp(t)
	a.resize(80, 24)
	typeRunes(t, a, "throw this away")
	press(t, a, kmsg(tea.KeyCtrlA))
	if got := a.in.SelectedText(); got != "throw this away" {
		t.Fatalf("ctrl+a selected %q", got)
	}
	typeRunes(t, a, "new")
	if got := a.in.Value(); got != "new" {
		t.Fatalf("draft = %q, want %q", got, "new")
	}
}

// A selection in the box is copied by ctrl+c, like one anywhere else, and esc
// drops it before esc means anything else.
func TestTheInputSelectionCopiesAndClears(t *testing.T) {
	a := testApp(t)
	a.resize(80, 24)
	var copied string
	old := copyToClipboard
	copyToClipboard = func(s string) error { copied = s; return nil }
	t.Cleanup(func() { copyToClipboard = old })

	typeRunes(t, a, "fix the parser")
	dragInputFrom(a, 0, 4, 0, 14)
	if copied != "the parser" {
		t.Fatalf("a finished drag copies: %q", copied)
	}
	copied = ""
	press(t, a, kmsg(tea.KeyCtrlC))
	if copied != "the parser" {
		t.Fatalf("ctrl+c copies it again: %q", copied)
	}
	press(t, a, kmsg(tea.KeyEsc))
	if a.in.HasSelection() {
		t.Fatal("esc drops the highlight")
	}
	if a.in.Value() != "fix the parser" {
		t.Fatal("and leaves the draft alone")
	}
}

// A draft that has wrapped selects across its rows, since the box's grid and
// the draft's are not the same thing.
func TestSelectingAcrossAWrappedDraft(t *testing.T) {
	a := testApp(t)
	a.resize(40, 24)
	typeRunes(t, a, "alpha beta gamma delta epsilon zeta eta")
	if a.in.TextRows() < 2 {
		t.Fatalf("this test needs a wrapped draft, got %d rows", a.in.TextRows())
	}
	const draft = "alpha beta gamma delta epsilon zeta eta"
	dragInputFrom(a, 0, 6, 1, 4)
	got := a.in.SelectedText()
	if !strings.HasPrefix(got, "beta") {
		t.Fatalf("a selection across rows should start where it began: %q", got)
	}
	// A wrap is not a line break. What comes out is what was typed, with no
	// newline the draft never had — reading it back off the rows put one in
	// the middle of a sentence and pasted it somewhere else.
	if strings.Contains(got, "\n") {
		t.Fatalf("a soft wrap must not become a newline: %q", got)
	}
	if !strings.Contains(draft, got) {
		t.Fatalf("the selection should be a piece of the draft: %q", got)
	}
	// And deleting it takes exactly that piece.
	press(t, a, kmsg(tea.KeyBackspace))
	if want := strings.Replace(draft, got, "", 1); a.in.Value() != want {
		t.Fatalf("after delete draft = %q, want %q", a.in.Value(), want)
	}
}

// The draft is not disturbed by asking where a position falls: the caret walk
// that answers it puts the caret back.
func TestMeasuringDoesNotMoveTheCaret(t *testing.T) {
	a := testApp(t)
	a.resize(40, 24)
	typeRunes(t, a, "alpha beta gamma delta epsilon zeta eta")
	press(t, a, tea.KeyMsg{Type: tea.KeyCtrlLeft}) // caret before "eta"
	was := a.in.caretOffset()
	_ = a.in.offsetAt(0, 3)
	if got := a.in.caretOffset(); got != was {
		t.Fatalf("the caret moved from %d to %d", was, got)
	}
	typeRunes(t, a, "X")
	if got := a.in.Value(); !strings.HasSuffix(got, "Xeta") {
		t.Fatalf("and typing should still land where it was: %q", got)
	}
}
