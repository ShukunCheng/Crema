package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
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
func (s stubAgent) Modes() []agent.PermissionMode {
	return []agent.PermissionMode{agent.PermissionDefault, agent.PermissionAcceptEdits}
}
func (s stubAgent) Models() []string                { return []string{agent.DefaultModel} }
func (s stubAgent) Commands(string) []agent.Command { return nil }
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

// The status-line bridge must never break the status line: whatever happens to
// the recording, the wrapped command runs and its output goes through.
func TestStatuslinePassesThroughWhateverHappens(t *testing.T) {
	for _, payload := range []string{
		`{"rate_limits":{"five_hour":{"used_percentage":87,"resets_at":4102444800}}}`,
		`{"model":{"id":"x"}}`,
		`not json at all`,
	} {
		out, code := runStatuslineWith(t, payload, "--then", `echo HUD`)
		if code != 0 {
			t.Fatalf("exit %d for %q", code, payload)
		}
		if strings.TrimSpace(out) != "HUD" {
			t.Fatalf("wrapped output = %q for %q", out, payload)
		}
	}
}

// And it records what it saw, where crema looks for it.
func TestStatuslineRecordsTheAllowance(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)         // Windows' UserConfigDir
	t.Setenv("XDG_CONFIG_HOME", dir) // and everyone else's
	runStatuslineWith(t, `{"rate_limits":{"five_hour":{"used_percentage":87,`+
		`"resets_at":4102444800}}}`)

	b, err := os.ReadFile(agent.UsagePath())
	if err != nil {
		t.Fatalf("nothing recorded: %v", err)
	}
	if !strings.Contains(string(b), `"utilization":0.87`) {
		t.Fatalf("recorded %s", b)
	}
}

// runStatuslineWith feeds a payload through the subcommand and captures what
// it printed.
func runStatuslineWith(t *testing.T, payload string, thenArgs ...string) (string, int) {
	t.Helper()
	// Never the real config directory: a test must not leave a fabricated
	// percentage behind for crema to show.
	cfg := t.TempDir()
	t.Setenv("APPDATA", cfg)         // Windows' UserConfigDir
	t.Setenv("XDG_CONFIG_HOME", cfg) // and everyone else's

	in, err := os.CreateTemp(t.TempDir(), "in")
	if err != nil {
		t.Fatal(err)
	}
	in.WriteString(payload)
	in.Seek(0, 0)
	out, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = in, out
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()

	code := runStatusline(thenArgs)

	os.Stdout = oldOut
	out.Seek(0, 0)
	b, _ := io.ReadAll(out)
	// Windows will not delete an open file, and the temp dir goes at cleanup.
	in.Close()
	out.Close()
	return string(b), code
}

// A status line is a shell one-liner with its own quoting — the one in the
// wild nests single quotes inside single quotes to feed awk. Escaping that
// into another command line is how you break someone's status bar, so the
// wrapped command comes from a file instead, byte for byte.
func TestTheWrappedCommandComesFromAFileUnmangled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hud.sh")
	// The exact shape that broke when it was escaped into an argument.
	inner := `bash -c 'awk -F/ '"'"'{ print $(NF-1) }'"'"' <<< a/b/c'`
	if err := os.WriteFile(path, []byte(inner+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readCommand(path); got != inner {
		t.Fatalf("readCommand mangled it:\n got %q\nwant %q", got, inner)
	}

	out, code := runStatuslineWith(t, `{"rate_limits":{"five_hour":`+
		`{"used_percentage":19,"resets_at":4102444800}}}`, "--then-file", path)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.TrimSpace(out) != "b" {
		t.Fatalf("the wrapped command printed %q, want the awk result", out)
	}
}

func TestAMissingWrappedCommandIsNotFatal(t *testing.T) {
	if got := readCommand(filepath.Join(t.TempDir(), "nope.sh")); got != "" {
		t.Fatalf("readCommand = %q", got)
	}
	// And the recording still happens, which is the half crema owns.
	_, code := runStatuslineWith(t, `{"rate_limits":{"five_hour":`+
		`{"used_percentage":5,"resets_at":4102444800}}}`,
		"--then-file", filepath.Join(t.TempDir(), "nope.sh"))
	if code != 0 {
		t.Fatalf("a missing file must not fail the status line: exit %d", code)
	}
}
