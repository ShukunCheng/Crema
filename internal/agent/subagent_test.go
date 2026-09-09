package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testdata/subagent_log.jsonl is a real transcript the CLI left behind for a
// subagent crema launched — the same run as claude_subagent.jsonl, whose
// task_started line carries the task id this file is named after.
func copyLog(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "agent-aae460da5608bf2ac.jsonl")
	b, err := os.ReadFile(filepath.Join("testdata", "subagent_log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := os.ReadFile(filepath.Join("testdata", "subagent_log.meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent-aae460da5608bf2ac.meta.json"), m, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// A subagent's own transcript reads as a conversation: the prompt it was
// given, what it thought, what it ran, and what it answered.
func TestASubagentTranscriptReadsAsAConversation(t *testing.T) {
	l := &SubagentLog{Path: copyLog(t, t.TempDir())}
	evs, err := l.Read()
	if err != nil {
		t.Fatal(err)
	}
	var kinds []Kind
	for _, e := range evs {
		kinds = append(kinds, e.Kind)
	}
	if len(evs) == 0 {
		t.Fatal("the transcript produced nothing")
	}
	if evs[0].Kind != KindUser || !strings.Contains(evs[0].Text, "echo") {
		t.Fatalf("the first thing in the file is the prompt: %+v", evs[0])
	}
	var tools, text int
	for _, e := range evs {
		switch e.Kind {
		case KindToolCall:
			tools++
			if e.Tool == nil || e.Tool.Name == "" {
				t.Fatalf("a tool call with no tool: %+v", e)
			}
		case KindText:
			text++
		}
	}
	if tools == 0 {
		t.Fatalf("the subagent ran a command; the transcript should say so: %v", kinds)
	}
	if text == 0 {
		t.Fatalf("and it answered: %v", kinds)
	}
}

// Reading it again returns only what has been written since — that is what
// makes it cheap enough to poll while the subagent is still going.
func TestATranscriptIsReadOnlyOnce(t *testing.T) {
	dir := t.TempDir()
	p := copyLog(t, dir)
	l := &SubagentLog{Path: p}
	first, err := l.Read()
	if err != nil {
		t.Fatal(err)
	}
	again, err := l.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("nothing was added, so nothing should come back: %d events", len(again))
	}
	// Now the subagent writes another line.
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"one more thing"}]}}` + "\n")
	f.Close()
	more, err := l.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(more) != 1 || more[0].Text != "one more thing" {
		t.Fatalf("only the new line should come back, got %+v", more)
	}
	if len(first) == 0 {
		t.Fatal("the first read should have returned the file")
	}
}

// The CLI writes a line at a time and crema reads whenever it likes, so a
// half-written line has to wait rather than be dropped.
func TestAHalfWrittenLineIsNotLost(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "agent-x.jsonl")
	whole := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"complete"}]}}` + "\n"
	if err := os.WriteFile(p, []byte(whole[:40]), 0o644); err != nil {
		t.Fatal(err)
	}
	l := &SubagentLog{Path: p}
	if evs, err := l.Read(); err != nil || len(evs) != 0 {
		t.Fatalf("half a line is not an event yet: %v %v", evs, err)
	}
	if err := os.WriteFile(p, []byte(whole), 0o644); err != nil {
		t.Fatal(err)
	}
	evs, err := l.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Text != "complete" {
		t.Fatalf("once it is finished it should arrive whole: %+v", evs)
	}
}

// The sidecar says what the subagent is, which is what the roster calls it.
func TestTheSidecarSaysWhatTheSubagentIs(t *testing.T) {
	dir := t.TempDir()
	copyLog(t, dir)
	home := t.TempDir()
	old := homeDir
	homeDir = func() (string, error) { return home, nil }
	defer func() { homeDir = old }()

	want := filepath.Join(home, ".claude", "projects", projectSlug(`D:\Crema`), "sess-1", "subagents")
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"agent-aae460da5608bf2ac.jsonl", "agent-aae460da5608bf2ac.meta.json"} {
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(want, n), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	l, err := OpenSubagentLog(`D:\Crema`, "sess-1", "aae460da5608bf2ac")
	if err != nil {
		t.Fatal(err)
	}
	if l.Meta.Type != "general-purpose" || l.Meta.Desc == "" {
		t.Fatalf("meta = %+v", l.Meta)
	}
	if _, err := OpenSubagentLog(`D:\Crema`, "sess-1", "never-existed"); err == nil {
		t.Fatal("a subagent with no file yet is not an error worth stopping for, but it is not a log either")
	}
}
