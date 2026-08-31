package ui

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
)

// The whole feature against a real capture: claude 2.1.229 running an async
// subagent, recorded verbatim in testdata. Every claim the UI makes about
// subagents is checked against what the CLI actually sends.
func TestARealSubagentStreamEndToEnd(t *testing.T) {
	f, err := os.Open("../agent/testdata/claude_subagent.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	p := &agent.ClaudeParser{}
	tl := NewTimeline(100, 40)
	s := NewSession(1, agent.NewMock(), t.TempDir())
	defer s.close()
	var cost float64
	var lastResult *agent.TurnResult
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		for _, ev := range p.ParseLine(sc.Bytes()) {
			tl.AppendEvent(ev)
			s.noteActivity(ev)
			if ev.Kind == agent.KindTask {
				s.noteTask(ev.Task)
			}
			if ev.Kind == agent.KindTurnEnd {
				cost += ev.Result.CostUSD
				if len(ev.Result.RateLimits) > 0 {
					lastResult = ev.Result
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}

	// The event's unifiedWindows reach the result: both windows, known.
	rl := lastResult.RateLimits
	if len(rl) != 2 || !rl[0].Known || rl[0].Utilization != 0.53 || rl[1].Utilization != 0.31 {
		t.Fatalf("RateLimits = %+v", rl)
	}

	// The run's own bill, counted once across its two result lines.
	if cost < 0.06509 || cost > 0.06510 {
		t.Fatalf("cost = %v, want the run's own 0.0650971 exactly once", cost)
	}

	v := stripSGR(tl.Content())
	if !strings.Contains(v, "subagent · Ran 1 shell command") {
		t.Fatalf("the subagent's work should fold under its own name:\n%s", v)
	}
	if !strings.Contains(v, "Reported back") {
		t.Fatalf("its closing words should be counted where they fold:\n%s", v)
	}
	if !strings.Contains(v, "DONE") {
		t.Fatalf("the main answer stays open:\n%s", v)
	}
	if strings.Contains(v, "sub-hello") {
		t.Fatalf("the subagent's output belongs behind the fold:\n%s", v)
	}

	// The task record carries both halves: the heartbeat's numbers and the
	// notification's output file.
	if len(s.tasks) != 1 {
		t.Fatalf("tasks = %+v", s.tasks)
	}
	tk := s.tasks[0]
	if tk.Status != "completed" || tk.Tokens != 15062 || tk.OutputFile == "" || !strings.Contains(tk.Summary, "sub-hello") {
		t.Fatalf("task = %+v", tk)
	}
}

// The synchronous shape, from a second real capture (haiku, 2.1.250): the
// model waits for the subagent rather than backgrounding it. The lifecycle
// lines are the same four, so the same detection has to hold — this is the
// case a user sees most often.
func TestASynchronousSubagentIsDetected(t *testing.T) {
	f, err := os.Open("../agent/testdata/claude_sync_subagent.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	p := &agent.ClaudeParser{}
	s := NewSession(1, agent.NewMock(), t.TempDir())
	defer s.close()
	s.busy = true
	var sawRunning bool
	var working string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		for _, ev := range p.ParseLine(sc.Bytes()) {
			s.noteActivity(ev)
			if ev.Kind == agent.KindTask {
				s.noteTask(ev.Task)
			}
			if s.RunningTasks() > 0 {
				sawRunning = true
				if w := stripSGR(s.workingLine("*", 90)); strings.Contains(w, "background task") {
					working = w
				}
			}
		}
	}
	if !sawRunning {
		t.Fatal("the subagent was never counted as running")
	}
	if !strings.Contains(working, "1 background task") {
		t.Fatalf("the working line should say so: %q", working)
	}
	if len(s.tasks) != 1 {
		t.Fatalf("tasks = %+v", s.tasks)
	}
	tk := s.tasks[0]
	if tk.Status != "completed" || tk.Type != "general-purpose" || tk.Tokens != 16011 || tk.LastTool != "Bash" {
		t.Fatalf("task = %+v", tk)
	}
	if tk.OutputFile == "" || !strings.Contains(tk.Summary, "sync-sub") {
		t.Fatalf("its report should be there for /tasks: %+v", tk)
	}
}
