package ui

import (
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
)

// grow puts the agent at a given share of its window, with something to
// summarise.
func grow(s *Session, frac float64) {
	s.ctxWindow = 1_000_000
	s.ctxTokens = int64(frac * 1_000_000)
	s.tl.Append(Block{Kind: BlockAssistant, Text: "a long conversation"})
}

// A conversation that has grown expensive folds itself up when the turn
// ends, the way an interactive session does — every turn pays to re-read it,
// and a cold process pays to rewrite it.
func TestAGrownConversationFoldsItselfUp(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	grow(s, 0.85)
	s.busy = true
	s.noteResult(&agent.TurnResult{})
	cmds := a.finishTurn(s)
	if !s.compacting {
		t.Fatal("at 85% of the window it should be folding up")
	}
	if len(cmds) == 0 {
		t.Fatal("and the summarising turn should have started")
	}
	got := stripSGR(s.tl.Content())
	if !strings.Contains(got, "85% of the window") || !strings.Contains(got, "/autocompact off") {
		t.Fatalf("it should say what it is doing and how to stop it:\n%s", got)
	}
	s.close()
}

// Below the line it leaves well alone: summarising costs a turn, and a small
// conversation is not worth one.
func TestASmallConversationIsLeftAlone(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	grow(s, 0.5)
	s.busy = true
	s.noteResult(&agent.TurnResult{})
	a.finishTurn(s)
	if s.compacting {
		t.Fatal("half a window is not expensive enough to fold")
	}
}

// It is per agent and it is remembered.
func TestAutoCompactCanBeTurnedOffAndIsRemembered(t *testing.T) {
	dir := t.TempDir()
	a := testApp(t)
	s := a.cur()
	s.Dir = dir
	if !s.AutoCompact {
		t.Fatal("on by default, like the CLI's own")
	}
	send(t, a, "/autocompact off")
	if s.AutoCompact {
		t.Fatal("off means off")
	}
	grow(s, 0.9)
	s.busy = true
	s.noteResult(&agent.TurnResult{})
	a.finishTurn(s)
	if s.compacting {
		t.Fatal("switched off, it must not fold anything")
	}

	if err := SaveState(a.StateSnapshot()); err != nil {
		t.Fatal(err)
	}
	b := testApp(t)
	b.sessions = nil
	b.RestoreSessions(LoadState())
	if b.cur().AutoCompact {
		t.Fatal("the switch should survive a restart")
	}
}

// An agent saved before the switch existed comes back with it on.
func TestAnOlderAgentComesBackWithItOn(t *testing.T) {
	a := testApp(t)
	a.cur().Dir = t.TempDir()
	st := a.StateSnapshot()
	st.Sessions[0].NoAutoCompact = false // as an older state file would have it
	if err := SaveState(st); err != nil {
		t.Fatal(err)
	}
	b := testApp(t)
	b.sessions = nil
	b.RestoreSessions(LoadState())
	if !b.cur().AutoCompact {
		t.Fatal("the default is on")
	}
}

// /autocompact with no argument reports where things stand.
func TestAutoCompactReportsItself(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	grow(s, 0.42)
	send(t, a, "/autocompact")
	got := lastBlock(s)
	if !strings.Contains(got, "autocompact is on") || !strings.Contains(got, "42%") {
		t.Fatalf("report = %q", got)
	}
	send(t, a, "/autocompact sideways")
	if !strings.Contains(a.note, "on, or /autocompact off") {
		t.Fatalf("note = %q", a.note)
	}
}

// An answer taller than the pane is shown from its first line: the end of a
// long answer is its least useful part.
func TestALongAnswerIsShownFromItsStart(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	for i := 0; i < 60; i++ {
		s.tl.Append(Block{Kind: BlockAssistant, Text: "an earlier line"})
	}
	send(t, a, "/help")
	view := stripSGR(s.tl.View())
	if !strings.Contains(view, "crema's own commands") {
		t.Fatalf("a long answer should start at its start:\n%s", view)
	}
	// A short answer still lands at the end, where new output appears.
	send(t, a, "/autocompact")
	if !strings.Contains(stripSGR(s.tl.View()), "autocompact is on") {
		t.Fatal("a short answer belongs at the end, in view")
	}
}
