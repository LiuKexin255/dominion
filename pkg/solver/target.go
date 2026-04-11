package solver

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// dominionSchemePrefix is the URI scheme prefix for dominion targets.
	dominionSchemePrefix = "dominion:///"
	// maxPort is the maximum valid TCP/UDP port number.
	maxPort = 65535
)

// Target holds the parsed dominion target parts.
type Target struct {
	App     string
	Service string
	Port    int
}

// ParseTarget parses app/service or app/service:port style dominion targets.
//
// The optional dominion:/// scheme prefix is accepted, and whitespace around
// segments is ignored.
func ParseTarget(raw string) (*Target, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("invalid target %q: want app/service[:port]", raw)
	}

	if strings.HasPrefix(trimmed, dominionSchemePrefix) {
		trimmed = strings.TrimPrefix(trimmed, dominionSchemePrefix)
	} else if strings.Contains(trimmed, "://") {
		return nil, fmt.Errorf("invalid target %q: want app/service[:port] or dominion:///app/service[:port]", raw)
	}

	pathPart := trimmed
	portPart := ""
	hasPort := false
	if before, after, ok := strings.Cut(trimmed, ":"); ok {
		pathPart = before
		portPart = after
		hasPort = true
	}

	appPart, servicePart, ok := strings.Cut(pathPart, "/")
	if !ok {
		return nil, fmt.Errorf("invalid target %q: want app/service[:port]", raw)
	}
	appPart = strings.TrimSpace(appPart)
	servicePart = strings.TrimSpace(servicePart)
	portPart = strings.TrimSpace(portPart)

	if appPart == "" || servicePart == "" || strings.Contains(servicePart, "/") {
		return nil, fmt.Errorf("invalid target %q: want app/service[:port]", raw)
	}
	if hasPort && portPart == "" {
		return nil, fmt.Errorf("invalid target %q: port must be numeric", raw)
	}

	port := 0
	if portPart != "" {
		parsedPort, err := strconv.Atoi(portPart)
		if err != nil {
			return nil, fmt.Errorf("invalid target %q: port must be numeric", raw)
		}
		if parsedPort < 0 || parsedPort > maxPort {
			return nil, fmt.Errorf("invalid target %q: port out of range", raw)
		}
		port = parsedPort
	}

	target := new(Target)
	target.App = appPart
	target.Service = servicePart
	target.Port = port

	return target, nil
}
