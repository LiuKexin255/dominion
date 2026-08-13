package config

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestParseServiceConfig_Configs 解析含 configs 的 service.yaml。
// 合法样例包含多个配置块，验证各块同文件独立解析、互不干扰（spec Edge Case，见
// specs/045-deploy-config/spec.md "Edge Cases"）。
func TestParseServiceConfig_Configs(t *testing.T) {
	root := newBazelWorkspace(t)

	tests := []struct {
		name string
		path string
		want *ServiceConfig
	}{
		{
			name: "读取含多个配置块的服务配置成功",
			path: filepath.Join(root, "testdata", "service.configs.yaml"),
			want: &ServiceConfig{
				Name:    "service",
				App:     "grpc-hello-world",
				Desc:    "grpc hello world service with configs",
				Version: "3.0",
				URI:     "//testdata/service.configs.yaml",
				Configs: []*ServiceConfigBlock{
					{
						Name: "service_config",
						Data: []*ServiceConfigEntry{
							{Name: "greeting", Value: "message: \"hello from config\"\ntimes: 3\n", Type: "yaml"},
							{Name: "limits", Value: `{"maxConn": 100}`, Type: "json"},
						},
					},
					{
						Name: "feature_flags",
						Data: []*ServiceConfigEntry{
							{Name: "debug", Value: "false", Type: "yaml"},
						},
					},
				},
				Artifacts: []*ServiceArtifact{
					{
						Name:   "service",
						Target: "//testdata:service_image",
						Ports: []*ServiceArtifactPort{
							{Name: "grpc", Port: 50051},
						},
					},
				},
				Kind: WorkloadKindStateless,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseServiceConfig(tt.path)
			if err != nil {
				t.Fatalf("ParseServiceConfig() failed: %v", err)
			}
			if !reflect.DeepEqual(tt.want, got) {
				t.Errorf("ParseServiceConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestParseServiceConfig_Configs_RejectsInvalid 拒绝非法的配置块声明：
// value 格式与 type 不一致（FR-003，见 specs/045-deploy-config/spec.md FR-003）、
// 重复配置块名与同块内重复条目名（FR-004）、空字符串 value（spec Edge Case，schema minLength 1）。
// 错误信息须指明具体配置块名/条目名与原因。
func TestParseServiceConfig_Configs_RejectsInvalid(t *testing.T) {
	root := newBazelWorkspace(t)

	tests := []struct {
		name   string
		path   string
		errSub string
	}{
		{
			name:   "json 类型 value 格式非法",
			path:   filepath.Join(root, "testdata", "service.configs.invalid-json.yaml"),
			errSub: `配置块 "service_config" 条目 "limits"`,
		},
		{
			name:   "yaml 类型 value 格式非法",
			path:   filepath.Join(root, "testdata", "service.configs.invalid-yaml.yaml"),
			errSub: `配置块 "service_config" 条目 "greeting"`,
		},
		{
			name:   "重复配置块名",
			path:   filepath.Join(root, "testdata", "service.configs.duplicate-block.yaml"),
			errSub: `配置块 "service_config" 重复声明`,
		},
		{
			name:   "同块内重复条目名",
			path:   filepath.Join(root, "testdata", "service.configs.duplicate-entry.yaml"),
			errSub: `配置块 "service_config" 内条目 "greeting" 重复声明`,
		},
		{
			name: "空字符串 value 被拒绝",
			path: filepath.Join(root, "testdata", "service.configs.empty-value.yaml"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseServiceConfig(tt.path)
			if err == nil {
				t.Fatal("ParseServiceConfig() succeeded unexpectedly")
			}
			if tt.errSub != "" && !strings.Contains(err.Error(), tt.errSub) {
				t.Errorf("ParseServiceConfig() error = %q, want substring %q", err.Error(), tt.errSub)
			}
		})
	}
}

// TestParseServiceConfig_Configs_BackwardCompat 无 configs 的现有 service.yaml 行为不变（FR-020）。
func TestParseServiceConfig_Configs_BackwardCompat(t *testing.T) {
	root := newBazelWorkspace(t)

	got, err := ParseServiceConfig(filepath.Join(root, "testdata", "service.yaml"))
	if err != nil {
		t.Fatalf("ParseServiceConfig() failed: %v", err)
	}
	if len(got.Configs) != 0 {
		t.Errorf("ParseServiceConfig() Configs = %v, want none", got.Configs)
	}
}
