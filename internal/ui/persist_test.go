package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

// tempState points persistence at a throwaway file for one test.
func tempState(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "state.json")
	prev := statePathOverride
	statePathOverride = p
	t.Cleanup(func() { statePathOverride = prev })
	return p
}

func regWith(agents ...agent.Agent) *agent.Registry {
	return &agent.Registry{Agents: agents}
}

func TestSaveAndRestoreRebuildsEveryAgent(t *testing.T) {
	tempState(t)
	dirA, dirB := t.TempDir(), t.TempDir()
	mk := fastMock()
	reg := regWith(mk)

	a := NewApp(reg, mk, dirA)
	a.resize(140, 30)
	a.addSession(mk, dirB).introduce()
	a.sessions[0].agentSID = "sid-a"
	a.sessions[1].agentSID = "sid-b"
	a.sessions[0].cost = 1.25
	a.cur().tl.Append(Block{Kind: BlockAssistant, Text: "remember me"})
	a.persist()

	restored := NewAppRestored(reg, LoadState(), mk, dirA)
	if len(restored.sessions) != 2 {
		t.Fatalf("want 2 restored agents, got %d", len(restored.sessions))
	}
	if restored.sessions[0].Dir != dirA || restored.sessions[1].Dir != dirB {
		t.Fatalf("directories not restored: %q, %q",
			restored.sessions[0].Dir, restored.sessions[1].Dir)
	}
	if restored.sessions[0].agentSID != "sid-a" || restored.sessions[1].agentSID != "sid-b" {
		t.Fatal("backend session ids must survive so the agent keeps its context")
	}
	if restored.sessions[0].cost != 1.25 {
		t.Fatalf("cost not restored: %v", restored.sessions[0].cost)
	}
	if !strings.Contains(restored.sessions[1].tl.Content(), "remember me") {
		t.Fatal("the conversation must come back")
	}
	if !strings.Contains(restored.sessions[1].tl.Content(), "sid-b") {
		t.Fatal("a restored agent should say which session it is continuing")
	}
}

func TestRestoredAgentResumesItsBackendSession(t *testing.T) {
	tempState(t)
	dir := t.TempDir()
	mk := fastMock()
	reg := regWith(mk)

	a := NewApp(reg, mk, dir)
	a.resize(140, 30)
	a.cur().agentSID = "sid-keep"
	a.persist()

	b := NewAppRestored(reg, LoadState(), mk, dir)
	b.resize(140, 30)
	b.in.ta.SetValue("carry on")
	b.Update(kmsg(tea.KeyEnter))
	if got := b.cur().lastOpts.SessionID; got != "sid-keep" {
		t.Fatalf("restored agent must resume its session, asked for %q", got)
	}
}

func TestRestoreSkipsMissingDirectoriesWithAReason(t *testing.T) {
	tempState(t)
	gone := filepath.Join(t.TempDir(), "deleted")
	if err := os.MkdirAll(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := t.TempDir()
	mk := fastMock()
	reg := regWith(mk)

	a := NewApp(reg, mk, keep)
	a.resize(140, 30)
	a.addSession(mk, gone).introduce()
	a.persist()
	os.RemoveAll(gone) // the project moved away between runs

	b := NewAppRestored(reg, LoadState(), mk, keep)
	if len(b.sessions) != 1 {
		t.Fatalf("only the surviving agent should come back, got %d", len(b.sessions))
	}
	all := b.sessions[0].tl.Content()
	if !strings.Contains(all, "could not restore") || !strings.Contains(all, "directory is gone") {
		t.Fatalf("the user must be told why an agent is missing:\n%s", all)
	}
}

func TestRestoreSkipsUnknownBackends(t *testing.T) {
	tempState(t)
	dir := t.TempDir()
	mk := fastMock()

	a := NewApp(regWith(mk, unavailableAgent{name: "codex"}), mk, dir)
	a.resize(140, 30)
	a.addSession(unavailableAgent{name: "codex"}, dir).introduce()
	a.persist()

	// this build no longer has a "codex" backend registered
	b := NewAppRestored(regWith(mk), LoadState(), mk, dir)
	if len(b.sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(b.sessions))
	}
	if !strings.Contains(b.sessions[0].tl.Content(), `no "codex" agent`) {
		t.Fatalf("must explain the skip:\n%s", b.sessions[0].tl.Content())
	}
}

func TestRestoreFallsBackWhenNothingIsSaved(t *testing.T) {
	tempState(t)
	dir := t.TempDir()
	mk := fastMock()
	a := NewAppRestored(regWith(mk), LoadState(), mk, dir)
	if len(a.sessions) != 1 || a.sessions[0].Dir != dir {
		t.Fatalf("a first run should open one agent in the given dir: %+v", a.sessions)
	}
	if !strings.Contains(a.sessions[0].tl.Content(), "working in") {
		t.Fatal("a fresh agent still gets its banner")
	}
}

func TestCorruptStateFileIsIgnoredNotFatal(t *testing.T) {
	p := tempState(t)
	if err := os.WriteFile(p, []byte("{not json at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LoadState(); len(got.Sessions) != 0 {
		t.Fatal("a corrupt file must load as empty rather than crash or half-restore")
	}
	mk := fastMock()
	a := NewAppRestored(regWith(mk), LoadState(), mk, t.TempDir())
	if len(a.sessions) != 1 {
		t.Fatal("crema must still start")
	}
}

func TestStateFromAFutureVersionIsIgnored(t *testing.T) {
	p := tempState(t)
	if err := os.WriteFile(p, []byte(`{"version":999,"sessions":[{"backend":"mock","dir":"/x"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LoadState(); len(got.Sessions) != 0 {
		t.Fatal("a newer on-disk format must be ignored, not misread")
	}
}

func TestSavingBoundsTheFileAndSaysWhatItDropped(t *testing.T) {
	blocks := make([]Block, maxSavedBlocks+50)
	for i := range blocks {
		blocks[i] = Block{Kind: BlockAssistant, Text: "line"}
	}
	blocks[len(blocks)-1].Text = strings.Repeat("x", maxSavedBlockRunes+500)

	out := trimForSaving(blocks)
	if len(out) != maxSavedBlocks+1 { // +1 for the notice block
		t.Fatalf("want %d blocks, got %d", maxSavedBlocks+1, len(out))
	}
	if !strings.Contains(out[0].Text, "50 earlier entries were not saved") {
		t.Fatalf("dropped history must be announced: %q", out[0].Text)
	}
	last := out[len(out)-1].Text
	if !strings.Contains(last, "500 characters were not saved") {
		t.Fatalf("a truncated block must say so: %q", last[len(last)-80:])
	}
}

func TestThemeChoiceIsRemembered(t *testing.T) {
	restoreTheme(t)
	tempState(t)
	mk := fastMock()
	a := NewApp(regWith(mk), mk, t.TempDir())
	a.resize(140, 30)

	SetMode(ModeDark)
	a.Update(kmsg(tea.KeyCtrlL)) // → light, and persists
	if LoadState().Theme != "light" {
		t.Fatalf("theme not saved: %q", LoadState().Theme)
	}
}

func TestQuittingSavesOpenAgents(t *testing.T) {
	tempState(t)
	mk := fastMock()
	a := NewApp(regWith(mk), mk, t.TempDir())
	a.resize(140, 30)
	a.addSession(mk, t.TempDir()).introduce()

	if _, cmd := a.Update(kmsg(tea.KeyCtrlQ)); cmd == nil {
		t.Fatal("ctrl+q should quit")
	}
	if n := len(LoadState().Sessions); n != 2 {
		t.Fatalf("quitting must save both agents, saved %d", n)
	}
}

func TestClosingAnAgentIsRemembered(t *testing.T) {
	tempState(t)
	mk := fastMock()
	a := NewApp(regWith(mk), mk, t.TempDir())
	a.resize(140, 30)
	a.addSession(mk, t.TempDir()).introduce()
	a.persist()

	a.Update(kmsg(tea.KeyCtrlW))
	if n := len(LoadState().Sessions); n != 1 {
		t.Fatalf("a closed agent must not come back, saved %d", n)
	}
}

// Spend survives a restart, so the context size has to as well — a bar that
// remembers what was paid but not what filled the window reads as broken.
func TestContextSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	a := testApp(t)
	s := a.cur()
	s.Dir = dir
	s.agentSID, s.cost = "sess-1", 3.5
	s.ctxTokens, s.ctxWindow = 41922, 200000
	if err := SaveState(a.StateSnapshot()); err != nil {
		t.Fatal(err)
	}

	b := testApp(t)
	b.sessions = nil
	if n, _ := b.RestoreSessions(LoadState()); n != 1 {
		t.Fatalf("restored %d agents", n)
	}
	r := b.cur()
	if r.ctxTokens != 41922 || r.ctxWindow != 200000 {
		t.Fatalf("context not restored: %d of %d", r.ctxTokens, r.ctxWindow)
	}
	if !strings.Contains(stripSGR(b.statusLine()), "21%") {
		t.Fatalf("the bar should know how full the window is:\n%s", stripSGR(b.statusLine()))
	}
}

// Without a session to resume there is nothing to remember: the next turn
// starts empty however big the last one got.
func TestContextIsNotRestoredWithoutASessionToResume(t *testing.T) {
	dir := t.TempDir()
	a := testApp(t)
	s := a.cur()
	s.Dir = dir
	s.ctxTokens, s.ctxWindow = 41922, 200000 // but no agentSID
	if err := SaveState(a.StateSnapshot()); err != nil {
		t.Fatal(err)
	}
	b := testApp(t)
	b.sessions = nil
	b.RestoreSessions(LoadState())
	if got := b.cur(); got.ctxTokens != 0 || got.ctxWindow != 0 {
		t.Fatalf("context carried over without a session: %d of %d", got.ctxTokens, got.ctxWindow)
	}
}
