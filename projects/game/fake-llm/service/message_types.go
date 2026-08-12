// Package service holds the fake-llm message store: message templates
// embedded into the binary, validated at startup, and served by the
// HTTP handler.
package service

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
// A nil/absent Stall preserves the original streaming behaviour, so existing
// Message entries are unchanged.
type Message struct {
	Name      string    `json:"name" yaml:"name"`
	Keywords  []string  `json:"keywords" yaml:"keywords"`
	Reasoning string    `json:"reasoning" yaml:"reasoning"`
	Text      string    `json:"text" yaml:"text"`
	ToolCall  *ToolCall `json:"tool_call,omitempty" yaml:"tool_call,omitempty"`
	Stall     bool      `json:"stall,omitempty" yaml:"stall,omitempty"`
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
