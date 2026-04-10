package mongo

import (
	"fmt"
	"strings"
)

const targetFormat = "app/name"

// Target holds the parsed dominion mongo target parts.
type Target struct {
	App  string
	Name string
}

// ParseTarget parses app/name style dominion mongo targets.
func ParseTarget(raw string) (*Target, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("invalid target %q: want %s", raw, targetFormat)
	}

	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.Contains(parts[1], "/") {
		return nil, fmt.Errorf("invalid target %q: want %s", raw, targetFormat)
	}
	parts[0] = strings.TrimSpace(parts[0])
	parts[1] = strings.TrimSpace(parts[1])
	if parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid target %q: want %s", raw, targetFormat)
	}

	return &Target{
		App:  parts[0],
		Name: parts[1],
	}, nil
}
