//     ▄▄ ▄▄ ▄▄  ▄▄▄▄  ▄▄▄▄ ▄▄    ▄▄▄▄▄ ▄▄▄▄
//     ██ ██ ██ ██ ▄▄ ██ ▄▄ ██    ██▄▄  ██▄█▄   Copyright (c) 2026 Julian Storer
//   ▄▄█▀ ▀███▀ ▀███▀ ▀███▀ ██▄▄▄ ██▄▄▄ ██ ██   AGPL-3.0-or-later - see LICENSE

package opencodego

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"juggler/cmd/juggler/providers/provider"
	"juggler/cmd/juggler/providers/utils"
)

// usageEndpoint is the Go subscription's own usage endpoint, served beside the
// chat wire on the same base. That placement is the whole reason it is the Go
// plan's snapshot rather than a Zen one: Zen publishes no usage endpoint at all
// (…/zen/v1/usage answers 404), and with the providers' credentials split this
// call authenticates with the key stored for Go.
//
// A var so tests can point it at an httptest server.
var usageEndpoint = "https://opencode.ai/zen/go/v1/usage"

// usageWindowOrder is the plan's three metered windows in the order a reader
// wants them: shortest first. They are the endpoint's own keys.
var usageWindowOrder = []string{"rolling", "weekly", "monthly"}

type usageResponse struct {
	// Usage is a map rather than three named fields: the plan meters three
	// windows today, and a window upstream adds later must still appear as its
	// own row instead of being dropped by this build.
	Usage map[string]usageWindow `json:"usage"`
}

type usageWindow struct {
	// Status is upstream's own word for the window ("ok" when nothing is
	// wrong). Any other word is shown verbatim rather than interpreted.
	Status string `json:"status"`
	// Percent is a pointer so an omitted field stays unknown. A window without
	// one is skipped: absence means "not reported", not "0% used".
	Percent *float64 `json:"percent"`
	// ResetsAt is RFC 3339, or empty when upstream didn't state a reset.
	ResetsAt string `json:"resetsAt"`
}

// usageStats fetches the Go subscription's usage: a rolling 5-hour allowance,
// the week, and the month, each reported as the share of that window's own
// allowance already spent.
//
// The three windows have separate allowances — the plan defines them as 20%,
// 50% and 100% of a model's monthly dollar limit, and documents the same three
// windows on its own page under "Usage limits" (https://opencode.ai/docs/go/) —
// so the percentages are per-window and never comparable with each other.
//
// That the number is the consumed share is a reading of the wire, not a label
// upstream sends: the payload carries only the number. The plan's own framing
// (limits reached by spending against an allowance, "track your current usage")
// and the magnitudes observed agree with it.
func usageStats(ctx context.Context, credential string, headers map[string]string) (provider.UsageStats, error) {
	var parsed usageResponse
	if err := utils.GetJSON(ctx, usageEndpoint, utils.JSONGetOptions{
		Bearer:   credential,
		Headers:  headers,
		Defaults: map[string]string{"Accept": "application/json"},
		Label:    "OpenCode Go usage",
	}, &parsed); err != nil {
		return provider.UsageStats{}, err
	}
	return buildUsageStats(parsed, time.Now()), nil
}

func buildUsageStats(parsed usageResponse, now time.Time) provider.UsageStats {
	stats := provider.UsageStats{
		Provider:  "opencodego",
		UpdatedAt: now,
	}
	for _, key := range orderedWindows(parsed.Usage) {
		window := parsed.Usage[key]
		if window.Percent == nil {
			continue
		}
		name, category, windowSecs := describeWindow(key)
		stat := provider.UsageStat{
			Name:        name,
			UsedPercent: provider.Pct(*window.Percent),
			WindowSecs:  windowSecs,
			Category:    category,
		}
		if status := strings.TrimSpace(window.Status); status != "" && !strings.EqualFold(status, "ok") {
			stat.Detail = status
		}
		if reset, err := time.Parse(time.RFC3339, window.ResetsAt); err == nil {
			reset = reset.UTC()
			stat.ResetsAt = &reset
		}
		stats.Stats = append(stats.Stats, stat)
	}
	return stats
}

// orderedWindows returns the payload's window keys with the three metered ones
// first, in their natural order, and anything else after them alphabetically —
// so a window upstream adds later appears as its own row without reordering the
// rows the user already reads.
func orderedWindows(windows map[string]usageWindow) []string {
	keys := make([]string, 0, len(windows))
	known := make(map[string]bool, len(usageWindowOrder))
	for _, key := range usageWindowOrder {
		if _, ok := windows[key]; ok {
			known[key] = true
			keys = append(keys, key)
		}
	}
	extra := make([]string, 0, len(windows))
	for key := range windows {
		if !known[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(extra)
	return append(keys, extra...)
}

// describeWindow maps a window key to the label the user reads, the UI category
// and the window's length where it is fixed. The 5-hour window and the week are
// constant; a month is not (28–31 days), so it carries no length and the UI
// draws no elapsed-time marker against it.
func describeWindow(key string) (name, category string, windowSecs int) {
	switch key {
	case "rolling":
		return "5-hour", "primary", 5 * 60 * 60
	case "weekly":
		return "Weekly", "weekly", 7 * 24 * 60 * 60
	case "monthly":
		return "Monthly", "monthly", 0
	default:
		return humaniseWindow(key), "model", 0
	}
}

// humaniseWindow capitalises an unrecognised window key for display. The first
// rune is uppercased rather than the first byte, so a non-ASCII key survives.
func humaniseWindow(key string) string {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return "Usage"
	}
	first, size := utf8.DecodeRuneInString(trimmed)
	return string(unicode.ToUpper(first)) + trimmed[size:]
}
