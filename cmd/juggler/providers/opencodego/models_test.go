//     ▄▄ ▄▄ ▄▄  ▄▄▄▄  ▄▄▄▄ ▄▄    ▄▄▄▄▄ ▄▄▄▄
//     ██ ██ ██ ██ ▄▄ ██ ▄▄ ██    ██▄▄  ██▄█▄   Copyright (c) 2026 Julian Storer
//   ▄▄█▀ ▀███▀ ▀███▀ ▀███▀ ██▄▄▄ ██▄▄▄ ██ ██   AGPL-3.0-or-later - see LICENSE

package opencodego

import (
	"slices"
	"strings"
	"testing"

	"juggler/cmd/juggler/providers/provider"
)

// TestContextWindow pins one window per vendor family plus both fallbacks. The
// gateway publishes no limits of its own, so a window that is wrong here is
// never corrected at runtime: the model silently compacts at that number.
func TestContextWindow(t *testing.T) {
	want := map[string]int{
		"deepseek-v4-pro":  1000000,
		"glm-5.1":          202752,
		"grok-4.6":         500000,
		"gpt-6-luna":       1050000,
		"hy3":              256000,
		"kimi-k3":          1048576,
		"mimo-v2.6-flash":  1048576,
		"minimax-m2.5":     204800,
		"qwen3.8-max":      1000000,
		"space-bunny-free": 1048576,
	}
	for model, window := range want {
		if got := contextWindowCaps.Lookup(model); got != window {
			t.Errorf("contextWindow(%q) = %d, want %d", model, got, window)
		}
	}

	// deepseek-flash is served by the gateway and appears in neither the
	// models.dev catalog nor OpenCode's own endpoint table, so there is no
	// published figure to record. It takes the default, which understates it at
	// worst — the alternative, a guess that overshoots, is rejected mid-request.
	if got := contextWindowCaps.Lookup("deepseek-flash"); got != DefaultContextWindow {
		t.Errorf("deepseek-flash window = %d, want the %d default", got, DefaultContextWindow)
	}
	if got := contextWindowCaps.Lookup("not-a-go-model"); got != DefaultContextWindow {
		t.Errorf("unlisted id window = %d, want the %d default", got, DefaultContextWindow)
	}
}

// TestModelFilter pins the deny-list contract. Every id the gateway serves is
// admitted — the gateway's listing, not this file, decides which models exist —
// so a model released after this code shipped appears on its own. Only the three
// ids that are known not to work are refused, and they are refused by name.
func TestModelFilter(t *testing.T) {
	admitted := []string{
		"deepseek-v4-flash",
		"deepseek-v4.1-flash",
		"deepseek-flash", // served, in no catalog: offered with default limits
		"glm-5.3",
		"grok-4.6",
		"gpt-6-luna",
		"hy4-preview",
		"kimi-k2.7-code",
		"longcat-2.5-preview-free",
		"mimo-v2.6-pro",
		"minimax-m3",
		"omen-alpha",
		"qwen3.8-flash",
		"space-bunny-free",
		// An id nobody has seen: the filter must not be an allow-list, or every
		// model the gateway adds needs a release to appear.
		"some-model-2027",
	}
	for _, id := range admitted {
		if !modelFilter(id) {
			t.Errorf("modelFilter(%q) = false, want admitted", id)
		}
		if !modelFilter("  " + id + "  ") {
			t.Errorf("modelFilter with surrounding spaces rejected %q", id)
		}
		if !modelFilter(strings.ToUpper(id)) {
			t.Errorf("modelFilter should match case-insensitively for %q", id)
		}
	}

	rejected := []string{
		// Refused by the gateway until the workspace allows models that train on
		// request data; opting in is the user's decision, not ours.
		"muse-spark-1.2-contributor",
		"muse-spark-1.3-contributor",
		// Served only as "format anthropic", a wire this provider does not speak.
		"minimax-m2.7",
	}
	for _, id := range rejected {
		if modelFilter(id) {
			t.Errorf("modelFilter(%q) = true, want rejected", id)
		}
	}
}

// TestResponsesOnlyModelIDsAreOffered keeps the routing list and the deny-list
// consistent, and keeps the list minimal.
func TestResponsesOnlyModelIDsAreOffered(t *testing.T) {
	for id := range responsesOnlyModelIDs {
		if !modelFilter(id) {
			t.Errorf("%q is routed to the Responses API but not offered — the entry does nothing", id)
		}
	}

	// The two ids the gateway serves on Responses and refuses on Chat
	// Completions. Without them every grok turn fails with
	// 400 ModelProtocolUnsupported.
	for _, id := range []string{"grok-4.6", "grok-4.7"} {
		if !responsesOnlyModelIDs[id] {
			t.Errorf("%q must route to the Responses API", id)
		}
	}

	// The other Responses-only ids on this gateway are OpenAI generations,
	// which openaibase already routes by number. Listing them here would be a
	// second, silently divergent copy of that rule.
	for _, id := range []string{"gpt-5.6-luna", "gpt-6-luna"} {
		if responsesOnlyModelIDs[id] {
			t.Errorf("%q is routed by its generation already; listing it here duplicates that rule", id)
		}
	}
}

// TestThinkingSpecNativeLevels pins the thinking contract the way the sibling
// Zen provider's test does: each level name IS the reasoning_effort the gateway
// takes, DeepSeek V4 exposes high/xhigh, and everything else low/medium/high.
func TestThinkingSpecNativeLevels(t *testing.T) {
	ds := thinkingSpec("deepseek-v4-pro")
	if !slices.Equal(ds.Levels, []string{"high", "xhigh"}) {
		t.Errorf("deepseek-v4 levels = %v, want [high xhigh]", ds.Levels)
	}
	if ds.Default != "high" {
		t.Errorf("deepseek-v4 default = %q, want high", ds.Default)
	}

	// One id per family the spec's other branch has to answer for, including
	// the two that share the deepseek prefix without being V4.
	for _, model := range []string{"deepseek-flash", "glm-5.3", "grok-4.6", "kimi-k3", "mimo-v2.6-flash", "qwen3.8-max"} {
		spec := thinkingSpec(model)
		if !slices.Equal(spec.Levels, []string{"low", "medium", "high"}) {
			t.Errorf("thinkingSpec(%q) levels = %v, want [low medium high]", model, spec.Levels)
		}
	}
}

// TestResolveModelCapabilities pins the admission contract through the registry:
// a catalogued id resolves with its catalogued numbers, an output that only the
// default can supply resolves at the default, and an id in neither table is not
// vouched for — the live list is what sizes that one.
func TestResolveModelCapabilities(t *testing.T) {
	Register()
	info, found := provider.GetProviderInfo("opencodego")
	if !found || info.ResolveModelCapabilities == nil {
		t.Fatal("opencodego registration has no capability resolver")
	}

	for _, tc := range []struct {
		model  string
		window int64
		output int64
	}{
		{"deepseek-v4-pro", 1000000, 384000},
		{"mimo-v2.6-pro", 1048576, 131072},
		// Catalogued window, and an output no one published: grok advertises
		// 500000 output for a 500000 window, which cannot both be true, so the
		// cap falls to the default.
		{"grok-4.6", 500000, DefaultMaxOutputTokens},
	} {
		got, found := info.ResolveModelCapabilities(tc.model)
		want := provider.ModelCapabilities{ContextWindowTokens: tc.window, MaxOutputTokens: tc.output}
		if !found || got != want {
			t.Errorf("%s capabilities = (%+v, %v), want (%+v, true)", tc.model, got, found, want)
		}
	}

	if _, found := info.ResolveModelCapabilities("deepseek-flash"); found {
		t.Error("deepseek-flash must not resolve statically: it has no published window, so its size comes from the live list and its default")
	}
}
