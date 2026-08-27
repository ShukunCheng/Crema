package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// The sidebar's order is the user's order: alt+1..9 jumps by position, tab
// walks it, and which agent is "the first one" is a decision about how you
// work rather than about when you happened to open it. So a row can be dragged
// to where it belongs.
//
// The move happens as the pointer crosses a row rather than on release, so the
// list under the cursor is always the list you will get — there is no ghost row
// to draw and no separate commit to get wrong.

// noDrag is the resting value of dragAgent: nothing is being moved.
const noDrag = -1

// beginAgentDrag remembers which row a press landed on, so a press that turns
// into a drag can move it. A press that never moves is just a click.
func (a *App) beginAgentDrag(i int) { a.dragAgent, a.dragMoved = i, false }

// dragAgentTo moves the agent being dragged to the row now under the pointer.
// The pointer's row is clamped to the list, so the block follows the mouse
// everywhere: dragged above the first row it rides at the top, below the last
// it rides at the bottom, and in between it is always the row under the
// cursor. It reports whether anything moved.
func (a *App) dragAgentTo(y int) bool {
	if a.dragAgent == noDrag || len(a.sessions) == 0 {
		return false
	}
	i := y - 1 - sidebarTitleRows // the pane's border, then the AGENTS title
	i = max(0, min(i, len(a.sessions)-1))
	if i == a.dragAgent {
		return false
	}
	a.moveSession(a.dragAgent, i)
	a.dragAgent = i
	a.dragMoved = true
	return true
}

// endAgentDrag finishes the move, saying and saving where the row landed —
// once, on the drop, rather than for every row it crossed on the way.
func (a *App) endAgentDrag() {
	if a.dragAgent == noDrag {
		return
	}
	if a.dragMoved {
		a.note = "moved " + a.sessions[a.dragAgent].Title() + " to " + itoa(a.dragAgent+1)
		a.persist() // the order is worth keeping, like everything else about an agent
	}
	a.dragAgent = noDrag
}

// dragging reports whether an agent is being moved, which is what stops a drag
// across the sidebar from also selecting text somewhere else.
func (a *App) draggingAgent() bool { return a.dragAgent != noDrag }

// moveSession takes the agent at from and puts it at to, sliding the rest
// along. Whoever you were looking at is still who you are looking at
// afterwards: the focus follows the agent, not the position.
func (a *App) moveSession(from, to int) {
	n := len(a.sessions)
	if from < 0 || to < 0 || from >= n || to >= n || from == to {
		return
	}
	focused := a.sessions[a.active]
	moved := a.sessions[from]

	rest := append(a.sessions[:from:from], a.sessions[from+1:]...)
	a.sessions = append(rest[:to:to], append([]*Session{moved}, rest[to:]...)...)

	for i, s := range a.sessions {
		if s == focused {
			a.active = i
			break
		}
	}
}

// sidebarMouse is the sidebar's whole share of the mouse: press to pick up,
// motion to move, release to put down. It reports handled for the events it
// consumed so a drag over the sidebar never becomes a text selection.
func (a *App) sidebarMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
	if !a.lay.ShowSidebar {
		return nil, false
	}
	// A drop-up over the bottom of the panes covers the sidebar's columns too,
	// and belongs to whatever raised it.
	inSidebar := msg.X < a.lay.SidebarW && msg.Y < a.lay.PaneH && !a.overCompletions(msg.Y)
	switch {
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && inSidebar:
		// Clicking a row twice is how a list says "rename this". Crema has no
		// text field in the sidebar to type into, so it writes the command into
		// the box you already type in, with the current name to edit.
		if target, i := SidebarRowAt(len(a.sessions), msg.Y-1); target == SidebarSession &&
			msg.Y >= 1 && a.secondClickOn(i) {
			return a.startRename(i), true
		}
		if s := a.cur(); s != nil {
			// Clicking away from the text panes drops the highlight, the same
			// as it did before the sidebar took this press for itself.
			s.tl.ClearSelection()
			s.dp.ClearSelection()
		}
		// The × closes rather than moves, and only a row with an agent on it
		// can be picked up at all.
		if target, i := SidebarRowAt(len(a.sessions), msg.Y-1); target == SidebarSession &&
			msg.Y >= 1 && msg.X-1 < SidebarCloseCol(a.lay.SidebarW-2) {
			a.beginAgentDrag(i)
		}
		return a.clickSidebar(msg.X, msg.Y), true

	case a.draggingAgent() && msg.Action == tea.MouseActionMotion:
		a.dragAgentTo(msg.Y)
		return nil, true

	case a.draggingAgent() && msg.Action == tea.MouseActionRelease:
		a.endAgentDrag()
		return nil, true
	}
	return nil, false
}

// doubleClick is how close together two presses on the same row have to be to
// mean "rename" rather than "select, then select again".
const doubleClick = 500 * time.Millisecond

// secondClickOn reports whether this press is the second of a pair on the same
// agent. It reads the clock directly rather than through the paste heuristic's
// seam, which tests deliberately run fast and loose.
func (a *App) secondClickOn(i int) bool {
	now := time.Now()
	was, when := a.lastRow, a.lastClick
	a.lastRow, a.lastClick = i, now
	return was == i && now.Sub(when) < doubleClick
}

// startRename puts the rename in the input box, filled in with the name the
// agent has now, so it can be edited rather than retyped.
func (a *App) startRename(i int) tea.Cmd {
	a.selectSession(i)
	s := a.sessions[i]
	a.in.SetValue("/rename " + s.Title())
	a.endBrowsing()
	a.note = "edit the name and press enter"
	a.focus = focusInput
	return a.in.Focus()
}
