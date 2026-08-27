package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/charmbracelet/lipgloss"
)

func sidebarSessions(t *testing.T) []*Session {
	t.Helper()
	a := NewSession(1, agent.NewMock(), t.TempDir()+"/alpha")
	b := NewSession(2, agent.NewMock(), t.TempDir()+"/beta")
	b.busy = true
	b.turnStart = time.Now().Add(-12 * time.Second)
	return []*Session{a, b}
}

func TestSidebarListsAgentsWithStateAndNewRow(t *testing.T) {
	out := RenderSidebar(sidebarSessions(t), 0, noDrag, "⠋", SidebarWidth-2, 10)
	for _, want := range []string{"AGENTS", "alpha", "beta", "idle", NewAgentRow, "▸"} {
		if !strings.Contains(out, want) {
			t.Fatalf("sidebar missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "12s") {
		t.Fatalf("a running agent must show elapsed time:\n%s", out)
	}
	if !strings.Contains(out, "1 ") || !strings.Contains(out, "2 ") {
		t.Fatalf("rows should be numbered for alt+N jumping:\n%s", out)
	}
}

func TestSidebarMarksTheActiveAgentOnly(t *testing.T) {
	out := RenderSidebar(sidebarSessions(t), 1, noDrag, "⠋", SidebarWidth-2, 10)
	if strings.Count(out, "▸") != 1 {
		t.Fatalf("exactly one active marker expected:\n%s", out)
	}
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, "▸") && !strings.Contains(ln, "beta") {
			t.Fatalf("the marker is on the wrong row: %q", ln)
		}
	}
}

func TestSidebarIsExactlyTheRequestedSize(t *testing.T) {
	for _, dims := range [][2]int{{22, 10}, {22, 4}, {14, 8}, {40, 20}} {
		out := RenderSidebar(sidebarSessions(t), 0, noDrag, "⠋", dims[0], dims[1])
		lines := strings.Split(out, "\n")
		if len(lines) != dims[1] {
			t.Fatalf("w=%d h=%d: %d lines, want %d", dims[0], dims[1], len(lines), dims[1])
		}
		for _, ln := range lines {
			if lipgloss.Width(ln) > dims[0] {
				t.Fatalf("w=%d exceeded (%d): %q", dims[0], lipgloss.Width(ln), ln)
			}
		}
	}
}

func TestSidebarHandlesNoSessions(t *testing.T) {
	out := RenderSidebar(nil, 0, noDrag, "⠋", SidebarWidth-2, 6)
	if !strings.Contains(out, NewAgentRow) {
		t.Fatalf("the new-agent row must always be offered:\n%s", out)
	}
	if len(strings.Split(out, "\n")) != 6 {
		t.Fatal("height must hold with no sessions")
	}
}
