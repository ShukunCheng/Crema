package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/ShukunCheng/Crema/internal/agent"
)

// A held-open process is let go after an hour because that is how long its
// prompt cache lives. But a subagent still running is not an idle process — it
// is the work — and closing the conversation kills the process tree it runs
// in. This is the bug that made long reviews never come back: the agent was
// handed a job, the turn ended, and an hour after the message that started it
// crema killed the lot mid-tool-call.
func TestBackgroundWorkKeepsTheProcessAlive(t *testing.T) {
	_, s, _ := ctlApp(t)
	if s.conv == nil {
		t.Fatal("this test needs a held-open process")
	}
	s.busy = false
	s.noteTask(&agent.TaskUpdate{ID: "a1", Status: "running", Type: "general-purpose",
		Desc: "PR review of the branch diff"})
	s.convAt = time.Now().Add(-2 * convIdleLife) // long past the hour

	s.maybeCloseIdleConv()
	if s.conv == nil {
		t.Fatal("a running subagent must not be killed to save a process")
	}
	// And the hour is measured from when the work stops, not from the message
	// that started it — otherwise it dies the moment the subagent reports.
	if time.Since(s.convAt) > time.Minute {
		t.Fatalf("the clock should have restarted, it reads %s old", time.Since(s.convAt))
	}
	s.noteTask(&agent.TaskUpdate{ID: "a1", Status: "completed"})
	s.maybeCloseIdleConv()
	if s.conv == nil {
		t.Fatal("and it gets its full hour after the work finishes")
	}
	s.close()
}

// With nothing running it still lets go, which is the point of the rule.
func TestAnIdleProcessIsStillLetGo(t *testing.T) {
	_, s, _ := ctlApp(t)
	s.busy = false
	// ctlApp leaves a subagent running; this test is about the case where
	// nothing is.
	s.noteTask(&agent.TaskUpdate{ID: "aae4", Status: "completed"})
	s.convAt = time.Now().Add(-2 * convIdleLife)
	s.maybeCloseIdleConv()
	if s.conv != nil {
		t.Fatal("an hour with nothing running is what the rule is for")
	}
	s.close()
}

// A turn that ran longer than the hour leaves the clock stale, and the next
// frame used to throw away a process whose cache had just been written.
func TestALongTurnDoesNotStrandTheClock(t *testing.T) {
	_, s, _ := ctlApp(t)
	if s.conv == nil {
		t.Fatal("this test needs a held-open process")
	}
	s.busy = true
	s.convAt = time.Now().Add(-2 * convIdleLife) // the message went two hours ago
	s.lastResult = &agent.TurnResult{}
	s.endTurn()

	s.maybeCloseIdleConv()
	if s.conv == nil {
		t.Fatal("the cache was written seconds ago; the process is worth keeping")
	}
	s.close()
}

// Closing the conversation is what kills the work, so it says so rather than
// leaving a subagent listed as running when nothing is left to run it.
func TestClosingTheConversationEndsItsTasks(t *testing.T) {
	_, s, _ := ctlApp(t)
	s.noteTask(&agent.TaskUpdate{ID: "a1", Status: "running", Type: "general-purpose"})
	if s.RunningTasks() == 0 {
		t.Fatal("the task should be running")
	}
	s.closeConv("a test closed it")
	if s.RunningTasks() != 0 {
		t.Fatal("its process is gone, so it is not running")
	}
	if got := s.tasks[0].Status; got != "stopped" {
		t.Fatalf("status = %q, want stopped", got)
	}
	if got := s.tasks[0].Why; !strings.Contains(got, "a test closed it") {
		t.Fatalf("and it should carry the reason its process was closed, got %q", got)
	}
	s.close()
}

// A turn that is stopped by something other than your own esc used to end in
// silence: the stream reports a closed process as a cancellation, and neither
// branch of finishTurn said a word, so the answer simply never arrived and
// there was nothing to chase. Now it names what did it.
func TestAStoppedTurnSaysWhatStoppedIt(t *testing.T) {
	for _, c := range []struct {
		name string
		do   func(a *App, s *Session)
		want string
	}{
		{"a settings change", func(a *App, s *Session) { s.SetEffort("low") }, effortWhy},
		{"the idle sweep", func(a *App, s *Session) {
			s.closeConv("the process had been idle for an hour")
		}, "idle for an hour"},
		{"something unnamed", func(a *App, s *Session) { s.closeConv("") }, "its process went away"},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, s, _ := ctlApp(t)
			s.busy = true // a long turn, still running
			c.do(a, s)
			// The stream reports the closed process the way it really does.
			s.noteResult(&agent.TurnResult{Canceled: true})
			a.finishTurn(s)

			got := stripSGR(s.tl.Content())
			if !strings.Contains(got, "this turn stopped before it finished") {
				t.Fatalf("it should say the turn was cut short:\n%s", lastLines(got, 4))
			}
			if !strings.Contains(got, c.want) {
				t.Fatalf("and name %q as the cause:\n%s", c.want, lastLines(got, 4))
			}
			s.close()
		})
	}
}

// Your own esc needs no explanation — the note already said so, and a red
// block for something you asked for is noise.
func TestCancellingByHandStaysQuiet(t *testing.T) {
	a, s, _ := ctlApp(t)
	s.busy = true
	s.canceled = true
	s.noteResult(&agent.TurnResult{Canceled: true})
	a.finishTurn(s)
	if got := stripSGR(s.tl.Content()); strings.Contains(got, "stopped before it finished") {
		t.Fatalf("cancelling by hand should not be reported as a fault:\n%s", lastLines(got, 4))
	}
	s.close()
}
