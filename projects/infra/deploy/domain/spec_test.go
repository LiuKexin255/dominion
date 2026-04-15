package domain

import "testing"

func TestArtifactSpec_Validate(t *testing.T) {
	tests := []struct {
		name    string
		spec    ArtifactSpec
		wantErr bool
	}{
		{
			name: "valid artifact spec without http",
			spec: ArtifactSpec{
				Name:     "api",
				App:      "app",
				Image:    "repo/app:v1",
				Ports:    []ArtifactPortSpec{{Name: "http", Port: 8080}},
				Replicas: 1,
			},
		},
		{
			name: "valid artifact spec with http",
			spec: ArtifactSpec{
				Name:  "api",
				App:   "app",
				Image: "repo/app:v1",
				Ports: []ArtifactPortSpec{{Name: "http", Port: 8080}},
				HTTP: &ArtifactHTTPSpec{
					Hostnames: []string{"example.com"},
					Matches: []HTTPRouteRule{{
						Backend: "http",
						Path:    HTTPPathRule{Type: HTTPPathRuleTypePathPrefix, Value: "/"},
					}},
				},
			},
		},
		{name: "missing name", spec: ArtifactSpec{App: "app", Image: "repo/app:v1"}, wantErr: true},
		{name: "missing app", spec: ArtifactSpec{Name: "api", Image: "repo/app:v1"}, wantErr: true},
		{name: "missing image", spec: ArtifactSpec{Name: "api", App: "app"}, wantErr: true},
		{name: "invalid port low", spec: ArtifactSpec{Name: "api", App: "app", Image: "repo/app:v1", Ports: []ArtifactPortSpec{{Port: 0}}}, wantErr: true},
		{name: "invalid port high", spec: ArtifactSpec{Name: "api", App: "app", Image: "repo/app:v1", Ports: []ArtifactPortSpec{{Port: 65536}}}, wantErr: true},
		{name: "negative replicas", spec: ArtifactSpec{Name: "api", App: "app", Image: "repo/app:v1", Replicas: -1}, wantErr: true},
		{
			name: "http validation failure",
			spec: ArtifactSpec{
				Name:  "api",
				App:   "app",
				Image: "repo/app:v1",
				HTTP:  &ArtifactHTTPSpec{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			spec := tt.spec

			// when
			err := spec.Validate()

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Validate() expected error")
				}
				if err.Error() == "" || err.Error() == ErrInvalidSpec.Error() {
					t.Fatalf("Validate() error = %v, want detailed invalid spec error", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestInfraSpec_Validate(t *testing.T) {
	tests := []struct {
		name    string
		spec    InfraSpec
		wantErr bool
	}{
		{name: "valid infra spec", spec: InfraSpec{Resource: "redis", Name: "cache"}},
		{name: "missing resource", spec: InfraSpec{Name: "cache"}, wantErr: true},
		{name: "missing name", spec: InfraSpec{Resource: "redis"}, wantErr: true},
		{name: "missing both", spec: InfraSpec{}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			spec := tt.spec

			// when
			err := spec.Validate()

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Validate() expected error")
				}
				return
			}

			if err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestHTTPRouteRule_Validate(t *testing.T) {
	tests := []struct {
		name    string
		rule    HTTPRouteRule
		wantErr bool
	}{
		{name: "valid http route rule", rule: HTTPRouteRule{Backend: "http", Path: HTTPPathRule{Type: HTTPPathRuleTypePathPrefix, Value: "/"}}},
		{name: "missing backend", rule: HTTPRouteRule{Path: HTTPPathRule{Type: HTTPPathRuleTypePathPrefix, Value: "/"}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			rule := tt.rule

			// when
			err := rule.Validate()

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Validate() expected error")
				}
				return
			}

			if err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestArtifactHTTPSpec_Validate(t *testing.T) {
	tests := []struct {
		name    string
		spec    ArtifactHTTPSpec
		wantErr bool
	}{
		{
			name: "valid artifact http spec",
			spec: ArtifactHTTPSpec{
				Hostnames: []string{"example.com"},
				Matches: []HTTPRouteRule{{
					Backend: "http",
					Path:    HTTPPathRule{Type: HTTPPathRuleTypePathPrefix, Value: "/"},
				}},
			},
		},
		{name: "missing hostnames", spec: ArtifactHTTPSpec{Matches: []HTTPRouteRule{{Backend: "http"}}}, wantErr: true},
		{name: "missing matches", spec: ArtifactHTTPSpec{Hostnames: []string{"example.com"}}, wantErr: true},
		{name: "invalid nested rule", spec: ArtifactHTTPSpec{Hostnames: []string{"example.com"}, Matches: []HTTPRouteRule{{}}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			spec := tt.spec

			// when
			err := spec.Validate()

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Validate() expected error")
				}
				return
			}

			if err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
		})
	}
}
