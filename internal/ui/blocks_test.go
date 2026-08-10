package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii) // strip ANSI so assertions see plain text
	os.Exit(m.Run())
}

func TestRenderToolShowsNameAndFullInput(t *testing.T) {
	out := RenderTool("Bash", "go test ./...\ngo vet ./...", 60)
	if !strings.Contains(out, "Bash") {
		t.Fatalf("missing tool name:\n%s", out)
	}
	for _, want := range []string{"go test ./...", "go vet ./..."} {
		if !strings.Contains(out, want) {
			t.Fatalf("input line %q missing:\n%s", want, out)
		}
	}
	for _, ln := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if lipgloss.Width(ln) > 60 {
			t.Fatalf("line exceeds width 60 (%d): %q", lipgloss.Width(ln), ln)
		}
	}
}

func TestRenderToolOutputNeverSilentlyFolds(t *testing.T) {
	body := strings.Repeat("line\n", 50)
	out := RenderToolOutput(body, false, 60)
	if strings.Count(out, "line") != 50 {
		t.Fatalf("all 50 lines must render, got %d", strings.Count(out, "line"))
	}
	if strings.Contains(out, "truncated") {
		t.Fatal("must not claim truncation when nothing was truncated")
	}
}

func TestRenderToolOutputLabelsTruncation(t *testing.T) {
	body := strings.Repeat("x\n", MaxOutputLines+25) // trailing \n is trimmed → 4025 lines
	out := RenderToolOutput(body, false, 60)
	if !strings.Contains(out, "truncated") {
		t.Fatal("truncation must be announced")
	}
	if !strings.Contains(out, "+25 lines truncated") {
		t.Fatal("the truncation label must carry the exact dropped-line count")
	}
}

func TestTruncateCountsRemainder(t *testing.T) {
	s, n := Truncate("a\nb\nc\nd", 2)
	if s != "a\nb" || n != 2 {
		t.Fatalf("Truncate = %q, %d", s, n)
	}
	if s2, n2 := Truncate("a\nb", 5); s2 != "a\nb" || n2 != 0 {
		t.Fatalf("under cap must be untouched: %q, %d", s2, n2)
	}
}

func TestRenderStatsIncludesCostAndDuration(t *testing.T) {
	out := RenderStats(&agent.TurnResult{DurationMS: 7557, CostUSD: 0.293986, InputTokens: 2, OutputTokens: 58}, 60)
	for _, want := range []string{"7.6s", "$0.2940", "58"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stats missing %q:\n%s", want, out)
		}
	}
}

func TestRenderStatsOmitsZeroCost(t *testing.T) {
	out := RenderStats(&agent.TurnResult{DurationMS: 1000, OutputTokens: 5}, 60)
	if strings.Contains(out, "$") {
		t.Fatalf("codex reports no USD; must omit the cost segment:\n%s", out)
	}
}

func TestRenderErrorIsVisiblyMarked(t *testing.T) {
	out := RenderError("credential expired", 60)
	if !strings.Contains(out, "credential expired") || !strings.Contains(strings.ToLower(out), "error") {
		t.Fatalf("error block: %s", out)
	}
}

func TestRenderersHandleTinyWidth(t *testing.T) {
	for _, w := range []int{0, 1, 8} {
		_ = RenderUser("hello there friend", w)
		_ = RenderTool("Bash", "echo hi", w)
		_ = RenderToolOutput("some output", true, w)
		_ = RenderThinking("pondering", w)
	} // must not panic
}
