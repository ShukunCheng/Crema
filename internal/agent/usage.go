package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Claude Code stops short of telling a headless run how much of the
// subscription is gone: the stream's rate_limit_event names the window and
// says when it resets, but carries no percentage (measured against CLI
// 2.1.229 — the utilization field older versions sent is not there any more).
//
// The CLI does keep the numbers, in ~/.claude/.usage_cache.json, which is
// where its own status line reads them from. Crema reads the same file.
//
// The file has no timestamp of its own, and only the interactive CLI refreshes
// it — a week of headless runs leaves it untouched. Its own resets_at is the
// freshness test, and an exact one: a window that has not reset yet is a
// window the number still applies to. Anything past its reset is dropped
// rather than shown stale.
const usageCacheFile = ".usage_cache.json"

type usageWindow struct {
	Utilization *float64 `json:"utilization"` // percent, 0..100
	ResetsAt    string   `json:"resets_at"`   // RFC 3339
}

// Usage reports the account's still-running allowance windows, soonest reset
// first. Empty when nothing local knows the numbers, which the status bar
// shows as "no percentage" rather than as zero.
//
// Crema's own record comes first: it is written by the status-line bridge from
// what an interactive session was told this minute, where .usage_cache.json is
// whatever the CLI last happened to write — on 2.1.229, nothing at all.
func (c *Claude) Usage() []RateLimit {
	if w := recordedUsage(); len(w) > 0 {
		return w
	}
	return c.cachedUsage()
}

func (c *Claude) cachedUsage() []RateLimit {
	home, err := homeDir()
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(home, ".claude", usageCacheFile))
	if err != nil {
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil
	}
	now := timeNow()
	var out []RateLimit
	for name, msg := range raw {
		var w usageWindow
		if err := json.Unmarshal(msg, &w); err != nil || w.Utilization == nil {
			continue // null windows, and extra_usage, which is a different shape
		}
		at, err := time.Parse(time.RFC3339, w.ResetsAt)
		if err != nil || !at.After(now) {
			continue // reset already; whatever has been used since is unknown
		}
		out = append(out, RateLimit{
			Type: name, Utilization: *w.Utilization / 100, Known: true, ResetsAt: at,
		})
	}
	// Soonest to reset first, which is also shortest window first: the five
	// hour one is the one about to bite.
	sort.Slice(out, func(i, j int) bool { return out[i].ResetsAt.Before(out[j].ResetsAt) })
	return out
}

// timeNow is a seam so the freshness test can be tested.
var timeNow = time.Now

// Claude Code hands its status-line program a JSON payload that includes the
// live allowance — the numbers the CLI's own status line shows. A headless run
// is never given it, so crema can be put in front of that program to write the
// numbers down as they go past. Nothing here talks to the API or touches a
// credential: it is the CLI's own figure, passing through.
type statuslinePayload struct {
	RateLimits map[string]struct {
		UsedPercentage *float64 `json:"used_percentage"`
		ResetsAt       *int64   `json:"resets_at"` // unix seconds
	} `json:"rate_limits"`
}

// ParseStatusline pulls the allowance windows out of a status-line payload.
func ParseStatusline(b []byte) []RateLimit {
	var p statuslinePayload
	if err := json.Unmarshal(b, &p); err != nil {
		return nil
	}
	var out []RateLimit
	for name, w := range p.RateLimits {
		if w.UsedPercentage == nil {
			continue
		}
		r := RateLimit{Type: name, Utilization: *w.UsedPercentage / 100, Known: true}
		if w.ResetsAt != nil && *w.ResetsAt > 0 {
			r.ResetsAt = time.Unix(*w.ResetsAt, 0)
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ResetsAt.Before(out[j].ResetsAt) })
	return out
}

// usageRecord is one window as crema writes it down.
type usageRecord struct {
	Type        string    `json:"type"`
	Utilization float64   `json:"utilization"` // 0..1
	ResetsAt    time.Time `json:"resets_at"`
}

// usagePathOverride lets tests keep out of the real config directory.
var usagePathOverride string

// UsagePath is where crema keeps what the status-line bridge told it, beside
// the state file rather than in the CLI's directory: it is crema's note, not
// the CLI's.
func UsagePath() string {
	if usagePathOverride != "" {
		return usagePathOverride
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "crema", "usage.json")
}

// RecordUsage writes the windows down for the next crema to read, merged
// over what is already there: the writers are the status-line bridge and
// every turn's own rate_limit_event, and either may know about a window the
// other's last report skipped. Replacing wholesale let a payload that
// happened to carry only the weekly window erase a five-hour value that was
// perfectly current. Windows past their reset are dropped, not kept stale.
// Written atomically, since a status line runs often and crema may read at
// any moment.
func RecordUsage(windows []RateLimit) error {
	p := UsagePath()
	if p == "" {
		return errors.New("no user config directory")
	}
	now := timeNow()
	merged := map[string]RateLimit{}
	for _, w := range recordedUsage() { // already drops the expired
		merged[w.Type] = w
	}
	for _, w := range windows {
		if w.Known && (w.ResetsAt.IsZero() || w.ResetsAt.After(now)) {
			merged[w.Type] = w
		}
	}
	recs := make([]usageRecord, 0, len(merged))
	for _, w := range merged {
		recs = append(recs, usageRecord{w.Type, w.Utilization, w.ResetsAt})
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].ResetsAt.Before(recs[j].ResetsAt) })
	b, err := json.Marshal(recs)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(p+".tmp", b, 0o600); err != nil {
		return err
	}
	return os.Rename(p+".tmp", p)
}

// recordedUsage reads back what the bridge wrote, dropping windows that have
// since reset — the same freshness rule the CLI's own cache gets, and for the
// same reason: nobody has said what has been used since.
func recordedUsage() []RateLimit {
	b, err := os.ReadFile(UsagePath())
	if err != nil {
		return nil
	}
	var recs []usageRecord
	if err := json.Unmarshal(b, &recs); err != nil {
		return nil
	}
	now := timeNow()
	var out []RateLimit
	for _, r := range recs {
		if r.ResetsAt.After(now) {
			out = append(out, RateLimit{
				Type: r.Type, Utilization: r.Utilization, Known: true, ResetsAt: r.ResetsAt,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ResetsAt.Before(out[j].ResetsAt) })
	return out
}
