//go:build windows

package main

import "syscall"

var user32 = syscall.NewLazyDLL("user32.dll")

// setProcessDPIAware calls SetProcessDPIAware to enable DPI-aware rendering.
// Must be called before window creation.
func setProcessDPIAware() {
	proc := user32.NewProc("SetProcessDPIAware")
	proc.Call()
}
