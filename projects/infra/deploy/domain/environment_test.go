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
			env, err := NewEnvironment(name, "demo environment", &desiredState)

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
			if env.status.State != StatePending {
				t.Fatalf("status.State = %v, want %v", env.status.State, StatePending)
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

func TestEnvironment_UpdateDesiredState(t *testing.T) {
	// given
	env := mustNewEnvironment(t)
	if err := env.MarkReconciling(); err != nil {
		t.Fatalf("MarkReconciling() unexpected error: %v", err)
	}
	if err := env.MarkReady(); err != nil {
		t.Fatalf("MarkReady() unexpected error: %v", err)
	}
	previousLastSuccessTime := env.status.LastSuccessTime
	newState := DesiredState{
		Artifacts: []*ArtifactSpec{{
			Name:     "api",
			App:      "demo",
			Image:    "repo/demo:v2",
			Ports:    []ArtifactPortSpec{{Name: "http", Port: 9090}},
			Replicas: 2,
			HTTP: &ArtifactHTTPSpec{
				Hostnames: []string{"example.com"},
				Matches: []HTTPRouteRule{{
					Backend: "http",
					Path:    HTTPPathRule{Type: HTTPPathRuleTypePathPrefix, Value: "/v2"},
				}},
			},
		}},
	}

	// when
	err := env.UpdateDesiredState(&newState)

	// then
	if err != nil {
		t.Fatalf("UpdateDesiredState() unexpected error: %v", err)
	}
	if env.status.State != StateReconciling {
		t.Fatalf("status.State = %v, want %v", env.status.State, StateReconciling)
	}
	if env.status.LastReconcileTime.IsZero() {
		t.Fatalf("LastReconcileTime should be set")
	}
	if env.status.LastSuccessTime != previousLastSuccessTime {
		t.Fatalf("LastSuccessTime = %v, want %v", env.status.LastSuccessTime, previousLastSuccessTime)
	}
	if got := env.desiredState.Artifacts[0].Image; got != "repo/demo:v2" {
		t.Fatalf("desiredState.Artifacts[0].Image = %q, want %q", got, "repo/demo:v2")
	}
}

func TestEnvironment_UpdateDesiredStateRejectsDeleting(t *testing.T) {
	// given
	env := mustNewEnvironment(t)
	if err := env.MarkDeleting(); err != nil {
		t.Fatalf("MarkDeleting() unexpected error: %v", err)
	}
	newState := DesiredState{
		Artifacts: []*ArtifactSpec{{
			Name:     "api",
			App:      "demo",
			Image:    "repo/demo:v2",
			Ports:    []ArtifactPortSpec{{Name: "http", Port: 9090}},
			Replicas: 2,
		}},
	}

	// when
	err := env.UpdateDesiredState(&newState)

	// then
	if err != ErrInvalidState {
		t.Fatalf("UpdateDesiredState() error = %v, want %v", err, ErrInvalidState)
	}
	if env.status.State != StateDeleting {
		t.Fatalf("status.State = %v, want %v", env.status.State, StateDeleting)
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
		name      string
		prepare   func(*testing.T, *Environment)
		wantErr   error
		wantState EnvironmentState
	}{
		{
			name: "reconciling to ready",
			prepare: func(t *testing.T, env *Environment) {
				if err := env.MarkReconciling(); err != nil {
					t.Fatalf("MarkReconciling() unexpected error: %v", err)
				}
			},
			wantState: StateReady,
		},
		{name: "pending to ready is invalid", wantErr: ErrInvalidState, wantState: StatePending},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			env := mustNewEnvironment(t)
			if tt.prepare != nil {
				tt.prepare(t, env)
			}

			// when
			err := env.MarkReady()

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
		})
	}
}

func TestEnvironment_MarkFailed(t *testing.T) {
	tests := []struct {
		name        string
		prepare     func(*testing.T, *Environment)
		message     string
		wantErr     error
		wantState   EnvironmentState
		wantMessage string
	}{
		{
			name: "reconciling to failed",
			prepare: func(t *testing.T, env *Environment) {
				if err := env.MarkReconciling(); err != nil {
					t.Fatalf("MarkReconciling() unexpected error: %v", err)
				}
			},
			message:     "apply failed",
			wantState:   StateFailed,
			wantMessage: "apply failed",
		},
		{
			name:        "pending to failed is invalid",
			message:     "apply failed",
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

			// when
			err := env.MarkFailed(tt.message)

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
		State:             StateReady,
		Message:           "ready",
		LastReconcileTime: createTime.Add(2 * time.Minute),
		LastSuccessTime:   createTime.Add(3 * time.Minute),
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
		Description:  "demo environment",
		DesiredState: desiredState,
		Status:       status,
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
	if env.status == status {
		t.Fatalf("status pointer should be cloned")
	}
	if env.status.State != StateReady {
		t.Fatalf("status.State = %v, want %v", env.status.State, StateReady)
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

	env, err := NewEnvironment(name, "demo environment", &DesiredState{
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
