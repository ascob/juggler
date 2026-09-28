//     ▄▄ ▄▄ ▄▄  ▄▄▄▄  ▄▄▄▄ ▄▄    ▄▄▄▄▄ ▄▄▄▄
//     ██ ██ ██ ██ ▄▄ ██ ▄▄ ██    ██▄▄  ██▄█▄   Copyright (c) 2026 Julian Storer
//   ▄▄█▀ ▀███▀ ▀███▀ ▀███▀ ██▄▄▄ ██▄▄▄ ██ ██   AGPL-3.0-or-later - see LICENSE

package opencodego

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func floatPtr(v float64) *float64 { return &v }

func derefPct(p *float64) float64 {
	if p == nil {
		return -1
	}
	return *p
}

// TestBuildUsageStatsMapsTheThreeWindows pins the wire → row mapping: the
// plan's rolling/weekly/monthly windows become the three rows the user reads,
// shortest first, each carrying its own window length and reset.
func TestBuildUsageStatsMapsTheThreeWindows(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	stats := buildUsageStats(usageResponse{Usage: map[string]usageWindow{
		"monthly": {Status: "ok", Percent: floatPtr(4), ResetsAt: "2026-10-22T02:15:39.000Z"},
		"rolling": {Status: "ok", Percent: floatPtr(2), ResetsAt: "2026-09-27T22:02:06.247Z"},
		"weekly":  {Status: "ok", Percent: floatPtr(8), ResetsAt: "2026-09-28T00:00:00.000Z"},
	}}, now)

	if stats.Provider != "opencodego" || !stats.UpdatedAt.Equal(now) {
		t.Fatalf("unexpected stats header: %+v", stats)
	}
	if len(stats.Stats) != 3 {
		t.Fatalf("got %d stats, want 3: %+v", len(stats.Stats), stats.Stats)
	}

	want := []struct {
		name       string
		category   string
		percent    float64
		windowSecs int
		resetsAt   time.Time
	}{
		{"5-hour", "primary", 2, 5 * 60 * 60, time.Date(2026, 9, 27, 22, 2, 6, 247_000_000, time.UTC)},
		{"Weekly", "weekly", 8, 7 * 24 * 60 * 60, time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)},
		{"Monthly", "monthly", 4, 0, time.Date(2026, 10, 22, 2, 15, 39, 0, time.UTC)},
	}
	for i, w := range want {
		got := stats.Stats[i]
		if got.Name != w.name || got.Category != w.category || got.WindowSecs != w.windowSecs {
			t.Errorf("stat[%d] = %+v, want name=%q category=%q windowSecs=%d", i, got, w.name, w.category, w.windowSecs)
		}
		if derefPct(got.UsedPercent) != w.percent {
			t.Errorf("stat[%d].UsedPercent = %v, want %v", i, derefPct(got.UsedPercent), w.percent)
		}
		if got.ResetsAt == nil || !got.ResetsAt.Equal(w.resetsAt) {
			t.Errorf("stat[%d].ResetsAt = %v, want %v", i, got.ResetsAt, w.resetsAt)
		}
		if got.Detail != "" {
			t.Errorf("stat[%d].Detail = %q, want empty for an ok window", i, got.Detail)
		}
	}
}

// TestBuildUsageStatsKeepsUnknownWindowAndSkipsUnreportedPercent covers the two
// halves of "don't lose what upstream says": a window this build has never
// heard of still becomes a row, while a window without a percentage is skipped
// rather than drawn as a genuine 0%.
func TestBuildUsageStatsKeepsUnknownWindowAndSkipsUnreportedPercent(t *testing.T) {
	stats := buildUsageStats(usageResponse{Usage: map[string]usageWindow{
		"rolling": {Status: "ok", Percent: floatPtr(1)},
		"daily":   {Status: "ok", Percent: floatPtr(63)},
		"yearly":  {Status: "ok"},
	}}, time.Unix(0, 0))

	if len(stats.Stats) != 2 {
		t.Fatalf("got %d stats, want 2 (the unreported window must be skipped): %+v", len(stats.Stats), stats.Stats)
	}
	if stats.Stats[0].Name != "5-hour" {
		t.Errorf("stat[0].Name = %q, want the known window first", stats.Stats[0].Name)
	}
	if stats.Stats[1].Name != "Daily" || derefPct(stats.Stats[1].UsedPercent) != 63 {
		t.Errorf("unknown window = %+v, want a humanised Daily row", stats.Stats[1])
	}

	// Humanising uppercases the first rune, not the first byte: a non-ASCII key
	// must not come out mangled.
	nonASCII := buildUsageStats(usageResponse{Usage: map[string]usageWindow{
		"échéance": {Status: "ok", Percent: floatPtr(1)},
	}}, time.Unix(0, 0))
	if len(nonASCII.Stats) != 1 || nonASCII.Stats[0].Name != "Échéance" {
		t.Errorf("non-ASCII window = %+v, want a single Échéance row", nonASCII.Stats)
	}
}

// TestBuildUsageStatsSurfacesNonOKStatus: upstream's own word for a window is
// shown rather than interpreted, so a window it reports as not-ok is visible
// instead of looking identical to a healthy one.
func TestBuildUsageStatsSurfacesNonOKStatus(t *testing.T) {
	stats := buildUsageStats(usageResponse{Usage: map[string]usageWindow{
		"rolling": {Status: "limited", Percent: floatPtr(100)},
	}}, time.Unix(0, 0))

	if len(stats.Stats) != 1 {
		t.Fatalf("got %d stats, want 1: %+v", len(stats.Stats), stats.Stats)
	}
	if stats.Stats[0].Detail != "limited" {
		t.Fatalf("Detail = %q, want the upstream status", stats.Stats[0].Detail)
	}
}

// liveUsagePayload is the response https://opencode.ai/zen/go/v1/usage returned
// to a real bearer key on 2026-09-27, kept verbatim. The other tests build their
// payloads from the same structs the parser reads, so a renamed field or a moved
// window key would decode to zero rows and still pass; this one fails instead,
// which is what makes the parse contract checkable rather than assumed.
const liveUsagePayload = `{"usage":{"rolling":{"status":"ok","percent":4,"resetsAt":"2026-09-28T03:04:59.200Z"},"weekly":{"status":"ok","percent":0,"resetsAt":"2026-10-05T00:00:00.000Z"},"monthly":{"status":"ok","percent":6,"resetsAt":"2026-10-22T02:15:39.000Z"}}}`

// TestBuildUsageStatsDecodesTheLivePayload pins the three windows, their keys and
// their field names against a captured response: rolling/weekly/monthly arrive as
// the rows the user reads, a genuine 0% week stays a row (rather than being
// dropped as unreported), and each reset is parsed.
func TestBuildUsageStatsDecodesTheLivePayload(t *testing.T) {
	var parsed usageResponse
	if err := json.Unmarshal([]byte(liveUsagePayload), &parsed); err != nil {
		t.Fatalf("decode the captured payload: %v", err)
	}
	if len(parsed.Usage) != 3 {
		t.Fatalf("captured payload decoded to %d windows, want 3 — the wire shape has changed", len(parsed.Usage))
	}

	stats := buildUsageStats(parsed, time.Unix(0, 0).UTC())
	if len(stats.Stats) != 3 {
		t.Fatalf("got %d stats, want 3: %+v", len(stats.Stats), stats.Stats)
	}
	for i, want := range []struct {
		name    string
		percent float64
		reset   string
	}{
		{"5-hour", 4, "2026-09-28T03:04:59.2Z"},
		{"Weekly", 0, "2026-10-05T00:00:00Z"},
		{"Monthly", 6, "2026-10-22T02:15:39Z"},
	} {
		got := stats.Stats[i]
		if got.Name != want.name || derefPct(got.UsedPercent) != want.percent {
			t.Errorf("stat[%d] = %q %.1f%%, want %q %.0f%%", i, got.Name, derefPct(got.UsedPercent), want.name, want.percent)
		}
		if got.ResetsAt == nil || got.ResetsAt.Format(time.RFC3339Nano) != want.reset {
			t.Errorf("stat[%d] reset = %v, want %s", i, got.ResetsAt, want.reset)
		}
		if got.Detail != "" {
			t.Errorf("stat[%d] detail = %q, want none for an ok window", i, got.Detail)
		}
	}
}

// TestUsageStatsSendsBearerAndParses drives the whole call: the endpoint takes
// the bearer key, the descriptor's headers reach it, and the payload decodes.
func TestUsageStatsSendsBearerAndParses(t *testing.T) {
	var gotAuth, gotPath, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"usage":{"rolling":{"status":"ok","percent":2,"resetsAt":"2026-09-27T22:02:06.247Z"}}}`))
	}))
	defer srv.Close()

	orig := usageEndpoint
	usageEndpoint = srv.URL + "/usage"
	defer func() { usageEndpoint = orig }()

	stats, err := usageStats(context.Background(), "secret-key", map[string]string{"User-Agent": "juggler"})
	if err != nil {
		t.Fatalf("usageStats: %v", err)
	}
	if gotAuth != "Bearer secret-key" {
		t.Fatalf("Authorization = %q, want a bearer key", gotAuth)
	}
	if gotPath != "/usage" {
		t.Fatalf("path = %q, want /usage", gotPath)
	}
	if gotUA != "juggler" {
		t.Fatalf("User-Agent = %q, want the descriptor's header to reach the endpoint", gotUA)
	}
	if len(stats.Stats) != 1 || stats.Stats[0].Name != "5-hour" || derefPct(stats.Stats[0].UsedPercent) != 2 {
		t.Fatalf("unexpected stats: %+v", stats.Stats)
	}
}
