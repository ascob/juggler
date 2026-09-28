//     ▄▄ ▄▄ ▄▄  ▄▄▄▄  ▄▄▄▄ ▄▄    ▄▄▄▄▄ ▄▄▄▄
//     ██ ██ ██ ██ ▄▄ ██ ▄▄ ██    ██▄▄  ██▄█▄   Copyright (c) 2026 Julian Storer
//   ▄▄█▀ ▀███▀ ▀███▀ ▀███▀ ██▄▄▄ ██▄▄▄ ██ ██   AGPL-3.0-or-later - see LICENSE

package openaibase

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"juggler/cmd/juggler/providers/provider"
)

// captureHeaders returns an http.Client that records each request's configured
// session header (headerName) and replays a minimal stream for the given API
// shape ("chat" or "responses").
func captureHeaders(t *testing.T, headerName string, sessionIDs *[]string, shape string) *http.Client {
	t.Helper()
	var stream string
	switch shape {
	case "chat":
		stream = sseBody(`{"id":"x","object":"chat.completion.chunk","created":0,"model":"glm-4.6","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	case "responses":
		stream = sseBody(`{"type":"response.output_text.delta","delta":"hi","item_id":"m1","output_index":0,"content_index":0,"sequence_number":1}`)
	}
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		*sessionIDs = append(*sessionIDs, r.Header.Get(headerName))
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

func sessionAffinityClient(t *testing.T, sessionIDs *[]string, model, headerName string, quirks Quirks) *Client {
	t.Helper()
	c, err := NewClient(Config{
		APIKey:     "test",
		Model:      model,
		BaseURL:    "https://example.test",
		HTTPClient: captureHeaders(t, headerName, sessionIDs, "responses"),
		Quirks:     quirks,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func streamOnce(t *testing.T, c *Client, convID string) {
	t.Helper()
	_, err := c.streamMessage(context.Background(), provider.MessageRequest{
		Messages:       []provider.Message{{Type: "user", Content: "hello"}},
		ConversationID: convID,
	}, func(provider.StreamChunk) (*provider.ToolResult, error) { return nil, nil })
	if err != nil {
		t.Fatalf("streamMessage: %v", err)
	}
}

// TestSessionAffinityHeaderStablePerConversation: with the quirk on, every
// request for a conversation carries the same UUID-shaped session id — the
// ChatGPT Codex backend's cache-affinity key. Losing stability across turns
// would lose replica affinity and re-bill the whole prefix, so this pins the
// derivation, not just presence.
func TestSessionAffinityHeaderStablePerConversation(t *testing.T) {
	var got []string
	c := sessionAffinityClient(t, &got, "gpt-5-codex", "session_id", Quirks{ForceResponsesAPI: true, SessionAffinityHeader: "session_id"})

	streamOnce(t, c, "conv-abc")
	streamOnce(t, c, "conv-abc")
	streamOnce(t, c, "conv-other")

	if len(got) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(got))
	}
	uuidShape := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !uuidShape.MatchString(got[0]) {
		t.Fatalf("session_id %q is not UUID-shaped", got[0])
	}
	if got[0] != got[1] {
		t.Fatalf("session_id changed between turns of the same conversation: %q vs %q", got[0], got[1])
	}
	if got[0] == got[2] {
		t.Fatalf("different conversations share a session_id (%q) — they would funnel onto one replica", got[0])
	}
}

// TestSessionAffinityHeaderAbsences: no header without the quirk (other
// vendors don't know it, requests must stay byte-identical), and no header
// without a conversation id even with the quirk on.
func TestSessionAffinityHeaderAbsences(t *testing.T) {
	var withoutQuirk []string
	c := sessionAffinityClient(t, &withoutQuirk, "gpt-5-codex", "session_id", Quirks{ForceResponsesAPI: true})
	streamOnce(t, c, "conv-abc")
	if withoutQuirk[0] != "" {
		t.Fatalf("session_id %q sent without the quirk", withoutQuirk[0])
	}

	var withoutConv []string
	c = sessionAffinityClient(t, &withoutConv, "gpt-5-codex", "session_id", Quirks{ForceResponsesAPI: true, SessionAffinityHeader: "session_id"})
	streamOnce(t, c, "")
	if withoutConv[0] != "" {
		t.Fatalf("session_id %q sent with no conversation id", withoutConv[0])
	}
}

// TestSessionAffinityHeaderChatCompletions: the header must ride the Chat
// Completions path too. OpenCode Go refuses a request without it, so a quirk
// that only reached the Responses path would leave every Go turn failing with
// 400 MissingSessionID.
func TestSessionAffinityHeaderChatCompletions(t *testing.T) {
	var got []string
	c, err := NewClient(Config{
		APIKey:     "test",
		Model:      "deepseek-v4.1-flash", // no "codex" → Chat Completions path
		BaseURL:    "https://example.test",
		HTTPClient: captureHeaders(t, "x-opencode-session", &got, "chat"),
		Quirks:     Quirks{SessionAffinityHeader: "x-opencode-session"},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	streamOnce(t, c, "conv-abc")
	streamOnce(t, c, "conv-abc")

	if len(got) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(got))
	}
	if want := sessionAffinityID("conv-abc"); got[0] != want {
		t.Fatalf("x-opencode-session = %q, want %q", got[0], want)
	}
	if got[0] != got[1] {
		t.Fatalf("x-opencode-session changed between turns: %q vs %q", got[0], got[1])
	}

	var withoutConv []string
	c, err = NewClient(Config{
		APIKey:     "test",
		Model:      "deepseek-v4.1-flash",
		BaseURL:    "https://example.test",
		HTTPClient: captureHeaders(t, "x-opencode-session", &withoutConv, "chat"),
		Quirks:     Quirks{SessionAffinityHeader: "x-opencode-session"},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	streamOnce(t, c, "")
	if withoutConv[0] != "" {
		t.Fatalf("x-opencode-session %q sent with no conversation id", withoutConv[0])
	}
}
