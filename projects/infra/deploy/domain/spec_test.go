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
		{
			name: "stateful valid with full matches",
			spec: ArtifactSpec{
				Name:         "api",
				App:          "app",
				Image:        "repo/app:v1",
				WorkloadKind: WorkloadKindStateful,
				HTTP: &ArtifactHTTPSpec{
					Hostnames: []string{"example.com"},
					Matches: []HTTPRouteRule{{
						Backend: "http",
						Path:    HTTPPathRule{Type: HTTPPathRuleTypePathPrefix, Value: "/"},
					}},
				},
			},
		},
		{
			name: "stateful valid without http",
			spec: ArtifactSpec{
				Name:         "api",
				App:          "app",
				Image:        "repo/app:v1",
				WorkloadKind: WorkloadKindStateful,
			},
		},
		{
			name: "stateful with hostnames but no matches rejected",
			spec: ArtifactSpec{
				Name:         "api",
				App:          "app",
				Image:        "repo/app:v1",
				WorkloadKind: WorkloadKindStateful,
				HTTP: &ArtifactHTTPSpec{
					Hostnames: []string{"example.com"},
				},
			},
			wantErr: true,
		},
		{
			name: "stateful with empty backend rejected",
			spec: ArtifactSpec{
				Name:         "api",
				App:          "app",
				Image:        "repo/app:v1",
				WorkloadKind: WorkloadKindStateful,
				HTTP: &ArtifactHTTPSpec{
					Hostnames: []string{"example.com"},
					Matches:   []HTTPRouteRule{{}},
				},
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
	// given
	tests := []struct {
		name    string
		kind    WorkloadKind
		spec    ArtifactHTTPSpec
		wantErr bool
	}{
		// stateless: hostnames AND matches must be non-empty
		{
			name: "stateless valid with hostnames and matches",
			kind: WorkloadKindStateless,
			spec: ArtifactHTTPSpec{
				Hostnames: []string{"example.com"},
				Matches: []HTTPRouteRule{{
					Backend: "http",
					Path:    HTTPPathRule{Type: HTTPPathRuleTypePathPrefix, Value: "/"},
				}},
			},
		},
		{
			name:    "stateless missing hostnames",
			kind:    WorkloadKindStateless,
			spec:    ArtifactHTTPSpec{Matches: []HTTPRouteRule{{Backend: "http"}}},
			wantErr: true,
		},
		{
			name:    "stateless missing matches",
			kind:    WorkloadKindStateless,
			spec:    ArtifactHTTPSpec{Hostnames: []string{"example.com"}},
			wantErr: true,
		},
		{
			name:    "stateless invalid nested rule",
			kind:    WorkloadKindStateless,
			spec:    ArtifactHTTPSpec{Hostnames: []string{"example.com"}, Matches: []HTTPRouteRule{{}}},
			wantErr: true,
		},
		// stateful: validation path matches stateless
		{
			name: "stateful valid with full matches",
			kind: WorkloadKindStateful,
			spec: ArtifactHTTPSpec{
				Hostnames: []string{"example.com"},
				Matches: []HTTPRouteRule{{
					Backend: "http",
					Path:    HTTPPathRule{Type: HTTPPathRuleTypePathPrefix, Value: "/"},
				}},
			},
		},
		{
			name:    "stateful missing matches",
			kind:    WorkloadKindStateful,
			spec:    ArtifactHTTPSpec{Hostnames: []string{"example.com"}},
			wantErr: true,
		},
		{
			name: "stateful multiple matches",
			kind: WorkloadKindStateful,
			spec: ArtifactHTTPSpec{
				Hostnames: []string{"example.com"},
				Matches: []HTTPRouteRule{{
					Backend: "http",
					Path:    HTTPPathRule{Type: HTTPPathRuleTypePathPrefix, Value: "/api"},
				}, {
					Backend: "grpc",
					Path:    HTTPPathRule{Type: HTTPPathRuleTypePathPrefix, Value: "/grpc"},
				}},
			},
		},
		{
			name:    "stateful missing hostnames",
			kind:    WorkloadKindStateful,
			spec:    ArtifactHTTPSpec{Matches: []HTTPRouteRule{{Backend: "http"}}},
			wantErr: true,
		},
		// default (zero value = WorkloadKindStateless)
		{
			name: "default kind treated as stateless valid",
			spec: ArtifactHTTPSpec{
				Hostnames: []string{"example.com"},
				Matches: []HTTPRouteRule{{
					Backend: "http",
					Path:    HTTPPathRule{Type: HTTPPathRuleTypePathPrefix, Value: "/"},
				}},
			},
		},
		{
			name:    "default kind treated as stateless missing matches",
			spec:    ArtifactHTTPSpec{Hostnames: []string{"example.com"}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			err := tt.spec.Validate()

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
