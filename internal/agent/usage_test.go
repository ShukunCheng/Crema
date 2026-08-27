package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeUsageCache plants a .usage_cache.json in a temporary home.
func writeUsageCache(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", usageCacheFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	old := homeDir
	homeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { homeDir = old })
}

// pinClock freezes the freshness test's idea of now.
func pinClock(t *testing.T, at time.Time) {
	t.Helper()
	old := timeNow
	timeNow = func() time.Time { return at }
	t.Cleanup(func() { timeNow = old })
}

// The percentages the CLI's own status line shows come from this file, and so
// do crema's.
func TestUsageReadsTheCLIsOwnCache(t *testing.T) {
	writeUsageCache(t, `{
	  "five_hour":  {"utilization": 12.5, "resets_at": "2026-08-14T18:00:00+02:00"},
	  "seven_day":  {"utilization": 27.0, "resets_at": "2026-08-17T18:00:00+02:00"},
	  "seven_day_opus": null,
	  "extra_usage": {"is_enabled": false, "utilization": null}
	}`)
	pinClock(t, time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC))

	got := (&Claude{}).Usage()
	if len(got) != 2 {
		t.Fatalf("want the two windows that have numbers, got %+v", got)
	}
	if got[0].Label() != "5h" || got[1].Label() != "7d" {
		t.Fatalf("soonest to reset should come first: %+v", got)
	}
	if !got[0].Known || got[0].Utilization != 0.125 {
		t.Fatalf("utilization is a percentage in the file, a fraction here: %+v", got[0])
	}
	if got[1].Utilization != 0.27 {
		t.Fatalf("seven day: %+v", got[1])
	}
}

// Only the interactive CLI refreshes the file — a fortnight of headless runs
// leaves it untouched. A window past its own reset says nothing about now.
func TestAnExpiredWindowIsDroppedRatherThanShownStale(t *testing.T) {
	writeUsageCache(t, `{
	  "five_hour": {"utilization": 97.0, "resets_at": "2026-03-23T14:00:00+01:00"},
	  "seven_day": {"utilization": 27.0, "resets_at": "2026-08-17T18:00:00+02:00"}
	}`)
	pinClock(t, time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC))

	got := (&Claude{}).Usage()
	if len(got) != 1 || got[0].Label() != "7d" {
		t.Fatalf("the five-hour window reset months ago: %+v", got)
	}
}

func TestUsageIsEmptyWithoutACache(t *testing.T) {
	home := t.TempDir()
	old := homeDir
	homeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { homeDir = old })

	if got := (&Claude{}).Usage(); got != nil {
		t.Fatalf("no cache means no claims, got %+v", got)
	}
}

func TestUsageSurvivesAMangledCache(t *testing.T) {
	writeUsageCache(t, `{"five_hour": {"utilization": "lots"}, "seven_day": nul`)
	if got := (&Claude{}).Usage(); got != nil {
		t.Fatalf("a broken cache must not produce numbers: %+v", got)
	}
}

// The status-line payload is the only place the live percentage appears, so
// the bridge has to read it the way the CLI writes it.
func TestParseStatuslineReadsTheLiveAllowance(t *testing.T) {
	got := ParseStatusline([]byte(`{
	  "model": {"id": "claude-opus-5"},
	  "rate_limits": {
	    "seven_day": {"used_percentage": 40, "resets_at": 1786900000},
	    "five_hour": {"used_percentage": 87, "resets_at": 1786710600},
	    "seven_day_opus": {"used_percentage": null, "resets_at": null}
	  }}`))
	if len(got) != 2 {
		t.Fatalf("want the two windows with numbers: %+v", got)
	}
	if got[0].Label() != "5h" || !got[0].Known || got[0].Utilization != 0.87 {
		t.Fatalf("five hour: %+v", got[0])
	}
	if got[0].ResetsAt.Unix() != 1786710600 {
		t.Fatalf("reset not parsed: %v", got[0].ResetsAt)
	}
	if got[1].Label() != "7d" || got[1].Utilization != 0.40 {
		t.Fatalf("seven day: %+v", got[1])
	}
}

func TestParseStatuslineIgnoresAPayloadWithoutLimits(t *testing.T) {
	for _, in := range []string{`{"model":{"id":"x"}}`, `{"rate_limits":null}`, `not json`, ``} {
		if got := ParseStatusline([]byte(in)); got != nil {
			t.Fatalf("ParseStatusline(%q) = %+v", in, got)
		}
	}
}

// What the bridge writes is what crema reads, and it outranks the CLI's own
// cache — it is the newer of the two by construction.
func TestRecordedUsageIsPreferredAndExpires(t *testing.T) {
	dir := t.TempDir()
	old := usagePathOverride
	usagePathOverride = filepath.Join(dir, "usage.json")
	t.Cleanup(func() { usagePathOverride = old })

	writeUsageCache(t, `{"five_hour": {"utilization": 99.0, "resets_at": "2026-08-14T18:00:00+02:00"}}`)
	pinClock(t, time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC))

	live := time.Date(2026, 8, 14, 14, 0, 0, 0, time.UTC)
	if err := RecordUsage([]RateLimit{
		{Type: "five_hour", Utilization: 0.13, Known: true, ResetsAt: live},
		{Type: "seven_day", Utilization: 0.4}, // not Known: nothing to record
	}); err != nil {
		t.Fatal(err)
	}
	got := (&Claude{}).Usage()
	if len(got) != 1 || got[0].Utilization != 0.13 {
		t.Fatalf("the bridge's number should win: %+v", got)
	}

	// Once the window it described has reset, it says nothing about now, and
	// the older cache is asked instead.
	pinClock(t, time.Date(2026, 8, 14, 15, 0, 0, 0, time.UTC))
	if got := (&Claude{}).Usage(); len(got) != 1 || got[0].Utilization != 0.99 {
		t.Fatalf("expired record should fall through to the cache: %+v", got)
	}
}

// The CLI sends a warning line rather than a number once a window is far
// enough along — the one figure a headless run gets when it starts to matter.
func TestAWarningThresholdIsKept(t *testing.T) {
	p := &ClaudeParser{}
	p.ParseLine([]byte(`{"type":"rate_limit_event","session_id":"s","rate_limit_info":` +
		`{"status":"allowed_warning","resetsAt":1786438800,"rateLimitType":"five_hour",` +
		`"surpassedThreshold":0.75}}`))
	evs := p.ParseLine([]byte(`{"type":"result","subtype":"success","session_id":"s"}`))
	rl := evs[0].Result.RateLimit
	if rl.Known {
		t.Fatalf("a threshold is not a utilization: %+v", rl)
	}
	if rl.Surpassed != 0.75 || rl.Status != "allowed_warning" {
		t.Fatalf("rate limit: %+v", rl)
	}
}
