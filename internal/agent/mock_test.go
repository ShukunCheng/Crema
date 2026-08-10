package agent

import (
	"context"
	"testing"
	"time"
)

func collect(t *testing.T, ch <-chan Event) []Event {
	t.Helper()
	var evs []Event
	for ev := range ch {
		evs = append(evs, ev)
	}
	return evs
}

func TestMockEmitsFullScriptEndingInTurnEnd(t *testing.T) {
	m := NewMock()
	m.StepDelay = time.Millisecond
	ch, err := m.Run(context.Background(), RunOptions{Prompt: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	evs := collect(t, ch)
	if len(evs) < 4 {
		t.Fatalf("want ≥4 events (text, toolcall, tooloutput, turnend), got %d", len(evs))
	}
	last := evs[len(evs)-1]
	if last.Kind != KindTurnEnd || last.Result == nil || last.Result.Err != "" {
		t.Fatalf("final event must be successful KindTurnEnd, got %+v", last)
	}
	var sawCall, sawOut bool
	for _, ev := range evs {
		sawCall = sawCall || ev.Kind == KindToolCall
		sawOut = sawOut || ev.Kind == KindToolOutput
	}
	if !sawCall || !sawOut {
		t.Fatal("script must include a tool call and its output")
	}
}

func TestMockCancelStillClosesWithCanceledTurnEnd(t *testing.T) {
	m := NewMock()
	m.StepDelay = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := m.Run(ctx, RunOptions{Prompt: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	<-ch // first event arrived; cancel mid-stream
	cancel()
	deadline := time.After(2 * time.Second)
	var last Event
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				if last.Kind != KindTurnEnd || last.Result == nil || !last.Result.Canceled {
					t.Fatalf("final event must be canceled TurnEnd, got %+v", last)
				}
				return
			}
			last = ev
		case <-deadline:
			t.Fatal("channel did not close after cancel")
		}
	}
}
