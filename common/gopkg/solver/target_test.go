package solver

import (
	"reflect"
	"testing"
)

func TestParseTarget(t *testing.T) {
	tests := []struct {
		name    string
		given   string
		want    *Target
		wantErr bool
	}{
		{name: "with port", given: "app/service:50051", want: &Target{App: "app", Service: "service", PortSelector: NumericPort(50051)}},
		{name: "without port", given: "app/service", wantErr: true},
		{name: "with scheme", given: "dominion:///app/service:50051", want: &Target{App: "app", Service: "service", PortSelector: NumericPort(50051)}},
		{name: "empty string", given: "", wantErr: true},
		{name: "missing service", given: "app/", wantErr: true},
		{name: "missing app", given: "/service", wantErr: true},
		{name: "named port", given: "app/service:grpc", want: &Target{App: "app", Service: "service", PortSelector: NamedPort("grpc")}},
		{name: "named port with hyphens", given: "billing/api:grpc-web", want: &Target{App: "billing", Service: "api", PortSelector: NamedPort("grpc-web")}},
		{name: "invalid named port uppercase", given: "app/service:Grpc", wantErr: true},
		{name: "invalid named port special char", given: "app/service:grpc_web", wantErr: true},
		{name: "port out of range", given: "app/service:70000", wantErr: true},
		{name: "extra slashes", given: "a/b/c", wantErr: true},
		{name: "with whitespace", given: " app / service : 50051 ", want: &Target{App: "app", Service: "service", PortSelector: NumericPort(50051)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, err := ParseTarget(tt.given)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseTarget(%q) expected error", tt.given)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTarget(%q) unexpected error: %v", tt.given, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ParseTarget(%q) = %#v, want %#v", tt.given, got, tt.want)
			}
		})
	}
}

func TestPortSelector(t *testing.T) {
	tests := []struct {
		name string
		got  PortSelector
		want struct {
			numeric   int
			name      string
			isNumeric bool
			isNamed   bool
			isEmpty   bool
			string    string
		}
	}{
		{
			name: "numeric port",
			got:  NumericPort(80),
			want: struct {
				numeric   int
				name      string
				isNumeric bool
				isNamed   bool
				isEmpty   bool
				string    string
			}{numeric: 80, isNumeric: true, string: "80"},
		},
		{
			name: "named port",
			got:  NamedPort("grpc"),
			want: struct {
				numeric   int
				name      string
				isNumeric bool
				isNamed   bool
				isEmpty   bool
				string    string
			}{name: "grpc", isNamed: true, string: "grpc"},
		},
		{
			name: "empty selector",
			got:  PortSelector{},
			want: struct {
				numeric   int
				name      string
				isNumeric bool
				isNamed   bool
				isEmpty   bool
				string    string
			}{isEmpty: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.got.Numeric(); got != tt.want.numeric {
				t.Fatalf("Numeric() = %d, want %d", got, tt.want.numeric)
			}
			if got := tt.got.Name(); got != tt.want.name {
				t.Fatalf("Name() = %q, want %q", got, tt.want.name)
			}
			if got := tt.got.IsNumeric(); got != tt.want.isNumeric {
				t.Fatalf("IsNumeric() = %t, want %t", got, tt.want.isNumeric)
			}
			if got := tt.got.IsNamed(); got != tt.want.isNamed {
				t.Fatalf("IsNamed() = %t, want %t", got, tt.want.isNamed)
			}
			if got := tt.got.IsEmpty(); got != tt.want.isEmpty {
				t.Fatalf("IsEmpty() = %t, want %t", got, tt.want.isEmpty)
			}
			if got := tt.got.String(); got != tt.want.string {
				t.Fatalf("String() = %q, want %q", got, tt.want.string)
			}
		})
	}
}
