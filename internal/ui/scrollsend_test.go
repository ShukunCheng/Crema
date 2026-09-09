package ui

import (
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

// tallTalk fills the conversation past the pane and scrolls back up it, which
// is the state every test here starts from: reading something older.
func tallTalk(t *testing.T) (*App, *Session) {
	t.Helper()
	a := testApp(t)
	a.resize(100, 24)
	s := a.cur()
	for i := 0; i < 80; i++ {
		s.tl.Append(Block{Kind: BlockAssistant, Text: "an older line"})
	}
	s.tl.Update(kmsg(tea.KeyHome))
	if s.tl.Following() {
		t.Fatal("the pane should be scrolled back, not following")
	}
	return a, s
}

// Sending a message ends the reading: the answer to it arrives at the bottom,
// so that is where the pane goes.
func TestSendingScrollsToTheBottom(t *testing.T) {
	a, s := tallTalk(t)
	send(t, a, "what about this")
	if !s.tl.Following() {
		t.Fatal("sending should take the pane back to the bottom")
	}
	if !strings.Contains(stripSGR(s.tl.View()), "what about this") {
		t.Fatalf("and the message should be on screen:\n%s", stripSGR(s.tl.View()))
	}
}

// The one that was actually broken: a message sent into a busy turn only joins
// the queue, and startTurn — which is what used to do the scrolling — never
// runs. The queue is drawn at the bottom of the conversation, so it went
// somewhere the eye was not.
func TestQueueingAMessageScrollsToTheBottomToo(t *testing.T) {
	a, s := tallTalk(t)
	s.busy = true
	send(t, a, "one more thing")
	if len(s.Queued()) != 1 {
		t.Fatalf("it should have queued, got %q", s.Queued())
	}
	if !s.tl.Following() {
		t.Fatal("queueing should take the pane back to the bottom")
	}
	_ = a.View() // the queue is drawn by the frame
	if !strings.Contains(stripSGR(s.tl.View()), "one more thing") {
		t.Fatalf("the waiting message should be visible:\n%s", stripSGR(s.tl.View()))
	}
	s.busy = false
}

// A crema command is an answer like any other, and it lands at the bottom.
func TestABuiltinScrollsToTheBottom(t *testing.T) {
	a, s := tallTalk(t)
	send(t, a, "/cost")
	if !strings.Contains(stripSGR(s.tl.View()), "spent on this agent") {
		t.Fatalf("the answer should be on screen:\n%s", stripSGR(s.tl.View()))
	}
}

// A long answer is still shown from its first line — going to the bottom must
// not undo that, since the end of /help is its least useful part.
func TestALongBuiltinAnswerIsStillShownFromItsStart(t *testing.T) {
	a, s := tallTalk(t)
	send(t, a, "/help")
	if !strings.Contains(stripSGR(s.tl.View()), "crema's own commands") {
		t.Fatalf("a long answer should start at its start:\n%s", stripSGR(s.tl.View()))
	}
}

// Sending while looking at a subagent's transcript comes back to the agent's
// own conversation: that is where the answer is going to arrive, whether the
// message was for the agent or for the subagent it relays to.
func TestSendingLeavesASubagentTranscript(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	s.agentSID = "sess-9"
	s.noteTask(&agent.TaskUpdate{ID: "a1", Status: "running", Type: "Explore"})
	a.resize(120, 30)
	a.viewing = "a1"
	send(t, a, "how is it going")
	if a.viewing != "" {
		t.Fatalf("the pane should be back on the conversation, viewing = %q", a.viewing)
	}
	if !strings.Contains(stripSGR(s.tl.View()), "how is it going") {
		t.Fatalf("and the message should be on it:\n%s", stripSGR(s.tl.View()))
	}
}
