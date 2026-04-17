// Package domain contains the deploy service domain model.
package domain

import (
	"errors"
	"fmt"
	"regexp"
)

const (
	serviceEndpointsNamePattern = `^[a-z][a-z0-9]{0,7}$`
	serviceEndpointsNameFormat  = "deploy/scopes/%s/environments/%s/apps/%s/services/%s/endpoints"
	serviceEndpointsLabelFormat = "%s.%s"
	appNamePattern              = `^[a-z][a-z0-9-]{0,19}$`
)

var (
	serviceEndpointsNameRegexp        = regexp.MustCompile(serviceEndpointsNamePattern)
	appNameRegexp                     = regexp.MustCompile(appNamePattern)
	serviceEndpointsResourceNameRegex = regexp.MustCompile(`^deploy/scopes/([a-z][a-z0-9]{0,7})/environments/([a-z][a-z0-9]{0,7})/apps/([a-z][a-z0-9-]{0,19})/services/([a-z][a-z0-9-]{0,19})/endpoints$`)

	// ErrServiceNotFound indicates that the requested service does not exist.
	ErrServiceNotFound = errors.New("service does not exist")

	// ErrServicePortMapUnavailable indicates that the service port map cannot be resolved.
	ErrServicePortMapUnavailable = errors.New("service port map unavailable")
)

// ServiceQueryResult holds the resolved ports map and endpoint addresses.
type ServiceQueryResult struct {
	Ports     map[string]int32
	Endpoints []string
}

// ServiceEndpointsName represents the canonical resource name for service endpoints.
type ServiceEndpointsName struct {
	scope   string
	envName string
	app     string
	service string
}

// ParseServiceEndpointsName parses deploy/scopes/{scope}/environments/{env_name}/apps/{app}/services/{service}/endpoints into a ServiceEndpointsName.
func ParseServiceEndpointsName(name string) (ServiceEndpointsName, error) {
	matches := serviceEndpointsResourceNameRegex.FindStringSubmatch(name)
	if len(matches) != 5 {
		return ServiceEndpointsName{}, ErrInvalidName
	}

	return NewServiceEndpointsName(matches[1], matches[2], matches[3], matches[4])
}

// NewServiceEndpointsName validates scope, envName, app, and service and constructs a ServiceEndpointsName.
func NewServiceEndpointsName(scope, envName, app, service string) (ServiceEndpointsName, error) {
	if !serviceEndpointsNameRegexp.MatchString(scope) || !serviceEndpointsNameRegexp.MatchString(envName) {
		return ServiceEndpointsName{}, ErrInvalidName
	}
	if !appNameRegexp.MatchString(app) || !appNameRegexp.MatchString(service) {
		return ServiceEndpointsName{}, ErrInvalidName
	}

	return ServiceEndpointsName{
		scope:   scope,
		envName: envName,
		app:     app,
		service: service,
	}, nil
}

// String returns the canonical resource name deploy/scopes/{scope}/environments/{env_name}/apps/{app}/services/{service}/endpoints.
func (n ServiceEndpointsName) String() string {
	return fmt.Sprintf(serviceEndpointsNameFormat, n.scope, n.envName, n.app, n.service)
}

// Scope returns the scope segment of the service endpoints resource name.
func (n ServiceEndpointsName) Scope() string {
	return n.scope
}

// EnvName returns the environment name segment of the service endpoints resource name.
func (n ServiceEndpointsName) EnvName() string {
	return n.envName
}

// App returns the app segment of the service endpoints resource name.
func (n ServiceEndpointsName) App() string {
	return n.app
}

// Service returns the service segment of the service endpoints resource name.
func (n ServiceEndpointsName) Service() string {
	return n.service
}

// EnvironmentName returns the environment resource name for the service endpoints resource.
func (n ServiceEndpointsName) EnvironmentName() (EnvironmentName, error) {
	return NewEnvironmentName(n.scope, n.envName)
}

// EnvLabel returns the environment label scope.envName.
func (n ServiceEndpointsName) EnvLabel() string {
	return fmt.Sprintf(serviceEndpointsLabelFormat, n.scope, n.envName)
}
