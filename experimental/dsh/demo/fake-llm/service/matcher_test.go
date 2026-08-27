package service

import (
	"testing"
)

// Test_match covers the pure keyword-match path and the fallback
// hand-off of match (specs/047-dsh-chat-demo/contracts/fake-llm-
// templates.md §3, priorities 2-3). Each case supplies a fixed template
// catalogue and request messages, and asserts the returned template
// plus the matched flag. The catalogue is intentionally NOT pre-sorted
// by Name so match's own alphabetical tie-break is exercised; it also
// carries one multi-turn template (z-multi) whose conditions never hold
// in these single-turn requests, so every case lands on priority 2 or 3.
func Test_match(t *testing.T) {
	// given: templates shared across cases.
	templates := []*Message{
		{Name: "zeta", Keywords: []string{"unique-zeta"}, Text: "zeta-text"},
		{Name: "z-multi", Keywords: []string{"shared-kw"}, HistoryKeywords: []string{"seen"}, Text: "multi-text"},
		{Name: "alpha", Keywords: []string{"shared-kw"}, Text: "alpha-text"},
		{Name: "beta", Keywords: []string{"shared-kw"}, Text: "beta-text"},
		{Name: "farewell", Keywords: nil, Text: "farewell-text"},
	}

	tests := []struct {
		name      string
		messages  []*chatMessage
		wantName  string
		wantText  string
		wantMatch bool
	}{
		{
			name:      "single keyword match returns configured text",
			messages:  []*chatMessage{{Role: "user", Content: "please use the unique-zeta path"}},
			wantName:  "zeta",
			wantText:  "zeta-text",
			wantMatch: true,
		},
		{
			// alpha and beta (and the reserved z-multi) share "shared-kw";
			// the lowest-Name non-multi-turn match wins.
			name:      "multi-match returns alphabetically-first name",
			messages:  []*chatMessage{{Role: "user", Content: "trigger the shared-kw behaviour"}},
			wantName:  "alpha",
			wantText:  "alpha-text",
			wantMatch: true,
		},
		{
			name:      "case-insensitive keyword substring still matches",
			messages:  []*chatMessage{{Role: "user", Content: "SHARED-KW uppercase still works"}},
			wantName:  "alpha",
			wantText:  "alpha-text",
			wantMatch: true,
		},
		{
			name:      "keyword matches as substring inside larger word",
			messages:  []*chatMessage{{Role: "user", Content: "shared-kw-suffixed is fine"}},
			wantName:  "alpha",
			wantText:  "alpha-text",
			wantMatch: true,
		},
		{
			name:      "last user message wins over earlier ones",
			messages:  []*chatMessage{{Role: "user", Content: "shared-kw first"}, {Role: "assistant", Content: "ack"}, {Role: "user", Content: "unique-zeta now"}},
			wantName:  "zeta",
			wantText:  "zeta-text",
			wantMatch: true,
		},
		{
			name:      "no keyword hit returns the unique pure fallback",
			messages:  []*chatMessage{{Role: "user", Content: "xyzzy-no-such-keyword"}},
			wantName:  "farewell",
			wantText:  "farewell-text",
			wantMatch: false,
		},
		{
			name:      "no user message falls back deterministically",
			messages:  []*chatMessage{{Role: "system", Content: "sys"}, {Role: "assistant", Content: "ack"}},
			wantName:  "farewell",
			wantText:  "farewell-text",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, matched := match(templates, tt.messages)

			// then
			if matched != tt.wantMatch {
				t.Fatalf("match matched=%v, want %v", matched, tt.wantMatch)
			}
			if got.Name != tt.wantName {
				t.Fatalf("match name = %q, want %q", got.Name, tt.wantName)
			}
			if got.Text != tt.wantText {
				t.Fatalf("match text = %q, want %q", got.Text, tt.wantText)
			}
		})
	}
}

// Test_match_MultiTurn covers matching priority 1
// (specs/047-dsh-chat-demo/contracts/fake-llm-templates.md §3): the
// multi-turn condition semantics — keyword condition on the LAST user
// message, ALL history keywords each hitting some message of the
// history set (everything except the last user message), the turn
// lower bound — plus the specificity ordering (declared-condition
// count, then Name) and the fall-through to the lower priorities when a
// condition fails.
func Test_match_MultiTurn(t *testing.T) {
	// given: the shipped US1/US2 catalogue shape and a two-turn hello
	// conversation — the exact request sequence the agent produces for
	// the acceptance scenarios (§4). Cases needing a different catalogue
	// or conversation carry their own.
	catalogue := []*Message{
		{Name: "greeting", Keywords: []string{"hello"}, Text: "greeting-text"},
		{Name: "greeting-again", Keywords: []string{"hello"}, HistoryKeywords: []string{"hello"}, MinTurn: 2, Text: "again-text"},
		{Name: "farewell", Text: "farewell-text"},
	}
	helloConversation := []*chatMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "Hello! How can I help you today?"},
		{Role: "user", Content: "hello"},
	}

	tests := []struct {
		name      string
		templates []*Message
		messages  []*chatMessage
		wantName  string
		wantMatch bool
	}{
		{
			// First turn: history is empty so the history keyword
			// misses and turn=1 < min_turn — the pure keyword branch
			// answers (US2-2 first-turn semantics at matcher level).
			name:      "first turn stays on the pure keyword template",
			templates: catalogue,
			messages:  []*chatMessage{{Role: "user", Content: "hello"}},
			wantName:  "greeting",
			wantMatch: true,
		},
		{
			// Second turn of ONE conversation: "hello" hits the last
			// message, history carries the earlier "hello" (user and
			// assistant messages both belong to the history set), and
			// turn=2 reaches min_turn (US2-1).
			name:      "second hello turn takes the multi-turn branch",
			templates: catalogue,
			messages:  helloConversation,
			wantName:  "greeting-again",
			wantMatch: true,
		},
		{
			// Isolation at matcher level: the history belongs to a
			// different conversation and never contained "hello", so
			// the history condition fails and the keyword branch wins.
			name:      "history lacking the history keyword keeps the keyword branch",
			templates: catalogue,
			messages: []*chatMessage{
				{Role: "user", Content: "what is the weather"},
				{Role: "assistant", Content: "I'm sorry, I didn't catch that."},
				{Role: "user", Content: "hello"},
			},
			wantName:  "greeting",
			wantMatch: true,
		},
		{
			// The history keyword may hit ANY history message — here
			// the assistant reply alone carries it (case-insensitive).
			name:      "history keyword satisfied by the assistant reply alone",
			templates: catalogue,
			messages: []*chatMessage{
				{Role: "user", Content: "greetings and salutations"},
				{Role: "assistant", Content: "Hello! How can I help you today?"},
				{Role: "user", Content: "hello"},
			},
			wantName:  "greeting-again",
			wantMatch: true,
		},
		{
			// Conditions not satisfied → fall through: turn=2 still
			// below min_turn 3, so the keyword branch answers.
			name: "turn below min_turn falls through to the keyword template",
			templates: []*Message{
				{Name: "greeting", Keywords: []string{"hello"}, Text: "greeting-text"},
				{Name: "late-branch", Keywords: []string{"hello"}, HistoryKeywords: []string{"hello"}, MinTurn: 3, Text: "late-text"},
			},
			messages:  helloConversation,
			wantName:  "greeting",
			wantMatch: true,
		},
		{
			// No keyword template exists and the multi-turn history
			// condition fails → the pure fallback answers.
			name: "unsatisfied multi-turn conditions fall to the pure fallback",
			templates: []*Message{
				{Name: "farewell", Text: "farewell-text"},
				{Name: "greeting-again", Keywords: []string{"hi-there"}, HistoryKeywords: []string{"never-seen"}, MinTurn: 2, Text: "again-text"},
			},
			messages: []*chatMessage{
				{Role: "user", Content: "hi-there"},
				{Role: "assistant", Content: "ack"},
				{Role: "user", Content: "hi-there"},
			},
			wantName:  "farewell",
			wantMatch: false,
		},
		{
			// Specificity: 3 declared conditions beat 2 despite the
			// lexicographically later Name.
			name: "more declared conditions win the multi-turn conflict",
			templates: []*Message{
				{Name: "aaa-broad", Keywords: []string{"hello"}, HistoryKeywords: []string{"hello"}, Text: "broad-text"},
				{Name: "zzz-narrow", Keywords: []string{"hello"}, HistoryKeywords: []string{"hello"}, MinTurn: 2, Text: "narrow-text"},
			},
			messages:  helloConversation,
			wantName:  "zzz-narrow",
			wantMatch: true,
		},
		{
			name: "equal condition count breaks ties by lowest name",
			templates: []*Message{
				{Name: "bbb-peer", Keywords: []string{"hello"}, HistoryKeywords: []string{"hello"}, MinTurn: 2, Text: "bbb-text"},
				{Name: "aaa-peer", Keywords: []string{"hello"}, HistoryKeywords: []string{"hello"}, MinTurn: 2, Text: "aaa-text"},
			},
			messages:  helloConversation,
			wantName:  "aaa-peer",
			wantMatch: true,
		},
		{
			// A multi-turn template declaring NO keywords leaves the
			// keyword condition vacuous (§2) and matches on history +
			// turn alone.
			name: "keyword-less multi-turn template matches on history and turn",
			templates: []*Message{
				{Name: "history-only", HistoryKeywords: []string{"seen-it"}, MinTurn: 2, Text: "history-text"},
				{Name: "farewell", Text: "farewell-text"},
			},
			messages: []*chatMessage{
				{Role: "user", Content: "seen-it once"},
				{Role: "assistant", Content: "ack"},
				{Role: "user", Content: "anything else"},
			},
			wantName:  "history-only",
			wantMatch: true,
		},
		{
			// EVERY history keyword must hit: "beta" never appeared, so
			// the multi-turn branch misses and the keyword branch wins.
			name: "every history keyword must hit some history message",
			templates: []*Message{
				{Name: "greeting", Keywords: []string{"hello"}, Text: "greeting-text"},
				{Name: "both-keywords", Keywords: []string{"hello"}, HistoryKeywords: []string{"alpha", "beta"}, MinTurn: 2, Text: "both-text"},
			},
			messages: []*chatMessage{
				{Role: "user", Content: "alpha only"},
				{Role: "assistant", Content: "ack"},
				{Role: "user", Content: "hello"},
			},
			wantName:  "greeting",
			wantMatch: true,
		},
		{
			name: "history keywords may hit across different history messages",
			templates: []*Message{
				{Name: "both-keywords", Keywords: []string{"hello"}, HistoryKeywords: []string{"alpha", "beta"}, MinTurn: 2, Text: "both-text"},
			},
			messages: []*chatMessage{
				{Role: "user", Content: "alpha here"},
				{Role: "assistant", Content: "beta there"},
				{Role: "user", Content: "hello"},
			},
			wantName:  "both-keywords",
			wantMatch: true,
		},
		{
			// The history set excludes the LAST user message itself:
			// "hello" appearing only there does not satisfy the
			// history condition.
			name:      "history keyword on the last user message alone does not count",
			templates: catalogue,
			messages: []*chatMessage{
				{Role: "user", Content: "good morning"},
				{Role: "assistant", Content: "ack"},
				{Role: "user", Content: "hello"},
			},
			wantName:  "greeting",
			wantMatch: true,
		},
		{
			// The multi-turn keyword condition skips the branch even
			// with history and turn satisfied: "hello" hits the history
			// and turn=2 reaches min_turn, but the LAST message
			// ("bye now") misses the template's keywords — priority 2
			// takes over with its own keyword hit.
			name: "last message missing the multi-turn keywords falls to the keyword branch",
			templates: []*Message{
				{Name: "greeting-again", Keywords: []string{"hello"}, HistoryKeywords: []string{"hello"}, MinTurn: 2, Text: "again-text"},
				{Name: "bye-template", Keywords: []string{"bye"}, Text: "bye-text"},
			},
			messages: []*chatMessage{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "Hello! How can I help you today?"},
				{Role: "user", Content: "bye now"},
			},
			wantName:  "bye-template",
			wantMatch: true,
		},
		{
			// Priority 1 outranks priority 2 even against a
			// lexicographically earlier pure keyword template.
			name: "multi-turn outranks an equally-hitting pure keyword template",
			templates: []*Message{
				{Name: "aaa-keyword", Keywords: []string{"hello"}, Text: "keyword-text"},
				{Name: "zzz-multi", Keywords: []string{"hello"}, MinTurn: 2, Text: "multi-text"},
			},
			messages:  helloConversation,
			wantName:  "zzz-multi",
			wantMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, matched := match(tt.templates, tt.messages)

			// then
			if matched != tt.wantMatch {
				t.Fatalf("match matched=%v, want %v", matched, tt.wantMatch)
			}
			if got.Name != tt.wantName {
				t.Fatalf("match name = %q, want %q", got.Name, tt.wantName)
			}
		})
	}
}

// Test_match_FallbackDeterministicSeed exercises the degenerate fallback
// (no pure fallback template exists, §3.3): the pick comes from the
// stable request-seed over all non-multi-turn templates, so the SAME
// request must always receive the SAME reply (US1-2's determinism
// covers the fallback path) and the pick must stay within the pool.
func Test_match_FallbackDeterministicSeed(t *testing.T) {
	// given: a catalogue with two keyword templates and no pure
	// fallback, so every no-match request takes the seed pick.
	templates := []*Message{
		{Name: "alpha", Keywords: []string{"only-alpha"}, Text: "alpha-text"},
		{Name: "beta", Keywords: []string{"only-beta"}, Text: "beta-text"},
	}
	messages := []*chatMessage{{Role: "user", Content: "nothing matches here"}}

	// when: the same request is matched twice.
	first, firstMatched := match(templates, messages)
	second, secondMatched := match(templates, messages)

	// then: both take the fallback path and agree on the pick.
	if firstMatched || secondMatched {
		t.Fatalf("match matched=true on no-match input, want false")
	}
	if first.Name != second.Name {
		t.Fatalf("fallback pick drifted: %q then %q, want the same template for the same request", first.Name, second.Name)
	}
	valid := map[string]bool{"alpha": true, "beta": true}
	if !valid[first.Name] {
		t.Fatalf("fallback picked %q, want one of the pool members", first.Name)
	}
}

// Test_lastUserText covers the last-user extraction directly,
// independent of the matching logic: case-insensitive role match, LAST
// user precedence, and the empty-string result when no user message is
// present.
func Test_lastUserText(t *testing.T) {
	tests := []struct {
		name     string
		messages []*chatMessage
		want     string
	}{
		{
			name:     "single user message",
			messages: []*chatMessage{{Role: "user", Content: "hello"}},
			want:     "hello",
		},
		{
			name: "last user wins when multiple present",
			messages: []*chatMessage{
				{Role: "user", Content: "first"},
				{Role: "assistant", Content: "ack"},
				{Role: "user", Content: "second"},
			},
			want: "second",
		},
		{
			name:     "role case-insensitive (User)",
			messages: []*chatMessage{{Role: "User", Content: "cased"}},
			want:     "cased",
		},
		{
			name:     "no user message returns empty",
			messages: []*chatMessage{{Role: "system", Content: "sys"}},
			want:     "",
		},
		{
			name:     "no messages at all returns empty",
			messages: nil,
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lastUserText(tt.messages)
			if got != tt.want {
				t.Fatalf("lastUserText = %q, want %q", got, tt.want)
			}
		})
	}
}

// Test_requestSeed pins the property the fallback determinism relies
// on: equal request messages hash to equal seeds, and differing content
// changes the seed.
func Test_requestSeed(t *testing.T) {
	a := []*chatMessage{{Role: "user", Content: "hello"}}
	b := []*chatMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "Hello! How can I help you today?"},
		{Role: "user", Content: "hello"},
	}

	if requestSeed(a) != requestSeed(a) {
		t.Fatal("requestSeed not deterministic for identical input")
	}
	if requestSeed(a) == requestSeed(b) {
		t.Fatal("requestSeed collided for different message lists")
	}
	if requestSeed(nil) == requestSeed(a) {
		t.Fatal("requestSeed collided for empty versus non-empty input")
	}
}

// Test_snippet pins the snippet behaviours: short strings pass through
// unchanged; overlong strings are truncated to maxRunes runes with a
// trailing ellipsis.
func Test_snippet(t *testing.T) {
	tests := []struct {
		name  string
		input string
		max   int
		want  string
	}{
		{name: "empty stays empty", input: "", max: 50, want: ""},
		{name: "short passthrough", input: "hello", max: 50, want: "hello"},
		{name: "exact length passthrough", input: "abcde", max: 5, want: "abcde"},
		{name: "truncation appends ellipsis", input: "abcdef", max: 3, want: "abc…"},
		{name: "multibyte rune boundary respected", input: "世界你好", max: 2, want: "世界…"},
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

// Test_match_EmptyTemplates verifies the guard for an unvalidated empty
// catalogue: match returns nil rather than panicking (validated stores
// always yield a pick; this is a defensive-return test).
func Test_match_EmptyTemplates(t *testing.T) {
	got, matched := match(nil, []*chatMessage{{Role: "user", Content: "hello"}})
	if matched {
		t.Fatal("match matched=true on empty templates, want false")
	}
	if got != nil {
		t.Fatalf("match got = %v, want nil", got)
	}
}
