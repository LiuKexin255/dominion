package runtime

import (
	"fmt"
	"net/url"
	"strings"
)

const sessionRoleWindowsAgent = "GAME_CLIENT_ROLE_WINDOWS_AGENT"

// Session records the gateway session identity for one agent runtime.
type Session struct {
	ID         string
	ConnectURL string
	Role       string
}

// ParseSessionURL extracts the session ID from a gateway WebSocket connect URL path.
// Token semantics are intentionally ignored here.
func ParseSessionURL(connectURL string) (string, error) {
	parsed, err := url.Parse(connectURL)
	if err != nil {
		return "", fmt.Errorf("parse connect URL: %w", err)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 4 || parts[0] != "v1" || parts[1] != "sessions" || parts[3] != "game" {
		return "", fmt.Errorf("invalid connect URL path: %s", parsed.Path)
	}
	if len(parts) != 5 || parts[4] != "connect" || parts[2] == "" {
		return "", fmt.Errorf("invalid session ID in path: %s", parsed.Path)
	}
	return parts[2], nil
}
