package solver

import (
	"fmt"
	"strconv"
	"strings"
)

// Target holds the parsed dominion grpc target parts.
type Target struct {
	App     string
	Service string
	Port    int
}

// ParseTarget parses app/service:port style dominion targets.
func ParseTarget(raw string) (*Target, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("invalid target %q: want app/service:port", raw)
	}

	if withoutScheme, ok := strings.CutPrefix(trimmed, Scheme+":///"); ok {
		trimmed = withoutScheme
	} else if strings.Contains(trimmed, "://") {
		return nil, fmt.Errorf("invalid target %q: want app/service:port or %s:///app/service:port", raw, Scheme)
	}

	parts := strings.SplitN(trimmed, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid target %q: want app/service:port", raw)
	}
	if strings.Contains(parts[1], ":") {
		return nil, fmt.Errorf("invalid target %q: want app/service:port", raw)
	}

	path := strings.SplitN(parts[0], "/", 2)
	if len(path) != 2 || path[0] == "" || path[1] == "" || strings.Contains(path[1], "/") {
		return nil, fmt.Errorf("invalid target %q: want app/service:port", raw)
	}

	for _, ch := range parts[1] {
		if ch < '0' || ch > '9' {
			return nil, fmt.Errorf("invalid target %q: port must be numeric", raw)
		}
	}

	port, err := strconv.Atoi(parts[1])
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid target %q: want app/service:port", raw)
	}

	return &Target{
		App:     path[0],
		Service: path[1],
		Port:    port,
	}, nil
}
