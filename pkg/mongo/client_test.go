package mongo

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"dominion/pkg/solver"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
)

type stubResolver struct {
	lookupServiceName string
	resolveAddresses  []string
	err               error
}

func (s *stubResolver) Lookup(ctx context.Context, target *solver.Target) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.lookupServiceName, nil
}

func (s *stubResolver) ResolveEndpoints(ctx context.Context, target *solver.Target, serviceName string) ([]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.resolveAddresses, nil
}

func (s *stubResolver) Resolve(ctx context.Context, target *solver.Target) ([]string, error) {
	serviceName, err := s.Lookup(ctx, target)
	if err != nil {
		return nil, err
	}
	return s.ResolveEndpoints(ctx, target, serviceName)
}

func TestNewClient(t *testing.T) {
	tests := []struct {
		name      string
		rawTarget string
		env       map[string]string
		resolver  solver.Resolver
		clientErr error
		wantURI   string
		wantErr   bool
		errSubstr string
	}{
		{
			name:      "success",
			rawTarget: "app/mongo-main",
			env:       map[string]string{dominionEnvironmentEnvKey: "dev"},
			resolver:  &stubResolver{lookupServiceName: "svc-dev-mongo-main-bfc75601", resolveAddresses: []string{net.JoinHostPort("10.0.0.12", "27017")}},
			wantURI:   "mongodb://admin:ZOp8SzWTYjjDRAtgSa3MgPeRQ8Zp3aZQ@10.0.0.12:27017/admin?authSource=admin",
		},
		{
			name:      "sanitized service name",
			rawTarget: " GRPC_HELLO.WORLD / mongo@main ",
			env:       map[string]string{dominionEnvironmentEnvKey: " Dev "},
			resolver:  &stubResolver{lookupServiceName: "svc-dev-mongo-main-395bea0a", resolveAddresses: []string{net.JoinHostPort("10.0.0.34", "27017")}},
			wantURI:   "mongodb://admin:lJnPUcMYMLzwulQenzwZDlPgim1pydYM@10.0.0.34:27017/admin?authSource=admin",
		},
		{name: "invalid target", rawTarget: "app", wantErr: true, errSubstr: "want app/name"},
		{name: "resolve error", rawTarget: "app/mongo-main", env: map[string]string{dominionEnvironmentEnvKey: "dev"}, resolver: &stubResolver{err: errors.New("resolve failed")}, wantErr: true, errSubstr: "resolve failed"},
		{name: "client creation error", rawTarget: "app/mongo-main", env: map[string]string{dominionEnvironmentEnvKey: "dev"}, resolver: &stubResolver{lookupServiceName: "svc-dev-mongo-main-bfc75601", resolveAddresses: []string{net.JoinHostPort("10.0.0.12", "27017")}}, clientErr: errors.New("connect failed"), wantErr: true, errSubstr: "connect failed"},
		{name: "no ready endpoints", rawTarget: "app/mongo-main", env: map[string]string{dominionEnvironmentEnvKey: "dev"}, resolver: &stubResolver{lookupServiceName: "svc-dev-mongo-main-bfc75601"}, wantErr: true, errSubstr: "no ready endpoints found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalLookupEnv := lookupEnv
			originalNewResolver := newResolver
			originalNewMongoClient := newMongoClient
			var gotURI string
			lookupEnv = func(key string) (string, bool) {
				if tt.env == nil {
					return "", false
				}
				value, ok := tt.env[key]
				return value, ok
			}
			newResolver = func() (solver.Resolver, error) {
				if tt.resolver == nil {
					return nil, errors.New("missing resolver")
				}
				return tt.resolver, nil
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
				newResolver = originalNewResolver
				newMongoClient = originalNewMongoClient
			})

			// when
			got, err := NewClient(tt.rawTarget)

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

func Test_lookupEnvOrDefault(t *testing.T) {
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
			got := lookupEnvOrDefault(dominionEnvironmentEnvKey, tt.defaultVal)

			// then
			if got != tt.want {
				t.Fatalf("lookupEnvOrDefault(%q, %q) = %q, want %q", dominionEnvironmentEnvKey, tt.defaultVal, got, tt.want)
			}
		})
	}
}
