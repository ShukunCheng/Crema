package ui

import (
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

// openControls presses ↓ from the input, which is how the row is reached.
func openControls(t *testing.T, a *App) *Controls {
	t.Helper()
	a.Update(kmsg(tea.KeyDown))
	if a.controls == nil {
		t.Fatal("↓ did not raise the button row")
	}
	return a.controls
}

// ↓ used to leave for a full-screen panel. It stays here now: the buttons
// above the input take the highlight, and the conversation stays on screen.
func TestDownHighlightsTheButtonsWithoutLeavingThePage(t *testing.T) {
	a := testApp(t)
	a.cur().tl.Append(Block{Kind: BlockAssistant, Text: "still here"})
	c := openControls(t, a)

	if c.Open() {
		t.Fatal("nothing should be open yet — ↓ only highlights")
	}
	view := stripSGR(a.View())
	if !strings.Contains(view, "still here") {
		t.Fatalf("the conversation was replaced:\n%s", view)
	}
	for _, want := range []string{"model", "permissions", "enter open"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the button row is missing %q:\n%s", want, view)
		}
	}
}

// Enter opens the highlighted button's values, starting on the current one.
func TestEnterOpensADropdownOnTheCurrentValue(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	s.SetModel("demo-slow")
	c := openControls(t, a)

	a.Update(kmsg(tea.KeyEnter))
	if !c.Open() {
		t.Fatal("enter should open the list")
	}
	if got := c.chips[c.idx].options[c.opt].label; got != "demo-slow" {
		t.Fatalf("the list opened on %q, want the value in force", got)
	}
	view := stripSGR(a.View())
	for _, want := range []string{"demo-fast", "demo-slow", "enter apply"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the list is missing %q:\n%s", want, view)
		}
	}
}

// Choosing applies to the focused agent and shuts the list, leaving the row up
// so the other button is one keystroke away.
func TestChoosingAppliesAndKeepsTheRow(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	c := openControls(t, a)

	a.Update(kmsg(tea.KeyEnter)) // open models
	a.Update(kmsg(tea.KeyDown))  // default → demo-fast
	a.Update(kmsg(tea.KeyEnter))
	if s.Model != "demo-fast" {
		t.Fatalf("Model = %q", s.Model)
	}
	if c.Open() {
		t.Fatal("choosing should close the list")
	}
	if a.controls == nil {
		t.Fatal("but not the row")
	}
	if !strings.Contains(stripSGR(a.View()), "demo-fast") {
		t.Fatal("the button should show what it is now set to")
	}

	// And the permission button, from the same row.
	a.Update(kmsg(tea.KeyRight))
	a.Update(kmsg(tea.KeyEnter))
	a.Update(kmsg(tea.KeyDown))
	a.Update(kmsg(tea.KeyEnter))
	if s.Permission == agent.PermissionAcceptEdits {
		t.Fatal("the permission should have changed")
	}
}

// The CLI's own default is a real choice, not an empty one.
func TestTheDefaultModelCanBeChosenBack(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	s.SetModel("demo-fast")
	openControls(t, a)
	a.Update(kmsg(tea.KeyEnter)) // open models, on demo-fast
	a.Update(kmsg(tea.KeyUp))    // back to default
	a.Update(kmsg(tea.KeyEnter))
	if s.Model != agent.DefaultModel {
		t.Fatalf("Model = %q, want the CLI's default", s.Model)
	}
}

// esc steps back out one layer at a time: list, row, input.
func TestEscapeUnwindsOneLayerAtATime(t *testing.T) {
	a := testApp(t)
	c := openControls(t, a)
	a.Update(kmsg(tea.KeyEnter))
	a.Update(kmsg(tea.KeyEsc))
	if a.controls == nil || c.Open() {
		t.Fatal("esc should close the list and leave the row")
	}
	a.Update(kmsg(tea.KeyEsc))
	if a.controls != nil {
		t.Fatal("esc again should close the row")
	}
	if a.focus != focusInput {
		t.Fatal("and give the input its focus back")
	}
	if a.cur().busy {
		t.Fatal("esc must not have reached the turn")
	}
}

// Typing is always a message: the row stands aside rather than eating letters.
func TestTypingDismissesTheRow(t *testing.T) {
	a := testApp(t)
	openControls(t, a)
	typeRunes(t, a, "hello")
	if a.controls != nil {
		t.Fatal("typing should have dismissed the row")
	}
	if a.in.Value() != "hello" {
		t.Fatalf("draft = %q", a.in.Value())
	}
}

// The buttons are buttons: clicking one opens it, clicking a value applies it.
func TestClickingAButtonAndThenAValue(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	c := openControls(t, a)

	row := a.lay.PaneH - a.dropUpHeight() // the button row, when nothing is open
	perm := c.chips[1]
	a.Update(click(perm.col+1, row))
	if !c.Open() || c.idx != 1 {
		t.Fatalf("clicking the permissions button should open it: idx=%d open=%v", c.idx, c.Open())
	}

	// The open list holds the bottom strip, where the status bar was.
	top := a.h - c.ListHeight()
	a.Update(click(4, top+1)) // the first value, just under the title
	if s.Permission != c.chips[1].options[0].perm {
		t.Fatalf("Permission = %q, want %q", s.Permission, c.chips[1].options[0].perm)
	}
}

// A change is written into the conversation, so the transcript explains why a
// later turn behaved differently — and it is on screen when it happens.
func TestAChangeIsRecordedInTheConversation(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	for i := 0; i < 40; i++ {
		s.tl.Append(Block{Kind: BlockAssistant, Text: "filler"})
	}
	s.tl.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	openControls(t, a)
	a.Update(kmsg(tea.KeyEnter))
	a.Update(kmsg(tea.KeyDown))
	a.Update(kmsg(tea.KeyEnter))
	if !strings.Contains(stripSGR(s.tl.View()), "demo-fast") {
		t.Fatalf("the change should be visible in the conversation:\n%s", stripSGR(s.tl.View()))
	}
}

// The row and its list live above the input, and the frame stays exactly as
// tall as the terminal.
func TestTheRowFitsInTheFrame(t *testing.T) {
	a := testApp(t)
	for _, size := range [][2]int{{140, 40}, {80, 24}, {64, 12}} {
		a.resize(size[0], size[1])
		a.controls = NewControls(a.cur())
		if got := len(strings.Split(a.View(), "\n")); got != size[1] {
			t.Fatalf("%dx%d: frame is %d lines with the row up", size[0], size[1], got)
		}
		a.controls.openList()
		if got := len(strings.Split(a.View(), "\n")); got != size[1] {
			t.Fatalf("%dx%d: frame is %d lines with a list open", size[0], size[1], got)
		}
	}
}

// The model list reads like the CLI's own picker: numbered, ticked, and each
// name followed by what it actually gets you.
func TestTheModelListSaysWhatEachOneIs(t *testing.T) {
	a := testApp(t)
	a.cur().Backend = &agent.Claude{}
	c := openControls(t, a)
	a.Update(kmsg(tea.KeyEnter))

	rows := c.chips[0].rows()
	if len(rows) != 5 {
		t.Fatalf("rows = %q", rows)
	}
	// The notes name no generation: an alias is whatever the CLI resolves it
	// to today, and pinning "Opus 5" here would be a lie the day Opus 6 ships.
	for i, want := range []string{
		"1. ✓ default  whatever the CLI resolves",
		"2.   opus     Opus with 1M context",
		"3.   fable    Fable",
		"4.   sonnet   Sonnet",
		"5.   haiku    Haiku",
	} {
		if !strings.HasPrefix(rows[i], want) {
			t.Fatalf("row %d is %q, want it to start %q", i+1, rows[i], want)
		}
	}
	if !strings.Contains(stripSGR(a.View()), "1-5 pick") {
		t.Fatal("the hint should say the numbers work")
	}
}

// A backend with nothing to say about its models still lists them.
func TestAModelListWithoutNotesIsJustNames(t *testing.T) {
	a := testApp(t) // the mock is no ModelDescriber
	c := openControls(t, a)
	rows := c.chips[0].rows()
	if len(rows) != 3 || !strings.HasPrefix(rows[1], "2.   demo-fast") {
		t.Fatalf("rows = %q", rows)
	}
	if strings.Contains(rows[1], "  ") != true { // padding only, no description
		t.Fatalf("row = %q", rows[1])
	}
}

// The number picks the row, which is quicker than arrowing to the fifth.
func TestANumberPicksAModel(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	s.Backend = &agent.Claude{}
	openControls(t, a)
	a.Update(kmsg(tea.KeyEnter)) // open the model list
	press(t, a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	if s.Model != "fable" {
		t.Fatalf("Model = %q, want the third row", s.Model)
	}
	if a.controls.Open() {
		t.Fatal("picking closes the list")
	}
}

// A number past the end of the list is not a pick.
func TestANumberBeyondTheListDoesNothing(t *testing.T) {
	a := testApp(t)
	s := a.cur() // the mock offers three
	openControls(t, a)
	a.Update(kmsg(tea.KeyEnter))
	press(t, a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'9'}})
	if s.Model != agent.DefaultModel {
		t.Fatalf("Model = %q, want it untouched", s.Model)
	}
	if !a.controls.Open() {
		t.Fatal("the list should still be open")
	}
}

// The permission list is numbered the same way, with its own descriptions.
func TestThePermissionListIsNumberedToo(t *testing.T) {
	a := testApp(t)
	c := openControls(t, a)
	a.Update(kmsg(tea.KeyRight))
	rows := c.chips[1].rows()
	if !strings.HasPrefix(rows[0], "1. ") || !strings.Contains(rows[0], "ask") {
		t.Fatalf("rows = %q", rows)
	}
	if !strings.Contains(strings.Join(rows, "\n"), "file edits apply") {
		t.Fatalf("the modes should keep their descriptions: %q", rows)
	}
}

// The open list takes the status bar's place at the bottom of the frame —
// choosing is the one thing happening — and gives it back on close, with the
// panes yielding exactly the difference in rows.
func TestTheOpenListReplacesTheStatusBar(t *testing.T) {
	a := testApp(t)
	a.resize(100, 26)
	paneH := a.lay.PaneH
	c := openControls(t, a)
	if v := stripSGR(a.View()); !strings.Contains(v, "Context") {
		t.Fatal("buttons alone leave the status bar in place")
	}

	a.Update(kmsg(tea.KeyEnter)) // open the model list
	v := stripSGR(a.View())
	if !strings.Contains(v, "MODEL") || !strings.Contains(v, "1. ✓ default") {
		t.Fatalf("the picker should hold the bottom strip:\n%s", v)
	}
	if strings.Contains(v, "Context") {
		t.Fatal("the status bar stands aside while a choice is being made")
	}
	if lines := strings.Split(a.View(), "\n"); len(lines) != 26 {
		t.Fatalf("frame is %d rows, want 26 exactly", len(lines))
	}
	if want := paneH - (c.ListHeight() - StatusRows(26)); a.lay.PaneH != want {
		t.Fatalf("PaneH = %d, want %d — the panes yield the difference", a.lay.PaneH, want)
	}

	a.Update(kmsg(tea.KeyEsc)) // back to the buttons
	if !strings.Contains(stripSGR(a.View()), "Context") {
		t.Fatal("closing the list gives the status bar its place back")
	}
	if a.lay.PaneH != paneH {
		t.Fatalf("PaneH = %d, want %d restored", a.lay.PaneH, paneH)
	}
}

// A click on a value in the bottom strip applies it, and the strip closes.
func TestClickingTheBottomStripPicksAValue(t *testing.T) {
	a := testApp(t)
	a.resize(100, 26)
	s := a.cur()
	c := openControls(t, a)
	a.Update(kmsg(tea.KeyRight)) // over to permissions
	a.Update(kmsg(tea.KeyEnter)) // open its list
	top := a.h - c.ListHeight()
	a.Update(click(5, top+1)) // the first value: the CLI's own default
	if s.Permission != c.chips[1].options[0].perm {
		t.Fatalf("Permission = %q", s.Permission)
	}
	if c.Open() {
		t.Fatal("picking closes the list")
	}
	if !strings.Contains(stripSGR(a.View()), "Context") {
		t.Fatal("and the status bar comes back")
	}
}
