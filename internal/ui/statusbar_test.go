package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestRenderStatusIsExactlyOneLineOfWidth(t *testing.T) {
	s := StatusData{Agent: "Claude Code", Mode: "acceptEdits", Dir: "D:/Crema",
		Busy: true, Spin: "⠋", ElapsedSec: 12.5, Cost: 0.42, Adds: 10, Dels: 3}
	for _, w := range []int{80, 100, 40, 20} {
		out := RenderStatus(s, w)
		if strings.Contains(out, "\n") {
			t.Fatalf("status must be one line at w=%d", w)
		}
		if lipgloss.Width(out) != w {
			t.Fatalf("width = %d, want %d: %q", lipgloss.Width(out), w, out)
		}
	}
}

func TestRenderStatusShowsAgentAndPermissionMode(t *testing.T) {
	out := RenderStatus(StatusData{Agent: "Codex", Mode: "full-auto", Adds: 1}, 100)
	if !strings.Contains(out, "Codex") || !strings.Contains(out, "full-auto") {
		t.Fatalf("status: %q", out)
	}
}

func TestRenderStatusIdleHidesTimer(t *testing.T) {
	out := RenderStatus(StatusData{Agent: "Codex", Mode: "full-auto", ElapsedSec: 9}, 100)
	if strings.Contains(out, "9.0s") {
		t.Fatalf("idle status must not show a running timer: %q", out)
	}
}
