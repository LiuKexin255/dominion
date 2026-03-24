package workspace

import (
	"fmt"
	"os"
)

const (
	WorkspaceKey = "BUILD_WORKSPACE_DIRECTORY"
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

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
