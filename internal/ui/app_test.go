package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func testApp(t *testing.T) *App {
	t.Helper()
	mk := agent.NewMock()
	mk.StepDelay = time.Millisecond
	reg := &agent.Registry{Agents: []agent.Agent{mk}}
	a := NewApp(reg, mk, t.TempDir())
	a.resize(120, 40)
	return a
}

// pump executes cmd and feeds every resulting message back into the model,
// exactly like the bubbletea runtime, until the queue drains.
func pump(t *testing.T, a *App, cmd tea.Cmd) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 500 {
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

func TestComputeLayoutHidesDiffOnNarrowTerminals(t *testing.T) {
	l := ComputeLayout(80, 24, true)
	if l.ShowDiff {
		t.Fatal("80 cols must not show the diff pane")
	}
	if l.TimelineW != 80 || l.PaneH != 20 {
		t.Fatalf("layout = %+v, want timeline 80 / paneH 20", l)
	}
}

func TestComputeLayoutSplitsWideTerminals(t *testing.T) {
	l := ComputeLayout(120, 40, true)
	if !l.ShowDiff || l.TimelineW+l.DiffW != 120 {
		t.Fatalf("layout = %+v", l)
	}
	if l.DiffW < 34 || l.DiffW > 70 {
		t.Fatalf("diff width out of bounds: %+v", l)
	}
	if off := ComputeLayout(120, 40, false); off.ShowDiff || off.TimelineW != 120 {
		t.Fatalf("toggled-off layout = %+v", off)
	}
}

func TestViewFillsExactlyTheTerminalHeight(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}, {100, 30}} {
		a := testApp(t)
		a.resize(size[0], size[1])
		got := len(strings.Split(a.View(), "\n"))
		if got != size[1] {
			t.Fatalf("at %dx%d View has %d lines, want %d", size[0], size[1], got, size[1])
		}
	}
}

func TestFullTurnFlowsThroughTheTimeline(t *testing.T) {
	a := testApp(t)
	a.in.ta.SetValue("make a file")
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !a.busy {
		t.Fatal("sending must mark the app busy")
	}
	if a.in.Value() != "" {
		t.Fatal("input must clear on send")
	}
	pump(t, a, cmd)
	if a.busy {
		t.Fatal("TurnEnd must clear busy")
	}
	c := a.tl.Content()
	for _, want := range []string{"make a file", "hello.txt", "Bash"} {
		if !strings.Contains(c, want) {
			t.Fatalf("timeline missing %q:\n%s", want, c)
		}
	}
	if a.sessions["mock"] != "mock-session" {
		t.Fatalf("session id not stored: %+v", a.sessions)
	}
}

func TestSecondTurnResumesTheStoredSession(t *testing.T) {
	a := testApp(t)
	a.sessions["mock"] = "mock-session"
	a.in.ta.SetValue("again")
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if a.lastOpts.SessionID != "mock-session" {
		t.Fatalf("resume not requested: %+v", a.lastOpts)
	}
}

func TestEnterWhileBusyDoesNotStartASecondTurn(t *testing.T) {
	a := testApp(t)
	a.in.ta.SetValue("first")
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	id := a.streamID
	a.in.ta.SetValue("second")
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if a.streamID != id {
		t.Fatal("a second turn must not start while busy")
	}
	if a.note == "" {
		t.Fatal("the user must be told why nothing happened")
	}
}

func TestEscapeCancelsTheTurn(t *testing.T) {
	a := testApp(t)
	a.in.ta.SetValue("long task")
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	pump(t, a, cmd)
	if a.busy {
		t.Fatal("cancel must end the turn")
	}
	if !strings.Contains(a.tl.Content(), "canceling") {
		t.Fatalf("cancel must be visible in the timeline:\n%s", a.tl.Content())
	}
}

func TestStaleStreamEventsAreDropped(t *testing.T) {
	a := testApp(t)
	before := a.tl.Len()
	a.Update(agentEventMsg{id: a.streamID + 99, ev: agent.Event{Kind: agent.KindText, Text: "ghost"}})
	if a.tl.Len() != before {
		t.Fatal("events from a superseded stream must be ignored")
	}
}

func TestTabSwitchesAgentOnlyWhenIdle(t *testing.T) {
	mk := agent.NewMock()
	mk.StepDelay = time.Millisecond
	other := agent.NewMock() // second instance stands in for a second backend
	reg := &agent.Registry{Agents: []agent.Agent{mk, other}}
	a := NewApp(reg, mk, t.TempDir())
	a.resize(120, 40)
	a.busy = true
	a.Update(tea.KeyMsg{Type: tea.KeyTab})
	if a.cur != agent.Agent(mk) {
		t.Fatal("must not switch mid-turn")
	}
	a.busy = false
	a.Update(tea.KeyMsg{Type: tea.KeyTab})
	if a.cur != agent.Agent(other) {
		t.Fatal("tab must switch agents when idle")
	}
}

func TestDiffPaneTogglesWithCtrlT(t *testing.T) {
	a := testApp(t)
	if !a.lay.ShowDiff {
		t.Fatal("diff should start visible at 120 cols")
	}
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if a.lay.ShowDiff {
		t.Fatal("ctrl+t must hide the diff pane")
	}
	if got := len(strings.Split(a.View(), "\n")); got != 40 {
		t.Fatalf("View height after toggle = %d, want 40", got)
	}
}
