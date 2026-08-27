package ui

import (
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
)

// A new agent starts with full access. Headless there is no prompt to approve
// anything at, so a narrower mode doesn't ask — it fails the tool mid-turn.
func TestANewAgentStartsWithFullAccess(t *testing.T) {
	s := NewSession(1, agent.NewMock(), t.TempDir())
	defer s.close()
	if s.Permission != agent.PermissionFull {
		t.Fatalf("Permission = %q, want full access", s.Permission)
	}
	if !strings.Contains(s.modeNote(), "full access") {
		t.Fatalf("the banner should say so: %q", s.modeNote())
	}
	s.startTurn("hi", "hi")
	if got := s.lastOpts.Permission; got != agent.PermissionFull {
		t.Fatalf("the turn runs as %q", got)
	}
}

// Only ever a mode the backend actually has.
func TestTheDefaultIsAModeTheBackendHas(t *testing.T) {
	for _, b := range []agent.Agent{agent.NewMock(), agent.NewClaude(), agent.NewCodex(), noFull{}} {
		got := defaultMode(b)
		var ok bool
		for _, m := range b.Modes() {
			ok = ok || m == got
		}
		if !ok {
			t.Fatalf("%s got %q, which it does not support", b.Name(), got)
		}
	}
	if got := defaultMode(noFull{}); got != agent.PermissionAcceptEdits {
		t.Fatalf("without full access, take the next loosest: %q", got)
	}
}

// noFull is a backend that stops short of full access.
type noFull struct{ *agent.Mock }

func (noFull) Modes() []agent.PermissionMode {
	return []agent.PermissionMode{agent.PermissionDefault, agent.PermissionAcceptEdits}
}
