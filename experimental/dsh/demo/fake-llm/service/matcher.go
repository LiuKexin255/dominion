package service

import (
	"fmt"
	"hash/fnv"
	"log/slog"
	"strings"
)

// chatMessage is one normalized request message: its role plus the
// decoded text content. The handler converts OpenAI wire messages
// (string or array content forms) into this shape before matching.
type chatMessage struct {
	Role    string
	Content string
}

// match picks the response template for one chat-completions request in
// a fully stateless way (specs/047-dsh-chat-demo/contracts/
// fake-llm-templates.md §3, US1 subset):
//
//  1. Pure keyword templates (non-multi-turn, non-empty Keywords) whose
//     ANY keyword is a case-insensitive substring of the LAST user
//     message — ties broken by lowest Name for determinism.
//  2. Fallback: the unique pure fallback template (empty Keywords,
//     non-multi-turn) if exactly one exists; otherwise a stable-seed
//     pick over all non-multi-turn templates seeded by the request
//     messages' hash, so the same request always yields the same reply
//     (§3.3, US1-2's determinism covers the fallback path).
//
// Multi-turn templates are reserved for US2 (T021) and never match here.
// The bool return reports the keyword path (true) versus the fallback
// path (false).
func match(templates []*Message, messages []*chatMessage) (*Message, bool) {
	lowered := strings.ToLower(lastUserText(messages))

	var best *Message
	for _, t := range templates {
		if t.isMultiTurn() || len(t.Keywords) == 0 {
			continue
		}
		if !anyKeywordMatches(t.Keywords, lowered) {
			continue
		}
		if best == nil || t.Name < best.Name {
			best = t
		}
	}
	if best != nil {
		return best, true
	}
	return fallback(templates, messages), false
}

// fallback resolves the deterministic no-match reply
// (specs/047-dsh-chat-demo/contracts/fake-llm-templates.md §3.3): a
// unique pure fallback template is returned directly (the shipped
// testdata's farewell — the US1-3 assertion anchor); with zero or
// several pure fallback templates the pick falls to a stable-seed
// selection over all non-multi-turn templates. Validate guarantees the
// pool is never empty, so a nil return only occurs for an unvalidated
// (empty) template set.
func fallback(templates []*Message, messages []*chatMessage) *Message {
	var pure []*Message
	var pool []*Message
	for _, t := range templates {
		if t.isMultiTurn() {
			continue
		}
		pool = append(pool, t)
		if len(t.Keywords) == 0 {
			pure = append(pure, t)
		}
	}
	if len(pool) == 0 {
		return nil
	}

	var pick *Message
	if len(pure) == 1 {
		pick = pure[0]
	} else {
		pick = pool[requestSeed(messages)%uint64(len(pool))]
	}
	slog.Info("no keyword matched, returning fallback template",
		slog.String("user_snippet", snippet(lastUserText(messages), maxSnippetRunes)),
		slog.String("fallback_name", pick.Name),
	)
	return pick
}

// requestSeed hashes the request messages' full text into a stable
// uint64 seed (FNV-1a over role + content pairs). Equal requests map to
// equal seeds, which is what makes the fallback pick deterministic
// across repeated calls (US1-2).
func requestSeed(messages []*chatMessage) uint64 {
	h := fnv.New64a()
	for _, m := range messages {
		fmt.Fprintf(h, "%s\x00%s\x00", m.Role, m.Content)
	}
	return h.Sum64()
}

// lastUserText returns the content of the LAST message whose role
// equals "user" (compared case-insensitively), or the empty string when
// no user message is present — the keyword match then deterministically
// falls through to the fallback.
func lastUserText(messages []*chatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "user") {
			return messages[i].Content
		}
	}
	return ""
}

// anyKeywordMatches reports whether any entry in keywords is a
// case-insensitive substring of loweredUserText. loweredUserText must
// already be lower-cased by the caller; each keyword is lower-cased
// here so callers can reuse one lowered prompt across many templates.
func anyKeywordMatches(keywords []string, loweredUserText string) bool {
	for _, kw := range keywords {
		if strings.Contains(loweredUserText, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// maxSnippetRunes caps the user-text snippet included in the fallback
// INFO log so a giant prompt cannot blow up the log line.
const maxSnippetRunes = 50

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
