package domain

import (
	"errors"
	"fmt"
)

// HTTPPathRuleType defines the type of HTTP path matching rule.
type HTTPPathRuleType int

const (
	// HTTPPathRuleTypeUnspecified indicates no path rule type has been set.
	HTTPPathRuleTypeUnspecified HTTPPathRuleType = 0
	// HTTPPathRuleTypePathPrefix matches requests by path prefix.
	HTTPPathRuleTypePathPrefix HTTPPathRuleType = 1
)

// ArtifactPortSpec describes a single port exposed by an artifact.
type ArtifactPortSpec struct {
	Name string
	Port int32
}

// ArtifactHTTPSpec describes the desired HTTP routing state for an artifact.
type ArtifactHTTPSpec struct {
	Hostnames []string
	Matches   []HTTPRouteRule
}

// ArtifactSpec describes the desired state of a deployable application artifact.
type ArtifactSpec struct {
	Name       string
	App        string
	Image      string
	Ports      []ArtifactPortSpec
	Replicas   int32
	TLSEnabled bool
	HTTP       *ArtifactHTTPSpec
}

// InfraPersistenceSpec describes infrastructure persistence settings.
type InfraPersistenceSpec struct {
	Enabled bool
}

// InfraSpec describes the desired state of an infrastructure resource.
type InfraSpec struct {
	Resource    string
	Profile     string
	Name        string
	App         string
	Persistence InfraPersistenceSpec
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

// Validate checks that the ArtifactSpec contains valid field values.
// It verifies that name, app, and image are non-empty, each port is in
// the range 1-65535, replicas is non-negative, and nested HTTP settings are valid.
func (s *ArtifactSpec) Validate() error {
	var errs []error

	if s.Name == "" {
		errs = append(errs, errors.New("name must not be empty"))
	}
	if s.App == "" {
		errs = append(errs, errors.New("app must not be empty"))
	}
	if s.Image == "" {
		errs = append(errs, errors.New("image must not be empty"))
	}
	for i, p := range s.Ports {
		if p.Port < 1 || p.Port > 65535 {
			errs = append(errs, fmt.Errorf("ports[%d].port must be between 1 and 65535, got %d", i, p.Port))
		}
	}
	if s.Replicas < 0 {
		errs = append(errs, errors.New("replicas must be non-negative"))
	}
	if s.HTTP != nil {
		if err := s.HTTP.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("http: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%w: %w", ErrInvalidSpec, errors.Join(errs...))
	}
	return nil
}

// Validate checks that the InfraSpec contains valid field values.
// It verifies that resource and name are non-empty.
func (s *InfraSpec) Validate() error {
	var errs []error

	if s.Resource == "" {
		errs = append(errs, errors.New("resource must not be empty"))
	}
	if s.Name == "" {
		errs = append(errs, errors.New("name must not be empty"))
	}

	if len(errs) > 0 {
		return fmt.Errorf("%w: %w", ErrInvalidSpec, errors.Join(errs...))
	}
	return nil
}

// Validate checks that the ArtifactHTTPSpec contains valid field values.
// It verifies that hostnames and matches are non-empty, and each rule is valid.
func (s *ArtifactHTTPSpec) Validate() error {
	var errs []error

	if len(s.Hostnames) == 0 {
		errs = append(errs, errors.New("hostnames must not be empty"))
	}
	if len(s.Matches) == 0 {
		errs = append(errs, errors.New("matches must not be empty"))
	}
	for i, r := range s.Matches {
		if err := r.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("matches[%d]: %w", i, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%w: %w", ErrInvalidSpec, errors.Join(errs...))
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
