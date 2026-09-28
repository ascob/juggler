//     ▄▄ ▄▄ ▄▄  ▄▄▄▄  ▄▄▄▄ ▄▄    ▄▄▄▄▄ ▄▄▄▄
//     ██ ██ ██ ██ ▄▄ ██ ▄▄ ██    ██▄▄  ██▄█▄   Copyright (c) 2026 Julian Storer
//   ▄▄█▀ ▀███▀ ▀███▀ ▀███▀ ██▄▄▄ ██▄▄▄ ██ ██   AGPL-3.0-or-later - see LICENSE

package opencodego

import (
	"juggler/cmd/juggler/providers/openaibase"
)

// Register adds the OpenCode Go provider to the global registry. Called
// explicitly from main; no init()-time side effects.
//
// OpenCode Go is the subscription gateway beside OpenCode Zen: a $10/month
// curated set of models on the same OpenCode account key. This provider offers
// every model the gateway serves (see deniedModelIDs for the three it does
// not), routed per model to the wire that model requires.
//
// It stores its key under its OWN credential name, not OpenCode Zen's. The two
// gateways are one account and usually the same secret, but the key entry is
// also the switch that turns a provider off: sharing the name would mean
// deleting Zen's key silently uncredentialed Go as well (and the other way
// round), because both the settings field and the delete action address the
// provider's key by ConfigKeyName. Pasting the same account key into both
// fields costs one paste and keeps each gateway switchable on its own.
//
// Left unset, deliberately, and each for a reason:
//
//   - CheapModel: micro-tasks must not silently spend a subscription whose
//     limits are metered per model and window.
//   - InputModalitiesFn: models.dev marks a dozen of these ids image-input, but
//     nothing has probed the gateway's image path, and claiming an input the
//     endpoint rejects is worse than offering text only. Zen declares no
//     modalities either.
//   - EchoReasoningContent: 24 of the catalogued ids declare
//     interleaved.field = reasoning_content, and Zen's catalog declares the same
//     for 41 of its ids while its descriptor ignores it and works — a continued
//     turn through this gateway (tool call, then the request that follows) does
//     too, without the quirk.
//   - ForcedToolChoiceSupported: Zen parity, and DeepSeek's own thinking mode
//     rejects a named tool_choice, so the safe default is to downgrade.
//   - LimitDiscoveryUnsupported: left off because discovery is harmless here —
//     the gateway publishes no limits, so it finds none and the catalog stands.
func Register() {
	openaibase.Register(openaibase.Descriptor{
		Name:               "opencodego",
		DisplayName:        "OpenCode Go",
		Description:        "Every model on the OpenCode Go subscription gateway. Uses your OpenCode account key, kept separately from OpenCode Zen's.",
		ConfigKeyName:      "opencode_go_api_key",
		EnvVarName:         "OPENCODE_GO_API_KEY",
		APIKeyURL:          "https://opencode.ai/auth",
		DisplayProvider:    "OpenCode Go",
		Filter:             modelFilter,
		ContextWindowCaps:  contextWindowCaps,
		MaxOutputCaps:      maxOutputCaps,
		ThinkingSpecFn:     thinkingSpec,
		UsageStatsOverride: usageStats,
		BaseURL:            "https://opencode.ai/zen/go/v1",
		Headers: map[string]string{
			// The gateway asks every client to identify itself with its own
			// agent name rather than arrive as a generic SDK, and monitors
			// traffic for abuse. The probe that established the session-header
			// requirement sent this same value.
			"User-Agent": "juggler",
		},
		Quirks: openaibase.Quirks{
			// The gateway's docs ask for this header on every request ("send a
			// stable session ID in x-opencode-session ... so we can optimize
			// routing and prompt caching"); measured, a request without it is
			// refused with 400 MissingSessionID, so it is a requirement here
			// rather than the optimisation it is for the Codex backend.
			SessionAffinityHeader: "x-opencode-session",
			// The gateway serves some models only on the Responses API and
			// answers 400 ModelProtocolUnsupported on the other wire.
			ResponsesAPIModelIDs: responsesOnlyModelIDs,
		},
	})
}
