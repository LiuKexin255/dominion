package k8s

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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

func Test_newInstanceObjectName(t *testing.T) {
	tests := []struct {
		name          string
		kind          WorkloadKind
		fullEnvName   string
		serviceName   string
		instanceIndex int
		wantSuffix    string
	}{
		{
			name:          "normal instance",
			kind:          WorkloadKindInstanceService,
			fullEnvName:   "grpc-hello-world",
			serviceName:   "gateway",
			instanceIndex: 0,
			wantSuffix:    "-0",
		},
		{
			name:          "instance index greater than 9",
			kind:          WorkloadKindInstanceRoute,
			fullEnvName:   "grpc-hello-world",
			serviceName:   "gateway",
			instanceIndex: 12,
			wantSuffix:    "-12",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newInstanceObjectName(tt.kind, tt.fullEnvName, tt.serviceName, tt.instanceIndex)
			if !strings.HasSuffix(got, tt.wantSuffix) {
				t.Fatalf("newInstanceObjectName() = %q, want suffix %q", got, tt.wantSuffix)
			}
			if len(got) > maxK8sResourceNameSize {
				t.Fatalf("newInstanceObjectName() length = %d, want <= %d", len(got), maxK8sResourceNameSize)
			}
			base := newObjectName(tt.kind, tt.fullEnvName, tt.serviceName)
			suffix := fmt.Sprintf("-%d", tt.instanceIndex)
			maxBase := maxK8sResourceNameSize - len(suffix)
			if len(base) <= maxBase {
				want := base + suffix
				if got != want {
					t.Fatalf("newInstanceObjectName() = %q, want %q", got, want)
				}
			}
		})
	}
}

func Test_newInstanceObjectName_MaxLength(t *testing.T) {
	got := newInstanceObjectName(
		WorkloadKindInstanceService,
		strings.Repeat("a", 25),
		strings.Repeat("b", 25),
		99,
	)
	if len(got) > maxK8sResourceNameSize {
		t.Fatalf("newInstanceObjectName() length = %d, want <= %d", len(got), maxK8sResourceNameSize)
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
