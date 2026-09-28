//     ▄▄ ▄▄ ▄▄  ▄▄▄▄  ▄▄▄▄ ▄▄    ▄▄▄▄▄ ▄▄▄▄
//     ██ ██ ██ ██ ▄▄ ██ ▄▄ ██    ██▄▄  ██▄█▄   Copyright (c) 2026 Julian Storer
//   ▄▄█▀ ▀███▀ ▀███▀ ▀███▀ ██▄▄▄ ██▄▄▄ ██ ██   AGPL-3.0-or-later - see LICENSE

package openaibase

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// capturePaths returns an http.Client that records each request's URL path and
// replays a minimal one-chunk stream shaped for that path, so a call routed to
// the wrong wire is visible as a path rather than as a parse failure.
func capturePaths(t *testing.T, paths *[]string) *http.Client {
	t.Helper()
	chat := sseBody(`{"id":"x","object":"chat.completion.chunk","created":0,"model":"glm-4.6","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	responses := sseBody(`{"type":"response.output_text.delta","delta":"hi","item_id":"m1","output_index":0,"content_index":0,"sequence_number":1}`)
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		*paths = append(*paths, r.URL.Path)
		stream := chat
		if strings.HasSuffix(r.URL.Path, "/responses") {
			stream = responses
		}
		header := make(http.Header)
		header.Set("Content-Type", "text/event-stream")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     header,
			Body:       io.NopCloser(strings.NewReader(stream)),
			Request:    r,
		}, nil
	})}
}

// TestResponsesAPIModelIDsRouteResponses: a gateway that serves both wires and
// picks between them per model cannot be read off the id, so the descriptor
// names its Responses-only ids. OpenCode Go answers 400 ModelProtocolUnsupported
// to a Chat Completions call for "grok-4.6" while serving the same model on
// Responses, and that id is neither a codex slug nor an OpenAI generation — so a
// request that ignores this list fails every turn on that model, and one that
// applies it to the wrong id fails there instead. Both directions are pinned.
func TestResponsesAPIModelIDsRouteResponses(t *testing.T) {
	listed := Quirks{ResponsesAPIModelIDs: map[string]bool{"grok-4.6": true}}

	cases := []struct {
		name          string
		model         string
		quirks        Quirks
		wantResponses bool
	}{
		{"listed id routes Responses", "grok-4.6", listed, true},
		{"listed id matches case and padding insensitively", " Grok-4.6 ", listed, true},
		{"unlisted id stays on Chat Completions", "deepseek-v4.1-flash", listed, false},
		{"no list leaves a non-OpenAI id on Chat Completions", "grok-4.6", Quirks{}, false},
		{"the generation heuristic still routes", "gpt-6-luna", Quirks{}, true},
		{"older generations stay on Chat Completions", "gpt-5.5", Quirks{}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var paths []string
			c, err := NewClient(Config{
				APIKey:     "test",
				Model:      tc.model,
				BaseURL:    "https://example.test",
				HTTPClient: capturePaths(t, &paths),
				Quirks:     tc.quirks,
			})
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			streamOnce(t, c, "conv-abc")

			if len(paths) != 1 {
				t.Fatalf("expected 1 request, got %d (%v)", len(paths), paths)
			}
			got := strings.HasSuffix(paths[0], "/responses")
			if got != tc.wantResponses {
				t.Errorf("model %q with ResponsesAPIModelIDs=%v hit %q: Responses=%v, want %v",
					tc.model, tc.quirks.ResponsesAPIModelIDs, paths[0], got, tc.wantResponses)
			}
		})
	}
}
