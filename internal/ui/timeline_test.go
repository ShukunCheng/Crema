package ui

import (
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

func TestAppendEventMapsEveryKind(t *testing.T) {
	tl := NewTimeline(60, 10)
	tl.AppendEvent(agent.Event{Kind: agent.KindText, Text: "hello"})
	tl.AppendEvent(agent.Event{Kind: agent.KindText, Thinking: true, Text: "hmm"})
	tl.AppendEvent(agent.Event{Kind: agent.KindToolCall, Tool: &agent.ToolCall{Name: "Bash", Input: "ls -la"}})
	tl.AppendEvent(agent.Event{Kind: agent.KindToolOutput, Output: &agent.ToolOutput{Content: "a.txt"}})
	tl.AppendEvent(agent.Event{Kind: agent.KindError, Text: "went wrong"})
	tl.AppendEvent(agent.Event{Kind: agent.KindTurnEnd, Result: &agent.TurnResult{DurationMS: 1000}})
	if tl.Len() != 6 {
		t.Fatalf("Len = %d, want 6", tl.Len())
	}
	c := tl.Content()
	for _, want := range []string{"hello", "hmm", "Bash", "ls -la", "a.txt", "went wrong"} {
		if !strings.Contains(c, want) {
			t.Fatalf("content missing %q:\n%s", want, c)
		}
	}
}

func TestContentRerendersOnWidthChange(t *testing.T) {
	tl := NewTimeline(80, 10)
	long := strings.Repeat("word ", 40)
	tl.Append(Block{Kind: BlockAssistant, Text: long})
	wide := len(strings.Split(tl.Content(), "\n"))
	tl.SetSize(30, 10)
	narrow := len(strings.Split(tl.Content(), "\n"))
	if narrow <= wide {
		t.Fatalf("narrower width must wrap into more lines: wide=%d narrow=%d", wide, narrow)
	}
}

func TestFollowStaysAtBottomUntilUserScrollsUp(t *testing.T) {
	tl := NewTimeline(40, 5)
	for i := 0; i < 40; i++ {
		tl.Append(Block{Kind: BlockAssistant, Text: "line"})
	}
	if !tl.Following() {
		t.Fatal("should auto-follow new output")
	}
	tl.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if tl.Following() {
		t.Fatal("scrolling up must stop follow mode")
	}
	tl.Append(Block{Kind: BlockAssistant, Text: "more"})
	if tl.Following() {
		t.Fatal("appending must not yank a scrolled-back user to the bottom")
	}
	tl.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if !tl.Following() {
		t.Fatal("End must resume follow mode")
	}
}

func TestViewFitsRequestedHeight(t *testing.T) {
	tl := NewTimeline(40, 6)
	for i := 0; i < 30; i++ {
		tl.Append(Block{Kind: BlockAssistant, Text: "x"})
	}
	if got := len(strings.Split(tl.View(), "\n")); got != 6 {
		t.Fatalf("View height = %d, want 6", got)
	}
}
