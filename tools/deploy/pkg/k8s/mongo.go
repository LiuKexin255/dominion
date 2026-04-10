package k8s

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"strings"

	"dominion/tools/deploy/pkg/config"
)

const (
	mongoPasswordHMACKey  = "dominion-mongo-stable-password"
	mongoPasswordAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	mongoPasswordMinLen   = 24
	mongoPasswordJoiner   = "\x00"
	mongoResourceDesc     = "mongo builtin service"
)

// MongoDBWorkload 描述 MongoDB workload 生成所需字段。
type MongoDBWorkload struct {
	ServiceName     string
	EnvironmentName string
	App             string
	DominionApp     string
	Desc            string
	ProfileName     string
	Persistence     config.DeployInfraPersistence
}

// ResourceName 返回 MongoDB workload 对应的资源名。
// 若 w 为空，则返回空字符串。
func (w *MongoDBWorkload) ResourceName() string {
	if w == nil {
		return ""
	}

	return newObjectName(WorkloadKindMongoDB, w.App, w.DominionApp, w.ServiceName, w.EnvironmentName)
}

// Validate 校验 MongoDB workload 字段是否合法。
func (w *MongoDBWorkload) Validate() error {
	if w == nil {
		return fmt.Errorf("mongo workload 为空")
	}
	if strings.TrimSpace(w.ServiceName) == "" {
		return fmt.Errorf("mongo workload 缺少 service name")
	}
	if strings.TrimSpace(w.EnvironmentName) == "" {
		return fmt.Errorf("mongo workload 缺少 environment name")
	}
	if strings.TrimSpace(w.App) == "" {
		return fmt.Errorf("mongo workload 缺少 app")
	}
	if strings.TrimSpace(w.Desc) == "" {
		return fmt.Errorf("mongo workload 缺少 desc")
	}
	if len(w.ResourceName()) > maxK8sResourceNameSize {
		return fmt.Errorf("mongo workload name 超过 63 字符")
	}
	if strings.TrimSpace(w.ProfileName) == "" {
		return fmt.Errorf("mongo workload 缺少 profile name")
	}

	return nil
}

// MongoDBSecretWorkload 描述 MongoDB Secret workload 生成所需字段。
type MongoDBSecretWorkload struct {
	ServiceName     string
	EnvironmentName string
	App             string
	DominionApp     string
}

// ResourceName 返回 MongoDB Secret workload 对应的资源名。
// 若 w 为空，则返回空字符串。
func (w *MongoDBSecretWorkload) ResourceName() string {
	if w == nil {
		return ""
	}

	return newObjectName(WorkloadKindSecret, w.App, w.DominionApp, w.ServiceName, w.EnvironmentName)
}

// Validate 校验 MongoDB Secret workload 字段是否合法。
func (w *MongoDBSecretWorkload) Validate() error {
	if w == nil {
		return fmt.Errorf("mongo secret workload 为空")
	}
	if strings.TrimSpace(w.ServiceName) == "" {
		return fmt.Errorf("mongo secret workload 缺少 service name")
	}
	if strings.TrimSpace(w.EnvironmentName) == "" {
		return fmt.Errorf("mongo secret workload 缺少 environment name")
	}
	if strings.TrimSpace(w.App) == "" {
		return fmt.Errorf("mongo secret workload 缺少 app")
	}
	if len(w.ResourceName()) > maxK8sResourceNameSize {
		return fmt.Errorf("mongo secret workload name 超过 63 字符")
	}

	return nil
}

// MongoDBPVCWorkload 描述 MongoDB PVC workload 生成所需字段。
type MongoDBPVCWorkload struct {
	ServiceName     string
	EnvironmentName string
	App             string
	DominionApp     string
}

// ResourceName 返回 MongoDB PVC workload 对应的资源名。
// 若 w 为空，则返回空字符串。
func (w *MongoDBPVCWorkload) ResourceName() string {
	if w == nil {
		return ""
	}

	return newObjectName(WorkloadKindPVC, w.App, w.DominionApp, w.ServiceName, w.EnvironmentName)
}

// Validate 校验 MongoDB PVC workload 字段是否合法。
func (w *MongoDBPVCWorkload) Validate() error {
	if w == nil {
		return fmt.Errorf("mongo pvc workload 为空")
	}
	if strings.TrimSpace(w.ServiceName) == "" {
		return fmt.Errorf("mongo pvc workload 缺少 service name")
	}
	if strings.TrimSpace(w.EnvironmentName) == "" {
		return fmt.Errorf("mongo pvc workload 缺少 environment name")
	}
	if strings.TrimSpace(w.App) == "" {
		return fmt.Errorf("mongo pvc workload 缺少 app")
	}
	if len(w.ResourceName()) > maxK8sResourceNameSize {
		return fmt.Errorf("mongo pvc workload name 超过 63 字符")
	}

	return nil
}

func generateStablePassword(inputs ...string) string {
	normalized := make([]string, 0, len(inputs))
	for _, input := range inputs {
		normalized = append(normalized, strings.TrimSpace(input))
	}

	mac := hmac.New(sha256.New, []byte(mongoPasswordHMACKey))
	_, _ = mac.Write([]byte(strings.Join(normalized, mongoPasswordJoiner)))
	sum := mac.Sum(nil)

	encoded := make([]byte, 0, len(sum))
	for _, b := range sum {
		encoded = append(encoded, mongoPasswordAlphabet[int(b)%len(mongoPasswordAlphabet)])
	}
	if len(encoded) >= mongoPasswordMinLen {
		return string(encoded)
	}

	for len(encoded) < mongoPasswordMinLen {
		for _, b := range sum {
			encoded = append(encoded, mongoPasswordAlphabet[int(b)%len(mongoPasswordAlphabet)])
			if len(encoded) >= mongoPasswordMinLen {
				break
			}
		}
	}

	return string(encoded)
}

func newMongoDBWorkload(infra config.DeployInfra, envName string, dominionApp string) (*MongoDBWorkload, error) {
	profile := LoadMongoProfile(infra.Profile)
	if profile == nil {
		return nil, fmt.Errorf("mongo workload profile %s 不存在", strings.TrimSpace(infra.Profile))
	}

	w := new(MongoDBWorkload)
	w.ServiceName = strings.TrimSpace(infra.Name)
	w.EnvironmentName = strings.TrimSpace(envName)
	w.App = strings.TrimSpace(dominionApp)
	w.DominionApp = strings.TrimSpace(dominionApp)
	w.Desc = mongoResourceDesc
	w.ProfileName = strings.TrimSpace(infra.Profile)
	w.Persistence = infra.Persistence

	if err := w.Validate(); err != nil {
		return nil, err
	}

	return w, nil
}
