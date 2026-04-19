package solver_test

import (
	"testing"

	"dominion/pkg/grpc/solver"
)

func TestURI(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		raw  string
		opts []solver.URIOption
		want string
	}{
		{
			name: "plain target is converted to dominion URI",
			raw:  "app/service:8080",
			opts: nil,
			want: "dominion:///app/service:8080",
		},
		{
			name: "existing dominion URI is preserved",
			raw:  "dominion:///app/service:8080",
			opts: nil,
			want: "dominion:///app/service:8080",
		},
		{
			name: "surrounding spaces are trimmed before conversion",
			raw:  "  app/service:8080  ",
			opts: nil,
			want: "dominion:///app/service:8080",
		},
		{name: "empty target falls back to original raw value", raw: "   ", opts: nil, want: "   "},
		{
			name: "non dominion scheme falls back to original raw value",
			raw:  "dns:///app/service:8080",
			opts: nil,
			want: "dns:///app/service:8080",
		},
		{name: "named port converts to dominion URI", raw: "app/service:http", opts: nil, want: "dominion:///app/service:http"},
		{name: "missing port falls back to original raw value", raw: "app/service", opts: nil, want: "app/service"},
		{
			name: "port out of range falls back to original raw value",
			raw:  "app/service:65536",
			opts: nil,
			want: "app/service:65536",
		},
		{
			name: "missing service falls back to original raw value",
			raw:  "app/:8080",
			opts: nil,
			want: "app/:8080",
		},
		{
			name: "with instance 0 uses stateful scheme",
			raw:  "app/svc:grpc",
			opts: []solver.URIOption{solver.WithInstance(0)},
			want: "dominion-stateful:///app/svc:grpc?instance=0",
		},
		{
			name: "with instance 3 uses stateful scheme",
			raw:  "app/svc:grpc",
			opts: []solver.URIOption{solver.WithInstance(3)},
			want: "dominion-stateful:///app/svc:grpc?instance=3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := solver.URI(tt.raw, tt.opts...)
			if got != tt.want {
				t.Fatalf("URI(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
