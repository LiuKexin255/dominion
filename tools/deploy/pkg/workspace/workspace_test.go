package workspace_test

import (
	"path/filepath"
	"testing"

	"dominion/tools/deploy/pkg/workspace"
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

func TestResolvePath(t *testing.T) {
	workspaceRoot := t.TempDir()
	workingDir := t.TempDir()
	t.Setenv(workspace.WorkspaceKey, workspaceRoot)
	t.Setenv(workspace.WorkingKey, workingDir)

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "workspace prefix path",
			path: "//tools/deploy/deploy.yaml",
			want: filepath.Join(workspaceRoot, "tools/deploy/deploy.yaml"),
		},
		{
			name: "relative path",
			path: "tools/deploy/deploy.yaml",
			want: filepath.Join(workingDir, "tools/deploy/deploy.yaml"),
		},
		{
			name: "absolute path",
			path: "/tmp/deploy.yaml",
			want: "/tmp/deploy.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := workspace.ResolvePath(tt.path)
			if got != tt.want {
				t.Fatalf("resolvePath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestToURI(t *testing.T) {
	root := t.TempDir()

	tests := []struct {
		name    string
		root    string // BUILD_WORKSPACE_DIRECTORY 值，空字符串表示未设置
		absPath string
		want    string
		wantErr bool
	}{
		{
			name:    "仓库内路径正确转换为 URI",
			root:    root,
			absPath: filepath.Join(root, "tools/deploy/deploy.yaml"),
			want:    "//tools/deploy/deploy.yaml",
		},
		{
			name:    "仓库外路径返回错误",
			root:    root,
			absPath: filepath.Join(t.TempDir(), "outside.yaml"),
			wantErr: true,
		},
		{
			name:    "BUILD_WORKSPACE_DIRECTORY 未设置时返回错误",
			root:    "",
			absPath: filepath.Join(t.TempDir(), "tools/deploy/deploy.yaml"),
			wantErr: true,
		},
		{
			name:    "共享前缀但非子目录返回错误",
			root:    root,
			absPath: root + "-sibling/file.yaml",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(workspace.WorkspaceKey, tt.root)
			got, gotErr := workspace.ToURI(tt.absPath)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("ToURI(%q) failed: %v", tt.absPath, gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatalf("ToURI(%q) succeeded unexpectedly", tt.absPath)
			}
			if got != tt.want {
				t.Errorf("ToURI(%q) = %q, want %q", tt.absPath, got, tt.want)
			}
		})
	}
}
