package ui

import (
	"strings"
	"testing"
)

// An answer taller than the pane is shown from its first line: the end of a
// long answer is its least useful part.
func TestALongAnswerIsShownFromItsStart(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	for i := 0; i < 60; i++ {
		s.tl.Append(Block{Kind: BlockAssistant, Text: "an earlier line"})
	}
	send(t, a, "/help")
	view := stripSGR(s.tl.View())
	if !strings.Contains(view, "crema's own commands") {
		t.Fatalf("a long answer should start at its start:\n%s", view)
	}
	// A short answer still lands at the end, where new output appears.
	send(t, a, "/cost")
	if !strings.Contains(stripSGR(s.tl.View()), "spent on this agent") {
		t.Fatal("a short answer belongs at the end, in view")
	}
}
