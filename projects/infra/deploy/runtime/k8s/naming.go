package k8s

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

var (
	// nonDNSLabel 匹配名称中不符合 DNS label 规范的字符。
	nonDNSLabel = regexp.MustCompile(`[^a-z0-9-]+`)
)

// WorkloadKind 表示不同 Kubernetes workload 对象的类型前缀。
type WorkloadKind string

const (
	// WorkloadEmpty 类型为空。
	WorkloadEmpty = ""
	// WorkloadUnknown 表示未知类型前缀。
	WorkloadUnknown WorkloadKind = "unknown"
	// WorkloadKindDeployment 表示 Deployment 类型前缀。
	WorkloadKindDeployment WorkloadKind = "dp"
	// WorkloadKindService 表示 Service 类型前缀。
	WorkloadKindService WorkloadKind = "svc"
	// WorkloadKindHTTPRoute 表示 HTTPRoute 类型前缀。
	WorkloadKindHTTPRoute WorkloadKind = "route"
	// WorkloadKindMongoDB 表示 MongoDB 类型前缀。
	WorkloadKindMongoDB WorkloadKind = "mongo"
	// WorkloadKindPVC 表示 PVC 类型前缀。
	WorkloadKindPVC WorkloadKind = "pvc"
	// WorkloadKindSecret 表示 Secret 类型前缀。
	WorkloadKindSecret WorkloadKind = "secret"

	maxK8sResourceNameSize = 63
)

func newObjectName(kind WorkloadKind, fullEnvName string, serviceName string) string {
	if kind == WorkloadEmpty {
		kind = WorkloadUnknown
	}
	parts := []string{string(kind), fullEnvName, serviceName, shortNameHash(fullEnvName)}
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		part = sanitizeNamePart(part)
		if part != "" {
			normalized = append(normalized, part)
		}
	}

	return strings.Join(normalized, "-")
}

func sanitizeNamePart(part string) string {
	part = strings.TrimSpace(strings.ToLower(part))
	part = nonDNSLabel.ReplaceAllString(part, "-")
	part = strings.Trim(part, "-")
	return part
}

func shortNameHash(fullEnvName string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(fullEnvName)))
	return hex.EncodeToString(sum[:4])
}
