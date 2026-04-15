package k8s

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func Test_newObjectName(t *testing.T) {
	tests := []struct {
		name        string
		kind        WorkloadKind
		fullEnvName string
		serviceName string
		want        string
	}{
		{
			name:        "normal",
			kind:        WorkloadKindDeployment,
			fullEnvName: "grpc-hello-world",
			serviceName: "gateway",
			want:        "dp-grpc-hello-world-gateway-" + shortNameHash("grpc-hello-world"),
		},
		{
			name:        "normalize and sanitize",
			kind:        WorkloadKindService,
			fullEnvName: "GRPC_HELLO_WORLD",
			serviceName: "gateway@v1",
			want:        "svc-grpc-hello-world-gateway-v1-" + shortNameHash("GRPC_HELLO_WORLD"),
		},
		{
			name:        "only kind when all parts empty",
			kind:        WorkloadKindHTTPRoute,
			fullEnvName: "",
			serviceName: "",
			want:        "route-" + shortNameHash(""),
		},
		{
			name:        "fallback to unknown kind",
			kind:        "",
			fullEnvName: "app",
			serviceName: "svc",
			want:        "unknown-app-svc-" + shortNameHash("app"),
		},
		{
			name:        "skip empty normalized part",
			kind:        WorkloadKindDeployment,
			fullEnvName: "---dev",
			serviceName: "svc",
			want:        "dp-dev-svc-" + shortNameHash("---dev"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newObjectName(tt.kind, tt.fullEnvName, tt.serviceName)
			if got != tt.want {
				t.Fatalf("newObjectName() = %q, want %q", got, tt.want)
			}
			if len(got) > maxK8sResourceNameSize {
				t.Fatalf("newObjectName() length = %d, want <= %d", len(got), maxK8sResourceNameSize)
			}
		})
	}
}

func Test_newObjectName_MaxLength(t *testing.T) {
	got := newObjectName(
		WorkloadKindDeployment,
		strings.Repeat("a", 25),
		strings.Repeat("b", 25),
	)
	if len(got) > maxK8sResourceNameSize {
		t.Fatalf("newObjectName() length = %d, want <= %d", len(got), maxK8sResourceNameSize)
	}
	if len(got) != maxK8sResourceNameSize {
		t.Fatalf("newObjectName() length = %d, want %d", len(got), maxK8sResourceNameSize)
	}
}

func Test_shortNameHash(t *testing.T) {
	input := "GRPC_HELLO_WORLD"
	sum := sha256.Sum256([]byte(strings.TrimSpace(input)))
	want := hex.EncodeToString(sum[:4])
	if got := shortNameHash(input); got != want {
		t.Fatalf("shortNameHash() = %q, want %q", got, want)
	}
	if got := shortNameHash(input); got != want {
		t.Fatalf("shortNameHash() not stable: %q, want %q", got, want)
	}
}
