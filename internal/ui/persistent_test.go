package ui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

// fakeConv is a conversation the test drives by hand.
type fakeConv struct {
	events chan agent.Event
	mu     sync.Mutex
	sent   []string
	closed bool
}

func (c *fakeConv) Send(p string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("closed")
	}
	c.sent = append(c.sent, p)
	return nil
}
func (c *fakeConv) Events() <-chan agent.Event { return c.events }
func (c *fakeConv) SessionID() string          { return "fake-session" }
func (c *fakeConv) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed, _ = true, 0
		close(c.events)
	}
	return nil
}
func (c *fakeConv) sends() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.sent...)
}

// streamMock is a backend that holds its conversation open.
type streamMock struct {
	*agent.Mock
	mu    sync.Mutex
	opens int
	last  *fakeConv
	opts  []agent.RunOptions
}

func (m *streamMock) Open(_ context.Context, o agent.RunOptions) (agent.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.opens++
	m.opts = append(m.opts, o)
	m.last = &fakeConv{events: make(chan agent.Event, 64)}
	return m.last, nil
}
func (m *streamMock) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.opens
}

func streamApp(t *testing.T) (*App, *Session, *streamMock) {
	t.Helper()
	a := testApp(t)
	m := &streamMock{Mock: agent.NewMock()}
	s := a.cur()
	s.Backend = m
	return a, s, m
}

// result delivers a turn's closing line the way the CLI does.
func result(t *testing.T, a *App, s *Session) {
	t.Helper()
	a.Update(agentEventMsg{sess: s.ID, seq: s.streamSeq,
		ev: agent.Event{Kind: agent.KindTurnEnd, Result: &agent.TurnResult{SessionID: "fake-session"}}})
}

// The whole point: two turns, one process. The context is put down once
// instead of once per message.
func TestTwoTurnsShareOneProcess(t *testing.T) {
	a, s, m := streamApp(t)
	send(t, a, "first") // the listener blocks a synchronous pump; events are delivered by hand
	if m.count() != 1 || !s.busy {
		t.Fatalf("opens=%d busy=%v", m.count(), s.busy)
	}
	result(t, a, s)
	if s.busy {
		t.Fatal("the result line ends the turn when the process stays put")
	}
	send(t, a, "second") // the listener blocks a synchronous pump; events are delivered by hand
	if m.count() != 1 {
		t.Fatalf("the second turn opened another process: %d", m.count())
	}
	if got := m.last.sends(); len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("sends = %q", got)
	}
	s.close()
}

// A message typed during a turn still goes on its own when the turn ends.
func TestAQueuedMessageGoesDownTheSameProcess(t *testing.T) {
	a, s, m := streamApp(t)
	send(t, a, "first") // the listener blocks a synchronous pump; events are delivered by hand
	typeRunes(t, a, "queued one")
	press(t, a, kmsg(tea.KeyEnter))
	if len(s.Queued()) != 1 {
		t.Fatalf("queued = %q", s.Queued())
	}
	result(t, a, s)
	if len(s.Queued()) != 0 || !s.busy {
		t.Fatalf("the queue should have gone: %q busy=%v", s.Queued(), s.busy)
	}
	if m.count() != 1 {
		t.Fatalf("and down the same process: opens=%d", m.count())
	}
	if got := m.last.sends(); len(got) != 2 || got[1] != "queued one" {
		t.Fatalf("sends = %q", got)
	}
	s.close()
}

// Model and permissions are command-line flags, so changing one needs a new
// process — the conversation itself survives, resumed by id.
func TestSettingsThatLiveOnTheCommandLineReopen(t *testing.T) {
	for _, c := range []struct {
		name   string
		change func(*Session)
	}{
		{"model", func(s *Session) { s.SetModel("demo-fast") }},
		{"permissions", func(s *Session) { s.SetPermission(agent.PermissionPlan) }},
		{"clear", func(s *Session) { s.reset() }},
	} {
		a, s, m := streamApp(t)
		send(t, a, "first") // the listener blocks a synchronous pump; events are delivered by hand
		result(t, a, s)
		c.change(s)
		if s.conv != nil {
			t.Fatalf("%s: the process should have been let go", c.name)
		}
		send(t, a, "second") // the listener blocks a synchronous pump; events are delivered by hand
		if m.count() != 2 {
			t.Fatalf("%s: opens = %d, want a fresh process", c.name, m.count())
		}
		s.close()
	}
}

// The next process resumes the conversation the CLI kept.
func TestAReopenedProcessResumesTheConversation(t *testing.T) {
	a, s, m := streamApp(t)
	send(t, a, "first") // the listener blocks a synchronous pump; events are delivered by hand
	a.Update(agentEventMsg{sess: s.ID, seq: s.streamSeq,
		ev: agent.Event{Kind: agent.KindTurnEnd, Result: &agent.TurnResult{SessionID: "kept-id"}}})
	s.SetModel("demo-fast")
	send(t, a, "second") // the listener blocks a synchronous pump; events are delivered by hand
	if got := m.opts[1].SessionID; got != "kept-id" {
		t.Fatalf("SessionID = %q, want the conversation resumed", got)
	}
	s.close()
}

// Cancelling closes the process — the CLI has no stdin word for "stop" — and
// the turn ends without crema claiming anything went wrong.
func TestCancellingClosesTheProcessQuietly(t *testing.T) {
	a, s, _ := streamApp(t)
	send(t, a, "long one") // the listener blocks a synchronous pump; events are delivered by hand
	if !s.cancelTurn() {
		t.Fatal("cancel should have taken")
	}
	if s.conv != nil {
		t.Fatal("the process should be gone")
	}
	a.Update(streamClosedMsg{sess: s.ID, seq: s.streamSeq})
	if s.busy {
		t.Fatal("and the turn over")
	}
	if got := stripSGR(s.tl.Content()); strings.Contains(got, "without finishing") {
		t.Fatalf("a cancel is not a failure:\n%s", got)
	}
}

// A process that dies mid-turn says so, and the next message starts another.
func TestADeadProcessIsReportedAndReplaced(t *testing.T) {
	a, s, m := streamApp(t)
	send(t, a, "first") // the listener blocks a synchronous pump; events are delivered by hand
	m.last.Close()      // the process exits under the turn
	a.Update(streamClosedMsg{sess: s.ID, seq: s.streamSeq})
	if s.busy {
		t.Fatal("the turn is over either way")
	}
	if !strings.Contains(stripSGR(s.tl.Content()), "without finishing") {
		t.Fatal("and crema should say the turn never finished")
	}
	send(t, a, "second") // the listener blocks a synchronous pump; events are delivered by hand
	if m.count() != 2 {
		t.Fatalf("opens = %d, want a new process", m.count())
	}
	s.close()
}

// A process idle past the cache lifetime it exists to keep warm is let go.
func TestAnIdleProcessIsLetGo(t *testing.T) {
	a, s, _ := streamApp(t)
	send(t, a, "first") // the listener blocks a synchronous pump; events are delivered by hand
	result(t, a, s)
	s.maybeCloseIdleConv()
	if s.conv == nil {
		t.Fatal("a fresh conversation is worth holding")
	}
	s.convAt = time.Now().Add(-convIdleLife - time.Minute)
	s.maybeCloseIdleConv()
	if s.conv != nil {
		t.Fatal("past the cache's own lifetime it is only holding memory")
	}
}

// A backend without the mode keeps the process-per-turn lifecycle, where the
// exit is the end of the turn.
func TestABackendWithoutTheModeIsUnchanged(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	if s.persistent() {
		t.Fatal("the mock holds nothing open")
	}
	send(t, a, "hello") // the listener blocks a synchronous pump; events are delivered by hand
	a.Update(agentEventMsg{sess: s.ID, seq: s.streamSeq,
		ev: agent.Event{Kind: agent.KindTurnEnd, Result: &agent.TurnResult{}}})
	if !s.busy {
		t.Fatal("a result is only a leg there; the exit ends the turn")
	}
	a.Update(streamClosedMsg{sess: s.ID, seq: s.streamSeq})
	if s.busy {
		t.Fatal("and now it is over")
	}
}
