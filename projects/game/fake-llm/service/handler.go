// Package service holds the fake-llm message store and HTTP handler.
//
// handler.go implements the OpenAI-compatible POST /v1/chat/completions
// endpoint: stateless keyword matching against the MessageStore, with
// streaming SSE and non-streaming JSON response shapes that mirror the
// validated prototype in experimental/openai_llm/fake_service.
package service

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"
)

// FakeModel is the literal model string advertised in every response
// chunk. Clients that filter by model should accept any value because
// we ignore the request's model field entirely.
const FakeModel = "fake-model"

// FakeResponseID is the chat completion id reused for every chunk of a
// single response. It is constant because there is no per-request
// state and clients only need it as a correlation key within one
// completion, not across calls.
const FakeResponseID = "fake-1"

// chatCompletionRequest is the subset of the OpenAI
// /v1/chat/completions request schema the handler actually consumes.
// The model field is decoded but ignored; only stream + messages
// drive behaviour.
type chatCompletionRequest struct {
	Model    string         `json:"model"`
	Stream   bool           `json:"stream"`
	Messages []messageParam `json:"messages"`
}

// messageParam mirrors one entry of the request's messages array.
// Content is left as json.RawMessage so the same field can carry either
// the plain-string form (`"hello"`) or the array-of-content-parts form
// (`[{"type":"text","text":"hello"}]`) per the OpenAI multimodal spec.
type messageParam struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// contentPart is one element of the array-form Content. Only entries
// with type "text" contribute their Text; non-text parts are ignored.
type contentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// assistantMessage is the unified message shape used for both the
// non-streaming response's `message` field and the streaming response's
// `delta` field. The omitempty tags keep the wire shape compact while
// still emitting explicit empty strings where the validated prototype
// does (e.g. an empty content on the reasoning delta).
type assistantMessage struct {
	Role             string `json:"role,omitempty"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
	Content          string `json:"content,omitempty"`
}

// choice is one entry of the response's choices array. Message is set
// for non-streaming responses; Delta for streaming chunks. FinishReason
// is a pointer so it serialises as null when absent (OpenAI expects
// null, not omitted, for unfinished streaming chunks).
type choice struct {
	Index        int               `json:"index"`
	Message      *assistantMessage `json:"message,omitempty"`
	Delta        *assistantMessage `json:"delta,omitempty"`
	FinishReason *string           `json:"finish_reason"`
}

// completionResponse is the top-level non-streaming response body.
type completionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []choice `json:"choices"`
}

// ChatHandler serves POST /v1/chat/completions. It is stateless across
// requests: every request goes through Match against the same store
// snapshot, and the RNG is shared only for fallback randomisation.
type ChatHandler struct {
	store *MessageStore
	rng   *rand.Rand
}

// NewChatHandler wires the handler to a loaded MessageStore and the
// shared RNG used for the no-match fallback. The store must already be
// loaded and validated (see NewMessageStore); the rng must be non-nil.
func NewChatHandler(store *MessageStore, rng *rand.Rand) *ChatHandler {
	return &ChatHandler{store: store, rng: rng}
}

// ServeHTTP implements http.Handler. It accepts any Authorization
// bearer (no validation), decodes the request body, runs Match against
// the last user message, and dispatches to the streaming or
// non-streaming writer based on the request's stream flag.
func (h *ChatHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req chatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	userText := lastUserText(req.Messages)
	msg, _ := Match(h.store.Messages(), userText, h.rng)

	if req.Stream {
		serveStreaming(w, msg)
		return
	}
	serveNonStreaming(w, msg)
}

// lastUserText extracts the text of the LAST message whose role equals
// "user" (compared case-insensitively). When Content is a string it is
// returned as-is; when Content is an array the .text fields of every
// {type:"text"} entry are joined with a single space. Returns the
// empty string when no user message is present — Match will then
// deterministically fall through to the random fallback.
func lastUserText(messages []messageParam) string {
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if !strings.EqualFold(m.Role, "user") {
			continue
		}
		return decodeContent(m.Content)
	}
	return ""
}

// decodeContent handles both Content forms. A quoted JSON string is
// returned directly; a JSON array is parsed for {type:"text"} parts
// whose .text values are joined with " ". Anything else — including a
// malformed payload — degrades to the empty string so Match drives the
// random fallback rather than 500-ing the request.
func decodeContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// String form: a leading double-quote means json.Unmarshal into a
	// string will succeed and is the cheapest path.
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			slog.Warn("failed to decode string content, falling back to empty",
				slog.String("error", err.Error()))
			return ""
		}
		return s
	}

	// Array form: pick out type:text parts and join them.
	var parts []contentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		slog.Warn("failed to decode array content, falling back to empty",
			slog.String("error", err.Error()))
		return ""
	}
	var texts []string
	for _, p := range parts {
		if p.Type == "text" {
			texts = append(texts, p.Text)
		}
	}
	return strings.Join(texts, " ")
}

// serveNonStreaming writes the single chat.completion JSON object
// carrying BOTH reasoning_content and content in the assistant message.
// Created is captured once and reused; the finish_reason is the literal
// "stop".
func serveNonStreaming(w http.ResponseWriter, msg Message) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	stop := "stop"
	resp := completionResponse{
		ID:      FakeResponseID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   FakeModel,
		Choices: []choice{
			{
				Index: 0,
				Message: &assistantMessage{
					Role:             "assistant",
					ReasoningContent: msg.Reasoning,
					Content:          msg.Text,
				},
				FinishReason: &stop,
			},
		},
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode non-streaming response",
			slog.String("error", err.Error()))
	}
}

// serveStreaming writes the SSE event stream. The chunk sequence MUST
// be, in order:
//
//  1. delta with role:"assistant" + reasoning_content (so the
//     @langchain/openai parser treats the stream as an assistant turn);
//  2. delta with content (the visible answer);
//  3. delta {} with finish_reason:"stop";
//  4. the literal "data: [DONE]" terminator.
//
// Each frame is flushed before the next is written so a slow client
// observes progressive output. If the ResponseWriter does not implement
// http.Flusher we surface a 500 once, before any chunk is emitted, so
// the client does not receive a half-finished stream.
func serveStreaming(w http.ResponseWriter, msg Message) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	now := time.Now().Unix()
	chunks := []completionResponse{
		{
			ID: FakeResponseID, Object: "chat.completion.chunk",
			Created: now, Model: FakeModel,
			Choices: []choice{{
				Index: 0,
				Delta: &assistantMessage{
					Role:             "assistant",
					ReasoningContent: msg.Reasoning,
				},
				FinishReason: nil,
			}},
		},
		{
			ID: FakeResponseID, Object: "chat.completion.chunk",
			Created: now, Model: FakeModel,
			Choices: []choice{{
				Index: 0,
				Delta: &assistantMessage{
					Content: msg.Text,
				},
				FinishReason: nil,
			}},
		},
		{
			ID: FakeResponseID, Object: "chat.completion.chunk",
			Created: now, Model: FakeModel,
			Choices: []choice{{
				Index:        0,
				Delta:        &assistantMessage{},
				FinishReason: strPtr("stop"),
			}},
		},
	}

	for _, chunk := range chunks {
		data, err := json.Marshal(chunk)
		if err != nil {
			slog.Error("failed to marshal stream chunk",
				slog.String("error", err.Error()))
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// strPtr returns a pointer to s, used so FinishReason can serialise as
// the literal string "stop" rather than null in the final chunk.
func strPtr(s string) *string {
	return &s
}
