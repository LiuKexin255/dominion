package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseV3DeployConfig(t *testing.T) {
	root := newBazelWorkspace(t)

	tests := []struct {
		name    string
		path    string
		wantErr bool
		errSub  string
	}{
		{
			name:    "accepts version 3.0",
			path:    filepath.Join(root, "testdata", "deploy-v3.yaml"),
			wantErr: false,
		},
		{
			name:    "rejects no version",
			path:    filepath.Join(root, "testdata", "deploy-v2-no-version.yaml"),
			wantErr: true,
			errSub:  "version",
		},
		{
			name:    "rejects version 2.0",
			path:    filepath.Join(root, "testdata", "deploy-v2-explicit.yaml"),
			wantErr: true,
			errSub:  "version",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := ParseV3DeployConfig(tt.path)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("ParseV3DeployConfig() failed: %v", gotErr)
				}
				if tt.errSub != "" && !strings.Contains(gotErr.Error(), tt.errSub) {
					t.Errorf("ParseV3DeployConfig() error = %q, want substring %q", gotErr.Error(), tt.errSub)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("ParseV3DeployConfig() succeeded unexpectedly")
			}
			if got.Version != "3.0" {
				t.Errorf("ParseV3DeployConfig() Version = %q, want %q", got.Version, "3.0")
			}
		})
	}
}

func TestParseV3ServiceConfig(t *testing.T) {
	root := newBazelWorkspace(t)

	tests := []struct {
		name    string
		path    string
		wantErr bool
		errSub  string
	}{
		{
			name:    "accepts version 3.0",
			path:    filepath.Join(root, "testdata", "service-v3.yaml"),
			wantErr: false,
		},
		{
			name:    "accepts version 3.0 with configs",
			path:    filepath.Join(root, "testdata", "service.configs.yaml"),
			wantErr: false,
		},
		{
			name:    "rejects no version",
			path:    filepath.Join(root, "testdata", "service-v2-no-version.yaml"),
			wantErr: true,
			errSub:  "version",
		},
		{
			name:    "rejects version 2.0",
			path:    filepath.Join(root, "testdata", "service-v2-explicit.yaml"),
			wantErr: true,
			errSub:  "version",
		},
		{
			name:    "rejects version 2.0 with configs",
			path:    filepath.Join(root, "testdata", "service.configs-v2.yaml"),
			wantErr: true,
			errSub:  "version",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := ParseV3ServiceConfig(tt.path)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("ParseV3ServiceConfig() failed: %v", gotErr)
				}
				if tt.errSub != "" && !strings.Contains(gotErr.Error(), tt.errSub) {
					t.Errorf("ParseV3ServiceConfig() error = %q, want substring %q", gotErr.Error(), tt.errSub)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("ParseV3ServiceConfig() succeeded unexpectedly")
			}
			if got.Version != "3.0" {
				t.Errorf("ParseV3ServiceConfig() Version = %q, want %q", got.Version, "3.0")
			}
		})
	}
}

// TestParseV3ServiceConfig_Configs 版本门禁（R8，见 specs/045-deploy-config/research.md R8）：
// version "3.0" 且含 configs 的 service.yaml 经 ParseV3ServiceConfig 解析通过并保留配置块池
// （ParseV3ServiceConfig 委托 ParseServiceConfig，config 校验自动生效，v3.go 无代码改动）。
func TestParseV3ServiceConfig_Configs(t *testing.T) {
	root := newBazelWorkspace(t)

	got, err := ParseV3ServiceConfig(filepath.Join(root, "testdata", "service.configs.yaml"))
	if err != nil {
		t.Fatalf("ParseV3ServiceConfig() failed: %v", err)
	}
	if len(got.Configs) != 2 {
		t.Errorf("ParseV3ServiceConfig() Configs len = %d, want 2", len(got.Configs))
	}
}
