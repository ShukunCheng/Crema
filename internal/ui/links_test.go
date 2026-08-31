package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTrimURLDropsProsePunctuation(t *testing.T) {
	for in, want := range map[string]string{
		"https://x.test/y.":            "https://x.test/y",
		"https://x.test/y),":           "https://x.test/y",
		"https://x.test/a?b=1&c=2":     "https://x.test/a?b=1&c=2",
		"https://en.x.org/wiki/Go_(x)": "https://en.x.org/wiki/Go_(x)",
		"https://x.test/y]:":           "https://x.test/y",
	} {
		if got := trimURL(in); got != want {
			t.Fatalf("trimURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// A URL renders underlined — only the underline toggles, so the block's own
// colors survive on both sides of it.
func TestURLsRenderUnderlined(t *testing.T) {
	out := RenderAssistant("the run is at https://ci.test/run/5. done", 80)
	if !strings.Contains(out, "\x1b[4mhttps://ci.test/run/5\x1b[24m") {
		t.Fatalf("no underline around the URL:\n%q", out)
	}
	if strings.Contains(out, "\x1b[4mhttps://ci.test/run/5.") {
		t.Fatal("the sentence's dot is not part of the link")
	}
}

// Clicking a link opens it; clicking beside it does not.
func TestClickingALinkOpensIt(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	var opened string
	prev := openURL
	openURL = func(u string) error { opened = u; return nil }
	t.Cleanup(func() { openURL = prev })

	s.tl.Append(Block{Kind: BlockAssistant, Text: "see https://ci.test/run/5 for the logs"})
	_ = a.View()

	// Find where the link landed on screen.
	lines := strings.Split(stripSGR(s.tl.Content()), "\n")
	row, col := -1, -1
	for i, ln := range lines {
		if j := strings.Index(ln, "https://"); j >= 0 {
			row, col = i, j
			break
		}
	}
	if row < 0 {
		t.Fatalf("the link is not on screen:\n%s", strings.Join(lines, "\n"))
	}
	x := a.lay.SidebarW + col + 2
	y := row - s.tl.YOffset()
	a.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	a.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	if opened != "https://ci.test/run/5" {
		t.Fatalf("opened = %q", opened)
	}

	opened = ""
	a.Update(tea.MouseMsg{X: a.lay.SidebarW + 1, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	a.Update(tea.MouseMsg{X: a.lay.SidebarW + 1, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	if opened != "" {
		t.Fatalf("a click beside the link opened %q", opened)
	}
}

// A long URL wraps; clicking its first line opens the whole thing, resolved
// from the block's own text.
func TestAWrappedLinkOpensWhole(t *testing.T) {
	long := "https://github.com/Gomocha-FSP/fsp-horizon/actions/runs/32964431105/attempts/1?check_suite_focus=true"
	tl := NewTimeline(40, 20)
	tl.Append(Block{Kind: BlockAssistant, Text: "pipeline: " + long})
	lines := strings.Split(stripSGR(tl.Content()), "\n")
	row, col := -1, -1
	for i, ln := range lines {
		if j := strings.Index(ln, "https://"); j >= 0 {
			row, col = i, j
			break
		}
	}
	if got := tl.LinkAt(row, col+3); got != long {
		t.Fatalf("LinkAt = %q, want the whole URL", got)
	}
}

// Drag-selecting across a link still selects — only a motionless click opens.
func TestDraggingOverALinkSelectsInstead(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	var opened string
	prev := openURL
	openURL = func(u string) error { opened = u; return nil }
	t.Cleanup(func() { openURL = prev })
	s.tl.Append(Block{Kind: BlockAssistant, Text: "see https://ci.test/run/5 for the logs"})
	_ = a.View()
	y := 0
	for i, ln := range strings.Split(stripSGR(s.tl.Content()), "\n") {
		if strings.Contains(ln, "https://") {
			y = i - s.tl.YOffset()
		}
	}
	x := a.lay.SidebarW + 6
	a.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	a.Update(tea.MouseMsg{X: x + 12, Y: y, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	a.Update(tea.MouseMsg{X: x + 12, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	if opened != "" {
		t.Fatalf("a drag is a selection, not a click: opened %q", opened)
	}
}
