package domain

import (
	"strings"
	"testing"
)

func TestValidateMemoryID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{name: "lower-case letters digits hyphen and underscore", id: "mem-1_ab"},
		{name: "digits only", id: "123"},
		{name: "upper-case is rejected", id: "Mem-1", wantErr: true},
		{name: "slash is rejected", id: "mem/1", wantErr: true},
		{name: "dot is rejected", id: "mem.1", wantErr: true},
		{name: "empty is rejected", id: "", wantErr: true},
		{name: "non-ascii is rejected", id: "记忆", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			err := ValidateMemoryID(tt.id)

			// then
			assertErrorPresence(t, err, tt.wantErr)
		})
	}
}

func TestValidateMemoryContent(t *testing.T) {
	// The multibyte cases pin the rune measure: 1024 CJK characters are 3072
	// bytes, so a byte-based bound would reject the boundary case.
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name:    "empty content is not a length violation",
			content: "",
		},
		{
			name:    "1024 ASCII runes at the boundary",
			content: strings.Repeat("a", MaxMemoryContentRunes),
		},
		{
			name:    "1025 ASCII runes is over the boundary",
			content: strings.Repeat("a", MaxMemoryContentRunes+1),
			wantErr: true,
		},
		{
			name:    "1024 multibyte runes at the boundary",
			content: strings.Repeat("中", MaxMemoryContentRunes),
		},
		{
			name:    "1025 multibyte runes is over the boundary",
			content: strings.Repeat("中", MaxMemoryContentRunes+1),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			err := ValidateMemoryContent(tt.content)

			// then
			assertErrorPresence(t, err, tt.wantErr)
		})
	}
}

func TestValidateMemoryContentErrorMessage(t *testing.T) {
	// given
	content := strings.Repeat("中", MaxMemoryContentRunes+1)

	// when
	err := ValidateMemoryContent(content)

	// then — the message names both the limit and the actual rune count
	// (specs/069-wiki-template-web/contracts/memory-content-limit.md §2/§5).
	if err == nil {
		t.Fatalf("ValidateMemoryContent(%d runes) = nil, want error", MaxMemoryContentRunes+1)
	}
	for _, want := range []string{"1024", "1025"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ValidateMemoryContent error = %q, want it to contain %q", err.Error(), want)
		}
	}
}
