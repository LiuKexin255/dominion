package gameconst_test

import (
	"errors"
	"testing"

	"dominion/projects/game/pkg/gameconst"
)

func TestSessionID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{
			name:    "valid session name returns ID",
			input:   "sessions/abc",
			want:    "abc",
			wantErr: nil,
		},
		{
			name:    "missing prefix returns ErrInvalidSessionName",
			input:   "abc",
			want:    "",
			wantErr: gameconst.ErrInvalidSessionName,
		},
		{
			name:    "prefix only (empty ID) returns ErrInvalidSessionName",
			input:   "sessions/",
			want:    "",
			wantErr: gameconst.ErrInvalidSessionName,
		},
		{
			name:    "extra slashes in ID returns ErrInvalidSessionName",
			input:   "sessions/abc/agent",
			want:    "",
			wantErr: gameconst.ErrInvalidSessionName,
		},
		{
			name:    "empty string returns ErrInvalidSessionName",
			input:   "",
			want:    "",
			wantErr: gameconst.ErrInvalidSessionName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			// input is set via tt.input

			// when
			got, err := gameconst.SessionID(tt.input)

			// then
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("SessionID(%q) error = %v, want %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("SessionID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSessionName(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		want      string
	}{
		{
			name:      "valid session ID returns prefixed name",
			sessionID: "abc",
			want:      "sessions/abc",
		},
		{
			name:      "empty session ID returns prefix only",
			sessionID: "",
			want:      "sessions/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			// sessionID is set via tt.sessionID

			// when
			got := gameconst.SessionName(tt.sessionID)

			// then
			if got != tt.want {
				t.Fatalf("SessionName(%q) = %q, want %q", tt.sessionID, got, tt.want)
			}
		})
	}
}
