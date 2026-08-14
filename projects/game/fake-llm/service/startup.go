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
//   - message Names must be unique across all merged files;
//   - chunked-reasoning rules V1/V2/V4/V5 (specs/046-fake-llm-think-
//     chunking/research.md §validation): every reasoning_chunks entry
//     non-empty; chunk_delays length ≤ len(reasoning_chunks)−1 with
//     every entry parseable via time.ParseDuration; reasoning and
//     reasoning_chunks mutually exclusive; tool_call mutually exclusive
//     with reasoning_chunks/chunk_delays. (Rules V3 and the stall_after
//     half of V5 land with the StallAfter field, US2.)
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

		// V1: an empty chunk would emit an empty reasoning_content
		// delta, which a consuming model parse would drop silently —
		// fail fast instead (FR-017).
		for i, chunk := range m.ReasoningChunks {
			if chunk == "" {
				return fmt.Errorf("validate: message %q has an empty reasoning_chunks entry at index %d", m.Name, i)
			}
		}
		// V2: with N chunks there are N−1 gaps; a longer list cannot be
		// applied and a shorter one just defaults the missing gaps to 0.
		// Without chunks no delay is meaningful, so the upper bound is
		// max(0, len(reasoning_chunks)-1). Each entry must parse as a Go
		// duration string (FR-017).
		maxDelays := len(m.ReasoningChunks) - 1
		if maxDelays < 0 {
			maxDelays = 0
		}
		if len(m.ChunkDelays) > maxDelays {
			return fmt.Errorf("validate: message %q has %d chunk_delays entries, at most %d (len(reasoning_chunks)-1)",
				m.Name, len(m.ChunkDelays), maxDelays)
		}
		if _, err := parseDelays(m.ChunkDelays); err != nil {
			return fmt.Errorf("validate: message %q has an unparseable chunk_delays entry: %v", m.Name, err)
		}
		// V4: declaring reasoning both ways is ambiguous about which
		// form wins — reject rather than pick silently
		// (specs/046-fake-llm-think-chunking/research.md D6).
		if m.Reasoning != "" && len(m.ReasoningChunks) > 0 {
			return fmt.Errorf("validate: message %q declares both reasoning and reasoning_chunks (mutually exclusive)", m.Name)
		}
		// V5: a tool-call response streams no reasoning, so chunking on
		// a tool_call template is a config mistake
		// (specs/046-fake-llm-think-chunking/research.md D5).
		if m.ToolCall != nil && (len(m.ReasoningChunks) > 0 || len(m.ChunkDelays) > 0) {
			return fmt.Errorf("validate: message %q carries tool_call together with reasoning_chunks/chunk_delays (mutually exclusive)", m.Name)
		}
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
