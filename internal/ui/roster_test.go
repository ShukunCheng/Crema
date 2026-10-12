package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func withSubagents(t *testing.T, w int) (*App, *Session) {
	t.Helper()
	a := testApp(t)
	a.resize(w, 26)
	s := a.cur()
	s.busy, s.turnStart, s.activity, s.turnOut = true, time.Now().Add(-95*time.Second), "Edit", 3400
	s.noteTask(&agent.TaskUpdate{ID: "a1", Status: "running", Type: "general-purpose",
		Desc: "Locating orderContextMenu render in PlanboardDialogs.tsx", LastTool: "Grep", Tokens: 17000})
	s.noteTask(&agent.TaskUpdate{ID: "a2", Status: "running", Type: "structural-completeness-reviewer",
		Desc: "Diffing PlanboardViewSwitch.test.tsx", Tokens: 15600})
	s.tasks[0].Started = time.Now().Add(-411 * time.Second)
	s.tasks[1].Started = time.Now().Add(-401 * time.Second)
	a.resize(w, 26) // the rows the roster takes come off the panes
	return a, s
}

// Running work gets rows of its own: who, what they are on now, how long, and
// what they have written — the agent itself first.
func TestTheRosterListsWhatIsRunning(t *testing.T) {
	a, _ := withSubagents(t, 120)
	view := stripSGR(a.View())
	for _, want := range []string{
		"● main", "Edit", "1m 35s", "↓ 3.4k",
		"○ general-purpose", "Grep: Locating orderContextMenu", "6m 51s", "↓ 17k",
		"○ structural-completeness", "Diffing PlanboardViewSwitch", "6m 41s", "↓ 15k",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("the roster is missing %q:\n%s", want, lastLines(view, 5))
		}
	}
}

// Every row is exactly the pane's width, at any size: the numbers must not
// wrap onto a line of their own and push the frame out of shape.
func TestTheRosterFitsItsWidth(t *testing.T) {
	for _, w := range []int{80, 100, 120, 200} {
		a, _ := withSubagents(t, w)
		lines := strings.Split(a.View(), "\n")
		if len(lines) != 26 {
			t.Fatalf("width %d: frame is %d rows, want 26", w, len(lines))
		}
		for _, ln := range lines[len(lines)-3:] {
			if got := lipgloss.Width(stripSGR(ln)); got != w {
				t.Fatalf("width %d: a roster row is %d columns: %q", w, got, stripSGR(ln))
			}
		}
	}
}

// The rows are only there while something is running; an idle agent gets its
// screen back, and the conversation gets the rows back.
func TestTheRosterGoesWhenTheWorkDoes(t *testing.T) {
	a, s := withSubagents(t, 120)
	busyPane := a.lay.PaneH
	if s.RosterHeight() != 3 {
		t.Fatalf("RosterHeight = %d", s.RosterHeight())
	}
	s.noteTask(&agent.TaskUpdate{ID: "a1", Status: "completed"})
	s.noteTask(&agent.TaskUpdate{ID: "a2", Status: "completed"})
	a.resize(a.w, a.h)
	if s.RosterHeight() != 0 {
		t.Fatalf("nothing is running: %+v", s.roster())
	}
	if strings.Contains(stripSGR(a.View()), "● main") {
		t.Fatal("and the rows should be gone")
	}
	if a.lay.PaneH <= busyPane {
		t.Fatal("the conversation should have the rows back")
	}
}

// A turn that has ended while its subagents work says so rather than showing
// a frozen clock for work it is not doing.
func TestTheMainRowWaitsWhenTheTurnIsDone(t *testing.T) {
	a, s := withSubagents(t, 120)
	s.busy = false
	view := stripSGR(a.View())
	if !strings.Contains(view, "waiting on the background work") {
		t.Fatalf("the main row should say what it is doing:\n%s", lastLines(view, 4))
	}
	if !strings.Contains(view, "○ general-purpose") {
		t.Fatal("and its subagents are still listed")
	}
}

// A crowd is the other case: the rows go, the hub keeps the counts, and the
// walk gets one stop for all of it rather than a row each eating the pane.
func TestACrowdCollapsesToTheCounts(t *testing.T) {
	a, s := withSubagents(t, 120)
	for i := 0; i < 8; i++ {
		s.noteTask(&agent.TaskUpdate{ID: string(rune('c' + i)), Status: "running",
			Type: "general-purpose", Desc: "more work"})
	}
	a.resize(a.w, a.h)
	if s.RosterHeight() != 0 {
		t.Fatalf("RosterHeight = %d, want no rows for a crowd", s.RosterHeight())
	}
	view := stripSGR(a.View())
	if strings.Contains(view, "● main") {
		t.Fatal("the rows should be gone")
	}
	if !strings.Contains(view, "10 agents") {
		t.Fatalf("the hub should carry the count instead:\n%s", lastLines(view, 3))
	}
	c := NewControls(s)
	last := c.chips[len(c.chips)-1]
	if len(c.chips) != 4 || last.kind != controlBackground {
		t.Fatalf("the walk should be the hub's own values then one stop for the crowd: %+v", c.chips)
	}
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// The walk always starts in the same place. ↓ reaches the model first, then
// the permission mode, and only then the running work — so the two settings
// keep their two positions whether anything is running or not. They used to
// come after the roster, which meant the number of presses to reach the model
// depended on how many subagents happened to be going.
func TestTheWalkReachesTheSettingsBeforeTheRunningWork(t *testing.T) {
	a, s := withSubagents(t, 120)
	c := openControls(t, a)
	want := []controlKind{controlModel, controlPermission, controlEffort,
		controlMain, controlAgent, controlAgent}
	for i, k := range want {
		if c.Kind() != k {
			t.Fatalf("stop %d is kind %v, want %v", i, c.Kind(), k)
		}
		a.Update(kmsg(tea.KeyDown))
	}
	// And with nothing running the first two stops are the same two.
	b := testApp(t)
	b.resize(120, 26)
	d := openControls(t, b)
	if d.Kind() != controlModel {
		t.Fatalf("idle, ↓ should still land on the model, got %v", d.Kind())
	}
	b.Update(kmsg(tea.KeyDown))
	if d.Kind() != controlPermission {
		t.Fatalf("idle, then the permission mode, got %v", d.Kind())
	}
	_ = s
}

// Clicking a roster row still puts the walk on that row, now that the rows are
// no longer the first chips.
func TestClickingARosterRowSelectsIt(t *testing.T) {
	a, s := withSubagents(t, 120)
	rows := s.RosterHeight()
	if rows != 3 {
		t.Fatalf("RosterHeight = %d, want the main row and two subagents", rows)
	}
	// The last roster row is the last line of the frame.
	clickPR(a, a.w/2, a.h-1)
	if a.controls == nil {
		t.Fatal("clicking a row should open the walk on it")
	}
	if got := a.controls.Task(); got != "a2" {
		t.Fatalf("the walk is on task %q, want the row that was clicked", got)
	}
	// And the main row, two lines above it.
	clickPR(a, a.w/2, a.h-3)
	if a.controls.Kind() != controlMain {
		t.Fatalf("the walk should be on the main row, got %v", a.controls.Kind())
	}
}

// A shell's description is its command, and a command can be a whole script.
// Drawn as it came, every line of a heredoc became a row under the status
// bar, pushing the frame past the bottom of the screen. The row shows the
// first line and says there is more; the frame stays exactly as tall as the
// window.
func TestAShellRunningAScriptTakesOneRow(t *testing.T) {
	a := testApp(t)
	a.resize(120, 30)
	s := a.cur()
	script := "cat > /d/tmp/ci/monitor.sh <<'EOF'\n#!/usr/bin/env bash\nwhile :; do\n  sleep 300\ndone\nEOF\nbash /d/tmp/ci/monitor.sh"
	s.noteTask(&agent.TaskUpdate{ID: "b1", Status: "running", Type: "local_bash", Desc: script})
	a.resize(120, 30)
	view := stripSGR(a.View())
	if got := len(strings.Split(view, "\n")); got != 30 {
		t.Fatalf("the frame is %d rows, want the window's 30", got)
	}
	if strings.Contains(view, "#!/usr/bin/env bash") || strings.Contains(view, "sleep 300") {
		t.Fatalf("the script's body should not be drawn:\n%s", lastLines(view, 6))
	}
	if !strings.Contains(view, "cat > /d/tmp/ci/monitor.sh <<'EOF' …") {
		t.Fatalf("the row should show the first line and that there is more:\n%s", lastLines(view, 6))
	}
}

func TestDescLine(t *testing.T) {
	for in, want := range map[string]string{
		"":                        "",
		"one line":                "one line",
		"  padded \t tabs ":       "padded tabs",
		"first\nsecond":           "first …",
		"only\n\n  \n":            "only",
		"\n\nafter blank lines\n": "after blank lines",
	} {
		if got := descLine(in); got != want {
			t.Errorf("descLine(%q) = %q, want %q", in, got, want)
		}
	}
}
