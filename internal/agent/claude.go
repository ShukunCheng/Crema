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

// NewClaude pins the one-hour prompt-cache lifetime. This is belt and
// braces, not a fix: measured on real traffic, a subscriber's headless runs
// were already getting the hour — every cache write in a long crema session
// was filed as ephemeral_1h, both before this flag existed and after. It is
// kept because the CLI's default rests on a subscriber check that could
// change, and asking costs nothing.
//
// What the cache actually costs crema is elsewhere, and is not fixable from
// out here: one turn is one `claude -p --resume` process, and a resumed
// process rebuilds far more of the prefix than a live one keeps. Measured
// over 92 crema turns against 65 interactive ones, the first call of a crema
// turn rewrote 59% of its prefix where an interactive turn rewrote 11%. The
// cure for that is one long-lived process fed by --input-format stream-json,
// not an environment variable.
func NewClaude() *Claude {
	return &Claude{bin: "claude", extraEnv: []string{"ENABLE_PROMPT_CACHING_1H=1"}}
}

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
