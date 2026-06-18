// Package service holds the fake-llm message store: message templates
// embedded into the binary, validated at startup, and served by the
// HTTP handler.
package service

// Message is a single templated LLM response. It is keyed by Name for
// uniqueness and matched by Keywords at request time (T2). Reasoning
// and Text are the literal strings returned to the caller.
//
// Name, Reasoning and Text are the single source of truth asserted by
// the T6 integration tests; the testdata files are the only place they
// are defined.
type Message struct {
	Name      string   `json:"name" yaml:"name"`
	Keywords  []string `json:"keywords" yaml:"keywords"`
	Reasoning string   `json:"reasoning" yaml:"reasoning"`
	Text      string   `json:"text" yaml:"text"`
}
