package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/ShukunCheng/Crema/internal/agent"
)

// The one number crema cannot get for itself is how much of the subscription
// is gone. Claude Code knows it — its own status line shows it — but it only
// ever tells a status-line program, and a headless run has no status line.
//
// So crema can stand in front of yours. `crema statusline --then "<your
// command>"` reads the payload the CLI sends, writes the allowance down where
// crema can find it, and runs your real status line with the same input, so
// what you see in an interactive session is unchanged.
//
// It is the CLI's own figure passing through. Nothing here reads a credential
// or calls an API, which is the line crema does not cross.
const statuslineUsage = `crema statusline — record the allowance your interactive sessions are told

  Put your status line in a file of its own, and crema in front of it in
  ~/.claude/settings.json:

    "statusLine": {"type": "command",
                   "command": "crema statusline --then-file <path>"}

  --then <command> works too, but a status line is a shell one-liner with its
  own quoting, and a file spares you escaping it twice.

  With no --then it prints nothing, which is a blank status line — use that
  only if you had none.
`

// runStatusline is the subcommand. It never fails the status line: whatever
// goes wrong with the recording, the wrapped command still runs and its output
// still reaches the CLI.
func runStatusline(args []string) int {
	var then string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--then" && i+1 < len(args):
			i++
			then = args[i]
		case strings.HasPrefix(a, "--then="):
			then = strings.TrimPrefix(a, "--then=")
		case a == "--then-file" && i+1 < len(args):
			i++
			then = readCommand(args[i])
		case strings.HasPrefix(a, "--then-file="):
			then = readCommand(strings.TrimPrefix(a, "--then-file="))
		case a == "-h" || a == "--help":
			fmt.Print(statuslineUsage)
			return 0
		}
	}

	payload, _ := io.ReadAll(os.Stdin) // a short read is still worth parsing
	if w := agent.ParseStatusline(payload); len(w) > 0 {
		if err := agent.RecordUsage(w); err != nil {
			fmt.Fprintln(os.Stderr, "crema: could not record usage:", err)
		}
	}
	if then == "" {
		return 0
	}
	return passThrough(then, payload)
}

// readCommand loads a wrapped status line from a file. A status-line command is
// a shell one-liner with its own quoting — the one in the wild nests single
// quotes inside single quotes to feed awk — and putting that inside another
// command line means escaping it for a shell that has not run yet. Escaping it
// wrongly breaks the status line; a file has no quoting for crema to get
// wrong.
func readCommand(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "crema: status line:", err)
		return ""
	}
	return strings.TrimRight(string(b), "\r\n")
}

// passThrough runs the real status line with the payload it expected on stdin,
// forwarding everything it prints.
func passThrough(command string, stdin []byte) int {
	sh, flag := shell()
	cmd := exec.Command(sh, flag, command)
	cmd.Stdin = strings.NewReader(string(stdin))
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "crema: status line:", err)
		return 1
	}
	return 0
}

// shell picks what to run the wrapped command line with. A status-line command
// is written for a shell — the one in the wild is bash, quotes and all — so
// bash is preferred wherever it exists, including on Windows, where Claude
// Code ships alongside Git's.
func shell() (string, string) {
	for _, name := range []string{"bash", "sh"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, "-c"
		}
	}
	if runtime.GOOS == "windows" {
		return "cmd.exe", "/c"
	}
	return "/bin/sh", "-c"
}
