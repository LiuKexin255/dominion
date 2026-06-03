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

func TestAgentProfileID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{
			name:    "valid agent profile name returns ID",
			input:   "agentProfiles/abc",
			want:    "abc",
			wantErr: nil,
		},
		{
			name:    "missing prefix returns ErrInvalidAgentProfileName",
			input:   "abc",
			want:    "",
			wantErr: gameconst.ErrInvalidAgentProfileName,
		},
		{
			name:    "prefix only (empty ID) returns ErrInvalidAgentProfileName",
			input:   "agentProfiles/",
			want:    "",
			wantErr: gameconst.ErrInvalidAgentProfileName,
		},
		{
			name:    "extra slashes in ID returns ErrInvalidAgentProfileName",
			input:   "agentProfiles/abc/extra",
			want:    "",
			wantErr: gameconst.ErrInvalidAgentProfileName,
		},
		{
			name:    "empty string returns ErrInvalidAgentProfileName",
			input:   "",
			want:    "",
			wantErr: gameconst.ErrInvalidAgentProfileName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			// input is set via tt.input

			// when
			got, err := gameconst.AgentProfileID(tt.input)

			// then
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("AgentProfileID(%q) error = %v, want %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("AgentProfileID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestAgentProfileName(t *testing.T) {
	tests := []struct {
		name      string
		profileID string
		want      string
	}{
		{
			name:      "valid profile ID returns prefixed name",
			profileID: "abc",
			want:      "agentProfiles/abc",
		},
		{
			name:      "empty profile ID returns prefix only",
			profileID: "",
			want:      "agentProfiles/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			// profileID is set via tt.profileID

			// when
			got := gameconst.AgentProfileName(tt.profileID)

			// then
			if got != tt.want {
				t.Fatalf("AgentProfileName(%q) = %q, want %q", tt.profileID, got, tt.want)
			}
		})
	}
}

func TestSkillID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{
			name:    "valid skill name returns ID",
			input:   "skills/abc",
			want:    "abc",
			wantErr: nil,
		},
		{
			name:    "missing prefix returns ErrInvalidSkillName",
			input:   "abc",
			want:    "",
			wantErr: gameconst.ErrInvalidSkillName,
		},
		{
			name:    "prefix only (empty ID) returns ErrInvalidSkillName",
			input:   "skills/",
			want:    "",
			wantErr: gameconst.ErrInvalidSkillName,
		},
		{
			name:    "extra slashes in ID returns ErrInvalidSkillName",
			input:   "skills/abc/extra",
			want:    "",
			wantErr: gameconst.ErrInvalidSkillName,
		},
		{
			name:    "empty string returns ErrInvalidSkillName",
			input:   "",
			want:    "",
			wantErr: gameconst.ErrInvalidSkillName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			// input is set via tt.input

			// when
			got, err := gameconst.SkillID(tt.input)

			// then
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("SkillID(%q) error = %v, want %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("SkillID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSkillName(t *testing.T) {
	tests := []struct {
		name   string
		skillID string
		want   string
	}{
		{
			name:    "valid skill ID returns prefixed name",
			skillID: "abc",
			want:    "skills/abc",
		},
		{
			name:    "empty skill ID returns prefix only",
			skillID: "",
			want:    "skills/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			// skillID is set via tt.skillID

			// when
			got := gameconst.SkillName(tt.skillID)

			// then
			if got != tt.want {
				t.Fatalf("SkillName(%q) = %q, want %q", tt.skillID, got, tt.want)
			}
		})
	}
}
