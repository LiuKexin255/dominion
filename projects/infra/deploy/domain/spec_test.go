package domain

import "testing"

func TestServiceSpec_Validate(t *testing.T) {
	tests := []struct {
		name    string
		spec    ServiceSpec
		wantErr bool
	}{
		{
			name: "valid service spec",
			spec: ServiceSpec{
				Name:     "api",
				App:      "app",
				Image:    "repo/app:v1",
				Ports:    []ServicePortSpec{{Name: "http", Port: 8080}},
				Replicas: 1,
			},
		},
		{name: "missing name", spec: ServiceSpec{App: "app", Image: "repo/app:v1"}, wantErr: true},
		{name: "missing app", spec: ServiceSpec{Name: "api", Image: "repo/app:v1"}, wantErr: true},
		{name: "missing image", spec: ServiceSpec{Name: "api", App: "app"}, wantErr: true},
		{name: "invalid port low", spec: ServiceSpec{Name: "api", App: "app", Image: "repo/app:v1", Ports: []ServicePortSpec{{Port: 0}}}, wantErr: true},
		{name: "invalid port high", spec: ServiceSpec{Name: "api", App: "app", Image: "repo/app:v1", Ports: []ServicePortSpec{{Port: 65536}}}, wantErr: true},
		{name: "negative replicas", spec: ServiceSpec{Name: "api", App: "app", Image: "repo/app:v1", Replicas: -1}, wantErr: true},
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
		{name: "valid http route rule", rule: HTTPRouteRule{Backend: "api", Path: HTTPPathRule{Type: HTTPPathRuleTypePathPrefix, Value: "/"}}},
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

func TestHTTPRouteSpec_Validate(t *testing.T) {
	tests := []struct {
		name    string
		spec    HTTPRouteSpec
		wantErr bool
	}{
		{
			name: "valid http route spec",
			spec: HTTPRouteSpec{
				Hostnames: []string{"example.com"},
				Rules:     []HTTPRouteRule{{Backend: "api", Path: HTTPPathRule{Type: HTTPPathRuleTypePathPrefix, Value: "/"}}},
			},
		},
		{name: "missing hostnames", spec: HTTPRouteSpec{Rules: []HTTPRouteRule{{Backend: "api"}}}, wantErr: true},
		{name: "missing rules", spec: HTTPRouteSpec{Hostnames: []string{"example.com"}}, wantErr: true},
		{name: "invalid nested rule", spec: HTTPRouteSpec{Hostnames: []string{"example.com"}, Rules: []HTTPRouteRule{{}}}, wantErr: true},
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
