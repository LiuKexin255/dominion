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
func Match(messages []Message, userText string, rng *rand.Rand) (Message, bool) {
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
			best = &messages[i]
		}
	}
	if best != nil {
		return *best, true
	}

	pick := messages[rng.IntN(len(messages))]
	slog.Warn("no keyword matched for user text, returning random message",
		slog.String("user_snippet", snippet(userText, maxSnippetRunes)),
		slog.String("random_name", pick.Name),
	)
	return pick, false
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
