// Package domain contains the deploy service domain model.
package domain

import "strconv"

// EnvironmentDesired describes the target lifecycle state of an environment.
type EnvironmentDesired int

const (
	// DesiredUnspecified indicates that no desired target has been assigned yet.
	DesiredUnspecified EnvironmentDesired = iota
	// DesiredPresent indicates that the environment should exist.
	DesiredPresent
	// DesiredAbsent indicates that the environment should be deleted.
	DesiredAbsent
)

// String returns the symbolic name of the desired environment target.
func (d EnvironmentDesired) String() string {
	switch d {
	case DesiredUnspecified:
		return "DesiredUnspecified"
	case DesiredPresent:
		return "DesiredPresent"
	case DesiredAbsent:
		return "DesiredAbsent"
	default:
		return "EnvironmentDesired(" + strconv.Itoa(int(d)) + ")"
	}
}

// EnvironmentState describes the observed lifecycle state of an environment.
type EnvironmentState int

const (
	// StateUnspecified indicates that no state has been assigned yet.
	StateUnspecified EnvironmentState = 0
	// StatePending indicates that the environment is waiting to be reconciled.
	StatePending EnvironmentState = 1
	// StateReconciling indicates that the environment is currently being reconciled.
	StateReconciling EnvironmentState = 2
	// StateReady indicates that the environment is ready.
	StateReady EnvironmentState = 3
	// StateFailed indicates that reconciliation failed.
	StateFailed EnvironmentState = 4
	// StateDeleting indicates that the environment is being deleted.
	StateDeleting EnvironmentState = 5
)

// String returns the symbolic name of the observed environment state.
func (s EnvironmentState) String() string {
	switch s {
	case StateUnspecified:
		return "StateUnspecified"
	case StatePending:
		return "StatePending"
	case StateReconciling:
		return "StateReconciling"
	case StateReady:
		return "StateReady"
	case StateFailed:
		return "StateFailed"
	case StateDeleting:
		return "StateDeleting"
	default:
		return "EnvironmentState(" + strconv.Itoa(int(s)) + ")"
	}
}

// CanTransition reports whether an environment can move from one state to another.
func CanTransition(from, to EnvironmentState) bool {
	switch {
	case from == StatePending && to == StateReconciling:
		return true
	case from == StateReconciling && (to == StateReconciling || to == StateReady || to == StateFailed):
		return true
	case (from == StateReady || from == StateFailed) && to == StateReconciling:
		return true
	case isActiveState(from) && to == StateDeleting:
		return true
	default:
		return false
	}
}

func isActiveState(state EnvironmentState) bool {
	switch state {
	case StatePending, StateReconciling, StateReady, StateFailed:
		return true
	default:
		return false
	}
}
