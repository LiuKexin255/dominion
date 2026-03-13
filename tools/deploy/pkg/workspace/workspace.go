package workspace

import (
	"fmt"
	"os"
)

// Root 返回项目根目录
func Root() (string, error) {
	root := os.Getenv("BUILD_WORKSPACE_DIRECTORY")
	if root == "" {
		return "", fmt.Errorf("获取项目根目录失败")
	}

	if !exists(root) {
		return "", fmt.Errorf("项目目录 %s 读取失败", root)
	}

	return root, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
