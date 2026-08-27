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
	// In the order the CLI's own /model picker lists them.
	return []string{DefaultModel, "opus", "fable", "sonnet", "haiku"}
}

// modelNotes is what each alias resolves to and what it is for, in the CLI's
// own words: copied from what `/model` printed on Claude Code 2.1.229 rather
// than invented here. A headless run is never told any of this, so crema
// cannot read it — which means it can go stale when Anthropic ships a new
// generation. Nothing depends on it beyond the line under the name.
var modelNotes = map[string]string{
	"opus":   "Opus 5 with 1M context · best for everyday, complex tasks",
	"fable":  "Fable 5 · most capable for your hardest and longest-running tasks",
	"sonnet": "Sonnet 5 · efficient for routine tasks",
	"haiku":  "Haiku 4.5 · fastest for quick answers",
}

// DescribeModel says what an alias will get you, or "" when crema has nothing
// to add. Implements the optional ModelDescriber, so a backend without notes
// simply lists its names.
func (c *Claude) DescribeModel(model string) string {
	if model == DefaultModel {
		// What that resolves to is the CLI's own configuration, which a
		// headless run is not told — so crema does not guess at it.
		return "whatever the CLI is configured to use"
	}
	return modelNotes[model]
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
