package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func fastMock() *agent.Mock {
	m := agent.NewMock()
	m.StepDelay = time.Millisecond
	return m
}

func kmsg(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func testApp(t *testing.T) *App {
	t.Helper()
	mk := fastMock()
	reg := &agent.Registry{Agents: []agent.Agent{mk}}
	a := NewApp(reg, mk, t.TempDir())
	a.resize(140, 40)
	return a
}

// pump executes cmd and feeds every resulting message back into the model,
// exactly like the bubbletea runtime, until the queue drains.
func pump(t *testing.T, a *App, cmd tea.Cmd) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 800 {
			t.Fatal("command loop did not settle")
		}
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		if msg == nil {
			continue
		}
		if _, ok := msg.(spinner.TickMsg); ok {
			continue // the spinner re-ticks forever; not under test
		}
		_, next := a.Update(msg)
		queue = append(queue, next)
	}
}

func TestComputeLayoutDropsPanesAsTerminalNarrows(t *testing.T) {
	tiny := ComputeLayout(64, 24, true, true)
	if tiny.ShowSidebar || tiny.ShowDiff {
		t.Fatalf("64 cols must show neither optional pane: %+v", tiny)
	}
	if tiny.TimelineW != 64 || tiny.PaneH != 20 {
		t.Fatalf("layout = %+v, want timeline 64 / paneH 20", tiny)
	}
	narrow := ComputeLayout(80, 24, true, true)
	if !narrow.ShowSidebar || narrow.ShowDiff {
		t.Fatalf("80 cols: sidebar available, diff not; got %+v", narrow)
	}
	if narrow.SidebarW+narrow.TimelineW != 80 {
		t.Fatalf("widths must fill the terminal: %+v", narrow)
	}
	mid := ComputeLayout(100, 30, true, true)
	if !mid.ShowSidebar || mid.ShowDiff {
		t.Fatalf("100 cols: sidebar yes, diff no; got %+v", mid)
	}
	if mid.SidebarW+mid.TimelineW != 100 {
		t.Fatalf("widths must fill the terminal: %+v", mid)
	}
	wide := ComputeLayout(140, 40, true, true)
	if !wide.ShowSidebar || !wide.ShowDiff {
		t.Fatalf("140 cols must show both: %+v", wide)
	}
	if wide.SidebarW+wide.TimelineW+wide.DiffW != 140 {
		t.Fatalf("widths must fill the terminal: %+v", wide)
	}
}

func TestComputeLayoutRespectsToggles(t *testing.T) {
	noSide := ComputeLayout(140, 40, false, true)
	if noSide.ShowSidebar || noSide.SidebarW != 0 {
		t.Fatalf("sidebar toggled off: %+v", noSide)
	}
	if noSide.TimelineW+noSide.DiffW != 140 {
		t.Fatalf("widths must still fill: %+v", noSide)
	}
	neither := ComputeLayout(140, 40, false, false)
	if neither.TimelineW != 140 {
		t.Fatalf("timeline should take the whole width: %+v", neither)
	}
}

func TestViewFillsExactlyTheTerminalHeight(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {100, 30}, {140, 40}, {200, 60}} {
		a := testApp(t)
		a.resize(size[0], size[1])
		if got := len(strings.Split(a.View(), "\n")); got != size[1] {
			t.Fatalf("at %dx%d View has %d lines, want %d", size[0], size[1], got, size[1])
		}
	}
}

func TestViewWithPickerOpenKeepsHeight(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {140, 40}} {
		a := testApp(t)
		a.resize(size[0], size[1])
		a.Update(kmsg(tea.KeyCtrlN))
		if a.picker == nil {
			t.Fatal("ctrl+n must open the picker")
		}
		if got := len(strings.Split(a.View(), "\n")); got != size[1] {
			t.Fatalf("picker open at %dx%d: %d lines, want %d", size[0], size[1], got, size[1])
		}
	}
}

func TestFullTurnFlowsThroughTheActiveSession(t *testing.T) {
	a := testApp(t)
	a.in.ta.SetValue("make a file")
	_, cmd := a.Update(kmsg(tea.KeyEnter))
	s := a.cur()
	if !s.busy {
		t.Fatal("sending must mark the session busy")
	}
	if a.in.Value() != "" {
		t.Fatal("input must clear on send")
	}
	pump(t, a, cmd)
	if s.busy {
		t.Fatal("TurnEnd must clear busy")
	}
	c := s.tl.Content()
	for _, want := range []string{"make a file", "hello.txt", "Bash"} {
		if !strings.Contains(c, want) {
			t.Fatalf("timeline missing %q:\n%s", want, c)
		}
	}
	if s.agentSID != "mock-session" {
		t.Fatalf("session id not stored: %q", s.agentSID)
	}
}

func TestTwoAgentsRunConcurrentlyAndKeepSeparateTimelines(t *testing.T) {
	a := testApp(t)
	second := a.addSession(fastMock(), t.TempDir())
	first := a.sessions[0]

	a.selectSession(0)
	a.in.ta.SetValue("first task")
	_, c1 := a.Update(kmsg(tea.KeyEnter))
	a.selectSession(1)
	a.in.ta.SetValue("second task")
	_, c2 := a.Update(kmsg(tea.KeyEnter))

	if !first.busy || !second.busy {
		t.Fatalf("both agents should be running: first=%v second=%v", first.busy, second.busy)
	}
	pump(t, a, tea.Batch(c1, c2))

	if first.busy || second.busy {
		t.Fatal("both turns should have finished")
	}
	if !strings.Contains(first.tl.Content(), "first task") ||
		strings.Contains(first.tl.Content(), "second task") {
		t.Fatal("session 1 timeline must contain only its own prompt")
	}
	if !strings.Contains(second.tl.Content(), "second task") ||
		strings.Contains(second.tl.Content(), "first task") {
		t.Fatal("session 2 timeline must contain only its own prompt")
	}
}

func TestBusyAgentRefusesASecondTurnButOthersStayFree(t *testing.T) {
	a := testApp(t)
	a.in.ta.SetValue("first")
	a.Update(kmsg(tea.KeyEnter))
	seq := a.cur().streamSeq
	a.in.ta.SetValue("second")
	a.Update(kmsg(tea.KeyEnter))
	if a.cur().streamSeq != seq {
		t.Fatal("a second turn must not start on a busy agent")
	}
	if a.note == "" {
		t.Fatal("the user must be told why nothing happened")
	}
}

func TestSecondTurnResumesTheStoredSession(t *testing.T) {
	a := testApp(t)
	a.cur().agentSID = "mock-session"
	a.in.ta.SetValue("again")
	a.Update(kmsg(tea.KeyEnter))
	if a.cur().lastOpts.SessionID != "mock-session" {
		t.Fatalf("resume not requested: %+v", a.cur().lastOpts)
	}
}

func TestEachSessionResumesItsOwnBackendSession(t *testing.T) {
	a := testApp(t)
	a.addSession(fastMock(), t.TempDir())
	a.sessions[0].agentSID = "sid-one"
	a.sessions[1].agentSID = "sid-two"
	a.selectSession(1)
	a.in.ta.SetValue("go")
	a.Update(kmsg(tea.KeyEnter))
	if got := a.sessions[1].lastOpts.SessionID; got != "sid-two" {
		t.Fatalf("wrong session resumed: %q", got)
	}
	if a.sessions[1].lastOpts.Dir != a.sessions[1].Dir {
		t.Fatal("each agent must run in its own directory")
	}
}

func TestEscapeCancelsOnlyTheFocusedAgent(t *testing.T) {
	a := testApp(t)
	second := a.addSession(fastMock(), t.TempDir())
	first := a.sessions[0]

	a.selectSession(0)
	a.in.ta.SetValue("long one")
	_, c1 := a.Update(kmsg(tea.KeyEnter))
	a.selectSession(1)
	a.in.ta.SetValue("long two")
	_, c2 := a.Update(kmsg(tea.KeyEnter))

	a.selectSession(0)
	a.Update(kmsg(tea.KeyEsc))
	if !strings.Contains(first.tl.Content(), "canceling") {
		t.Fatal("cancel must be visible in the focused agent's timeline")
	}
	if strings.Contains(second.tl.Content(), "canceling") {
		t.Fatal("esc must not touch the other agent")
	}
	pump(t, a, tea.Batch(c1, c2))
	if first.busy || second.busy {
		t.Fatal("both should have settled")
	}
}

func TestStaleStreamEventsAreDropped(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	before := s.tl.Len()
	a.Update(agentEventMsg{sess: s.ID, seq: s.streamSeq + 99, ev: agent.Event{Kind: agent.KindText, Text: "ghost"}})
	a.Update(agentEventMsg{sess: 4242, seq: 1, ev: agent.Event{Kind: agent.KindText, Text: "ghost"}})
	if s.tl.Len() != before {
		t.Fatal("events from a superseded stream or dead session must be ignored")
	}
}

func TestTabCyclesSessionsAndAltJumps(t *testing.T) {
	a := testApp(t)
	a.addSession(fastMock(), t.TempDir())
	a.addSession(fastMock(), t.TempDir())
	a.selectSession(0)

	a.Update(kmsg(tea.KeyTab))
	if a.active != 1 {
		t.Fatalf("tab should move to session 2, got %d", a.active)
	}
	a.Update(kmsg(tea.KeyShiftTab))
	if a.active != 0 {
		t.Fatalf("shift+tab should move back, got %d", a.active)
	}
	a.Update(kmsg(tea.KeyTab))
	a.Update(kmsg(tea.KeyTab))
	a.Update(kmsg(tea.KeyTab))
	if a.active != 0 {
		t.Fatalf("tab should wrap around, got %d", a.active)
	}
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}, Alt: true})
	if a.active != 2 {
		t.Fatalf("alt+3 should jump to session 3, got %d", a.active)
	}
}

func TestCloseSessionRemovesItAndQuitsOnTheLastOne(t *testing.T) {
	a := testApp(t)
	a.addSession(fastMock(), t.TempDir())
	if _, cmd := a.Update(kmsg(tea.KeyCtrlW)); cmd != nil {
		t.Fatal("closing one of two agents must not quit")
	}
	if len(a.sessions) != 1 {
		t.Fatalf("want 1 session left, got %d", len(a.sessions))
	}
	_, cmd := a.Update(kmsg(tea.KeyCtrlW))
	if cmd == nil {
		t.Fatal("closing the last agent must quit")
	}
	if len(a.sessions) != 0 {
		t.Fatalf("want 0 sessions, got %d", len(a.sessions))
	}
	if got := len(strings.Split(a.View(), "\n")); got != a.h {
		t.Fatalf("View with no sessions must still fill the height: %d != %d", got, a.h)
	}
}

func TestPickerCreatesASessionInTheChosenDirectory(t *testing.T) {
	a := testApp(t)
	target := t.TempDir()
	a.Update(kmsg(tea.KeyCtrlN))
	if a.picker == nil {
		t.Fatal("ctrl+n must open the picker")
	}
	a.picker.dir = target // stand in for the user browsing there
	a.Update(kmsg(tea.KeyEnter))
	if a.picker == nil || a.picker.Stage() != stageDir {
		t.Fatal("choosing a backend must advance to the directory step")
	}
	a.picker.loadDir(target)
	a.Update(kmsg(tea.KeyEnter)) // the first row is "use this directory"

	if a.picker != nil {
		t.Fatal("picker should close once a directory is chosen")
	}
	if len(a.sessions) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(a.sessions))
	}
	got := a.sessions[1]
	if got.Dir != target {
		t.Fatalf("new agent dir = %q, want %q", got.Dir, target)
	}
	if a.active != 1 {
		t.Fatal("a new agent should become the focused one")
	}
}

func TestPickerEscapeClosesWithoutCreating(t *testing.T) {
	a := testApp(t)
	a.Update(kmsg(tea.KeyCtrlN))
	a.Update(kmsg(tea.KeyEsc))
	if a.picker != nil {
		t.Fatal("esc on the backend step must close the picker")
	}
	if len(a.sessions) != 1 {
		t.Fatalf("no session should have been created, got %d", len(a.sessions))
	}
}

func TestKeysGoToThePickerWhileItIsOpen(t *testing.T) {
	a := testApp(t)
	a.Update(kmsg(tea.KeyCtrlN))
	before := len(a.sessions)
	a.Update(kmsg(tea.KeyCtrlW)) // must not close a session behind the modal
	if len(a.sessions) != before {
		t.Fatal("the modal must swallow app keybindings")
	}
}

func TestSidebarTogglesAndStatusCountsRunningAgents(t *testing.T) {
	a := testApp(t)
	if !a.lay.ShowSidebar {
		t.Fatal("sidebar should start visible at 140 cols")
	}
	a.Update(kmsg(tea.KeyCtrlB))
	if a.lay.ShowSidebar {
		t.Fatal("ctrl+b must hide the sidebar")
	}
	if got := len(strings.Split(a.View(), "\n")); got != a.h {
		t.Fatalf("height after toggle = %d, want %d", got, a.h)
	}
	a.Update(kmsg(tea.KeyCtrlB))

	a.addSession(fastMock(), t.TempDir())
	a.selectSession(0)
	a.in.ta.SetValue("work")
	a.Update(kmsg(tea.KeyEnter))
	if !strings.Contains(a.statusLine(), "1 running") {
		t.Fatalf("status should report running agents: %q", a.statusLine())
	}
	if !strings.Contains(a.statusLine(), "[1/2]") {
		t.Fatalf("status should show which agent is focused: %q", a.statusLine())
	}
}
