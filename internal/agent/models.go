package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Crema's model list is aliases the CLI resolves for itself — opus, sonnet —
// so a new generation under an existing name is picked up for free, and
// "default" (no --model flag at all) always follows whatever the CLI decides.
// What that misses is a genuinely new name: when Fable arrived, no alias
// crema knew about pointed at it.
//
// The CLI writes the options it is offering into ~/.claude.json, label and
// description included, which is exactly what a picker wants. Crema merges
// that in. It is additive on purpose: the cache holds only the *extra*
// options beyond the base lineup — measured, a single Fable entry — so
// trusting it as the whole list would empty the picker of opus and haiku.
// Gaining entries, never losing them.

// claudeConfigFile is where the CLI keeps its own state, beside the settings.
const claudeConfigFile = ".claude.json"

// offeredModels is the cache's shape. Value is what --model takes.
type offeredModel struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// modelCacheTTL keeps repeated pickers off the disk without going stale for
// long: the file only changes when the CLI itself is run.
const modelCacheTTL = time.Minute

var (
	modelsMu   sync.Mutex
	modelsAt   time.Time
	modelsSeen []offeredModel
)

// offered reads the CLI's cached model options, memoised briefly. Any problem
// — no file, unreadable, a shape crema doesn't recognise — yields nothing,
// which leaves the built-in aliases exactly as they were.
func offered() []offeredModel {
	modelsMu.Lock()
	defer modelsMu.Unlock()
	if time.Since(modelsAt) < modelCacheTTL {
		return modelsSeen
	}
	modelsAt = timeNow()
	modelsSeen = nil
	home, err := homeDir()
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(home, claudeConfigFile))
	if err != nil {
		return nil
	}
	var cfg struct {
		Options []offeredModel `json:"additionalModelOptionsCache"`
	}
	if json.Unmarshal(b, &cfg) != nil {
		return nil
	}
	for _, o := range cfg.Options {
		if o.Value != "" {
			modelsSeen = append(modelsSeen, o)
		}
	}
	return modelsSeen
}

// baseAliases is the lineup crema knows by name. Aliases rather than model
// ids, because the CLI resolves an alias to its current generation: "opus"
// meant Opus 4 when this was written and means Opus 5 now, with no change
// here.
var baseAliases = []string{"opus", "fable", "sonnet", "haiku"}

// aliasFor reduces an offered value to the alias it is already covered by, so
// a cached "claude-fable-5[1m]" doesn't appear beside crema's own "fable".
func aliasFor(value string) string {
	v := strings.ToLower(value)
	for _, a := range baseAliases {
		if strings.Contains(v, a) {
			return a
		}
	}
	return ""
}

// Models lists selectable aliases: crema's own, plus anything the CLI is
// offering that none of them covers.
func (c *Claude) Models() []string {
	// In the order the CLI's own /model picker lists them.
	out := append([]string{DefaultModel}, baseAliases...)
	for _, o := range offered() {
		if aliasFor(o.Value) == "" {
			out = append(out, o.Value)
		}
	}
	return out
}

// modelNotes is what each alias resolves to and what it is for, in the CLI's
// own words: copied from what `/model` printed on Claude Code 2.1.229. The
// generations move on, so DescribeModel prefers whatever the CLI is saying
// about a model today and falls back to this.
var modelNotes = map[string]string{
	"opus":   "Opus with 1M context · best for everyday, complex tasks",
	"fable":  "Fable · most capable for your hardest and longest-running tasks",
	"sonnet": "Sonnet · efficient for routine tasks",
	"haiku":  "Haiku · fastest for quick answers",
}

// DescribeModel says what an alias will get you. The CLI's own description
// wins when it has one — it names the generation, which crema cannot know —
// and the built-in note is the fallback.
func (c *Claude) DescribeModel(model string) string {
	if model == DefaultModel {
		// What that resolves to is the CLI's own decision, made fresh on
		// every run: org policy, then ANTHROPIC_DEFAULT_MODEL, then your
		// account's tier. Crema passes no --model at all and follows it.
		return "whatever the CLI resolves — today's best model for your account"
	}
	for _, o := range offered() {
		if o.Value == model || aliasFor(o.Value) == model {
			if o.Description != "" {
				return strings.TrimSpace(o.Description)
			}
			return o.Label
		}
	}
	return modelNotes[model]
}
