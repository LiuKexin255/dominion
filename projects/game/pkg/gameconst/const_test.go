package gameconst_test

import (
	"errors"
	"testing"

	"dominion/projects/game/pkg/gameconst"
)

func TestSessionName(t *testing.T) {
	tests := []struct {
		name      string
		template  string
		sessionID string
		want      string
	}{
		{
			name:      "valid template and session ID",
			template:  "saolei",
			sessionID: "abc",
			want:      "templates/saolei/sessions/abc",
		},
		{
			name:      "empty session ID",
			template:  "saolei",
			sessionID: "",
			want:      "templates/saolei/sessions/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := gameconst.SessionName(tt.template, tt.sessionID)

			// then
			if got != tt.want {
				t.Fatalf("SessionName(%q, %q) = %q, want %q", tt.template, tt.sessionID, got, tt.want)
			}
		})
	}
}

func TestSessionID(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		want        string
		wantSession string
		wantErr     error
	}{
		{
			name:        "valid session name returns template and ID",
			input:       "templates/saolei/sessions/abc",
			want:        "saolei",
			wantSession: "abc",
			wantErr:     nil,
		},
		{
			name:    "old flat sessions prefix returns ErrInvalidSessionName",
			input:   "sessions/abc",
			wantErr: gameconst.ErrInvalidSessionName,
		},
		{
			name:    "missing templates prefix returns ErrInvalidSessionName",
			input:   "abc",
			wantErr: gameconst.ErrInvalidSessionName,
		},
		{
			name:    "missing sessions segment returns ErrInvalidSessionName",
			input:   "templates/saolei/abc",
			wantErr: gameconst.ErrInvalidSessionName,
		},
		{
			name:    "empty template returns ErrInvalidSessionName",
			input:   "templates//sessions/abc",
			wantErr: gameconst.ErrInvalidSessionName,
		},
		{
			name:    "empty session ID returns ErrInvalidSessionName",
			input:   "templates/saolei/sessions/",
			wantErr: gameconst.ErrInvalidSessionName,
		},
		{
			name:    "extra segment returns ErrInvalidSessionName",
			input:   "templates/saolei/sessions/abc/extra",
			wantErr: gameconst.ErrInvalidSessionName,
		},
		{
			name:    "empty string returns ErrInvalidSessionName",
			input:   "",
			wantErr: gameconst.ErrInvalidSessionName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, gotSession, err := gameconst.SessionID(tt.input)

			// then
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("SessionID(%q) error = %v, want %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("SessionID(%q) template = %q, want %q", tt.input, got, tt.want)
			}
			if gotSession != tt.wantSession {
				t.Fatalf("SessionID(%q) session = %q, want %q", tt.input, gotSession, tt.wantSession)
			}
		})
	}
}

func TestTeamName(t *testing.T) {
	tests := []struct {
		name      string
		template  string
		sessionID string
		want      string
	}{
		{
			name:      "valid template and session ID",
			template:  "saolei",
			sessionID: "abc",
			want:      "templates/saolei/sessions/abc/team",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := gameconst.TeamName(tt.template, tt.sessionID)

			// then
			if got != tt.want {
				t.Fatalf("TeamName(%q, %q) = %q, want %q", tt.template, tt.sessionID, got, tt.want)
			}
		})
	}
}

func TestTeamSessionID(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		want        string
		wantSession string
		wantErr     error
	}{
		{
			name:        "valid team name returns template and session ID",
			input:       "templates/saolei/sessions/abc/team",
			want:        "saolei",
			wantSession: "abc",
			wantErr:     nil,
		},
		{
			name:    "session name without team suffix returns ErrInvalidTeamName",
			input:   "templates/saolei/sessions/abc",
			wantErr: gameconst.ErrInvalidTeamName,
		},
		{
			name:    "old agent suffix returns ErrInvalidTeamName",
			input:   "templates/saolei/sessions/abc/agent",
			wantErr: gameconst.ErrInvalidTeamName,
		},
		{
			name:    "empty string returns ErrInvalidTeamName",
			input:   "",
			wantErr: gameconst.ErrInvalidTeamName,
		},
		{
			name:    "extra segment returns ErrInvalidTeamName",
			input:   "templates/saolei/sessions/abc/team/extra",
			wantErr: gameconst.ErrInvalidTeamName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, gotSession, err := gameconst.TeamSessionID(tt.input)

			// then
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("TeamSessionID(%q) error = %v, want %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("TeamSessionID(%q) template = %q, want %q", tt.input, got, tt.want)
			}
			if gotSession != tt.wantSession {
				t.Fatalf("TeamSessionID(%q) session = %q, want %q", tt.input, gotSession, tt.wantSession)
			}
		})
	}
}

func TestMessageAgentName(t *testing.T) {
	tests := []struct {
		name      string
		template  string
		sessionID string
		agent     string
		messageID string
		want      string
	}{
		{
			name:      "valid parts",
			template:  "saolei",
			sessionID: "abc",
			agent:     "player",
			messageID: "m1",
			want:      "templates/saolei/sessions/abc/team/agents/player/messages/m1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := gameconst.MessageAgentName(tt.template, tt.sessionID, tt.agent, tt.messageID)

			// then
			if got != tt.want {
				t.Fatalf("MessageAgentName(%q, %q, %q, %q) = %q, want %q",
					tt.template, tt.sessionID, tt.agent, tt.messageID, got, tt.want)
			}
		})
	}
}

func TestMessageAgentParse(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		want        string
		wantSession string
		wantAgent   string
		wantErr     error
	}{
		{
			name:        "valid message name returns template, session and agent",
			input:       "templates/saolei/sessions/abc/team/agents/player/messages/m1",
			want:        "saolei",
			wantSession: "abc",
			wantAgent:   "player",
			wantErr:     nil,
		},
		{
			name:    "missing messages segment returns ErrInvalidTeamName",
			input:   "templates/saolei/sessions/abc/team/agents/player",
			wantErr: gameconst.ErrInvalidTeamName,
		},
		{
			name:    "missing agents segment returns ErrInvalidTeamName",
			input:   "templates/saolei/sessions/abc/team/messages/m1",
			wantErr: gameconst.ErrInvalidTeamName,
		},
		{
			name:    "old agent partition returns ErrInvalidTeamName",
			input:   "templates/saolei/sessions/abc/agent/messages/m1",
			wantErr: gameconst.ErrInvalidTeamName,
		},
		{
			name:    "empty agent returns ErrInvalidTeamName",
			input:   "templates/saolei/sessions/abc/team/agents//messages/m1",
			wantErr: gameconst.ErrInvalidTeamName,
		},
		{
			name:    "empty string returns ErrInvalidTeamName",
			input:   "",
			wantErr: gameconst.ErrInvalidTeamName,
		},
		{
			name:    "extra segment returns ErrInvalidTeamName",
			input:   "templates/saolei/sessions/abc/team/agents/player/messages/m1/extra",
			wantErr: gameconst.ErrInvalidTeamName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, gotSession, gotAgent, err := gameconst.MessageAgentParse(tt.input)

			// then
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("MessageAgentParse(%q) error = %v, want %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("MessageAgentParse(%q) template = %q, want %q", tt.input, got, tt.want)
			}
			if gotSession != tt.wantSession {
				t.Fatalf("MessageAgentParse(%q) session = %q, want %q", tt.input, gotSession, tt.wantSession)
			}
			if gotAgent != tt.wantAgent {
				t.Fatalf("MessageAgentParse(%q) agent = %q, want %q", tt.input, gotAgent, tt.wantAgent)
			}
		})
	}
}

func TestTeamProfileName(t *testing.T) {
	tests := []struct {
		name      string
		template  string
		profileID string
		want      string
	}{
		{
			name:      "valid template and profile ID",
			template:  "saolei",
			profileID: "default",
			want:      "templates/saolei/profiles/default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := gameconst.TeamProfileName(tt.template, tt.profileID)

			// then
			if got != tt.want {
				t.Fatalf("TeamProfileName(%q, %q) = %q, want %q", tt.template, tt.profileID, got, tt.want)
			}
		})
	}
}

func TestTeamProfileID(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		want        string
		wantProfile string
		wantErr     error
	}{
		{
			name:        "valid team profile name returns template and profile ID",
			input:       "templates/saolei/profiles/default",
			want:        "saolei",
			wantProfile: "default",
			wantErr:     nil,
		},
		{
			name:    "old prompts prefix returns ErrInvalidTeamProfileName",
			input:   "prompts/agentProfiles/default",
			wantErr: gameconst.ErrInvalidTeamProfileName,
		},
		{
			name:    "missing templates prefix returns ErrInvalidTeamProfileName",
			input:   "saolei/profiles/default",
			wantErr: gameconst.ErrInvalidTeamProfileName,
		},
		{
			name:    "missing profiles segment returns ErrInvalidTeamProfileName",
			input:   "templates/saolei/default",
			wantErr: gameconst.ErrInvalidTeamProfileName,
		},
		{
			name:    "empty profile ID returns ErrInvalidTeamProfileName",
			input:   "templates/saolei/profiles/",
			wantErr: gameconst.ErrInvalidTeamProfileName,
		},
		{
			name:    "empty string returns ErrInvalidTeamProfileName",
			input:   "",
			wantErr: gameconst.ErrInvalidTeamProfileName,
		},
		{
			name:    "extra segment returns ErrInvalidTeamProfileName",
			input:   "templates/saolei/profiles/default/extra",
			wantErr: gameconst.ErrInvalidTeamProfileName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, gotProfile, err := gameconst.TeamProfileID(tt.input)

			// then
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("TeamProfileID(%q) error = %v, want %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("TeamProfileID(%q) template = %q, want %q", tt.input, got, tt.want)
			}
			if gotProfile != tt.wantProfile {
				t.Fatalf("TeamProfileID(%q) profile = %q, want %q", tt.input, gotProfile, tt.wantProfile)
			}
		})
	}
}
