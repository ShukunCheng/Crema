package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeArgs(t *testing.T) {
	c := NewClaude()
	got := c.args(RunOptions{Prompt: "fix the bug", Permission: PermissionAcceptEdits})
	want := []string{"-p", "fix the bug", "--output-format", "stream-json", "--verbose", "--permission-mode", "acceptEdits"}
	if len(got) != len(want) {
		t.Fatalf("args = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %v, want %v", got, want)
		}
	}
}

func TestClaudeArgsPermissionAndModel(t *testing.T) {
	c := NewClaude()
	full := strings.Join(c.args(RunOptions{Prompt: "x", Permission: PermissionFull, Model: "opus"}), " ")
	if !strings.Contains(full, "--permission-mode bypassPermissions") {
		t.Fatalf("full access must map to bypassPermissions: %s", full)
	}
	if !strings.Contains(full, "--model opus") {
		t.Fatalf("model not passed: %s", full)
	}
	plan := strings.Join(c.args(RunOptions{Prompt: "x", Permission: PermissionPlan}), " ")
	if !strings.Contains(plan, "--permission-mode plan") {
		t.Fatalf("plan mode: %s", plan)
	}
	// the default mode leaves the CLI's own configuration alone
	def := strings.Join(c.args(RunOptions{Prompt: "x", Permission: PermissionDefault}), " ")
	if strings.Contains(def, "--permission-mode") || strings.Contains(def, "--model") {
		t.Fatalf("default must add no flags: %s", def)
	}
}

func TestClaudeArgsResume(t *testing.T) {
	c := NewClaude()
	got := c.args(RunOptions{Prompt: "continue", SessionID: "sid-1"})
	last2 := got[len(got)-2:]
	if last2[0] != "--resume" || last2[1] != "sid-1" {
		t.Fatalf("resume args missing: %v", got)
	}
}

func TestClaudeRunAgainstFakeBinary(t *testing.T) {
	src, err := os.ReadFile("testdata/claude_tools.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "f.jsonl")
	if err := os.WriteFile(fixture, src, 0o644); err != nil {
		t.Fatal(err)
	}
	c := NewClaude()
	c.bin = os.Args[0]
	c.extraEnv = fakeEnv("stream", "CREMA_FAKE_FIXTURE="+fixture)
	ch, err := c.Run(context.Background(), RunOptions{Prompt: "x", Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	evs := collect(t, ch)
	last := evs[len(evs)-1]
	if last.Kind != KindTurnEnd || last.Result.SessionID != "aaaa-1111" || last.Result.CostUSD != 0.05 {
		t.Fatalf("end: %+v", last)
	}
}

func TestClaudeUnavailableWhenBinaryMissing(t *testing.T) {
	c := NewClaude()
	c.bin = "definitely-not-a-real-binary-xyz"
	if c.Available() == nil {
		t.Fatal("want availability error")
	}
}

// Crema's claude runs carry the one-hour prompt-cache switch. Losing it costs
// real money: a headless run defaults to the five-minute lifetime, and an
// agent revisited a quarter-hour later re-writes most of its conversation.
func TestClaudeRunsAskForTheHourLongCache(t *testing.T) {
	c := NewClaude()
	var found bool
	for _, e := range c.extraEnv {
		found = found || e == "ENABLE_PROMPT_CACHING_1H=1"
	}
	if !found {
		t.Fatalf("extraEnv = %v, want the 1h cache switch", c.extraEnv)
	}
}
