package runid

import (
	"regexp"
	"testing"
)

func TestGenerate(t *testing.T) {
	tests := []struct {
		name   string
		wantRe string
	}{
		{
			name:   "format matches lt prefix with 6 base36 chars",
			wantRe: `^lt[a-z0-9]{6}$`,
		},
		{
			name:   "satisfies deploy env name constraint",
			wantRe: `^[a-z][a-z0-9]{0,7}$`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Generate()
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}

			re := regexp.MustCompile(tt.wantRe)
			if !re.MatchString(got) {
				t.Fatalf("Generate() = %q, want match %s", got, tt.wantRe)
			}
		})
	}
}

func TestGenerateCollision(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 100; i++ {
		id, err := Generate()
		if err != nil {
			t.Fatalf("Generate() error = %v", err)
		}
		if _, ok := seen[id]; ok {
			t.Fatalf("Generate() collision at iteration %d: %q", i, id)
		}
		seen[id] = struct{}{}
	}
}
