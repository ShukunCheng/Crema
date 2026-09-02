package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

// A subagent's work folds into its own labeled line, prose included — it
// reported to the model, not to you — and one click opens all of it.
func TestSubagentWorkFoldsIntoItsOwnRun(t *testing.T) {
	tl := NewTimeline(90, 20)
	tl.AppendEvent(agent.Event{Kind: agent.KindToolCall, Tool: &agent.ToolCall{Name: "Agent", Input: "{}"}})
	tl.AppendEvent(agent.Event{Kind: agent.KindToolCall, SubID: "toolu_p", Tool: &agent.ToolCall{Name: "Bash", Input: "echo sub-hello"}})
	tl.AppendEvent(agent.Event{Kind: agent.KindToolOutput, SubID: "toolu_p", Output: &agent.ToolOutput{Content: "sub-hello"}})
	tl.AppendEvent(agent.Event{Kind: agent.KindText, SubID: "toolu_p", Text: "The output is sub-hello."})
	tl.AppendEvent(agent.Event{Kind: agent.KindText, Text: "DONE"})

	v := stripSGR(tl.Content())
	if !strings.Contains(v, "subagent · Ran 1 shell command") {
		t.Fatalf("the subagent's run should carry its name:\n%s", v)
	}
	if strings.Contains(v, "The output is sub-hello") {
		t.Fatalf("its prose belongs behind the fold:\n%s", v)
	}
	if !strings.Contains(v, "DONE") {
		t.Fatal("the main conversation's answer stays open")
	}
	if !strings.Contains(v, "▸ Ran 1 agent") {
		t.Fatalf("the launch folds as the main turn's own run:\n%s", v)
	}

	// Click the subagent's summary line: everything behind it opens.
	for i, b := range tl.Blocks() {
		if b.Sub {
			tl.ToggleCollapse(i)
			break
		}
	}
	if v = stripSGR(tl.Content()); !strings.Contains(v, "The output is sub-hello") {
		t.Fatalf("opening the run should show its prose:\n%s", v)
	}
}

// A subagent's file edit arrives open — a turn is judged by what it changed,
// whoever in it did the changing.
func TestASubagentsEditArrivesOpen(t *testing.T) {
	tl := NewTimeline(90, 20)
	tl.AppendEvent(agent.Event{Kind: agent.KindToolCall, SubID: "toolu_p", Tool: &agent.ToolCall{Name: "Edit", Input: "the change"}})
	if tl.Blocks()[0].Collapsed {
		t.Fatal("an edit is the point, not an aside")
	}
}

// Task reports merge by id: the progress line has tokens, the notification
// has the output file, and /tasks needs both halves of the story.
func TestTaskReportsMergeAndAreListed(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	s.noteTask(&agent.TaskUpdate{ID: "aae4", Status: "running", Desc: "Echo the string", Type: "general-purpose"})
	s.noteTask(&agent.TaskUpdate{ID: "aae4", Status: "running", Tokens: 15062, LastTool: "Bash"})
	s.noteTask(&agent.TaskUpdate{ID: "b4z", Status: "running", Desc: "ping -n 4 127.0.0.1", Type: "local_bash"})
	if s.RunningTasks() != 2 {
		t.Fatalf("RunningTasks = %d", s.RunningTasks())
	}
	s.noteTask(&agent.TaskUpdate{ID: "aae4", Status: "completed", Summary: "sub-hello"})
	if s.RunningTasks() != 1 {
		t.Fatalf("after completion: %d", s.RunningTasks())
	}

	send(t, a, "/tasks")
	got := lastBlock(s)
	for _, want := range []string{"aae4", "completed", "general-purpose · Echo the string", "15k tokens", "b4z", "shell · ping -n 4 127.0.0.1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the list is missing %q:\n%s", want, got)
		}
	}
	if s.busy {
		t.Fatal("/tasks costs no turn")
	}
}

// /tasks <id> reads the tail of the output file the CLI named.
func TestOneTasksOutputCanBeRead(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	out := filepath.Join(t.TempDir(), "aae4.output")
	if err := os.WriteFile(out, []byte("line one\nline two\nsub-hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.noteTask(&agent.TaskUpdate{ID: "aae4", Status: "completed", Desc: "Echo", Type: "general-purpose", OutputFile: out, Summary: "did it"})

	send(t, a, "/tasks aae4")
	got := lastBlock(s)
	for _, want := range []string{"did it", "sub-hello", out} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}

	send(t, a, "/tasks nope")
	if !strings.Contains(a.note, "matches no task") {
		t.Fatalf("note = %q", a.note)
	}
}

// One run can end twice — an async task revives it. Money is counted per leg,
// the queue holds until the stream closes, and nothing after the first result
// is lost.
func TestATurnOnlyEndsWhenTheStreamCloses(t *testing.T) {
	a := testApp(t)
	s := busyTurn(t, a)
	seq := s.streamSeq

	a.Update(agentEventMsg{sess: s.ID, seq: seq, ev: agent.Event{Kind: agent.KindText, Text: "waiting on the subagent"}})
	a.Update(agentEventMsg{sess: s.ID, seq: seq,
		ev: agent.Event{Kind: agent.KindTurnEnd, Result: &agent.TurnResult{CostUSD: 0.065}}})
	if !s.busy {
		t.Fatal("a result ends a leg, not the turn")
	}

	typeRunes(t, a, "queued while the task runs")
	press(t, a, kmsg(tea.KeyEnter))
	if got := s.Queued(); len(got) != 1 {
		t.Fatalf("the draft should wait: %q", got)
	}

	a.Update(agentEventMsg{sess: s.ID, seq: seq, ev: agent.Event{Kind: agent.KindText, SubID: "toolu_p", Text: "sub result"}})
	a.Update(agentEventMsg{sess: s.ID, seq: seq, ev: agent.Event{Kind: agent.KindText, Text: "DONE"}})
	a.Update(agentEventMsg{sess: s.ID, seq: seq,
		ev: agent.Event{Kind: agent.KindTurnEnd, Result: &agent.TurnResult{CostUSD: 0.005}}})
	a.Update(streamClosedMsg{sess: s.ID, seq: seq})

	if got := s.cost; got < 0.0699 || got > 0.0701 {
		t.Fatalf("cost = %v, want the two legs added once each", got)
	}
	if !strings.Contains(stripSGR(s.tl.Content()), "DONE") {
		t.Fatal("the second leg's answer must not be lost")
	}
	if len(s.Queued()) != 0 || !s.busy {
		t.Fatal("the close should have sent the waiting message")
	}
	s.close()
}

// The sidebar is the only place every agent is visible at once, so it says
// when one has subagents working — an agent whose subagent is grinding away
// used to look exactly like one thinking by itself.
func TestTheSidebarShowsSubagentsWorking(t *testing.T) {
	a := threeAgents(t)
	s := a.sessions[1]
	s.busy = true
	s.noteTask(&agent.TaskUpdate{ID: "t1", Status: "running", Desc: "audit", Type: "general-purpose"})
	s.noteTask(&agent.TaskUpdate{ID: "t2", Status: "running", Desc: "tests", Type: "general-purpose"})

	row := strings.Split(stripSGR(RenderSidebar(a.sessions, a.active, noDrag, "*", SidebarWidth-2, 10)), "\n")[SidebarRowOf(1)]
	if !strings.Contains(row, "+2") {
		t.Fatalf("the row should count the subagents: %q", row)
	}

	s.noteTask(&agent.TaskUpdate{ID: "t1", Status: "completed"})
	row = strings.Split(stripSGR(RenderSidebar(a.sessions, a.active, noDrag, "*", SidebarWidth-2, 10)), "\n")[SidebarRowOf(1)]
	if !strings.Contains(row, "+1") || strings.Contains(row, "+2") {
		t.Fatalf("one finished, one to go: %q", row)
	}
}

// A background task belongs to the run that launched it: when the CLI exits
// without saying how one ended, it stops counting as running rather than
// haunting the sidebar forever.
func TestTasksDoNotOutliveTheirRun(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	s.busy = true
	s.noteTask(&agent.TaskUpdate{ID: "t1", Status: "running", Desc: "long one"})
	if s.RunningTasks() != 1 {
		t.Fatal("it should count while the run is alive")
	}
	s.noteResult(&agent.TurnResult{})
	s.endTurn()
	if s.RunningTasks() != 0 {
		t.Fatalf("nothing is left to run it: %+v", s.tasks)
	}
	if len(s.tasks) != 1 || !strings.Contains(s.tasks[0].Status, "ended") {
		t.Fatalf("and /tasks should still say what became of it: %+v", s.tasks)
	}
}

// The hub counts what is running behind the turn, the way the CLI's own
// status line does, and says which key opens it.
func TestTheHubCountsBackgroundWork(t *testing.T) {
	a := testApp(t)
	a.resize(140, 26)
	s := a.cur()
	s.noteTask(&agent.TaskUpdate{ID: "b1", Status: "running", Type: "local_bash", Desc: "npx vitest run"})
	s.noteTask(&agent.TaskUpdate{ID: "b2", Status: "running", Type: "local_bash", Desc: "dotnet build"})
	s.noteTask(&agent.TaskUpdate{ID: "a1", Status: "running", Type: "general-purpose", Desc: "audit"})
	if sh, ag := s.Background(); sh != 2 || ag != 1 {
		t.Fatalf("Background() = %d shells, %d agents", sh, ag)
	}
	view := stripSGR(a.View())
	if !strings.Contains(view, "2 shells · 1 agent") {
		t.Fatalf("the hub should count them:\n%s", stripSGR(a.statusLine()))
	}
	if !strings.Contains(view, "(↓ to see)") {
		t.Fatal("and teach the gesture that opens them")
	}

	// Finished work stops being counted.
	s.noteTask(&agent.TaskUpdate{ID: "b1", Status: "completed"})
	s.noteTask(&agent.TaskUpdate{ID: "a1", Status: "completed"})
	if !strings.Contains(stripSGR(a.View()), "1 shell") {
		t.Fatalf("one left:\n%s", stripSGR(a.statusLine()))
	}
}

// ↓ opens the button row, which grows a background button while there is
// background work; picking a row shows what that task has been doing.
func TestTheBackgroundButtonShowsATask(t *testing.T) {
	a := testApp(t)
	a.resize(140, 26)
	s := a.cur()
	s.noteTask(&agent.TaskUpdate{ID: "b1", Status: "running", Type: "local_bash",
		Desc: "npx vitest run", LastTool: "Bash", Tokens: 15062})

	c := openControls(t, a)
	if len(c.chips) != 3 || c.chips[2].kind != controlBackground {
		t.Fatalf("the row should carry a background button: %+v", c.chips)
	}
	if c.chips[2].value != "1 shell" {
		t.Fatalf("button reads %q", c.chips[2].value)
	}
	rows := c.chips[2].rows()
	if len(rows) != 1 || !strings.Contains(rows[0], "shell") || !strings.Contains(rows[0], "npx vitest run") {
		t.Fatalf("rows = %q", rows)
	}
	if !strings.Contains(rows[0], "now: Bash") || !strings.Contains(rows[0], "15k tokens") {
		t.Fatalf("a row should say what it is doing: %q", rows[0])
	}

	a.Update(kmsg(tea.KeyRight)) // model -> permissions
	a.Update(kmsg(tea.KeyRight)) // -> background
	a.Update(kmsg(tea.KeyEnter)) // open its list
	a.Update(kmsg(tea.KeyEnter)) // take the first row
	if got := lastBlock(s); !strings.Contains(got, "npx vitest run") {
		t.Fatalf("picking one should show what it has been up to:\n%s", got)
	}
	if s.busy {
		t.Fatal("looking at background work costs no turn")
	}
}

// With nothing running, the button is not in the way.
func TestNoBackgroundNoButton(t *testing.T) {
	a := testApp(t)
	c := openControls(t, a)
	if len(c.chips) != 2 {
		t.Fatalf("chips = %d, want just model and permissions", len(c.chips))
	}
	if strings.Contains(stripSGR(a.statusLine()), "to see") {
		t.Fatal("and the hub says nothing about background work")
	}
}
