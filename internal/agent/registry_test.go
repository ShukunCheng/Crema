package agent

import (
	"context"
	"errors"
	"testing"
)

type fakeAgent struct {
	name string
	err  error
}

func (f fakeAgent) Name() string     { return f.name }
func (f fakeAgent) Label() string    { return f.name }
func (f fakeAgent) Available() error { return f.err }
func (f fakeAgent) Run(context.Context, RunOptions) (<-chan Event, error) {
	return nil, errors.New("not implemented")
}

func TestFirstAvailableSkipsUnavailable(t *testing.T) {
	r := &Registry{Agents: []Agent{
		fakeAgent{name: "claude", err: errors.New("not installed")},
		fakeAgent{name: "codex"},
	}}
	if got := r.FirstAvailable().Name(); got != "codex" {
		t.Fatalf("FirstAvailable = %q, want codex", got)
	}
}

func TestNextCyclesAndSkipsUnavailable(t *testing.T) {
	r := &Registry{Agents: []Agent{
		fakeAgent{name: "claude"},
		fakeAgent{name: "codex", err: errors.New("nope")},
		fakeAgent{name: "mock"},
	}}
	if got := r.Next("claude").Name(); got != "mock" {
		t.Fatalf("Next(claude) = %q, want mock (codex unavailable)", got)
	}
	if got := r.Next("mock").Name(); got != "claude" {
		t.Fatalf("Next(mock) = %q, want claude (wraps)", got)
	}
}

func TestFirstAvailableFallsBackToNilWhenEmpty(t *testing.T) {
	r := &Registry{}
	if r.FirstAvailable() != nil {
		t.Fatal("empty registry must return nil")
	}
	if r.Next("anything") != nil {
		t.Fatal("empty registry must have no next agent")
	}
}
