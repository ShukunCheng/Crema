//go:build !windows

package main

// Everywhere else the terminal emulator owns the window and its taskbar entry,
// and there is no second host to start under: a program cannot hand itself a
// window. consoleNow reports a state that never asks for one.
func consoleNow() consoleState { return consoleState{} }

func startInOwnWindow() error { return nil }
