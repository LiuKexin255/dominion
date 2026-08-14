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
