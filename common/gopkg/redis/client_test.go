package redis

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"dominion/common/gopkg/solver"

	goredis "github.com/redis/go-redis/v9"
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

		options      []ClientOption
		wantAddr     string
		wantPassword string
		wantErr      bool
		errSubstr    string
	}{
		{
			name:      "resolves first endpoint and derives instance vector",
			rawTarget: "myapp/myredis",
			env:       map[string]string{dominionEnvironmentEnvKey: "dev"},
			options: []ClientOption{
				withStubResolver(&stubResolver{resolveAddresses: []string{
					net.JoinHostPort("10.0.0.12", "6379"),
					net.JoinHostPort("10.0.0.13", "6379"),
				}}),
			},
			wantAddr:     "10.0.0.12:6379",
			wantPassword: "k1OTVRTOdkt9XeuhOVEk5yR82ZZqnjxt",
		},
		{
			name:      "trims target segments and environment",
			rawTarget: " myapp / myredis ",
			env:       map[string]string{dominionEnvironmentEnvKey: " dev "},
			options: []ClientOption{
				withStubResolver(&stubResolver{resolveAddresses: []string{net.JoinHostPort("10.0.0.12", "6379")}}),
			},
			wantAddr:     "10.0.0.12:6379",
			wantPassword: "k1OTVRTOdkt9XeuhOVEk5yR82ZZqnjxt",
		},
		{
			name:      "missing environment falls back to default",
			rawTarget: "myapp/myredis",
			options: []ClientOption{
				withStubResolver(&stubResolver{resolveAddresses: []string{net.JoinHostPort("10.0.0.12", "6379")}}),
			},
			wantAddr:     "10.0.0.12:6379",
			wantPassword: "FF53SVZiUNQCFp1k4B2c0fp4P4NCM729",
		},
		{
			name:      "invalid target",
			rawTarget: "myapp",
			wantErr:   true,
			errSubstr: "want app/name",
		},
		{
			name:      "empty service segment",
			rawTarget: "myapp/",
			wantErr:   true,
			errSubstr: "want app/name",
		},
		{
			name:      "resolve error",
			rawTarget: "myapp/myredis",
			options: []ClientOption{
				withStubResolver(&stubResolver{err: errors.New("resolve failed")}),
			},
			wantErr:   true,
			errSubstr: "resolve failed",
		},
		{
			name:      "no ready endpoints",
			rawTarget: "myapp/myredis",
			options: []ClientOption{
				withStubResolver(new(stubResolver)),
			},
			wantErr:   true,
			errSubstr: "no ready endpoints found",
		},
		{
			name:      "resolver builder error",
			rawTarget: "myapp/myredis",
			options: []ClientOption{
				withStubResolver(nil),
			},
			wantErr:   true,
			errSubstr: "missing resolver",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalLookupEnv := lookupEnv
			originalNewRedisClient := newRedisClient
			var gotOptions *goredis.Options
			lookupEnv = func(key string) (string, bool) {
				if tt.env == nil {
					return "", false
				}
				value, ok := tt.env[key]
				return value, ok
			}
			newRedisClient = func(options *goredis.Options) *goredis.Client {
				gotOptions = options
				return new(goredis.Client)
			}
			t.Cleanup(func() {
				lookupEnv = originalLookupEnv
				newRedisClient = originalNewRedisClient
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
			if gotOptions == nil {
				t.Fatal("NewClient() did not construct client options")
			}
			if gotOptions.Addr != tt.wantAddr {
				t.Fatalf("NewClient(%q) Addr = %q, want %q", tt.rawTarget, gotOptions.Addr, tt.wantAddr)
			}
			if gotOptions.Password != tt.wantPassword {
				t.Fatalf("NewClient(%q) Password = %q, want %q", tt.rawTarget, gotOptions.Password, tt.wantPassword)
			}
		})
	}
}

func Test_parseRedisTarget(t *testing.T) {
	tests := []struct {
		name        string
		rawTarget   string
		wantApp     string
		wantService string
		wantPort    int
		wantErr     bool
	}{
		{name: "app and service", rawTarget: "myapp/myredis", wantApp: "myapp", wantService: "myredis", wantPort: defaultRedisPort},
		{name: "trims surrounding whitespace", rawTarget: " myapp / myredis ", wantApp: "myapp", wantService: "myredis", wantPort: defaultRedisPort},
		{name: "missing slash", rawTarget: "myapp", wantErr: true},
		{name: "empty app", rawTarget: "/myredis", wantErr: true},
		{name: "empty service", rawTarget: "myapp/", wantErr: true},
		{name: "nested service path", rawTarget: "myapp/a/b", wantErr: true},
		{name: "blank", rawTarget: "   ", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, err := parseRedisTarget(tt.rawTarget)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseRedisTarget(%q) expected error", tt.rawTarget)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRedisTarget(%q) unexpected error: %v", tt.rawTarget, err)
			}
			if got.App != tt.wantApp || got.Service != tt.wantService {
				t.Fatalf("parseRedisTarget(%q) = %q/%q, want %q/%q", tt.rawTarget, got.App, got.Service, tt.wantApp, tt.wantService)
			}
			if got.PortSelector.Numeric() != tt.wantPort {
				t.Fatalf("parseRedisTarget(%q) port = %d, want %d", tt.rawTarget, got.PortSelector.Numeric(), tt.wantPort)
			}
		})
	}
}

func Test_environmentName(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		present bool
		want    string
	}{
		{name: "present and trimmed", value: " dev ", present: true, want: "dev"},
		{name: "blank falls back", value: "   ", present: true, want: defaultEnvironmentName},
		{name: "missing falls back", present: false, want: defaultEnvironmentName},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalLookupEnv := lookupEnv
			lookupEnv = func(key string) (string, bool) {
				if !tt.present {
					return "", false
				}
				return tt.value, true
			}
			t.Cleanup(func() {
				lookupEnv = originalLookupEnv
			})

			// when
			got := environmentName()

			// then
			if got != tt.want {
				t.Fatalf("environmentName() = %q, want %q", got, tt.want)
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
