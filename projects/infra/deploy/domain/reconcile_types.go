package domain

import (
	"strconv"
	"time"
)

// RolloutState describes the result of a single rollout check.
type RolloutState int

const (
	// RolloutReady indicates the rollout has completed successfully.
	RolloutReady RolloutState = iota
	// RolloutWaiting indicates the rollout is still in progress.
	RolloutWaiting
	// RolloutFailed indicates the rollout has failed.
	RolloutFailed
)

// RolloutStatus is the result of a single CheckRollout call.
type RolloutStatus struct {
	// State is the rollout state.
	State RolloutState
	// Message provides additional details about the rollout state.
	Message string
	// Services carries the observed rollout state of each service.
	Services []*ServiceStatus
}

// ServiceKind identifies the source of a service within an environment.
type ServiceKind int

const (
	// ServiceKindUnspecified indicates that no kind has been assigned yet.
	ServiceKindUnspecified ServiceKind = iota
	// ServiceKindArtifact indicates an application service declared as an artifact.
	ServiceKindArtifact
	// ServiceKindInfra indicates an infrastructure service declared as an infra.
	ServiceKindInfra
)

// String returns the symbolic name of the service kind.
func (k ServiceKind) String() string {
	switch k {
	case ServiceKindArtifact:
		return "ServiceKindArtifact"
	case ServiceKindInfra:
		return "ServiceKindInfra"
	default:
		return "ServiceKind(" + strconv.Itoa(int(k)) + ")"
	}
}

// ServiceRolloutState describes the observed rollout state of a single service.
type ServiceRolloutState int

const (
	// ServiceRolloutStateUnspecified indicates that no state has been assigned yet.
	ServiceRolloutStateUnspecified ServiceRolloutState = iota
	// ServiceRolloutStatePending indicates that resources have been submitted
	// and the first rollout observation has not happened yet.
	ServiceRolloutStatePending
	// ServiceRolloutStateReady indicates that the service rollout has completed.
	ServiceRolloutStateReady
	// ServiceRolloutStateWaiting indicates that the service rollout is still in progress.
	ServiceRolloutStateWaiting
	// ServiceRolloutStateFailed indicates that the service rollout has failed.
	ServiceRolloutStateFailed
)

// String returns the symbolic name of the service rollout state.
func (s ServiceRolloutState) String() string {
	switch s {
	case ServiceRolloutStatePending:
		return "ServiceRolloutStatePending"
	case ServiceRolloutStateReady:
		return "ServiceRolloutStateReady"
	case ServiceRolloutStateWaiting:
		return "ServiceRolloutStateWaiting"
	case ServiceRolloutStateFailed:
		return "ServiceRolloutStateFailed"
	default:
		return "ServiceRolloutState(" + strconv.Itoa(int(s)) + ")"
	}
}

// ServiceStatus describes the observed rollout state of a single service
// (an artifact or an infra) in an environment.
type ServiceStatus struct {
	// Name is the service name (ArtifactSpec.Name or InfraSpec.Name).
	Name string
	// App is the application the service belongs to.
	App string
	// Kind identifies the service source (artifact or infra).
	Kind ServiceKind
	// State is the observed rollout state.
	State ServiceRolloutState
	// Message provides details about the state (e.g. "可用副本不足（available: 0/1）").
	Message string
}

// ServicesEqual reports whether two per-service status lists are equal. It
// compares Name, App, Kind, State and Message of each element in order.
// 供 retainWaitingRollout 早退条件比较使用（specs/032-guitar-deploy-failure-state/research.md 决策 R11）。
func ServicesEqual(a, b []*ServiceStatus) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] == nil || b[i] == nil {
			if a[i] != b[i] {
				return false
			}
			continue
		}
		if a[i].Name != b[i].Name || a[i].App != b[i].App || a[i].Kind != b[i].Kind ||
			a[i].State != b[i].State || a[i].Message != b[i].Message {
			return false
		}
	}

	return true
}

// ProcessResult describes the outcome of a reconcile step.
type ProcessResult struct {
	// Changed indicates whether the environment state was changed.
	Changed bool
	// Terminal indicates whether processing is complete for this work item.
	Terminal bool
	// RequeueAfter specifies a delay before the next processing attempt.
	RequeueAfter time.Duration
	// Source is the origin of the work item that led to this result.
	Source WorkItemSource
}
