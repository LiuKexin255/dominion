package service

import (
	"slices"
	"testing"
	"time"
)

// Test_parseDelays pins the ChunkDelays parsing helper
// (specs/046-fake-llm-think-chunking/research.md D2): valid Go
// duration strings parse to their time.Duration values, an unparseable
// entry fails the whole list, and a nil/empty list yields nil.
func Test_parseDelays(t *testing.T) {
	tests := []struct {
		name    string
		delays  []string
		want    []time.Duration
		wantErr bool
	}{
		{
			name:   "valid durations parse to their values",
			delays: []string{"500ms", "2s", "1.5s"},
			want:   []time.Duration{500 * time.Millisecond, 2 * time.Second, 1500 * time.Millisecond},
		},
		{
			name:    "unparseable string rejected",
			delays:  []string{"not-a-duration"},
			wantErr: true,
		},
		{
			name:    "one bad entry rejects the whole list",
			delays:  []string{"1s", "oops"},
			wantErr: true,
		},
		{
			name:   "nil list yields nil",
			delays: nil,
			want:   nil,
		},
		{
			name:   "empty list yields nil",
			delays: []string{},
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, err := parseDelays(tt.delays)

			// then
			if tt.wantErr && err == nil {
				t.Fatalf("parseDelays(%v) expected error, got nil", tt.delays)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("parseDelays(%v) unexpected error: %v", tt.delays, err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("parseDelays(%v) = %v, want %v", tt.delays, got, tt.want)
			}
		})
	}
}

// TestMessage_effectiveMinTurn pins the MinTurn default/clamp semantics
// (specs/047-dsh-chat-demo/contracts/fake-llm-templates.md §2, aligned by
// specs/049-agent-v2-dsh-init/contracts/fake-responses-wire.md §3):
// undeclared means 1 and negative values clamp up to 1.
func TestMessage_effectiveMinTurn(t *testing.T) {
	tests := []struct {
		name    string
		minTurn int
		want    int
	}{
		{name: "undeclared defaults to 1", minTurn: 0, want: 1},
		{name: "explicit 1 stays 1", minTurn: 1, want: 1},
		{name: "explicit higher value preserved", minTurn: 3, want: 3},
		{name: "negative value clamps to 1", minTurn: -2, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			m := &Message{Name: "m", MinTurn: tt.minTurn}

			// when + then
			if got := m.effectiveMinTurn(); got != tt.want {
				t.Fatalf("effectiveMinTurn(%d) = %d, want %d", tt.minTurn, got, tt.want)
			}
		})
	}
}

// TestMessage_isResponsesOnly pins the isResponsesOnly set
// (specs/049-agent-v2-dsh-init/contracts/fake-responses-wire.md §3): the
// explicit marker, any multi-turn condition, or a failure injection each
// make a template responses-only; a plain template is not.
func TestMessage_isResponsesOnly(t *testing.T) {
	tests := []struct {
		name    string
		msg     *Message
		wantYes bool
	}{
		{
			name:    "plain keyword template is not responses-only",
			msg:     &Message{Name: "plain", Keywords: []string{"kw"}, Text: "t"},
			wantYes: false,
		},
		{
			name:    "explicit responses_only marker",
			msg:     &Message{Name: "m", Keywords: []string{"kw"}, ResponsesOnly: true},
			wantYes: true,
		},
		{
			name:    "history_keywords declares a multi-turn condition",
			msg:     &Message{Name: "m", Keywords: []string{"kw"}, HistoryKeywords: []string{"earlier"}},
			wantYes: true,
		},
		{
			name:    "min_turn above the default declares a multi-turn condition",
			msg:     &Message{Name: "m", Keywords: []string{"kw"}, MinTurn: 2},
			wantYes: true,
		},
		{
			name:    "min_turn at the default does not",
			msg:     &Message{Name: "m", Keywords: []string{"kw"}, MinTurn: 1},
			wantYes: false,
		},
		{
			name:    "failure injection",
			msg:     &Message{Name: "m", Keywords: []string{"kw"}, Failure: &ResponseFailure{Code: "c", Message: "m"}},
			wantYes: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when + then
			if got := tt.msg.isResponsesOnly(); got != tt.wantYes {
				t.Fatalf("isResponsesOnly(%+v) = %v, want %v", tt.msg, got, tt.wantYes)
			}
		})
	}
}
