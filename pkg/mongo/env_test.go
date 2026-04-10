package mongo

import "testing"

func TestLoadEnvironment(t *testing.T) {
	tests := []struct {
		name    string
		target  *Target
		env     map[string]string
		want    *Environment
		wantErr bool
	}{
		{
			name:   "success",
			target: &Target{App: "app", Name: "mongo-main"},
			env: map[string]string{
				dominionAppEnvKey:         "app",
				dominionEnvironmentEnvKey: "dev",
				podNamespaceEnvKey:        "team-a",
			},
			want: &Environment{Name: "dev", App: "app", Namespace: "team-a"},
		},
		{
			name:   "trims env values",
			target: &Target{App: "app", Name: "mongo-main"},
			env: map[string]string{
				dominionAppEnvKey:         " app ",
				dominionEnvironmentEnvKey: " dev ",
				podNamespaceEnvKey:        " team-a ",
			},
			want: &Environment{Name: "dev", App: "app", Namespace: "team-a"},
		},
		{name: "missing app", target: &Target{App: "app", Name: "mongo-main"}, env: map[string]string{dominionEnvironmentEnvKey: "dev", podNamespaceEnvKey: "team-a"}, wantErr: true},
		{name: "missing environment", target: &Target{App: "app", Name: "mongo-main"}, env: map[string]string{dominionAppEnvKey: "app", podNamespaceEnvKey: "team-a"}, wantErr: true},
		{name: "missing namespace", target: &Target{App: "app", Name: "mongo-main"}, env: map[string]string{dominionAppEnvKey: "app", dominionEnvironmentEnvKey: "dev"}, wantErr: true},
		{name: "blank app", target: &Target{App: "app", Name: "mongo-main"}, env: map[string]string{dominionAppEnvKey: "   ", dominionEnvironmentEnvKey: "dev", podNamespaceEnvKey: "team-a"}, wantErr: true},
		{name: "cross app target rejected", target: &Target{App: "other-app", Name: "mongo-main"}, env: map[string]string{dominionAppEnvKey: "app", dominionEnvironmentEnvKey: "dev", podNamespaceEnvKey: "team-a"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalLookupEnv := lookupEnv
			t.Cleanup(func() { lookupEnv = originalLookupEnv })
			lookupEnv = func(key string) (string, bool) {
				value, ok := tt.env[key]
				return value, ok
			}

			// when
			got, err := loadEnvironment(tt.target)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("loadEnvironment(%#v) expected error", tt.target)
				}
				return
			}
			if err != nil {
				t.Fatalf("loadEnvironment(%#v) unexpected error: %v", tt.target, err)
			}
			if got == nil {
				t.Fatalf("loadEnvironment(%#v) = nil, want %#v", tt.target, tt.want)
			}
			if *got != *tt.want {
				t.Fatalf("loadEnvironment(%#v) = %#v, want %#v", tt.target, *got, *tt.want)
			}
		})
	}
}

func TestOSEnvLoader_Load(t *testing.T) {
	loader := new(OSEnvLoader)
	t.Setenv(dominionAppEnvKey, "app")
	t.Setenv(dominionEnvironmentEnvKey, "dev")
	t.Setenv(podNamespaceEnvKey, "team-a")

	// when
	got, err := loader.Load(&Target{App: "app", Name: "mongo-main"})

	// then
	if err != nil {
		t.Fatalf("OSEnvLoader.Load() unexpected error: %v", err)
	}
	want := &Environment{Name: "dev", App: "app", Namespace: "team-a"}
	if got == nil {
		t.Fatalf("OSEnvLoader.Load() = nil, want %#v", want)
	}
	if *got != *want {
		t.Fatalf("OSEnvLoader.Load() = %#v, want %#v", *got, *want)
	}
}
