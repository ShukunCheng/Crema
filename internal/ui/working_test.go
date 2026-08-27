package ui

import (
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

// feed delivers one stream event to a session, the way the adapter does.
func feed(a *App, s *Session, ev agent.Event) {
	a.Update(agentEventMsg{sess: s.ID, seq: s.streamSeq, ev: ev})
}

// While a turn runs the conversation ends with a line saying so — the status
// bar in the corner is not where you are looking.
func TestAWorkingLineSitsUnderTheConversation(t *testing.T) {
	a := testApp(t)
	s := busyTurn(t, a)
	_ = a.View()

	lines := strings.Split(strings.TrimRight(stripSGR(s.tl.Content()), "\n"), "\n")
	last := lines[len(lines)-1]
	for _, want := range []string{"working…", "0s", "esc to cancel"} {
		if !strings.Contains(last, want) {
			t.Fatalf("the working line is missing %q: %q", want, last)
		}
	}
	if !strings.Contains(stripSGR(s.tl.View()), "esc to cancel") {
		t.Fatal("and it should be on screen")
	}

	finish(t, a, s)
	_ = a.View()
	if strings.Contains(stripSGR(s.tl.Content()), "esc to cancel") {
		t.Fatal("an idle agent has nothing to say")
	}
}

// The verb is the last event that arrived, since the backends never announce
// what they are up to.
func TestTheWorkingLineSaysWhatItIsDoing(t *testing.T) {
	a := testApp(t)
	s := busyTurn(t, a)
	for _, c := range []struct {
		ev   agent.Event
		want string
	}{
		{agent.Event{Kind: agent.KindText, Thinking: true, Text: "hmm"}, "thinking…"},
		{agent.Event{Kind: agent.KindToolCall, Tool: &agent.ToolCall{Name: "Bash"}}, "Bash…"},
		{agent.Event{Kind: agent.KindToolOutput, Output: &agent.ToolOutput{Content: "ok"}}, "reading the result…"},
		{agent.Event{Kind: agent.KindText, Text: "here you go"}, "writing…"},
	} {
		feed(a, s, c.ev)
		_ = a.View()
		if !strings.Contains(stripSGR(s.tl.Content()), c.want) {
			t.Fatalf("after %v the line should say %q", c.ev.Kind, c.want)
		}
	}
	s.close()
}

// Tokens show what the backend reported writing, and only when it reported.
func TestTheWorkingLineCountsTokensOnlyWhenToldAny(t *testing.T) {
	a := testApp(t)
	s := busyTurn(t, a)
	_ = a.View()
	if strings.Contains(stripSGR(s.tl.Content()), "tokens") {
		t.Fatal("nothing has been reported yet, so nothing may be claimed")
	}
	feed(a, s, agent.Event{Kind: agent.KindText, Text: "a", OutTokens: 1234})
	_ = a.View()
	if !strings.Contains(stripSGR(s.tl.Content()), "↓ 1.2k tokens") {
		t.Fatalf("token count missing:\n%s", stripSGR(s.tl.Content()))
	}
	// A later event with no count of its own must not erase the one we have.
	feed(a, s, agent.Event{Kind: agent.KindToolOutput, Output: &agent.ToolOutput{Content: "x"}})
	_ = a.View()
	if !strings.Contains(stripSGR(s.tl.Content()), "1.2k tokens") {
		t.Fatal("the count went backwards")
	}
	s.close()
}

func TestShortCountReadsTheWayAPersonWouldSayIt(t *testing.T) {
	for _, c := range []struct {
		n    int64
		want string
	}{{0, "0"}, {812, "812"}, {1234, "1.2k"}, {9999, "10.0k"}, {34_500, "34k"}} {
		if got := shortCount(c.n); got != c.want {
			t.Fatalf("shortCount(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// The working line sits between the conversation and anything waiting, which
// is the order those things happen in — and the click targets have to know.
func TestTheWorkingLineDoesNotConfuseTheQueue(t *testing.T) {
	a := testApp(t)
	s := busyTurn(t, a)
	typeRunes(t, a, "afterwards")
	press(t, a, kmsg(tea.KeyEnter))
	_ = a.View()

	lines := strings.Split(strings.TrimRight(stripSGR(s.tl.Content()), "\n"), "\n")
	if n := len(lines); !strings.Contains(lines[n-2], "esc to cancel") ||
		!strings.Contains(lines[n-1], "afterwards") {
		t.Fatalf("want working then waiting, got:\n%s", strings.Join(lines[n-2:], "\n"))
	}
	clickQueued(a, 0) // still finds the waiting message, one row further down
	if len(s.Queued()) != 0 || a.in.Value() != "afterwards" {
		t.Fatalf("queue = %q, draft = %q", s.Queued(), a.in.Value())
	}
	s.close()
}

// Each agent's own line, so a background agent still says what it is doing.
func TestEachAgentHasItsOwnWorkingLine(t *testing.T) {
	a := testApp(t)
	first := busyTurn(t, a)
	second := a.addSession(first.Backend, t.TempDir())
	a.selectSession(1)
	_ = a.View()

	if !strings.Contains(stripSGR(first.tl.Content()), "esc to cancel") {
		t.Fatal("the busy agent should still show its line")
	}
	if strings.Contains(stripSGR(second.tl.Content()), "esc to cancel") {
		t.Fatal("the idle one should not")
	}
	first.close()
	second.close()
}
