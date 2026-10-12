package agent

import (
	"strings"
	"testing"
)

// The one failure crema can tell you how to fix. The subscription login is an
// OAuth pair — a short-lived access token and a refresh token to renew it —
// and when the refresh fails there is nothing crema can do from here, because
// it never handles credentials. So it names the fix rather than leaving the
// CLI's sentence sitting on its own.
func TestALoginFailureSaysWhatToDoAboutIt(t *testing.T) {
	for _, stderr := range []string{
		"Failed to authenticate: OAuth session expired and could not be refreshed",
		"Error: unauthorized",
		"Please log in to continue",
		"invalid credentials",
	} {
		if got := loginHint(stderr); got == "" {
			t.Errorf("no hint for %q", stderr)
		} else if !strings.Contains(got, "claude") {
			t.Errorf("the hint should name the command that fixes it: %q", got)
		}
	}
	// Anything else is not a login problem and must not be dressed up as one.
	for _, stderr := range []string{
		"", "panic: runtime error: index out of range",
		"ENOSPC: no space left on device",
	} {
		if got := loginHint(stderr); got != "" {
			t.Errorf("%q is not a login failure, got hint %q", stderr, got)
		}
	}
}

// A process that dies before it produces a single event used to take the
// reason with it: inTurn was false, read returned in silence, and the stderr
// that explained the failure was dropped. An auth failure is exactly that
// shape — it prints one line and exits.
func TestAProcessThatDiesEarlyStillSaysWhy(t *testing.T) {
	cv := &claudeConv{tail: &tailBuffer{max: 8 * 1024}}
	cv.tail.Write([]byte("Failed to authenticate: OAuth session expired and could not be refreshed\n"))

	got := cv.deathNote(nil)
	if !strings.Contains(got, "OAuth session expired") {
		t.Fatalf("the CLI's own words should survive: %q", got)
	}
	if !strings.Contains(got, "Run `claude` in a terminal") {
		t.Fatalf("and the way out should be named: %q", got)
	}

	// A process that ended cleanly with nothing to say says nothing: the CLI
	// exiting between turns is ordinary, not a fault.
	quiet := &claudeConv{tail: &tailBuffer{max: 1024}}
	if got := quiet.deathNote(nil); got != "" {
		t.Fatalf("a clean exit is not news: %q", got)
	}
}
