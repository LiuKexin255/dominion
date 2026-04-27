package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	WorkspacePathPrefix = "//"
	bazelWorkspaceFile  = "WORKSPACE.bazel"
	bazelModuleFile     = "MODULE.bazel"
)

// Root 返回项目根目录
func Root() (string, error) {
	working, err := Working()
	if err != nil {
		return "", err
	}

	for {
		if hasBazelWorkspace(working) {
			return working, nil
		}

		parent := filepath.Dir(working)
		if parent == working {
			break
		}
		working = parent
	}

	return "", fmt.Errorf("未找到 Bazel 工作区根目录")
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
	working, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("获取当前工作目录失败: %w", err)
	}

	return filepath.Clean(working), nil
}

// MustWorking 返回当前工作目录，如果报错抛出 panic。
func MustWorking() string {
	ws, err := Working()
	if err != nil {
		panic(err)
	}
	return ws
}

func hasBazelWorkspace(path string) bool {
	_, err := os.Stat(filepath.Join(path, bazelWorkspaceFile))
	if err == nil {
		return true
	}
	_, err = os.Stat(filepath.Join(path, bazelModuleFile))
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
