package mongo

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"testing"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/solver"

	mongodriver "go.mongodb.org/mongo-driver/mongo"
)

type stubResolver struct {
	resolveAddresses []string
	err              error
}

func (s *stubResolver) Resolve(_ context.Context, _ *solver.Target) ([]string, error) {
	return s.resolveAddresses, s.err
}

func withStubResolver(r solver.Resolver) ClientOption {
	return func(opts *ClientOptions) {
		opts.resolverBuilder = func() (solver.Resolver, error) {
			if r == nil {
				return nil, errors.New("missing resolver")
			}
			return r, nil
		}
	}
}

func TestNewClient(t *testing.T) {
	tests := []struct {
		name      string
		rawTarget string
		env       map[string]string

		options   []ClientOption
		clientErr error
		wantURI   string
		wantErr   bool
		errSubstr string
	}{
		{
			name:      "success",
			rawTarget: "app/mongo-main",
			env:       map[string]string{dominionEnvironmentEnvKey: "dev"},
			options: []ClientOption{
				func(opts *ClientOptions) {
					opts.resolverBuilder = func() (solver.Resolver, error) {
						return &stubResolver{resolveAddresses: []string{net.JoinHostPort("10.0.0.12", "27017")}}, nil

					}
				},
			},
			wantURI: "mongodb://admin:ZOp8SzWTYjjDRAtgSa3MgPeRQ8Zp3aZQ@10.0.0.12:27017/admin?authSource=admin",
		},
		{
			name:      "sanitized service name",
			rawTarget: " GRPC_HELLO.WORLD / mongo@main ",
			env:       map[string]string{dominionEnvironmentEnvKey: " Dev "},
			options: []ClientOption{
				func(opts *ClientOptions) {
					opts.resolverBuilder = func() (solver.Resolver, error) {
						return &stubResolver{resolveAddresses: []string{net.JoinHostPort("10.0.0.34", "27017")}}, nil

					}
				},
			},
			wantURI: "mongodb://admin:lJnPUcMYMLzwulQenzwZDlPgim1pydYM@10.0.0.34:27017/admin?authSource=admin",
		},
		{
			name:      "invalid target",
			rawTarget: "app",
			wantErr:   true,
			errSubstr: "want app/name"},
		{
			name:      "resolve error",
			rawTarget: "app/mongo-main",
			env:       map[string]string{dominionEnvironmentEnvKey: "dev"},
			options: []ClientOption{
				func(opts *ClientOptions) {
					opts.resolverBuilder = func() (solver.Resolver, error) {
						return &stubResolver{err: errors.New("resolve failed")}, nil

					}
				},
			},
			wantErr:   true,
			errSubstr: "resolve failed",
		},
		{
			name:      "client creation error",
			rawTarget: "app/mongo-main",
			env:       map[string]string{dominionEnvironmentEnvKey: "dev"},
			options: []ClientOption{
				func(opts *ClientOptions) {
					opts.resolverBuilder = func() (solver.Resolver, error) {
						return &stubResolver{resolveAddresses: []string{net.JoinHostPort("10.0.0.12", "27017")}}, nil

					}
				},
			},
			clientErr: errors.New("connect failed"),
			wantErr:   true,
			errSubstr: "connect failed",
		},
		{
			name:      "no ready endpoints",
			rawTarget: "app/mongo-main",
			env:       map[string]string{dominionEnvironmentEnvKey: "dev"},
			options: []ClientOption{
				func(opts *ClientOptions) {
					opts.resolverBuilder = func() (solver.Resolver, error) {
						return new(stubResolver), nil

					}
				},
			},
			wantErr:   true,
			errSubstr: "no ready endpoints found",
		},
		{
			name:      "with injected resolver option",
			rawTarget: "app/mongo-main",
			env:       map[string]string{dominionEnvironmentEnvKey: "dev"},
			options: []ClientOption{
				func(opts *ClientOptions) {
					opts.resolverBuilder = func() (solver.Resolver, error) {
						return &stubResolver{resolveAddresses: []string{net.JoinHostPort("10.0.0.12", "27017")}}, nil

					}
				},
			},
			wantURI: "mongodb://admin:ZOp8SzWTYjjDRAtgSa3MgPeRQ8Zp3aZQ@10.0.0.12:27017/admin?authSource=admin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalLookupEnv := lookupEnv
			originalNewMongoClient := newMongoClient
			var gotURI string
			lookupEnv = func(key string) (string, bool) {
				if tt.env == nil {
					return "", false
				}
				value, ok := tt.env[key]
				return value, ok
			}
			newMongoClient = func(uri string) (*mongodriver.Client, error) {
				gotURI = uri
				if tt.clientErr != nil {
					return nil, tt.clientErr
				}
				return new(mongodriver.Client), nil
			}
			t.Cleanup(func() {
				lookupEnv = originalLookupEnv
				newMongoClient = originalNewMongoClient
			})

			// when
			got, err := NewClient(tt.rawTarget, tt.options...)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NewClient(%q) expected error", tt.rawTarget)
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("NewClient(%q) error = %v, want substring %q", tt.rawTarget, err, tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewClient(%q) unexpected error: %v", tt.rawTarget, err)
			}
			if got == nil {
				t.Fatalf("NewClient(%q) = nil, want client", tt.rawTarget)
			}
			if gotURI != tt.wantURI {
				t.Fatalf("NewClient(%q) uri = %q, want %q", tt.rawTarget, gotURI, tt.wantURI)
			}
		})
	}
}

func TestWithK8sResolver(t *testing.T) {
	// given
	opts := defaultOptions()

	// when
	WithK8sResolver()(opts)

	// then
	if opts.resolverBuilder == nil {
		t.Fatal("WithK8sResolver() did not set resolverBuilder")
	}
}

func Test_buildMongoURI(t *testing.T) {
	originalLookupEnv := lookupEnv
	lookupEnv = func(key string) (string, bool) {
		env := map[string]string{dominionEnvironmentEnvKey: "dev"}
		value, ok := env[key]
		return value, ok
	}
	t.Cleanup(func() {
		lookupEnv = originalLookupEnv
	})

	tests := []struct {
		name   string
		target *solver.Target
		addr   string
		want   string
	}{
		{
			name:   "matches deploy naming and password rules",
			target: &solver.Target{App: "app", Service: "mongo-main"},
			addr:   net.JoinHostPort("10.0.0.12", "27017"),
			want:   "mongodb://admin:ZOp8SzWTYjjDRAtgSa3MgPeRQ8Zp3aZQ@10.0.0.12:27017/admin?authSource=admin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := buildMongoURI(tt.target, tt.addr)

			// then
			if got != tt.want {
				t.Fatalf("buildMongoURI(%#v, %q) = %q, want %q", tt.target, tt.addr, got, tt.want)
			}
		})
	}
}

func Test_envOrDefault(t *testing.T) {
	originalLookupEnv := lookupEnv
	t.Cleanup(func() {
		lookupEnv = originalLookupEnv
	})

	tests := []struct {
		name       string
		value      string
		present    bool
		defaultVal string
		want       string
	}{
		{name: "present and trimmed", value: " dev ", present: true, defaultVal: "default", want: "dev"},
		{name: "blank falls back", value: "   ", present: true, defaultVal: "default", want: "default"},
		{name: "missing falls back", present: false, defaultVal: "default", want: "default"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookupEnv = func(key string) (string, bool) {
				if !tt.present {
					return "", false
				}
				return tt.value, true
			}

			// when
			got := envOrDefault(dominionEnvironmentEnvKey, tt.defaultVal)

			// then
			if got != tt.want {
				t.Fatalf("envOrDefault(%q, %q) = %q, want %q", dominionEnvironmentEnvKey, tt.defaultVal, got, tt.want)
			}
		})
	}
}

func TestNewClientLogging(t *testing.T) {
	t.Run("success logs target and endpoint info", func(t *testing.T) {
		var buf bytes.Buffer
		handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
		logs.SetDefault(slog.New(handler))

		originalLookupEnv := lookupEnv
		originalNewMongoClient := newMongoClient
		lookupEnv = func(key string) (string, bool) { return "dev", true }
		newMongoClient = func(uri string) (*mongodriver.Client, error) {
			return new(mongodriver.Client), nil
		}
		t.Cleanup(func() {
			lookupEnv = originalLookupEnv
			newMongoClient = originalNewMongoClient
		})

		got, err := NewClient("app/mongo-main", withStubResolver(&stubResolver{
			resolveAddresses: []string{net.JoinHostPort("10.0.0.12", "27017")},
		}))
		if err != nil {
			t.Fatalf("NewClient() unexpected error: %v", err)
		}
		if got == nil {
			t.Fatal("NewClient() = nil, want client")
		}

		output := buf.String()
		if !strings.Contains(output, "mongo client initializing") {
			t.Errorf("log missing 'mongo client initializing'\n got:\n%s", output)
		}
		if !strings.Contains(output, "mongo endpoints resolved") {
			t.Errorf("log missing 'mongo endpoints resolved'\n got:\n%s", output)
		}
		if !strings.Contains(output, "mongo endpoint selected") {
			t.Errorf("log missing 'mongo endpoint selected'\n got:\n%s", output)
		}
		if !strings.Contains(output, "10.0.0.12") {
			t.Errorf("log missing resolved address\n got:\n%s", output)
		}
		if !strings.Contains(output, "address_count") {
			t.Errorf("log missing address_count\n got:\n%s", output)
		}
	})

	t.Run("client creation error logs safe fields, not URI", func(t *testing.T) {
		var buf bytes.Buffer
		handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
		logs.SetDefault(slog.New(handler))

		originalLookupEnv := lookupEnv
		originalNewMongoClient := newMongoClient
		lookupEnv = func(key string) (string, bool) { return "dev", true }
		newMongoClient = func(uri string) (*mongodriver.Client, error) {
			return nil, errors.New("connection refused")
		}
		t.Cleanup(func() {
			lookupEnv = originalLookupEnv
			newMongoClient = originalNewMongoClient
		})

		_, err := NewClient("app/mongo-main", withStubResolver(&stubResolver{
			resolveAddresses: []string{net.JoinHostPort("10.0.0.12", "27017")},
		}))
		if err == nil {
			t.Fatal("NewClient() expected error")
		}

		output := buf.String()
		if !strings.Contains(output, "mongo client failed") {
			t.Errorf("log missing 'mongo client failed'\n got:\n%s", output)
		}
		if !strings.Contains(output, "app/mongo-main") {
			t.Errorf("log missing safe target\n got:\n%s", output)
		}
		// FORBIDDEN: URI, password, or auth credentials
		if strings.Contains(output, "mongodb://") {
			t.Error("log contains mongodb URI with password — FORBIDDEN")
		}
		if strings.Contains(output, "ZOp8SzWTYjjDRAtgSa3MgPeRQ8Zp3aZQ") {
			t.Error("log contains password — FORBIDDEN")
		}
		if strings.Contains(output, "authSource") {
			t.Error("log contains auth credentials — FORBIDDEN")
		}
	})

	t.Run("invalid target produces no log output", func(t *testing.T) {
		var buf bytes.Buffer
		handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
		logs.SetDefault(slog.New(handler))

		_, err := NewClient("invalid-target")
		if err == nil {
			t.Fatal("NewClient() expected error")
		}

		output := buf.String()
		if output != "" {
			t.Errorf("expected no log output for invalid target, got:\n%s", output)
		}
	})
}
