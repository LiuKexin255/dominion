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
// a fully stateless way, at the three priorities of
// specs/047-dsh-chat-demo/contracts/fake-llm-templates.md §3:
//
//  1. Multi-turn condition templates (history_keywords non-empty or
//     min_turn > 1) whose every condition holds — the keyword condition
//     (vacuous when the template declares no keywords), ALL history
//     keywords each hitting some message before the last user message,
//     and the user-message count reaching min_turn. Conflicts resolve
//     to the most declared conditions first, then the lowest Name.
//  2. Pure keyword templates (non-multi-turn, non-empty Keywords) whose
//     ANY keyword is a case-insensitive substring of the LAST user
//     message — ties broken by lowest Name for determinism.
//  3. Fallback: the unique pure fallback template (empty Keywords,
//     non-multi-turn) if exactly one exists; otherwise a stable-seed
//     pick over all non-multi-turn templates seeded by the request
//     messages' hash, so the same request always yields the same reply
//     (§3.3, US1-2's determinism covers the fallback path).
//
// The bool return reports a condition path (multi-turn or pure keyword,
// true) versus the fallback path (false).
func match(templates []*Message, messages []*chatMessage) (*Message, bool) {
	lowered := strings.ToLower(lastUserText(messages))

	if best := matchMultiTurn(templates, lowered, loweredHistoryTexts(messages), userTurnCount(messages)); best != nil {
		return best, true
	}

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

// matchMultiTurn resolves matching priority 1 (§3): among templates
// declaring multi-turn conditions whose conditions ALL hold, the pick is
// the most specific — more declared conditions first, then the lowest
// Name as the stable tie-break. The keyword condition is vacuous for a
// multi-turn template declaring no keywords (§2: 恒通过); each history
// keyword must hit SOME message of the history set; turn counts user
// messages only. Returns nil when no multi-turn template fully matches,
// deferring to the next priorities.
func matchMultiTurn(templates []*Message, loweredLast string, loweredHistory []string, turn int) *Message {
	var best *Message
	for _, t := range templates {
		if !t.isMultiTurn() {
			continue
		}
		if len(t.Keywords) > 0 && !anyKeywordMatches(t.Keywords, loweredLast) {
			continue
		}
		if !allHistoryKeywordsHit(t.HistoryKeywords, loweredHistory) {
			continue
		}
		if turn < t.effectiveMinTurn() {
			continue
		}
		if best == nil || moreSpecificMultiTurn(t, best) {
			best = t
		}
	}
	return best
}

// moreSpecificMultiTurn orders two fully-matched multi-turn templates
// for the priority-1 pick: the template declaring more conditions wins
// (更具体优先, §3 priority 1); equal counts fall to the lowest Name.
func moreSpecificMultiTurn(a, b *Message) bool {
	if ca, cb := a.declaredConditions(), b.declaredConditions(); ca != cb {
		return ca > cb
	}
	return a.Name < b.Name
}

// declaredConditions counts the template's non-vacuous declared matching
// conditions — a non-empty keyword set, a non-empty history keyword set,
// and an above-default min_turn each count once. Vacuous declarations
// (an empty keyword list, 恒通过 per §2; a min_turn at or below the
// default 1) never constrain matching, so they do not count toward
// specificity.
func (m *Message) declaredConditions() int {
	n := 0
	if len(m.Keywords) > 0 {
		n++
	}
	if len(m.HistoryKeywords) > 0 {
		n++
	}
	if m.effectiveMinTurn() > 1 {
		n++
	}
	return n
}

// allHistoryKeywordsHit reports whether EVERY history keyword is a
// case-insensitive substring of at least one lowered history text. Each
// keyword may hit a different history message (§3 condition 2: ∀h ∃某条
// 消息).
func allHistoryKeywordsHit(historyKeywords, loweredHistory []string) bool {
	for _, kw := range historyKeywords {
		loweredKw := strings.ToLower(kw)
		hit := false
		for _, h := range loweredHistory {
			if strings.Contains(h, loweredKw) {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	return true
}

// loweredHistoryTexts lower-cases the content of every message EXCEPT
// the last user message — the contract's `history` set (§3: user and
// assistant messages alike). On a conversation's first turn the history
// is empty, so any declared history keyword misses and the multi-turn
// branch cannot fire (US2-2's first-turn semantics).
func loweredHistoryTexts(messages []*chatMessage) []string {
	lastIdx := lastUserIndex(messages)
	var texts []string
	for i, m := range messages {
		if i != lastIdx {
			texts = append(texts, strings.ToLower(m.Content))
		}
	}
	return texts
}

// lastUserIndex returns the index of the LAST message whose role equals
// "user" (compared case-insensitively), or -1 when no user message is
// present.
func lastUserIndex(messages []*chatMessage) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "user") {
			return i
		}
	}
	return -1
}

// userTurnCount counts the request's user-role messages — the
// contract's `turn` (§3), the quantity min_turn lower-bounds.
func userTurnCount(messages []*chatMessage) int {
	turn := 0
	for _, m := range messages {
		if strings.EqualFold(m.Role, "user") {
			turn++
		}
	}
	return turn
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
	slog.Info("no template condition matched, returning fallback template",
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
