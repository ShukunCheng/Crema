package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// ask sends a message and lets the turn finish, so the next one can be sent.
func ask(t *testing.T, a *App, text string) {
	t.Helper()
	pump(t, a, send(t, a, text))
}

// ↑ walks back through what you have asked, newest first; ↓ walks forward
// again and ends on the draft it interrupted.
func TestUpWalksBackThroughWhatYouAsked(t *testing.T) {
	a := testApp(t)
	for _, m := range []string{"first thing", "second thing", "third thing"} {
		ask(t, a, m)
	}
	typeRunes(t, a, "half-written")

	press(t, a, kmsg(tea.KeyUp))
	if got := a.in.Value(); got != "third thing" {
		t.Fatalf("first ↑ = %q, want the newest", got)
	}
	press(t, a, kmsg(tea.KeyUp))
	press(t, a, kmsg(tea.KeyUp))
	if got := a.in.Value(); got != "first thing" {
		t.Fatalf("third ↑ = %q, want the oldest", got)
	}
	press(t, a, kmsg(tea.KeyUp)) // no wrap: the oldest is the end of the road
	if got := a.in.Value(); got != "first thing" {
		t.Fatalf("past the oldest = %q", got)
	}

	press(t, a, kmsg(tea.KeyDown))
	if got := a.in.Value(); got != "second thing" {
		t.Fatalf("↓ = %q", got)
	}
	press(t, a, kmsg(tea.KeyDown))
	press(t, a, kmsg(tea.KeyDown))
	if got := a.in.Value(); got != "half-written" {
		t.Fatalf("↓ off the end should restore the draft, got %q", got)
	}
	a.cur().close()
}

// Only once the history is back where it started does ↓ reach the buttons.
func TestDownReachesTheButtonsAfterTheHistory(t *testing.T) {
	a := testApp(t)
	ask(t, a, "something")
	press(t, a, kmsg(tea.KeyUp))
	press(t, a, kmsg(tea.KeyDown)) // back to the empty draft
	if a.controls != nil {
		t.Fatal("↓ inside the history must not open the buttons")
	}
	press(t, a, kmsg(tea.KeyDown))
	if a.controls == nil {
		t.Fatal("↓ at the end of the history should reach the buttons")
	}
	a.cur().close()
}

// With nothing asked yet, ↑ has nothing to say and ↓ still finds the buttons.
func TestArrowsWithoutAHistory(t *testing.T) {
	a := testApp(t)
	press(t, a, kmsg(tea.KeyUp))
	if a.in.Value() != "" {
		t.Fatalf("draft = %q", a.in.Value())
	}
	press(t, a, kmsg(tea.KeyDown))
	if a.controls == nil {
		t.Fatal("↓ should still open the buttons")
	}
}

// Typing makes the draft yours again: the next ↑ starts from the newest.
func TestTypingEndsTheWalk(t *testing.T) {
	a := testApp(t)
	ask(t, a, "one")
	ask(t, a, "two")
	press(t, a, kmsg(tea.KeyUp))
	press(t, a, kmsg(tea.KeyUp))
	if got := a.in.Value(); got != "one" {
		t.Fatalf("walked to %q", got)
	}
	typeRunes(t, a, "!")
	if a.browsing() {
		t.Fatal("typing should have ended the walk")
	}
	press(t, a, kmsg(tea.KeyUp))
	if got := a.in.Value(); got != "two" {
		t.Fatalf("↑ after typing = %q, want the newest again", got)
	}
	a.cur().close()
}

// A recalled message is sent like any other, and lands at the front rather
// than being repeated in place.
func TestARecalledMessageCanBeSentAgain(t *testing.T) {
	a := testApp(t)
	ask(t, a, "run the tests")
	ask(t, a, "now the linter")
	press(t, a, kmsg(tea.KeyUp))
	press(t, a, kmsg(tea.KeyUp))
	ask(t, a, a.in.Value()) // "run the tests", sent again

	h := a.cur().History()
	if len(h) != 3 || h[2] != "run the tests" {
		t.Fatalf("history = %q", h)
	}
	a.cur().close()
}

// The same thing twice in a row is one entry, the way a shell's history works.
func TestRepeatsCollapse(t *testing.T) {
	s := NewSession(1, nil, "")
	for _, m := range []string{"go test", "go test", " go test ", "go vet", "go test"} {
		s.remember(m)
	}
	if got := s.History(); len(got) != 3 ||
		got[0] != "go test" || got[1] != "go vet" || got[2] != "go test" {
		t.Fatalf("history = %q", got)
	}
	s.remember("   ")
	if len(s.History()) != 3 {
		t.Fatalf("blank input is not history: %q", s.History())
	}
}

// It is bounded, like everything else crema keeps.
func TestHistoryIsCapped(t *testing.T) {
	s := NewSession(1, nil, "")
	for i := 0; i < maxHistory+50; i++ {
		s.remember(strings.Repeat("x", i+1))
	}
	h := s.History()
	if len(h) != maxHistory {
		t.Fatalf("kept %d entries, want %d", len(h), maxHistory)
	}
	if h[len(h)-1] != strings.Repeat("x", maxHistory+50) {
		t.Fatal("the newest entry should survive the cap")
	}
}

// Each agent has its own; switching does not offer you the other one's.
func TestEachAgentHasItsOwnHistory(t *testing.T) {
	a := testApp(t)
	ask(t, a, "for the first agent")
	a.addSession(a.sessions[0].Backend, t.TempDir())
	a.selectSession(1)

	press(t, a, kmsg(tea.KeyUp))
	if got := a.in.Value(); got != "" {
		t.Fatalf("a new agent has no history, got %q", got)
	}
	ask(t, a, "for the second")
	a.selectSession(0)
	press(t, a, kmsg(tea.KeyUp))
	if got := a.in.Value(); got != "for the first agent" {
		t.Fatalf("↑ = %q, want the first agent's own", got)
	}
	for _, s := range a.sessions {
		s.close()
	}
}

// A multi-line draft keeps the arrows as cursor movement.
func TestArrowsMoveTheCursorInAMultiLineDraft(t *testing.T) {
	a := testApp(t)
	ask(t, a, "earlier")
	a.in.SetValue("one\ntwo")
	press(t, a, kmsg(tea.KeyUp))
	if got := a.in.Value(); got != "one\ntwo" {
		t.Fatalf("the draft was replaced: %q", got)
	}
	if a.browsing() {
		t.Fatal("↑ in a multi-line draft is cursor movement")
	}
	a.cur().close()
}

// What you typed outlives what the agent knows: /clear is about the
// conversation, not about your own notes.
func TestClearKeepsTheHistory(t *testing.T) {
	a := testApp(t)
	ask(t, a, "worth keeping")
	send(t, a, "/clear")
	press(t, a, kmsg(tea.KeyUp))
	if got := a.in.Value(); got != "/clear" {
		t.Fatalf("↑ = %q, want the last thing typed", got)
	}
	press(t, a, kmsg(tea.KeyUp))
	if got := a.in.Value(); got != "worth keeping" {
		t.Fatalf("↑↑ = %q", got)
	}
}

// And it survives a restart, which is most of the point of having it.
func TestHistorySurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	a := testApp(t)
	a.cur().Dir = dir
	ask(t, a, "the thing I always run")
	if err := SaveState(a.StateSnapshot()); err != nil {
		t.Fatal(err)
	}
	a.cur().close()

	b := testApp(t)
	b.sessions = nil
	b.RestoreSessions(LoadState())
	press(t, b, kmsg(tea.KeyUp))
	if got := b.in.Value(); got != "the thing I always run" {
		t.Fatalf("↑ after a restart = %q", got)
	}
}
