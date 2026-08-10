package agent

import (
	"strings"
	"testing"
)

func TestCodexRealErrorCapture(t *testing.T) {
	p := &CodexParser{}
	evs := parseFixture(t, p, "testdata/codex_error.jsonl")
	if len(evs) != 2 {
		t.Fatalf("events: %+v", evs)
	}
	if evs[0].Kind != KindError || !strings.Contains(evs[0].Text, "gpt-5.2-codex") {
		t.Fatalf("error event: %+v", evs[0])
	}
	end := evs[1]
	if end.Kind != KindTurnEnd || end.Result.Err == "" ||
		end.Result.SessionID != "019feacf-f403-7f90-a8d0-d5ca79578c8c" {
		t.Fatalf("turn end: %+v", end)
	}
}

func TestCodexNewSchemaSuccess(t *testing.T) {
	p := &CodexParser{}
	evs := parseFixture(t, p, "testdata/codex_new.jsonl")
	want := []Kind{KindText, KindToolCall, KindToolOutput, KindText, KindToolCall, KindToolOutput, KindTurnEnd}
	got := kinds(evs)
	if len(got) != len(want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", got, want)
		}
	}
	if !evs[0].Thinking {
		t.Fatal("reasoning must map to thinking text")
	}
	if evs[1].Tool.Name != "shell" || evs[1].Tool.Input != "ls -la" {
		t.Fatalf("tool call: %+v", evs[1].Tool)
	}
	if !strings.Contains(evs[2].Output.Content, "a.txt") || evs[2].Output.ToolID != "item_1" {
		t.Fatalf("tool output: %+v", evs[2].Output)
	}
	end := evs[len(evs)-1].Result
	if end.InputTokens != 1234 || end.OutputTokens != 45 || end.SessionID == "" {
		t.Fatalf("turn end: %+v", end)
	}
}

func TestCodexLegacySchema(t *testing.T) {
	p := &CodexParser{}
	evs := parseFixture(t, p, "testdata/codex_legacy.jsonl")
	want := []Kind{KindText, KindText, KindToolCall, KindToolOutput, KindTurnEnd}
	got := kinds(evs)
	if len(got) != len(want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	if !evs[0].Thinking || evs[1].Text != "Hello from legacy codex" {
		t.Fatalf("texts: %+v", evs[:2])
	}
	if evs[2].Tool.Input != "bash -lc ls" {
		t.Fatalf("legacy command join: %q", evs[2].Tool.Input)
	}
	if evs[len(evs)-1].Result.InputTokens != 100 {
		t.Fatalf("token_count must flow into TurnEnd: %+v", evs[len(evs)-1].Result)
	}
}

func TestCodexCommandAsArgvArrayStillParses(t *testing.T) {
	// Schema-drift guard: codex has shipped `command` as both a string and an array.
	p := &CodexParser{}
	evs := p.ParseLine([]byte(`{"type":"item.started","item":{"id":"i9","item_type":"command_execution","command":["bash","-lc","echo hi"]}}`))
	if len(evs) != 1 || evs[0].Tool.Input != "bash -lc echo hi" {
		t.Fatalf("argv-form command: %+v", evs)
	}
}

func TestCodexArgs(t *testing.T) {
	c := NewCodex()
	got := c.args(RunOptions{Prompt: "do it"})
	want := []string{"exec", "--json", "--full-auto", "do it"}
	if len(got) != len(want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %v, want %v", got, want)
		}
	}
	res := c.args(RunOptions{Prompt: "more", SessionID: "tid-9"})
	wantRes := []string{"exec", "resume", "tid-9", "--json", "--full-auto", "more"}
	if len(res) != len(wantRes) {
		t.Fatalf("resume args = %v, want %v", res, wantRes)
	}
	for i := range wantRes {
		if res[i] != wantRes[i] {
			t.Fatalf("resume args = %v, want %v", res, wantRes)
		}
	}
}
