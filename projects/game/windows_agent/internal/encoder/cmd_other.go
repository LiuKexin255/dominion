//go:build !windows
// +build !windows

package encoder

import "os/exec"

func setCmdHideWindow(cmd *exec.Cmd) {}
