package k8s

import (
	"reflect"
	"testing"
)

func TestLoadK8sConfig(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "namespace", value: LoadK8sConfig().Namespace},
		{name: "managed_by", value: LoadK8sConfig().ManagedBy},
		{name: "gateway.name", value: LoadK8sConfig().Gateway.Name},
		{name: "gateway.namespace", value: LoadK8sConfig().Gateway.Namespace},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value == "" {
				t.Fatalf("LoadK8sConfig() %s is empty", tt.name)
			}
		})
	}
}

func TestParseK8sConfig(t *testing.T) {
	tests := []struct {
		name    string
		raw     []byte
		wantErr bool
		want    *K8sConfig
	}{
		{
			name:    "invalid yaml",
			raw:     []byte("namespace: ["),
			wantErr: true,
		},
		{
			name:    "missing namespace",
			raw:     []byte("managed_by: x\ngateway:\n  name: y\n  namespace: z\n"),
			wantErr: true,
		},
		{
			name: "placeholder allowed",
			raw:  []byte("namespace: __NAMESPACE__\nmanaged_by: __MANAGED_BY__\ngateway:\n  name: __GATEWAY_NAME__\n  namespace: __GATEWAY_NAMESPACE__\n"),
			want: &K8sConfig{Namespace: "__NAMESPACE__"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseK8sConfig(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("parseK8sConfig() expected error")
				}
				return
			}

			if err != nil {
				t.Fatalf("parseK8sConfig() failed: %v", err)
			}
			if reflect.DeepEqual(tt.want, cfg) {
				t.Fatalf("namespace = %s, want %s", cfg, tt.want)
			}
		})
	}
}
