//go:build !windows
// +build !windows

package input

import "os/exec"

func setCmdHideWindow(cmd *exec.Cmd) {}
