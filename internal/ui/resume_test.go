package ui

import (
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
)

// The sequence from crema's own log: the turn ends leaving subagents running,
// the last one finishes, and the CLI picks the turn back up on its own to read
// the results — no message sent. The agent is working, and it used to show as
// idle, because busy was only ever set by a message you sent.
func TestAResumedTurnIsNotIdle(t *testing.T) {
	a, s, _ := streamApp(t)
	send(t, a, "fan out a review")
	s.noteTask(&agent.TaskUpdate{ID: "a1", Status: "running", Type: "general-purpose"})
	result(t, a, s) // the turn ends, the subagent still going
	if s.busy {
		t.Fatal("the turn should be over")
	}
	if s.RunningTasks() != 1 {
		t.Fatal("with the subagent still running")
	}

	// The subagent finishes. For a moment nothing is running at all.
	s.noteTask(&agent.TaskUpdate{ID: "a1", Status: "completed"})
	if !sidebarSays(a, "idle") {
		t.Fatal("between the two, idle is honestly what it is")
	}

	// And the CLI carries on by itself, reading what came back.
	say(t, a, s, "the review found three problems")
	if !s.busy {
		t.Fatal("the agent is speaking; it is working, not idle")
	}
	if sidebarSays(a, "idle") {
		t.Fatal("the sidebar must not call a working agent idle")
	}

	// The resumed leg ends like any other turn.
	result(t, a, s)
	if s.busy {
		t.Fatal("and when it finishes, it is idle again")
	}
	s.close()
}

// A subagent's own events stream through the main agent's process, tagged with
// the tool call that launched it. That is the subagent working, which the
// running count already shows — it is not the main agent picking back up.
func TestASubagentsEventsDoNotResumeTheTurn(t *testing.T) {
	a, s, _ := streamApp(t)
	send(t, a, "fan out")
	s.noteTask(&agent.TaskUpdate{ID: "a1", Status: "running", Type: "Explore"})
	result(t, a, s)

	a.Update(agentEventMsg{sess: s.ID, seq: s.streamSeq,
		ev: agent.Event{Kind: agent.KindToolCall, SubID: "toolu_launch",
			Tool: &agent.ToolCall{Name: "Grep"}}})
	if s.busy {
		t.Fatal("a subagent's tool call is not the main agent resuming")
	}
	s.close()
}

// sidebarSays reports whether the sidebar row for the focused agent reads with
// the given word.
func sidebarSays(a *App, word string) bool {
	a.resize(140, 30)
	return strings.Contains(stripSGR(RenderSidebar(a.sessions, a.active, noDrag, "*", 30, 20)), word)
}
