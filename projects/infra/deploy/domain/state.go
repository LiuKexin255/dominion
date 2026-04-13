// Package domain contains the deploy service domain model.
package domain

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
