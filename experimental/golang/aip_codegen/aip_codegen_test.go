package aip_codegen_test

import (
	"testing"

	aip "dominion/experimental/golang/aip_codegen"
)

// aip_codegen_test demonstrates the protoc-gen-go-aip generated API and guards
// its shape. It mirrors the migration pattern that will replace the hand-
// written gameconst parsers. See FINDINGS.md.
func TestParseSessionName(t *testing.T) {
	// given: a well-formed session resource name
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "valid", input: "templates/saolei/sessions/sess-1"},
		{name: "bad prefix", input: "foo/saolei/sessions/sess-1", wantErr: true},
		{name: "too few segments", input: "templates/saolei", wantErr: true},
		{name: "empty session id", input: "templates/saolei/sessions/", wantErr: true},
		{name: "wrong collection", input: "templates/saolei/profiles/p1", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when: parse via the generated function
			got, err := aip.ParseSessionName(tt.input)

			// then: error expectation holds
			if tt.wantErr && err == nil {
				t.Fatalf("ParseSessionName(%q) expected error, got %+v", tt.input, got)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ParseSessionName(%q) unexpected error: %v", tt.input, err)
			}
		})
	}
}

func TestSessionNameRoundtrip(t *testing.T) {
	// given: a parsed session name
	parsed, err := aip.ParseSessionName("templates/saolei/sessions/sess-1")
	if err != nil {
		t.Fatalf("ParseSessionName error: %v", err)
	}

	// then: segments are extracted once (the core value over gameconst's
	// positional return)
	if parsed.TemplateID != "saolei" || parsed.SessionID != "sess-1" {
		t.Errorf("segments: got Template=%q Session=%q", parsed.TemplateID, parsed.SessionID)
	}

	// then: String() reconstructs the resource name (replaces SessionName())
	if got := parsed.String(); got != "templates/saolei/sessions/sess-1" {
		t.Errorf("String(): got %q", got)
	}

	// then: Validate passes for a well-formed name
	if err := parsed.Validate(); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}
}

func TestTeamNameTrailingLiteralSingleton(t *testing.T) {
	// given: the Team resource ends in a literal "team" segment (mirrors
	// game.proto Team pattern). The generated parser must validate that
	// literal segment and String() must reconstruct it.
	parsed, err := aip.ParseTeamName("templates/saolei/sessions/sess-1/team")
	if err != nil {
		t.Fatalf("ParseTeamName error: %v", err)
	}
	if got := parsed.String(); got != "templates/saolei/sessions/sess-1/team" {
		t.Errorf("Team String(): got %q", got)
	}

	// when: the trailing literal is missing
	if _, err := aip.ParseTeamName("templates/saolei/sessions/sess-1"); err == nil {
		t.Error("ParseTeamName expected error for missing trailing 'team' segment")
	}
}
