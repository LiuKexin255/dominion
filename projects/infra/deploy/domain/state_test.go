package domain

import "testing"

func TestCanTransition(t *testing.T) {
	tests := []struct {
		name string
		from EnvironmentState
		to   EnvironmentState
		want bool
	}{
		{name: "pending to reconciling", from: StatePending, to: StateReconciling, want: true},
		{name: "reconciling to reconciling", from: StateReconciling, to: StateReconciling, want: true},
		{name: "reconciling to ready", from: StateReconciling, to: StateReady, want: true},
		{name: "reconciling to failed", from: StateReconciling, to: StateFailed, want: true},
		{name: "ready to reconciling", from: StateReady, to: StateReconciling, want: true},
		{name: "failed to reconciling", from: StateFailed, to: StateReconciling, want: true},
		{name: "pending to deleting", from: StatePending, to: StateDeleting, want: true},
		{name: "reconciling to deleting", from: StateReconciling, to: StateDeleting, want: true},
		{name: "ready to deleting", from: StateReady, to: StateDeleting, want: true},
		{name: "failed to deleting", from: StateFailed, to: StateDeleting, want: true},
		{name: "pending to ready", from: StatePending, to: StateReady, want: false},
		{name: "deleting to ready", from: StateDeleting, to: StateReady, want: false},
		{name: "unspecified to pending", from: StateUnspecified, to: StatePending, want: false},
		{name: "deleting to deleting", from: StateDeleting, to: StateDeleting, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			from := tt.from
			to := tt.to

			// when
			got := CanTransition(from, to)

			// then
			if got != tt.want {
				t.Fatalf("CanTransition(%v, %v) = %v, want %v", from, to, got, tt.want)
			}
		})
	}
}
