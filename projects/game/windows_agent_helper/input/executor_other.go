//go:build !windows

package main

import "errors"

// WindowsExecutor is a non-Windows placeholder used for unit tests on host platforms.
type WindowsExecutor struct{}

// NewWindowsExecutor returns a placeholder executor on non-Windows platforms.
func NewWindowsExecutor() (*WindowsExecutor, error) {
	return new(WindowsExecutor), nil
}

// Execute reports that mouse execution is only available in Windows builds.
func (executor *WindowsExecutor) Execute(command *Command) error {
	return errors.New("windows input execution is unavailable on this platform")
}

// ReleaseAll is a no-op on non-Windows platforms.
func (executor *WindowsExecutor) ReleaseAll() {}
