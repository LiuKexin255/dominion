// Package service holds the fake-llm message store and HTTP handler.
//
// handler.go implements the OpenAI-compatible POST /v1/chat/completions
// endpoint: stateless keyword matching against the MessageStore, with
// streaming SSE and non-streaming JSON response shapes that mirror the
// validated prototype in experimental/openai_llm/fake_service.
package service

import (
	"bytes"
	"encoding/base64"
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

// chatCompletionRequest is the subset of the OpenAI
// /v1/chat/completions request schema the handler actually consumes.
// The model field is decoded but ignored; only stream + messages
// drive behaviour.
type chatCompletionRequest struct {
	Model    string          `json:"model"`
	Stream   bool            `json:"stream"`
	Messages []*messageParam `json:"messages"`
}

// messageParam mirrors one entry of the request's messages array.
// Content is left as json.RawMessage so the same field can carry either
// the plain-string form (`"hello"`) or the array-of-content-parts form
// (`[{"type":"text","text":"hello"}]`) per the OpenAI multimodal spec.
// ToolCallID/Name/ToolCalls carry the tool-message and assistant-tool-call
// metadata used by the tools dispatch branch.
type messageParam struct {
	Role       string           `json:"role"`
	Content    json.RawMessage  `json:"content"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Name       string           `json:"name,omitempty"`
	ToolCalls  []*toolCallParam `json:"tool_calls,omitempty"`
}

// toolCallParam mirrors one entry of an assistant message's tool_calls
// array in the request. Function.Arguments is a JSON string per the
// OpenAI schema.
type toolCallParam struct {
	ID       string                `json:"id"`
	Type     string                `json:"type"`
	Function toolCallParamFunction `json:"function"`
}

// toolCallParamFunction is the function sub-object of a request tool_call.
type toolCallParamFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// contentPart is one element of the array-form Content. Only entries
// with type "text" contribute their Text to keyword matching. Entries
// with type "image_url" carry a data URL in ImageURL.URL; their bytes
// are never searched for keywords (per contract, only text blocks drive
// keyword matching).
type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

// imageURL is the image_url sub-object of an image_url content part.
// Only the URL field is consumed; the detail field (if present) is
// ignored.
type imageURL struct {
	URL string `json:"url"`
}

// assistantMessage is the unified message shape used for both the
// non-streaming response's `message` field and the streaming response's
// `delta` field. The omitempty tags keep the wire shape compact while
// still emitting explicit empty strings where the validated prototype
// does (e.g. an empty content on the reasoning delta). ToolCalls is
// populated only when the response is a tool-call (finish_reason =
// "tool_calls"); it is omitted otherwise so the text-response wire
// shape is unchanged.
type assistantMessage struct {
	Role             string          `json:"role,omitempty"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
	Content          string          `json:"content,omitempty"`
	ToolCalls        []*toolCallResp `json:"tool_calls,omitempty"`
}

// toolCallResp is one entry of the response message's tool_calls array.
// It follows the OpenAI Chat Completions format: Type is always
// "function", Function.Arguments is a JSON-stringified object.
type toolCallResp struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function toolCallRespFunc `json:"function"`
}

// toolCallRespFunc is the function sub-object of a response tool_call.
type toolCallRespFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
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
	ID      string    `json:"id"`
	Object  string    `json:"object"`
	Created int64     `json:"created"`
	Model   string    `json:"model"`
	Choices []*choice `json:"choices"`
}

// generateResponseID returns a unique chat completion ID. Each call
// produces a different value so that LangGraph's addMessages reducer
// treats AIMessages from separate turns as distinct entries rather than
// replacements.
func generateResponseID() string {
	return fmt.Sprintf("fake-%d-%04d", time.Now().UnixNano(), rand.IntN(10000))
}

// generateToolCallID returns a unique tool-call ID. The "call_" prefix
// matches the OpenAI convention so LangChain's parser accepts it without
// special-casing.
func generateToolCallID() string {
	return fmt.Sprintf("call_%d_%04d", time.Now().UnixNano(), rand.IntN(10000))
}

// finishReason values per the OpenAI Chat Completions schema. "stop" is
// emitted for plain text responses; "tool_calls" when the assistant
// message carries one or more tool_calls entries.
const (
	finishStop      = "stop"
	finishToolCalls = "tool_calls"
)

// responseSpec is the handler-internal description of what to serialise
// into the response body, abstracting over the two response shapes the
// fake-LLM can emit (text vs tool-call). Exactly one meaningful path is
// taken per request:
//
//   - TextPath: Reasoning + Text are set, ToolCall is nil → the response
//     carries an assistant message with content and finish_reason "stop".
//   - ToolCallPath: ToolCall is non-nil → the response carries an
//     assistant message with tool_calls and finish_reason "tool_calls";
//     Text is emitted as content only when non-empty (rare for tool-call
//     responses but allowed by the schema).
//
// Stall pauses the stream after the first chunk for either path: the
// handler writes the opening delta, flushes it, then blocks until the
// request context is cancelled (the client's abort), simulating a
// mid-stream stall with the connection kept alive (specs/043-llm-stream-
// stall-recovery — the feature's idle timeout is the expected trigger).
type responseSpec struct {
	Reasoning string
	Text      string
	ToolCall  *ToolCall
	Stall     bool
}

// isToolCall reports whether the spec describes a tool-call response.
func (s responseSpec) isToolCall() bool { return s.ToolCall != nil }

// specFromMessage adapts a keyword-matched Message into a responseSpec.
// When the Message carries a ToolCall the spec takes the ToolCallPath
// (the response carries tool_calls + finish_reason "tool_calls"); otherwise
// it takes the TextPath (reasoning + text + finish_reason "stop"). A nil
// ToolCall reproduces the original text-only behaviour exactly, so existing
// Message entries are unaffected. Stall is passed through so a stall-marked
// Message pauses the stream after its first chunk (the large test's
// stall-recovery trigger).
func specFromMessage(msg *Message) responseSpec {
	return responseSpec{Reasoning: msg.Reasoning, Text: msg.Text, ToolCall: msg.ToolCall, Stall: msg.Stall}
}

// specFromTool adapts a matched ToolConfig's RespondWith into a
// responseSpec. Text and ToolCall are passed through verbatim; the
// handler decides which path to take based on isToolCall().
func specFromTool(tc *ToolConfig) responseSpec {
	return responseSpec{
		Text:     tc.RespondWith.Text,
		ToolCall: tc.RespondWith.ToolCall,
	}
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
// bearer (no validation), decodes the request body, and dispatches by
// the role of the LAST message:
//
//   - role "tool" → tools branch: MatchToolResult against store.Tools()
//     by tool_name (+ optional match_result_contains), producing either
//     a text response or a tool_call response.
//   - any other role (user/assistant/system) → messages branch: the
//     existing keyword-match path against store.Messages().
//
// Both branches share the streaming/non-streaming writers; the spec
// produced by the branch decides the wire shape.
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

	if err := validateImageContent(req.Messages); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	spec := h.dispatch(req.Messages)

	respID := generateResponseID()
	if req.Stream {
		serveStreaming(w, r, spec, respID)
		return
	}
	serveNonStreaming(w, spec, respID)
}

// logSystemPrompts logs every role:"system" message of a request at INFO.
// The keyword matcher only reads user text (README.md §4), so the system
// content is otherwise unobservable — logging it lets large-test operators
// verify prompt injection (e.g. the saolei team planner's "## Player 可用工具"
// tool-description section, specs/037-saolei-team-optimize FR-016) via the
// fake-llm logs (signoz) after a test drives the planner.
func logSystemPrompts(messages []*messageParam) {
	for _, m := range messages {
		if !strings.EqualFold(m.Role, "system") {
			continue
		}
		slog.Info("system prompt received",
			slog.String("snippet", snippet(decodeContent(m.Content), maxSystemPromptSnippetRunes)))
	}
}

// maxSystemPromptSnippetRunes caps the system-prompt log line so a verbose
// prompt cannot blow up the log (a full planner prompt is well under this).
const maxSystemPromptSnippetRunes = 4000

// dispatch inspects the last message role and returns the responseSpec
// for the appropriate branch. It is split out of ServeHTTP so tests can
// exercise the dispatch logic without going through the HTTP layer.
func (h *ChatHandler) dispatch(messages []*messageParam) responseSpec {
	logSystemPrompts(messages)

	if lastMessageRole(messages) == "tool" {
		toolName := extractToolName(messages)
		resultText := decodeContent(lastMessageContent(messages))
		tc, _ := MatchToolResult(h.store.Tools(), toolName, resultText, h.rng)
		return specFromTool(tc)
	}

	userText := lastUserText(messages)
	msg, _ := Match(h.store.Messages(), userText, h.rng)
	return specFromMessage(msg)
}

// lastUserText extracts the text of the LAST message whose role equals
// "user" (compared case-insensitively). When Content is a string it is
// returned as-is; when Content is an array the .text fields of every
// {type:"text"} entry are joined with a single space. Returns the
// empty string when no user message is present — Match will then
// deterministically fall through to the random fallback.
func lastUserText(messages []*messageParam) string {
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if !strings.EqualFold(m.Role, "user") {
			continue
		}
		return decodeContent(m.Content)
	}
	return ""
}

// lastMessageRole returns the role of the last message in the slice
// (case-preserved), or "" when the slice is empty.
func lastMessageRole(messages []*messageParam) string {
	if len(messages) == 0 {
		return ""
	}
	return messages[len(messages)-1].Role
}

// lastMessageContent returns the Content of the last message in the
// slice, or nil when the slice is empty.
func lastMessageContent(messages []*messageParam) json.RawMessage {
	if len(messages) == 0 {
		return nil
	}
	return messages[len(messages)-1].Content
}

// extractToolName resolves the tool_name for a tool-role last message.
// It checks the message's `name` field first (set by LangChain's
// completions converter), then falls back to searching backward for the
// assistant message whose tool_call ID matches the tool message's
// `tool_call_id`. Returns "" when neither path yields a name.
func extractToolName(messages []*messageParam) string {
	if len(messages) == 0 {
		return ""
	}
	last := messages[len(messages)-1]
	if last.Name != "" {
		return last.Name
	}
	if last.ToolCallID == "" {
		return ""
	}
	for i := len(messages) - 2; i >= 0; i-- {
		for _, tc := range messages[i].ToolCalls {
			if tc.ID == last.ToolCallID {
				return tc.Function.Name
			}
		}
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

// serveNonStreaming writes the single chat.completion JSON object.
// For a text response it carries BOTH reasoning_content and content in
// the assistant message with finish_reason "stop". For a tool-call
// response it carries tool_calls (and optional content) with
// finish_reason "tool_calls".
func serveNonStreaming(w http.ResponseWriter, spec responseSpec, respID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	finish := finishStop
	msg := &assistantMessage{
		Role:             "assistant",
		ReasoningContent: spec.Reasoning,
		Content:          spec.Text,
	}
	if spec.isToolCall() {
		finish = finishToolCalls
		msg.ToolCalls = []*toolCallResp{buildToolCallResp(spec.ToolCall)}
	}

	resp := completionResponse{
		ID:      respID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   FakeModel,
		Choices: []*choice{
			{
				Index:        0,
				Message:      msg,
				FinishReason: &finish,
			},
		},
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode non-streaming response",
			slog.String("error", err.Error()))
	}
}

// buildToolCallResp converts a config ToolCall into the OpenAI wire
// shape, generating a unique call ID and JSON-stringifying the
// arguments map.
func buildToolCallResp(tc *ToolCall) *toolCallResp {
	args := "{}"
	if len(tc.Arguments) > 0 {
		if b, err := json.Marshal(tc.Arguments); err == nil {
			args = string(b)
		}
	}
	return &toolCallResp{
		ID:   generateToolCallID(),
		Type: "function",
		Function: toolCallRespFunc{
			Name:      tc.Name,
			Arguments: args,
		},
	}
}

// serveStreaming writes the SSE event stream. For a text response the
// chunk sequence is, in order:
//
//  1. delta with role:"assistant" + reasoning_content (so the
//     @langchain/openai parser treats the stream as an assistant turn);
//  2. delta with content (the visible answer);
//  3. delta {} with finish_reason:"stop";
//  4. the literal "data: [DONE]" terminator.
//
// For a tool-call response the sequence is:
//
//  1. delta with role:"assistant" + tool_calls (the full call in one
//     delta — LangChain's streaming parser tolerates this);
//  2. delta {} with finish_reason:"tool_calls";
//  3. "data: [DONE]".
//
// Each frame is flushed before the next is written so a slow client
// observes progressive output. If the ResponseWriter does not implement
// http.Flusher we surface a 500 once, before any chunk is emitted, so
// the client does not receive a half-finished stream.
//
// When spec.Stall is set the stream stops after the FIRST chunk: the
// connection stays alive (the http.Server has no WriteTimeout) but no
// further data is written until r.Context() is cancelled by the caller —
// the "TCP alive, no SSE data" failure mode of specs/043-llm-stream-
// stall-recovery. The handler then returns normally; the stalled request
// is indistinguishable from an aborted client connection, which is the
// point (LangGraph's idle-timeout abort is what unblocks it).
func serveStreaming(w http.ResponseWriter, r *http.Request, spec responseSpec, respID string) {
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
	var chunks []*completionResponse
	if spec.isToolCall() {
		chunks = toolCallStreamChunks(respID, now, spec.ToolCall)
	} else {
		chunks = textStreamChunks(respID, now, spec)
	}

	for i, chunk := range chunks {
		data, err := json.Marshal(chunk)
		if err != nil {
			slog.Error("failed to marshal stream chunk",
				slog.String("error", err.Error()))
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		if spec.Stall && i == 0 {
			<-r.Context().Done()
			return
		}
	}

	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// textStreamChunks builds the 3-chunk sequence for a plain text response
// (role+reasoning delta, content delta, empty delta with finish "stop").
func textStreamChunks(respID string, now int64, spec responseSpec) []*completionResponse {
	return []*completionResponse{
		{
			ID: respID, Object: "chat.completion.chunk",
			Created: now, Model: FakeModel,
			Choices: []*choice{{
				Index: 0,
				Delta: &assistantMessage{
					Role:             "assistant",
					ReasoningContent: spec.Reasoning,
				},
				FinishReason: nil,
			}},
		},
		{
			ID: respID, Object: "chat.completion.chunk",
			Created: now, Model: FakeModel,
			Choices: []*choice{{
				Index: 0,
				Delta: &assistantMessage{
					Content: spec.Text,
				},
				FinishReason: nil,
			}},
		},
		{
			ID: respID, Object: "chat.completion.chunk",
			Created: now, Model: FakeModel,
			Choices: []*choice{{
				Index:        0,
				Delta:        new(assistantMessage),
				FinishReason: strPtr(finishStop),
			}},
		},
	}
}

// toolCallStreamChunks builds the 2-chunk sequence for a tool-call
// response (role+tool_calls delta, empty delta with finish "tool_calls").
func toolCallStreamChunks(respID string, now int64, tc *ToolCall) []*completionResponse {
	return []*completionResponse{
		{
			ID: respID, Object: "chat.completion.chunk",
			Created: now, Model: FakeModel,
			Choices: []*choice{{
				Index: 0,
				Delta: &assistantMessage{
					Role:      "assistant",
					ToolCalls: []*toolCallResp{buildToolCallResp(tc)},
				},
				FinishReason: nil,
			}},
		},
		{
			ID: respID, Object: "chat.completion.chunk",
			Created: now, Model: FakeModel,
			Choices: []*choice{{
				Index:        0,
				Delta:        new(assistantMessage),
				FinishReason: strPtr(finishToolCalls),
			}},
		},
	}
}

// strPtr returns a pointer to s, used so FinishReason can serialise as
// the literal string "stop" rather than null in the final chunk.
func strPtr(s string) *string {
	return &s
}

// pngMagic is the PNG file signature.
var pngMagic = []byte{0x89, 0x50, 0x4e, 0x47}

// validateImageContent rejects malformed image_url data URLs the way a
// real OpenAI-compatible provider would ("Param Incorrect"), and logs
// the URL structure so test runs reveal what the agent actually
// serialised (real base64 vs Buffer toString vs comma-separated bytes).
func validateImageContent(messages []*messageParam) error {
	for _, msg := range messages {
		if len(msg.Content) == 0 || msg.Content[0] != '[' {
			continue
		}
		var parts []contentPart
		if err := json.Unmarshal(msg.Content, &parts); err != nil {
			continue
		}
		for _, p := range parts {
			if p.Type != "image_url" || p.ImageURL == nil {
				continue
			}
			if err := validateDataURL(p.ImageURL.URL); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateDataURL validates one image data URL: data: prefix, ;base64
// marker, image/* MIME, and decodable base64. Logs the URL prefix and
// decoded length for diagnosis.
func validateDataURL(rawURL string) error {
	slog.Info("image_url part received",
		slog.Int("url_len", len(rawURL)),
		slog.String("url_prefix", truncPrefix(rawURL, 100)),
	)

	if !strings.HasPrefix(rawURL, "data:") {
		return fmt.Errorf("Param Incorrect: image_url is not a data URL (prefix=%q)", truncPrefix(rawURL, 40))
	}

	commaIdx := strings.Index(rawURL, ",")
	if commaIdx < 0 {
		return fmt.Errorf("Param Incorrect: data URL has no comma separator")
	}

	meta := rawURL[:commaIdx]
	b64data := rawURL[commaIdx+1:]

	if !strings.HasSuffix(meta, ";base64") {
		return fmt.Errorf("Param Incorrect: data URL meta lacks ;base64 (meta=%q)", meta)
	}

	mime := strings.TrimSuffix(strings.TrimPrefix(meta, "data:"), ";base64")
	if !strings.HasPrefix(mime, "image/") {
		return fmt.Errorf("Param Incorrect: invalid MIME %q", mime)
	}

	decoded, err := base64.StdEncoding.DecodeString(b64data)
	if err != nil {
		slog.Error("image_url base64 decode failed",
			slog.String("error", err.Error()),
			slog.Int("b64_len", len(b64data)),
			slog.String("b64_prefix", truncPrefix(b64data, 80)),
		)
		return fmt.Errorf("Param Incorrect: invalid base64 (%v)", err)
	}

	slog.Info("image_url decoded successfully",
		slog.Int("decoded_len", len(decoded)),
		slog.String("mime", mime),
		slog.Bool("is_png", len(decoded) >= 4 && bytes.HasPrefix(decoded, pngMagic)),
	)
	return nil
}

func truncPrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
