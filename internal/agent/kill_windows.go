//go:build windows

package agent

import (
	"fmt"
	"os/exec"
	"strconv"
)

func setSysProcAttr(cmd *exec.Cmd) {}

// killTree kills the whole process tree: claude/codex spawn node and helper
// children that would otherwise survive a cancel.
func killTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
	if out, err := kill.CombinedOutput(); err != nil {
		return fmt.Errorf("taskkill: %v (%s)", err, out)
	}
	return nil
}
