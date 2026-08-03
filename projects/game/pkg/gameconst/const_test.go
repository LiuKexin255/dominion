package gameconst_test

import (
	"errors"
	"testing"

	game "dominion/projects/game"
	"dominion/projects/game/pkg/gameconst"
)

func TestSaoleiTemplate(t *testing.T) {
	// when
	got := gameconst.SaoleiTemplate

	// then
	if got.TemplateID != "saolei" {
		t.Fatalf("SaoleiTemplate.TemplateID = %q, want %q", got.TemplateID, "saolei")
	}
	if got.String() != "templates/saolei" {
		t.Fatalf("SaoleiTemplate.String() = %q, want %q", got.String(), "templates/saolei")
	}
	if got.Validate() != nil {
		t.Fatalf("SaoleiTemplate.Validate() error = %v, want nil", got.Validate())
	}
}

func TestParseTemplateName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "valid template name",
			input: "templates/saolei",
			want:  "saolei",
		},
		{
			name:    "missing templates prefix",
			input:   "saolei",
			wantErr: true,
		},
		{
			name:    "empty template segment",
			input:   "templates/",
			wantErr: true,
		},
		{
			name:    "extra segment",
			input:   "templates/saolei/extra",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, err := game.ParseTemplateName(tt.input)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseTemplateName(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTemplateName(%q) unexpected error: %v", tt.input, err)
			}
			if got.TemplateID != tt.want {
				t.Fatalf("ParseTemplateName(%q) TemplateID = %q, want %q", tt.input, got.TemplateID, tt.want)
			}
		})
	}
}

func TestValidateTemplateName(t *testing.T) {
	tests := []struct {
		name    string
		tpl     game.TemplateName
		wantErr error
	}{
		{
			name:    "known template",
			tpl:     gameconst.SaoleiTemplate,
			wantErr: nil,
		},
		{
			name:    "unknown template",
			tpl:     game.TemplateName{TemplateID: "xxx"},
			wantErr: gameconst.ErrInvalidTemplate,
		},
		{
			name:    "empty template id",
			tpl:     game.TemplateName{TemplateID: ""},
			wantErr: gameconst.ErrInvalidTemplate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			err := gameconst.ValidateTemplateName(tt.tpl)

			// then
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateTemplateName(%v) error = %v, want %v", tt.tpl, err, tt.wantErr)
			}
		})
	}
}

func TestIsKnownTemplateID(t *testing.T) {
	tests := []struct {
		name    string
		segment string
		want    bool
	}{
		{name: "known template", segment: "saolei", want: true},
		{name: "unknown template", segment: "xxx", want: false},
		{name: "empty segment", segment: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := gameconst.IsKnownTemplateID(tt.segment)

			// then
			if got != tt.want {
				t.Fatalf("IsKnownTemplateID(%q) = %v, want %v", tt.segment, got, tt.want)
			}
		})
	}
}
