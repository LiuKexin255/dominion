// Package service holds the fake-llm message store: message templates
// embedded into the binary, validated at startup, and served by the
// HTTP handler.
package service

import "time"

// Message is a single templated LLM response. It is keyed by Name for
// uniqueness and matched by Keywords at request time (T2). Reasoning
// and Text are the literal strings returned to the caller.
//
// ToolCall optionally turns a Message into a tool-call trigger: when a
// user/assistant/system turn matches the Keywords, the response carries a
// tool_calls entry (finish_reason "tool_calls") instead of text. This lets
// large tests drive the model→tool_call→execution chain from a plain user
// turn — the original spec 012 (line 162) scoped tool-call simulation out,
// but the feature was later extended to support it. A nil ToolCall preserves
// the original text-only behaviour, so existing Message entries are
// unchanged.
//
// Name, Reasoning and Text are the single source of truth asserted by
// the T6 integration tests; the testdata files are the only place they
// are defined.
//
// Stall simulates a mid-stream LLM stall (specs/043-llm-stream-stall-recovery):
// the streaming handler emits the first chunk (the role+reasoning delta)
// and then blocks without closing the connection — no more data arrives
// until the caller cancels the request. The connection staying alive while
// data stops is exactly the failure mode the feature's idle timeout detects.
// It is the legacy shorthand for StallAfter 0 (specs/046-fake-llm-think-
// chunking/research.md D3): a nil/absent Stall preserves the original
// streaming behaviour, so existing Message entries are unchanged.
//
// StallAfter positions the permanent stall after a chosen reasoning chunk
// (specs/046-fake-llm-think-chunking — contract
// specs/046-fake-llm-think-chunking/contracts/template-config.md §2): a
// 0-based index into the effective reasoning pieces (the chunked form or
// the legacy single Reasoning string) after which the streaming handler
// blocks until the caller cancels the request. It generalises the legacy
// Stall shorthand — stall:true ≡ stall_after:0 — and wins when both are
// set (an explicit position is more specific than the boolean). A
// nil/absent StallAfter preserves the original streaming behaviour.
//
// ReasoningChunks and ChunkDelays are the chunked-reasoning form
// (specs/046-fake-llm-think-chunking — contract
// specs/046-fake-llm-think-chunking/contracts/template-config.md §2):
// ReasoningChunks declares the think content as an explicit ordered list —
// the streaming handler emits one reasoning_content SSE delta per entry —
// and is mutually exclusive with the legacy Reasoning string (validation
// rule V4, specs/046-fake-llm-think-chunking/research.md D6). ChunkDelays
// carries the optional inter-chunk output intervals as Go
// time.ParseDuration strings; ChunkDelays[i] is the delay applied before
// emitting ReasoningChunks[i+1], missing entries default to 0, and the
// list length must not exceed len(ReasoningChunks)-1 (validation rule V2,
// specs/046-fake-llm-think-chunking/research.md D2).
// ResponsesOnly marks a template as serving the /v1/responses endpoint
// only (specs/049-agent-v2-dsh-init/contracts/fake-responses-wire.md §3).
// Responses-only templates stay out of the chat-completions no-match
// random fallback pool, so a no-match chat turn never randomly picks a
// template authored for the agent_v2 large tests; the keyword path still
// serves them, and the Responses endpoint matches through its own matcher
// (responses.go).
//
// HistoryKeywords and MinTurn are the optional multi-turn conditions of
// the Responses projection (fake-responses-wire.md §3, aligned with the
// demo fake-llm pattern, specs/047-dsh-chat-demo/research.md D7): EVERY
// history keyword must hit some message before the last user message, and
// the request's user-message count must reach MinTurn (default 1). For a
// multi-turn template an empty Keywords leaves the keyword condition
// vacuous. Templates declaring either condition are also multi-turn
// templates for isResponsesOnly purposes.
//
// Failure injects a provider failure into the Responses stream
// (fake-responses-wire.md §2 invariant 4): a matched template carrying a
// Failure emits response.failed with the configured code/message instead of
// the think/text payload. Failure templates are excluded from both
// endpoints' fallback pools so an unrelated turn never fails by accident.
type ResponseFailure struct {
	Code    string `json:"code" yaml:"code"`
	Message string `json:"message" yaml:"message"`
}

type Message struct {
	Name            string           `json:"name" yaml:"name"`
	Keywords        []string         `json:"keywords" yaml:"keywords"`
	Reasoning       string           `json:"reasoning" yaml:"reasoning"`
	ReasoningChunks []string         `json:"reasoning_chunks,omitempty" yaml:"reasoning_chunks,omitempty"`
	ChunkDelays     []string         `json:"chunk_delays,omitempty" yaml:"chunk_delays,omitempty"`
	Text            string           `json:"text" yaml:"text"`
	ToolCall        *ToolCall        `json:"tool_call,omitempty" yaml:"tool_call,omitempty"`
	Stall           bool             `json:"stall,omitempty" yaml:"stall,omitempty"`
	StallAfter      *int             `json:"stall_after,omitempty" yaml:"stall_after,omitempty"`
	ResponsesOnly   bool             `json:"responses_only,omitempty" yaml:"responses_only,omitempty"`
	HistoryKeywords []string         `json:"history_keywords,omitempty" yaml:"history_keywords,omitempty"`
	MinTurn         int              `json:"min_turn,omitempty" yaml:"min_turn,omitempty"`
	Failure         *ResponseFailure `json:"failure,omitempty" yaml:"failure,omitempty"`
}

// effectiveMinTurn returns the turn lower bound with the contract default
// applied: an undeclared (zero) MinTurn means 1
// (specs/047-dsh-chat-demo/contracts/fake-llm-templates.md §2 semantics,
// aligned by specs/049-agent-v2-dsh-init/contracts/fake-responses-wire.md
// §3). Negative values are clamped so matching stays well-defined.
func (m *Message) effectiveMinTurn() int {
	if m.MinTurn < 1 {
		return 1
	}
	return m.MinTurn
}

// isResponsesOnly reports whether the template serves the /v1/responses
// endpoint only: an explicit responses_only marker, declared multi-turn
// conditions (history_keywords / min_turn above the default), or a failure
// injection. Such templates MUST stay out of the chat-completions no-match
// random fallback pool (see the ResponsesOnly field doc).
func (m *Message) isResponsesOnly() bool {
	return m.ResponsesOnly || len(m.HistoryKeywords) > 0 || m.effectiveMinTurn() > 1 || m.Failure != nil
}

// ToolConfig is a single templated response to a tool result message.
// It is keyed by Name for uniqueness, matched by ToolName at request
// time, and optionally further filtered by MatchResultContains. The
// response is described by RespondWith, which may carry either a plain
// text answer or a follow-up tool call for multi-step agent flows.
//
// ToolConfig entries live in the `tools:` YAML section of a config file
// (see MessageStore). They are completely independent of Message
// entries: a request whose last message role is "tool" dispatches into
// the tools branch and never touches the messages branch.
type ToolConfig struct {
	Name                string       `json:"name" yaml:"name"`
	ToolName            string       `json:"tool_name" yaml:"tool_name"`
	MatchResultContains []string     `json:"match_result_contains,omitempty" yaml:"match_result_contains,omitempty"`
	RespondWith         ToolResponse `json:"respond_with" yaml:"respond_with"`
}

// ToolResponse describes what the fake-LLM emits when a ToolConfig
// matches. Exactly one of Text or ToolCall is meaningful per entry:
//
//   - Text is the final assistant text returned to the caller (the
//     agent treats it as the model's natural-language answer).
//   - ToolCall is a follow-up function call, emitted as an OpenAI
//     chat-completion tool_calls entry so the agent executes another
//     step in a multi-tool chain.
type ToolResponse struct {
	Text     string    `json:"text,omitempty" yaml:"text,omitempty"`
	ToolCall *ToolCall `json:"tool_call,omitempty" yaml:"tool_call,omitempty"`
}

// ToolCall is one OpenAI-format function call. Arguments is a
// free-form map serialised to a JSON string on the wire (per the
// OpenAI Chat Completions schema where function.arguments is a string,
// not an object).
type ToolCall struct {
	Name      string         `json:"name" yaml:"name"`
	Arguments map[string]any `json:"arguments,omitempty" yaml:"arguments,omitempty"`
}

// parseDelays converts a ChunkDelays entry list into time.Duration
// values via time.ParseDuration (specs/046-fake-llm-think-chunking/
// research.md D2). It is shared by Validate (fail-fast at startup) and
// the streaming builder; an unparseable entry surfaces the underlying
// ParseDuration error. A nil/empty input yields nil (style: empty
// slices are returned as nil).
func parseDelays(delays []string) ([]time.Duration, error) {
	if len(delays) == 0 {
		return nil, nil
	}
	parsed := make([]time.Duration, 0, len(delays))
	for _, d := range delays {
		dur, err := time.ParseDuration(d)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, dur)
	}
	return parsed, nil
}

// effectiveReasoning returns the ordered reasoning pieces a template
// declares (specs/046-fake-llm-think-chunking/data-model.md §1): the
// chunked form when ReasoningChunks is set, else the legacy single
// Reasoning string wrapped as a 1-element slice (one delta, unchanged
// behaviour — FR-007), else nil when the template carries no reasoning.
func effectiveReasoning(msg *Message) []string {
	if len(msg.ReasoningChunks) > 0 {
		return msg.ReasoningChunks
	}
	if msg.Reasoning != "" {
		return []string{msg.Reasoning}
	}
	return nil
}

// effectiveStallAfter returns the effective permanent-stall position
// (specs/046-fake-llm-think-chunking/data-model.md §1,
// specs/046-fake-llm-think-chunking/research.md D3): the explicit
// StallAfter index when set, else a pointer to 0 when the legacy Stall
// shorthand is set (stall:true ≡ stall_after:0), else nil (no stall).
// StallAfter wins over Stall when both are set — the bool is redundant
// but harmless, so validation does not reject the combination.
func effectiveStallAfter(msg *Message) *int {
	if msg.StallAfter != nil {
		return msg.StallAfter
	}
	if msg.Stall {
		zero := 0
		return &zero
	}
	return nil
}
