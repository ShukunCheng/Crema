package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
)

type stubAgent struct {
	name string
	err  error
}

func (s stubAgent) Name() string     { return s.name }
func (s stubAgent) Label() string    { return strings.ToUpper(s.name) }
func (s stubAgent) Available() error { return s.err }
func (s stubAgent) Run(context.Context, agent.RunOptions) (<-chan agent.Event, error) {
	return nil, errors.New("stub")
}

func TestPickDefaultsToFirstAvailable(t *testing.T) {
	reg := &agent.Registry{Agents: []agent.Agent{
		stubAgent{name: "claude", err: errors.New("missing")},
		stubAgent{name: "codex"},
	}}
	got, err := pick(reg, "")
	if err != nil || got.Name() != "codex" {
		t.Fatalf("pick = %v, %v", got, err)
	}
}

func TestPickNamedUnavailableExplainsWhy(t *testing.T) {
	reg := &agent.Registry{Agents: []agent.Agent{stubAgent{name: "claude", err: errors.New("claude CLI not found")}}}
	if _, err := pick(reg, "claude"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestPickUnknownNameListsChoices(t *testing.T) {
	reg := &agent.Registry{Agents: []agent.Agent{stubAgent{name: "claude"}}}
	_, err := pick(reg, "gemini")
	if err == nil || !strings.Contains(err.Error(), "claude") {
		t.Fatalf("err = %v", err)
	}
}

func TestDoctorReportFlagsMissingAgents(t *testing.T) {
	reg := &agent.Registry{Agents: []agent.Agent{
		stubAgent{name: "claude", err: errors.New("claude CLI not found")},
		stubAgent{name: "mock"},
	}}
	out, ok := doctorReport(reg, t.TempDir())
	if ok {
		t.Fatal("mock alone must not count as a usable setup")
	}
	for _, want := range []string{"claude CLI not found", "✓ MOCK", "not a git repository"} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, out)
		}
	}
}

func TestDoctorReportOKWithRealAgent(t *testing.T) {
	reg := &agent.Registry{Agents: []agent.Agent{stubAgent{name: "codex"}}}
	if _, ok := doctorReport(reg, t.TempDir()); !ok {
		t.Fatal("an available real agent must report ok")
	}
}
