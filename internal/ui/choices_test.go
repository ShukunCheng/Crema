package ui

import (
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

func TestParseChoicesReadsAQuestionsOptions(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		{"numbered", "Which theme should Crema default to?\n\n1. dark\n2. light\n3. follow the terminal",
			[]string{"dark", "light", "follow the terminal"}},
		{"parenthesised", "What next?\n1) ship it\n2) keep going",
			[]string{"ship it", "keep going"}},
		{"dashes", "Which one do you want?\n- rename it\n- leave it",
			[]string{"rename it", "leave it"}},
		{"lettered", "Pick one — which suits you?\na) fast\nb) careful",
			[]string{"fast", "careful"}},
		{"markdown stripped", "Which?\n1. **dark** — the current default\n2. `light`",
			[]string{"dark — the current default", "light"}},
		{"trailing blank lines", "Which?\n1. one\n2. two\n\n",
			[]string{"one", "two"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseChoices(c.text)
			if len(got) != len(c.want) {
				t.Fatalf("got %q, want %q", got, c.want)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Fatalf("option %d = %q, want %q", i, got[i], c.want[i])
				}
			}
		})
	}
}

// The picker must be hard to trigger: an ordinary list is not a question.
func TestParseChoicesIgnoresListsThatAreNotQuestions(t *testing.T) {
	cases := []struct{ name, text string }{
		{"no question mark", "Here is what I changed:\n1. renamed the type\n2. added a test"},
		{"only one option", "Shall I?\n1. yes"},
		{"prose after the list", "Which?\n1. one\n2. two\n\nI'll start on the first one."},
		{"list in the middle", "Which?\n1. one\n2. two\nand then some more prose\nabout it"},
		{"plain prose", "I renamed the type. Does that look right to you?"},
		{"empty", ""},
		{"too many", "Which?\n" + strings.Repeat("- x\n", maxChoices+1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ParseChoices(c.text); got != nil {
				t.Fatalf("expected no picker, got %q", got)
			}
		})
	}
}

// endTurnWith finishes a turn whose last words are text, the way a real
// stream does.
func endTurnWith(t *testing.T, a *App, text string) {
	t.Helper()
	s := a.cur()
	s.streamSeq++
	seq := s.streamSeq
	s.busy = true
	a.Update(agentEventMsg{sess: s.ID, seq: seq, ev: agent.Event{Kind: agent.KindText, Text: text}})
	a.Update(agentEventMsg{sess: s.ID, seq: seq,
		ev: agent.Event{Kind: agent.KindTurnEnd, Result: &agent.TurnResult{}}})
	a.Update(streamClosedMsg{sess: s.ID, seq: seq})
	_ = a.View()
}

func TestAgentsQuestionBecomesAPicker(t *testing.T) {
	a := testApp(t)
	endTurnWith(t, a, "Which theme should Crema default to?\n\n1. dark\n2. light\n3. follow the terminal")

	if a.choices == nil {
		t.Fatal("the agent asked a question with options and nothing was offered")
	}
	if !strings.Contains(stripSGR(a.View()), "follow the terminal") {
		t.Fatal("the options are not on screen")
	}

	press(t, a, kmsg(tea.KeyDown))
	press(t, a, kmsg(tea.KeyEnter))
	if a.choices != nil {
		t.Fatal("answering closes the picker")
	}
	if !a.cur().busy {
		t.Fatal("the answer must go back as the next turn")
	}
	// The answer reads like something the user typed.
	last := a.cur().tl.blocks[len(a.cur().tl.blocks)-1]
	if last.Kind != BlockUser || last.Text != "light" {
		t.Fatalf("sent %+v, want the chosen option as a user message", last)
	}
	a.cur().close()
}

// Answering in your own words always wins: the first character dismisses the
// picker and lands in the draft.
func TestTypingDismissesTheQuestion(t *testing.T) {
	a := testApp(t)
	endTurnWith(t, a, "Which one?\n1. dark\n2. light")
	if a.choices == nil {
		t.Fatal("no picker to dismiss")
	}
	typeRunes(t, a, "neither, use")
	if a.choices != nil {
		t.Fatal("typing must dismiss the picker")
	}
	if got := a.in.Value(); got != "neither, use" {
		t.Fatalf("draft = %q, want every character kept", got)
	}
	if a.cur().busy {
		t.Fatal("typing must not send anything")
	}
}

func TestEscDismissesTheQuestion(t *testing.T) {
	a := testApp(t)
	endTurnWith(t, a, "Which one?\n1. dark\n2. light")
	press(t, a, kmsg(tea.KeyEsc))
	if a.choices != nil {
		t.Fatal("esc must dismiss the picker")
	}
	if a.cur().busy {
		t.Fatal("esc must not send anything")
	}
}

// A draft in progress outranks a suggestion, and a question from an agent you
// are not looking at must not take the keyboard.
func TestNoPickerOverADraftOrFromABackgroundAgent(t *testing.T) {
	a := testApp(t)
	typeRunes(t, a, "already writing")
	endTurnWith(t, a, "Which one?\n1. dark\n2. light")
	if a.choices != nil {
		t.Fatal("a draft in progress must not be interrupted")
	}

	b := testApp(t)
	background := b.cur()
	b.addSession(fastMock(), t.TempDir()).introduce() // focus moves to the new agent
	background.streamSeq++
	b.Update(agentEventMsg{sess: background.ID, seq: background.streamSeq,
		ev: agent.Event{Kind: agent.KindText, Text: "Which one?\n1. dark\n2. light"}})
	b.Update(agentEventMsg{sess: background.ID, seq: background.streamSeq,
		ev: agent.Event{Kind: agent.KindTurnEnd, Result: &agent.TurnResult{}}})
	b.Update(streamClosedMsg{sess: background.ID, seq: background.streamSeq})
	if b.choices != nil {
		t.Fatal("an agent in the background must not take the keyboard")
	}
}
