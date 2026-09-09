package agent

import (
	"encoding/json"
	"testing"
)

// The wire shape is the CLI's own: an envelope naming the request, the
// subtype and fields inside it. Measured from its bundle, not guessed.
func TestControlRequestWireShape(t *testing.T) {
	b, err := controlRequest("crema-7", "stop_task", map[string]any{"task_id": "aae4"})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Type      string         `json:"type"`
		RequestID string         `json:"request_id"`
		Request   map[string]any `json:"request"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != "control_request" || got.RequestID != "crema-7" {
		t.Fatalf("envelope = %+v", got)
	}
	if got.Request["subtype"] != "stop_task" || got.Request["task_id"] != "aae4" {
		t.Fatalf("request = %v", got.Request)
	}

	// The default model is asked for as null, which is what the CLI's
	// handler accepts as "your own default".
	b, _ = controlRequest("crema-8", "set_model", map[string]any{"model": nil})
	if string(b) != `{"request":{"model":null,"subtype":"set_model"},"request_id":"crema-8","type":"control_request"}` {
		t.Fatalf("set_model default = %s", b)
	}
}

// background_tasks_changed is the CLI's whole list of background work, and
// an empty one is the important case: everything has ended.
func TestTheBackgroundListBecomesAnEvent(t *testing.T) {
	p := &ClaudeParser{}
	evs := p.ParseLine([]byte(`{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"b4z4j7yds","task_type":"local_bash","description":"ping -n 4 127.0.0.1"}]}`))
	if len(evs) != 1 || evs[0].Kind != KindTaskList || len(evs[0].Tasks) != 1 {
		t.Fatalf("events = %+v", evs)
	}
	if x := evs[0].Tasks[0]; x.ID != "b4z4j7yds" || x.Type != "local_bash" || x.Status != "running" || !x.Backgrounded {
		t.Fatalf("task = %+v", x)
	}
	empty := p.ParseLine([]byte(`{"type":"system","subtype":"background_tasks_changed","tasks":[]}`))
	if len(empty) != 1 || empty[0].Kind != KindTaskList || len(empty[0].Tasks) != 0 {
		t.Fatalf("events = %+v", empty)
	}
}

// The CLI's answer becomes a KindControl event that names the request it
// answers, so the caller can match it to what it asked.
func TestControlResponsesBecomeEvents(t *testing.T) {
	p := &ClaudeParser{}
	ok := p.ParseLine([]byte(`{"type":"control_response","response":{"subtype":"success","request_id":"crema-1","response":{"stopped":true}}}`))
	if len(ok) != 1 || ok[0].Kind != KindControl {
		t.Fatalf("events = %+v", ok)
	}
	if r := ok[0].Control; r.RequestID != "crema-1" || !r.OK || r.Err != "" || string(r.Response) != `{"stopped":true}` {
		t.Fatalf("reply = %+v", r)
	}

	bad := p.ParseLine([]byte(`{"type":"control_response","response":{"subtype":"error","request_id":"crema-2","error":"stop_task: task_id must be a string"}}`))
	if r := bad[0].Control; r.RequestID != "crema-2" || r.OK || r.Err != "stop_task: task_id must be a string" {
		t.Fatalf("reply = %+v", r)
	}

	// A malformed one is skipped, never fatal — like every other line.
	if got := p.ParseLine([]byte(`{"type":"control_response"}`)); len(got) != 0 {
		t.Fatalf("events = %+v", got)
	}
}
