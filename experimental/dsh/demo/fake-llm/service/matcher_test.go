package service

import (
	"testing"
)

// Test_match covers the US1 keyword-match path and the fallback hand-off
// of match (specs/047-dsh-chat-demo/contracts/fake-llm-templates.md §3,
// US1 subset). Each case supplies a fixed template catalogue and request
// messages, and asserts the returned template plus the matched flag.
// The catalogue is intentionally NOT pre-sorted by Name so match's own
// alphabetical tie-break is exercised, and it carries one multi-turn
// template (reserved for US2/T021) that must never match here.
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

// Test_match_MultiTurnReserved pins the US1 scope boundary: a template
// declaring multi-turn conditions (history_keywords / min_turn > 1) is
// reserved for the US2 matcher (specs/047-dsh-chat-demo/tasks.md T021)
// and must not fire even when its keyword hits — the request falls
// through to the next priority instead.
func Test_match_MultiTurnReserved(t *testing.T) {
	// given: the only "hello" keyword template declares min_turn 2; the
	// single-message request cannot satisfy it (US1 scope), so the
	// unique pure fallback must answer.
	templates := []*Message{
		{Name: "greeting-again", Keywords: []string{"hello"}, MinTurn: 2, Text: "again-text"},
		{Name: "farewell", Keywords: []string{}, Text: "farewell-text"},
	}

	// when
	got, matched := match(templates, []*chatMessage{{Role: "user", Content: "hello"}})

	// then
	if matched {
		t.Fatalf("match matched=true on a reserved multi-turn template, want false")
	}
	if got.Name != "farewell" {
		t.Fatalf("match name = %q, want farewell", got.Name)
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
