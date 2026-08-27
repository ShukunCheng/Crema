package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// consoleFontGlyphs is every non-ASCII rune the interface is allowed to draw.
//
// Crema's Windows shortcut runs it under the console host in Cascadia Mono
// (see scripts/shortcut.ps1), and the console host's font fallback is poor: a
// rune the face lacks comes out as a box, and a fallback face can pick a
// different advance width, which shifts every column after it on that row.
// Windows Terminal hides both problems, so a bad glyph is easy to add and only
// shows up for someone launching from the shortcut.
//
// Every entry below was checked against the face's CharacterToGlyphMap. To add
// one, check it the same way first:
//
//	Add-Type -AssemblyName PresentationCore
//	$f = [Windows.Media.Fonts]::SystemTypefaces | Where-Object {
//	    $_.FontFamily.Source -eq 'Cascadia Mono' -and $_.Style -eq 'Normal' -and
//	    $_.Weight.ToOpenTypeWeight() -eq 400 } | Select-Object -First 1
//	$g = $null; [void]$f.TryGetGlyphTypeface([ref]$g)
//	$g.CharacterToGlyphMap.ContainsKey(0x25B6)
//
// Notable absences, all of which used to be in here: ⏵ U+23F5, ✳ U+2733,
// ✔ U+2714, ✖ U+2716, ✚ U+271A, ➜ U+279C.
var consoleFontGlyphs = []rune(
	"·×—…›" + // punctuation and separators
		"←↑→↓−" + // arrows and the diff's minus
		"─│" + // box drawing: rails and rules
		"▸▾●◨▌" + // markers: folded, open, agent state, split view, text cursor
		"✓⨯❯" + // ok, canceled, prompt
		"∙", // spinner.Dot, which comes from bubbles rather than this package
)

// TestUIUsesOnlyConsoleFontGlyphs walks the package's own source rather than a
// rendered frame, so it also covers strings behind branches a test never hits.
func TestUIUsesOnlyConsoleFontGlyphs(t *testing.T) {
	allowed := map[rune]bool{}
	for _, r := range consoleFontGlyphs {
		allowed[r] = true
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		for i, r := range string(src) {
			if r < 128 || allowed[r] {
				continue
			}
			line := 1 + strings.Count(string(src[:i]), "\n")
			t.Errorf("%s:%d: %q (U+%04X) is not known to exist in Cascadia Mono — "+
				"verify it and add it to consoleFontGlyphs, or use one already there",
				f, line, r, r)
		}
	}
	if checked == 0 {
		t.Fatal("no source files scanned — the glob or the working directory is wrong")
	}
}
