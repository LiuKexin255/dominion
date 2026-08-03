// Package domain contains the deploy service domain model.
package domain

import (
	"fmt"
	"regexp"
)

const (
	environmentNamePattern = `^[a-z][a-z0-9]{0,7}$`
	environmentNameFormat  = "deploy/scopes/%s/environments/%s"
	environmentLabelFormat = "%s.%s"
)

var (
	environmentNameRegexp = regexp.MustCompile(environmentNamePattern)
)

// EnvironmentName represents the canonical resource name for an environment.
type EnvironmentName struct {
	scope   string
	envName string
}

// ValidateScope reports whether s conforms to the scope format rule
// (^[a-z][a-z0-9]{0,7}$). Used by handler-layer scope validation after
// codegen ParseScopeName (structural validation) and before cross-scope
// query dispatch.
func ValidateScope(s string) error {
	if !environmentNameRegexp.MatchString(s) {
		return ErrInvalidName
	}
	return nil
}

// NewEnvironmentName validates scope and envName and constructs an EnvironmentName.
func NewEnvironmentName(scope, envName string) (EnvironmentName, error) {
	if !environmentNameRegexp.MatchString(scope) || !environmentNameRegexp.MatchString(envName) {
		return EnvironmentName{}, ErrInvalidName
	}

	return EnvironmentName{
		scope:   scope,
		envName: envName,
	}, nil
}

// String returns the canonical resource name deploy/scopes/{scope}/environments/{env_name}.
func (n EnvironmentName) String() string {
	return fmt.Sprintf(environmentNameFormat, n.scope, n.envName)
}

// Label 标签名
func (n EnvironmentName) Label() string {
	return fmt.Sprintf(environmentLabelFormat, n.scope, n.envName)
}

// Scope returns the scope segment of the environment resource name.
func (n EnvironmentName) Scope() string {
	return n.scope
}

// EnvName returns the environment name segment of the environment resource name.
func (n EnvironmentName) EnvName() string {
	return n.envName
}
