// Package domain contains the deploy service domain model.
package domain

import (
	"errors"
	"fmt"
	"time"
)

// EnvironmentSnapshot captures the full persisted state of an environment.
// It is used to rehydrate existing aggregates from storage without bypassing
// domain encapsulation.
type EnvironmentSnapshot struct {
	Name         EnvironmentName
	EnvType      EnvironmentType
	Description  string
	DesiredState *DesiredState
	Status       *EnvironmentStatus
	Generation   int64
	CreateTime   time.Time
	UpdateTime   time.Time
	ETag         string
}

// Environment is the aggregate root for a deploy environment.
type Environment struct {
	name         EnvironmentName
	envType      EnvironmentType
	description  string
	desiredState *DesiredState
	status       *EnvironmentStatus
	generation   int64
	createTime   time.Time
	updateTime   time.Time
	etag         string
}

// DesiredState describes the target deployment content of an environment.
type DesiredState struct {
	Artifacts []*ArtifactSpec
	Infras    []*InfraSpec
}

// EnvironmentStatus describes the observed reconciliation status.
type EnvironmentStatus struct {
	Desired            EnvironmentDesired
	State              EnvironmentState
	ObservedGeneration int64
	Message            string
	LastReconcileTime  time.Time
	LastSuccessTime    time.Time
}

// NewEnvironment validates and constructs an environment in the pending state.
func NewEnvironment(name EnvironmentName, envType EnvironmentType, description string, desiredState *DesiredState) (*Environment, error) {
	if envType == EnvironmentTypeUnspecified {
		return nil, ErrInvalidType
	}
	if desiredState == nil {
		return nil, ErrInvalidSpec
	}

	env := &Environment{
		name:         name,
		envType:      envType,
		description:  description,
		desiredState: cloneDesiredState(desiredState),
		status: &EnvironmentStatus{
			Desired:            DesiredPresent,
			State:              StatePending,
			ObservedGeneration: 0,
		},
		generation: 1,
	}

	if err := env.Validate(); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	env.createTime = now
	env.updateTime = now

	return env, nil
}

// RehydrateEnvironment reconstructs an existing environment from persisted
// state. It is intended for repository implementations loading aggregates from
// storage rather than creating brand-new environments.
func RehydrateEnvironment(snapshot EnvironmentSnapshot) (*Environment, error) {
	if snapshot.DesiredState == nil {
		return nil, ErrInvalidSpec
	}
	if snapshot.Status == nil {
		return nil, ErrInvalidState
	}

	env := &Environment{
		name:         snapshot.Name,
		envType:      snapshot.EnvType,
		description:  snapshot.Description,
		desiredState: cloneDesiredState(snapshot.DesiredState),
		status:       cloneStatus(snapshot.Status),
		generation:   snapshot.Generation,
		createTime:   snapshot.CreateTime,
		updateTime:   snapshot.UpdateTime,
		etag:         snapshot.ETag,
	}

	if err := env.Validate(); err != nil {
		return nil, err
	}

	return env, nil
}

// Type returns the environment type.
func (e *Environment) Type() EnvironmentType {
	return e.envType
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
func (e *Environment) DesiredState() *DesiredState {
	return cloneDesiredState(e.desiredState)
}

// Status returns the observed status.
func (e *Environment) Status() *EnvironmentStatus {
	return e.status
}

// Generation returns the current desired-state version.
func (e *Environment) Generation() int64 {
	return e.generation
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

// SetDesiredPresent accepts a present target and resets actual state to pending.
func (e *Environment) SetDesiredPresent(newDesiredState *DesiredState) error {
	if e.status.Desired == DesiredAbsent {
		return ErrInvalidState
	}

	previous := e.desiredState
	if newDesiredState != nil {
		e.desiredState = cloneDesiredState(newDesiredState)
		if err := e.Validate(); err != nil {
			e.desiredState = previous
			return err
		}
	}

	e.status.Desired = DesiredPresent
	e.status.State = StatePending
	e.status.Message = ""
	e.generation++
	e.updateTime = time.Now().UTC()
	return nil
}

// SetDesiredAbsent accepts an absent target and resets actual state to pending.
func (e *Environment) SetDesiredAbsent() error {
	e.status.Desired = DesiredAbsent
	e.status.State = StatePending
	e.status.Message = ""
	e.generation++
	e.updateTime = time.Now().UTC()
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
func (e *Environment) MarkReady(processedGeneration int64) error {
	if err := e.transitionTo(StateReady); err != nil {
		return err
	}

	e.status.Message = ""
	e.status.ObservedGeneration = processedGeneration
	e.status.LastSuccessTime = time.Now().UTC()
	return nil
}

// MarkFailed transitions the environment to failed and records the message.
func (e *Environment) MarkFailed(processedGeneration int64, msg string) error {
	if err := e.transitionTo(StateFailed); err != nil {
		return err
	}

	e.status.ObservedGeneration = processedGeneration
	e.status.Message = msg
	return nil
}

// MarkDeleting transitions the environment to deleting.
func (e *Environment) MarkDeleting() error {
	return e.transitionTo(StateDeleting)
}

// SetStatusMessage records a status message while the environment remains deleting.
func (e *Environment) SetStatusMessage(msg string) error {
	if e.status.State != StateDeleting {
		return ErrInvalidState
	}

	e.status.Message = msg
	return nil
}

// SetReconcilingMessage records a status message while the environment remains reconciling.
func (e *Environment) SetReconcilingMessage(msg string) error {
	if e.status.State != StateReconciling {
		return ErrInvalidState
	}

	e.status.Message = msg
	return nil
}

// Validate checks the desired state and cross-object references.
func (e *Environment) Validate() error {
	var errs []error

	artifactNames := make(map[string]struct{})
	artifactPortNames := make(map[string]map[string]struct{})
	for i, artifact := range e.desiredState.Artifacts {
		if err := artifact.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("artifacts[%d]: %w", i, err))
			continue
		}
		if _, exists := artifactNames[artifact.Name]; exists {
			errs = append(errs, fmt.Errorf("artifacts[%d]: name %q already exists", i, artifact.Name))
			continue
		}
		artifactNames[artifact.Name] = struct{}{}

		portNames := make(map[string]struct{})
		for _, port := range artifact.Ports {
			if port.Name == "" {
				continue
			}
			portNames[port.Name] = struct{}{}
		}
		artifactPortNames[artifact.Name] = portNames
	}

	for i, artifact := range e.desiredState.Artifacts {
		if artifact.HTTP == nil {
			continue
		}
		portNames := artifactPortNames[artifact.Name]
		for j, match := range artifact.HTTP.Matches {
			if _, ok := portNames[match.Backend]; !ok {
				errs = append(errs, fmt.Errorf("artifacts[%d].http.matches[%d]: backend %q does not reference artifact %q port", i, j, match.Backend, artifact.Name))
			}
		}
	}

	for i, infra := range e.desiredState.Infras {
		if err := infra.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("infras[%d]: %w", i, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%w: %w", ErrInvalidSpec, errors.Join(errs...))
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

func cloneDesiredState(state *DesiredState) *DesiredState {
	if state == nil {
		return nil
	}

	return &DesiredState{
		Artifacts: cloneArtifacts(state.Artifacts),
		Infras:    cloneInfras(state.Infras),
	}
}

func cloneStatus(status *EnvironmentStatus) *EnvironmentStatus {
	if status == nil {
		return nil
	}

	cloned := *status
	return &cloned
}

func cloneArtifacts(artifacts []*ArtifactSpec) []*ArtifactSpec {
	if len(artifacts) == 0 {
		return nil
	}

	cloned := make([]*ArtifactSpec, len(artifacts))
	for i, artifact := range artifacts {
		spec := *artifact
		if len(artifact.Ports) > 0 {
			spec.Ports = append([]ArtifactPortSpec(nil), artifact.Ports...)
		}
		if artifact.HTTP != nil {
			httpSpec := *artifact.HTTP
			if len(artifact.HTTP.Hostnames) > 0 {
				httpSpec.Hostnames = append([]string(nil), artifact.HTTP.Hostnames...)
			}
			if len(artifact.HTTP.Matches) > 0 {
				httpSpec.Matches = append([]HTTPRouteRule(nil), artifact.HTTP.Matches...)
			}
			spec.HTTP = &httpSpec
		}
		if len(artifact.Env) > 0 {
			spec.Env = make(map[string]string, len(artifact.Env))
			for k, v := range artifact.Env {
				spec.Env[k] = v
			}
		}
		cloned[i] = &spec
	}

	return cloned
}

func cloneInfras(infras []*InfraSpec) []*InfraSpec {
	if len(infras) == 0 {
		return nil
	}

	cloned := make([]*InfraSpec, len(infras))
	for i, infra := range infras {
		cp := *infra
		cloned[i] = &cp
	}
	return cloned
}

// ValidateEnvConflict checks that no artifact env key conflicts with a reserved variable name.
func (e *Environment) ValidateEnvConflict(reservedEnvVars []string) error {
	reserved := make(map[string]struct{}, len(reservedEnvVars))
	for _, v := range reservedEnvVars {
		reserved[v] = struct{}{}
	}

	var errs []error
	for i, artifact := range e.desiredState.Artifacts {
		for key := range artifact.Env {
			if _, ok := reserved[key]; ok {
				errs = append(errs, fmt.Errorf("artifacts[%d].env[%q]: conflicts with reserved environment variable", i, key))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%w: %w", ErrInvalidSpec, errors.Join(errs...))
	}
	return nil
}
