package domain

import "time"

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
