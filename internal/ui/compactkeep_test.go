package ui

import (
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

// say streams a line of prose into a held-open conversation.
func say(t *testing.T, a *App, s *Session, text string) {
	t.Helper()
	a.Update(agentEventMsg{sess: s.ID, seq: s.streamSeq,
		ev: agent.Event{Kind: agent.KindText, Text: text}})
}

// The bug this guards against: /compact used to wipe the pane and show the
// summary in its place, so the answer you were reading vanished the moment
// you asked for the conversation to be folded up.
func TestCompactionKeepsTheAnswerOnScreen(t *testing.T) {
	a, s, m := streamApp(t)
	send(t, a, "first") // the listener blocks a synchronous pump; events are delivered by hand
	say(t, a, s, "THE ANSWER you were reading")
	result(t, a, s)

	send(t, a, "/compact")
	say(t, a, s, "THE SUMMARY the agent carries forward")
	result(t, a, s)
	if s.compacting || s.busy {
		t.Fatalf("the fold should be over: compacting=%v busy=%v", s.compacting, s.busy)
	}
	got := stripSGR(s.tl.Content())
	if !strings.Contains(got, "THE ANSWER") {
		t.Fatalf("the answer was wiped by the compaction:\n%s", got)
	}
	if !strings.Contains(got, "▸ compacted · the agent carries on") { // the rest may wrap
		t.Fatalf("one folded line should mark where the fold happened:\n%s", got)
	}
	if strings.Contains(got, "THE SUMMARY") {
		t.Fatalf("the summary arrives folded — the conversation above it is the readable version:\n%s", got)
	}

	// The agent's side really was folded: a fresh process, opened with the
	// summary in front of the next message.
	send(t, a, "next") // the listener blocks a synchronous pump; events are delivered by hand
	if m.count() != 2 {
		t.Fatalf("the next message should open a new process, opens=%d", m.count())
	}
	if p := m.last.sends()[0]; !strings.Contains(p, "THE SUMMARY") || !strings.HasSuffix(p, "next") {
		t.Fatalf("the summary should lead the next prompt: %q", p)
	}
	say(t, a, s, "NEXT OUTPUT")
	result(t, a, s)
	if got := stripSGR(s.tl.Content()); !strings.Contains(got, "NEXT OUTPUT") {
		t.Fatalf("the turn after a compaction should show its answer:\n%s", got)
	}
	s.close()
}

// The summary is one click away, and folds back to its one line.
func TestTheCompactionSummaryOpensOnClick(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	pump(t, a, send(t, a, "make a file"))
	pump(t, a, send(t, a, "/compact"))

	i := s.tl.Len() - 1
	if b := s.tl.Blocks()[i]; b.Kind != BlockSummary || !b.Collapsed {
		t.Fatalf("the fold should end in a folded summary block, got %+v", b)
	}
	summary := s.tl.Blocks()[i].Text
	s.tl.ToggleCollapse(i)
	got := stripSGR(s.tl.Content())
	if !strings.Contains(got, "▾ compacted · the agent carries on") || !strings.Contains(got, summary) {
		t.Fatalf("opening it should show the summary under its header:\n%s", got)
	}
	s.tl.ToggleCollapse(i)
	// The mock answers every turn alike, so the summary's text is also the
	// answer above it; the marker is what says whether it is folded.
	got = stripSGR(s.tl.Content())
	if strings.Contains(got, "▾ compacted") || !strings.Contains(got, "▸ compacted") {
		t.Fatalf("and closing it should fold it back:\n%s", got)
	}
}

// A summary never joins the tool run before it: "Ran 3 shell commands"
// cannot also mean the conversation was folded here.
func TestTheCompactionSummaryFoldsAlone(t *testing.T) {
	blocks := []Block{
		{Kind: BlockUser, Text: "/compact"},
		{Kind: BlockTool, Name: "Bash", Text: "ls", Collapsed: true},
		{Kind: BlockToolOutput, Text: "a", Collapsed: true},
		{Kind: BlockSummary, Text: "s", Collapsed: true},
	}
	if from, to := foldRun(blocks, 3); from != 3 || to != 4 {
		t.Fatalf("the summary's run = [%d,%d), want [3,4)", from, to)
	}
	if from, to := foldRun(blocks, 1); from != 1 || to != 3 {
		t.Fatalf("the tools' run = [%d,%d), want [1,3)", from, to)
	}
	if got := summarize(blocks[3:4]); got != compactedLabel {
		t.Fatalf("its line = %q", got)
	}
}

// A message typed while the conversation is being summarised is sent into the
// folded conversation, not lost with the old one. It was typed into this
// conversation, and the folded one is the same conversation, cheaper.
func TestAMessageQueuedIntoACompactionIsStillSent(t *testing.T) {
	a, s, m := streamApp(t)
	send(t, a, "first") // the listener blocks a synchronous pump; events are delivered by hand
	say(t, a, s, "THE ANSWER")
	result(t, a, s)

	send(t, a, "/compact")
	if !s.compacting {
		t.Fatal("/compact should start the summarising turn")
	}
	typeRunes(t, a, "queued one")
	press(t, a, kmsg(tea.KeyEnter))
	if len(s.Queued()) != 1 {
		t.Fatalf("a message typed during the fold should wait, queued = %q", s.Queued())
	}
	say(t, a, s, "THE SUMMARY")
	result(t, a, s)
	if s.compacting || len(s.Queued()) != 0 || !s.busy {
		t.Fatalf("after the fold the queued message should be on its way: compacting=%v queued=%q busy=%v",
			s.compacting, s.Queued(), s.busy)
	}
	if m.count() != 2 {
		t.Fatalf("it should have opened the folded conversation's process, opens=%d", m.count())
	}
	if p := m.last.sends()[0]; !strings.Contains(p, "THE SUMMARY") || !strings.HasSuffix(p, "queued one") {
		t.Fatalf("the summary should lead it: %q", p)
	}
	s.close()
}
