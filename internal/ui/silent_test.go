package ui

import (
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
)

// A turn that ends cleanly having said nothing must say so. Measured in the
// wild: a resume of a session that died mid-tool-call came back "success" in
// 1.6 seconds with zero tokens, and crema showed silence.
func TestASilentTurnIsCalledOut(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	s.tl.Append(Block{Kind: BlockUser, Text: "continue"})
	s.busy, s.turnEvents = true, 0
	s.noteResult(&agent.TurnResult{DurationMS: 1593})
	s.endTurn()
	got := lastBlock(s)
	if !strings.Contains(got, "having produced nothing") {
		t.Fatalf("the silence should be named: %q", got)
	}
	if bs := s.tl.Blocks(); bs[len(bs)-1].Kind != BlockError {
		t.Fatal("and named loudly, as an error")
	}
}

// A turn that answered, was canceled, or already failed is not silent.
func TestOrdinaryEndsGetNoSilenceNotice(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	for _, c := range []struct {
		name   string
		events int
		res    *agent.TurnResult
	}{
		{"it answered", 3, &agent.TurnResult{DurationMS: 900}},
		{"it was canceled", 0, &agent.TurnResult{Canceled: true}},
		{"it already failed", 0, &agent.TurnResult{Err: "boom"}},
		{"the stream just died", 0, nil},
	} {
		s.tl.Append(Block{Kind: BlockUser, Text: c.name})
		s.busy, s.turnEvents = true, c.events
		s.noteResult(c.res)
		s.endTurn()
		if got := lastBlock(s); strings.Contains(got, "having produced nothing") {
			t.Fatalf("%s: no notice belongs here, got %q", c.name, got)
		}
	}
}
