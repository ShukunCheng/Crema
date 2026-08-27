package agent

import (
	"bufio"
	"fmt"
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
	if r.ContextTokens != 2+13537+20326 {
		t.Fatalf("context should be input+cache reads+cache writes, got %d", r.ContextTokens)
	}
	if r.ContextWindow != 1000000 {
		t.Fatalf("context window should come from modelUsage, got %d", r.ContextWindow)
	}
	if r.RateLimit == nil {
		t.Fatal("the rate_limit_event should be attached to the turn result")
	}
	if r.RateLimit.Label() != "5h" || r.RateLimit.Utilization != 0.97 {
		t.Fatalf("rate limit: %+v", r.RateLimit)
	}
	if r.RateLimit.ResetsAt.Unix() != 1786438800 {
		t.Fatalf("reset time not parsed: %v", r.RateLimit.ResetsAt)
	}
}

func TestClaudeReportsNoUsageWhenTheStreamHasNone(t *testing.T) {
	// codex-style minimal result: no modelUsage, no rate_limit_event
	p := &ClaudeParser{}
	evs := p.ParseLine([]byte(`{"type":"result","subtype":"success","duration_ms":10,"session_id":"s"}`))
	r := evs[0].Result
	if r.ContextWindow != 0 || r.RateLimit != nil {
		t.Fatalf("absent usage must stay absent, not be invented: %+v", r)
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

// A turn is one bill but many API calls, and each call re-reads the whole
// conversation. The result line's usage adds them all up, so using it as the
// context size counts the conversation once per call — measured against CLI
// 2.1.229, a three-call turn read 41,922 tokens and billed 73,420. What fills
// the window is the last call's.
func TestContextIsTheLastCallNotTheWholeBill(t *testing.T) {
	p := &ClaudeParser{}
	msg := func(in, read, write int) string {
		return fmt.Sprintf(`{"type":"assistant","message":{"content":[{"type":"text","text":"x"}],`+
			`"usage":{"input_tokens":%d,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d}},`+
			`"session_id":"s"}`, in, read, write)
	}
	p.ParseLine([]byte(msg(10, 26651, 4837))) // 31,498
	p.ParseLine([]byte(msg(8, 31488, 10426))) // 41,922 — the state at the end
	evs := p.ParseLine([]byte(`{"type":"result","subtype":"success","session_id":"s",` +
		`"usage":{"input_tokens":18,"cache_read_input_tokens":58139,"cache_creation_input_tokens":15263},` +
		`"modelUsage":{"m":{"contextWindow":200000}}}`))
	got := evs[0].Result.ContextTokens
	if got != 8+31488+10426 {
		t.Fatalf("ContextTokens = %d, want the last call's %d", got, 8+31488+10426)
	}
	if got >= 18+58139+15263 {
		t.Fatal("the turn's whole bill was used as the context size")
	}
}

// Claude Code 2.1.229 reports the window and its reset but no percentage.
// Zero would be a lie, so the share is marked unknown and the reset kept.
func TestARateLimitWithoutAPercentageIsNotZeroPercent(t *testing.T) {
	p := &ClaudeParser{}
	p.ParseLine([]byte(`{"type":"rate_limit_event","session_id":"s","rate_limit_info":` +
		`{"status":"allowed","resetsAt":1786710600,"rateLimitType":"five_hour","isUsingOverage":false}}`))
	evs := p.ParseLine([]byte(`{"type":"result","subtype":"success","session_id":"s"}`))
	rl := evs[0].Result.RateLimit
	if rl == nil {
		t.Fatal("the event should still reach the turn — its reset time is real")
	}
	if rl.Known {
		t.Fatalf("no utilization was reported, so none may be claimed: %+v", rl)
	}
	if rl.Label() != "5h" || rl.ResetsAt.Unix() != 1786710600 || rl.Status != "allowed" {
		t.Fatalf("rate limit: %+v", rl)
	}
}

// A result can end a do-nothing turn with an error subtype and is_error still
// false — seen live on a resume of a session that died mid-tool-call. The
// subtype is the only thing that says it went wrong, so it has to count.
func TestAnErrorSubtypeIsAnErrorEvenWhenIsErrorSaysNo(t *testing.T) {
	p := &ClaudeParser{}
	evs := p.ParseLine([]byte(`{"type":"result","subtype":"error_during_execution","is_error":false,"duration_ms":1593,"result":""}`))
	if len(evs) != 1 || evs[0].Kind != KindTurnEnd {
		t.Fatalf("events = %+v", evs)
	}
	if got := evs[0].Result.Err; got != "claude ended the turn with error during execution" {
		t.Fatalf("Err = %q", got)
	}
}

// And a plain success stays clean.
func TestASuccessResultCarriesNoError(t *testing.T) {
	p := &ClaudeParser{}
	evs := p.ParseLine([]byte(`{"type":"result","subtype":"success","is_error":false,"duration_ms":900,"result":"done"}`))
	if len(evs) != 1 || evs[0].Result.Err != "" {
		t.Fatalf("events = %+v", evs)
	}
}

// A subagent's work streams inline, tagged with the tool call that launched
// it. The lines here are from a real capture (haiku, 2.1.229).
func TestSubagentEventsCarryTheirParent(t *testing.T) {
	p := &ClaudeParser{}
	evs := p.ParseLine([]byte(`{"type":"assistant","parent_tool_use_id":"toolu_PARENT","message":{"id":"msg_1","content":[{"type":"tool_use","id":"toolu_child","name":"Bash","input":{"command":"echo sub-hello"}}],"usage":{"input_tokens":9,"output_tokens":50,"cache_read_input_tokens":14000}}}`))
	if len(evs) != 1 || evs[0].SubID != "toolu_PARENT" {
		t.Fatalf("events = %+v", evs)
	}
	if evs[0].OutTokens != 0 {
		t.Fatal("a subagent's output is not the main turn's running total")
	}
	if p.context != 0 {
		t.Fatalf("a subagent's usage must not become the conversation's context: %d", p.context)
	}
	main := p.ParseLine([]byte(`{"type":"assistant","message":{"id":"msg_2","content":[{"type":"text","text":"DONE"}],"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":30000}}}`))
	if main[0].SubID != "" || p.context != 30010 {
		t.Fatalf("the main conversation should count as before: sub=%q ctx=%d", main[0].SubID, p.context)
	}
}

// The task lifecycle lines become KindTask events, from the shapes a real run
// produced: started, a progress heartbeat, and the completion notification.
func TestTaskLifecycleLines(t *testing.T) {
	p := &ClaudeParser{}
	started := p.ParseLine([]byte(`{"type":"system","subtype":"task_started","task_id":"b4z4j7yds","tool_use_id":"toolu_1","description":"ping -n 4 127.0.0.1","is_backgrounded":true,"task_type":"local_bash"}`))
	if len(started) != 1 || started[0].Kind != KindTask {
		t.Fatalf("events = %+v", started)
	}
	if u := started[0].Task; u.ID != "b4z4j7yds" || u.Status != "running" || u.Type != "local_bash" || u.Desc != "ping -n 4 127.0.0.1" {
		t.Fatalf("started = %+v", u)
	}

	prog := p.ParseLine([]byte(`{"type":"system","subtype":"task_progress","task_id":"aae4","tool_use_id":"toolu_2","description":"Running Echo","subagent_type":"general-purpose","usage":{"total_tokens":15062,"tool_uses":1,"duration_ms":1894},"last_tool_name":"Bash"}`))
	if u := prog[0].Task; u.Status != "running" || u.Type != "general-purpose" || u.Tokens != 15062 || u.LastTool != "Bash" {
		t.Fatalf("progress = %+v", u)
	}

	upd := p.ParseLine([]byte(`{"type":"system","subtype":"task_updated","task_id":"aae4","patch":{"status":"completed","end_time":1787754586179}}`))
	if u := upd[0].Task; u.Status != "completed" || u.ID != "aae4" {
		t.Fatalf("updated = %+v", u)
	}

	note := p.ParseLine([]byte(`{"type":"system","subtype":"task_notification","task_id":"aae4","tool_use_id":"toolu_2","status":"completed","output_file":"C:/tmp/tasks/aae4.output","summary":"sub-hello"}`))
	if u := note[0].Task; u.Status != "completed" || u.OutputFile != "C:/tmp/tasks/aae4.output" || u.Summary != "sub-hello" {
		t.Fatalf("notification = %+v", u)
	}
}

// One run, two result lines — the second one's total_cost_usd is cumulative,
// so only the growth is new money. Both measured on a real async-subagent run.
func TestASecondResultReportsOnlyTheGrowth(t *testing.T) {
	p := &ClaudeParser{}
	one := p.ParseLine([]byte(`{"type":"result","subtype":"success","total_cost_usd":0.0650971,"duration_ms":18424,"result":"waiting"}`))
	two := p.ParseLine([]byte(`{"type":"result","subtype":"success","total_cost_usd":0.0700971,"duration_ms":3291,"result":"DONE"}`))
	if got := one[0].Result.CostUSD; got != 0.0650971 {
		t.Fatalf("first = %v", got)
	}
	if got := two[0].Result.CostUSD; got < 0.00499 || got > 0.00501 {
		t.Fatalf("second should be the delta: %v", got)
	}
}
