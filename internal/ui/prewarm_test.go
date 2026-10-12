package ui

import (
	"testing"
	"time"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

// A fresh process spends seconds booting before it will take a message — a
// median of 4.6 across twenty-one real launches — and none of that is in any
// transcript. Launched ahead of time, the boot is over before you send, and the
// message goes to the process that is already waiting rather than a new one.
func TestAWarmProcessTakesTheFirstMessage(t *testing.T) {
	a, s, m := streamApp(t)
	s.Model, s.Effort = "claude-opus-5", "xhigh"

	if cmd := s.Prewarm(); cmd == nil {
		t.Fatal("prewarming should arm a listener")
	}
	if m.count() != 1 || s.conv == nil {
		t.Fatalf("it should start the process now: opens=%d", m.count())
	}
	// Started with the agent's own settings, and nothing sent to it.
	if got := m.opts[0]; got.Model != "claude-opus-5" || got.Effort != "xhigh" || got.Prompt != "" {
		t.Fatalf("opened with %+v", got)
	}
	if n := len(m.last.sends()); n != 0 {
		t.Fatalf("a warm process must not be sent anything — that would be a turn; got %d", n)
	}

	send(t, a, "now do the thing")
	if m.count() != 1 {
		t.Fatalf("the message should go to the warm process, not a second one: opens=%d", m.count())
	}
	if got := m.last.sends(); len(got) != 1 {
		t.Fatalf("and it should get exactly the message: %q", got)
	}
	s.close()
}

// There is nothing to warm when something is already running, or already
// there, or when the backend is started a turn at a time.
func TestPrewarmLeavesWellAlone(t *testing.T) {
	a, s, m := streamApp(t)
	s.busy = true
	if s.Prewarm() != nil || m.count() != 0 {
		t.Fatal("a turn is running; its process is not to be touched")
	}
	s.busy = false
	s.Prewarm()
	s.Prewarm()
	if m.count() != 1 {
		t.Fatalf("a second warm should not start a second process: opens=%d", m.count())
	}
	s.close()

	plain := testApp(t) // the process-per-turn mock has no process to hold
	if plain.cur().Prewarm() != nil {
		t.Fatal("a backend without held processes has nothing to warm")
	}
	_ = a
}

// Only the agent you are looking at: a waiting process holds a couple of hundred
// megabytes, so warming every agent on start would cost more than it saves.
// Moving to another agent warms that one.
func TestOnlyTheFocusedAgentIsWarmed(t *testing.T) {
	a := testApp(t)
	first := &streamMock{Mock: agent.NewMock()}
	second := &streamMock{Mock: agent.NewMock()}
	a.cur().Backend = first
	other := a.addSession(second, t.TempDir())
	a.selectSession(0)
	a.warmSoon = false

	runCmd(a.Init())
	if first.count() != 1 || second.count() != 0 {
		t.Fatalf("startup should warm the focused agent alone: first=%d second=%d",
			first.count(), second.count())
	}
	a.Update(kmsg(tea.KeyCtrlDown)) // to the other agent
	if second.count() != 1 {
		t.Fatalf("moving to an agent should warm it: opens=%d", second.count())
	}
	a.cur().close()
	other.close()
}

// A warm process nobody uses is let go the same as any other, rather than
// holding its memory for the rest of the day.
func TestAnUnusedWarmProcessIsStillSwept(t *testing.T) {
	_, s, _ := streamApp(t)
	s.Prewarm()
	if s.convAt.IsZero() {
		t.Fatal("the idle clock has to be running, or the sweep never looks at it")
	}
	s.convAt = time.Now().Add(-2 * convIdleLife)
	s.maybeCloseIdleConv()
	if s.conv != nil {
		t.Fatal("an hour unused is an hour unused")
	}
}

// runCmd runs a command and any batch it holds, without pumping the listeners
// it arms — those block on a channel nothing will write to in a test.
func runCmd(c tea.Cmd) {
	if c == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- c() }()
	select {
	case msg := <-done:
		if b, ok := msg.(tea.BatchMsg); ok {
			for _, sub := range b {
				runCmdShallow(sub)
			}
		}
	case <-time.After(200 * time.Millisecond):
	}
}

func runCmdShallow(c tea.Cmd) {
	if c == nil {
		return
	}
	done := make(chan struct{})
	go func() { c(); close(done) }()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
	}
}
