package ui

import (
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

// send types a message and presses enter, returning whatever command came back.
func send(t *testing.T, a *App, text string) tea.Cmd {
	t.Helper()
	a.in.ta.SetValue(text)
	_, cmd := a.Update(kmsg(tea.KeyEnter))
	return cmd
}

func TestSplitCommandTellsACommandFromAPath(t *testing.T) {
	for _, c := range []struct {
		in, name, arg string
		ok            bool
	}{
		{"/clear", "clear", "", true},
		{"/MODEL opus", "model", "opus", true},
		{"/model  opus ", "model", "opus", true},
		{"/src/main.go is broken", "", "", false},
		{`/c:\tmp`, "", "", false},
		{"//not a command", "", "", false},
		{"tell me about /clear", "", "", false},
		{"/", "", "", false},
	} {
		name, arg, ok := splitCommand(c.in)
		if ok != c.ok || name != c.name || arg != c.arg {
			t.Fatalf("splitCommand(%q) = %q,%q,%v; want %q,%q,%v",
				c.in, name, arg, ok, c.name, c.arg, c.ok)
		}
	}
}

// /clear drops the conversation and the backend session behind it, so the next
// message is not a --resume of the one just cleared.
func TestClearStartsANewBackendSession(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	pump(t, a, send(t, a, "make a file"))
	if s.agentSID == "" {
		t.Fatal("the first turn should have stored a session id")
	}
	before := s.tl.Len()

	send(t, a, "/clear")
	if s.agentSID != "" {
		t.Fatalf("clear must forget the backend session, got %q", s.agentSID)
	}
	if s.tl.Len() >= before {
		t.Fatalf("clear must drop the conversation: %d blocks left", s.tl.Len())
	}
	if s.busy {
		t.Fatal("clear is crema's own work; it must not start a turn")
	}
	pump(t, a, send(t, a, "and again"))
	if s.lastOpts.SessionID != "" {
		t.Fatalf("the next turn resumed %q instead of starting fresh", s.lastOpts.SessionID)
	}
	s.close()
}

// The CLI's built-ins are not commands to a headless run — they are prompts it
// charges for. None of them may reach the agent.
func TestBuiltinsNeverReachTheAgent(t *testing.T) {
	for _, text := range []string{"/clear", "/help", "/cost", "/diff", "/model", "/config", "/doctor"} {
		a := testApp(t)
		s := a.cur()
		send(t, a, text)
		if s.busy || s.lastOpts.Prompt != "" {
			t.Fatalf("%s was sent to the agent as %q", text, s.lastOpts.Prompt)
		}
		s.close()
	}
}

// A command the CLI really has still goes to the CLI, even when crema has one
// by the same name.
func TestABackendCommandWinsItsName(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	s.cmds, s.cmdsLoaded = []agent.Command{{Name: "clear", Desc: "the project's own"}}, true
	send(t, a, "/clear")
	if !s.busy {
		t.Fatal("a real /clear command must be sent, not intercepted")
	}
	if s.lastOpts.Prompt != "/clear" {
		t.Fatalf("the agent was sent %q", s.lastOpts.Prompt)
	}
	s.close()
}

// The ones crema can't do are answered rather than charged for.
func TestAnInteractiveOnlyCommandIsExplained(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	send(t, a, "/doctor")
	if s.busy {
		t.Fatal("/doctor must not start a turn")
	}
	if c := s.tl.Content(); !strings.Contains(c, "/doctor") || !strings.Contains(c, "headlessly") {
		t.Fatalf("the timeline should say why nothing happened:\n%s", c)
	}
}

func TestModelSetsAndRejects(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	send(t, a, "/model demo-slow")
	if s.Model != "demo-slow" {
		t.Fatalf("Model = %q", s.Model)
	}
	send(t, a, "/model default")
	if s.Model != agent.DefaultModel {
		t.Fatalf("Model = %q, want the CLI's default", s.Model)
	}
	send(t, a, "/model nonesuch")
	if s.Model != agent.DefaultModel {
		t.Fatalf("a bad name must change nothing, got %q", s.Model)
	}
	if !strings.Contains(a.note, "demo-slow") {
		t.Fatalf("note should list what is on offer: %q", a.note)
	}
	// Bare /model raises the buttons that do the same job.
	send(t, a, "/model")
	if a.controls == nil {
		t.Fatal("/model on its own should raise the button row")
	}
}

func TestCostAndHelpAnswerInTheConversation(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	s.cost = 1.25
	send(t, a, "/cost")
	if c := s.tl.Content(); !strings.Contains(c, "$1.2500") {
		t.Fatalf("/cost should report the spend:\n%s", c)
	}
	send(t, a, "/help")
	if c := s.tl.Content(); !strings.Contains(c, "/compact") || !strings.Contains(c, "ctrl+t") {
		t.Fatalf("/help should list the commands and the keys:\n%s", c)
	}
}

func TestDiffCommandMovesTheDiffPane(t *testing.T) {
	a := testApp(t)
	before := a.diffView
	send(t, a, "/diff")
	if a.diffView == before {
		t.Fatal("/diff should move the diff pane")
	}
}

func TestQuitCommandQuits(t *testing.T) {
	a := testApp(t)
	cmd := send(t, a, "/quit")
	if cmd == nil {
		t.Fatal("/quit returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("/quit produced %T, want tea.QuitMsg", cmd())
	}
}

// /compact is two steps: ask for a summary, then start over knowing it.
func TestCompactSummarisesThenStartsFresh(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	pump(t, a, send(t, a, "make a file"))
	sid := s.agentSID
	if sid == "" {
		t.Fatal("no session to compact")
	}

	cmd := send(t, a, "/compact")
	if !s.busy || !s.compacting {
		t.Fatal("/compact should start the summarising turn")
	}
	if !strings.Contains(s.lastOpts.Prompt, "Summarise this conversation") {
		t.Fatalf("the agent was asked %q", s.lastOpts.Prompt)
	}
	pump(t, a, cmd)

	if s.compacting {
		t.Fatal("the compaction should have finished with the turn")
	}
	if s.agentSID != "" {
		t.Fatalf("compaction must leave the old session behind, got %q", s.agentSID)
	}
	if !strings.Contains(s.tl.Content(), "compacted") {
		t.Fatalf("the conversation should say what happened:\n%s", s.tl.Content())
	}

	// The summary opens the next turn, so the new session is not cold.
	pump(t, a, send(t, a, "carry on"))
	if !strings.HasPrefix(s.lastOpts.Prompt, "This continues an earlier session") {
		t.Fatalf("the summary did not lead the next prompt: %q", s.lastOpts.Prompt)
	}
	if !strings.HasSuffix(s.lastOpts.Prompt, "carry on") {
		t.Fatalf("the message itself went missing: %q", s.lastOpts.Prompt)
	}
	// And only once.
	pump(t, a, send(t, a, "again"))
	if s.lastOpts.Prompt != "again" {
		t.Fatalf("the summary was sent twice: %q", s.lastOpts.Prompt)
	}
	s.close()
}

// Nothing that rewrites the conversation may do it under a running turn.
func TestClearAndCompactWaitForTheTurn(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	send(t, a, "start something")
	if !s.busy {
		t.Fatal("expected a running turn")
	}
	before := s.tl.Len()
	for _, text := range []string{"/clear", "/compact"} {
		send(t, a, text)
		if s.tl.Len() < before {
			t.Fatalf("%s wiped the conversation mid-turn", text)
		}
		if !strings.Contains(a.note, text) {
			t.Fatalf("%s should say why it did nothing: %q", text, a.note)
		}
	}
	s.close()
}

// The drop-up offers crema's commands alongside the backend's, in one alphabet.
func TestTheCommandListHoldsBothKinds(t *testing.T) {
	a := testApp(t)
	names := []string{}
	for _, c := range allCommands(a.cur()) {
		names = append(names, c.Name)
	}
	joined := strings.Join(names, " ")
	for _, want := range []string{"clear", "compact", "demo", "demo-skill"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("%q missing from %v", want, names)
		}
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("the list is not alphabetical: %v", names)
		}
	}
}

// The CLI publishes what it can be asked for in its opening report. That list
// is the only complete one: built-ins like /init and /security-review are not
// files anywhere, so walking the disk can never find them.
func TestTheBackendsOwnCommandListIsOffered(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	feed(a, s, agent.Event{Kind: agent.KindReady,
		Commands: []string{"init", "security-review", "demo", "clear"}})

	if len(s.cliCmds) != 4 {
		t.Fatalf("the report should have been kept: %q", s.cliCmds)
	}
	names := map[string]string{}
	for _, c := range allCommands(s) {
		names[c.Name] = c.Scope
	}
	for _, want := range []string{"init", "security-review"} {
		if _, ok := names[want]; !ok {
			t.Fatalf("/%s should be on offer now: %v", want, names)
		}
	}
	if names["demo"] != "mock" {
		t.Fatalf("a command found on disk keeps its own scope, got %q", names["demo"])
	}
	if names["clear"] != "mock" {
		t.Fatalf("the backend's own /clear should outrank crema's: %q", names["clear"])
	}
	if names["compact"] != "crema" {
		t.Fatalf("crema's own should still be there: %q", names["compact"])
	}
}

// A command the backend has listed is sent to it, even though crema has never
// heard of it — that is the whole point of asking.
func TestACommandTheBackendKnowsIsSent(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	feed(a, s, agent.Event{Kind: agent.KindReady, Commands: []string{"security-review"}})
	send(t, a, "/security-review")
	if !s.busy || s.lastOpts.Prompt != "/security-review" {
		t.Fatalf("it should have gone to the agent, got %q", s.lastOpts.Prompt)
	}
	s.close()
}

// A name it has never listed is a typo. Saying so costs nothing; sending it
// costs a turn.
func TestAnUnknownCommandIsNotPaidFor(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	feed(a, s, agent.Event{Kind: agent.KindReady, Commands: []string{"init"}})
	send(t, a, "/inti")
	if s.busy {
		t.Fatal("a typo must not start a turn")
	}
	if c := s.tl.Content(); !strings.Contains(c, "/inti is not a command") {
		t.Fatalf("it should say so:\n%s", c)
	}
	// Before the backend has said anything, crema assumes nothing and sends.
	b := testApp(t)
	send(t, b, "/whatever")
	if !b.cur().busy {
		t.Fatal("with no list to check against, the command goes as written")
	}
	b.cur().close()
}

// The list survives a restart, so / is complete from the first keystroke
// rather than only after a turn.
func TestTheCommandListSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	a := testApp(t)
	a.cur().Dir = dir
	feed(a, a.cur(), agent.Event{Kind: agent.KindReady, Commands: []string{"init", "recap"}})
	if err := SaveState(a.StateSnapshot()); err != nil {
		t.Fatal(err)
	}
	b := testApp(t)
	b.sessions = nil
	b.RestoreSessions(LoadState())
	if got := b.cur().cliCmds; len(got) != 2 || got[0] != "init" {
		t.Fatalf("cliCmds = %q", got)
	}
}

// Every name crema blocks has to be one the CLI actually has, or the block is
// a guess dressed up as knowledge.
func TestTheBlockedListIsNotMadeUp(t *testing.T) {
	// The list a real `claude -p` reported (2.1.229) — the same source the
	// production list was built from.
	reported := map[string]bool{}
	for _, c := range []string{
		"agents", "autocompact", "batch", "clear", "color", "compact", "config",
		"context", "debug", "design", "doctor", "effort", "extra-usage", "fast",
		"goal", "heapdump", "import", "init", "insights", "loop", "mcp", "model",
		"recap", "reload-skills", "rename", "run", "schedule", "usage",
		"usage-credits", "verify",
	} {
		reported[c] = true
	}
	for _, n := range interactiveOnly {
		if !reported[n] {
			t.Errorf("/%s is blocked but the CLI never listed it", n)
		}
	}
}

// /clear starts the meter over with everything else; /compact carries it,
// because the work continues there.
func TestClearResetsTheSpendAndCompactKeepsIt(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	s.cost = 1.23
	s.noteTask(&agent.TaskUpdate{ID: "t1", Status: "completed", Desc: "old work"})
	send(t, a, "/clear")
	if s.cost != 0 {
		t.Fatalf("cost = %v, want the meter back at zero", s.cost)
	}
	if len(s.tasks) != 0 {
		t.Fatal("the dropped conversation's tasks went with it")
	}

	s.cost = 2.34
	s.tl.Append(Block{Kind: BlockAssistant, Text: "a summary"})
	send(t, a, "/compact")
	if s.cost != 2.34 {
		t.Fatalf("cost = %v — compact continues the work and keeps its bill", s.cost)
	}
}
