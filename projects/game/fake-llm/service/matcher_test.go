package service

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"math/rand/v2"
)

// TestMatch covers the keyword-match path of Match. Each case provides
// a fixed message slice and user text, plus a deterministic RNG (used
// only on the fallback path, which these cases must not take), and
// asserts the returned message plus the matched=true flag.
func TestMatch(t *testing.T) {
	// given: a fixed catalogue shared across cases, intentionally NOT
	// pre-sorted by Name so Match's own alphabetical pick is exercised.
	messages := []*Message{
		{
			Name:      "zeta",
			Keywords:  []string{"unique-zeta"},
			Reasoning: "zeta-reasoning",
			Text:      "zeta-text",
		},
		{
			Name:      "alpha",
			Keywords:  []string{"shared-kw"},
			Reasoning: "alpha-reasoning",
			Text:      "alpha-text",
		},
		{
			Name:      "beta",
			Keywords:  []string{"shared-kw"},
			Reasoning: "beta-reasoning",
			Text:      "beta-text",
		},
	}

	tests := []struct {
		name     string
		userText string
		wantName string
		wantText string
	}{
		{
			name:     "single keyword match returns configured text",
			userText: "please use the unique-zeta path",
			wantName: "zeta",
			wantText: "zeta-text",
		},
		{
			name:     "multi-match returns alphabetically-first name",
			userText: "trigger the shared-kw behaviour",
			wantName: "alpha",
			wantText: "alpha-text",
		},
		{
			name:     "case-insensitive keyword substring still matches",
			userText: "SHARED-KW uppercase still works",
			wantName: "alpha",
			wantText: "alpha-text",
		},
		{
			name:     "keyword matches as substring inside larger word",
			userText: "shared-kw-suffixed is fine",
			wantName: "alpha",
			wantText: "alpha-text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, matched := Match(messages, tt.userText, rand.New(rand.NewPCG(42, 0)))

			// then
			if !matched {
				t.Fatalf("Match matched=false, want true for %q", tt.userText)
			}
			if got.Name != tt.wantName {
				t.Fatalf("Match name = %q, want %q", got.Name, tt.wantName)
			}
			if got.Text != tt.wantText {
				t.Fatalf("Match text = %q, want %q", got.Text, tt.wantText)
			}
		})
	}
}

// TestMatch_NoMatchRandom exercises the fallback: when no keyword
// matches, Match must return matched=false and pick one of the supplied
// messages via the deterministic RNG. The exact pick is pinned by the
// PCG seed so any drift in RNG wiring or iteration order surfaces here.
func TestMatch_NoMatchRandom(t *testing.T) {
	messages := []*Message{
		{Name: "alpha", Keywords: []string{"only-alpha"}, Text: "alpha-text"},
		{Name: "beta", Keywords: []string{"only-beta"}, Text: "beta-text"},
		{Name: "gamma", Keywords: []string{"only-gamma"}, Text: "gamma-text"},
	}

	// given: a seeded RNG whose first IntN(3) selects the expected
	// index. Capture it up front so the assertion documents the
	// exact selection the seed produces.
	rng := rand.New(rand.NewPCG(7, 0))
	wantIdx := rng.IntN(len(messages))
	wantName := messages[wantIdx].Name

	// when: a fresh RNG with the same seed is passed so the fallback
	// path inside Match consumes the same first IntN value.
	got, matched := Match(messages, "nothing here matches", rand.New(rand.NewPCG(7, 0)))

	// then
	if matched {
		t.Fatalf("Match matched=true on no-match input, want false")
	}
	if got.Name != wantName {
		t.Fatalf("Match fallback name = %q, want %q (idx %d)", got.Name, wantName, wantIdx)
	}
	if got.Text != messages[wantIdx].Text {
		t.Fatalf("Match fallback text = %q, want %q", got.Text, messages[wantIdx].Text)
	}
}

// TestMatch_NoMatchLogsWarning verifies the no-match path emits a WARN
// log carrying the user-text snippet and the randomly chosen Name, so
// operators can correlate fallback responses with their prompts. The
// default slog logger is swapped for a text handler writing into a
// bytes.Buffer for the duration of the test, then restored.
func TestMatch_NoMatchLogsWarning(t *testing.T) {
	// given: a long user prompt exercises the 50-rune snippet cap; the
	// catalogue has one entry so the RNG pick is fully deterministic.
	messages := []*Message{
		{Name: "only", Keywords: []string{"never"}, Text: "only-text"},
	}
	longPrompt := strings.Repeat("abcdefghij", 20) // 200 runes, no "never"

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(prev)

	// when
	_, matched := Match(messages, longPrompt, rand.New(rand.NewPCG(1, 0)))

	// then
	if matched {
		t.Fatalf("Match matched=true on no-match input, want false")
	}
	logged := buf.String()
	if !strings.Contains(logged, "level=WARN") {
		t.Fatalf("expected WARN level in slog output, got: %s", logged)
	}
	if !strings.Contains(logged, "random_name=only") {
		t.Fatalf("expected random_name=only in slog output, got: %s", logged)
	}
	// 50 runes of "abcdefghij..." + the trailing ellipsis sign.
	wantSnippet := strings.Repeat("abcdefghij", 5) + "\u2026"
	if !strings.Contains(logged, "user_snippet="+wantSnippet) {
		t.Fatalf("expected user_snippet=%q in slog output, got: %s", wantSnippet, logged)
	}
}

// TestSnippet pins the two snippet behaviours: short strings pass
// through unchanged; overlong strings are truncated to maxRunes runes
// with a trailing ellipsis.
func TestSnippet(t *testing.T) {
	tests := []struct {
		name  string
		input string
		max   int
		want  string
	}{
		{name: "empty stays empty", input: "", max: 50, want: ""},
		{name: "short passthrough", input: "hello", max: 50, want: "hello"},
		{name: "exact length passthrough", input: "abcde", max: 5, want: "abcde"},
		{name: "truncation appends ellipsis", input: "abcdef", max: 3, want: "abc\u2026"},
		{name: "multibyte rune boundary respected", input: "世界你好", max: 2, want: "世界\u2026"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := snippet(tt.input, tt.max)
			if got != tt.want {
				t.Fatalf("snippet(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
			}
		})
	}
}

// TestMatchToolResult covers the tool-result matching path. Each case
// supplies a fixed tools catalogue, a tool_name and result text, plus a
// deterministic RNG (consumed only on the fallback path), and asserts
// the returned ToolConfig plus the matched=true flag.
func TestMatchToolResult(t *testing.T) {
	// given: a catalogue with two mouse entries (one with a substring
	// constraint) and one keyboard entry, intentionally NOT pre-sorted
	// by Name so MatchToolResult's own alphabetical pick is exercised.
	tools := []*ToolConfig{
		{
			Name:     "z-mouse-generic",
			ToolName: "mouse",
			RespondWith: ToolResponse{
				Text: "generic mouse response",
			},
		},
		{
			Name:                "a-mouse-button",
			ToolName:            "mouse",
			MatchResultContains: []string{"button"},
			RespondWith: ToolResponse{
				ToolCall: &ToolCall{
					Name:      "mouse",
					Arguments: map[string]any{"action": "LEFT_CLICK"},
				},
			},
		},
		{
			Name:     "keyboard-generic",
			ToolName: "keyboard",
			RespondWith: ToolResponse{
				Text: "keyboard response",
			},
		},
	}

	tests := []struct {
		name       string
		toolName   string
		resultText string
		wantName   string
		wantMatch  bool
	}{
		{
			name:       "tool_name match with no substring constraint",
			toolName:   "mouse",
			resultText: "cursor moved to 100,200",
			wantName:   "z-mouse-generic",
			wantMatch:  true,
		},
		{
			name:       "tool_name + substring match selects constrained entry",
			toolName:   "mouse",
			resultText: "found a button at 50,60",
			wantName:   "a-mouse-button",
			wantMatch:  true,
		},
		{
			name:       "substring not present falls back to unconstrained entry",
			toolName:   "mouse",
			resultText: "cursor moved to 100,200",
			wantName:   "z-mouse-generic",
			wantMatch:  true,
		},
		{
			name:       "different tool_name matches its own entry",
			toolName:   "keyboard",
			resultText: "keys typed",
			wantName:   "keyboard-generic",
			wantMatch:  true,
		},
		{
			name:       "case-insensitive tool_name match",
			toolName:   "MOUSE",
			resultText: "moved",
			wantName:   "z-mouse-generic",
			wantMatch:  true,
		},
		{
			name:       "case-insensitive substring match",
			toolName:   "mouse",
			resultText: "found a BUTTON",
			wantName:   "a-mouse-button",
			wantMatch:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, matched := MatchToolResult(tools, tt.toolName, tt.resultText, rand.New(rand.NewPCG(42, 0)))

			// then
			if matched != tt.wantMatch {
				t.Fatalf("MatchToolResult matched=%v, want %v", matched, tt.wantMatch)
			}
			if got.Name != tt.wantName {
				t.Fatalf("MatchToolResult name = %q, want %q", got.Name, tt.wantName)
			}
		})
	}
}

// TestMatchToolResult_NoMatchRandom exercises the fallback: when no
// tool_name matches, MatchToolResult returns matched=false and picks a
// random entry via the deterministic RNG. The exact pick is pinned by
// the PCG seed.
func TestMatchToolResult_NoMatchRandom(t *testing.T) {
	tools := []*ToolConfig{
		{Name: "alpha", ToolName: "mouse", RespondWith: ToolResponse{Text: "a"}},
		{Name: "beta", ToolName: "mouse", RespondWith: ToolResponse{Text: "b"}},
		{Name: "gamma", ToolName: "keyboard", RespondWith: ToolResponse{Text: "c"}},
	}

	// given: a seeded RNG whose first IntN(3) selects the expected index.
	rng := rand.New(rand.NewPCG(7, 0))
	wantIdx := rng.IntN(len(tools))
	wantName := tools[wantIdx].Name

	// when: a fresh RNG with the same seed so the fallback path
	// consumes the same first IntN value.
	got, matched := MatchToolResult(tools, "nonexistent-tool", "result", rand.New(rand.NewPCG(7, 0)))

	// then
	if matched {
		t.Fatalf("MatchToolResult matched=true on no-match input, want false")
	}
	if got.Name != wantName {
		t.Fatalf("MatchToolResult fallback name = %q, want %q (idx %d)", got.Name, wantName, wantIdx)
	}
}

// TestMatchToolResult_EmptyTools verifies that an empty tools slice
// returns nil and false without touching the RNG (avoiding a panic on
// rng.IntN(0)).
func TestMatchToolResult_EmptyTools(t *testing.T) {
	got, matched := MatchToolResult(nil, "mouse", "result", rand.New(rand.NewPCG(1, 0)))
	if matched {
		t.Fatalf("MatchToolResult matched=true on empty tools, want false")
	}
	if got != nil {
		t.Fatalf("MatchToolResult got = %v, want nil", got)
	}
}

// TestMatchToolResult_AllSubstringsRequired verifies that when
// match_result_contains lists multiple substrings, ALL must be present
// (not just any one).
func TestMatchToolResult_AllSubstringsRequired(t *testing.T) {
	tools := []*ToolConfig{
		{
			Name:                "a-strict-match",
			ToolName:            "screen",
			MatchResultContains: []string{"width", "height"},
			RespondWith:         ToolResponse{Text: "both present"},
		},
		{
			Name:        "z-fallback",
			ToolName:    "screen",
			RespondWith: ToolResponse{Text: "generic"},
		},
	}

	// given: result text with only one of the two required substrings.
	got, matched := MatchToolResult(tools, "screen", "width=1920 but missing the other key", rand.New(rand.NewPCG(1, 0)))

	// then: a-strict-match must NOT qualify; z-fallback wins.
	if !matched {
		t.Fatalf("MatchToolResult matched=false, want true (fallback should match)")
	}
	if got.Name != "z-fallback" {
		t.Fatalf("MatchToolResult name = %q, want z-fallback (a-strict-match requires both substrings)", got.Name)
	}

	// given: result text with BOTH required substrings.
	got, matched = MatchToolResult(tools, "screen", "width=1920 height=1080", rand.New(rand.NewPCG(1, 0)))

	// then: a-strict-match qualifies and wins (alphabetically before z-fallback).
	if !matched {
		t.Fatalf("MatchToolResult matched=false, want true")
	}
	if got.Name != "a-strict-match" {
		t.Fatalf("MatchToolResult name = %q, want a-strict-match", got.Name)
	}
}

// TestValidateTools pins the tool-config validation rules in isolation.
func TestValidateTools(t *testing.T) {
	tests := []struct {
		name    string
		tools   []*ToolConfig
		wantErr string
	}{
		{
			name:    "nil tools valid (optional section)",
			tools:   nil,
			wantErr: "",
		},
		{
			name:    "empty tools valid",
			tools:   []*ToolConfig{},
			wantErr: "",
		},
		{
			name: "valid text response",
			tools: []*ToolConfig{
				{Name: "a", ToolName: "mouse", RespondWith: ToolResponse{Text: "hi"}},
			},
			wantErr: "",
		},
		{
			name: "valid tool_call response",
			tools: []*ToolConfig{
				{Name: "a", ToolName: "mouse", RespondWith: ToolResponse{ToolCall: &ToolCall{Name: "mouse"}}},
			},
			wantErr: "",
		},
		{
			name: "empty name rejected",
			tools: []*ToolConfig{
				{Name: "", ToolName: "mouse", RespondWith: ToolResponse{Text: "hi"}},
			},
			wantErr: "empty name",
		},
		{
			name: "empty tool_name rejected",
			tools: []*ToolConfig{
				{Name: "a", ToolName: "", RespondWith: ToolResponse{Text: "hi"}},
			},
			wantErr: "empty tool_name",
		},
		{
			name: "empty respond_with rejected",
			tools: []*ToolConfig{
				{Name: "a", ToolName: "mouse"},
			},
			wantErr: "empty respond_with",
		},
		{
			name: "duplicate name rejected",
			tools: []*ToolConfig{
				{Name: "a", ToolName: "mouse", RespondWith: ToolResponse{Text: "x"}},
				{Name: "a", ToolName: "keyboard", RespondWith: ToolResponse{Text: "y"}},
			},
			wantErr: "duplicate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTools(tt.tools)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("ValidateTools unexpected error: %v", err)
			}
			if tt.wantErr != "" && err == nil {
				t.Fatalf("ValidateTools expected error containing %q, got nil", tt.wantErr)
			}
			if tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateTools error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}
