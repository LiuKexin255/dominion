package service

import "fmt"

// Validate enforces message-store startup invariants over a flat slice
// of templates and returns an error describing the first violation.
//
// The invariants are, in order:
//
//   - at least one message must be loaded (zero messages is a startup
//     failure, not an allowed empty state);
//   - every message must carry a non-empty Name and a non-empty Text
//     (both required by specs/047-dsh-chat-demo/contracts/
//     fake-llm-templates.md §2);
//   - no keyword / history_keyword element may be the empty string
//     (an empty keyword can never distinguish templates);
//   - min_turn may not be negative (the declared lower bound on user
//     turn count; 0 is accepted and clamped to the default 1);
//   - message Names must be unique across all merged files;
//   - at least one non-multi-turn template must exist so the
//     deterministic no-match fallback always has a servable candidate
//     (§3.3).
//
// It is called by LoadFromFS once all embedded files have been parsed
// and merged, so a non-nil error aborts startup (see cmd/main.go).
func Validate(messages []*Message) error {
	if len(messages) == 0 {
		return fmt.Errorf("validate: no messages loaded")
	}

	seen := make(map[string]struct{})
	servable := false
	for _, m := range messages {
		if m.Name == "" {
			return fmt.Errorf("validate: message with empty name")
		}
		if m.Text == "" {
			return fmt.Errorf("validate: message %q has empty text", m.Name)
		}
		for _, kw := range m.Keywords {
			if kw == "" {
				return fmt.Errorf("validate: message %q has an empty keyword", m.Name)
			}
		}
		for _, kw := range m.HistoryKeywords {
			if kw == "" {
				return fmt.Errorf("validate: message %q has an empty history_keyword", m.Name)
			}
		}
		if m.MinTurn < 0 {
			return fmt.Errorf("validate: message %q has negative min_turn %d", m.Name, m.MinTurn)
		}
		if _, ok := seen[m.Name]; ok {
			return fmt.Errorf("validate: duplicate message name %q", m.Name)
		}
		seen[m.Name] = struct{}{}
		if !m.isMultiTurn() {
			servable = true
		}
	}
	if !servable {
		return fmt.Errorf("validate: no non-multi-turn message loaded; the fallback path would be unservable")
	}
	return nil
}
