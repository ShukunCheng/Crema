package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// threeAgents opens three, named by their directories so a reorder is visible.
func threeAgents(t *testing.T) *App {
	t.Helper()
	a := testApp(t)
	a.addSession(fastMock(), t.TempDir())
	a.addSession(fastMock(), t.TempDir())
	a.selectSession(0)
	return a
}

func order(a *App) []string {
	var out []string
	for _, s := range a.sessions {
		out = append(out, s.Dir)
	}
	return out
}

// agentRowY is the screen row an agent's sidebar entry is drawn on.
func agentRowY(i int) int { return sidebarRowY(SidebarRowOf(i)) }

// press, move, release: the row follows the pointer and the list ends up in
// the order it was dragged into.
func TestDraggingAnAgentReordersTheList(t *testing.T) {
	a := threeAgents(t)
	was := order(a)

	a.Update(tea.MouseMsg{X: 3, Y: agentRowY(0), Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	a.Update(tea.MouseMsg{X: 3, Y: agentRowY(2), Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	a.Update(tea.MouseMsg{X: 3, Y: agentRowY(2), Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})

	if got := order(a); got[2] != was[0] || got[0] != was[1] || got[1] != was[2] {
		t.Fatalf("order = %v, want the first one moved to the end of %v", got, was)
	}
	if a.draggingAgent() {
		t.Fatal("the drag should have ended on release")
	}
	if !strings.Contains(a.note, "moved") {
		t.Fatalf("note = %q", a.note)
	}
}

// The agent you are looking at is still the one you are looking at, whether it
// was the one dragged or merely shoved along by it.
func TestReorderingKeepsYouOnTheSameAgent(t *testing.T) {
	a := threeAgents(t)
	a.selectSession(2)
	watching := a.cur()

	a.moveSession(0, 2) // moves the other two around it
	if a.cur() != watching {
		t.Fatalf("focus jumped to %q", a.cur().Dir)
	}
	if a.active != 1 {
		t.Fatalf("active = %d, want the index the same agent now sits at", a.active)
	}

	a.selectSession(1)
	dragged := a.cur()
	a.moveSession(1, 0)
	if a.cur() != dragged || a.active != 0 {
		t.Fatalf("dragging the focused agent should carry the focus with it: %d", a.active)
	}
}

// Moving through the list a row at a time, the way a real drag arrives.
func TestADragMovesOneRowAtATime(t *testing.T) {
	a := threeAgents(t)
	first := a.sessions[0].Dir

	a.Update(tea.MouseMsg{X: 3, Y: agentRowY(0), Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	a.Update(tea.MouseMsg{X: 3, Y: agentRowY(1), Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	if a.sessions[1].Dir != first {
		t.Fatalf("after one row it should be second: %v", order(a))
	}
	a.Update(tea.MouseMsg{X: 3, Y: agentRowY(2), Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	if a.sessions[2].Dir != first {
		t.Fatalf("after two it should be last: %v", order(a))
	}
	// Back up again, still the same drag.
	a.Update(tea.MouseMsg{X: 3, Y: agentRowY(0), Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	if a.sessions[0].Dir != first {
		t.Fatalf("dragging back should return it: %v", order(a))
	}
	a.Update(tea.MouseMsg{X: 3, Y: agentRowY(0), Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
}

// A press that never moves is a click, and still selects.
func TestAPressWithoutAMoveIsStillJustAClick(t *testing.T) {
	a := threeAgents(t)
	was := order(a)
	clickPR(a, 3, agentRowY(1))
	if got := order(a); got[0] != was[0] || got[1] != was[1] || got[2] != was[2] {
		t.Fatalf("a click reordered the list: %v", got)
	}
	if a.active != 1 {
		t.Fatalf("and it should still select: active = %d", a.active)
	}
}

// The × closes rather than picks up, so a slip of the mouse on it cannot start
// a drag.
func TestTheCloseButtonIsNotAHandle(t *testing.T) {
	a := threeAgents(t)
	closeX := 1 + SidebarCloseCol(a.lay.SidebarW-2)
	a.Update(tea.MouseMsg{X: closeX, Y: agentRowY(0), Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if a.draggingAgent() {
		t.Fatal("the close button must not begin a drag")
	}
	if len(a.sessions) != 2 {
		t.Fatalf("it should have closed one: %d left", len(a.sessions))
	}
}

// Dragging over the sidebar never leaks into the conversation's text
// selection, which is what the same gesture means one column to the right.
func TestADragOverTheSidebarDoesNotSelectText(t *testing.T) {
	a := threeAgents(t)
	a.Update(tea.MouseMsg{X: 3, Y: agentRowY(0), Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	a.Update(tea.MouseMsg{X: 3, Y: agentRowY(1), Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	if a.cur().tl.HasSelection() || a.dragging != dragNone {
		t.Fatal("the conversation must not think it is being selected")
	}
	a.Update(tea.MouseMsg{X: 3, Y: agentRowY(1), Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
}

// Nonsense targets change nothing rather than panicking.
func TestMoveSessionIgnoresImpossibleMoves(t *testing.T) {
	a := threeAgents(t)
	was := order(a)
	for _, c := range [][2]int{{0, 0}, {-1, 1}, {0, 9}, {5, 5}} {
		a.moveSession(c[0], c[1])
	}
	for i, d := range order(a) {
		if d != was[i] {
			t.Fatalf("order changed: %v", order(a))
		}
	}
}

// Dropping a row somewhere that isn't an agent — the gap, the + row, the
// border — leaves it where it was rather than guessing.
// The block follows the mouse past the ends of the list: dragged below the
// last row it rides at the bottom, hauled back above the title it rides at
// the top. It is never dropped by the pointer straying.
func TestTheDraggedRowFollowsPastTheEnds(t *testing.T) {
	a := threeAgents(t)
	was := order(a)
	a.Update(tea.MouseMsg{X: 3, Y: agentRowY(0), Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	a.Update(tea.MouseMsg{X: 3, Y: a.lay.PaneH + 5, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	if got := order(a); got[2] != was[0] {
		t.Fatalf("dragged far below, the row should ride at the bottom: %v", got)
	}
	a.Update(tea.MouseMsg{X: 3, Y: 0, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	if got := order(a); got[0] != was[0] {
		t.Fatalf("hauled back above the title, it should ride at the top: %v", got)
	}
	a.Update(tea.MouseMsg{X: 3, Y: 0, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	for i, d := range order(a) {
		if d != was[i] {
			t.Fatalf("order = %v, want %v — it went down and came back", order(a), was)
		}
	}
}

// The row in hand is painted lifted — the user band behind it, a grab mark in
// the margin — so what is being carried reads as carried.
func TestTheDraggedRowLooksLifted(t *testing.T) {
	a := threeAgents(t)
	a.Update(tea.MouseMsg{X: 3, Y: agentRowY(1), Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	a.Update(tea.MouseMsg{X: 3, Y: agentRowY(2), Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	lines := strings.Split(RenderSidebar(a.sessions, a.active, a.dragAgent, "*", SidebarWidth-2, 10), "\n")
	if !strings.HasPrefix(stripSGR(lines[SidebarRowOf(2)]), "▌ ") {
		t.Fatalf("the carried row should wear the grab mark: %q", stripSGR(lines[SidebarRowOf(2)]))
	}
	a.Update(tea.MouseMsg{X: 3, Y: agentRowY(2), Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	lines = strings.Split(RenderSidebar(a.sessions, a.active, a.dragAgent, "*", SidebarWidth-2, 10), "\n")
	if strings.Contains(stripSGR(strings.Join(lines, "\n")), "▌") {
		t.Fatal("dropped, no row is in hand")
	}
}

// A click that never travels is a click: no "moved" note for going nowhere.
func TestAStillClickIsNotAnnouncedAsAMove(t *testing.T) {
	a := threeAgents(t)
	a.note = ""
	a.Update(tea.MouseMsg{X: 3, Y: agentRowY(1), Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	a.Update(tea.MouseMsg{X: 3, Y: agentRowY(1), Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	if strings.Contains(a.note, "moved") {
		t.Fatalf("note = %q", a.note)
	}
}
