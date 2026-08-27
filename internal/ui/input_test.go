package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestInputGrowsWithTheDraftAndPanesGiveUpTheRows(t *testing.T) {
	a := testApp(t)
	a.resize(100, 30)
	onePane := a.lay.PaneH
	if a.in.Height() != InputHeight {
		t.Fatalf("empty box is %d rows, want %d", a.in.Height(), InputHeight)
	}

	typeRunes(t, a, "one")
	press(t, a, kmsg(tea.KeyCtrlJ)) // newline
	// The row that was already there has to stay on screen: the box grew for
	// the new one, it didn't scroll past the old one.
	if got := inputRows(a); len(got) != 2 || got[0] != "one" || got[1] != "" {
		t.Fatalf("box shows %q, want the first line kept and an empty one below", got)
	}
	typeRunes(t, a, "two")
	if a.in.Height() != InputHeight+1 {
		t.Fatalf("two-line draft: box is %d rows, want %d", a.in.Height(), InputHeight+1)
	}
	if a.lay.PaneH != onePane-1 {
		t.Fatalf("panes did not give up the row: %d, want %d", a.lay.PaneH, onePane-1)
	}
	if got := inputRows(a); len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("box shows %q, want both lines", got)
	}
	if frame := strings.Split(a.View(), "\n"); len(frame) != 30 {
		t.Fatalf("frame is %d lines, want 30", len(frame))
	}

	for i := 0; i < 10; i++ { // well past the cap
		press(t, a, kmsg(tea.KeyCtrlJ))
	}
	if a.in.Height() != maxInputRows+2 {
		t.Fatalf("box is %d rows, want the cap %d", a.in.Height(), maxInputRows+2)
	}
	if got := inputRows(a); len(got) != maxInputRows || got[len(got)-1] != "" {
		t.Fatalf("box shows %q, want the caret's end of an overlong draft", got)
	}

	// Deleting it all again gives the rows back.
	for i := 0; i < 40; i++ {
		press(t, a, kmsg(tea.KeyBackspace))
	}
	if a.in.Height() != InputHeight || a.lay.PaneH != onePane {
		t.Fatalf("box %d rows / paneH %d, want %d / %d",
			a.in.Height(), a.lay.PaneH, InputHeight, onePane)
	}
}

// A draft that outgrew the box and then shrank again has to be shown from its
// first row, not from wherever the caret had scrolled the text area to.
func TestInputShowsTheWholeDraftAfterItShrinksBack(t *testing.T) {
	a := testApp(t)
	a.resize(70, 20)
	for i := 1; i <= 9; i++ {
		typeRunes(t, a, "line")
		if i < 9 {
			press(t, a, kmsg(tea.KeyCtrlJ))
		}
	}
	for i := 0; i < 6*5; i++ { // back down to three lines
		press(t, a, kmsg(tea.KeyBackspace))
	}
	if got, want := a.in.Value(), "line\nline\nline"; got != want {
		t.Fatalf("draft = %q, want %q", got, want)
	}
	got := inputRows(a)
	if len(got) != 3 || got[0] != "line" || got[2] != "line" {
		t.Fatalf("box shows %q, want all three rows from the top", got)
	}
}

// inputRows is the text of the rows the input box is showing.
func inputRows(a *App) []string {
	var out []string
	for _, l := range strings.Split(a.in.View(), "\n") {
		l = stripSGR(l)
		if !strings.HasPrefix(l, "│") {
			continue // the box's own top and bottom borders
		}
		l = strings.TrimSuffix(strings.TrimPrefix(l, "│"), "│")
		out = append(out, strings.TrimRight(strings.TrimPrefix(l, "❯ "), " "))
	}
	return out
}

func TestInputGrowsWhenAWideDraftWraps(t *testing.T) {
	a := testApp(t)
	a.resize(60, 30)
	typeRunes(t, a, strings.Repeat("x", 120)) // more than two rows' worth
	if a.in.Height() <= InputHeight {
		t.Fatalf("a wrapped draft did not grow the box: %d rows", a.in.Height())
	}
	// Narrowing wraps it further; widening lets it shrink back.
	tall := a.in.Height()
	a.resize(140, 30)
	if a.in.Height() >= tall {
		t.Fatalf("box stayed %d rows after widening (was %d)", a.in.Height(), tall)
	}
}

// Typing is meant for the message, wherever the focus was — the input takes
// it back and the character lands there rather than being swallowed.
func TestTypingTakesTheFocusBackToTheInput(t *testing.T) {
	a := testApp(t)
	a.Update(kmsg(tea.KeyCtrlO)) // focus the conversation
	if a.focus != focusTimeline {
		t.Fatalf("focus = %v, want the timeline", a.focus)
	}
	typeRunes(t, a, "hello")
	if a.focus != focusInput {
		t.Fatalf("focus = %v, want the input", a.focus)
	}
	if got := a.in.Value(); got != "hello" {
		t.Fatalf("draft = %q, want the typing to have landed", got)
	}
	if !a.in.Focused() {
		t.Fatal("the input must actually take the focus, caret and all")
	}
}

// Movement and shortcuts still belong to whatever is focused.
func TestArrowsAndShortcutsDoNotStealTheFocus(t *testing.T) {
	a := testApp(t)
	a.Update(kmsg(tea.KeyCtrlO))
	for _, k := range []tea.KeyMsg{kmsg(tea.KeyPgDown), kmsg(tea.KeyDown), kmsg(tea.KeyHome)} {
		a.Update(k)
		if a.focus != focusTimeline {
			t.Fatalf("%v moved the focus off the conversation", k)
		}
	}
}

// ctrl+arrow jumps a word, the way it does everywhere else on the desktop.
func TestCtrlArrowJumpsAWord(t *testing.T) {
	a := testApp(t)
	typeRunes(t, a, "alpha beta")
	press(t, a, tea.KeyMsg{Type: tea.KeyCtrlLeft})
	typeRunes(t, a, "X")
	if got := a.in.Value(); got != "alpha Xbeta" {
		t.Fatalf("draft = %q, want the caret to have jumped to %q", got, "alpha Xbeta")
	}
	press(t, a, tea.KeyMsg{Type: tea.KeyCtrlRight})
	typeRunes(t, a, "!")
	if got := a.in.Value(); got != "alpha Xbeta!" {
		t.Fatalf("draft = %q, want the caret at the end of the word", got)
	}
}

// pasteClock makes every key look like it arrived at replay speed, the way a
// console does when it replays pasted text as keystrokes.
func pasteClock(t *testing.T) {
	t.Helper()
	prevTime, prevEnter := timeNow, enterHeld
	var clock time.Time
	timeNow = func() time.Time {
		clock = clock.Add(time.Millisecond)
		return clock
	}
	enterHeld = func() bool { return false } // nothing is physically held in a paste
	t.Cleanup(func() { timeNow, enterHeld = prevTime, prevEnter })
}

// A newline inside pasted text must land in the draft, not fire off the first
// line — the console replays a paste as keystrokes, enter and all.
func TestPastedNewlinesStayInTheDraft(t *testing.T) {
	a := testApp(t)
	pasteClock(t)

	typeRunes(t, a, "first line")
	press(t, a, kmsg(tea.KeyEnter))
	typeRunes(t, a, "second line")
	press(t, a, kmsg(tea.KeyEnter))
	typeRunes(t, a, "third line")

	if a.cur().busy {
		t.Fatal("a pasted newline sent the turn")
	}
	if got, want := a.in.Value(), "first line\nsecond line\nthird line"; got != want {
		t.Fatalf("draft = %q, want %q", got, want)
	}
	if a.in.Height() != InputHeight+2 {
		t.Fatalf("box is %d rows, want the three lines shown", a.in.Height())
	}
}

// Typing speed is what tells the two apart: an enter that follows a pause is
// the user asking to send.
func TestTypedEnterStillSends(t *testing.T) {
	a := testApp(t)
	typeRunes(t, a, "ship it") // the package clock puts a second between keys
	press(t, a, kmsg(tea.KeyEnter))
	if !a.cur().busy {
		t.Fatal("a typed enter must still send the turn")
	}
	a.cur().close()
}

// If the app was slow enough to process an enter after the user let go of it,
// the key looks physically up — but it followed a pause, so it still sends.
func TestALaggedEnterAfterAPauseStillSends(t *testing.T) {
	a := testApp(t)
	prev := enterHeld
	enterHeld = func() bool { return false }
	t.Cleanup(func() { enterHeld = prev })

	typeRunes(t, a, "ship it") // the package clock leaves a second between keys
	press(t, a, kmsg(tea.KeyEnter))
	if !a.cur().busy {
		t.Fatal("an enter after a pause must send, however late it was read")
	}
	a.cur().close()
}

// A terminal that brackets its pastes says so on the message, and then the
// text goes in whole — shortcuts inside it are text, not keys.
func TestBracketedPasteGoesStraightIntoTheDraft(t *testing.T) {
	a := testApp(t)
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("one\ntwo"), Paste: true})
	if a.cur().busy {
		t.Fatal("a bracketed paste sent the turn")
	}
	if got, want := a.in.Value(), "one\ntwo"; got != want {
		t.Fatalf("draft = %q, want %q", got, want)
	}
}

// ctrl+enter breaks the line; a plain enter sends. The console reports both
// enters the same way, so the test drives the real sequence — the rune-0 key
// the ctrl press itself emits, then the enter, with the control key held.
func TestCtrlEnterBreaksTheLineAndPlainEnterSends(t *testing.T) {
	a := testApp(t)
	held := false
	old := ctrlHeld
	ctrlHeld = func() bool { return held }
	t.Cleanup(func() { ctrlHeld = old })

	typeRunes(t, a, "first line")
	held = true
	press(t, a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0}}) // ctrl goes down
	press(t, a, kmsg(tea.KeyEnter))
	held = false
	typeRunes(t, a, "second line")

	if a.cur().busy {
		t.Fatal("ctrl+enter sent the turn instead of breaking the line")
	}
	if got, want := a.in.Value(), "first line\nsecond line"; got != want {
		t.Fatalf("draft = %q, want %q", got, want)
	}
	if a.in.Height() != InputHeight+1 {
		t.Fatalf("box is %d rows, want it grown for the second line", a.in.Height())
	}

	press(t, a, kmsg(tea.KeyEnter)) // no ctrl: this one sends
	if !a.cur().busy {
		t.Fatal("a plain enter must send")
	}
	last := a.cur().tl.blocks[len(a.cur().tl.blocks)-1]
	if last.Kind != BlockUser || last.Text != "first line\nsecond line" {
		t.Fatalf("sent %+v, want both lines with no stray characters", last)
	}
	a.cur().close()
}

// The newline keys still make newlines.
func TestAltEnterAndCtrlJStillBreakTheLine(t *testing.T) {
	a := testApp(t)
	typeRunes(t, a, "one")
	press(t, a, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	typeRunes(t, a, "two")
	press(t, a, kmsg(tea.KeyCtrlJ))
	typeRunes(t, a, "three")

	if a.cur().busy {
		t.Fatal("neither newline key may send")
	}
	if got, want := a.in.Value(), "one\ntwo\nthree"; got != want {
		t.Fatalf("draft = %q, want %q", got, want)
	}
	if a.in.Height() != InputHeight+2 {
		t.Fatalf("box is %d rows, want it grown for three lines", a.in.Height())
	}
}

// A backspace that arrives while the control key is held deletes a word, even
// though the Windows console reader reports it as a plain backspace.
func TestCtrlHeldTurnsBackspaceIntoDeleteWord(t *testing.T) {
	a := testApp(t)
	held := false
	old := ctrlHeld
	ctrlHeld = func() bool { return held }
	t.Cleanup(func() { ctrlHeld = old })

	typeRunes(t, a, "fix the parser")
	a.Update(kmsg(tea.KeyBackspace))
	if got := a.in.Value(); got != "fix the parse" {
		t.Fatalf("plain backspace = %q, want one character gone", got)
	}
	held = true
	a.Update(kmsg(tea.KeyBackspace))
	if got := a.in.Value(); got != "fix the " {
		t.Fatalf("ctrl+backspace = %q, want %q", got, "fix the ")
	}
}

// ctrl+backspace deletes a word; the textarea's own alt+backspace does not.
func TestCtrlBackspaceDeletesAWord(t *testing.T) {
	a := testApp(t)
	typeRunes(t, a, "fix the parser")
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlH}) // what terminals send for ctrl+backspace
	if got := a.in.Value(); got != "fix the " {
		t.Fatalf("input = %q, want %q", got, "fix the ")
	}
	a.Update(tea.KeyMsg{Type: tea.KeyBackspace, Alt: true})
	if got := a.in.Value(); got != "fix the " {
		t.Fatalf("alt+backspace still edits the draft: %q", got)
	}
	a.Update(kmsg(tea.KeyBackspace))
	if got := a.in.Value(); got != "fix the" {
		t.Fatalf("plain backspace should delete one character: %q", got)
	}
}

// stripSGR removes color escapes so a test can look at the text of a row.
func stripSGR(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// shift+enter breaks the line too. The console reports it as a plain enter
// with the modifier thrown away, exactly as it does for ctrl+enter, so the
// same recovery has to cover it.
func TestShiftEnterBreaksTheLine(t *testing.T) {
	a := testApp(t)
	held := false
	old := shiftHeld
	shiftHeld = func() bool { return held }
	t.Cleanup(func() { shiftHeld = old })

	typeRunes(t, a, "first line")
	held = true
	press(t, a, kmsg(tea.KeyEnter))
	held = false
	typeRunes(t, a, "second line")

	if a.cur().busy {
		t.Fatal("shift+enter sent the turn instead of breaking the line")
	}
	if got, want := a.in.Value(), "first line\nsecond line"; got != want {
		t.Fatalf("draft = %q, want %q", got, want)
	}

	press(t, a, kmsg(tea.KeyEnter)) // nothing held: this one sends
	if !a.cur().busy {
		t.Fatal("a plain enter must still send")
	}
	if a.in.Value() != "" {
		t.Fatalf("the draft should have gone: %q", a.in.Value())
	}
	a.cur().close()
}

// A terminal that can tell the two enters apart names the key itself, and the
// text area is bound for that as well as for the recovered form.
func TestATerminalThatNamesShiftEnterIsBoundToo(t *testing.T) {
	a := testApp(t)
	typeRunes(t, a, "one")
	press(t, a, tea.KeyMsg{Type: tea.KeyShiftDown}) // not enter: must not break
	press(t, a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("two")})
	if a.in.Value() != "onetwo" {
		t.Fatalf("draft = %q", a.in.Value())
	}
	for _, k := range []string{"ctrl+enter", "shift+enter", "alt+enter", "ctrl+j"} {
		if !keyBoundToNewline(k) {
			t.Fatalf("%s should insert a newline", k)
		}
	}
	a.cur().close()
}

// keyBoundToNewline asks the input's own keymap, which is what decides.
func keyBoundToNewline(k string) bool {
	for _, bound := range NewInput(40).ta.KeyMap.InsertNewline.Keys() {
		if bound == k {
			return true
		}
	}
	return false
}
