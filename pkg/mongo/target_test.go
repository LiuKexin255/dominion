package mongo

import (
	"reflect"
	"testing"
)

func TestParseTarget(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    *Target
		wantErr bool
	}{
		{name: "success", raw: "app/mongo-main", want: &Target{App: "app", Name: "mongo-main"}},
		{name: "trimmed target", raw: "  app/mongo-main  ", want: &Target{App: "app", Name: "mongo-main"}},
		{name: "trims parts", raw: " app / mongo-main ", want: &Target{App: "app", Name: "mongo-main"}},
		{name: "blank", raw: "   ", wantErr: true},
		{name: "missing slash", raw: "app", wantErr: true},
		{name: "missing app", raw: "/mongo-main", wantErr: true},
		{name: "missing name", raw: "app/", wantErr: true},
		{name: "extra path segment", raw: "app/mongo/main", wantErr: true},
		{name: "scheme rejected", raw: "mongodb://app/mongo-main", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, err := ParseTarget(tt.raw)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseTarget(%q) expected error", tt.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTarget(%q) unexpected error: %v", tt.raw, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ParseTarget(%q) = %#v, want %#v", tt.raw, got, tt.want)
			}
		})
	}
}
