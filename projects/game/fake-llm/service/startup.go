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
func Validate(messages []*Message) error {
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

// ValidateTools enforces tool-config startup invariants over a flat
// slice of ToolConfig entries and returns an error describing the first
// violation. The invariants are:
//
//   - tool Name must be non-empty;
//   - ToolName (the trigger key matched against request tool_name) must
//     be non-empty;
//   - RespondWith must carry at least one of Text or ToolCall (an empty
//     response is a config mistake, not a useful state);
//   - tool Names must be unique across all merged files.
//
// A nil slice is valid (tools are optional). It is called by LoadFromFS
// after parsing and merging, so a non-nil error aborts startup.
func ValidateTools(tools []*ToolConfig) error {
	seen := make(map[string]struct{})
	for _, t := range tools {
		if t.Name == "" {
			return fmt.Errorf("validate: tool config with empty name")
		}
		if t.ToolName == "" {
			return fmt.Errorf("validate: tool config %q has empty tool_name", t.Name)
		}
		if t.RespondWith.Text == "" && t.RespondWith.ToolCall == nil {
			return fmt.Errorf("validate: tool config %q has empty respond_with", t.Name)
		}
		if _, ok := seen[t.Name]; ok {
			return fmt.Errorf("validate: duplicate tool config name %q", t.Name)
		}
		seen[t.Name] = struct{}{}
	}
	return nil
}
