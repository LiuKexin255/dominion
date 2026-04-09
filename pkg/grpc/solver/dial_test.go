package solver_test

import (
	"dominion/pkg/grpc/solver"
	"testing"
)

func TestURI(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		raw  string
		want string
	}{
		{
			name: "plain target is converted to dominion URI",
			raw:  "app/service:8080",
			want: "dominion:///app/service:8080",
		},
		{
			name: "existing dominion URI is preserved",
			raw:  "dominion:///app/service:8080",
			want: "dominion:///app/service:8080",
		},
		{
			name: "surrounding spaces are trimmed before conversion",
			raw:  "  app/service:8080  ",
			want: "dominion:///app/service:8080",
		},
		{
			name: "empty target falls back to original raw value",
			raw:  "   ",
			want: "   ",
		},
		{
			name: "non dominion scheme falls back to original raw value",
			raw:  "dns:///app/service:8080",
			want: "dns:///app/service:8080",
		},
		{
			name: "non numeric port falls back to original raw value",
			raw:  "app/service:http",
			want: "app/service:http",
		},
		{
			name: "port out of range falls back to original raw value",
			raw:  "app/service:65536",
			want: "app/service:65536",
		},
		{
			name: "missing service falls back to original raw value",
			raw:  "app/:8080",
			want: "app/:8080",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := solver.URI(tt.raw)
			if got != tt.want {
				t.Fatalf("URI(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
