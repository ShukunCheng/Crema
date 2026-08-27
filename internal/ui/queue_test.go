package ui

import (
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

// busyTurn starts a turn and leaves it running, the way the agent does while
// it works.
func busyTurn(t *testing.T, a *App) *Session {
	t.Helper()
	s := a.cur()
	typeRunes(t, a, "first job")
	press(t, a, kmsg(tea.KeyEnter))
	if !s.busy {
		t.Fatal("the first message should have started a turn")
	}
	return s
}

// finish ends the running turn the way the stream does.
func finish(t *testing.T, a *App, s *Session) {
	t.Helper()
	seq := s.streamSeq
	a.Update(agentEventMsg{sess: s.ID, seq: seq,
		ev: agent.Event{Kind: agent.KindTurnEnd, Result: &agent.TurnResult{}}})
	// The result ends a leg; the stream closing is what ends the turn.
	a.Update(streamClosedMsg{sess: s.ID, seq: seq})
}

// Typing while the agent works is allowed: the message waits behind the turn
// and goes on its own the moment the turn ends.
func TestAMessageSentWhileBusyIsQueuedThenSent(t *testing.T) {
	a := testApp(t)
	s := busyTurn(t, a)

	typeRunes(t, a, "and then the tests")
	press(t, a, kmsg(tea.KeyEnter))

	if a.in.Value() != "" {
		t.Fatalf("the draft should have been taken: %q", a.in.Value())
	}
	if got := s.Queued(); len(got) != 1 || got[0] != "and then the tests" {
		t.Fatalf("queued = %q", got)
	}
	if !strings.Contains(a.note, "queued") {
		t.Fatalf("note = %q, want it to say the message is waiting", a.note)
	}
	if !strings.Contains(stripSGR(a.View()), "and then the tests") {
		t.Fatal("a waiting message must be on screen")
	}

	finish(t, a, s)
	if len(s.Queued()) != 0 {
		t.Fatalf("the queue should have been drained: %q", s.Queued())
	}
	if !s.busy {
		t.Fatal("the queued message should have started the next turn")
	}
	if got := s.lastOpts.Prompt; got != "and then the tests" {
		t.Fatalf("the agent was sent %q", got)
	}
	s.close()
}

// Several messages keep their order.
func TestQueuedMessagesGoInTurn(t *testing.T) {
	a := testApp(t)
	s := busyTurn(t, a)
	for _, m := range []string{"one", "two"} {
		typeRunes(t, a, m)
		press(t, a, kmsg(tea.KeyEnter))
	}
	if got := s.Queued(); len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("queued = %q", got)
	}
	finish(t, a, s)
	if s.lastOpts.Prompt != "one" {
		t.Fatalf("sent %q first, want %q", s.lastOpts.Prompt, "one")
	}
	finish(t, a, s)
	if s.lastOpts.Prompt != "two" {
		t.Fatalf("sent %q second, want %q", s.lastOpts.Prompt, "two")
	}
	if len(s.Queued()) != 0 {
		t.Fatalf("queue should be empty: %q", s.Queued())
	}
	s.close()
}

// An image pasted into a queued message is still expanded when it goes.
func TestAQueuedMessageKeepsItsAttachment(t *testing.T) {
	a := testApp(t)
	s := busyTurn(t, a)
	stubClipboard(t, `C:\tmp\shot.png`, nil, "", nil)

	typeRunes(t, a, "look at ")
	press(t, a, tea.KeyMsg{Type: tea.KeyCtrlV})
	press(t, a, kmsg(tea.KeyEnter))

	if got := s.Queued(); len(got) != 1 || got[0] != "look at [Image #1]" {
		t.Fatalf("queued = %q, want the marker", got)
	}
	finish(t, a, s)
	if got, want := s.lastOpts.Prompt, `look at C:\tmp\shot.png`; got != want {
		t.Fatalf("the agent was sent %q, want %q", got, want)
	}
	s.close()
}

// Cancelling means cancelling: what was written expecting this turn to finish
// does not fire off the moment it dies.
func TestCancellingDropsTheQueue(t *testing.T) {
	a := testApp(t)
	s := busyTurn(t, a)
	typeRunes(t, a, "and then the tests")
	press(t, a, kmsg(tea.KeyEnter))

	press(t, a, kmsg(tea.KeyEsc))
	if len(s.Queued()) != 0 {
		t.Fatalf("esc must drop the queue: %q", s.Queued())
	}
	if !strings.Contains(a.note, "dropped") {
		t.Fatalf("note = %q, want it to say what was dropped", a.note)
	}
	finish(t, a, s)
	if s.busy {
		t.Fatal("nothing should have started after the cancel")
	}
}

// clickQueued clicks the nth waiting message, which is drawn after the
// conversation rather than in a box of its own.
func clickQueued(a *App, n int) {
	tl := a.cur().tl
	_ = a.View() // the pending rows are reconciled when the frame is drawn
	clickPR(a, a.lay.SidebarW+2, tl.blockLines()+tl.statusRows()+n-tl.YOffset())
}

// Clicking a waiting message takes it back out — into the input box, which is
// the only reason to want it back: to change it and send it again.
func TestClickingAQueuedMessagePutsItBackInTheBox(t *testing.T) {
	a := testApp(t)
	s := busyTurn(t, a)
	for _, m := range []string{"one", "two"} {
		typeRunes(t, a, m)
		press(t, a, kmsg(tea.KeyEnter))
	}
	clickQueued(a, 0)
	if got := s.Queued(); len(got) != 1 || got[0] != "two" {
		t.Fatalf("queued = %q, want the first one gone", got)
	}
	if got := a.in.Value(); got != "one" {
		t.Fatalf("the draft is %q, want the message back", got)
	}
	if !strings.Contains(a.note, "back in the box") {
		t.Fatalf("note = %q", a.note)
	}
	// And it can be sent again, going back to the end of the queue.
	press(t, a, kmsg(tea.KeyEnter))
	if got := s.Queued(); len(got) != 2 || got[1] != "one" {
		t.Fatalf("queued = %q, want it re-queued last", got)
	}
	s.close()
}

// A draft being written is never overwritten, and the message stays queued so
// neither one is lost.
func TestUnqueueingWillNotOverwriteADraft(t *testing.T) {
	a := testApp(t)
	s := busyTurn(t, a)
	typeRunes(t, a, "waiting message")
	press(t, a, kmsg(tea.KeyEnter))
	typeRunes(t, a, "half a thought")

	clickQueued(a, 0)
	if got := a.in.Value(); got != "half a thought" {
		t.Fatalf("the draft was overwritten: %q", got)
	}
	if len(s.Queued()) != 1 {
		t.Fatalf("the message should still be waiting: %q", s.Queued())
	}
	if !strings.Contains(a.note, "draft") {
		t.Fatalf("note should say why nothing happened: %q", a.note)
	}
	s.close()
}

// A picture comes back with the message it was attached to, so re-sending it
// hands the agent the file rather than the literal marker.
func TestUnqueueingBringsBackTheAttachment(t *testing.T) {
	a := testApp(t)
	s := busyTurn(t, a)
	stubClipboard(t, `C:\tmp\shot.png`, nil, "", nil)
	typeRunes(t, a, "look at ")
	press(t, a, tea.KeyMsg{Type: tea.KeyCtrlV})
	press(t, a, kmsg(tea.KeyEnter))

	clickQueued(a, 0)
	if got := a.in.Value(); got != "look at [Image #1]" {
		t.Fatalf("draft = %q", got)
	}
	press(t, a, kmsg(tea.KeyEnter)) // re-queue, then let the turn finish
	finish(t, a, s)
	if got, want := s.lastOpts.Prompt, `look at C:\tmp\shot.png`; got != want {
		t.Fatalf("the agent was sent %q, want %q", got, want)
	}
	s.close()
}

// A waiting message belongs at the end of the conversation, where it is going
// to land — not in a box floating over it.
func TestWaitingMessagesSitAtTheEndOfTheConversation(t *testing.T) {
	a := testApp(t)
	s := busyTurn(t, a)
	typeRunes(t, a, "then the tests")
	press(t, a, kmsg(tea.KeyEnter))

	if a.dropUpHeight() != 0 {
		t.Fatalf("the queue should not raise a box of its own: %d rows", a.dropUpHeight())
	}
	lines := strings.Split(strings.TrimRight(stripSGR(s.tl.Content()), "\n"), "\n")
	last := lines[len(lines)-1]
	if !strings.Contains(last, "then the tests") || !strings.Contains(last, "waiting") {
		t.Fatalf("the last line of the conversation is %q", last)
	}
	if !strings.Contains(stripSGR(s.tl.View()), "then the tests") {
		t.Fatal("and it should be on screen")
	}

	// It stops being a pending line the moment it becomes a real one.
	finish(t, a, s)
	_ = a.View()
	content := stripSGR(s.tl.Content())
	if strings.Contains(content, "· waiting") {
		t.Fatalf("the sent message should no longer be pending:\n%s", content)
	}
	if strings.Count(content, "then the tests") != 1 {
		t.Fatalf("it should appear once, as a sent message:\n%s", content)
	}
	s.close()
}

// Each agent shows its own queue, not the focused one's.
func TestEachAgentShowsItsOwnQueue(t *testing.T) {
	a := testApp(t)
	first := busyTurn(t, a)
	typeRunes(t, a, "for the first")
	press(t, a, kmsg(tea.KeyEnter))

	second := a.addSession(first.Backend, t.TempDir())
	a.selectSession(1)
	_ = a.View()
	if strings.Contains(stripSGR(second.tl.Content()), "for the first") {
		t.Fatal("the other agent's queue leaked into this one")
	}
	if !strings.Contains(stripSGR(first.tl.Content()), "for the first") {
		t.Fatal("a background agent should still show what it has waiting")
	}
	first.close()
	second.close()
}
