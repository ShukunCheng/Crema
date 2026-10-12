package ui

import (
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
)

// A turn that stopped rather than finished should say so. max_tokens and
// refusal are the two the model reports itself, and both used to look exactly
// like an ordinary ending — the reply just stopped, mid-sentence, with a green
// tick under it.
func TestATurnThatStoppedSaysWhy(t *testing.T) {
	for _, c := range []struct{ reason, want string }{
		{"max_tokens", "length ceiling"},
		{"refusal", "declined"},
	} {
		a := testApp(t)
		s := a.cur()
		s.busy = true
		s.noteResult(&agent.TurnResult{StopReason: c.reason})
		a.finishTurn(s)
		if got := stripSGR(s.tl.Content()); !strings.Contains(got, c.want) {
			t.Fatalf("%s should be reported (%q):\n%s", c.reason, c.want, lastLines(got, 4))
		}
	}
	// The ordinary endings say nothing: the answer is the account.
	for _, reason := range []string{"end_turn", "tool_use", ""} {
		a := testApp(t)
		s := a.cur()
		s.busy = true
		s.noteResult(&agent.TurnResult{StopReason: reason})
		a.finishTurn(s)
		got := stripSGR(s.tl.Content())
		if strings.Contains(got, "length ceiling") || strings.Contains(got, "declined") {
			t.Fatalf("%q is an ordinary ending and needs no line:\n%s", reason, lastLines(got, 4))
		}
	}
}

// A turn can be over while the work is not. Saying so is the difference
// between "it stopped" and "it handed the job on and is waiting".
func TestATurnLeavingWorkBehindSaysSo(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	s.noteTask(&agent.TaskUpdate{ID: "a1", Status: "running", Type: "general-purpose",
		Desc: "PR review"})
	s.noteTask(&agent.TaskUpdate{ID: "a2", Status: "running", Type: "general-purpose",
		Desc: "test coverage"})
	s.busy = true
	s.noteResult(&agent.TurnResult{})
	a.finishTurn(s)

	got := stripSGR(s.tl.Content())
	if !strings.Contains(got, "2 subagents are still running") {
		t.Fatalf("it should say what is left running:\n%s", lastLines(got, 4))
	}
	// One reads as one.
	b := testApp(t)
	x := b.cur()
	x.noteTask(&agent.TaskUpdate{ID: "a1", Status: "running", Type: "Explore"})
	x.busy = true
	x.noteResult(&agent.TurnResult{})
	b.finishTurn(x)
	if got := stripSGR(x.tl.Content()); !strings.Contains(got, "1 subagent is still running") {
		t.Fatalf("one subagent should read as one:\n%s", lastLines(got, 4))
	}
	// And a turn with nothing behind it says nothing.
	c := testApp(t)
	y := c.cur()
	y.busy = true
	y.noteResult(&agent.TurnResult{})
	c.finishTurn(y)
	if got := stripSGR(y.tl.Content()); strings.Contains(got, "still running") {
		t.Fatalf("nothing is running; there is nothing to say:\n%s", lastLines(got, 4))
	}
}

// A subagent killed with its process leaves nothing in the conversation of its
// own — its answer never arrived — so the conversation is where it is said.
// The roster row is gone in thirty seconds; the question it was answering is
// not.
func TestAKilledSubagentSaysWhyInTheConversation(t *testing.T) {
	_, s, _ := ctlApp(t)
	s.noteTask(&agent.TaskUpdate{ID: "a1", Status: "running", Type: "general-purpose",
		Desc: "PR review of the branch diff"})
	s.closeConv("the process had been idle for an hour")

	got := stripSGR(s.tl.Content())
	if !strings.Contains(got, "stopped before it finished") {
		t.Fatalf("the conversation should carry it:\n%s", lastLines(got, 6))
	}
	if !strings.Contains(got, "idle for an hour") {
		t.Fatalf("and name the reason:\n%s", lastLines(got, 6))
	}
	if !strings.Contains(got, "PR review of the branch diff") {
		t.Fatalf("and say what was lost:\n%s", lastLines(got, 6))
	}
	// The task itself carries the reason too, for /tasks and the roster.
	for _, task := range s.tasks {
		if task.Status == "stopped" && task.Why == "" {
			t.Fatalf("a stopped task should carry its reason: %+v", task)
		}
	}
	s.close()
}

// A subagent that finished on its own is not a casualty and gets no such line.
func TestACompletedSubagentIsNotReportedAsStopped(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	s.noteTask(&agent.TaskUpdate{ID: "a1", Status: "running", Type: "Explore", Desc: "look"})
	s.noteTask(&agent.TaskUpdate{ID: "a1", Status: "completed"})
	s.endStrayTasks("the agent's process ended")
	if got := stripSGR(s.tl.Content()); strings.Contains(got, "stopped before it finished") {
		t.Fatalf("it had already finished:\n%s", lastLines(got, 4))
	}
}
