package ui

import (
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

// Effort is the only lever on output tokens, and caching cannot touch those.
// It has to reach the command line, because that is the only way the CLI takes
// it — measured against the CLI's own bundle, which offers set_model and
// set_permission_mode over stdin and nothing for this.
func TestEffortReachesTheCommandLine(t *testing.T) {
	c := &agent.Claude{}
	for _, tc := range []struct {
		level string
		want  bool
	}{{"", false}, {"low", true}, {"xhigh", true}, {"max", true}} {
		args := agent.ClaudeArgsForTest(c, agent.RunOptions{Prompt: "hi", Effort: tc.level})
		got := false
		for i, a := range args {
			if a == "--effort" && i+1 < len(args) && args[i+1] == tc.level {
				got = true
			}
		}
		if got != tc.want {
			t.Errorf("effort %q: --effort on the command line = %v, want %v (args %v)",
				tc.level, got, tc.want, args)
		}
	}
}

// Setting it costs a process, because there is no control request for it — so
// crema says so rather than leaving it to be discovered in the bill.
func TestChangingEffortSaysItCostsAColdStart(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	send(t, a, "/effort xhigh")
	if s.Effort != "xhigh" {
		t.Fatalf("effort = %q", s.Effort)
	}
	got := lastBlock(s)
	if !strings.Contains(got, "xhigh") || !strings.Contains(got, "next message") {
		t.Fatalf("it should say what changed and when it applies: %q", got)
	}
	// Nothing was running, so nothing was closed and there is nothing to warn
	// about; the warning belongs to the case where a process was open.
	if strings.Contains(got, "new process") {
		t.Fatalf("no process was open, so there is no cold start to announce: %q", got)
	}
	send(t, a, "/effort default")
	if s.Effort != agent.DefaultEffort {
		t.Fatalf("/effort default should put it back, got %q", s.Effort)
	}
	send(t, a, "/effort sideways")
	if s.Effort != agent.DefaultEffort {
		t.Fatal("a level the backend does not have should be refused")
	}
	if !strings.Contains(a.note, "low") || !strings.Contains(a.note, "max") {
		t.Fatalf("and the note should say what the levels are: %q", a.note)
	}
}

// A live process is closed when the level changes, and the cost is stated.
func TestChangingEffortUnderALiveProcessWarns(t *testing.T) {
	_, s, _ := ctlApp(t)
	if s.conv == nil {
		t.Fatal("this test needs a held-open process")
	}
	s.SetEffort("low")
	if s.conv != nil {
		t.Fatal("the process should be closed: effort only travels on the command line")
	}
	if got := lastBlock(s); !strings.Contains(got, "new process") {
		t.Fatalf("the cold start should be announced: %q", got)
	}
	s.close()
}

// It is on the walk, between the permission mode and the running work, and it
// is remembered.
func TestEffortIsOnTheWalkAndIsRemembered(t *testing.T) {
	dir := t.TempDir()
	a := testApp(t)
	s := a.cur()
	s.Dir = dir
	a.resize(120, 26)

	c := walkTo(t, a, controlEffort)
	// Walking onto it shows it, even at the default, so there is something to
	// see where the highlight is.
	if !strings.Contains(stripSGR(a.View()), "effort default") {
		t.Fatalf("the hub should show the value the walk is on:\n%s", lastLines(stripSGR(a.View()), 3))
	}
	a.Update(kmsg(tea.KeyEnter)) // open the list
	if !c.Open() {
		t.Fatal("enter opens the levels")
	}
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5")}) // low..max are 2..6
	if s.Effort != "xhigh" {
		t.Fatalf("picking the fifth row should set xhigh, got %q", s.Effort)
	}
	if !strings.Contains(stripSGR(a.View()), "effort xhigh") {
		t.Fatal("and the hub should carry it once it is set")
	}

	if err := SaveState(a.StateSnapshot()); err != nil {
		t.Fatal(err)
	}
	b := testApp(t)
	b.sessions = nil
	b.RestoreSessions(LoadState())
	if got := b.cur().Effort; got != "xhigh" {
		t.Fatalf("the level should survive a restart, got %q", got)
	}
}

// A backend with no such setting is not offered a dial that turns nothing.
func TestABackendWithoutEffortIsNotOfferedIt(t *testing.T) {
	s := NewSession(1, plainBackend{}, t.TempDir())
	if len(s.efforts()) != 0 {
		t.Fatalf("efforts = %v", s.efforts())
	}
	c := NewControls(s)
	for _, chip := range c.chips {
		if chip.kind == controlEffort {
			t.Fatal("the walk should not stop on a setting this backend does not have")
		}
	}
	if got := effortStatus(s); got != "" {
		t.Fatalf("and the hub should say nothing about it, got %q", got)
	}
}

// plainBackend is the mock without the effort setting, which is what a backend
// that has never heard of one looks like.
type plainBackend struct{ agent.Agent }

func (plainBackend) Name() string     { return "plain" }
func (plainBackend) Label() string    { return "Plain" }
func (plainBackend) Available() error { return nil }
func (plainBackend) Models() []string { return []string{agent.DefaultModel} }
func (plainBackend) Modes() []agent.PermissionMode {
	return []agent.PermissionMode{agent.PermissionFull}
}
