package env

import (
	"reflect"
	"strings"
	"testing"

	"dominion/tools/guitar/pkg/config"
)

func TestBuildEnvVars(t *testing.T) {
	tests := []struct {
		name  string
		given *config.Suite
		want  map[string]string
	}{
		{
			name: "single endpoint",
			given: &config.Suite{
				Env: "game.lt",
				Endpoint: map[string]config.Endpoints{
					"http": {
						"public": "https://example.com",
					},
				},
			},
			want: map[string]string{
				envKeyPrefix:                      "game.lt",
				endpointKeyPrefix + "HTTP_PUBLIC": "https://example.com",
			},
		},
		{
			name: "multiple endpoints in same protocol",
			given: &config.Suite{
				Env: "game.lt",
				Endpoint: map[string]config.Endpoints{
					"http": {
						"public": "https://example.com",
						"admin":  "https://admin.example.com",
					},
				},
			},
			want: map[string]string{
				envKeyPrefix:                      "game.lt",
				endpointKeyPrefix + "HTTP_PUBLIC": "https://example.com",
				endpointKeyPrefix + "HTTP_ADMIN":  "https://admin.example.com",
			},
		},
		{
			name: "multiple protocols",
			given: &config.Suite{
				Env: "game.lt",
				Endpoint: map[string]config.Endpoints{
					"http": {
						"public": "https://example.com",
					},
					"grpc": {
						"admin": "https://grpc.example.com",
					},
				},
			},
			want: map[string]string{
				envKeyPrefix:                      "game.lt",
				endpointKeyPrefix + "HTTP_PUBLIC": "https://example.com",
				endpointKeyPrefix + "GRPC_ADMIN":  "https://grpc.example.com",
			},
		},
		{
			name:  "no endpoints",
			given: &config.Suite{Env: "game.lt"},
			want: map[string]string{
				envKeyPrefix: "game.lt",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := BuildEnvVars(tt.given)

			// then
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("BuildEnvVars() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildTestEnvFlags(t *testing.T) {
	tests := []struct {
		name  string
		given *config.Suite
		want  map[string]string
	}{
		{
			name: "env and one endpoint",
			given: &config.Suite{
				Env: "game.lt",
				Endpoint: map[string]config.Endpoints{
					"http": {
						"public": "https://example.com",
					},
				},
			},
			want: map[string]string{
				envKeyPrefix:                      "game.lt",
				endpointKeyPrefix + "HTTP_PUBLIC": "https://example.com",
			},
		},
		{
			name:  "env only",
			given: &config.Suite{Env: "game.lt"},
			want: map[string]string{
				envKeyPrefix: "game.lt",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			flags := BuildTestEnvFlags(tt.given)

			// then
			if len(flags) != len(tt.want) {
				t.Fatalf("BuildTestEnvFlags() len = %d, want %d", len(flags), len(tt.want))
			}

			got := make(map[string]string, len(flags))
			for _, flag := range flags {
				if !strings.HasPrefix(flag, "--test_env=") {
					t.Fatalf("BuildTestEnvFlags() flag %q does not start with --test_env=", flag)
				}
				kv := strings.TrimPrefix(flag, "--test_env=")
				key, value, ok := strings.Cut(kv, "=")
				if !ok {
					t.Fatalf("BuildTestEnvFlags() flag %q is not in --test_env=KEY=VALUE format", flag)
				}
				got[key] = value
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("BuildTestEnvFlags() = %v, want %v", got, tt.want)
			}
		})
	}
}
