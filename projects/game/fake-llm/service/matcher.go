package service

import (
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
)

// maxSnippetRunes caps the user-text snippet included in the no-match
// WARN log so a giant prompt cannot blow up the log line.
const maxSnippetRunes = 50

// Match picks the response Message for a single request in a fully
// stateless way: it scans the supplied messages for the first one (in
// alphabetical order by Name) whose Keywords contain a case-insensitive
// substring of userText, and falls back to a uniform-random pick when
// nothing matches.
//
// The bool return distinguishes the two outcomes: true means a keyword
// matched deterministically; false means the no-match fallback fired and
// the caller received a random message.
//
// rng must be non-nil and is consumed only on the fallback path so
// deterministic keyword matches never advance the RNG state.
func Match(messages []*Message, userText string, rng *rand.Rand) (*Message, bool) {
	lowered := strings.ToLower(userText)

	// Iterate messages alphabetically by Name: the caller (MessageStore)
	// already sorts by Name, but Match does not assume that — it tracks
	// the lowest-Name match explicitly so passing an unsorted slice
	// still yields the contractually correct pick.
	var best *Message
	for i := range messages {
		if !anyKeywordMatches(messages[i].Keywords, lowered) {
			continue
		}
		if best == nil || messages[i].Name < best.Name {
			best = messages[i]
		}
	}
	if best != nil {
		return best, true
	}

	// Random fallback: only text-only Messages are eligible. A Message
	// carrying a ToolCall represents an explicit test trigger that
	// requires a keyword match; emitting one at random would
	// nonsensically invoke a desktop operation. A stall-marked Message is
	// likewise excluded — a random stall would hang an unrelated turn
	// (specs/043-llm-stream-stall-recovery large tests depend on stall
	// being a deliberate, keyword-gated trigger). Spec 012's random-
	// fallback contract (FR-008) predates tool_call Messages, so
	// restricting the fallback pool to text-only Messages is the
	// coherent extension. When every Message carries a ToolCall or
	// Stall the full set is used rather than panicking on IntN(0).
	var pool []*Message
	for i := range messages {
		if messages[i].ToolCall == nil && !messages[i].Stall {
			pool = append(pool, messages[i])
		}
	}
	if len(pool) == 0 {
		pool = messages
	}
	pick := pool[rng.IntN(len(pool))]
	slog.Warn("no keyword matched for user text, returning random message",
		slog.String("user_snippet", snippet(userText, maxSnippetRunes)),
		slog.String("random_name", pick.Name),
	)
	return pick, false
}

// MatchToolResult picks the ToolConfig for a tool-result request. The
// primary key is toolName (case-insensitive exact match against
// ToolConfig.ToolName). When MatchResultContains is non-empty on a
// candidate, the result text must contain ALL listed substrings
// (case-insensitive) for the candidate to qualify.
//
// When multiple configs share the same ToolName, the one whose Name
// sorts alphabetically first wins, mirroring Match's tie-break rule.
// When no config matches, the bool return is false and a uniform-random
// pick from the supplied slice is returned (empty slice yields nil);
// the RNG is consumed only on the fallback path.
//
// rng must be non-nil when tools is non-empty.
func MatchToolResult(tools []*ToolConfig, toolName string, resultText string, rng *rand.Rand) (*ToolConfig, bool) {
	if len(tools) == 0 {
		return nil, false
	}
	loweredResult := strings.ToLower(resultText)

	var best *ToolConfig
	for i := range tools {
		if !strings.EqualFold(tools[i].ToolName, toolName) {
			continue
		}
		if !allSubstringsPresent(tools[i].MatchResultContains, loweredResult) {
			continue
		}
		if best == nil || tools[i].Name < best.Name {
			best = tools[i]
		}
	}
	if best != nil {
		return best, true
	}

	pick := tools[rng.IntN(len(tools))]
	slog.Warn("no tool config matched, returning random tool response",
		slog.String("tool_name", toolName),
		slog.String("result_snippet", snippet(resultText, maxSnippetRunes)),
		slog.String("random_name", pick.Name),
	)
	return pick, false
}

// allSubstringsPresent reports whether loweredText contains every entry
// in subs (each lower-cased here). An empty subs slice is a no-op match
// (the caller has no additional constraint), mirroring the contract's
// "match_result_contains is optional" semantics.
func allSubstringsPresent(subs []string, loweredText string) bool {
	for _, s := range subs {
		if !strings.Contains(loweredText, strings.ToLower(s)) {
			return false
		}
	}
	return true
}

// anyKeywordMatches reports whether any entry in keywords is a
// case-insensitive substring of loweredUserText. loweredUserText must
// already be lower-cased by the caller; each keyword is lower-cased
// here so callers can reuse one lowered prompt across many messages.
func anyKeywordMatches(keywords []string, loweredUserText string) bool {
	for _, kw := range keywords {
		if strings.Contains(loweredUserText, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// snippet returns the first maxRunes runes of s, with a trailing "…"
// ellipsis when the original string was longer. It keeps log lines
// bounded even for pathologically long user prompts.
func snippet(s string, maxRunes int) string {
	if len([]rune(s)) <= maxRunes {
		return s
	}
	r := []rune(s)
	return fmt.Sprintf("%s…", string(r[:maxRunes]))
}
