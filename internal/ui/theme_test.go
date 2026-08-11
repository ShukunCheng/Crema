package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// restoreTheme puts the global palette back so theme tests don't leak.
func restoreTheme(t *testing.T) {
	t.Helper()
	prev := CurrentMode()
	t.Cleanup(func() { SetMode(prev) })
}

func TestToggleModeFlipsBackAndForth(t *testing.T) {
	restoreTheme(t)
	SetMode(ModeDark)
	if ToggleMode() != ModeLight || CurrentMode() != ModeLight {
		t.Fatal("dark should toggle to light")
	}
	if T != LightTheme {
		t.Fatal("the active palette must follow the mode")
	}
	if ToggleMode() != ModeDark || T != DarkTheme {
		t.Fatal("light should toggle back to dark")
	}
}

func TestModeString(t *testing.T) {
	if ModeLight.String() != "light" || ModeDark.String() != "dark" {
		t.Fatal("mode names are user-visible in the status bar")
	}
}

func TestLightAndDarkPalettesDiffer(t *testing.T) {
	if LightTheme == DarkTheme {
		t.Fatal("the two palettes must not be identical")
	}
	// Every slot must be set in both, or a switch would render invisible text.
	for name, pair := range map[string][2]lipgloss.Color{
		"Pink":    {DarkTheme.Pink, LightTheme.Pink},
		"Magenta": {DarkTheme.Magenta, LightTheme.Magenta},
		"Purple":  {DarkTheme.Purple, LightTheme.Purple},
		"Lilac":   {DarkTheme.Lilac, LightTheme.Lilac},
		"Muted":   {DarkTheme.Muted, LightTheme.Muted},
		"Fg":      {DarkTheme.Fg, LightTheme.Fg},
		"Green":   {DarkTheme.Green, LightTheme.Green},
		"Red":     {DarkTheme.Red, LightTheme.Red},
		"Yellow":  {DarkTheme.Yellow, LightTheme.Yellow},
		"Surface": {DarkTheme.Surface, LightTheme.Surface},
	} {
		if pair[0] == "" || pair[1] == "" {
			t.Fatalf("%s is unset in one of the palettes", name)
		}
		if pair[0] == pair[1] {
			t.Fatalf("%s is the same in both palettes", name)
		}
	}
}

func TestCtrlLTogglesThemeAndRepaintsCachedViews(t *testing.T) {
	restoreTheme(t)
	SetMode(ModeDark)
	// Real colors, so a repaint is observable in the rendered escape codes.
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	a := testApp(t)
	a.cur().tl.Append(Block{Kind: BlockAssistant, Text: "some output"})
	dark := a.cur().tl.Content()

	a.Update(kmsg(tea.KeyCtrlL))

	if CurrentMode() != ModeLight {
		t.Fatal("ctrl+l must switch the mode")
	}
	light := a.cur().tl.Content()
	if light == dark {
		t.Fatal("the timeline still holds the old palette — Invalidate did not run")
	}
	if !strings.Contains(light, "some output") {
		t.Fatal("repainting must preserve the text")
	}
	if !strings.Contains(a.note, "light") {
		t.Fatalf("the status bar should report the new mode: %q", a.note)
	}
}
