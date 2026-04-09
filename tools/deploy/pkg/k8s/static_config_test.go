package k8s

import (
	"os"
	"reflect"
	"sync"
	"testing"
)

func TestLoadK8sConfig(t *testing.T) {
	tests := []struct {
		name string
		want string
		got  string
	}{
		{name: "namespace", want: "default", got: LoadK8sConfig().Namespace},
		{name: "managed_by", want: "dominion.io", got: LoadK8sConfig().ManagedBy},
		{name: "gateway.name", want: "traefik-gateway", got: LoadK8sConfig().Gateway.Name},
		{name: "gateway.namespace", want: "ingress", got: LoadK8sConfig().Gateway.Namespace},
		{name: "tls.secret", want: "my-https-cert", got: LoadK8sConfig().TLS.Secret},
		{name: "tls.domain", want: "tls.liukexin.com", got: LoadK8sConfig().TLS.Domain},
		{name: "tls.ca_config_map.name", want: "my-ca.crt", got: LoadK8sConfig().TLS.CAConfigMap.Name},
		{name: "tls.ca_config_map.key", want: "ca.crt", got: LoadK8sConfig().TLS.CAConfigMap.Key},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("LoadK8sConfig() %s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestParseK8sConfig(t *testing.T) {
	tests := []struct {
		name    string
		raw     func(t *testing.T) []byte
		wantErr bool
		want    *K8sConfig
	}{
		{
			name:    "invalid yaml",
			raw:     func(t *testing.T) []byte { return []byte("namespace: [") },
			wantErr: true,
		},
		{
			name: "valid config without tls section remains compatible",
			raw: func(t *testing.T) []byte {
				return []byte("namespace: __NAMESPACE__\nmanaged_by: __MANAGED_BY__\ngateway:\n  name: __GATEWAY_NAME__\n  namespace: __GATEWAY_NAMESPACE__\n")
			},
			want: &K8sConfig{
				Namespace: "__NAMESPACE__",
				ManagedBy: "__MANAGED_BY__",
				Gateway: GatewayConfig{
					Name:      "__GATEWAY_NAME__",
					Namespace: "__GATEWAY_NAMESPACE__",
				},
			},
		},
		{
			name: "valid tls config",
			raw: func(t *testing.T) []byte {
				return mustReadStaticConfigTestdata(t, "static_config.tls.valid.yaml")
			},
			want: &K8sConfig{
				Namespace: "default",
				ManagedBy: "dominion",
				Gateway: GatewayConfig{
					Name:      "traefik-gateway",
					Namespace: "ingress",
				},
				TLS: TLSConfig{
					Secret:    "grpc-hello-world-service-tls",
					Domain:    "grpc-hello-world-service.default.svc.cluster.local",
					CAConfigMap: ConfigMapConfig{
						Name: "grpc-hello-world-service-ca",
						Key:  "ca.crt",
					},
				},
			},
		},
		{
			name: "missing secret",
			raw: func(t *testing.T) []byte {
				return mustReadStaticConfigTestdata(t, "static_config.tls.missing-secret-name.yaml")
			},
			wantErr: true,
		},
		{
			name: "missing domain",
			raw: func(t *testing.T) []byte {
				return mustReadStaticConfigTestdata(t, "static_config.tls.missing-server-name.yaml")
			},
			wantErr: true,
		},
		{
			name: "missing ca config map",
			raw: func(t *testing.T) []byte {
				return mustReadStaticConfigTestdata(t, "static_config.tls.missing-ca-file.yaml")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseK8sConfig(tt.raw(t))
			if tt.wantErr {
				if err == nil {
					t.Fatal("parseK8sConfig() expected error")
				}
				return
			}

			if err != nil {
				t.Fatalf("parseK8sConfig() failed: %v", err)
			}
			if !reflect.DeepEqual(tt.want, cfg) {
				t.Fatalf("parseK8sConfig() = %#v, want %#v", cfg, tt.want)
			}
		})
	}
}

func mustReadStaticConfigTestdata(t *testing.T, name string) []byte {
	t.Helper()

	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("ReadFile(%q) failed: %v", name, err)
	}

	return raw
}

func stubLoadK8sConfig(t *testing.T, cfg *K8sConfig) {
	t.Helper()

	originalLoadFunc := loadK8sConfigFunc
	originalReadFile := readStaticConfigFile
	originalLoadedConfig := loadedK8sConfig
	t.Cleanup(func() {
		loadK8sConfigFunc = originalLoadFunc
		readStaticConfigFile = originalReadFile
		loadedK8sConfig = originalLoadedConfig
		loadK8sConfigOnce = sync.Once{}
	})

	loadK8sConfigOnce = sync.Once{}
	loadedK8sConfig = nil
	loadK8sConfigFunc = func() *K8sConfig {
		return cfg
	}
}
