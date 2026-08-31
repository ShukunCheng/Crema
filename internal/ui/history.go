package ui

import "strings"

// maxHistory bounds what one agent remembers you asking it. Long enough to
// cover a working day, short enough that the state file stays small.
const maxHistory = 200

// remember files a message away for ↑ to find. Consecutive repeats collapse,
// the way a shell's history does: pressing enter twice on the same thing does
// not make it two things to walk past.
func (s *Session) remember(text string) {
	if text = strings.TrimSpace(text); text == "" {
		return
	}
	if n := len(s.history); n > 0 && s.history[n-1] == text {
		return
	}
	s.history = append(s.history, text)
	if len(s.history) > maxHistory {
		s.history = append(s.history[:0], s.history[len(s.history)-maxHistory:]...)
	}
}

// History is what this agent has been asked, oldest first.
func (s *Session) History() []string { return s.history }

// browsing reports whether ↑ has taken the draft over. While it has, ↓ walks
// forward through the history rather than reaching the buttons below.
func (a *App) browsing() bool { return a.histIdx >= 0 }

// endBrowsing forgets the walk without touching the draft — called whenever
// the draft stops being the history's to write: a keystroke, a send, a
// different agent with a different history.
func (a *App) endBrowsing() { a.histIdx, a.histDraft = -1, "" }

// recallPrev steps back through what you have asked this agent. It reports
// false when there is nothing to recall, which leaves ↑ meaning whatever it
// meant before.
func (a *App) recallPrev() bool {
	s := a.cur()
	if s == nil || len(s.history) == 0 {
		return false
	}
	if !a.browsing() {
		a.histDraft = a.in.Value() // to come back to
		a.histIdx = len(s.history)
	}
	if a.histIdx == 0 {
		return true // at the oldest; stay there rather than wrapping
	}
	a.histIdx--
	a.in.SetValue(s.history[a.histIdx])
	return true
}

// recallNext walks back towards the present, ending on the draft that was
// interrupted. False means the history was not open, so ↓ means what it
// otherwise means.
func (a *App) recallNext() bool {
	s := a.cur()
	if s == nil || !a.browsing() {
		return false
	}
	if a.histIdx++; a.histIdx >= len(s.history) {
		draft := a.histDraft
		a.endBrowsing()
		a.in.SetValue(draft)
		return true
	}
	a.in.SetValue(s.history[a.histIdx])
	return true
}

// arrowsAreForHistory keeps ↑↓ as cursor movement in a draft that has more
// than one line — there, moving between the lines is the obvious meaning.
// But a walk already under way keeps the arrows whatever the box holds:
// recalling a multi-line message used to strand the walk on it, with the
// down arrow suddenly meaning cursor movement instead of scrolling on.
// Editing is still one keystroke away: typing anything ends the walk.
func (a *App) arrowsAreForHistory() bool {
	if a.focus != focusInput {
		return false
	}
	return a.browsing() || !strings.Contains(a.in.Value(), "\n")
}
