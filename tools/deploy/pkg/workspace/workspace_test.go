package workspace_test

import (
	"dominion/tools/deploy/pkg/workspace"
	"path/filepath"
	"testing"
)

func TestRoot(t *testing.T) {
	tests := []struct {
		name    string // description of this test case
		want    string
		wantErr bool
	}{
		{
			name:    "未能获取到项目目录地址",
			wantErr: true,
		},
		{
			name:    "获取到的项目地址无法读取",
			want:    filepath.Join(t.TempDir(), "not-exists"),
			wantErr: true,
		},
		{
			name: "正常读取到项目地址",
			want: t.TempDir(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BUILD_WORKSPACE_DIRECTORY", tt.want)

			got, gotErr := workspace.Root()
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("Root() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("Root() succeeded unexpectedly")
			}
			if got != tt.want {
				t.Errorf("Root() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWorking(t *testing.T) {
	tests := []struct {
		name    string
		want    string
		wantErr bool
	}{
		{
			name:    "未能获取到当前目录地址",
			wantErr: true,
		},
		{
			name:    "获取到的当前目录地址无法读取",
			want:    filepath.Join(t.TempDir(), "not-exists"),
			wantErr: true,
		},
		{
			name: "正常读取到当前目录地址",
			want: t.TempDir(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(workspace.WorkingKey, tt.want)

			got, gotErr := workspace.Working()
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("Working() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("Working() succeeded unexpectedly")
			}
			if got != tt.want {
				t.Errorf("Working() = %v, want %v", got, tt.want)
			}
		})
	}
}
