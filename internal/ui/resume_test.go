package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

// withSessions is a mock backend that also lists saved conversations, the way
// Claude Code does.
type withSessions struct{ *agent.Mock }

func (withSessions) Sessions(string) []agent.SessionInfo {
	return []agent.SessionInfo{
		{ID: "22222222-fresh", When: time.Now().Add(-5 * time.Minute), Preview: "add dark mode"},
		{ID: "11111111-old", When: time.Now().Add(-2 * time.Hour), Preview: "fix the login bug"},
	}
}

func resumeApp(t *testing.T) *App {
	t.Helper()
	a := testApp(t)
	a.cur().Backend = withSessions{agent.NewMock()}
	return a
}

// /resume lists the project's conversations newest first and puts them in the
// option picker; picking one points the agent at it.
func TestResumeListsAndAttaches(t *testing.T) {
	a := resumeApp(t)
	s := a.cur()
	s.agentSID = "11111111-old"
	send(t, a, "/resume")
	got := lastBlock(s)
	for _, want := range []string{"22222222", "5m ago", "add dark mode", "11111111", "the one this agent is on"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the list is missing %q:\n%s", want, got)
		}
	}
	if a.choices == nil {
		t.Fatal("the conversations should be in the option picker")
	}

	press(t, a, kmsg(tea.KeyEnter)) // the newest is first
	if s.agentSID != "22222222-fresh" {
		t.Fatalf("agentSID = %q", s.agentSID)
	}
	if !strings.Contains(lastBlock(s), "add dark mode") {
		t.Fatalf("the attach should say what it attached to: %q", lastBlock(s))
	}
	if s.busy {
		t.Fatal("attaching costs no turn — the next message does the resuming")
	}
	if s.ctxTokens != 0 {
		t.Fatal("the old conversation's size does not describe the new one")
	}
}

func TestResumeByPrefixAndItsRefusals(t *testing.T) {
	a := resumeApp(t)
	s := a.cur()
	send(t, a, "/resume 1111")
	if s.agentSID != "11111111-old" {
		t.Fatalf("agentSID = %q", s.agentSID)
	}
	send(t, a, "/resume 1111")
	if !strings.Contains(a.note, "already on") {
		t.Fatalf("note = %q", a.note)
	}
	send(t, a, "/resume zzz")
	if !strings.Contains(a.note, "matches no saved conversation") {
		t.Fatalf("note = %q", a.note)
	}
}

// A backend that keeps its conversations to itself gets an honest refusal,
// not an empty list.
func TestResumeOnABackendWithoutSavedSessions(t *testing.T) {
	a := testApp(t)
	send(t, a, "/resume")
	if !strings.Contains(a.note, "does not keep conversations") {
		t.Fatalf("note = %q", a.note)
	}
}
