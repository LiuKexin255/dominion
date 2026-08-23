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
// chunk. The request's model field is ignored entirely: the fake model
// catalogue is aligned on the dsh side via the adapter's models[] config
// (specs/047-dsh-chat-demo/contracts/fake-llm-wire.md §2).
const FakeModel = "fake-chat-v1"

// fixedUsage is the constant usage object carried by the finish chunk
// and the non-streaming response (specs/047-dsh-chat-demo/contracts/
// fake-llm-wire.md §3/§4 — the demo streams a single text segment, so
// the token counts are fixed placeholders).
var fixedUsage = &usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2}

// chatCompletionRequest is the subset of the OpenAI
// /v1/chat/completions request schema the handler actually consumes.
// Model is decoded but ignored; only stream + messages drive behaviour.
type chatCompletionRequest struct {
	Model    string          `json:"model"`
	Stream   bool            `json:"stream"`
	Messages []*messageParam `json:"messages"`
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
// with type "text" contribute their Text to keyword matching; other
// part types are ignored.
type contentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// delta is the unified message shape used for both the non-streaming
// response's `message` field and the streaming response's `delta`
// field. Content is a pointer so the first SSE frame can serialise the
// explicit empty string (`"content":""`,
// specs/047-dsh-chat-demo/contracts/fake-llm-wire.md §3 frame 1) while
// the finish frame omits the field entirely (`"delta":{}`).
type delta struct {
	Role    string  `json:"role,omitempty"`
	Content *string `json:"content,omitempty"`
}

// choice is one entry of the response's choices array. Message is set
// for non-streaming responses; Delta for streaming chunks. FinishReason
// is a pointer so it serialises as null when absent (OpenAI expects
// null, not omitted, for unfinished streaming chunks).
type choice struct {
	Index        int     `json:"index"`
	Message      *delta  `json:"message,omitempty"`
	Delta        *delta  `json:"delta,omitempty"`
	FinishReason *string `json:"finish_reason"`
}

// usage is the token-usage object carried on the wire.
type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// completionResponse is the top-level response body shape, shared by
// the non-streaming completion and every streaming chunk (their
// top-level fields match).
type completionResponse struct {
	ID      string    `json:"id"`
	Object  string    `json:"object"`
	Created int64     `json:"created"`
	Model   string    `json:"model"`
	Choices []*choice `json:"choices"`
	Usage   *usage    `json:"usage,omitempty"`
}

// errorResponse is the OpenAI-style error body returned for malformed
// requests (specs/047-dsh-chat-demo/contracts/fake-llm-wire.md §5).
type errorResponse struct {
	Error errorBody `json:"error"`
}

// errorBody is the inner error object of errorResponse: the
// human-readable failure message plus its OpenAI error type
// ("invalid_request_error" for every failure this service produces).
type errorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// finishStop is the finish_reason for plain text responses per the
// OpenAI Chat Completions schema.
const finishStop = "stop"

// ChatHandler serves POST /v1/chat/completions. It is stateless across
// requests: every request matches against the same store snapshot, and
// the no-match fallback is deterministic (seeded by the request
// content), so equal requests always receive equal replies.
type ChatHandler struct {
	store *MessageStore
}

// NewChatHandler wires the handler to a loaded MessageStore. The store
// must already be loaded and validated (see NewMessageStore).
func NewChatHandler(store *MessageStore) *ChatHandler {
	return &ChatHandler{store: store}
}

// ServeHTTP implements http.Handler. It accepts any Authorization
// bearer and tolerates the dsh adapter's extra headers
// (x-deepseek-harness-*, attribution User-Agent) without validation
// (specs/047-dsh-chat-demo/contracts/fake-llm-wire.md §2), decodes the
// request body, matches a template, and dispatches to the streaming or
// non-streaming writer. A malformed JSON body — or a messages array
// carrying a null entry, which would otherwise panic on the nil
// element — is rejected with the 400 error object of §5; an unmatched
// request still yields 200 with the fallback template.
func (h *ChatHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req chatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		serveError(w, fmt.Sprintf("invalid request: %v", err))
		return
	}

	messages, err := normalizeMessages(req.Messages)
	if err != nil {
		serveError(w, err.Error())
		return
	}
	tmpl, matched := match(h.store.Messages(), messages)
	slog.Info("chat completion served",
		slog.String("template", tmpl.Name),
		slog.Bool("condition_matched", matched),
		slog.Bool("stream", req.Stream),
	)

	respID := generateResponseID()
	if req.Stream {
		serveStreaming(w, tmpl.Text, respID)
		return
	}
	serveNonStreaming(w, tmpl.Text, respID)
}

// HandleHealth serves GET /health, the liveness probe. It returns the
// literal body "ok".
func HandleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// normalizeMessages converts the wire messages into the matcher's
// normalized shape, decoding both content forms (plain string and
// array-of-parts) to plain text. A null entry in the messages array
// decodes to a nil *messageParam; it is reported as a 400-worthy
// request error rather than skipped, so malformed input surfaces as the
// contract's error object (specs/047-dsh-chat-demo/contracts/
// fake-llm-wire.md §5) instead of a panic mid-handler.
func normalizeMessages(params []*messageParam) ([]*chatMessage, error) {
	var messages []*chatMessage
	for i, p := range params {
		if p == nil {
			return nil, fmt.Errorf("invalid request: messages[%d] is null", i)
		}
		messages = append(messages, &chatMessage{
			Role:    p.Role,
			Content: decodeContent(p.Content),
		})
	}
	return messages, nil
}

// decodeContent handles both Content forms. A quoted JSON string is
// returned directly; a JSON array is parsed for {type:"text"} parts
// whose .text values are joined with " ". Anything else — including a
// malformed payload — degrades to the empty string so match drives the
// deterministic fallback rather than 500-ing the request.
func decodeContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return ""
		}
		return s
	}
	if raw[0] != '[' {
		return ""
	}
	var parts []contentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
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

// generateResponseID returns a unique chat completion ID with the
// "chatcmpl-" prefix used on the OpenAI wire.
func generateResponseID() string {
	return fmt.Sprintf("chatcmpl-%d-%04d", time.Now().UnixNano(), rand.IntN(10000))
}

// serveError writes the OpenAI-style 400 error object.
func serveError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(errorResponse{
		Error: errorBody{Message: msg, Type: "invalid_request_error"},
	})
}

// serveNonStreaming writes the single chat.completion JSON object
// (specs/047-dsh-chat-demo/contracts/fake-llm-wire.md §4):
// choices[0].message.content = the template text, finish_reason "stop",
// usage attached.
func serveNonStreaming(w http.ResponseWriter, text, respID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	finish := finishStop
	resp := completionResponse{
		ID:      respID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   FakeModel,
		Choices: []*choice{{
			Index:        0,
			Message:      &delta{Role: "assistant", Content: &text},
			FinishReason: &finish,
		}},
		Usage: fixedUsage,
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode non-streaming response",
			slog.String("error", err.Error()))
	}
}

// serveStreaming writes the SSE event stream
// (specs/047-dsh-chat-demo/contracts/fake-llm-wire.md §3). The chunk
// sequence is, in order:
//
//  1. delta with role:"assistant" + explicit empty content (invariant 1);
//  2. one content delta carrying the template text in full (invariant 2
//     — the demo emits a single segment, no injected inter-frame delay);
//  3. empty delta {} with finish_reason:"stop" + usage (invariant 3);
//  4. the literal "data: [DONE]" terminator (invariant 4 — its absence
//     triggers the adapter's STREAM_CLOSED error).
//
// Each frame is flushed before the next is written so a slow client
// observes progressive output. The http.Flusher capability check runs
// BEFORE any header is committed — if the ResponseWriter does not
// implement http.Flusher the 500 error is still writable (WriteHeader
// has not been called yet) and the client does not receive a
// half-finished stream.
func serveStreaming(w http.ResponseWriter, text, respID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	now := time.Now().Unix()
	frames := []*completionResponse{
		{
			ID: respID, Object: "chat.completion.chunk",
			Created: now, Model: FakeModel,
			Choices: []*choice{{
				Index:        0,
				Delta:        &delta{Role: "assistant", Content: strPtr("")},
				FinishReason: nil,
			}},
		},
		{
			ID: respID, Object: "chat.completion.chunk",
			Created: now, Model: FakeModel,
			Choices: []*choice{{
				Index:        0,
				Delta:        &delta{Content: &text},
				FinishReason: nil,
			}},
		},
		{
			ID: respID, Object: "chat.completion.chunk",
			Created: now, Model: FakeModel,
			Choices: []*choice{{
				Index:        0,
				Delta:        new(delta),
				FinishReason: strPtr(finishStop),
			}},
			Usage: fixedUsage,
		},
	}

	for _, frame := range frames {
		data, err := json.Marshal(frame)
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

// strPtr returns a pointer to s, used so Content can serialise as the
// literal empty string rather than being omitted.
func strPtr(s string) *string {
	return &s
}
