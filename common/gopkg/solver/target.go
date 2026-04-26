package solver

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	// dominionSchemePrefix is the URI scheme prefix for dominion targets.
	dominionSchemePrefix = "dominion:///"
	// maxPort is the maximum valid TCP/UDP port number.
	maxPort = 65535
)

var dnsLabelPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// PortSelector selects a port either by number or by name.
type PortSelector struct {
	numeric int
	name    string
}

// NumericPort returns a PortSelector that matches a specific port number.
func NumericPort(port int) PortSelector {
	return PortSelector{numeric: port}
}

// NamedPort returns a PortSelector that matches a port by its name.
func NamedPort(name string) PortSelector {
	return PortSelector{name: name}
}

// Numeric returns the numeric port value.
func (p PortSelector) Numeric() int {
	return p.numeric
}

// Name returns the named port value.
func (p PortSelector) Name() string {
	return p.name
}

// IsNumeric reports whether the selector references a numeric port.
func (p PortSelector) IsNumeric() bool {
	return p.numeric > 0
}

// IsNamed reports whether the selector references a named port.
func (p PortSelector) IsNamed() bool {
	return p.name != ""
}

// IsEmpty reports whether the selector has no port value.
func (p PortSelector) IsEmpty() bool {
	return p.numeric == 0 && p.name == ""
}

// String returns the numeric port if set, otherwise the port name.
func (p PortSelector) String() string {
	if p.numeric > 0 {
		return strconv.Itoa(p.numeric)
	}
	return p.name
}

// Target holds the parsed dominion target parts.
type Target struct {
	App          string
	Service      string
	PortSelector PortSelector
}

// ParseTarget parses app/service:port style dominion targets.
//
// The optional dominion:/// scheme prefix is accepted, and whitespace around
// segments is ignored.
func ParseTarget(raw string) (*Target, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("invalid target %q: want app/service:port", raw)
	}

	if withoutScheme, ok := strings.CutPrefix(trimmed, dominionSchemePrefix); ok {
		trimmed = withoutScheme
	} else if strings.Contains(trimmed, "://") {
		return nil, fmt.Errorf("invalid target %q: want app/service:port or dominion:///app/service:port", raw)
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
		return nil, fmt.Errorf("invalid target %q: want app/service:port", raw)
	}
	appPart = strings.TrimSpace(appPart)
	servicePart = strings.TrimSpace(servicePart)
	portPart = strings.TrimSpace(portPart)

	if appPart == "" || servicePart == "" || strings.Contains(servicePart, "/") {
		return nil, fmt.Errorf("invalid target %q: want app/service:port", raw)
	}
	if portPart == "" {
		if hasPort {
			return nil, fmt.Errorf("invalid target %q: port must be numeric or a valid DNS label", raw)
		}
		return nil, fmt.Errorf("invalid target %q: port is required", raw)
	}

	parsedPort, err := strconv.Atoi(portPart)
	if err == nil {
		if parsedPort < 0 || parsedPort > maxPort {
			return nil, fmt.Errorf("invalid target %q: port out of range", raw)
		}
		return &Target{App: appPart, Service: servicePart, PortSelector: NumericPort(parsedPort)}, nil
	}
	if !dnsLabelPattern.MatchString(portPart) {
		return nil, fmt.Errorf("invalid target %q: port must be numeric or a valid DNS label", raw)
	}

	return &Target{App: appPart, Service: servicePart, PortSelector: NamedPort(portPart)}, nil
}
