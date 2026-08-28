// responses.go implements the OpenAI Responses wire endpoint
// POST /v1/responses consumed by the @dominion/dsh-llm-glm adapter
// (specs/049-agent-v2-dsh-init/contracts/fake-responses-wire.md). It
// reuses the shared template store (keyword matching, multi-turn
// conditions, chunked think with controllable delays) and projects the
// matched template's reasoning/text fields onto the Responses SSE event
// vocabulary. The chat-completions endpoint is untouched.
package service

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Deterministic wire identities: the same request always observes the
// same ids and usage numbers (fake-responses-wire.md §2 invariant 3).
const (
	responsesRespID = "resp_fake_1"
	responsesRsnID  = "rs_fake_1"
	responsesMsgID  = "msg_fake_1"
)

// responsesRequest is the subset of the OpenAI /v1/responses request
// schema the handler consumes. Model and instructions are decoded but
// ignored (the template catalog is aligned on the agent_v2 side); Input
// is a pointer so an absent array (missing input) is distinguishable
// from an empty one — the former is a 400, the latter falls through to
// the deterministic fallback.
type responsesRequest struct {
	Model        string                 `json:"model"`
	Instructions string                 `json:"instructions"`
	Input        *[]*responsesInputItem `json:"input"`
	Stream       bool                   `json:"stream"`
}

// responsesInputItem is one entry of the request's input array. Only
// message items are consumed; every other item type (reasoning,
// function_call, …) is ignored (fake-responses-wire.md §1).
type responsesInputItem struct {
	Type    string          `json:"type"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// responsesContentPart is one element of the array-form Content. Both the
// input_text (user side) and output_text (assistant side) types
// contribute their Text to matching (fake-responses-wire.md §1).
type responsesContentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// responsesMessage is one normalized request message: role plus decoded
// text content, the shape the Responses matcher operates on.
type responsesMessage struct {
	Role string
	Text string
}

// ResponsesHandler serves POST /v1/responses. It is stateless across
// requests: every request matches against the same store snapshot as the
// chat-completions endpoint.
type ResponsesHandler struct {
	store *MessageStore
}

// NewResponsesHandler wires the handler to a loaded MessageStore. The
// store must already be loaded and validated (see NewMessageStore).
func NewResponsesHandler(store *MessageStore) *ResponsesHandler {
	return &ResponsesHandler{store: store}
}

// ServeHTTP implements http.Handler. Any Authorization bearer is accepted
// without validation (header tolerance, fake-responses-wire.md §1).
// Missing model, missing input, or a malformed body yields 400; a
// matched template streams (or, for stream:false, returns as one JSON
// object) its think/text projection; a Failure template emits
// response.failed with the configured code/message.
func (h *ResponsesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req responsesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}
	if req.Model == "" {
		http.Error(w, "invalid request: missing model", http.StatusBadRequest)
		return
	}
	if req.Input == nil {
		http.Error(w, "invalid request: missing input", http.StatusBadRequest)
		return
	}

	messages := normalizeResponsesInput(*req.Input)
	msg := matchResponses(h.store.Messages(), messages)
	spec := specFromMessage(msg)

	if req.Stream {
		serveResponsesStreaming(w, r, spec, msg)
		return
	}
	serveResponsesNonStreaming(w, spec, msg)
}

// normalizeResponsesInput flattens the input items into messages: message
// items contribute their role and the joined input_text/output_text text
// (string content counts as one text part); unknown item types are
// dropped. Malformed content degrades to the empty string so matching
// falls through deterministically rather than failing the request.
func normalizeResponsesInput(items []*responsesInputItem) []responsesMessage {
	var messages []responsesMessage
	for _, item := range items {
		if item == nil || item.Type != "message" {
			continue
		}
		messages = append(messages, responsesMessage{
			Role: item.Role,
			Text: decodeResponsesContent(item.Content),
		})
	}
	return messages
}

// decodeResponsesContent handles both content forms: a JSON string is the
// whole text; a JSON array contributes the input_text/output_text parts,
// joined with a single space.
func decodeResponsesContent(raw json.RawMessage) string {
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
	var parts []responsesContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}
	var texts []string
	for _, p := range parts {
		if p.Type == "input_text" || p.Type == "output_text" {
			texts = append(texts, p.Text)
		}
	}
	return strings.Join(texts, " ")
}

// matchResponses picks the template for one Responses request with the
// demo fake-llm priorities (specs/047-dsh-chat-demo/research.md D7,
// projected by specs/049-agent-v2-dsh-init/contracts/fake-responses-wire.md
// §3):
//
//  1. Multi-turn templates (history_keywords non-empty or min_turn > 1)
//     whose every condition holds — the keyword condition (vacuous when
//     the template declares no keywords), ALL history keywords each
//     hitting some message before the last user message, and the
//     user-message count reaching min_turn. Conflicts resolve to the most
//     declared conditions first, then the lowest Name.
//  2. Pure keyword templates (non-multi-turn, non-empty Keywords) whose
//     ANY keyword is a case-insensitive substring of the last user
//     message — ties broken by the lowest Name.
//  3. Deterministic fallback: a stable-seed pick over the eligible pool
//     (no tool_call, no failure, non-multi-turn, non-hang-capable)
//     seeded by the request's full text, so the same request always
//     yields the same reply.
//
// The store cannot be empty (startup validation), so the third priority
// always returns a template for a validated store.
func matchResponses(templates []*Message, messages []responsesMessage) *Message {
	loweredLast := strings.ToLower(lastResponsesUserText(messages))

	if best := matchResponsesMultiTurn(templates, loweredLast, loweredResponsesHistory(messages), userTurnCount(messages)); best != nil {
		return best
	}

	var best *Message
	for _, t := range templates {
		if t.isMultiTurnTemplate() || len(t.Keywords) == 0 {
			continue
		}
		if !anyKeywordMatches(t.Keywords, loweredLast) {
			continue
		}
		if best == nil || t.Name < best.Name {
			best = t
		}
	}
	if best != nil {
		return best
	}

	pool := responsesFallbackPool(templates)
	pick := pool[responsesSeed(messages)%uint64(len(pool))]
	slog.Info("no responses template condition matched, returning fallback template",
		slog.String("user_snippet", snippet(lastResponsesUserText(messages), maxSnippetRunes)),
		slog.String("fallback_name", pick.Name),
	)
	return pick
}

// matchResponsesMultiTurn resolves matching priority 1; the pick is the
// most specific (more declared conditions first, then the lowest Name).
func matchResponsesMultiTurn(templates []*Message, loweredLast string, loweredHistory []string, turn int) *Message {
	var best *Message
	for _, t := range templates {
		if !t.isMultiTurnTemplate() {
			continue
		}
		if len(t.Keywords) > 0 && !anyKeywordMatches(t.Keywords, loweredLast) {
			continue
		}
		if !allHistoryKeywordsHit(t.HistoryKeywords, loweredHistory) {
			continue
		}
		if turn < t.effectiveMinTurn() {
			continue
		}
		if best == nil || moreSpecificResponses(t, best) {
			best = t
		}
	}
	return best
}

// isMultiTurnTemplate reports whether the template declares multi-turn
// conditions (fake-responses-wire.md §3 多轮条件): history_keywords
// non-empty or min_turn above the default 1.
func (m *Message) isMultiTurnTemplate() bool {
	return len(m.HistoryKeywords) > 0 || m.effectiveMinTurn() > 1
}

// moreSpecificResponses orders two fully-matched multi-turn templates:
// more declared conditions first, then the lowest Name.
func moreSpecificResponses(a, b *Message) bool {
	if ca, cb := declaredResponsesConditions(a), declaredResponsesConditions(b); ca != cb {
		return ca > cb
	}
	return a.Name < b.Name
}

// declaredResponsesConditions counts the template's non-vacuous declared
// matching conditions; vacuous declarations never constrain matching and
// do not count toward specificity.
func declaredResponsesConditions(m *Message) int {
	n := 0
	if len(m.Keywords) > 0 {
		n++
	}
	if len(m.HistoryKeywords) > 0 {
		n++
	}
	if m.effectiveMinTurn() > 1 {
		n++
	}
	return n
}

// allHistoryKeywordsHit reports whether EVERY history keyword is a
// case-insensitive substring of at least one lowered history text.
func allHistoryKeywordsHit(historyKeywords, loweredHistory []string) bool {
	for _, kw := range historyKeywords {
		loweredKw := strings.ToLower(kw)
		hit := false
		for _, h := range loweredHistory {
			if strings.Contains(h, loweredKw) {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	return true
}

// loweredResponsesHistory lower-cases the text of every message EXCEPT
// the last user message — the history set. On a first turn the history is
// empty, so any declared history keyword misses and the multi-turn branch
// cannot fire.
func loweredResponsesHistory(messages []responsesMessage) []string {
	lastIdx := lastResponsesUserIndex(messages)
	var texts []string
	for i, m := range messages {
		if i != lastIdx {
			texts = append(texts, strings.ToLower(m.Text))
		}
	}
	return texts
}

// lastResponsesUserIndex returns the index of the LAST user-role message,
// or -1 when none is present.
func lastResponsesUserIndex(messages []responsesMessage) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "user") {
			return i
		}
	}
	return -1
}

// lastResponsesUserText returns the text of the LAST user-role message, or
// the empty string when none is present.
func lastResponsesUserText(messages []responsesMessage) string {
	i := lastResponsesUserIndex(messages)
	if i < 0 {
		return ""
	}
	return messages[i].Text
}

// userTurnCount counts the request's user-role messages.
func userTurnCount(messages []responsesMessage) int {
	turn := 0
	for _, m := range messages {
		if strings.EqualFold(m.Role, "user") {
			turn++
		}
	}
	return turn
}

// responsesFallbackPool returns the templates eligible for the
// deterministic no-match fallback: no tool_call (an accidental tool
// trigger), no failure injection (an accidental failure), non-multi-turn,
// and non-hang-capable (an accidental stall or delay). The pool is never
// empty for a validated store that contains at least one plain template;
// otherwise the full set is used rather than panicking on IntN(0).
func responsesFallbackPool(templates []*Message) []*Message {
	var pool []*Message
	for _, t := range templates {
		if t.ToolCall == nil && t.Failure == nil && !t.isMultiTurnTemplate() && !isHangCapable(t) {
			pool = append(pool, t)
		}
	}
	if len(pool) == 0 {
		return templates
	}
	return pool
}

// responsesSeed hashes the request messages' full text into a stable
// uint64 seed (FNV-1a over role + text pairs, in request order), which is
// what makes the fallback pick deterministic across repeated calls.
func responsesSeed(messages []responsesMessage) uint64 {
	h := fnv.New64a()
	for _, m := range messages {
		fmt.Fprintf(h, "%s\x00%s\x00", m.Role, m.Text)
	}
	return h.Sum64()
}

// responsesUsage is the deterministic usage projection
// (fake-responses-wire.md §2 invariant 3): every value derives from the
// matched template's lengths, never from randomness.
type responsesUsage struct {
	InputTokens         int                        `json:"input_tokens"`
	OutputTokens        int                        `json:"output_tokens"`
	OutputTokensDetails responsesUsageTokensDetail `json:"output_tokens_details"`
}

// responsesUsageTokensDetail carries the reasoning-token count of the
// OpenAI usage payload.
type responsesUsageTokensDetail struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

// usageFromSpec derives the usage constants from the matched template:
// reasoning tokens are the think length, output tokens the think+text
// length, and input tokens mirror the template's total length.
func usageFromSpec(spec responseSpec) responsesUsage {
	reasoning := 0
	for _, piece := range spec.Reasoning {
		reasoning += len([]rune(piece))
	}
	text := len([]rune(spec.Text))
	return responsesUsage{
		InputTokens:         reasoning + text,
		OutputTokens:        reasoning + text,
		OutputTokensDetails: responsesUsageTokensDetail{ReasoningTokens: reasoning},
	}
}

// serveResponsesNonStreaming writes the stream:false shape: one JSON
// response object whose output carries the reasoning and message items
// with the full text, plus the derived usage. A Failure template returns
// the failed status with the configured error.
func serveResponsesNonStreaming(w http.ResponseWriter, spec responseSpec, msg *Message) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	resp := map[string]any{
		"id":     responsesRespID,
		"object": "response",
		"status": "completed",
	}
	if msg.Failure != nil {
		// The failure path carries no usage, mirroring the streaming
		// path's response.failed event.
		resp["status"] = "failed"
		resp["error"] = map[string]any{
			"code":    msg.Failure.Code,
			"message": msg.Failure.Message,
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			slog.Error("failed to encode responses non-streaming body",
				slog.String("error", err.Error()))
		}
		return
	}

	resp["usage"] = usageFromSpec(spec)
	var output []map[string]any
	if think := strings.Join(spec.Reasoning, ""); think != "" {
		output = append(output, map[string]any{
			"type": "reasoning",
			"id":   responsesRsnID,
		})
	}
	output = append(output, map[string]any{
		"type": "message",
		"id":   responsesMsgID,
		"role": "assistant",
		"content": []map[string]any{
			{"type": "output_text", "text": spec.Text},
		},
	})
	resp["output"] = output

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode responses non-streaming body",
			slog.String("error", err.Error()))
	}
}

// serveResponsesStreaming writes the SSE event stream of
// fake-responses-wire.md §2, in order:
//
//  1. response.created;
//  2. when the template has think: one output_item.added (reasoning
//     item, output_index 0), then one reasoning_summary_text.delta per
//     think piece — the configured chunk_delays gap precedes each piece
//     after the first (context-aware, so caller aborts unblock promptly);
//  3. one output_item.added (message item, next output_index — think and
//     text never share an output item, §2 invariant 2), one
//     output_text.delta with the full text, and the output_item.done
//     carrying the complete message item;
//  4. response.completed with the derived usage, always last (§2
//     invariant 3).
//
// A Failure template emits response.created then response.failed with the
// configured code/message and nothing else (§2 invariant 4).
//
// The chat-completions endpoint's permanent-stall simulation (stall /
// stall_after, specs/043-llm-stream-stall-recovery / specs/046-fake-llm-
// think-chunking) is a chat-completions facility and is deliberately not
// projected here: the Responses handler honors only the inter-chunk
// chunk_delays, which is what the FR-012 queue-window scenarios need.
func serveResponsesStreaming(w http.ResponseWriter, r *http.Request, spec responseSpec, msg *Message) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	writeEvent(w, flusher, "response.created", map[string]any{
		"type":     "response.created",
		"response": map[string]any{"id": responsesRespID, "status": "in_progress"},
	})

	if msg.Failure != nil {
		writeEvent(w, flusher, "response.failed", map[string]any{
			"type": "response.failed",
			"response": map[string]any{
				"id":     responsesRespID,
				"status": "failed",
				"error": map[string]any{
					"code":    msg.Failure.Code,
					"message": msg.Failure.Message,
				},
			},
		})
		return
	}

	textOutputIndex := 0
	if len(spec.Reasoning) > 0 {
		textOutputIndex = 1
		writeEvent(w, flusher, "response.output_item.added", map[string]any{
			"type":         "response.output_item.added",
			"output_index": 0,
			"item":         map[string]any{"type": "reasoning", "id": responsesRsnID},
		})
		for i, piece := range spec.Reasoning {
			if i >= 1 {
				if d := delayBefore(spec, i); d > 0 {
					select {
					case <-time.After(d):
					case <-r.Context().Done():
						return
					}
				}
			}
			writeEvent(w, flusher, "response.reasoning_summary_text.delta", map[string]any{
				"type":         "response.reasoning_summary_text.delta",
				"item_id":      responsesRsnID,
				"output_index": 0,
				"delta":        piece,
			})
		}
	}

	writeEvent(w, flusher, "response.output_item.added", map[string]any{
		"type":         "response.output_item.added",
		"output_index": textOutputIndex,
		"item":         map[string]any{"type": "message", "role": "assistant"},
	})
	writeEvent(w, flusher, "response.output_text.delta", map[string]any{
		"type":         "response.output_text.delta",
		"item_id":      responsesMsgID,
		"output_index": textOutputIndex,
		"delta":        spec.Text,
	})
	writeEvent(w, flusher, "response.output_item.done", map[string]any{
		"type":         "response.output_item.done",
		"output_index": textOutputIndex,
		"item": map[string]any{
			"type": "message",
			"id":   responsesMsgID,
			"role": "assistant",
			"content": []map[string]any{
				{"type": "output_text", "text": spec.Text},
			},
		},
	})
	writeEvent(w, flusher, "response.completed", map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id":     responsesRespID,
			"status": "completed",
			"usage":  usageFromSpec(spec),
		},
	})
}

// writeEvent emits one SSE frame ("event:" line + JSON "data:" line) and
// flushes it, so a slow client observes progressive output. Every frame
// is logged for test operators correlating runs in signoz.
func writeEvent(w http.ResponseWriter, flusher http.Flusher, event string, payload map[string]any) {
	data, err := json.Marshal(payload)
	if err != nil {
		slog.Error("failed to marshal responses stream event",
			slog.String("event", event),
			slog.String("error", err.Error()))
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	flusher.Flush()
	slog.Info("responses stream event emitted", slog.String("event", event))
}
