package service

import (
	"slices"
	"sync"
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
		{
			name:    "transient fault injection",
			msg:     &Message{Name: "m", Keywords: []string{"kw"}, Transient: &Transient{Times: 1, HTTPStatus: 503}},
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

// TestMessage_takeInjection pins the per-template transient budget
// (specs/063-llm-reliability-opencode-go/contracts/fake-llm-fault-injection.md
// §1): Times bounds how many selecting requests receive the injection, a
// missing/zero (or negative) Times is unbounded, and the legacy top-level
// Failure is the unbounded shorthand whose behaviour is unchanged. A declared
// transient block owns the injection when both are present.
func TestMessage_takeInjection(t *testing.T) {
	legacy := &ResponseFailure{Code: "glm_test_failure", Message: "injected"}
	tests := []struct {
		name      string
		transient *Transient
		failure   *ResponseFailure
		calls     int
		wantHits  int
	}{
		{name: "no injection declared", calls: 2, wantHits: 0},
		{name: "legacy failure is unbounded", failure: legacy, calls: 3, wantHits: 3},
		{
			name:      "transient times one injects only the first selecting request",
			transient: &Transient{Times: 1, HTTPStatus: 503},
			calls:     2,
			wantHits:  1,
		},
		{
			name:      "transient times two injects the first two requests",
			transient: &Transient{Times: 2, HTTPStatus: 500},
			calls:     3,
			wantHits:  2,
		},
		{
			name:      "transient without times is unbounded",
			transient: &Transient{HTTPStatus: 401},
			calls:     3,
			wantHits:  3,
		},
		{
			name:      "transient times zero is unbounded",
			transient: &Transient{Times: 0, Empty: true},
			calls:     2,
			wantHits:  2,
		},
		{
			name:      "transient negative times is unbounded",
			transient: &Transient{Times: -1, Empty: true},
			calls:     2,
			wantHits:  2,
		},
		{
			name:      "declared transient block owns the injection over a legacy failure",
			transient: &Transient{Times: 1, HTTPStatus: 429},
			failure:   legacy,
			calls:     2,
			wantHits:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given: one template whose every request selects it.
			msg := &Message{Name: "m", Keywords: []string{"kw"}, Failure: tt.failure, Transient: tt.transient}

			// when: takeInjection is called once per selecting request.
			hits := 0
			for range tt.calls {
				if msg.takeInjection() != nil {
					hits++
				}
			}

			// then
			if hits != tt.wantHits {
				t.Fatalf("takeInjection hits = %d, want %d after %d selecting requests", hits, tt.wantHits, tt.calls)
			}
		})
	}
}

// TestMessage_takeInjection_ResolvesFields pins the resolved injection
// payload: the declared HTTP status, Retry-After, error message and failure
// reach the handler unchanged, so the wire body/headers are fully driven by
// the template's transient block.
func TestMessage_takeInjection_ResolvesFields(t *testing.T) {
	// given
	retryAfter := 1
	legacy := &ResponseFailure{Code: "glm_test_failure", Message: "injected"}
	msg := &Message{
		Name:     "m",
		Keywords: []string{"kw"},
		Transient: &Transient{
			HTTPStatus:   503,
			RetryAfter:   &retryAfter,
			ErrorMessage: "insufficient quota",
			Failure:      legacy,
		},
	}

	// when
	got := msg.takeInjection()

	// then
	if got == nil {
		t.Fatalf("takeInjection() = nil, want the declared injection")
	}
	if got.httpStatus != 503 {
		t.Errorf("httpStatus = %d, want 503", got.httpStatus)
	}
	if got.retryAfter == nil || *got.retryAfter != 1 {
		t.Errorf("retryAfter = %v, want the declared 1 second", got.retryAfter)
	}
	if got.errorMessage != "insufficient quota" {
		t.Errorf("errorMessage = %q, want %q", got.errorMessage, "insufficient quota")
	}
	if got.failure != legacy {
		t.Errorf("failure = %v, want the declared failure pointer", got.failure)
	}
}

// TestMessage_takeInjection_Concurrent verifies the mutex-protected counter
// (contract §1): many concurrent selecting requests inject exactly Times
// times, never more or fewer.
func TestMessage_takeInjection_Concurrent(t *testing.T) {
	const (
		times   = 5
		callers = 50
	)
	msg := &Message{Name: "m", Keywords: []string{"kw"}, Transient: &Transient{Times: times, HTTPStatus: 503}}

	// when: 50 goroutines race for the budget.
	var wg sync.WaitGroup
	hits := make(chan struct{}, callers)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if msg.takeInjection() != nil {
				hits <- struct{}{}
			}
		}()
	}
	wg.Wait()
	close(hits)

	// then: exactly the declared budget was consumed.
	if got := len(hits); got != times {
		t.Fatalf("takeInjection concurrent hits = %d, want exactly %d", got, times)
	}
}
