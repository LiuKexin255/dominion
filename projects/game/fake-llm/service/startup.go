package service

import "fmt"

// Validate enforces message-store startup invariants over a flat slice
// of messages and returns an error describing the first violation.
//
// The invariants are, in order:
//
//   - at least one message must be loaded (zero messages is a startup
//     failure, not an allowed empty state);
//   - every message must carry at least one keyword;
//   - no keyword element may be the empty string;
//   - message Names must be unique across all merged files.
//
// It is called by LoadFromFS once all embedded files have been parsed
// and merged, so a non-nil error aborts startup (see cmd/main.go).
func Validate(messages []Message) error {
	if len(messages) == 0 {
		return fmt.Errorf("validate: no messages loaded")
	}

	seen := make(map[string]struct{})
	for _, m := range messages {
		if len(m.Keywords) == 0 {
			return fmt.Errorf("validate: message %q has no keywords", m.Name)
		}
		for _, kw := range m.Keywords {
			if kw == "" {
				return fmt.Errorf("validate: message %q has an empty keyword", m.Name)
			}
		}
		if _, ok := seen[m.Name]; ok {
			return fmt.Errorf("validate: duplicate message name %q", m.Name)
		}
		seen[m.Name] = struct{}{}
	}
	return nil
}
