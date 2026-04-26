package testtool

import (
	"os"
	"testing"
)

func TestEnv(t *testing.T) {
	tests := []struct {
		name string
		val  string
		ok   bool
		want string
	}{
		{name: "success", val: "dev", ok: true, want: "dev"},
		{name: "missing", ok: false},
		{name: "blank", val: "   ", ok: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			givenLookupEnv(t, map[string]string{EnvKey(): tt.val}, tt.ok)

			got, err := Env()
			if tt.want != "" {
				if err != nil {
					t.Fatalf("Env() unexpected error: %v", err)
				}
				if got != tt.want {
					t.Fatalf("Env() = %q, want %q", got, tt.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("Env() expected error")
			}
		})
	}
}

func TestMustEnv(t *testing.T) {
	tests := []struct {
		name    string
		val     string
		ok      bool
		want    string
		wantPan bool
	}{
		{name: "success", val: "qa", ok: true, want: "qa"},
		{name: "missing", ok: false, wantPan: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			givenLookupEnv(t, map[string]string{EnvKey(): tt.val}, tt.ok)

			if tt.wantPan {
				defer func() {
					if recover() == nil {
						t.Fatalf("MustEnv() expected panic")
					}
				}()
			}

			got := MustEnv()
			if tt.wantPan {
				return
			}
			if got != tt.want {
				t.Fatalf("MustEnv() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		endpoint string
		val      string
		ok       bool
		want     string
		wantErr  bool
	}{
		{name: "success", protocol: "http", endpoint: "public", val: "https://example.test", ok: true, want: "https://example.test"},
		{name: "missing", protocol: "http", endpoint: "public", ok: false, wantErr: true},
		{name: "invalid leading digit", protocol: "http", endpoint: "2bad", val: "https://example.test", ok: true, wantErr: true},
		{name: "invalid hyphen", protocol: "http", endpoint: "bad-api", val: "https://example.test", ok: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			givenLookupEnv(t, map[string]string{EndpointKey(tt.protocol, tt.endpoint): tt.val}, tt.ok)

			got, err := Endpoint(tt.protocol, tt.endpoint)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Endpoint() expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Endpoint() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Endpoint() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMustEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		endpoint string
		val      string
		ok       bool
		want     string
		wantPan  bool
	}{
		{name: "success", protocol: "http", endpoint: "public", val: "https://example.test", ok: true, want: "https://example.test"},
		{name: "missing", protocol: "http", endpoint: "public", ok: false, wantPan: true},
		{name: "invalid name", protocol: "http", endpoint: "bad-api", val: "https://example.test", ok: true, wantPan: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			givenLookupEnv(t, map[string]string{EndpointKey(tt.protocol, tt.endpoint): tt.val}, tt.ok)

			if tt.wantPan {
				defer func() {
					if recover() == nil {
						t.Fatalf("MustEndpoint() expected panic")
					}
				}()
			}

			got := MustEndpoint(tt.protocol, tt.endpoint)
			if tt.wantPan {
				return
			}
			if got != tt.want {
				t.Fatalf("MustEndpoint() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEndpointKey(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		endpoint string
		want     string
	}{
		{name: "format", protocol: "http", endpoint: "public", want: "TESTTOOL_ENDPOINT_HTTP_PUBLIC"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EndpointKey(tt.protocol, tt.endpoint)
			if got != tt.want {
				t.Fatalf("EndpointKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func givenLookupEnv(t *testing.T, values map[string]string, ok bool) {
	t.Helper()

	lookupEnv = func(key string) (string, bool) {
		if !ok {
			return "", false
		}
		value, found := values[key]
		if !found {
			return "", false
		}
		return value, true
	}
	t.Cleanup(func() {
		lookupEnv = os.LookupEnv
	})
}
