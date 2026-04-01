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

	WorkspacePathPrefix = "//"
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
	if strings.HasPrefix(inputPath, WorkspacePathPrefix) {
		return ResolveRootPath(inputPath)
	}

	if filepath.IsAbs(inputPath) {
		return inputPath
	}
	return filepath.Join(MustWorking(), inputPath)
}

func ResolveRootPath(inputPath string) string {
	return filepath.Join(MustRoot(), strings.TrimPrefix(inputPath, WorkspacePathPrefix))
}

// ToURI 将路径转换为 // 开头的仓库相对 URI。
// 如果路径不在仓库根目录下或无法获取仓库根目录，返回错误。
func ToURI(absPath string) (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return "", fmt.Errorf("计算路径 %s 相对于 %s 的相对路径失败: %w", absPath, root, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("路径 %s 不在项目目录 %s 下", absPath, root)
	}
	return WorkspacePathPrefix + rel, nil
}
