package agent

import (
	"bufio"
	"os"
	"testing"
)

func parseFixture(t *testing.T, p interface {
	ParseLine([]byte) []Event
}, path string) []Event {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var evs []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		evs = append(evs, p.ParseLine(sc.Bytes())...)
	}
	return evs
}

func kinds(evs []Event) []Kind {
	out := make([]Kind, len(evs))
	for i, e := range evs {
		out[i] = e.Kind
	}
	return out
}

func TestClaudeSmokeFixture(t *testing.T) {
	p := &ClaudeParser{}
	evs := parseFixture(t, p, "testdata/claude_smoke.jsonl")
	// the empty thinking block is dropped → text, turnend only
	want := []Kind{KindText, KindTurnEnd}
	if len(evs) != len(want) {
		t.Fatalf("got %d events %v, want %v", len(evs), kinds(evs), want)
	}
	if evs[0].Text != "OK" {
		t.Fatalf("text = %q", evs[0].Text)
	}
	r := evs[1].Result
	if r == nil || r.SessionID != "988f2fb1-09f3-40fc-b3ff-14edfeba92a4" ||
		r.CostUSD != 0.293986 || r.DurationMS != 7557 || r.Err != "" {
		t.Fatalf("bad TurnResult: %+v", r)
	}
	if p.SessionID() != "988f2fb1-09f3-40fc-b3ff-14edfeba92a4" {
		t.Fatalf("SessionID() = %q", p.SessionID())
	}
	if p.Skipped == 0 {
		t.Fatal("rate_limit_event should have been counted as skipped")
	}
}

func TestClaudeToolsFixture(t *testing.T) {
	p := &ClaudeParser{}
	evs := parseFixture(t, p, "testdata/claude_tools.jsonl")
	want := []Kind{KindText, KindToolCall, KindToolOutput, KindToolCall, KindToolOutput, KindToolOutput, KindTurnEnd}
	got := kinds(evs)
	if len(got) != len(want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", got, want)
		}
	}
	tc := evs[1].Tool
	if tc.ID != "toolu_01" || tc.Name != "Bash" || tc.Input == "" {
		t.Fatalf("tool call: %+v", tc)
	}
	if evs[4].Output.Content != "hi" {
		t.Fatalf("array-form tool_result content = %q", evs[4].Output.Content)
	}
	if !evs[5].Output.IsError || evs[5].Output.ToolID != "toolu_99" {
		t.Fatalf("error tool_result: %+v", evs[5].Output)
	}
}

func TestClaudeGarbageAndUnknownLinesAreSkipped(t *testing.T) {
	p := &ClaudeParser{}
	for _, line := range []string{"not json at all", `{"type":"brand_new_event_2027","x":1}`, "", "   "} {
		if evs := p.ParseLine([]byte(line)); len(evs) != 0 {
			t.Fatalf("line %q produced events %v", line, evs)
		}
	}
	if p.Skipped != 2 { // blank lines don't count
		t.Fatalf("Skipped = %d, want 2", p.Skipped)
	}
}
