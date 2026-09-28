//     ▄▄ ▄▄ ▄▄  ▄▄▄▄  ▄▄▄▄ ▄▄    ▄▄▄▄▄ ▄▄▄▄
//     ██ ██ ██ ██ ▄▄ ██ ▄▄ ██    ██▄▄  ██▄█▄   Copyright (c) 2026 Julian Storer
//   ▄▄█▀ ▀███▀ ▀███▀ ▀███▀ ██▄▄▄ ██▄▄▄ ██ ██   AGPL-3.0-or-later - see LICENSE

// Package opencodego serves the OpenCode Go gateway: the subscription endpoint
// beside OpenCode Zen, whose model list is read from the gateway itself rather
// than compiled in.
package opencodego

import (
	"strings"

	"juggler/cmd/juggler/providers/openaibase"
	"juggler/cmd/juggler/providers/utils"
)

// deniedModelIDs are the ids the gateway lists but this provider does not
// offer. The list is a deny-list, not an allow-list: every other id the gateway
// serves is admitted as it appears, so a model released tomorrow shows up
// without a code change. Only an id that is known not to work is named here,
// with what it does instead.
//
//   - muse-spark-1.2-contributor, muse-spark-1.3-contributor: refused with
//     `400 "Upstream request failed: This Go model trains on request data.
//     Allow paid endpoints that train on request data in your workspace's
//     Privacy settings to use it."` — the models are usable only once the user
//     opts into that workspace setting, and an opt-in is a decision to surface
//     in the UI, not something to send on their behalf. (Probed 2026-09-27.)
//     The gateway's own model list and privacy table describe the same trade:
//     heavily discounted pricing in exchange for permission to train on your
//     prompts and completions, and availability limited by region
//     (https://opencode.ai/docs/go/).
//   - minimax-m2.7: answers `400 ModelProtocolUnsupported` to both wires this
//     provider speaks; the endpoint table on the same page serves it only on
//     /messages as "format anthropic", a third wire nothing here implements.
//     Its siblings minimax-m2.5 and minimax-m3 are listed there as /messages
//     models too but answer on Chat Completions (see responsesOnlyModelIDs),
//     so they are offered and only this one is not.
//
// A model that changes wire later, or a new Responses-only family, fails loudly
// instead of silently: the gateway answers `400 ModelProtocolUnsupported` for
// the wrong protocol. So this list is a correction, not a safety net.
var deniedModelIDs = map[string]bool{
	"muse-spark-1.2-contributor": true,
	"muse-spark-1.3-contributor": true,
	"minimax-m2.7":               true,
}

// modelFilter admits every id the gateway serves except the denied ones above.
// The gateway's own listing is the source of truth for which models exist —
// nothing here decides that — so an id it adds appears on its own, sized by the
// tables below where they know it and by the defaults where they do not.
func modelFilter(id string) bool {
	return !deniedModelIDs[normalizeModelID(id)]
}

// responsesOnlyModelIDs are the ids the gateway serves on the Responses API and
// refuses on Chat Completions with `400 ModelProtocolUnsupported` (probed
// 2026-09-27). The gateway picks the protocol per model and its ids say nothing
// about it, so each one has to be named.
//
// Only the grok ids need naming: the other two Responses-only ids on this
// gateway are gpt-5.6-luna and gpt-6-luna, which openaibase's own
// gpt-generation rule already routes (any gpt-5.6 and later). A new
// Responses-only id from a vendor whose names carry no generation — another
// grok, say — fails with that 400 until it is added here.
//
// The gateway's endpoint table (https://opencode.ai/docs/go/) agrees on these
// four and on the two denied muse-spark ids, and lists seven more ids on
// /messages: minimax-m2.5, minimax-m3, qwen3.6-plus, qwen3.7-max, qwen3.7-plus,
// qwen3.8-flash and qwen3.8-max. This provider sends those to Chat Completions
// anyway, because that is where the gateway answers them: probed 2026-09-27,
// each returned a 200 stream on /chat/completions, while /messages returned
// 401 {"type":"error","error":{"type":"AuthError","message":"Missing API key."}}
// to the same bearer key (and, for a Responses-only id, "Model grok-4.6 is not
// supported for format anthropic"). So that table names the endpoint OpenCode's
// own client uses per model, not the only one the gateway accepts; minimax-m2.7
// is the one /messages id that refuses chat, and it is denied above.
var responsesOnlyModelIDs = map[string]bool{
	"grok-4.6": true,
	"grok-4.7": true,
}

// normalizeModelID lowercases and trims an id for the lookups here, matching
// how the model list and the configuration both present it.
func normalizeModelID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}

// modelContextWindows maps each id to its context window. Source: the
// opencode-go entry of https://models.dev/api.json — the catalog OpenCode
// itself consumes. The gateway's own listing carries id, object, created and
// owned_by and no limits, so nothing corrects these at runtime: an id missing
// here is sized by DefaultContextWindow for its whole life, which understates a
// million-token model fivefold. Add ids as models.dev publishes them; the
// opt-in TestCatalogDrift reports ones the gateway serves that are absent.
var modelContextWindows = map[string]int{
	"deepseek-v4-flash":            1000000,
	"deepseek-v4-flash-vision-exp": 1000000,
	"deepseek-v4-pro":              1000000,
	"deepseek-v4.1-flash":          1000000,
	"glm-5.1":                      202752,
	"glm-5.2":                      1000000,
	"glm-5.3":                      1000000,
	"glm-5.3-flash":                1000000,
	"gpt-5.6-luna":                 1050000,
	"gpt-6-luna":                   1050000,
	"grok-4.6":                     500000,
	"grok-4.7":                     500000,
	"hy3":                          256000,
	"hy4-preview":                  1024000,
	"kimi-k2.6":                    262144,
	"kimi-k2.7-code":               262144,
	"kimi-k3":                      1048576,
	"longcat-2.0":                  1000000,
	"longcat-2.5-preview-free":     1000000,
	"mimo-v2.5":                    1000000,
	"mimo-v2.5-pro":                1048576,
	"mimo-v2.6-flash":              1048576,
	"mimo-v2.6-pro":                1048576,
	"minimax-m2.5":                 204800,
	"minimax-m3":                   1000000,
	"omen-alpha":                   500000,
	"qwen3.6-plus":                 1000000,
	"qwen3.7-max":                  1000000,
	"qwen3.7-plus":                 1000000,
	"qwen3.8-flash":                1000000,
	"qwen3.8-max":                  1000000,
	"space-bunny-free":             1048576,
}

// modelMaxOutputTokens maps each id to its output ceiling, same source. Every
// entry is strictly below its model's window, as an output cap at or above the
// window leaves no room for input.
//
// Three ids are deliberately absent although models.dev publishes a figure:
// grok-4.6 and grok-4.7 (500000 of a 500000 window) and kimi-k2.7-code
// (262144 of 262144). Their vendor's number is the whole window, and writing it
// here would fail TestCatalogInvariants — the entry has to be the number that
// fits, and nobody has published that one. They fall to
// DefaultMaxOutputTokens instead, which is far below the share of the window a
// request may occupy (85%), so input room is not in question either way; the
// clamp would substitute the derived reserve — min(20000, window/5) — for the
// vendor's number had it been written here.
var modelMaxOutputTokens = map[string]int{
	"deepseek-v4-flash":            384000,
	"deepseek-v4-flash-vision-exp": 384000,
	"deepseek-v4-pro":              384000,
	"deepseek-v4.1-flash":          384000,
	"glm-5.1":                      32768,
	"glm-5.2":                      131072,
	"glm-5.3":                      131072,
	"glm-5.3-flash":                131072,
	"gpt-5.6-luna":                 128000,
	"gpt-6-luna":                   128000,
	"hy3":                          128000,
	"hy4-preview":                  64000,
	"kimi-k2.6":                    65536,
	"kimi-k3":                      131072,
	"longcat-2.0":                  131072,
	"longcat-2.5-preview-free":     131072,
	"mimo-v2.5":                    128000,
	"mimo-v2.5-pro":                128000,
	"mimo-v2.6-flash":              131072,
	"mimo-v2.6-pro":                131072,
	"minimax-m2.5":                 65536,
	"minimax-m3":                   131072,
	"omen-alpha":                   128000,
	"qwen3.6-plus":                 65536,
	"qwen3.7-max":                  65536,
	"qwen3.7-plus":                 65536,
	"qwen3.8-flash":                131072,
	"qwen3.8-max":                  131072,
	"space-bunny-free":             524288,
}

// DefaultContextWindow / DefaultMaxOutputTokens size an id the tables above do
// not know — a model the gateway added before models.dev listed it, or one
// models.dev never lists (deepseek-flash, served on this gateway and in neither
// the catalog nor OpenCode's own documentation).
const DefaultContextWindow = 200000
const DefaultMaxOutputTokens = 32000

var (
	contextWindowCaps = utils.ModelCaps{Default: DefaultContextWindow, Overrides: modelContextWindows}
	maxOutputCaps     = utils.ModelCaps{Default: DefaultMaxOutputTokens, Overrides: modelMaxOutputTokens}
)

// thinkingSpec returns the reasoning-effort spec for a Go model. DeepSeek V4
// takes high/xhigh; everything else takes low/medium/high.
//
// The levels are not per-model data because the gateway does not distinguish:
// probing every chat family on 2026-09-27 found low, medium, high and xhigh
// accepted across all of them, with only "minimal" refused (by glm-5.1 and
// qwen3.6-plus), so no family needs a vocabulary of its own. models.dev's
// per-model reasoning_options do not describe this gateway — it calls
// longcat-2.0 a "toggle" while the endpoint accepts an effort there — which is
// why the levels come from the endpoint's behaviour rather than that field.
func thinkingSpec(modelID string) openaibase.ThinkingSpec {
	if strings.HasPrefix(normalizeModelID(modelID), "deepseek-v4") {
		return openaibase.EffortSpec("high", "high", "xhigh")
	}
	return openaibase.EffortSpec("medium", "low", "medium", "high")
}
