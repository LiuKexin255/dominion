package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewEnvironment(t *testing.T) {
	name, err := NewEnvironmentName("scope1", "dev")
	if err != nil {
		t.Fatalf("NewEnvironmentName() unexpected error: %v", err)
	}

	tests := []struct {
		name         string
		desiredState DesiredState
		wantErr      error
	}{
		{
			name: "valid desired state",
			desiredState: DesiredState{
				Artifacts: []*ArtifactSpec{{
					Name:     "api",
					App:      "demo",
					Image:    "repo/demo:v1",
					Ports:    []ArtifactPortSpec{{Name: "http", Port: 8080}},
					Replicas: 1,
					HTTP: &ArtifactHTTPSpec{
						Hostnames: []string{"example.com"},
						Matches: []HTTPRouteRule{{
							Backend: "http",
							Path:    HTTPPathRule{Type: HTTPPathRuleTypePathPrefix, Value: "/"},
						}},
					},
				}},
				Infras: []*InfraSpec{{
					Resource: "redis",
					Name:     "cache",
				}},
			},
		},
		{
			name: "invalid nested artifact spec",
			desiredState: DesiredState{
				Artifacts: []*ArtifactSpec{{
					Name:  "api",
					App:   "demo",
					Ports: []ArtifactPortSpec{{Name: "http", Port: 8080}},
				}},
			},
			wantErr: ErrInvalidSpec,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			desiredState := tt.desiredState

			// when
			env, err := NewEnvironment(name, EnvironmentTypeProd, "demo environment", &desiredState)

			// then
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("NewEnvironment() error = %v, want %v", err, tt.wantErr)
				}
				if env != nil {
					t.Fatalf("NewEnvironment() env = %#v, want nil", env)
				}
				return
			}

			if err != nil {
				t.Fatalf("NewEnvironment() unexpected error: %v", err)
			}
			if got := env.Generation(); got != 1 {
				t.Fatalf("Generation() = %d, want 1", got)
			}
			if env.status.Desired != DesiredPresent {
				t.Fatalf("status.Desired = %v, want %v", env.status.Desired, DesiredPresent)
			}
			if env.status.State != StatePending {
				t.Fatalf("status.State = %v, want %v", env.status.State, StatePending)
			}
			if env.status.ObservedGeneration != 0 {
				t.Fatalf("status.ObservedGeneration = %d, want 0", env.status.ObservedGeneration)
			}
			if env.createTime.IsZero() {
				t.Fatalf("createTime should be set")
			}
			if env.updateTime.IsZero() {
				t.Fatalf("updateTime should be set")
			}
			if env.createTime != env.updateTime {
				t.Fatalf("createTime = %v, updateTime = %v, want equal", env.createTime, env.updateTime)
			}
			if env.description != "demo environment" {
				t.Fatalf("description = %q, want %q", env.description, "demo environment")
			}
		})
	}
}

func TestNewEnvironment_WithValidTypes(t *testing.T) {
	name, err := NewEnvironmentName("scope1", "dev")
	if err != nil {
		t.Fatalf("NewEnvironmentName() unexpected error: %v", err)
	}

	tests := []struct {
		name    string
		envType EnvironmentType
	}{
		{name: "prod", envType: EnvironmentTypeProd},
		{name: "test", envType: EnvironmentTypeTest},
		{name: "dev", envType: EnvironmentTypeDev},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			desiredState := &DesiredState{
				Artifacts: []*ArtifactSpec{{
					Name:     "api",
					App:      "demo",
					Image:    "repo/demo:v1",
					Ports:    []ArtifactPortSpec{{Name: "http", Port: 8080}},
					Replicas: 1,
				}},
			}

			// when
			env, err := NewEnvironment(name, tt.envType, "demo environment", desiredState)

			// then
			if err != nil {
				t.Fatalf("NewEnvironment() unexpected error: %v", err)
			}
			if got := env.Type(); got != tt.envType {
				t.Fatalf("Type() = %v, want %v", got, tt.envType)
			}
		})
	}
}

func TestNewEnvironment_RejectUnspecified(t *testing.T) {
	name, err := NewEnvironmentName("scope1", "dev")
	if err != nil {
		t.Fatalf("NewEnvironmentName() unexpected error: %v", err)
	}

	// given
	desiredState := &DesiredState{
		Artifacts: []*ArtifactSpec{{
			Name:     "api",
			App:      "demo",
			Image:    "repo/demo:v1",
			Ports:    []ArtifactPortSpec{{Name: "http", Port: 8080}},
			Replicas: 1,
		}},
	}

	// when
	env, err := NewEnvironment(name, EnvironmentTypeUnspecified, "demo environment", desiredState)

	// then
	if !errors.Is(err, ErrInvalidType) {
		t.Fatalf("NewEnvironment() error = %v, want %v", err, ErrInvalidType)
	}
	if env != nil {
		t.Fatalf("NewEnvironment() env = %#v, want nil", env)
	}
}

func TestEnvironment_Type(t *testing.T) {
	// given
	env := mustNewEnvironment(t)

	// when
	got := env.Type()

	// then
	if got != EnvironmentTypeProd {
		t.Fatalf("Type() = %v, want %v", got, EnvironmentTypeProd)
	}
}

func TestEnvironment_SetDesiredPresent(t *testing.T) {
	tests := []struct {
		name              string
		prepare           func(*testing.T, *Environment)
		newDesiredState   *DesiredState
		wantErr           error
		wantGeneration    int64
		wantDesired       EnvironmentDesired
		wantState         EnvironmentState
		wantImage         string
		wantMessage       string
		wantPreserveReady bool
	}{
		{
			name: "ready environment accepts new desired state",
			prepare: func(t *testing.T, env *Environment) {
				if err := env.MarkReconciling(); err != nil {
					t.Fatalf("MarkReconciling() unexpected error: %v", err)
				}
				if err := env.MarkReady(env.Generation()); err != nil {
					t.Fatalf("MarkReady() unexpected error: %v", err)
				}
				env.status.Message = "stale error"
			},
			newDesiredState: &DesiredState{
				Artifacts: []*ArtifactSpec{{
					Name:     "api",
					App:      "demo",
					Image:    "repo/demo:v2",
					Ports:    []ArtifactPortSpec{{Name: "http", Port: 9090}},
					Replicas: 2,
				}},
			},
			wantGeneration:    2,
			wantDesired:       DesiredPresent,
			wantState:         StatePending,
			wantImage:         "repo/demo:v2",
			wantMessage:       "",
			wantPreserveReady: true,
		},
		{
			name: "nil desired state keeps original content",
			prepare: func(t *testing.T, env *Environment) {
				if err := env.MarkReconciling(); err != nil {
					t.Fatalf("MarkReconciling() unexpected error: %v", err)
				}
				if err := env.MarkFailed(env.Generation(), "apply failed"); err != nil {
					t.Fatalf("MarkFailed() unexpected error: %v", err)
				}
			},
			wantGeneration: 2,
			wantDesired:    DesiredPresent,
			wantState:      StatePending,
			wantImage:      "repo/demo:v1",
			wantMessage:    "",
		},
		{
			name: "desired absent rejects present update",
			prepare: func(t *testing.T, env *Environment) {
				if err := env.SetDesiredAbsent(); err != nil {
					t.Fatalf("SetDesiredAbsent() unexpected error: %v", err)
				}
			},
			newDesiredState: &DesiredState{
				Artifacts: []*ArtifactSpec{{
					Name:     "api",
					App:      "demo",
					Image:    "repo/demo:v3",
					Ports:    []ArtifactPortSpec{{Name: "http", Port: 7070}},
					Replicas: 3,
				}},
			},
			wantErr:        ErrInvalidState,
			wantGeneration: 2,
			wantDesired:    DesiredAbsent,
			wantState:      StatePending,
			wantImage:      "repo/demo:v1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			env := mustNewEnvironment(t)
			if tt.prepare != nil {
				tt.prepare(t, env)
			}
			previousGeneration := env.Generation()
			previousUpdateTime := env.UpdateTime()
			previousLastSuccessTime := env.Status().LastSuccessTime

			// when
			err := env.SetDesiredPresent(tt.newDesiredState)

			// then
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Fatalf("SetDesiredPresent() error = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("SetDesiredPresent() unexpected error: %v", err)
			}
			if got := env.Generation(); got != tt.wantGeneration {
				t.Fatalf("Generation() = %d, want %d", got, tt.wantGeneration)
			}
			if env.Status().Desired != tt.wantDesired {
				t.Fatalf("status.Desired = %v, want %v", env.Status().Desired, tt.wantDesired)
			}
			if env.Status().State != tt.wantState {
				t.Fatalf("status.State = %v, want %v", env.Status().State, tt.wantState)
			}
			if env.Status().Message != tt.wantMessage {
				t.Fatalf("status.Message = %q, want %q", env.Status().Message, tt.wantMessage)
			}
			if got := env.DesiredState().Artifacts[0].Image; got != tt.wantImage {
				t.Fatalf("DesiredState().Artifacts[0].Image = %q, want %q", got, tt.wantImage)
			}
			if tt.wantErr == nil && !env.UpdateTime().After(previousUpdateTime) {
				t.Fatalf("UpdateTime() = %v, want after %v", env.UpdateTime(), previousUpdateTime)
			}
			if tt.wantErr != nil && env.UpdateTime() != previousUpdateTime {
				t.Fatalf("UpdateTime() = %v, want %v", env.UpdateTime(), previousUpdateTime)
			}
			if tt.wantPreserveReady && env.Status().LastSuccessTime != previousLastSuccessTime {
				t.Fatalf("LastSuccessTime = %v, want %v", env.Status().LastSuccessTime, previousLastSuccessTime)
			}
			if tt.wantErr != nil && env.Generation() != previousGeneration {
				t.Fatalf("Generation() = %d, want unchanged %d", env.Generation(), previousGeneration)
			}
			if got := env.Type(); got != EnvironmentTypeProd {
				t.Fatalf("Type() = %v, want %v", got, EnvironmentTypeProd)
			}
		})
	}
}

func TestEnvironment_SetDesiredAbsent(t *testing.T) {
	tests := []struct {
		name           string
		prepare        func(*testing.T, *Environment)
		wantGeneration int64
		wantState      EnvironmentState
		wantDesired    EnvironmentDesired
		wantImage      string
	}{
		{
			name:           "ready environment becomes pending absent",
			wantGeneration: 2,
			wantState:      StatePending,
			wantDesired:    DesiredAbsent,
			wantImage:      "repo/demo:v1",
			prepare: func(t *testing.T, env *Environment) {
				if err := env.MarkReconciling(); err != nil {
					t.Fatalf("MarkReconciling() unexpected error: %v", err)
				}
				if err := env.MarkReady(env.Generation()); err != nil {
					t.Fatalf("MarkReady() unexpected error: %v", err)
				}
				env.status.Message = "old message"
			},
		},
		{
			name:           "failed environment keeps desired state content",
			wantGeneration: 2,
			wantState:      StatePending,
			wantDesired:    DesiredAbsent,
			wantImage:      "repo/demo:v1",
			prepare: func(t *testing.T, env *Environment) {
				if err := env.MarkReconciling(); err != nil {
					t.Fatalf("MarkReconciling() unexpected error: %v", err)
				}
				if err := env.MarkFailed(env.Generation(), "apply failed"); err != nil {
					t.Fatalf("MarkFailed() unexpected error: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			env := mustNewEnvironment(t)
			if tt.prepare != nil {
				tt.prepare(t, env)
			}
			previousLastSuccessTime := env.Status().LastSuccessTime
			previousUpdateTime := env.UpdateTime()

			// when
			err := env.SetDesiredAbsent()

			// then
			if err != nil {
				t.Fatalf("SetDesiredAbsent() unexpected error: %v", err)
			}
			if got := env.Generation(); got != tt.wantGeneration {
				t.Fatalf("Generation() = %d, want %d", got, tt.wantGeneration)
			}
			if env.Status().Desired != tt.wantDesired {
				t.Fatalf("status.Desired = %v, want %v", env.Status().Desired, tt.wantDesired)
			}
			if env.Status().State != tt.wantState {
				t.Fatalf("status.State = %v, want %v", env.Status().State, tt.wantState)
			}
			if env.Status().Message != "" {
				t.Fatalf("status.Message = %q, want empty", env.Status().Message)
			}
			if got := env.DesiredState().Artifacts[0].Image; got != tt.wantImage {
				t.Fatalf("DesiredState().Artifacts[0].Image = %q, want %q", got, tt.wantImage)
			}
			if !env.UpdateTime().After(previousUpdateTime) {
				t.Fatalf("UpdateTime() = %v, want after %v", env.UpdateTime(), previousUpdateTime)
			}
			if env.Status().LastSuccessTime != previousLastSuccessTime {
				t.Fatalf("LastSuccessTime = %v, want %v", env.Status().LastSuccessTime, previousLastSuccessTime)
			}
		})
	}
}

func TestEnvironment_MarkReconciling(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func(*testing.T, *Environment)
		wantErr   error
		wantState EnvironmentState
	}{
		{name: "pending to reconciling", wantState: StateReconciling},
		{
			name: "deleting to reconciling is invalid",
			prepare: func(t *testing.T, env *Environment) {
				if err := env.MarkDeleting(); err != nil {
					t.Fatalf("MarkDeleting() unexpected error: %v", err)
				}
			},
			wantErr:   ErrInvalidState,
			wantState: StateDeleting,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			env := mustNewEnvironment(t)
			if tt.prepare != nil {
				tt.prepare(t, env)
			}

			// when
			err := env.MarkReconciling()

			// then
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Fatalf("MarkReconciling() error = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("MarkReconciling() unexpected error: %v", err)
			}
			if env.status.State != tt.wantState {
				t.Fatalf("status.State = %v, want %v", env.status.State, tt.wantState)
			}
			if tt.wantErr == nil && env.status.LastReconcileTime.IsZero() {
				t.Fatalf("LastReconcileTime should be set")
			}
		})
	}
}

func TestEnvironment_MarkReady(t *testing.T) {
	tests := []struct {
		name                   string
		prepare                func(*testing.T, *Environment)
		processedGeneration    int64
		wantErr                error
		wantState              EnvironmentState
		wantObservedGeneration int64
	}{
		{
			name: "reconciling to ready",
			prepare: func(t *testing.T, env *Environment) {
				if err := env.MarkReconciling(); err != nil {
					t.Fatalf("MarkReconciling() unexpected error: %v", err)
				}
			},
			processedGeneration:    7,
			wantState:              StateReady,
			wantObservedGeneration: 7,
		},
		{name: "pending to ready is invalid", processedGeneration: 7, wantErr: ErrInvalidState, wantState: StatePending},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			env := mustNewEnvironment(t)
			if tt.prepare != nil {
				tt.prepare(t, env)
			}

			// when
			err := env.MarkReady(tt.processedGeneration)

			// then
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Fatalf("MarkReady() error = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("MarkReady() unexpected error: %v", err)
			}
			if env.status.State != tt.wantState {
				t.Fatalf("status.State = %v, want %v", env.status.State, tt.wantState)
			}
			if tt.wantErr == nil && env.status.LastSuccessTime.IsZero() {
				t.Fatalf("LastSuccessTime should be set")
			}
			if env.status.ObservedGeneration != tt.wantObservedGeneration {
				t.Fatalf("status.ObservedGeneration = %d, want %d", env.status.ObservedGeneration, tt.wantObservedGeneration)
			}
		})
	}
}

func TestEnvironment_MarkFailed(t *testing.T) {
	tests := []struct {
		name                   string
		prepare                func(*testing.T, *Environment)
		processedGeneration    int64
		message                string
		wantErr                error
		wantState              EnvironmentState
		wantMessage            string
		wantObservedGeneration int64
	}{
		{
			name: "reconciling to failed",
			prepare: func(t *testing.T, env *Environment) {
				if err := env.MarkReconciling(); err != nil {
					t.Fatalf("MarkReconciling() unexpected error: %v", err)
				}
			},
			processedGeneration:    7,
			message:                "apply failed",
			wantState:              StateFailed,
			wantMessage:            "apply failed",
			wantObservedGeneration: 7,
		},
		{
			name:                "pending to failed is invalid",
			processedGeneration: 7,
			message:             "apply failed",
			wantErr:             ErrInvalidState,
			wantState:           StatePending,
			wantMessage:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			env := mustNewEnvironment(t)
			if tt.prepare != nil {
				tt.prepare(t, env)
			}

			// when
			err := env.MarkFailed(tt.processedGeneration, tt.message)

			// then
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Fatalf("MarkFailed() error = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("MarkFailed() unexpected error: %v", err)
			}
			if env.status.State != tt.wantState {
				t.Fatalf("status.State = %v, want %v", env.status.State, tt.wantState)
			}
			if env.status.Message != tt.wantMessage {
				t.Fatalf("status.Message = %q, want %q", env.status.Message, tt.wantMessage)
			}
			if env.status.ObservedGeneration != tt.wantObservedGeneration {
				t.Fatalf("status.ObservedGeneration = %d, want %d", env.status.ObservedGeneration, tt.wantObservedGeneration)
			}
		})
	}
}

func TestEnvironment_SetStatusMessage(t *testing.T) {
	tests := []struct {
		name        string
		prepare     func(*testing.T, *Environment)
		message     string
		wantErr     error
		wantState   EnvironmentState
		wantMessage string
		checkTime   bool
	}{
		{
			name:    "deleting sets message",
			message: "delete failed",
			prepare: func(t *testing.T, env *Environment) {
				if err := env.MarkDeleting(); err != nil {
					t.Fatalf("MarkDeleting() unexpected error: %v", err)
				}
			},
			wantState:   StateDeleting,
			wantMessage: "delete failed",
			checkTime:   true,
		},
		{
			name:        "pending is invalid",
			message:     "delete failed",
			wantErr:     ErrInvalidState,
			wantState:   StatePending,
			wantMessage: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			env := mustNewEnvironment(t)
			if tt.prepare != nil {
				tt.prepare(t, env)
			}
			previousUpdateTime := env.updateTime

			// when
			err := env.SetStatusMessage(tt.message)

			// then
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Fatalf("SetStatusMessage() error = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("SetStatusMessage() unexpected error: %v", err)
			}
			if env.status.State != tt.wantState {
				t.Fatalf("status.State = %v, want %v", env.status.State, tt.wantState)
			}
			if env.status.Message != tt.wantMessage {
				t.Fatalf("status.Message = %q, want %q", env.status.Message, tt.wantMessage)
			}
			if tt.checkTime && !env.updateTime.Equal(previousUpdateTime) {
				t.Fatalf("updateTime = %v, want %v", env.updateTime, previousUpdateTime)
			}
		})
	}
}

func TestSetReconcilingMessage_ReconcilingState(t *testing.T) {
	// given
	env := mustNewEnvironment(t)
	if err := env.MarkReconciling(); err != nil {
		t.Fatalf("MarkReconciling() unexpected error: %v", err)
	}

	// when
	err := env.SetReconcilingMessage("applying deployment")

	// then
	if err != nil {
		t.Fatalf("SetReconcilingMessage() unexpected error: %v", err)
	}
	if env.status.State != StateReconciling {
		t.Fatalf("status.State = %v, want %v", env.status.State, StateReconciling)
	}
	if env.status.Message != "applying deployment" {
		t.Fatalf("status.Message = %q, want %q", env.status.Message, "applying deployment")
	}
}

func TestSetReconcilingMessage_NonReconcilingState(t *testing.T) {
	// given
	env := mustNewEnvironment(t)

	// when
	err := env.SetReconcilingMessage("applying deployment")

	// then
	if err != ErrInvalidState {
		t.Fatalf("SetReconcilingMessage() error = %v, want %v", err, ErrInvalidState)
	}
	if env.status.State != StatePending {
		t.Fatalf("status.State = %v, want %v", env.status.State, StatePending)
	}
	if env.status.Message != "" {
		t.Fatalf("status.Message = %q, want empty", env.status.Message)
	}
}

func TestSetReconcilingMessage_ReconcilingState_OverridesMessage(t *testing.T) {
	// given
	env := mustNewEnvironment(t)
	if err := env.MarkReconciling(); err != nil {
		t.Fatalf("MarkReconciling() unexpected error: %v", err)
	}
	if err := env.SetReconcilingMessage("step 1"); err != nil {
		t.Fatalf("SetReconcilingMessage() unexpected error = %v", err)
	}

	// when
	err := env.SetReconcilingMessage("step 2")

	// then
	if err != nil {
		t.Fatalf("SetReconcilingMessage() unexpected error: %v", err)
	}
	if env.status.Message != "step 2" {
		t.Fatalf("status.Message = %q, want %q", env.status.Message, "step 2")
	}
}

func TestEnvironment_MarkDeleting(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func(*testing.T, *Environment)
		wantErr   error
		wantState EnvironmentState
	}{
		{name: "pending to deleting", wantState: StateDeleting},
		{
			name: "deleting to deleting is invalid",
			prepare: func(t *testing.T, env *Environment) {
				if err := env.MarkDeleting(); err != nil {
					t.Fatalf("MarkDeleting() unexpected error: %v", err)
				}
			},
			wantErr:   ErrInvalidState,
			wantState: StateDeleting,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			env := mustNewEnvironment(t)
			if tt.prepare != nil {
				tt.prepare(t, env)
			}

			// when
			err := env.MarkDeleting()

			// then
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Fatalf("MarkDeleting() error = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("MarkDeleting() unexpected error: %v", err)
			}
			if env.status.State != tt.wantState {
				t.Fatalf("status.State = %v, want %v", env.status.State, tt.wantState)
			}
		})
	}
}

func TestEnvironment_Validate(t *testing.T) {
	name, err := NewEnvironmentName("scope1", "dev")
	if err != nil {
		t.Fatalf("NewEnvironmentName() unexpected error: %v", err)
	}

	tests := []struct {
		name         string
		desiredState DesiredState
		wantErr      error
		wantContains string
	}{
		{
			name: "backend references existing artifact port",
			desiredState: DesiredState{
				Artifacts: []*ArtifactSpec{{
					Name:     "api",
					App:      "demo",
					Image:    "repo/demo:v1",
					Ports:    []ArtifactPortSpec{{Name: "http", Port: 8080}},
					Replicas: 1,
					HTTP: &ArtifactHTTPSpec{
						Hostnames: []string{"example.com"},
						Matches: []HTTPRouteRule{{
							Backend: "http",
							Path:    HTTPPathRule{Type: HTTPPathRuleTypePathPrefix, Value: "/"},
						}},
					},
				}},
			},
		},
		{
			name: "backend reference missing artifact port",
			desiredState: DesiredState{
				Artifacts: []*ArtifactSpec{{
					Name:     "api",
					App:      "demo",
					Image:    "repo/demo:v1",
					Ports:    []ArtifactPortSpec{{Name: "http", Port: 8080}},
					Replicas: 1,
					HTTP: &ArtifactHTTPSpec{
						Hostnames: []string{"example.com"},
						Matches: []HTTPRouteRule{{
							Backend: "worker",
							Path:    HTTPPathRule{Type: HTTPPathRuleTypePathPrefix, Value: "/"},
						}},
					},
				}},
			},
			wantErr:      ErrInvalidSpec,
			wantContains: `backend "worker" does not reference artifact "api" port`,
		},
		{
			name: "http backend with no ports fails",
			desiredState: DesiredState{
				Artifacts: []*ArtifactSpec{{
					Name:     "api",
					App:      "demo",
					Image:    "repo/demo:v1",
					Replicas: 1,
					HTTP: &ArtifactHTTPSpec{
						Hostnames: []string{"example.com"},
						Matches: []HTTPRouteRule{{
							Backend: "http",
							Path:    HTTPPathRule{Type: HTTPPathRuleTypePathPrefix, Value: "/"},
						}},
					},
				}},
			},
			wantErr:      ErrInvalidSpec,
			wantContains: `backend "http" does not reference artifact "api" port`,
		},
		{
			name: "artifact name must be unique",
			desiredState: DesiredState{
				Artifacts: []*ArtifactSpec{
					{
						Name:     "api",
						App:      "demo",
						Image:    "repo/demo:v1",
						Ports:    []ArtifactPortSpec{{Name: "http", Port: 8080}},
						Replicas: 1,
					},
					{
						Name:     "api",
						App:      "demo",
						Image:    "repo/demo:v2",
						Ports:    []ArtifactPortSpec{{Name: "http", Port: 9090}},
						Replicas: 1,
					},
				},
			},
			wantErr:      ErrInvalidSpec,
			wantContains: `name "api" already exists`,
		},
		{
			name: "http backend must reference same artifact port",
			desiredState: DesiredState{
				Artifacts: []*ArtifactSpec{
					{
						Name:     "api",
						App:      "demo",
						Image:    "repo/demo:v1",
						Ports:    []ArtifactPortSpec{{Name: "http", Port: 8080}},
						Replicas: 1,
						HTTP: &ArtifactHTTPSpec{
							Hostnames: []string{"example.com"},
							Matches: []HTTPRouteRule{{
								Backend: "metrics",
								Path:    HTTPPathRule{Type: HTTPPathRuleTypePathPrefix, Value: "/"},
							}},
						},
					},
					{
						Name:     "worker",
						App:      "demo",
						Image:    "repo/worker:v1",
						Ports:    []ArtifactPortSpec{{Name: "metrics", Port: 9090}},
						Replicas: 1,
					},
				},
			},
			wantErr:      ErrInvalidSpec,
			wantContains: `backend "metrics" does not reference artifact "api" port`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			env := &Environment{
				name:         name,
				description:  "demo environment",
				desiredState: cloneDesiredState(&tt.desiredState),
				status: &EnvironmentStatus{
					State: StatePending,
				},
			}

			// when
			err := env.Validate()

			// then
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Validate() error = %v, want %v", err, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantContains) {
					t.Fatalf("Validate() error = %q, want substring %q", err.Error(), tt.wantContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestRehydrateEnvironment(t *testing.T) {
	name, err := NewEnvironmentName("scope1", "dev")
	if err != nil {
		t.Fatalf("NewEnvironmentName() unexpected error: %v", err)
	}

	createTime := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	updateTime := createTime.Add(5 * time.Minute)
	status := &EnvironmentStatus{
		Desired:            DesiredPresent,
		State:              StateReady,
		ObservedGeneration: 3,
		Message:            "ready",
		LastReconcileTime:  createTime.Add(2 * time.Minute),
		LastSuccessTime:    createTime.Add(3 * time.Minute),
	}
	desiredState := &DesiredState{
		Artifacts: []*ArtifactSpec{{
			Name:     "api",
			App:      "demo",
			Image:    "repo/demo:v1",
			Ports:    []ArtifactPortSpec{{Name: "http", Port: 8080}},
			Replicas: 1,
			HTTP: &ArtifactHTTPSpec{
				Hostnames: []string{"example.com"},
				Matches: []HTTPRouteRule{{
					Backend: "http",
					Path:    HTTPPathRule{Type: HTTPPathRuleTypePathPrefix, Value: "/"},
				}},
			},
		}},
		Infras: []*InfraSpec{{
			Resource: "postgres",
			Name:     "db",
		}},
	}

	env, err := RehydrateEnvironment(EnvironmentSnapshot{
		Name:         name,
		EnvType:      EnvironmentTypeTest,
		Description:  "demo environment",
		DesiredState: desiredState,
		Status:       status,
		Generation:   3,
		CreateTime:   createTime,
		UpdateTime:   updateTime,
		ETag:         "etag-1",
	})
	if err != nil {
		t.Fatalf("RehydrateEnvironment() unexpected error: %v", err)
	}

	if env.name != name {
		t.Fatalf("name = %#v, want %#v", env.name, name)
	}
	if env.description != "demo environment" {
		t.Fatalf("description = %q, want %q", env.description, "demo environment")
	}
	if env.envType != EnvironmentTypeTest {
		t.Fatalf("envType = %v, want %v", env.envType, EnvironmentTypeTest)
	}
	if env.Generation() != 3 {
		t.Fatalf("Generation() = %d, want 3", env.Generation())
	}
	if env.status == status {
		t.Fatalf("status pointer should be cloned")
	}
	if env.status.Desired != DesiredPresent {
		t.Fatalf("status.Desired = %v, want %v", env.status.Desired, DesiredPresent)
	}
	if env.status.State != StateReady {
		t.Fatalf("status.State = %v, want %v", env.status.State, StateReady)
	}
	if env.status.ObservedGeneration != 3 {
		t.Fatalf("status.ObservedGeneration = %d, want 3", env.status.ObservedGeneration)
	}
	if env.status.Message != "ready" {
		t.Fatalf("status.Message = %q, want %q", env.status.Message, "ready")
	}
	if !env.createTime.Equal(createTime) {
		t.Fatalf("createTime = %v, want %v", env.createTime, createTime)
	}
	if !env.updateTime.Equal(updateTime) {
		t.Fatalf("updateTime = %v, want %v", env.updateTime, updateTime)
	}
	if env.etag != "etag-1" {
		t.Fatalf("etag = %q, want %q", env.etag, "etag-1")
	}

	desiredState.Artifacts[0].Image = "repo/demo:v2"
	desiredState.Infras[0].Persistence.Enabled = true
	status.Message = "changed"
	if env.desiredState.Artifacts[0].Image != "repo/demo:v1" {
		t.Fatalf("desiredState should be cloned")
	}
	if env.desiredState.Infras[0].Persistence.Enabled {
		t.Fatalf("infra persistence should be cloned")
	}
	if env.status.Message != "ready" {
		t.Fatalf("status should be cloned")
	}
}

func TestRehydrateEnvironment_WithType(t *testing.T) {
	name, err := NewEnvironmentName("scope1", "dev")
	if err != nil {
		t.Fatalf("NewEnvironmentName() unexpected error: %v", err)
	}

	// given
	snapshot := EnvironmentSnapshot{
		Name:        name,
		EnvType:     EnvironmentTypeDev,
		Description: "demo environment",
		DesiredState: &DesiredState{
			Artifacts: []*ArtifactSpec{{
				Name:     "api",
				App:      "demo",
				Image:    "repo/demo:v1",
				Ports:    []ArtifactPortSpec{{Name: "http", Port: 8080}},
				Replicas: 1,
			}},
		},
		Status: &EnvironmentStatus{Desired: DesiredPresent, State: StateReady},
	}

	// when
	env, err := RehydrateEnvironment(snapshot)

	// then
	if err != nil {
		t.Fatalf("RehydrateEnvironment() unexpected error: %v", err)
	}
	if got := env.Type(); got != EnvironmentTypeDev {
		t.Fatalf("Type() = %v, want %v", got, EnvironmentTypeDev)
	}
}

func TestRehydrateEnvironmentRejectsNilStatus(t *testing.T) {
	name, err := NewEnvironmentName("scope1", "dev")
	if err != nil {
		t.Fatalf("NewEnvironmentName() unexpected error: %v", err)
	}

	_, err = RehydrateEnvironment(EnvironmentSnapshot{
		Name:        name,
		Description: "demo environment",
		DesiredState: &DesiredState{
			Artifacts: []*ArtifactSpec{{
				Name:     "api",
				App:      "demo",
				Image:    "repo/demo:v1",
				Ports:    []ArtifactPortSpec{{Name: "http", Port: 8080}},
				Replicas: 1,
				HTTP: &ArtifactHTTPSpec{
					Hostnames: []string{"example.com"},
					Matches: []HTTPRouteRule{{
						Backend: "http",
						Path:    HTTPPathRule{Type: HTTPPathRuleTypePathPrefix, Value: "/"},
					}},
				},
			}},
		},
	})
	if err != ErrInvalidState {
		t.Fatalf("RehydrateEnvironment() error = %v, want %v", err, ErrInvalidState)
	}
}

func TestDesiredState_InfraPersistenceZeroValue(t *testing.T) {
	// given
	state := &DesiredState{
		Infras: []*InfraSpec{{
			Resource: "postgres",
			Name:     "db",
		}},
	}

	// when
	cloned := cloneDesiredState(state)

	// then
	if cloned == nil {
		t.Fatal("cloneDesiredState() = nil, want non-nil")
	}
	if cloned.Infras[0].Persistence.Enabled {
		t.Fatal("cloneDesiredState().Infras[0].Persistence.Enabled = true, want false")
	}
	state.Infras[0].Persistence.Enabled = true
	if cloned.Infras[0].Persistence.Enabled {
		t.Fatal("cloned infra persistence should not change with source mutation")
	}
}

func mustNewEnvironment(t *testing.T) *Environment {
	t.Helper()

	name, err := NewEnvironmentName("scope1", "dev")
	if err != nil {
		t.Fatalf("NewEnvironmentName() unexpected error: %v", err)
	}

	env, err := NewEnvironment(name, EnvironmentTypeProd, "demo environment", &DesiredState{
		Artifacts: []*ArtifactSpec{{
			Name:     "api",
			App:      "demo",
			Image:    "repo/demo:v1",
			Ports:    []ArtifactPortSpec{{Name: "http", Port: 8080}},
			Replicas: 1,
			HTTP: &ArtifactHTTPSpec{
				Hostnames: []string{"example.com"},
				Matches: []HTTPRouteRule{{
					Backend: "http",
					Path:    HTTPPathRule{Type: HTTPPathRuleTypePathPrefix, Value: "/"},
				}},
			},
		}},
		Infras: []*InfraSpec{{
			Resource: "redis",
			Name:     "cache",
		}},
	})
	if err != nil {
		t.Fatalf("NewEnvironment() unexpected error: %v", err)
	}

	return env
}
