//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                = windows.NewLazySystemDLL("kernel32.dll")
	procGetConsoleWindow    = kernel32.NewProc("GetConsoleWindow")
	procGetConsoleProcesses = kernel32.NewProc("GetConsoleProcessList")

	user32            = windows.NewLazySystemDLL("user32.dll")
	procGetClassNameW = user32.NewProc("GetClassNameW")
)

// consoleWindowClass is the class of a classic console window — one crema has
// to itself. A terminal that draws crema in its own window leaves a stand-in
// behind this handle instead: Windows Terminal's is a PseudoConsoleWindow.
// Both are marked visible, so the class is what tells them apart.
const consoleWindowClass = "ConsoleWindowClass"

func consoleNow() consoleState {
	st := consoleState{child: relaunched(), terminal: stdoutIsTerminal()}
	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd == 0 {
		return st
	}
	st.console = true
	st.owned = windowClass(hwnd) == consoleWindowClass

	// One process attached to the console means nobody typed a command to get
	// here: no shell is sharing it.
	var pids [4]uint32
	n, _, _ := procGetConsoleProcesses.Call(
		uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	st.alone = n == 1
	return st
}

// startInOwnWindow runs crema again under the classic console host, which is
// what gives it a window nobody else is sharing. The child is handed the same
// arguments and working directory, so it picks up exactly where this one would
// have.
func startInOwnWindow() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	conhost := filepath.Join(os.Getenv("SystemRoot"), "System32", "conhost.exe")
	if _, err := os.Stat(conhost); err != nil {
		return fmt.Errorf("no console host to open a window with: %w", err)
	}
	cmd := exec.Command(conhost, append([]string{exe}, os.Args[1:]...)...)
	cmd.Env = append(os.Environ(), relaunchEnv+"=1")
	// Detached, not "new console": the console host's whole job is to make a
	// console, and handing it one to start with leaves it with nothing to host
	// — the window never appears. Started this way it behaves as it does when
	// the shell launches it, which is what the shortcut does too. No waiting
	// either; this process is about to exit and the window has to outlive it.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func windowClass(hwnd uintptr) string {
	buf := make([]uint16, 256)
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

func stdoutIsTerminal() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
