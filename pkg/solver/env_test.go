package solver

import (
	"reflect"
	"testing"
)

func Test_osEnvLoader_Load(t *testing.T) {
	tests := []struct {
		name    string
		given   map[string]string
		target  *Target
		want    *environment
		wantErr bool
	}{
		{
			name: "success",
			given: map[string]string{
				dominionAppEnvKey:         "app-a",
				dominionEnvironmentEnvKey: "dev",
				podNamespaceEnvKey:        "ns-a",
			},
			target: &Target{App: "app-a"},
			want:   &environment{Name: "dev", App: "app-a", Namespace: "ns-a"},
		},
		{
			name: "target app mismatch",
			given: map[string]string{
				dominionAppEnvKey:         "app-a",
				dominionEnvironmentEnvKey: "dev",
				podNamespaceEnvKey:        "ns-a",
			},
			target:  &Target{App: "app-b"},
			wantErr: true,
		},
		{
			name: "missing app env",
			given: map[string]string{
				dominionEnvironmentEnvKey: "dev",
				podNamespaceEnvKey:        "ns-a",
			},
			target:  &Target{App: "app-a"},
			wantErr: true,
		},
		{
			name: "missing environment env",
			given: map[string]string{
				dominionAppEnvKey:  "app-a",
				podNamespaceEnvKey: "ns-a",
			},
			target:  &Target{App: "app-a"},
			wantErr: true,
		},
		{
			name: "missing namespace env",
			given: map[string]string{
				dominionAppEnvKey:         "app-a",
				dominionEnvironmentEnvKey: "dev",
			},
			target:  &Target{App: "app-a"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalLookupEnv := lookupEnv
			t.Cleanup(func() {
				lookupEnv = originalLookupEnv
			})

			lookupEnv = func(key string) (string, bool) {
				value, ok := tt.given[key]
				return value, ok
			}

			// when
			got, err := loadEnvironment(tt.target)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Load(%#v) expected error", tt.target)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load(%#v) unexpected error: %v", tt.target, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Load(%#v) = %#v, want %#v", tt.target, got, tt.want)
			}
		})
	}
}
