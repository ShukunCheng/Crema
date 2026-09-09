package ui

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

// ctlConv is a held-open conversation with a control channel, recording what
// it is asked. Replies come from the test, the way the CLI's would.
type ctlConv struct {
	*fakeConv
	mu         sync.Mutex
	n          int
	interrupts int
	models     []string
	modes      []agent.PermissionMode
	stops      []string
	refuse     bool // pretend the CLI cannot take the request at all
}

func (c *ctlConv) next() (string, error) {
	c.n++
	if c.refuse {
		return "", context.Canceled
	}
	return "ctl-" + itoa(c.n), nil
}
func (c *ctlConv) Interrupt() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.interrupts++
	return c.next()
}
func (c *ctlConv) SetModel(m string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.models = append(c.models, m)
	return c.next()
}
func (c *ctlConv) SetPermission(p agent.PermissionMode) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.modes = append(c.modes, p)
	return c.next()
}
func (c *ctlConv) StopTask(id string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stops = append(c.stops, id)
	return c.next()
}

// ctlMock is a backend whose conversations take control requests.
type ctlMock struct {
	*agent.Mock
	mu    sync.Mutex
	opens int
	last  *ctlConv
}

func (m *ctlMock) Open(_ context.Context, _ agent.RunOptions) (agent.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.opens++
	m.last = &ctlConv{fakeConv: &fakeConv{events: make(chan agent.Event, 64)}}
	return m.last, nil
}

// walkTo takes the highlight from the input box to the first chip of a kind,
// pressing ↓ the way a person would. It walks rather than counting presses so
// that adding a value to the hub does not break every test that goes past it.
func walkTo(t *testing.T, a *App, kind controlKind) *Controls {
	t.Helper()
	c := openControls(t, a)
	for i := 0; i < 12 && c.Kind() != kind; i++ {
		a.Update(kmsg(tea.KeyDown))
	}
	if c.Kind() != kind {
		t.Fatalf("the walk never reached kind %v, stopped on %v", kind, c.Kind())
	}
	return c
}

// walkToSubagent is the common case: the hub's own values first, then the
// roster — the agent's own row, then each subagent.
func walkToSubagent(t *testing.T, a *App) {
	t.Helper()
	walkTo(t, a, controlAgent)
}

// ctlApp is an app whose focused agent has a live, controllable process, with
// one subagent running.
func ctlApp(t *testing.T) (*App, *Session, *ctlMock) {
	t.Helper()
	a := testApp(t)
	a.resize(120, 26)
	m := &ctlMock{Mock: agent.NewMock()}
	s := a.cur()
	s.Backend = m
	send(t, a, "start") // opens the process
	result(t, a, s)     // and the turn ends, the process staying
	s.noteTask(&agent.TaskUpdate{ID: "aae4", Status: "running", Type: "general-purpose", Desc: "audit the triggers"})
	a.resize(a.w, a.h)
	return a, s, m
}

// Enter on a subagent's row addresses the next message to it. Crema cannot
// reach a subagent's stdin, so the message goes to the agent with the ask to
// relay it by SendMessage — the CLI's own way of talking to one — and the
// transcript shows who it was for.
func TestEnterOnASubagentAddressesTheNextMessage(t *testing.T) {
	a, s, m := ctlApp(t)
	usual := a.in.ta.Placeholder

	walkToSubagent(t, a)
	a.Update(kmsg(tea.KeyEnter))
	if a.controls != nil {
		t.Fatal("talking closes the walk")
	}
	if a.recipient == nil || a.recipient.ID != "aae4" {
		t.Fatalf("recipient = %+v", a.recipient)
	}
	if !strings.Contains(a.in.ta.Placeholder, "to general-purpose (aae4)") {
		t.Fatalf("the box should say who it is talking to: %q", a.in.ta.Placeholder)
	}
	if !strings.Contains(stripSGR(a.statusLine()), "next message → general-purpose") {
		t.Fatal("and so should the hub")
	}

	send(t, a, "check the tests too")
	sent := m.last.sends()
	got := sent[len(sent)-1]
	for _, want := range []string{"SendMessage", "aae4", "general-purpose", "check the tests too"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the relay should carry %q:\n%s", want, got)
		}
	}
	if !strings.Contains(stripSGR(s.tl.Content()), "→ general-purpose: check the tests too") {
		t.Fatal("the transcript should show who the message was for")
	}
	if a.recipient != nil || a.in.ta.Placeholder != usual {
		t.Fatal("one message, then back to talking to the agent")
	}
	s.close()
}

// esc drops the recipient without sending anything.
func TestEscDropsTheRecipient(t *testing.T) {
	a, s, m := ctlApp(t)
	openControls(t, a)
	a.Update(kmsg(tea.KeyDown))
	a.Update(kmsg(tea.KeyEnter))
	before := len(m.last.sends())
	press(t, a, kmsg(tea.KeyEsc))
	if a.recipient != nil {
		t.Fatal("esc should drop the recipient")
	}
	if len(m.last.sends()) != before {
		t.Fatal("and send nothing")
	}
	s.close()
}

// x on a subagent's row stops it, through the CLI's own stop_task.
func TestXStopsASubagent(t *testing.T) {
	a, s, m := ctlApp(t)
	walkToSubagent(t, a)
	press(t, a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if got := m.last.stops; len(got) != 1 || got[0] != "aae4" {
		t.Fatalf("stops = %q", got)
	}
	if !strings.Contains(a.note, "stopping general-purpose") {
		t.Fatalf("note = %q", a.note)
	}
	// The CLI answers ok and, measured live, says nothing more about the
	// task — so the ok is what marks it stopped.
	a.Update(agentEventMsg{sess: s.ID, seq: s.streamSeq,
		ev: agent.Event{Kind: agent.KindControl, Control: &agent.ControlReply{RequestID: "ctl-" + itoa(m.last.n), OK: true}}})
	if s.RunningTasks() != 0 || s.tasks[0].Status != "stopped" {
		t.Fatalf("after the ok: %+v", s.tasks)
	}
	s.close()
}

// The CLI's list of background work is the truth about it: a backgrounded
// task — shell or subagent — that drops off it has ended. A foreground
// subagent is never on the list, so its absence says nothing.
func TestTheBackgroundListEndsWorkItDropped(t *testing.T) {
	a, s, _ := ctlApp(t) // its subagent aae4 is a foreground one
	s.noteTask(&agent.TaskUpdate{ID: "b1", Status: "running", Type: "local_bash", Desc: "ping", Backgrounded: true})
	s.noteTask(&agent.TaskUpdate{ID: "b2", Status: "running", Type: "local_agent", Desc: "build", Backgrounded: true})
	a.Update(agentEventMsg{sess: s.ID, seq: s.streamSeq, ev: agent.Event{Kind: agent.KindTaskList,
		Tasks: []agent.TaskUpdate{{ID: "b2", Type: "local_agent", Desc: "build", Status: "running", Backgrounded: true}}}})
	var b1, b2, sub string
	for _, t := range s.tasks {
		switch t.ID {
		case "b1":
			b1 = t.Status
		case "b2":
			b2 = t.Status
		case "aae4":
			sub = t.Status
		}
	}
	if b1 != "finished" || b2 != "running" || sub != "running" {
		t.Fatalf("b1=%q b2=%q subagent=%q", b1, b2, sub)
	}
	s.close()
}

// x in the background list — the too-many-to-list case — stops the
// highlighted task the same way.
func TestXInTheBackgroundListStops(t *testing.T) {
	a, s, m := ctlApp(t)
	for i := 0; i < maxRosterRows; i++ {
		s.noteTask(&agent.TaskUpdate{ID: "b" + itoa(i), Status: "running", Type: "local_bash", Desc: "ping"})
	}
	a.resize(a.w, a.h)
	walkTo(t, a, controlBackground)
	a.Update(kmsg(tea.KeyEnter)) // open the list
	a.Update(kmsg(tea.KeyDown))  // second row
	press(t, a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if got := m.last.stops; len(got) != 1 || got[0] != "b0" {
		t.Fatalf("stops = %q", got)
	}
	s.close()
}

// Cancelling a turn interrupts it over stdin and keeps the process — and its
// cache — rather than killing it and paying a cold start on the next message.
func TestCancelInterruptsInsteadOfClosing(t *testing.T) {
	a, s, m := ctlApp(t)
	send(t, a, "a long one")
	if !s.cancelTurn() {
		t.Fatal("cancel should take")
	}
	if m.last.interrupts != 1 || s.conv == nil {
		t.Fatalf("interrupts=%d conv=%v — the process should live", m.last.interrupts, s.conv != nil)
	}
	// The CLI ends the turn however it ends it; that is not a failure.
	a.Update(agentEventMsg{sess: s.ID, seq: s.streamSeq,
		ev: agent.Event{Kind: agent.KindTurnEnd, Result: &agent.TurnResult{Err: "claude ended the turn with interrupted"}}})
	if s.busy {
		t.Fatal("the turn is over")
	}
	if strings.Contains(stripSGR(s.tl.Content()), "interrupted") {
		t.Fatal("an interrupt asked for is not an error worth a red block")
	}
	s.close()
}

// Model and permission changes go down the live process's stdin and keep it,
// where they used to restart it.
func TestModelAndModeSwitchLiveOnAHeldProcess(t *testing.T) {
	a, s, m := ctlApp(t)
	s.SetModel("demo-fast")
	s.SetPermission(agent.PermissionPlan)
	if s.conv == nil {
		t.Fatal("the process should have been kept")
	}
	if got := m.last.models; len(got) != 1 || got[0] != "demo-fast" {
		t.Fatalf("models = %q", got)
	}
	if got := m.last.modes; len(got) != 1 || got[0] != agent.PermissionPlan {
		t.Fatalf("modes = %v", got)
	}
	send(t, a, "next")
	if m.opens != 1 {
		t.Fatalf("opens = %d, want the same process still", m.opens)
	}
	s.close()
}

// A switch the CLI refuses falls back to what crema did before: the process
// goes, and the next message starts one with the setting on its command line.
func TestARefusedSwitchFallsBackToARestart(t *testing.T) {
	a, s, m := ctlApp(t)
	s.SetModel("demo-fast")
	id := "ctl-" + itoa(m.last.n)
	a.Update(agentEventMsg{sess: s.ID, seq: s.streamSeq,
		ev: agent.Event{Kind: agent.KindControl, Control: &agent.ControlReply{RequestID: id, OK: false, Err: "no such model"}}})
	if s.conv != nil {
		t.Fatal("a refusal should let the process go")
	}
	if !strings.Contains(a.note, "fresh process") {
		t.Fatalf("and say what that costs: %q", a.note)
	}
	send(t, a, "next")
	if m.opens != 2 {
		t.Fatalf("opens = %d, want a new process with the flag", m.opens)
	}
	s.close()
}

// A control reply is crema being answered, not the turn speaking: it must
// not make a silent turn look like it said something.
func TestAControlReplyIsNotTurnOutput(t *testing.T) {
	a, s, _ := ctlApp(t)
	send(t, a, "quiet")
	s.noteActivity(agent.Event{Kind: agent.KindControl, Control: &agent.ControlReply{OK: true}})
	if s.turnEvents != 0 {
		t.Fatalf("turnEvents = %d", s.turnEvents)
	}
	s.close()
}
