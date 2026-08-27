//go:build !windows

package ui

import "errors"

// Reading a picture off the clipboard is done through the window system, and
// crema has no window: on Windows there is a clipboard API to call, while
// elsewhere it would mean talking to X11, Wayland or pbpaste. Type or drag the
// path in instead — the agents read a file just as happily.
var errNoImage = errors.New("no image on the clipboard")

func clipboardImage(string) (string, error) { return "", errNoImage }
