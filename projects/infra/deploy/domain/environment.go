// Package domain contains the deploy service domain model.
package domain

import (
	"fmt"
	"strings"
	"time"
)

// Environment is the aggregate root for a deploy environment.
type Environment struct {
	name         EnvironmentName
	description  string
	desiredState DesiredState
	status       EnvironmentStatus
	createTime   time.Time
	updateTime   time.Time
	etag         string
}

// DesiredState describes the target deployment content of an environment.
type DesiredState struct {
	Services   []ServiceSpec
	Infras     []InfraSpec
	HTTPRoutes []HTTPRouteSpec
}

// EnvironmentStatus describes the observed reconciliation status.
type EnvironmentStatus struct {
	State             EnvironmentState
	Message           string
	LastReconcileTime time.Time
	LastSuccessTime   time.Time
}

// NewEnvironment validates and constructs an environment in the pending state.
func NewEnvironment(name EnvironmentName, description string, desiredState DesiredState) (*Environment, error) {
	env := &Environment{
		name:         name,
		description:  description,
		desiredState: cloneDesiredState(desiredState),
		status: EnvironmentStatus{
			State: StatePending,
		},
	}

	if err := env.Validate(); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	env.createTime = now
	env.updateTime = now

	return env, nil
}

// Name returns the canonical resource name of the environment.
func (e *Environment) Name() EnvironmentName {
	return e.name
}

// Description returns the environment description.
func (e *Environment) Description() string {
	return e.description
}

// DesiredState returns a copy of the desired state.
func (e *Environment) DesiredState() DesiredState {
	return cloneDesiredState(e.desiredState)
}

// Status returns a copy of the observed status.
func (e *Environment) Status() EnvironmentStatus {
	return e.status
}

// CreateTime returns the creation timestamp.
func (e *Environment) CreateTime() time.Time {
	return e.createTime
}

// UpdateTime returns the last update timestamp.
func (e *Environment) UpdateTime() time.Time {
	return e.updateTime
}

// ETag returns the optimistic-lock token.
func (e *Environment) ETag() string {
	return e.etag
}

// UpdateDesiredState replaces the desired state and marks the environment reconciling.
func (e *Environment) UpdateDesiredState(newState DesiredState) error {
	if e.status.State == StateDeleting {
		return ErrInvalidState
	}

	previous := e.desiredState
	e.desiredState = cloneDesiredState(newState)
	if err := e.Validate(); err != nil {
		e.desiredState = previous
		return err
	}

	if err := e.transitionTo(StateReconciling); err != nil {
		e.desiredState = previous
		return err
	}

	e.status.Message = ""
	return nil
}

// MarkReconciling transitions the environment to reconciling.
func (e *Environment) MarkReconciling() error {
	if err := e.transitionTo(StateReconciling); err != nil {
		return err
	}

	e.status.Message = ""
	return nil
}

// MarkReady transitions the environment to ready and records success time.
func (e *Environment) MarkReady() error {
	if err := e.transitionTo(StateReady); err != nil {
		return err
	}

	e.status.Message = ""
	e.status.LastSuccessTime = time.Now().UTC()
	return nil
}

// MarkFailed transitions the environment to failed and records the message.
func (e *Environment) MarkFailed(msg string) error {
	if err := e.transitionTo(StateFailed); err != nil {
		return err
	}

	e.status.Message = msg
	return nil
}

// MarkDeleting transitions the environment to deleting.
func (e *Environment) MarkDeleting() error {
	return e.transitionTo(StateDeleting)
}

// Validate checks the desired state and cross-object references.
func (e *Environment) Validate() error {
	var errs []string

	serviceNames := make(map[string]struct{})
	for i := range e.desiredState.Services {
		svc := &e.desiredState.Services[i]
		if err := svc.Validate(); err != nil {
			errs = append(errs, fmt.Sprintf("services[%d]: %s", i, err.Error()))
			continue
		}
		serviceNames[svc.Name] = struct{}{}
	}

	for i := range e.desiredState.Infras {
		infra := &e.desiredState.Infras[i]
		if err := infra.Validate(); err != nil {
			errs = append(errs, fmt.Sprintf("infras[%d]: %s", i, err.Error()))
		}
	}

	for i := range e.desiredState.HTTPRoutes {
		route := &e.desiredState.HTTPRoutes[i]
		if err := route.Validate(); err != nil {
			errs = append(errs, fmt.Sprintf("http_routes[%d]: %s", i, err.Error()))
			continue
		}

		for j, rule := range route.Rules {
			if _, ok := serviceNames[rule.Backend]; !ok {
				errs = append(errs, fmt.Sprintf("http_routes[%d].rules[%d]: backend %q does not reference an existing service", i, j, rule.Backend))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%w: %s", ErrInvalidSpec, strings.Join(errs, "; "))
	}

	return nil
}

func (e *Environment) transitionTo(next EnvironmentState) error {
	if !CanTransition(e.status.State, next) {
		return ErrInvalidState
	}

	now := time.Now().UTC()
	e.status.State = next
	e.updateTime = now
	if next == StateReconciling {
		e.status.LastReconcileTime = now
	}

	return nil
}

func cloneDesiredState(state DesiredState) DesiredState {
	return DesiredState{
		Services:   cloneServices(state.Services),
		Infras:     cloneInfras(state.Infras),
		HTTPRoutes: cloneHTTPRoutes(state.HTTPRoutes),
	}
}

func cloneServices(services []ServiceSpec) []ServiceSpec {
	if len(services) == 0 {
		return nil
	}

	cloned := make([]ServiceSpec, len(services))
	for i, service := range services {
		cloned[i] = service
		if len(service.Ports) > 0 {
			cloned[i].Ports = append([]ServicePortSpec(nil), service.Ports...)
		}
	}

	return cloned
}

func cloneInfras(infras []InfraSpec) []InfraSpec {
	if len(infras) == 0 {
		return nil
	}

	return append([]InfraSpec(nil), infras...)
}

func cloneHTTPRoutes(routes []HTTPRouteSpec) []HTTPRouteSpec {
	if len(routes) == 0 {
		return nil
	}

	cloned := make([]HTTPRouteSpec, len(routes))
	for i, route := range routes {
		cloned[i] = route
		if len(route.Hostnames) > 0 {
			cloned[i].Hostnames = append([]string(nil), route.Hostnames...)
		}
		if len(route.Rules) > 0 {
			cloned[i].Rules = append([]HTTPRouteRule(nil), route.Rules...)
		}
	}

	return cloned
}
