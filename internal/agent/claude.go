package agent

import (
	"context"
	"fmt"
	"os/exec"
)

// Claude drives the official Claude Code CLI headlessly. Auth stays inside the
// CLI (subscription login); crema never reads or passes any credential.
type Claude struct {
	bin      string
	extraEnv []string
}

func NewClaude() *Claude { return &Claude{bin: "claude"} }

func (c *Claude) Name() string  { return "claude" }
func (c *Claude) Label() string { return "Claude Code" }

func (c *Claude) Available() error {
	if _, err := exec.LookPath(c.bin); err != nil {
		return fmt.Errorf("claude CLI not found — install Claude Code and run `claude` once to log in")
	}
	return nil
}

// Modes are the four the CLI's --permission-mode accepts that make sense
// headlessly, least permissive first.
func (c *Claude) Modes() []PermissionMode {
	return []PermissionMode{PermissionDefault, PermissionPlan, PermissionAcceptEdits, PermissionFull}
}

// Models are the CLI's aliases, which always track the current release —
// pinning a dated model id here would go stale.
func (c *Claude) Models() []string {
	return []string{DefaultModel, "opus", "sonnet", "haiku", "fable"}
}

// claudeMode maps crema's mode onto the CLI's --permission-mode values.
func claudeMode(p PermissionMode) string {
	switch p {
	case PermissionAcceptEdits:
		return "acceptEdits"
	case PermissionFull:
		return "bypassPermissions"
	case PermissionPlan:
		return "plan"
	default:
		return ""
	}
}

func (c *Claude) args(opts RunOptions) []string {
	a := []string{
		"-p", opts.Prompt,
		"--output-format", "stream-json",
		"--verbose", // required by the CLI when combining -p with stream-json
	}
	if m := claudeMode(opts.Permission); m != "" {
		a = append(a, "--permission-mode", m)
	}
	if opts.Model != DefaultModel {
		a = append(a, "--model", opts.Model)
	}
	if opts.SessionID != "" {
		a = append(a, "--resume", opts.SessionID)
	}
	return a
}

func (c *Claude) Run(ctx context.Context, opts RunOptions) (<-chan Event, error) {
	return runCLI(ctx, c.bin, c.args(opts), opts.Dir, c.extraEnv, &ClaudeParser{})
}
