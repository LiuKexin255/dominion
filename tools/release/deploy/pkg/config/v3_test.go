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
