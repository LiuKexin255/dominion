// Package domain contains the deploy service domain model.
package domain

import (
	"fmt"
	"regexp"
)

const (
	environmentNamePattern = `^[a-z][a-z0-9]{0,7}$`
	environmentNameFormat  = "deploy/scopes/%s/environments/%s"
)

var (
	environmentNameRegexp        = regexp.MustCompile(environmentNamePattern)
	environmentResourceNameRegex = regexp.MustCompile(`^deploy/scopes/([a-z][a-z0-9]{0,7})/environments/([a-z][a-z0-9]{0,7})$`)
)

// EnvironmentName represents the canonical resource name for an environment.
type EnvironmentName struct {
	scope   string
	envName string
}

// ParseResourceName parses deploy/scopes/{scope}/environments/{env_name} into an EnvironmentName.
func ParseResourceName(name string) (EnvironmentName, error) {
	matches := environmentResourceNameRegex.FindStringSubmatch(name)
	if len(matches) != 3 {
		return EnvironmentName{}, ErrInvalidName
	}

	return NewEnvironmentName(matches[1], matches[2])
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

// Scope returns the scope segment of the environment resource name.
func (n EnvironmentName) Scope() string {
	return n.scope
}

// EnvName returns the environment name segment of the environment resource name.
func (n EnvironmentName) EnvName() string {
	return n.envName
}
