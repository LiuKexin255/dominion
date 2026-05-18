//go:build windows
// +build windows

package encoder

import (
	"os/exec"
	"syscall"
)

func init() {
	syscall.NewLazyDLL("user32.dll").NewProc("SetProcessDPIAware").Call()
}

func setCmdHideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
