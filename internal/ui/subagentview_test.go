package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

// writeSubLog puts a subagent transcript where crema looks for one: under the
// project slug of the agent's directory, then the session id, then subagents.
// This is the CLI's own layout, measured against 417 of these on this machine.
func writeSubLog(t *testing.T, s *Session, task string, lines ...string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".claude", "projects", slugOf(s.Dir), s.agentSID, "subagents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "agent-"+task+".jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := `{"agentType":"Explore","description":"Find the column layout"}`
	if err := os.WriteFile(filepath.Join(dir, "agent-"+task+".meta.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// slugOf is the CLI's project-directory name, the same rule internal/agent
// uses: every byte that is not a letter or a digit becomes a dash.
func slugOf(dir string) string {
	var b strings.Builder
	for _, r := range dir {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

const subPrompt = `{"type":"user","message":{"role":"user","content":"Find where the columns are laid out."}}`
const subAnswer = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"It is in OrderTab.tsx"}]}}`
const subTool = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Grep","input":{"pattern":"grid-cols"}}]}}`

// Enter on a subagent's row opens that subagent's own conversation in the
// pane — the prompt it was handed, what it ran, what it answered — read from
// the transcript the CLI is writing as it goes. This is what enter does on
// the CLI's own subagent panel, and it costs nothing: the alternative was
// asking the main agent to relay it, which spends a whole turn.
func TestEnterOnASubagentOpensItsOwnConversation(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	s.agentSID = "sess-9"
	writeSubLog(t, s, "a1", subPrompt, subTool, subAnswer)
	s.noteTask(&agent.TaskUpdate{ID: "a1", Status: "running", Type: "Explore", Desc: "Find the columns"})
	a.resize(120, 30)

	walkToSubagent(t, a)            // model, permissions, main, the subagent
	press(t, a, kmsg(tea.KeyEnter)) // which enter opens
	if a.viewing != "a1" {
		t.Fatalf("enter on the row should open it, viewing = %q", a.viewing)
	}
	// Enter asks for an immediate poll, which reads in the background so the
	// keyboard never waits on the disk. A test runs that poll by hand.
	s.pollSubagents()
	view := stripSGR(a.View())
	for _, want := range []string{
		"Explore",                             // the header says whose conversation this is
		"esc to go back",                      // and how to leave it
		"Find where the columns are laid out", // the prompt it was given
		"It is in OrderTab.tsx",               // and what it answered
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("the subagent's conversation is missing %q:\n%s", want, view)
		}
	}
	// Esc puts the agent's own conversation back, and stops nothing.
	press(t, a, kmsg(tea.KeyEsc))
	if a.viewing != "" {
		t.Fatalf("esc should close it, viewing = %q", a.viewing)
	}
	if s.RunningTasks() != 1 {
		t.Fatal("looking away from a subagent must not stop it")
	}
}

// The transcript is read as it grows: only the new bytes, so polling it twice
// a second while the subagent works costs a stat and a short read.
func TestASubagentTranscriptIsReadAsItGrows(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	s.agentSID = "sess-9"
	p := writeSubLog(t, s, "a1", subPrompt)
	s.noteTask(&agent.TaskUpdate{ID: "a1", Status: "running", Type: "Explore"})
	a.resize(120, 30)

	if !s.pollSubagents() {
		t.Fatal("the prompt should have arrived")
	}
	if s.pollSubagents() {
		t.Fatal("nothing has been added, so nothing should arrive")
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(subAnswer + "\n")
	f.Close()
	if !s.pollSubagents() {
		t.Fatal("the answer should have arrived")
	}
	a.viewing = "a1"
	if got := stripSGR(a.View()); !strings.Contains(got, "It is in OrderTab.tsx") {
		t.Fatalf("the new line should be on screen:\n%s", got)
	}
}

// A subagent announced before its file exists says so, rather than showing an
// empty pane that reads as a subagent doing nothing.
func TestASubagentWithNoTranscriptYetSaysSo(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	s.agentSID = "sess-9"
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	s.noteTask(&agent.TaskUpdate{ID: "a1", Status: "running", Type: "Explore"})
	a.resize(120, 30)
	a.viewing = "a1"
	if got := stripSGR(a.View()); !strings.Contains(got, "no transcript for a1 yet") {
		t.Fatalf("it should say what it is waiting for:\n%s", got)
	}
}

// A subagent that succeeded leaves the roster at once — its answer is already
// in the conversation. One that failed or was stopped is held for half a
// minute, the way the CLI holds it: otherwise the row and the reason for it
// disappear at the same instant.
func TestAFailedSubagentIsHeldOnTheRosterAndASuccessfulOneIsNot(t *testing.T) {
	a, s := withSubagents(t, 120)
	s.noteTask(&agent.TaskUpdate{ID: "a1", Status: "completed"})
	s.noteTask(&agent.TaskUpdate{ID: "a2", Status: "failed"})
	a.resize(a.w, a.h)
	view := stripSGR(a.View())
	if strings.Contains(view, "general-purpose") {
		t.Fatalf("a completed subagent should go at once:\n%s", lastLines(view, 4))
	}
	if !strings.Contains(view, "failed") {
		t.Fatalf("a failed one should stay and say so:\n%s", lastLines(view, 4))
	}
	// And it goes on its own once it is no longer news.
	s.tasks[1].Ended = time.Now().Add(-taskLinger - time.Second)
	a.resize(a.w, a.h)
	if s.RosterHeight() != 0 {
		t.Fatalf("after %s the row is history: %+v", taskLinger, s.roster())
	}
}

// A subagent of a subagent is drawn as one: the CLI's own panel is a tree,
// and spawn_depth is on the task_started line, so crema does not have to
// guess.
func TestASubagentsSubagentIsIndented(t *testing.T) {
	a, s := withSubagents(t, 120)
	s.noteTask(&agent.TaskUpdate{ID: "a3", Status: "running", Type: "Explore",
		Desc: "reading callers", SpawnDepth: 2})
	a.resize(a.w, a.h)
	rows := s.roster()
	var deep *rosterRow
	for i := range rows {
		if rows[i].task == "a3" {
			deep = &rows[i]
		}
	}
	if deep == nil {
		t.Fatalf("the nested subagent should be on the roster: %+v", rows)
	}
	if got := rosterName(*deep); !strings.HasPrefix(got, "  ") {
		t.Fatalf("a depth-2 row should be indented, got %q", got)
	}
	if got := rosterName(rows[1]); strings.HasPrefix(got, " ") {
		t.Fatalf("a depth-1 row should not be, got %q", got)
	}
}

// ctrl+x ctrl+k stops every subagent at once — the CLI's own chord, and like
// the CLI it takes it twice, because a chord is easy to hit by accident and a
// subagent halfway through something is expensive to lose.
func TestCtrlXCtrlKStopsEverythingWhenAskedTwice(t *testing.T) {
	a, s := withSubagents(t, 120)
	press(t, a, kmsg(tea.KeyCtrlX))
	press(t, a, kmsg(tea.KeyCtrlK))
	if !strings.Contains(a.note, "again") {
		t.Fatalf("the first chord should ask again, note = %q", a.note)
	}
	if s.RunningTasks() != 2 {
		t.Fatal("and it should stop nothing yet")
	}
	press(t, a, kmsg(tea.KeyCtrlX))
	press(t, a, kmsg(tea.KeyCtrlK))
	if !strings.Contains(a.note, "stopping all") && !strings.Contains(a.note, "cannot be asked") {
		t.Fatalf("the second should act, note = %q", a.note)
	}
	// ctrl+k on its own is not the chord: it belongs to the message box.
	a.chordAt, a.stopAllAt = time.Time{}, time.Time{}
	a.note = ""
	press(t, a, kmsg(tea.KeyCtrlK))
	if strings.Contains(a.note, "stop") {
		t.Fatalf("ctrl+k alone should not stop anything, note = %q", a.note)
	}
}
