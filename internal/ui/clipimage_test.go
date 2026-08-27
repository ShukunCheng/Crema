package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// stubClipboard makes the clipboard answer with a picture, some text, or
// neither, without needing a real one.
func stubClipboard(t *testing.T, image string, imgErr error, text string, textErr error) {
	t.Helper()
	prevImg, prevText := readClipImage, readClipboard
	readClipImage = func(string) (string, error) { return image, imgErr }
	readClipboard = func() (string, error) { return text, textErr }
	t.Cleanup(func() { readClipImage, readClipboard = prevImg, prevText })
}

// A pasted picture shows as a label, not a mouthful of temp directory.
func TestPastingAnImageShowsAMarker(t *testing.T) {
	a := testApp(t)
	stubClipboard(t, `C:\tmp\crema-images\paste-1.png`, nil, "", nil)

	typeRunes(t, a, "what is wrong here? ")
	press(t, a, tea.KeyMsg{Type: tea.KeyCtrlV})

	if got, want := a.in.Value(), "what is wrong here? [Image #1] "; got != want {
		t.Fatalf("draft = %q, want %q", got, want)
	}
	if !strings.Contains(a.note, "paste-1.png") {
		t.Fatalf("note = %q, want the file named", a.note)
	}
	if a.cur().busy {
		t.Fatal("pasting must not send anything")
	}
}

// The label is for the reader; the agent is given the file. The conversation
// keeps the label, so the transcript reads the way the input box did.
func TestSendingExpandsTheMarkersToPaths(t *testing.T) {
	a := testApp(t)
	stubClipboard(t, `C:\tmp\one.png`, nil, "", nil)
	typeRunes(t, a, "compare ")
	press(t, a, tea.KeyMsg{Type: tea.KeyCtrlV})
	stubClipboard(t, `C:\tmp\two.png`, nil, "", nil)
	typeRunes(t, a, "with ")
	press(t, a, tea.KeyMsg{Type: tea.KeyCtrlV})

	if got, want := a.in.Value(), "compare [Image #1] with [Image #2] "; got != want {
		t.Fatalf("draft = %q, want %q", got, want)
	}
	press(t, a, kmsg(tea.KeyEnter))

	s := a.cur()
	if got, want := s.lastOpts.Prompt, `compare C:\tmp\one.png with C:\tmp\two.png`; got != want {
		t.Fatalf("the agent was sent %q, want %q", got, want)
	}
	last := s.tl.blocks[len(s.tl.blocks)-1]
	if last.Kind != BlockUser || last.Text != "compare [Image #1] with [Image #2]" {
		t.Fatalf("the conversation shows %+v, want the markers", last)
	}
	if a.images != nil {
		t.Fatal("the markers went with the draft; the next paste starts again at #1")
	}
	s.close()
}

func TestExpandImages(t *testing.T) {
	images := []string{`C:\tmp\one.png`, `C:\a folder\two.png`}
	cases := []struct{ draft, want string }{
		{"look at [Image #1]", `look at C:\tmp\one.png`},
		// A path with a space in it is quoted, so it still reads as one thing.
		{"and [Image #2] too", `and "C:\a folder\two.png" too`},
		{"[Image #1][Image #2]", `C:\tmp\one.png"C:\a folder\two.png"`},
		// Nothing behind it: left exactly as the user typed it.
		{"see [Image #9]", "see [Image #9]"},
		{"no markers here", "no markers here"},
	}
	for _, c := range cases {
		if got := expandImages(c.draft, images); got != c.want {
			t.Fatalf("expandImages(%q) = %q, want %q", c.draft, got, c.want)
		}
	}
	if got := expandImages("[Image #1]", nil); got != "[Image #1]" {
		t.Fatalf("with no images = %q, want it untouched", got)
	}
}

// With no picture on the clipboard, ctrl+v is an ordinary paste.
func TestPastingTextWhenThereIsNoImage(t *testing.T) {
	a := testApp(t)
	stubClipboard(t, "", errNoImage, "some copied text", nil)

	press(t, a, tea.KeyMsg{Type: tea.KeyCtrlV})
	if got := a.in.Value(); got != "some copied text" {
		t.Fatalf("draft = %q", got)
	}
}

// A clipboard that holds a picture crema can't read says so, and doesn't
// quietly paste something else instead.
func TestAnUnreadableImageIsReported(t *testing.T) {
	a := testApp(t)
	stubClipboard(t, "", errors.New("8 bits per pixel"), "fallback text", nil)

	press(t, a, tea.KeyMsg{Type: tea.KeyCtrlV})
	if a.in.Value() != "" {
		t.Fatalf("draft = %q, want it left alone", a.in.Value())
	}
	if !strings.Contains(a.note, "8 bits per pixel") {
		t.Fatalf("note = %q, want the reason", a.note)
	}
}

// Pasting while another pane has the focus brings it back, like typing does.
func TestPastingTakesTheFocusBack(t *testing.T) {
	a := testApp(t)
	stubClipboard(t, "", errNoImage, "text", nil)
	a.Update(kmsg(tea.KeyCtrlO)) // focus the conversation
	press(t, a, tea.KeyMsg{Type: tea.KeyCtrlV})
	if a.focus != focusInput {
		t.Fatalf("focus = %v, want the input", a.focus)
	}
	if a.in.Value() != "text" {
		t.Fatalf("draft = %q", a.in.Value())
	}
}

func TestImageDirIsOutsideTheProject(t *testing.T) {
	dir := imageDir()
	if !strings.HasPrefix(dir, os.TempDir()) {
		t.Fatalf("imageDir = %q, want it under %q", dir, os.TempDir())
	}
	if filepath.Base(dir) == "" {
		t.Fatal("imageDir must name a folder of its own")
	}
}
