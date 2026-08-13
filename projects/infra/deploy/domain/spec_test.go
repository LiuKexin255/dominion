package domain

import (
	"strings"
	"testing"
)

func TestSecretBinding_Validate(t *testing.T) {
	tests := []struct {
		name         string
		binding      SecretBinding
		wantErr      bool
		wantContains string
	}{
		{
			name:    "valid secret binding",
			binding: SecretBinding{LogicalName: "db-password", SecretName: "db-secret", Key: "password"},
		},
		{
			name:         "empty logical_name",
			binding:      SecretBinding{LogicalName: "", SecretName: "db-secret", Key: "password"},
			wantErr:      true,
			wantContains: "logical_name is required",
		},
		{
			name:         "empty secret_name",
			binding:      SecretBinding{LogicalName: "db-password", SecretName: "", Key: "password"},
			wantErr:      true,
			wantContains: "secret_name is required",
		},
		{
			name:         "empty key",
			binding:      SecretBinding{LogicalName: "db-password", SecretName: "db-secret", Key: ""},
			wantErr:      true,
			wantContains: "key is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			binding := tt.binding

			// when
			err := binding.Validate()

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Validate() expected error")
				}
				if tt.wantContains != "" && !strings.Contains(err.Error(), tt.wantContains) {
					t.Fatalf("Validate() error = %q, want substring %q", err.Error(), tt.wantContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestConfigEntry_Validate(t *testing.T) {
	tests := []struct {
		name         string
		entry        ConfigEntry
		wantErr      bool
		wantContains string
	}{
		{
			name:  "valid config entry",
			entry: ConfigEntry{Key: "greeting", Type: "yaml", Value: "message: hello\n"},
		},
		{
			name:         "empty key",
			entry:        ConfigEntry{Type: "yaml", Value: "message: hello\n"},
			wantErr:      true,
			wantContains: "key is required",
		},
		{
			name:         "empty type",
			entry:        ConfigEntry{Key: "greeting", Value: "message: hello\n"},
			wantErr:      true,
			wantContains: "type is required",
		},
		{
			name:         "empty value",
			entry:        ConfigEntry{Key: "greeting", Type: "yaml"},
			wantErr:      true,
			wantContains: "value is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			entry := tt.entry

			// when
			err := entry.Validate()

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Validate() expected error")
				}
				if tt.wantContains != "" && !strings.Contains(err.Error(), tt.wantContains) {
					t.Fatalf("Validate() error = %q, want substring %q", err.Error(), tt.wantContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestConfigBlock_Validate(t *testing.T) {
	tests := []struct {
		name         string
		block        ConfigBlock
		wantErr      bool
		wantContains string
	}{
		{
			name: "valid config block with entries",
			block: ConfigBlock{
				Block: "service_config",
				Entries: []*ConfigEntry{
					{Key: "greeting", Type: "yaml", Value: "message: hello\n"},
					{Key: "limits", Type: "json", Value: `{"maxConn": 100}`},
				},
			},
		},
		{
			name: "empty block name",
			block: ConfigBlock{
				Entries: []*ConfigEntry{{Key: "greeting", Type: "yaml", Value: "message: hello\n"}},
			},
			wantErr:      true,
			wantContains: "block is required",
		},
		{
			name: "empty entries",
			block: ConfigBlock{
				Block: "service_config",
			},
			wantErr:      true,
			wantContains: "entries must not be empty",
		},
		{
			name: "entry validation failure propagates",
			block: ConfigBlock{
				Block: "service_config",
				Entries: []*ConfigEntry{
					{Key: "", Type: "yaml", Value: "message: hello\n"},
				},
			},
			wantErr:      true,
			wantContains: "entries[0]: invalid deployment spec: key is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			block := tt.block

			// when
			err := block.Validate()

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Validate() expected error")
				}
				if tt.wantContains != "" && !strings.Contains(err.Error(), tt.wantContains) {
					t.Fatalf("Validate() error = %q, want substring %q", err.Error(), tt.wantContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestArtifactSpec_Validate(t *testing.T) {
	tests := []struct {
		name         string
		spec         ArtifactSpec
		wantErr      bool
		wantContains string
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
			name: "valid env keys",
			spec: ArtifactSpec{
				Name:  "api",
				App:   "app",
				Image: "repo/app:v1",
				Env: map[string]string{
					"FOO":      "bar",
					"_PRIVATE": "secret",
					"ABC123":   "value",
				},
			},
		},
		{
			name: "lowercase env key accepted",
			spec: ArtifactSpec{
				Name:  "api",
				App:   "app",
				Image: "repo/app:v1",
				Env:   map[string]string{"my_var": "value"},
			},
		},
		{
			name: "empty env value allowed",
			spec: ArtifactSpec{
				Name:  "api",
				App:   "app",
				Image: "repo/app:v1",
				Env:   map[string]string{"EMPTY": ""},
			},
		},
		{
			name:    "empty env key rejected",
			spec:    ArtifactSpec{Name: "api", App: "app", Image: "repo/app:v1", Env: map[string]string{"": "value"}},
			wantErr: true,
		},
		{
			name:    "env key starting with digit rejected",
			spec:    ArtifactSpec{Name: "api", App: "app", Image: "repo/app:v1", Env: map[string]string{"1FOO": "bar"}},
			wantErr: true,
		},
		{
			name:    "env key with hyphen rejected",
			spec:    ArtifactSpec{Name: "api", App: "app", Image: "repo/app:v1", Env: map[string]string{"FOO-BAR": "baz"}},
			wantErr: true,
		},
		{
			name:    "env key with space rejected",
			spec:    ArtifactSpec{Name: "api", App: "app", Image: "repo/app:v1", Env: map[string]string{"FOO BAR": "baz"}},
			wantErr: true,
		},
		{
			name:    "env key with dot rejected",
			spec:    ArtifactSpec{Name: "api", App: "app", Image: "repo/app:v1", Env: map[string]string{"FOO.BAR": "baz"}},
			wantErr: true,
		},
		{
			name: "stateful with http rejected",
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
			wantErr:      true,
			wantContains: "http is only supported for stateless workloads",
		},
		{
			name: "stateful without http valid",
			spec: ArtifactSpec{
				Name:         "api",
				App:          "app",
				Image:        "repo/app:v1",
				WorkloadKind: WorkloadKindStateful,
			},
		},
		{
			name: "valid with secret bindings",
			spec: ArtifactSpec{
				Name:  "api",
				App:   "app",
				Image: "repo/app:v1",
				SecretBindings: []*SecretBinding{
					{LogicalName: "db-password", SecretName: "db-secret", Key: "password"},
					{LogicalName: "api-key", SecretName: "api-secret", Key: "key"},
				},
			},
		},
		{
			name: "duplicate secret binding logical_name",
			spec: ArtifactSpec{
				Name:  "api",
				App:   "app",
				Image: "repo/app:v1",
				SecretBindings: []*SecretBinding{
					{LogicalName: "db-password", SecretName: "db-secret", Key: "password"},
					{LogicalName: "db-password", SecretName: "other-secret", Key: "other"},
				},
			},
			wantErr:      true,
			wantContains: `secret_bindings[1]: duplicate logical_name "db-password"`,
		},
		{
			name: "valid with config blocks",
			spec: ArtifactSpec{
				Name:  "api",
				App:   "app",
				Image: "repo/app:v1",
				ConfigBlocks: []*ConfigBlock{
					{
						Block: "service_config",
						Entries: []*ConfigEntry{
							{Key: "greeting", Type: "yaml", Value: "message: hello\n"},
							{Key: "limits", Type: "json", Value: `{"maxConn": 100}`},
						},
					},
					{
						Block: "feature_flags",
						Entries: []*ConfigEntry{
							{Key: "beta", Type: "yaml", Value: "true"},
						},
					},
				},
			},
		},
		{
			name: "empty config entry fields rejected",
			spec: ArtifactSpec{
				Name:  "api",
				App:   "app",
				Image: "repo/app:v1",
				ConfigBlocks: []*ConfigBlock{
					{
						Block: "service_config",
						Entries: []*ConfigEntry{
							{Key: "", Type: "yaml", Value: "message: hello\n"},
						},
					},
				},
			},
			wantErr:      true,
			wantContains: "config_blocks[0]: invalid deployment spec: entries[0]: invalid deployment spec: key is required",
		},
		{
			name: "duplicate config block name rejected",
			spec: ArtifactSpec{
				Name:  "api",
				App:   "app",
				Image: "repo/app:v1",
				ConfigBlocks: []*ConfigBlock{
					{
						Block:   "service_config",
						Entries: []*ConfigEntry{{Key: "greeting", Type: "yaml", Value: "message: hello\n"}},
					},
					{
						Block:   "service_config",
						Entries: []*ConfigEntry{{Key: "limits", Type: "json", Value: `{"maxConn": 100}`}},
					},
				},
			},
			wantErr:      true,
			wantContains: `config_blocks[1]: duplicate block "service_config"`,
		},
		{
			name: "duplicate entry key within block rejected",
			spec: ArtifactSpec{
				Name:  "api",
				App:   "app",
				Image: "repo/app:v1",
				ConfigBlocks: []*ConfigBlock{
					{
						Block: "service_config",
						Entries: []*ConfigEntry{
							{Key: "greeting", Type: "yaml", Value: "message: hello\n"},
							{Key: "greeting", Type: "json", Value: `{"x": 1}`},
						},
					},
				},
			},
			wantErr:      true,
			wantContains: `config_blocks[0]: duplicate entry key "greeting"`,
		},
		{
			name: "same key in different blocks is not a duplicate",
			spec: ArtifactSpec{
				Name:  "api",
				App:   "app",
				Image: "repo/app:v1",
				ConfigBlocks: []*ConfigBlock{
					{
						Block:   "service_config",
						Entries: []*ConfigEntry{{Key: "greeting", Type: "yaml", Value: "message: hello\n"}},
					},
					{
						Block:   "feature_flags",
						Entries: []*ConfigEntry{{Key: "greeting", Type: "yaml", Value: "false"}},
					},
				},
			},
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
				if tt.wantContains != "" && !strings.Contains(err.Error(), tt.wantContains) {
					t.Fatalf("Validate() error = %q, want substring %q", err.Error(), tt.wantContains)
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
