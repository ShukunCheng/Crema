package ui

import (
	"strings"
	"testing"
)

// An agent's name is yours to set: three agents on one project all read
// "mock · Crema" otherwise.
func TestRenamingAnAgent(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	derived := s.Title()

	send(t, a, "/rename the API one")
	if s.Title() != "the API one" {
		t.Fatalf("Title = %q", s.Title())
	}
	if !strings.Contains(a.note, "is now the API one") {
		t.Fatalf("note = %q", a.note)
	}
	if !strings.Contains(stripSGR(a.View()), "the API one") {
		t.Fatal("the sidebar should show it")
	}
	if s.busy {
		t.Fatal("renaming is crema's own work; no turn")
	}

	// Nothing takes it back to the name it had.
	send(t, a, "/rename")
	if s.Title() != derived {
		t.Fatalf("Title = %q, want the derived %q", s.Title(), derived)
	}
	if s.Name != "" {
		t.Fatalf("Name should be empty again, got %q", s.Name)
	}
}

func TestARenameIsTrimmedAndKept(t *testing.T) {
	dir := t.TempDir()
	a := testApp(t)
	a.cur().Dir = dir
	send(t, a, "/rename    spaced out   ")
	if got := a.cur().Name; got != "spaced out" {
		t.Fatalf("Name = %q", got)
	}
	if err := SaveState(a.StateSnapshot()); err != nil {
		t.Fatal(err)
	}
	b := testApp(t)
	b.sessions = nil
	b.RestoreSessions(LoadState())
	if got := b.cur().Title(); got != "spaced out" {
		t.Fatalf("after a restart: %q", got)
	}
}

// Two clicks on a row mean rename. There is no text field in the sidebar, so
// the command lands in the box you already type in, ready to edit.
func TestDoubleClickingARowStartsARename(t *testing.T) {
	a := threeAgents(t)
	a.sessions[1].Rename("web")

	clickPR(a, 3, agentRowY(1))
	if a.in.Value() != "" {
		t.Fatalf("one click just selects: %q", a.in.Value())
	}
	clickPR(a, 3, agentRowY(1))
	if got := a.in.Value(); got != "/rename web" {
		t.Fatalf("draft = %q, want the rename filled in", got)
	}
	if a.active != 1 || a.focus != focusInput {
		t.Fatalf("active = %d focus = %v", a.active, a.focus)
	}

	// Editing and sending it does the rename.
	a.in.SetValue("/rename web · staging")
	send(t, a, a.in.Value())
	if got := a.sessions[1].Title(); got != "web · staging" {
		t.Fatalf("Title = %q", got)
	}
}

// Two clicks on different rows are two selections, not a rename.
func TestClicksOnDifferentRowsAreNotARename(t *testing.T) {
	a := threeAgents(t)
	clickPR(a, 3, agentRowY(0))
	clickPR(a, 3, agentRowY(2))
	if a.in.Value() != "" {
		t.Fatalf("draft = %q", a.in.Value())
	}
	if a.active != 2 {
		t.Fatalf("the second click should still select: %d", a.active)
	}
}

// The × still closes, even twice in a row.
func TestDoubleClickingTheCloseButtonDoesNotRename(t *testing.T) {
	a := threeAgents(t)
	closeX := 1 + SidebarCloseCol(a.lay.SidebarW-2)
	clickPR(a, closeX, agentRowY(0))
	if a.in.Value() != "" {
		t.Fatalf("draft = %q", a.in.Value())
	}
	if len(a.sessions) != 2 {
		t.Fatalf("it should have closed one: %d left", len(a.sessions))
	}
}

// /rename is crema's own now — it used to be turned away as interactive-only.
func TestRenameIsOfferedAndNoLongerBlocked(t *testing.T) {
	for _, n := range interactiveOnly {
		if n == "rename" {
			t.Fatal("crema can rename an agent, so it must not block the name")
		}
	}
	a := testApp(t)
	var found bool
	for _, c := range allCommands(a.cur()) {
		found = found || (c.Name == "rename" && c.Scope == "crema")
	}
	if !found {
		t.Fatal("/rename should be in the list as crema's own")
	}
}

// A name only changes the label, not which directory the agent works in.
func TestRenamingChangesNothingButTheLabel(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	dir, backend := s.Dir, s.Backend
	send(t, a, "/rename anything")
	if s.Dir != dir || s.Backend != backend {
		t.Fatal("a rename must not move the agent")
	}
	pump(t, a, send(t, a, "hello"))
	if s.lastOpts.Dir != dir {
		t.Fatalf("the turn ran in %q", s.lastOpts.Dir)
	}
	s.close()
}
