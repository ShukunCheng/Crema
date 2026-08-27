package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// The scenario table doubles as the contract: what /keytest tells the user is
// what the judgment actually does, or this fails.
func TestTheScenarioTableIsTrue(t *testing.T) {
	for _, sc := range keyScenarios {
		newline, why := judgeEnter(sc.f)
		if newline != sc.want {
			t.Errorf("%s: verdict %s (%s), the table promises %s",
				sc.name, verdictWord(newline), why, verdictWord(sc.want))
		}
	}
}

// /keytest lists the scenarios and puts them in the option picker; picking
// one runs it through the real judgment. None of it costs a turn.
func TestKeytestOffersAChooserAndRunsTheChoice(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	send(t, a, "/keytest")
	if a.choices == nil {
		t.Fatal("the scenarios should be in the option picker")
	}
	if got := lastBlock(s); !strings.Contains(got, "key test") {
		t.Fatalf("the list should be in the conversation: %q", got)
	}

	press(t, a, kmsg(tea.KeyEnter)) // take the first option, /keytest plain
	if a.choices != nil {
		t.Fatal("the pick should close the chooser")
	}
	if got := lastBlock(s); !strings.Contains(got, "verdict: send — nothing marked it a paste") {
		t.Fatalf("picking a scenario should run it: %q", got)
	}
	if s.busy {
		t.Fatal("a key test must not cost a turn")
	}
}

func TestKeytestAllRunsTheWholeTable(t *testing.T) {
	a := testApp(t)
	send(t, a, "/keytest all")
	got := lastBlock(a.cur())
	for _, sc := range keyScenarios {
		if !strings.Contains(got, sc.name) {
			t.Fatalf("scenario %s is missing from the report:\n%s", sc.name, got)
		}
	}
	if strings.Contains(got, "WRONG") {
		t.Fatalf("the build disagrees with its own table:\n%s", got)
	}
}

func TestKeytestRefusesANameItDoesNotHave(t *testing.T) {
	a := testApp(t)
	send(t, a, "/keytest warp")
	if !strings.Contains(a.note, "not a scenario") {
		t.Fatalf("note = %q", a.note)
	}
}

// Enter used to be silently ignored while the focus was on another pane —
// type a message, click the conversation to copy something, press enter,
// nothing. A written draft now sends from anywhere.
func TestEnterSendsTheDraftFromAnyPane(t *testing.T) {
	a := testApp(t)
	typeRunes(t, a, "ship it")
	a.focus = focusTimeline
	press(t, a, kmsg(tea.KeyEnter))
	if !a.cur().busy {
		t.Fatal("a written draft plus enter is a send, wherever the focus is")
	}
	if a.focus != focusInput {
		t.Fatal("sending should bring the focus home")
	}
	a.cur().close()
}

// An empty box keeps enter quiet on the other panes — there is nothing to
// send, and the keystroke may belong to whatever the user was reading.
func TestEnterOnAnEmptyBoxStaysQuiet(t *testing.T) {
	a := testApp(t)
	a.focus = focusDiff
	press(t, a, kmsg(tea.KeyEnter))
	if a.cur().busy {
		t.Fatal("nothing was written; nothing should have been sent")
	}
	if a.focus != focusDiff {
		t.Fatal("and the focus should not have moved")
	}
}

// The swallowed send: typed fast, tapped enter, and crema looked only after
// the key was back up. The OS remembering the tap (enterHeld true) is what
// makes it a send even ninety milliseconds after the last letter.
func TestAFastTappedEnterStillSends(t *testing.T) {
	a := testApp(t)
	typedFast(t, 90)
	typeRunes(t, a, "ship it")
	press(t, a, kmsg(tea.KeyEnter))
	if !a.cur().busy {
		t.Fatal("a remembered tap must send, however late crema read it")
	}
	a.cur().close()
}

// The same timing with no press on record is a paste's trailing newline, and
// stays in the draft.
func TestATrailingPastedNewlineStaysPut(t *testing.T) {
	a := testApp(t)
	typedFast(t, 90)
	prev := enterHeld
	enterHeld = func() bool { return false }
	t.Cleanup(func() { enterHeld = prev })

	typeRunes(t, a, "pasted line")
	press(t, a, kmsg(tea.KeyEnter))
	if a.cur().busy {
		t.Fatal("no press on record and hot on the last key: that is a paste")
	}
	if got := a.in.Value(); got != "pasted line\n" {
		t.Fatalf("draft = %q, want the newline kept", got)
	}
}

// lastBlock is the newest thing in the conversation, whoever wrote it.
func lastBlock(s *Session) string {
	bs := s.tl.Blocks()
	if len(bs) == 0 {
		return ""
	}
	return bs[len(bs)-1].Text
}

// typedFast makes every key arrive ms milliseconds after the one before —
// quick typing, not a replay.
func typedFast(t *testing.T, ms int) {
	t.Helper()
	prev := timeNow
	var clock time.Time
	timeNow = func() time.Time {
		clock = clock.Add(time.Duration(ms) * time.Millisecond)
		return clock
	}
	t.Cleanup(func() { timeNow = prev })
}
