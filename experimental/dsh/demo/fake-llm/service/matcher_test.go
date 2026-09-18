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

// Test_match_SystemKeywords covers the system-keywords matching condition
// (specs/058-dsh-preset-roster-demo/contracts/fake-llm-system-keywords.md
// §1, merged into the multi-turn priority of
// specs/047-dsh-chat-demo/contracts/fake-llm-templates.md §3): the S set
// (newline-joined system-role message text), the every-keyword-hit rule,
// case-insensitive substring semantics, the vacuousness of an undeclared
// system_keywords (047 templates keep their exact behaviour even when the
// request carries system messages), and the specificity ordering.
func Test_match_SystemKeywords(t *testing.T) {
	// given: the shipped preset-scenario catalogue shape — system-keyword
	// templates carry a user-keyword probe so they only fire on requests
	// that deliberately probe the preset behaviour, never on the plain
	// 047 chat flows.
	catalogue := []*Message{
		{Name: "preset-persona-standard", Keywords: []string{"preset-probe"}, SystemKeywords: []string{"demo standard assistant"}, Text: "persona-standard-hit"},
		{Name: "preset-persona-tools", Keywords: []string{"preset-probe"}, SystemKeywords: []string{"demo tools assistant"}, Text: "persona-tools-hit"},
		{Name: "tool-guidance-present", Keywords: []string{"guidance-probe"}, SystemKeywords: []string{"demo_echo"}, Text: "tool-guidance-hit"},
		{Name: "greeting", Keywords: []string{"hello"}, Text: "greeting-text"},
		{Name: "farewell", Text: "farewell-text"},
	}
	// A demo-standard session's model request: system prompt carries the
	// standard persona, the user turn probes with preset-probe.
	standardProbe := []*chatMessage{
		{Role: "system", Content: "You are the demo standard assistant."},
		{Role: "user", Content: "preset-probe"},
	}
	// A demo-tools session's model request: system prompt carries the tools
	// persona AND the demo_echo guidance heading.
	toolsProbe := []*chatMessage{
		{Role: "system", Content: "You are the demo tools assistant.\n## demo_echo\n\ndemo_echo echoes text back verbatim."},
		{Role: "user", Content: "preset-probe"},
	}

	tests := []struct {
		name      string
		templates []*Message
		messages  []*chatMessage
		wantName  string
		wantMatch bool
	}{
		{
			// The standard session's probe: S contains the standard persona
			// and the last user message is the probe keyword — priority 1
			// answers even though "preset-probe" is not the greeting keyword.
			name:      "system and keyword conditions met take the persona template",
			templates: catalogue,
			messages:  standardProbe,
			wantName:  "preset-persona-standard",
			wantMatch: true,
		},
		{
			// The tools session's probe: the tools persona template wins the
			// two-way conflict over the guidance template (guidance's user
			// keyword guidance-probe is absent, so it is not even a
			// candidate; the persona hit stands).
			name:      "tools session probe takes the tools persona template",
			templates: catalogue,
			messages:  toolsProbe,
			wantName:  "preset-persona-tools",
			wantMatch: true,
		},
		{
			// Case-insensitive: the template keyword is matched against the
			// lower-cased system text.
			name:      "system keyword hit is case-insensitive",
			templates: catalogue,
			messages: []*chatMessage{
				{Role: "system", Content: "YOU ARE THE DEMO STANDARD ASSISTANT."},
				{Role: "user", Content: "PRESET-PROBE"},
			},
			wantName:  "preset-persona-standard",
			wantMatch: true,
		},
		{
			// Every declared system keyword must hit: the guidance template
			// needs demo_echo in S, which a standard session never carries —
			// the probe falls through to the pure fallback (guidance-absent
			// proof at matcher level).
			name:      "guidance probe on a standard session falls to the fallback",
			templates: catalogue,
			messages: []*chatMessage{
				{Role: "system", Content: "You are the demo standard assistant."},
				{Role: "user", Content: "guidance-probe"},
			},
			wantName:  "farewell",
			wantMatch: false,
		},
		{
			name:      "no system message leaves every declared system keyword missed",
			templates: catalogue,
			messages:  []*chatMessage{{Role: "user", Content: "preset-probe"}},
			wantName:  "farewell",
			wantMatch: false,
		},
		{
			// A system keyword appearing ONLY in the last user message does
			// not satisfy the system condition: S excludes user messages.
			name: "system keyword on the user message does not count",
			templates: []*Message{
				{Name: "sneaky", Keywords: []string{"demo standard assistant"}, SystemKeywords: []string{"demo standard assistant"}, Text: "sneaky-hit"},
				{Name: "farewell", Text: "farewell-text"},
			},
			messages:  []*chatMessage{{Role: "user", Content: "demo standard assistant"}},
			wantName:  "farewell",
			wantMatch: false,
		},
		{
			// BOTH declared system keywords must hit S; only one does.
			name: "every declared system keyword must hit",
			templates: []*Message{
				{Name: "both-system", SystemKeywords: []string{"demo standard assistant", "demo_echo"}, Text: "both-hit"},
				{Name: "farewell", Text: "farewell-text"},
			},
			messages: []*chatMessage{
				{Role: "system", Content: "You are the demo standard assistant."},
				{Role: "user", Content: "anything"},
			},
			wantName:  "farewell",
			wantMatch: false,
		},
		{
			// 047 zero-behavior-change: a plain "hello" turn against a
			// request carrying a system prompt keeps the pure keyword
			// template — the system templates' probe keywords miss, and
			// templates declaring no system_keywords never look at S.
			name:      "undeclared system keywords keep the pure keyword path",
			templates: catalogue,
			messages: []*chatMessage{
				{Role: "system", Content: "You are the demo standard assistant."},
				{Role: "user", Content: "hello"},
			},
			wantName:  "greeting",
			wantMatch: true,
		},
		{
			// Specificity: a template declaring keyword + history + system
			// (3 conditions) beats a keyword + history template (2) when
			// both fully match.
			name: "more declared conditions win with system in the mix",
			templates: []*Message{
				{Name: "aaa-two", Keywords: []string{"hello"}, HistoryKeywords: []string{"seen"}, Text: "two-hit"},
				{Name: "zzz-three", Keywords: []string{"hello"}, HistoryKeywords: []string{"seen"}, SystemKeywords: []string{"sys-marker"}, Text: "three-hit"},
			},
			messages: []*chatMessage{
				{Role: "system", Content: "sys-marker present"},
				{Role: "user", Content: "seen once"},
				{Role: "assistant", Content: "ack"},
				{Role: "user", Content: "hello"},
			},
			wantName:  "zzz-three",
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

// Test_loweredSystemText pins the S-set construction directly: system-role
// messages join with newlines (so a keyword cannot falsely span two
// messages), the role match is case-insensitive, and absence of system
// messages yields the empty string.
func Test_loweredSystemText(t *testing.T) {
	tests := []struct {
		name     string
		messages []*chatMessage
		want     string
	}{
		{
			name:     "no system message yields empty",
			messages: []*chatMessage{{Role: "user", Content: "hello"}},
			want:     "",
		},
		{
			name:     "single system message lower-cased",
			messages: []*chatMessage{{Role: "system", Content: "Demo Standard"}, {Role: "user", Content: "x"}},
			want:     "demo standard",
		},
		{
			name: "multiple system messages join with newline",
			messages: []*chatMessage{
				{Role: "system", Content: "AAA"},
				{Role: "user", Content: "middle"},
				{Role: "System", Content: "BBB"},
			},
			want: "aaa\nbbb",
		},
		{
			name:     "empty input yields empty",
			messages: nil,
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := loweredSystemText(tt.messages)
			if got != tt.want {
				t.Fatalf("loweredSystemText = %q, want %q", got, tt.want)
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
