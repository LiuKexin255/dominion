//go:build !windows

package window

// EnumerateWindows returns nil on non-Windows platforms.
func EnumerateWindows() ([]WindowInfo, error) {
	return nil, nil
}
