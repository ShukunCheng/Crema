package agent

import (
	"context"
	"time"
)

// Mock is a scripted agent used for demos (crema --agent mock) and UI tests.
type Mock struct {
	// StepDelay is the pause between scripted events; tests shrink it.
	StepDelay time.Duration
}

func NewMock() *Mock { return &Mock{StepDelay: 400 * time.Millisecond} }

func (m *Mock) Name() string     { return "mock" }
func (m *Mock) Label() string    { return "Mock" }
func (m *Mock) Available() error { return nil }

func (m *Mock) Run(ctx context.Context, opts RunOptions) (<-chan Event, error) {
	ch := make(chan Event, 8)
	script := []Event{
		{Kind: KindText, Thinking: true, Text: "Planning: touch a file, then summarize."},
		{Kind: KindText, Text: "I'll create hello.txt to demonstrate the fully expanded tool view."},
		{Kind: KindToolCall, Tool: &ToolCall{ID: "m1", Name: "Bash", Input: "echo hello > hello.txt"}},
		{Kind: KindToolOutput, Output: &ToolOutput{ToolID: "m1", Content: "(no output, exit 0)"}},
		{Kind: KindToolCall, Tool: &ToolCall{ID: "m2", Name: "Bash", Input: "wc -c hello.txt"}},
		{Kind: KindToolOutput, Output: &ToolOutput{ToolID: "m2", Content: "6 hello.txt"}},
		{Kind: KindText, Text: "Done — hello.txt created. Check the diff panel on the right."},
	}
	go func() {
		defer close(ch)
		start := time.Now()
		for _, ev := range script {
			select {
			case <-ctx.Done():
				ch <- Event{Kind: KindTurnEnd, Result: &TurnResult{
					SessionID: "mock-session", Canceled: true,
					DurationMS: time.Since(start).Milliseconds(),
				}}
				return
			case <-time.After(m.StepDelay):
				ch <- ev
			}
		}
		ch <- Event{Kind: KindTurnEnd, Result: &TurnResult{
			SessionID: "mock-session", DurationMS: time.Since(start).Milliseconds(),
			InputTokens: 42, OutputTokens: 128,
		}}
	}()
	return ch, nil
}
