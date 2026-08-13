package domain

import (
	"errors"
	"fmt"
	"regexp"
)

// HTTPPathRuleType defines the type of HTTP path matching rule.
type HTTPPathRuleType int

const (
	// HTTPPathRuleTypeUnspecified indicates no path rule type has been set.
	HTTPPathRuleTypeUnspecified HTTPPathRuleType = 0
	// HTTPPathRuleTypePathPrefix matches requests by path prefix.
	HTTPPathRuleTypePathPrefix HTTPPathRuleType = 1
)

// WorkloadKind defines whether a workload is stateless or stateful.
type WorkloadKind int

const (
	// WorkloadKindStateless indicates a stateless workload (default).
	WorkloadKindStateless WorkloadKind = 0
	// WorkloadKindStateful indicates a stateful workload.
	WorkloadKindStateful WorkloadKind = 1
)

// ArtifactPortSpec describes a single port exposed by an artifact.
type ArtifactPortSpec struct {
	Name string
	Port int32
}

// SecretBinding maps a logical secret name to a Kubernetes Secret key.
type SecretBinding struct {
	LogicalName string
	SecretName  string
	Key         string
}

// ConfigBlock 携带一个配置块及其条目，对齐 service.yaml 顶层 configs[]
// （见 specs/045-deploy-config/data-model.md §4）。
type ConfigBlock struct {
	Block   string
	Entries []*ConfigEntry
}

// Validate 校验 ConfigBlock 自身与每个条目（VR-CB-5/VR-CE-1，
// specs/045-deploy-config/data-model.md §4）。
func (b *ConfigBlock) Validate() error {
	var errs []error

	if b.Block == "" {
		errs = append(errs, errors.New("block is required"))
	}
	if len(b.Entries) == 0 {
		errs = append(errs, errors.New("entries must not be empty"))
	}
	for i, e := range b.Entries {
		if err := e.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("entries[%d]: %w", i, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%w: %w", ErrInvalidSpec, errors.Join(errs...))
	}
	return nil
}

// ConfigEntry 是配置块内的一个数据条目，block 归属由父节点 ConfigBlock 表达。
type ConfigEntry struct {
	Key   string
	Type  string
	Value string
}

// Validate checks that the ConfigEntry fields are non-empty (VR-CE-1，
// specs/045-deploy-config/data-model.md §4)。
func (e *ConfigEntry) Validate() error {
	var errs []error

	if e.Key == "" {
		errs = append(errs, errors.New("key is required"))
	}
	if e.Type == "" {
		errs = append(errs, errors.New("type is required"))
	}
	if e.Value == "" {
		errs = append(errs, errors.New("value is required"))
	}

	if len(errs) > 0 {
		return fmt.Errorf("%w: %w", ErrInvalidSpec, errors.Join(errs...))
	}
	return nil
}

// cloneConfigBlocks 深拷贝 ConfigBlock 列表（外层逐字段 + 内层逐条目元素拷贝），
// nil/空输入返回 nil。形态与 storage.configBlocksToMongo
// （projects/infra/deploy/storage/mongo.go）逐行对称——domain clone / storage 双向
// / handler 双向统一为逐字段构造风格（见 specs/045-deploy-config/contracts/proto.md
// §5"风格统一决策"）。
// 新增 ConfigBlock/ConfigEntry 字段时 MUST 同步以下映射点（任一遗漏即静默丢字段，
// 由"测试防线"捕获）：
//   - cloneConfigBlocks（projects/infra/deploy/domain/spec.go，本函数）
//   - configBlocksToMongo / configBlocksFromMongo（projects/infra/deploy/storage/mongo.go）
//   - toProtoConfigBlocks / fromProtoConfigBlocks（projects/infra/deploy/handler.go）
func cloneConfigBlocks(blocks []*ConfigBlock) []*ConfigBlock {
	if len(blocks) == 0 {
		return nil
	}
	result := make([]*ConfigBlock, len(blocks))
	for i, cb := range blocks {
		block := &ConfigBlock{Block: cb.Block}
		if len(cb.Entries) > 0 {
			block.Entries = make([]*ConfigEntry, len(cb.Entries))
			for j, ce := range cb.Entries {
				block.Entries[j] = &ConfigEntry{
					Key:   ce.Key,
					Type:  ce.Type,
					Value: ce.Value,
				}
			}
		}
		result[i] = block
	}
	return result
}

// Validate checks that the SecretBinding fields are non-empty.
func (b *SecretBinding) Validate() error {
	var errs []error

	if b.LogicalName == "" {
		errs = append(errs, errors.New("logical_name is required"))
	}
	if b.SecretName == "" {
		errs = append(errs, errors.New("secret_name is required"))
	}
	if b.Key == "" {
		errs = append(errs, errors.New("key is required"))
	}

	if len(errs) > 0 {
		return fmt.Errorf("%w: %w", ErrInvalidSpec, errors.Join(errs...))
	}
	return nil
}

// ArtifactHTTPSpec describes the desired HTTP routing state for an artifact.
type ArtifactHTTPSpec struct {
	Hostnames []string
	Matches   []HTTPRouteRule
}

// envKeyPattern defines the format for valid environment variable keys.
// Keys must start with a letter or underscore, followed by letters, digits, or underscores.
var envKeyPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// ArtifactSpec describes the desired state of a deployable application artifact.
type ArtifactSpec struct {
	Name           string
	App            string
	Image          string
	Ports          []ArtifactPortSpec
	Replicas       int32
	TLSEnabled     bool
	OSSEnabled     bool
	WorkloadKind   WorkloadKind
	HTTP           *ArtifactHTTPSpec
	Env            map[string]string
	SecretBindings []*SecretBinding
	ConfigBlocks   []*ConfigBlock // 期望状态层级的配置块（块包含条目）
}

// InfraPersistenceSpec describes infrastructure persistence settings.
type InfraPersistenceSpec struct {
	Enabled  bool
	Capacity string
}

// InfraSpec describes the desired state of an infrastructure resource.
type InfraSpec struct {
	Resource    string
	Profile     string
	Name        string
	App         string
	Persistence InfraPersistenceSpec
}

// HTTPPathRule describes an HTTP path matching rule.
type HTTPPathRule struct {
	Type  HTTPPathRuleType
	Value string
}

// HTTPRouteRule describes a single routing rule with a backend and path match.
type HTTPRouteRule struct {
	Backend string // 后端的端口名
	Path    HTTPPathRule
}

// Validate checks that the ArtifactSpec contains valid field values.
// It verifies that name, app, and image are non-empty, each port is in
// the range 1-65535, replicas is non-negative, and nested HTTP settings are valid.
func (s *ArtifactSpec) Validate() error {
	var errs []error

	if s.Name == "" {
		errs = append(errs, errors.New("name must not be empty"))
	}
	if s.App == "" {
		errs = append(errs, errors.New("app must not be empty"))
	}
	if s.Image == "" {
		errs = append(errs, errors.New("image must not be empty"))
	}
	for i, p := range s.Ports {
		if p.Port < 1 || p.Port > 65535 {
			errs = append(errs, fmt.Errorf("ports[%d].port must be between 1 and 65535, got %d", i, p.Port))
		}
	}
	if s.Replicas < 0 {
		errs = append(errs, errors.New("replicas must be non-negative"))
	}
	if s.WorkloadKind == WorkloadKindStateful && s.HTTP != nil {
		errs = append(errs, errors.New("invalid spec: http is only supported for stateless workloads"))
	}
	for key := range s.Env {
		if key == "" {
			errs = append(errs, errors.New("env key must not be empty"))
			continue
		}
		if !envKeyPattern.MatchString(key) {
			errs = append(errs, fmt.Errorf("env key %q must match pattern %s", key, envKeyPattern))
		}
	}
	if s.HTTP != nil {
		if err := s.HTTP.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("http: %w", err))
		}
	}
	for i, b := range s.SecretBindings {
		if err := b.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("secret_bindings[%d]: %w", i, err))
		}
	}
	seen := make(map[string]int, len(s.SecretBindings))
	for i, b := range s.SecretBindings {
		if b.LogicalName == "" {
			continue
		}
		if _, ok := seen[b.LogicalName]; ok {
			errs = append(errs, fmt.Errorf("secret_bindings[%d]: duplicate logical_name %q", i, b.LogicalName))
		} else {
			seen[b.LogicalName] = i
		}
	}
	for i, cb := range s.ConfigBlocks {
		if err := cb.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("config_blocks[%d]: %w", i, err))
		}
	}
	// 块名不重复（VR-CB-6）：同名块映射同一 ConfigMap "{workload}-config-{sanitize(block)}"，
	// 结构上必然冲突（specs/045-deploy-config/data-model.md §4）。
	seenBlocks := make(map[string]int, len(s.ConfigBlocks))
	for i, cb := range s.ConfigBlocks {
		if cb.Block == "" {
			continue
		}
		if _, ok := seenBlocks[cb.Block]; ok {
			errs = append(errs, fmt.Errorf("config_blocks[%d]: duplicate block %q", i, cb.Block))
		} else {
			seenBlocks[cb.Block] = i
		}
	}
	// 块内条目名不重复（VR-CE-2）：块内 key 重复即同一 ConfigMap 同一 data key
	// 冲突（map 写覆盖）；不同块属于不同 ConfigMap，天然隔离，仅检测块内即可
	// （specs/045-deploy-config/data-model.md §4）。
	for i, cb := range s.ConfigBlocks {
		seenKeys := make(map[string]int, len(cb.Entries))
		for j, ce := range cb.Entries {
			if ce.Key == "" {
				continue
			}
			if _, ok := seenKeys[ce.Key]; ok {
				errs = append(errs, fmt.Errorf("config_blocks[%d]: duplicate entry key %q", i, ce.Key))
			} else {
				seenKeys[ce.Key] = j
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%w: %w", ErrInvalidSpec, errors.Join(errs...))
	}
	return nil
}

// Validate checks that the InfraSpec contains valid field values.
// It verifies that resource and name are non-empty.
func (s *InfraSpec) Validate() error {
	var errs []error

	if s.Resource == "" {
		errs = append(errs, errors.New("resource must not be empty"))
	}
	if s.Name == "" {
		errs = append(errs, errors.New("name must not be empty"))
	}

	if len(errs) > 0 {
		return fmt.Errorf("%w: %w", ErrInvalidSpec, errors.Join(errs...))
	}
	return nil
}

// Validate checks that the ArtifactHTTPSpec contains valid field values.
// It verifies that hostnames and matches are non-empty, and each nested match is valid.
func (s *ArtifactHTTPSpec) Validate() error {
	var errs []error

	if len(s.Hostnames) == 0 {
		errs = append(errs, errors.New("hostnames must not be empty"))
	}

	if len(s.Matches) == 0 {
		errs = append(errs, errors.New("matches must not be empty"))
	}
	for i, r := range s.Matches {
		if err := r.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("matches[%d]: %w", i, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%w: %w", ErrInvalidSpec, errors.Join(errs...))
	}
	return nil
}

// Validate checks that the HTTPRouteRule contains valid field values.
// It verifies that backend is non-empty.
func (r *HTTPRouteRule) Validate() error {
	if r.Backend == "" {
		return fmt.Errorf("%w: backend must not be empty", ErrInvalidSpec)
	}
	return nil
}
