package domain

import (
	"fmt"
	"strings"
)

// HTTPPathRuleType defines the type of HTTP path matching rule.
type HTTPPathRuleType int

const (
	// HTTPPathRuleTypeUnspecified indicates no path rule type has been set.
	HTTPPathRuleTypeUnspecified HTTPPathRuleType = 0
	// HTTPPathRuleTypePathPrefix matches requests by path prefix.
	HTTPPathRuleTypePathPrefix HTTPPathRuleType = 1
)

// ServicePortSpec describes a single port exposed by a service.
type ServicePortSpec struct {
	Name string
	Port int32
}

// ServiceSpec describes the desired state of a deployable application service.
type ServiceSpec struct {
	Name       string
	App        string
	Image      string
	Ports      []ServicePortSpec
	Replicas   int32
	TLSEnabled bool
}

// InfraSpec describes the desired state of an infrastructure resource.
type InfraSpec struct {
	Resource           string
	Profile            string
	Name               string
	App                string
	PersistenceEnabled bool
}

// HTTPPathRule describes an HTTP path matching rule.
type HTTPPathRule struct {
	Type  HTTPPathRuleType
	Value string
}

// HTTPRouteRule describes a single routing rule with a backend and path match.
type HTTPRouteRule struct {
	Backend string // 后端的端口名
	Path    HTTPPathRule
}

// HTTPRouteSpec describes an HTTP routing configuration.
type HTTPRouteSpec struct {
	ServiceName string
	Hostnames   []string
	Rules       []HTTPRouteRule
}

// Validate checks that the ServiceSpec contains valid field values.
// It verifies that name, app, and image are non-empty, each port is in
// the range 1-65535, and replicas is non-negative.
func (s *ServiceSpec) Validate() error {
	var errs []string

	if s.Name == "" {
		errs = append(errs, "name must not be empty")
	}
	if s.App == "" {
		errs = append(errs, "app must not be empty")
	}
	if s.Image == "" {
		errs = append(errs, "image must not be empty")
	}
	for i, p := range s.Ports {
		if p.Port < 1 || p.Port > 65535 {
			errs = append(errs, fmt.Sprintf("ports[%d].port must be between 1 and 65535, got %d", i, p.Port))
		}
	}
	if s.Replicas < 0 {
		errs = append(errs, "replicas must be non-negative")
	}

	if len(errs) > 0 {
		return fmt.Errorf("%w: %s", ErrInvalidSpec, strings.Join(errs, "; "))
	}
	return nil
}

// Validate checks that the InfraSpec contains valid field values.
// It verifies that resource and name are non-empty.
func (s *InfraSpec) Validate() error {
	var errs []string

	if s.Resource == "" {
		errs = append(errs, "resource must not be empty")
	}
	if s.Name == "" {
		errs = append(errs, "name must not be empty")
	}

	if len(errs) > 0 {
		return fmt.Errorf("%w: %s", ErrInvalidSpec, strings.Join(errs, "; "))
	}
	return nil
}

// Validate checks that the HTTPRouteSpec contains valid field values.
// It verifies that service name, hostnames and rules are non-empty, and each
// rule has a non-empty backend.
func (s *HTTPRouteSpec) Validate() error {
	var errs []string

	if s.ServiceName == "" {
		errs = append(errs, "service_name must not be empty")
	}
	if len(s.Hostnames) == 0 {
		errs = append(errs, "hostnames must not be empty")
	}
	if len(s.Rules) == 0 {
		errs = append(errs, "rules must not be empty")
	}
	for i, r := range s.Rules {
		if err := r.Validate(); err != nil {
			errs = append(errs, fmt.Sprintf("rules[%d]: %s", i, err.Error()))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%w: %s", ErrInvalidSpec, strings.Join(errs, "; "))
	}
	return nil
}

// Validate checks that the HTTPRouteRule contains valid field values.
// It verifies that backend is non-empty.
func (r *HTTPRouteRule) Validate() error {
	if r.Backend == "" {
		return fmt.Errorf("%w: backend must not be empty", ErrInvalidSpec)
	}
	return nil
}
