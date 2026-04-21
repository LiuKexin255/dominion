package domain

import "testing"

func TestEnvironmentDesired(t *testing.T) {
	tests := []struct {
		name string
		got  EnvironmentDesired
		want int
		text string
	}{
		{name: "unspecified", got: DesiredUnspecified, want: 0, text: "DesiredUnspecified"},
		{name: "present", got: DesiredPresent, want: 1, text: "DesiredPresent"},
		{name: "absent", got: DesiredAbsent, want: 2, text: "DesiredAbsent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			got := int(tt.got)

			// when
			text := tt.got.String()

			// then
			if got != tt.want {
				t.Fatalf("int(%v) = %d, want %d", tt.got, got, tt.want)
			}
			if text != tt.text {
				t.Fatalf("%v.String() = %q, want %q", tt.got, text, tt.text)
			}
		})
	}
}

func TestEnvironmentState_String(t *testing.T) {
	tests := []struct {
		name string
		got  EnvironmentState
		want string
	}{
		{name: "unspecified", got: StateUnspecified, want: "StateUnspecified"},
		{name: "pending", got: StatePending, want: "StatePending"},
		{name: "reconciling", got: StateReconciling, want: "StateReconciling"},
		{name: "ready", got: StateReady, want: "StateReady"},
		{name: "failed", got: StateFailed, want: "StateFailed"},
		{name: "deleting", got: StateDeleting, want: "StateDeleting"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			got := tt.got.String()

			// when
			// then
			if got != tt.want {
				t.Fatalf("%v.String() = %q, want %q", tt.got, got, tt.want)
			}
		})
	}
}

func TestCanTransition(t *testing.T) {
	tests := []struct {
		name string
		from EnvironmentState
		to   EnvironmentState
		want bool
	}{
		{name: "pending target starts reconciling", from: StatePending, to: StateReconciling, want: true},
		{name: "reconciling to reconciling", from: StateReconciling, to: StateReconciling, want: true},
		{name: "reconciling to ready", from: StateReconciling, to: StateReady, want: true},
		{name: "reconciling to failed", from: StateReconciling, to: StateFailed, want: true},
		{name: "ready to reconciling", from: StateReady, to: StateReconciling, want: true},
		{name: "failed to reconciling", from: StateFailed, to: StateReconciling, want: true},
		{name: "pending target starts deleting", from: StatePending, to: StateDeleting, want: true},
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
