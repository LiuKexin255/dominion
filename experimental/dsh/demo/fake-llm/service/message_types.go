// Package service holds the dsh-demo fake-llm: scripted response
// templates embedded into the binary, the deterministic matcher, and the
// OpenAI chat-completions compatible HTTP handler consumed by the
// official dsh-llm-deepseek adapter (specs/047-dsh-chat-demo/contracts/
// fake-llm-wire.md).
package service

// Message is one scripted model-response template. The field set and
// defaults follow the author-facing contract
// specs/047-dsh-chat-demo/contracts/fake-llm-templates.md §2:
//
//   - Name is the unique identifier within the merged store (log and
//     test assertion anchor).
//   - Keywords: ANY entry appearing as a case-insensitive substring of
//     the LAST user message passes the condition. An empty Keywords
//     marks a pure fallback template (§3.3) — it never participates in
//     keyword matching and only serves the deterministic no-match path.
//   - HistoryKeywords / MinTurn are the optional multi-turn conditions
//     (§3 priority 1): EVERY history keyword must hit some message
//     before the last user message, and the request's user-message
//     count must reach MinTurn (default 1). For a multi-turn template
//     an empty Keywords leaves the keyword condition vacuous (恒通过).
//   - Text is the deterministic reply全文 (SSE content deltas
//     concatenate back to it exactly).
//   - Reasoning is a reserved schema-compatibility field; the
//     chat-completions path never sends it.
type Message struct {
	Name            string   `json:"name" yaml:"name"`
	Keywords        []string `json:"keywords" yaml:"keywords"`
	HistoryKeywords []string `json:"history_keywords,omitempty" yaml:"history_keywords,omitempty"`
	MinTurn         int      `json:"min_turn,omitempty" yaml:"min_turn,omitempty"`
	Text            string   `json:"text" yaml:"text"`
	Reasoning       string   `json:"reasoning,omitempty" yaml:"reasoning,omitempty"`
}

// effectiveMinTurn returns the turn lower bound with the contract
// default applied: an undeclared (zero) MinTurn means 1
// (specs/047-dsh-chat-demo/contracts/fake-llm-templates.md §2). Startup
// validation rejects negative values, so the clamp only ever lifts 0.
func (m *Message) effectiveMinTurn() int {
	if m.MinTurn < 1 {
		return 1
	}
	return m.MinTurn
}

// isMultiTurn reports whether the template declares multi-turn
// conditions (history_keywords non-empty or min_turn > 1 — the
// contract's definition of a 多轮条件模板,
// specs/047-dsh-chat-demo/contracts/fake-llm-templates.md §3). Such
// templates match at priority 1 and are excluded from the fallback
// pool so the deterministic no-match path always stays servable (§3.3).
func (m *Message) isMultiTurn() bool {
	return len(m.HistoryKeywords) > 0 || m.effectiveMinTurn() > 1
}
