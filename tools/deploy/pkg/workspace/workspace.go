package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	WorkspaceKey = "BUILD_WORKSPACE_DIRECTORY"
	WorkingKey   = "BUILD_WORKING_DIRECTORY"

	workspacePathPrefix = "//"
)

// Root 返回项目根目录
func Root() (string, error) {
	root := os.Getenv(WorkspaceKey)
	if root == "" {
		return "", fmt.Errorf("获取项目根目录失败")
	}

	if !exists(root) {
		return "", fmt.Errorf("项目目录 %s 读取失败", root)
	}

	return root, nil
}

// MustRoot 返回项目根目录，如果报错抛出 panic
func MustRoot() string {
	ws, err := Root()
	if err != nil {
		panic(err)
	}
	return ws
}

// Working 返回 shell 当前目录
func Working() (string, error) {
	working := os.Getenv(WorkingKey)
	if working == "" {
		return "", fmt.Errorf("获取当前目录失败")
	}

	if !exists(working) {
		return "", fmt.Errorf("当前目录 %s 读取失败", working)
	}

	return working, nil
}

// MustRoot 返回当前目录，如果报错抛出 panic
func MustWorking() string {
	ws, err := Working()
	if err != nil {
		panic(err)
	}
	return ws
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func ResolvePath(inputPath string) string {
	if strings.HasPrefix(inputPath, workspacePathPrefix) {
		return ResolveRootPath(inputPath)
	}

	if filepath.IsAbs(inputPath) {
		return inputPath
	}
	return filepath.Join(MustWorking(), inputPath)
}

func ResolveRootPath(inputPath string) string {
	return filepath.Join(MustRoot(), strings.TrimPrefix(inputPath, workspacePathPrefix))
}
