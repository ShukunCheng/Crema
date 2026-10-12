package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// shift+arrow selects a character at a time, ctrl+shift+arrow a word at a
// time — by the rule ctrl+backspace uses, so a branch name comes apart at its
// punctuation — and pressing on keeps extending from the same start.
func TestShiftArrowsSelectInTheMessageBox(t *testing.T) {
	a := testApp(t)
	a.resize(120, 30)
	typeRunes(t, a, "git checkout fix/1234-order-issue")

	press(t, a, kmsg(tea.KeyShiftLeft))
	press(t, a, kmsg(tea.KeyShiftLeft))
	if got := a.in.SelectedText(); got != "ue" {
		t.Fatalf("two shift+← should select two characters, got %q", got)
	}
	press(t, a, kmsg(tea.KeyCtrlShiftLeft))
	if got := a.in.SelectedText(); got != "issue" {
		t.Fatalf("ctrl+shift+← should extend to the start of the word, got %q", got)
	}
	press(t, a, kmsg(tea.KeyCtrlShiftLeft))
	press(t, a, kmsg(tea.KeyCtrlShiftLeft))
	if got := a.in.SelectedText(); got != "order-issue" {
		t.Fatalf("and on, a part at a time, got %q", got)
	}
	press(t, a, kmsg(tea.KeyShiftRight))
	if got := a.in.SelectedText(); got != "rder-issue" {
		t.Fatalf("shift+→ shrinks it back from the loose end, got %q", got)
	}
	press(t, a, kmsg(tea.KeyShiftHome))
	if got := a.in.SelectedText(); got != "git checkout fix/1234-order-issue" {
		t.Fatalf("shift+home takes it to the start, got %q", got)
	}

	// It is a selection like a drag's: ctrl+c copies it and typing replaces it.
	var copied string
	prev := copyToClipboard
	copyToClipboard = func(s string) error { copied = s; return nil }
	t.Cleanup(func() { copyToClipboard = prev })
	press(t, a, kmsg(tea.KeyCtrlC))
	if copied != "git checkout fix/1234-order-issue" {
		t.Fatalf("ctrl+c copied %q", copied)
	}
	typeRunes(t, a, "ls")
	if got := a.in.Value(); got != "ls" {
		t.Fatalf("typing over the selection should replace it, draft = %q", got)
	}
}

// A plain arrow after selecting drops the highlight, as in any text box, and
// selecting from the end forwards selects nothing.
func TestAPlainArrowEndsTheSelection(t *testing.T) {
	a := testApp(t)
	a.resize(120, 30)
	typeRunes(t, a, "hello world")
	press(t, a, kmsg(tea.KeyShiftRight)) // already at the end
	if a.in.HasSelection() {
		t.Fatalf("nothing to the right of the caret, got %q", a.in.SelectedText())
	}
	press(t, a, kmsg(tea.KeyCtrlShiftLeft))
	if got := a.in.SelectedText(); got != "world" {
		t.Fatalf("got %q", got)
	}
	press(t, a, kmsg(tea.KeyLeft))
	if a.in.HasSelection() {
		t.Fatal("a plain arrow should drop the selection")
	}
}

// Across lines, shift+↑ takes a row at a time and keeps the text exact.
func TestShiftUpSelectsAcrossLines(t *testing.T) {
	a := testApp(t)
	a.resize(120, 30)
	a.in.SetValue("first line\nsecond line")
	press(t, a, kmsg(tea.KeyShiftUp))
	got := a.in.SelectedText()
	if !strings.Contains(got, "\n") || !strings.HasSuffix(got, "second line") {
		t.Fatalf("shift+↑ should reach into the line above, got %q", got)
	}
}

// shift+click carries a finished selection on to the click, the way an
// editor does: select a start, shift+click an end, and everything between is
// selected and copied — no need to hold the button down across a long stretch.
func TestShiftClickExtendsASelection(t *testing.T) {
	clip := fakeClipboard(t)
	a := selApp(t)
	x := a.lay.SidebarW

	a.Update(drag(x, 0, tea.MouseActionPress))
	a.Update(drag(x+5, 0, tea.MouseActionMotion))
	a.Update(drag(x+5, 0, tea.MouseActionRelease))
	if *clip != "first" {
		t.Fatalf("the first drag copied %q", *clip)
	}

	press := drag(x+6, 1, tea.MouseActionPress)
	press.Shift = true
	a.Update(press)
	release := drag(x+6, 1, tea.MouseActionRelease)
	release.Shift = true
	a.Update(release)
	if *clip != "first line of output\nsecond" {
		t.Fatalf("shift+click should carry the selection to the click, copied %q", *clip)
	}

	// Without a selection to carry on, shift+click is an ordinary click.
	a.cur().tl.ClearSelection()
	*clip = ""
	a.Update(press)
	a.Update(release)
	if *clip != "" {
		t.Fatalf("with nothing selected there is nothing to extend, copied %q", *clip)
	}
}
