package input

import (
	"fmt"
	"path/filepath"
)

const helperRelativePath = "resources/bin/input-helper.exe"

// ResolveHelperPath resolves the input helper executable path from the agent directory.
// Expected layout: resources/bin/input-helper.exe in the same directory as the agent binary.
func ResolveHelperPath(agentDir string) (string, error) {
	if agentDir == "" {
		return "", fmt.Errorf("agent directory is empty")
	}
	return filepath.Join(agentDir, helperRelativePath), nil
}
