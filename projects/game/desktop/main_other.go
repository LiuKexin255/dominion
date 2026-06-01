//go:build !windows

package main

// setProcessDPIAware is a no-op on non-Windows platforms.
func setProcessDPIAware() {}
