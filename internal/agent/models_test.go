package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeConfig points model discovery at a temporary home holding one
// ~/.claude.json, and clears the memo so each case is read fresh.
func fakeConfig(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	if body != "" {
		if err := os.WriteFile(filepath.Join(home, claudeConfigFile), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	prevHome, prevNow := homeDir, timeNow
	homeDir = func() (string, error) { return home, nil }
	modelsMu.Lock()
	modelsAt, modelsSeen = time.Time{}, nil
	modelsMu.Unlock()
	t.Cleanup(func() {
		homeDir, timeNow = prevHome, prevNow
		modelsMu.Lock()
		modelsAt, modelsSeen = time.Time{}, nil
		modelsMu.Unlock()
	})
}

// The aliases are always offered, whatever the CLI's cache says — it lists
// only the extra options, so trusting it wholesale would empty the picker.
func TestTheBuiltInAliasesAreAlwaysOffered(t *testing.T) {
	for _, cfg := range []string{"", `{}`, `{"additionalModelOptionsCache":[]}`, `not json at all`} {
		fakeConfig(t, cfg)
		got := NewClaude().Models()
		want := []string{DefaultModel, "opus", "fable", "sonnet", "haiku"}
		if len(got) != len(want) {
			t.Fatalf("config %q gave %q", cfg, got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("config %q gave %q", cfg, got)
			}
		}
	}
}

// A model no alias covers joins the list, with the CLI's own words under it.
// This is the Fable case: when it shipped, no name crema knew pointed at it.
func TestANewModelJoinsTheList(t *testing.T) {
	fakeConfig(t, `{"additionalModelOptionsCache":[
		{"value":"claude-fable-5[1m]","label":"Fable","description":"Fable 5 · Most capable for your hardest tasks"},
		{"value":"claude-mythos-1","label":"Mythos","description":"Mythos 1 · Something wholly new"}]}`)
	c := NewClaude()
	got := c.Models()
	if len(got) != 6 || got[5] != "claude-mythos-1" {
		t.Fatalf("models = %q, want the unknown one appended", got)
	}
	if d := c.DescribeModel("claude-mythos-1"); d != "Mythos 1 · Something wholly new" {
		t.Fatalf("DescribeModel = %q — the CLI's own words belong under it", d)
	}
	// Fable is already an alias, so the cached entry must not double it up.
	n := 0
	for _, m := range got {
		if m == "fable" || m == "claude-fable-5[1m]" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("fable appears %d times in %q", n, got)
	}
}

// The CLI's description wins over crema's built-in note: it names the
// generation, which crema cannot know, and crema's note is the fallback.
func TestTheCLIsOwnDescriptionWins(t *testing.T) {
	fakeConfig(t, `{"additionalModelOptionsCache":[
		{"value":"claude-fable-6","label":"Fable","description":"Fable 6 · the newest one"}]}`)
	c := NewClaude()
	if d := c.DescribeModel("fable"); d != "Fable 6 · the newest one" {
		t.Fatalf("DescribeModel(fable) = %q", d)
	}
	if d := c.DescribeModel("haiku"); d != modelNotes["haiku"] {
		t.Fatalf("DescribeModel(haiku) = %q, want the built-in note", d)
	}
	if d := c.DescribeModel(DefaultModel); d == "" {
		t.Fatal("default should still explain itself")
	}
}

// The notes name no generation — "Opus 5" here becomes a lie the day Opus 6
// ships, and the alias silently moves with the CLI. A context size is fine;
// a version number after the family name is not.
func TestTheBuiltInNotesNameNoGeneration(t *testing.T) {
	for alias, note := range modelNotes {
		lower := strings.ToLower(note)
		i := strings.Index(lower, alias)
		if i < 0 {
			t.Fatalf("%s: %q should at least name the family", alias, note)
		}
		rest := strings.TrimSpace(lower[i+len(alias):])
		if rest != "" && rest[0] >= '0' && rest[0] <= '9' {
			t.Fatalf("%s: %q pins a generation; the alias moves with the CLI", alias, note)
		}
	}
}
