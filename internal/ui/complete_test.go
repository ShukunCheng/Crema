package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// typeRunes types s one key at a time, rendering after each the way the
// runtime does — the text area only learns what it is showing when asked to
// draw, so a test that skips the draws exercises a state that never happens.
func typeRunes(t *testing.T, a *App, s string) {
	t.Helper()
	for _, r := range s {
		a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		_ = a.View()
	}
}

// press sends one key and renders, like typeRunes.
func press(t *testing.T, a *App, msg tea.KeyMsg) {
	t.Helper()
	a.Update(msg)
	_ = a.View()
}

func TestCompletionTrigger(t *testing.T) {
	cases := []struct {
		draft string
		kind  completionKind
		query string
		at    int
		ok    bool
	}{
		{draft: "/", kind: completeCommand, ok: true},
		{draft: "/rev", kind: completeCommand, query: "rev", ok: true},
		{draft: "/honeycomb:query", kind: completeCommand, query: "honeycomb:query", ok: true},
		{draft: "/review the diff"}, // arguments started; the name is settled
		{draft: "/a\nb"},            // a command is one word
		{draft: ""},                 //
		{draft: "ship it"},          //
		{draft: "look at me"},       // no trigger anywhere
		{draft: "@", kind: completeFile, ok: true},
		{draft: "@int", kind: completeFile, query: "int", ok: true},
		// "@" works mid-sentence, which is the point of it.
		{draft: "explain @internal/ui", kind: completeFile, query: "internal/ui", at: 8, ok: true},
		{draft: "explain @a b"}, // that mention is finished
		{draft: "user@example.com"},
	}
	for _, c := range cases {
		kind, query, at, ok := completionTrigger(c.draft)
		if ok != c.ok || (ok && (kind != c.kind || query != c.query || at != c.at)) {
			t.Fatalf("completionTrigger(%q) = %v,%q,%d,%v want %v,%q,%d,%v",
				c.draft, kind, query, at, ok, c.kind, c.query, c.at, c.ok)
		}
	}
}

func TestMatchItemsRanksPrefixThenLastPartThenSubstring(t *testing.T) {
	all := []completionItem{
		{name: "deep-query"},
		{name: "honeycomb:query-patterns"},
		{name: "query-logs"},
	}
	got := matchItems(all, "query")
	want := []string{"query-logs", "honeycomb:query-patterns", "deep-query"}
	for i := range want {
		if got[i].name != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	if len(matchItems(all, "nothing-like-this")) != 0 {
		t.Fatal("a query matching nothing must return nothing")
	}
	if len(matchItems(all, "")) != len(all) {
		t.Fatal("a bare trigger must offer everything")
	}
	// The same rule finds a file by its base name.
	paths := []completionItem{{name: "internal/ui/app.go"}, {name: "app_test.go"}}
	if got := matchItems(paths, "app.go"); len(got) != 1 || got[0].name != "internal/ui/app.go" {
		t.Fatalf("matching a path by base name = %+v", got)
	}
}

func TestSlashOpensCompletionsAndTabInserts(t *testing.T) {
	a := testApp(t)
	typeRunes(t, a, "/")
	if a.comp == nil {
		t.Fatal("typing / did not open the command list")
	}
	// The list is the backend's commands and crema's own, in one alphabet.
	for _, want := range []string{"clear", "demo"} {
		found := false
		for _, r := range a.comp.rows {
			found = found || r.name == want
		}
		if !found {
			t.Fatalf("the list is missing /%s: %v", want, a.comp.rows)
		}
	}

	typeRunes(t, a, "demo")
	if len(a.comp.rows) != 2 { // the mock backend's two entries
		t.Fatalf("rows = %v", a.comp.rows)
	}
	if !strings.Contains(a.View(), "/demo-skill") {
		t.Fatal("the drop-up is not on screen")
	}

	a.Update(kmsg(tea.KeyDown))
	a.Update(kmsg(tea.KeyTab))
	if got := a.in.Value(); got != "/demo-skill " {
		t.Fatalf("input = %q, want %q", got, "/demo-skill ")
	}
	if a.comp != nil {
		t.Fatal("the list should close once a name is inserted")
	}
	if a.cur().busy {
		t.Fatal("tab must complete, not send the turn")
	}
}

func TestCompletionsFilterAsYouType(t *testing.T) {
	a := testApp(t)
	typeRunes(t, a, "/demo-s")
	if a.comp == nil || len(a.comp.rows) != 1 || a.comp.rows[0].name != "demo-skill" {
		t.Fatalf("comp = %+v", a.comp)
	}
	typeRunes(t, a, "zzz")
	if a.comp != nil {
		t.Fatal("a query matching nothing must not leave a box on screen")
	}
}

// esc dismisses the list without touching the turn, and typing brings it back.
func TestEscDismissesCompletionsThenTypingReopens(t *testing.T) {
	a := testApp(t)
	typeRunes(t, a, "/de")
	a.Update(kmsg(tea.KeyEsc))
	if a.comp != nil {
		t.Fatal("esc did not dismiss the list")
	}
	if a.in.Value() != "/de" {
		t.Fatalf("esc changed the draft: %q", a.in.Value())
	}
	typeRunes(t, a, "m")
	if a.comp == nil {
		t.Fatal("typing after esc did not reopen the list")
	}
}

// The list owns enter while it is open, so a half-typed name is completed
// rather than sent. With no list open, enter still sends.
func TestEnterCompletesWhileOpenAndSendsOtherwise(t *testing.T) {
	a := testApp(t)
	typeRunes(t, a, "/dem") // "/demo" is the highlighted row
	a.Update(kmsg(tea.KeyEnter))
	if a.cur().busy {
		t.Fatal("enter sent the turn instead of completing")
	}
	if got := a.in.Value(); got != "/demo " {
		t.Fatalf("input = %q", got)
	}
	a.Update(kmsg(tea.KeyEnter))
	if !a.cur().busy {
		t.Fatal("enter with no list open must send")
	}
	a.cur().close()
}

func TestClickingACompletionInsertsIt(t *testing.T) {
	a := testApp(t)
	typeRunes(t, a, "/demo")
	top := a.lay.PaneH - a.comp.Height()
	a.Update(tea.MouseMsg{
		X: 4, Y: top + 2, // border, first row, second row
		Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	if got := a.in.Value(); got != "/demo-skill " {
		t.Fatalf("input = %q, want the clicked row", got)
	}
}

// The draft is only ever what the user typed: the list shows the candidates,
// and nothing is written into the box until a name is taken. This is how the
// CLIs' own slash menus behave.
func TestTypingNeverRewritesTheDraft(t *testing.T) {
	a := testApp(t)
	for _, want := range []string{"/", "/d", "/de", "/dem"} {
		typeRunes(t, a, want[len(want)-1:])
		if got := a.in.Value(); got != want {
			t.Fatalf("input = %q, want %q — the draft must stay as typed", got, want)
		}
	}
	a.Update(kmsg(tea.KeyDown)) // moving the selection doesn't touch it either
	if got := a.in.Value(); got != "/dem" {
		t.Fatalf("input = %q after ↑↓, want %q", got, "/dem")
	}
	a.Update(kmsg(tea.KeyBackspace))
	if got := a.in.Value(); got != "/de" {
		t.Fatalf("input = %q after backspace, want %q", got, "/de")
	}
	if a.comp == nil {
		t.Fatal("the list should still be open")
	}
}

// Space ends the name, so the list closes and the draft keeps what was typed.
func TestSpaceClosesTheListWithoutCompleting(t *testing.T) {
	a := testApp(t)
	typeRunes(t, a, "/dem ")
	if got := a.in.Value(); got != "/dem " {
		t.Fatalf("input = %q, want %q", got, "/dem ")
	}
	if a.comp != nil {
		t.Fatal("the list should close once the name is settled")
	}
}

// fileApp is an app whose working directory holds a few files to mention.
func fileApp(t *testing.T) *App {
	t.Helper()
	a := testApp(t)
	s := a.cur()
	for _, p := range []string{"main.go", "internal/ui/app.go", "internal/ui/input.go", "README.md"} {
		writeUIFile(t, s.Dir, p)
	}
	s.Reload()
	return a
}

func writeUIFile(t *testing.T, dir, rel string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// "@" lists the project's files, the same way "/" lists its commands.
func TestAtOpensTheFileListAndTabInsertsAPath(t *testing.T) {
	a := fileApp(t)
	typeRunes(t, a, "@")
	if a.comp == nil {
		t.Fatal("typing @ did not open the file list")
	}
	if len(a.comp.rows) != 4 {
		t.Fatalf("rows = %+v, want the four files", a.comp.rows)
	}
	if !strings.Contains(a.View(), "@internal/ui/app.go") {
		t.Fatal("the drop-up is not on screen")
	}

	typeRunes(t, a, "input")
	if a.comp == nil || len(a.comp.rows) != 1 {
		t.Fatalf("typing must filter: %+v", a.comp)
	}
	press(t, a, kmsg(tea.KeyTab))
	if got := a.in.Value(); got != "@internal/ui/input.go " {
		t.Fatalf("draft = %q, want the path inserted", got)
	}
	if a.cur().busy {
		t.Fatal("tab must complete, not send")
	}
}

// A file can be mentioned in the middle of a sentence, and only that word is
// replaced.
func TestAtCompletesMidSentence(t *testing.T) {
	a := fileApp(t)
	typeRunes(t, a, "please read @app.go")
	if a.comp == nil {
		t.Fatalf("no list for a mid-sentence mention: %q", a.in.Value())
	}
	press(t, a, kmsg(tea.KeyEnter))
	if got := a.in.Value(); got != "please read @internal/ui/app.go " {
		t.Fatalf("draft = %q", got)
	}
	if a.cur().busy {
		t.Fatal("enter completed the mention, it must not also send")
	}
}

// An email address is not a file mention.
func TestAtInTheMiddleOfAWordIsNotAMention(t *testing.T) {
	a := fileApp(t)
	typeRunes(t, a, "mail me at user@example.com")
	if a.comp != nil {
		t.Fatalf("opened a list for %q", a.in.Value())
	}
}

func TestReloadRescansCommandsAndFiles(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	if len(s.Commands()) == 0 {
		t.Fatal("the mock backend lists commands")
	}
	s.Files()
	if !s.cmdsLoaded || !s.filesLoaded {
		t.Fatal("both lists should be cached after the first lookup")
	}
	s.Reload()
	if s.cmdsLoaded || s.cmds != nil || s.filesLoaded || s.files != nil {
		t.Fatal("Reload must drop both caches")
	}
}
