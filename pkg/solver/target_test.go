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
		{name: "with port", given: "app/service:50051", want: &Target{App: "app", Service: "service", Port: 50051}},
		{name: "without port", given: "app/service", want: &Target{App: "app", Service: "service", Port: 0}},
		{name: "with scheme", given: "dominion:///app/service:50051", want: &Target{App: "app", Service: "service", Port: 50051}},
		{name: "empty string", given: "", wantErr: true},
		{name: "missing service", given: "app/", wantErr: true},
		{name: "missing app", given: "/service", wantErr: true},
		{name: "port not numeric", given: "app/service:abc", wantErr: true},
		{name: "port out of range", given: "app/service:70000", wantErr: true},
		{name: "extra slashes", given: "a/b/c", wantErr: true},
		{name: "with whitespace", given: " app / service : 50051 ", want: &Target{App: "app", Service: "service", Port: 50051}},
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
