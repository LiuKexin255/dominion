package deploy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"dominion/projects/infra/deploy/domain"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

type fakeQueue struct {
	enqueued []domain.EnvironmentName
	err      error
}

func newFakeQueue() *fakeQueue {
	return &fakeQueue{}
}

func (q *fakeQueue) Enqueue(_ context.Context, envName domain.EnvironmentName) error {
	if q.err != nil {
		return q.err
	}
	q.enqueued = append(q.enqueued, envName)
	return nil
}

func TestHandler_GetEnvironment(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		seed     []*domain.Environment
		request  *GetEnvironmentRequest
		wantName string
		wantCode codes.Code
	}{
		{
			name:     "success",
			seed:     []*domain.Environment{mustNewDomainEnvironment(t, "dev", "alpha", newDesiredState())},
			request:  &GetEnvironmentRequest{Name: "deploy/scopes/dev/environments/alpha"},
			wantName: "deploy/scopes/dev/environments/alpha",
			wantCode: codes.OK,
		},
		{
			name:     "not found",
			seed:     []*domain.Environment{mustNewDomainEnvironment(t, "dev", "alpha", newDesiredState())},
			request:  &GetEnvironmentRequest{Name: "deploy/scopes/dev/environments/missing"},
			wantCode: codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			repo := newFakeRepository(tt.seed...)
			handler := NewHandler(repo, newFakeQueue(), &fakeServiceEndpointQuery{})

			// when
			got, err := handler.GetEnvironment(ctx, tt.request)

			// then
			assertStatusCode(t, err, tt.wantCode)
			if tt.wantCode != codes.OK {
				return
			}
			if got.GetName() != tt.wantName {
				t.Fatalf("GetEnvironment() name = %q, want %q", got.GetName(), tt.wantName)
			}
		})
	}
}

func TestHandler_ListEnvironments(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name          string
		seed          []*domain.Environment
		request       *ListEnvironmentsRequest
		wantNames     []string
		wantNextToken string
		wantCode      codes.Code
	}{
		{
			name: "success",
			seed: []*domain.Environment{
				mustNewDomainEnvironment(t, "dev", "alpha", newDesiredState()),
				mustNewDomainEnvironment(t, "dev", "beta", newDesiredState()),
				mustNewDomainEnvironment(t, "prod", "gamma", newDesiredState()),
			},
			request: &ListEnvironmentsRequest{Parent: "deploy/scopes/dev"},
			wantNames: []string{
				"deploy/scopes/dev/environments/alpha",
				"deploy/scopes/dev/environments/beta",
			},
			wantCode: codes.OK,
		},
		{
			name:     "empty results",
			request:  &ListEnvironmentsRequest{Parent: "deploy/scopes/dev"},
			wantCode: codes.OK,
		},
		{
			name: "pagination",
			seed: []*domain.Environment{
				mustNewDomainEnvironment(t, "dev", "alpha", newDesiredState()),
				mustNewDomainEnvironment(t, "dev", "beta", newDesiredState()),
				mustNewDomainEnvironment(t, "dev", "delta", newDesiredState()),
			},
			request:       &ListEnvironmentsRequest{Parent: "deploy/scopes/dev", PageSize: 2},
			wantNames:     []string{"deploy/scopes/dev/environments/alpha", "deploy/scopes/dev/environments/beta"},
			wantNextToken: domain.EncodePageToken(2),
			wantCode:      codes.OK,
		},
		{
			name:     "invalid parent",
			request:  &ListEnvironmentsRequest{Parent: "bad-parent"},
			wantCode: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			repo := newFakeRepository(tt.seed...)
			handler := NewHandler(repo, newFakeQueue(), &fakeServiceEndpointQuery{})

			// when
			got, err := handler.ListEnvironments(ctx, tt.request)

			// then
			assertStatusCode(t, err, tt.wantCode)
			if tt.wantCode != codes.OK {
				return
			}
			if len(got.GetEnvironments()) != len(tt.wantNames) {
				t.Fatalf("ListEnvironments() len = %d, want %d", len(got.GetEnvironments()), len(tt.wantNames))
			}
			for i, env := range got.GetEnvironments() {
				if env.GetName() != tt.wantNames[i] {
					t.Fatalf("ListEnvironments() item[%d] = %q, want %q", i, env.GetName(), tt.wantNames[i])
				}
			}
			if got.GetNextPageToken() != tt.wantNextToken {
				t.Fatalf("ListEnvironments() next token = %q, want %q", got.GetNextPageToken(), tt.wantNextToken)
			}
		})
	}
}

func TestHandler_CreateEnvironment(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		seed         []*domain.Environment
		repoSetup    func(*fakeRepository)
		request      *CreateEnvironmentRequest
		wantState    EnvironmentState
		wantEnqueued int
		wantCode     codes.Code
	}{
		{
			name: "success returns pending desired present and enqueues",
			request: &CreateEnvironmentRequest{
				Parent:      "deploy/scopes/dev",
				EnvName:     "alpha",
				Environment: &Environment{Description: "alpha", DesiredState: newProtoDesiredState(), Type: EnvironmentType_ENVIRONMENT_TYPE_PROD},
			},
			wantState:    EnvironmentState_ENVIRONMENT_STATE_PENDING,
			wantEnqueued: 1,
			wantCode:     codes.OK,
		},
		{
			name: "duplicate",
			seed: []*domain.Environment{mustNewDomainEnvironment(t, "dev", "alpha", newDesiredState())},
			request: &CreateEnvironmentRequest{
				Parent:      "deploy/scopes/dev",
				EnvName:     "alpha",
				Environment: &Environment{Description: "alpha", DesiredState: newProtoDesiredState(), Type: EnvironmentType_ENVIRONMENT_TYPE_PROD},
			},
			wantCode: codes.AlreadyExists,
		},
		{
			name: "invalid input",
			request: &CreateEnvironmentRequest{
				Parent:      "deploy/scopes/dev",
				EnvName:     "alpha",
				Environment: &Environment{Description: "alpha"},
			},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "invalid name",
			request: &CreateEnvironmentRequest{
				Parent:      "deploy/scopes/INVALID",
				EnvName:     "alpha",
				Environment: &Environment{Description: "alpha", DesiredState: newProtoDesiredState(), Type: EnvironmentType_ENVIRONMENT_TYPE_PROD},
			},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "repository save error",
			repoSetup: func(repo *fakeRepository) {
				repo.saveErr = errors.New("save failed")
			},
			request: &CreateEnvironmentRequest{
				Parent:      "deploy/scopes/dev",
				EnvName:     "alpha",
				Environment: &Environment{Description: "alpha", DesiredState: newProtoDesiredState(), Type: EnvironmentType_ENVIRONMENT_TYPE_PROD},
			},
			wantCode: codes.Internal,
		},
		{
			name:      "enqueue error",
			repoSetup: func(_ *fakeRepository) {},
			request: &CreateEnvironmentRequest{
				Parent:      "deploy/scopes/dev",
				EnvName:     "alpha",
				Environment: &Environment{Description: "alpha", DesiredState: newProtoDesiredState(), Type: EnvironmentType_ENVIRONMENT_TYPE_PROD},
			},
			wantCode: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			repo := newFakeRepository(tt.seed...)
			if tt.repoSetup != nil {
				tt.repoSetup(repo)
			}
			q := newFakeQueue()
			if tt.name == "enqueue error" {
				q.err = errors.New("queue full")
			}
			handler := NewHandler(repo, q, &fakeServiceEndpointQuery{})

			// when
			got, err := handler.CreateEnvironment(ctx, tt.request)

			// then
			assertStatusCode(t, err, tt.wantCode)
			if tt.wantCode != codes.OK {
				return
			}
			if got.GetStatus().GetState() != tt.wantState {
				t.Fatalf("CreateEnvironment() state = %v, want %v", got.GetStatus().GetState(), tt.wantState)
			}
			if len(q.enqueued) != tt.wantEnqueued {
				t.Fatalf("CreateEnvironment() enqueued = %d, want %d", len(q.enqueued), tt.wantEnqueued)
			}
			if got.GetDesiredState() == nil {
				t.Fatal("CreateEnvironment() desired_state = nil, want non-nil")
			}
			stored, err := repo.Get(ctx, q.enqueued[0])
			if err != nil {
				t.Fatalf("repo.Get() error = %v", err)
			}
			if stored.Status().State != domain.StatePending {
				t.Fatalf("stored env state = %v, want %v", stored.Status().State, domain.StatePending)
			}
			if stored.Status().Desired != domain.DesiredPresent {
				t.Fatalf("stored env desired = %v, want %v", stored.Status().Desired, domain.DesiredPresent)
			}
		})
	}
}

func TestHandler_CreateEnvironmentThenGet(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newFakeRepository()
	handler := NewHandler(repo, newFakeQueue(), &fakeServiceEndpointQuery{})
	createReq := &CreateEnvironmentRequest{
		Parent:      "deploy/scopes/dev",
		EnvName:     "alpha",
		Environment: &Environment{Description: "alpha", DesiredState: newProtoDesiredState(), Type: EnvironmentType_ENVIRONMENT_TYPE_PROD},
	}

	// when
	created, err := handler.CreateEnvironment(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateEnvironment() error = %v", err)
	}
	got, err := handler.GetEnvironment(ctx, &GetEnvironmentRequest{Name: "deploy/scopes/dev/environments/alpha"})

	// then
	if err != nil {
		t.Fatalf("GetEnvironment() error = %v", err)
	}
	if created.GetStatus().GetState() != EnvironmentState_ENVIRONMENT_STATE_PENDING {
		t.Fatalf("CreateEnvironment() state = %v, want %v", created.GetStatus().GetState(), EnvironmentState_ENVIRONMENT_STATE_PENDING)
	}
	if got.GetStatus().GetState() != EnvironmentState_ENVIRONMENT_STATE_PENDING {
		t.Fatalf("GetEnvironment() state = %v, want %v (async: no worker to reconcile)", got.GetStatus().GetState(), EnvironmentState_ENVIRONMENT_STATE_PENDING)
	}
	envName, err := domain.ParseResourceName("deploy/scopes/dev/environments/alpha")
	if err != nil {
		t.Fatalf("ParseResourceName() error = %v", err)
	}
	stored, err := repo.Get(ctx, envName)
	if err != nil {
		t.Fatalf("repo.Get() error = %v", err)
	}
	if stored.Status().Desired != domain.DesiredPresent {
		t.Fatalf("stored env desired = %v, want %v", stored.Status().Desired, domain.DesiredPresent)
	}
}

func TestHandler_UpdateEnvironment(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		seed         func(t *testing.T) *domain.Environment
		request      *UpdateEnvironmentRequest
		wantState    EnvironmentState
		wantEnqueued int
		wantCode     codes.Code
	}{
		{
			name: "success returns pending desired present and enqueues",
			seed: func(t *testing.T) *domain.Environment {
				env := mustNewDomainEnvironment(t, "dev", "alpha", newDesiredState())
				if err := env.MarkReconciling(); err != nil {
					t.Fatalf("MarkReconciling() error = %v", err)
				}
				if err := env.MarkReady(env.Generation()); err != nil {
					t.Fatalf("MarkReady() error = %v", err)
				}
				return env
			},
			request: &UpdateEnvironmentRequest{
				Environment: &Environment{Name: "deploy/scopes/dev/environments/alpha", DesiredState: newUpdatedProtoDesiredState()},
				UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"desired_state"}},
			},
			wantState:    EnvironmentState_ENVIRONMENT_STATE_PENDING,
			wantEnqueued: 1,
			wantCode:     codes.OK,
		},
		{
			name: "not found",
			request: &UpdateEnvironmentRequest{
				Environment: &Environment{Name: "deploy/scopes/dev/environments/missing", DesiredState: newUpdatedProtoDesiredState()},
				UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"desired_state"}},
			},
			wantCode: codes.NotFound,
		},
		{
			name: "deleting state rejected",
			seed: func(t *testing.T) *domain.Environment {
				env := mustNewDomainEnvironment(t, "dev", "alpha", newDesiredState())
				if err := env.MarkDeleting(); err != nil {
					t.Fatalf("MarkDeleting() error = %v", err)
				}
				return env
			},
			request: &UpdateEnvironmentRequest{
				Environment: &Environment{Name: "deploy/scopes/dev/environments/alpha", DesiredState: newUpdatedProtoDesiredState()},
				UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"desired_state"}},
			},
			wantCode: codes.FailedPrecondition,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			repo := newFakeRepository()
			if tt.seed != nil {
				if err := repo.Save(ctx, tt.seed(t)); err != nil {
					t.Fatalf("repo.Save() error = %v", err)
				}
			}
			q := newFakeQueue()
			handler := NewHandler(repo, q, &fakeServiceEndpointQuery{})

			// when
			got, err := handler.UpdateEnvironment(ctx, tt.request)

			// then
			assertStatusCode(t, err, tt.wantCode)
			if tt.wantCode != codes.OK {
				return
			}
			if got.GetStatus().GetState() != tt.wantState {
				t.Fatalf("UpdateEnvironment() state = %v, want %v", got.GetStatus().GetState(), tt.wantState)
			}
			if len(q.enqueued) != tt.wantEnqueued {
				t.Fatalf("UpdateEnvironment() enqueued = %d, want %d", len(q.enqueued), tt.wantEnqueued)
			}
			stored, err := repo.Get(ctx, q.enqueued[0])
			if err != nil {
				t.Fatalf("repo.Get() error = %v", err)
			}
			if stored.Status().State != domain.StatePending {
				t.Fatalf("stored env state = %v, want %v", stored.Status().State, domain.StatePending)
			}
			if stored.Status().Desired != domain.DesiredPresent {
				t.Fatalf("stored env desired = %v, want %v", stored.Status().Desired, domain.DesiredPresent)
			}
			if stored.DesiredState().Artifacts[0].Image != "example.com/gateway:v2" {
				t.Fatalf("stored env artifact image = %q, want %q", stored.DesiredState().Artifacts[0].Image, "example.com/gateway:v2")
			}
		})
	}
}

func TestHandler_UpdateEnvironmentThenGet(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newFakeRepository()
	seed := mustNewDomainEnvironment(t, "dev", "alpha", newDesiredState())
	if err := seed.MarkReconciling(); err != nil {
		t.Fatalf("MarkReconciling() error = %v", err)
	}
	if err := seed.MarkReady(seed.Generation()); err != nil {
		t.Fatalf("MarkReady() error = %v", err)
	}
	if err := repo.Save(ctx, seed); err != nil {
		t.Fatalf("repo.Save() error = %v", err)
	}
	handler := NewHandler(repo, newFakeQueue(), &fakeServiceEndpointQuery{})
	updateReq := &UpdateEnvironmentRequest{
		Environment: &Environment{Name: "deploy/scopes/dev/environments/alpha", DesiredState: newUpdatedProtoDesiredState()},
		UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"desired_state"}},
	}

	// when
	updated, err := handler.UpdateEnvironment(ctx, updateReq)
	if err != nil {
		t.Fatalf("UpdateEnvironment() error = %v", err)
	}
	got, err := handler.GetEnvironment(ctx, &GetEnvironmentRequest{Name: "deploy/scopes/dev/environments/alpha"})

	// then
	if err != nil {
		t.Fatalf("GetEnvironment() error = %v", err)
	}
	if updated.GetStatus().GetState() != EnvironmentState_ENVIRONMENT_STATE_PENDING {
		t.Fatalf("UpdateEnvironment() state = %v, want %v", updated.GetStatus().GetState(), EnvironmentState_ENVIRONMENT_STATE_PENDING)
	}
	if got.GetStatus().GetState() != EnvironmentState_ENVIRONMENT_STATE_PENDING {
		t.Fatalf("GetEnvironment() state = %v, want %v (async: no worker to reconcile)", got.GetStatus().GetState(), EnvironmentState_ENVIRONMENT_STATE_PENDING)
	}
	if got.GetDesiredState().GetArtifacts()[0].GetImage() != "example.com/gateway:v2" {
		t.Fatalf("GetEnvironment() desired_state.artifacts[0].image = %q, want %q", got.GetDesiredState().GetArtifacts()[0].GetImage(), "example.com/gateway:v2")
	}
}

func TestHandler_DeleteEnvironment(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		seed         []*domain.Environment
		request      *DeleteEnvironmentRequest
		wantResp     *emptypb.Empty
		wantEnqueued int
		wantCode     codes.Code
	}{
		{
			name:         "success marks pending desired absent and enqueues",
			seed:         []*domain.Environment{mustNewDomainEnvironment(t, "dev", "alpha", newDesiredState())},
			request:      &DeleteEnvironmentRequest{Name: "deploy/scopes/dev/environments/alpha"},
			wantResp:     new(emptypb.Empty),
			wantEnqueued: 1,
			wantCode:     codes.OK,
		},
		{
			name:     "not found",
			request:  &DeleteEnvironmentRequest{Name: "deploy/scopes/dev/environments/missing"},
			wantCode: codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			repo := newFakeRepository(tt.seed...)
			q := newFakeQueue()
			handler := NewHandler(repo, q, &fakeServiceEndpointQuery{})

			// when
			got, err := handler.DeleteEnvironment(ctx, tt.request)

			// then
			assertStatusCode(t, err, tt.wantCode)
			if tt.wantCode != codes.OK {
				return
			}
			if got == nil || tt.wantResp == nil {
				t.Fatalf("DeleteEnvironment() got = %v, want non-nil empty response", got)
			}
			if len(q.enqueued) != tt.wantEnqueued {
				t.Fatalf("DeleteEnvironment() enqueued = %d, want %d", len(q.enqueued), tt.wantEnqueued)
			}
			stored, err := repo.Get(ctx, q.enqueued[0])
			if err != nil {
				t.Fatalf("repo.Get() error = %v", err)
			}
			if stored.Status().State != domain.StatePending {
				t.Fatalf("stored env state = %v, want %v", stored.Status().State, domain.StatePending)
			}
			if stored.Status().Desired != domain.DesiredAbsent {
				t.Fatalf("stored env desired = %v, want %v", stored.Status().Desired, domain.DesiredAbsent)
			}
		})
	}
}

func TestHandler_DeleteEnvironmentKeepsEnvInRepo(t *testing.T) {
	ctx := context.Background()

	// given
	env := mustNewDomainEnvironment(t, "dev", "alpha", newDesiredState())
	repo := newFakeRepository(env)
	handler := NewHandler(repo, newFakeQueue(), &fakeServiceEndpointQuery{})

	// when
	_, err := handler.DeleteEnvironment(ctx, &DeleteEnvironmentRequest{Name: "deploy/scopes/dev/environments/alpha"})
	if err != nil {
		t.Fatalf("DeleteEnvironment() error = %v", err)
	}

	// then - env still exists with pending state until worker starts deleting
	got, err := repo.Get(ctx, env.Name())
	if err != nil {
		t.Fatalf("Get() error = %v, want env to still exist in repo", err)
	}
	if got.Status().State != domain.StatePending {
		t.Fatalf("env state = %v, want %v", got.Status().State, domain.StatePending)
	}
	if got.Status().Desired != domain.DesiredAbsent {
		t.Fatalf("env desired = %v, want %v", got.Status().Desired, domain.DesiredAbsent)
	}
}

func TestHandler_GetServiceEndpoints_SameEnv(t *testing.T) {
	ctx := context.Background()

	// given
	env := mustReadyDomainEnvironment(t, "prod", "alpha")
	query := &fakeServiceEndpointQuery{
		results: map[string]*domain.ServiceQueryResult{
			serviceQueryKey("prod.alpha", "gateway", "api"): {
				Endpoints: []string{"10.0.0.1:8080", "10.0.0.2:8080"},
				Ports: map[string]int32{
					"http": 8080,
				},
			},
		},
	}
	handler := NewHandler(newFakeRepository(env), newFakeQueue(), query)
	req := &GetServiceEndpointsRequest{
		Name: "deploy/scopes/prod/environments/alpha/apps/gateway/services/api/endpoints",
	}

	// when
	got, err := handler.GetServiceEndpoints(ctx, req)

	// then
	assertStatusCode(t, err, codes.OK)
	if got.GetName() != req.GetName() {
		t.Fatalf("GetServiceEndpoints() name = %q, want %q", got.GetName(), req.GetName())
	}
	if len(got.GetEndpoints()) != 2 {
		t.Fatalf("GetServiceEndpoints() endpoints = %v, want 2 entries", got.GetEndpoints())
	}
	if got.GetPorts()["http"] != 8080 {
		t.Fatalf("GetServiceEndpoints() ports[http] = %d, want 8080", got.GetPorts()["http"])
	}
	if got.GetResolutionMode() != ResolutionMode(0) {
		t.Fatalf("GetServiceEndpoints() resolution_mode = %v, want unspecified for basic view", got.GetResolutionMode())
	}
	if len(query.calls) != 1 {
		t.Fatalf("QueryServiceEndpoints() call count = %d, want 1", len(query.calls))
	}
	if len(query.statefulCalls) != 0 {
		t.Fatalf("QueryStatefulServiceEndpoints() call count = %d, want 0", len(query.statefulCalls))
	}
}

func TestHandler_GetServiceEndpoints_StatefulSameEnv(t *testing.T) {
	ctx := context.Background()

	// given
	env := mustReadyDomainEnvironmentWithDesiredState(t, "prod", "alpha", domain.EnvironmentTypeProd, domain.DesiredState{
		Artifacts: []*domain.ArtifactSpec{{
			Name:         "api",
			App:          "gateway",
			Image:        "example.com/gateway:v1",
			Ports:        []domain.ArtifactPortSpec{{Name: "http", Port: 8080}},
			Replicas:     1,
			WorkloadKind: domain.WorkloadKindStateful,
		}},
	})
	query := &fakeServiceEndpointQuery{
		statefulResults: map[string]*domain.ServiceQueryResult{
			serviceQueryKey("prod.alpha", "gateway", "api"): {
				Endpoints: []string{"10.0.0.1:8080"},
				Ports: map[string]int32{
					"http": 8080,
				},
				IsStateful: true,
				StatefulInstances: []*domain.StatefulInstance{{
					Index:     0,
					Hostname:  "gateway-api-0",
					Endpoints: []string{"10.0.0.1:8080"},
				}},
			},
		},
	}
	handler := NewHandler(newFakeRepository(env), newFakeQueue(), query)

	// when
	got, err := handler.GetServiceEndpoints(ctx, &GetServiceEndpointsRequest{Name: "deploy/scopes/prod/environments/alpha/apps/gateway/services/api/endpoints"})

	// then
	assertStatusCode(t, err, codes.OK)
	if !got.GetIsStateful() {
		t.Fatal("GetServiceEndpoints() is_stateful = false, want true")
	}
	if len(got.GetStatefulInstances()) != 1 {
		t.Fatalf("GetServiceEndpoints() stateful_instances len = %d, want 1", len(got.GetStatefulInstances()))
	}
	if len(query.calls) != 0 {
		t.Fatalf("QueryServiceEndpoints() call count = %d, want 0", len(query.calls))
	}
	if len(query.statefulCalls) != 1 {
		t.Fatalf("QueryStatefulServiceEndpoints() call count = %d, want 1", len(query.statefulCalls))
	}
}

func TestHandler_GetServiceEndpoints_ProdFallback(t *testing.T) {
	ctx := context.Background()

	// given
	primary := mustReadyDomainEnvironment(t, "prod", "alpha")
	fallbackA := mustReadyDomainEnvironment(t, "prod", "beta")
	fallbackB := mustReadyDomainEnvironment(t, "prod", "aardvark")
	query := &fakeServiceEndpointQuery{
		results: map[string]*domain.ServiceQueryResult{
			serviceQueryKey("prod.aardvark", "gateway", "api"): {
				Endpoints: []string{"10.1.0.1:9090"},
				Ports: map[string]int32{
					"grpc": 9090,
				},
			},
		},
		errs: map[string]error{
			serviceQueryKey("prod.alpha", "gateway", "api"):    domain.ErrServiceNotFound,
			serviceQueryKey("prod.beta", "gateway", "api"):     domain.ErrServiceNotFound,
			serviceQueryKey("prod.aardvark", "gateway", "api"): nil,
		},
	}
	handler := NewHandler(newFakeRepository(primary, fallbackA, fallbackB), newFakeQueue(), query)
	req := &GetServiceEndpointsRequest{
		Name: "deploy/scopes/prod/environments/alpha/apps/gateway/services/api/endpoints",
		View: ServiceEndpointsView_SERVICE_ENDPOINTS_VIEW_RESOLUTION,
	}

	// when
	got, err := handler.GetServiceEndpoints(ctx, req)

	// then
	assertStatusCode(t, err, codes.OK)
	if got.GetResolutionMode() != ResolutionMode_RESOLUTION_MODE_PROD_FALLBACK {
		t.Fatalf("GetServiceEndpoints() resolution_mode = %v, want PROD_FALLBACK", got.GetResolutionMode())
	}
	if got.GetResolvedScope() != "prod" {
		t.Fatalf("GetServiceEndpoints() resolved_scope = %q, want %q", got.GetResolvedScope(), "prod")
	}
	if got.GetResolvedEnvironment() != "aardvark" {
		t.Fatalf("GetServiceEndpoints() resolved_environment = %q, want %q", got.GetResolvedEnvironment(), "aardvark")
	}
	if got.GetPorts()["grpc"] != 9090 {
		t.Fatalf("GetServiceEndpoints() ports[grpc] = %d, want 9090", got.GetPorts()["grpc"])
	}
	if len(query.calls) != 2 {
		t.Fatalf("QueryServiceEndpoints() call count = %d, want 2", len(query.calls))
	}
	if len(query.statefulCalls) != 0 {
		t.Fatalf("QueryStatefulServiceEndpoints() call count = %d, want 0", len(query.statefulCalls))
	}
	if query.calls[1].envLabel != "prod.aardvark" {
		t.Fatalf("QueryServiceEndpoints() fallback first env = %q, want %q", query.calls[1].envLabel, "prod.aardvark")
	}
}

func TestHandler_GetServiceEndpoints_StatefulProdFallback(t *testing.T) {
	ctx := context.Background()

	// given
	primary := mustReadyDomainEnvironment(t, "prod", "alpha")
	fallback := mustReadyDomainEnvironmentWithDesiredState(t, "prod", "beta", domain.EnvironmentTypeProd, domain.DesiredState{
		Artifacts: []*domain.ArtifactSpec{{
			Name:         "api",
			App:          "gateway",
			Image:        "example.com/gateway:v1",
			Ports:        []domain.ArtifactPortSpec{{Name: "http", Port: 8080}},
			Replicas:     1,
			WorkloadKind: domain.WorkloadKindStateful,
		}},
	})
	query := &fakeServiceEndpointQuery{
		errs: map[string]error{
			serviceQueryKey("prod.alpha", "gateway", "api"): domain.ErrServiceNotFound,
		},
		statefulResults: map[string]*domain.ServiceQueryResult{
			serviceQueryKey("prod.beta", "gateway", "api"): {
				Ports:      map[string]int32{"http": 8080},
				Endpoints:  []string{"10.0.0.2:8080"},
				IsStateful: true,
			},
		},
	}
	handler := NewHandler(newFakeRepository(primary, fallback), newFakeQueue(), query)

	// when
	got, err := handler.GetServiceEndpoints(ctx, &GetServiceEndpointsRequest{
		Name: "deploy/scopes/prod/environments/alpha/apps/gateway/services/api/endpoints",
		View: ServiceEndpointsView_SERVICE_ENDPOINTS_VIEW_RESOLUTION,
	})

	// then
	assertStatusCode(t, err, codes.OK)
	if got.GetResolvedEnvironment() != "beta" {
		t.Fatalf("GetServiceEndpoints() resolved_environment = %q, want %q", got.GetResolvedEnvironment(), "beta")
	}
	if len(query.calls) != 1 {
		t.Fatalf("QueryServiceEndpoints() call count = %d, want 1", len(query.calls))
	}
	if len(query.statefulCalls) != 1 {
		t.Fatalf("QueryStatefulServiceEndpoints() call count = %d, want 1", len(query.statefulCalls))
	}
}

func TestHandler_GetServiceEndpoints_NonProdNotFound(t *testing.T) {
	ctx := context.Background()

	// given
	env := mustReadyDomainEnvironmentWithType(t, "dev", "alpha", domain.EnvironmentTypeDev)
	query := &fakeServiceEndpointQuery{
		errs: map[string]error{
			serviceQueryKey("dev.alpha", "gateway", "api"): domain.ErrServiceNotFound,
		},
	}
	handler := NewHandler(newFakeRepository(env), newFakeQueue(), query)

	// when
	_, err := handler.GetServiceEndpoints(ctx, &GetServiceEndpointsRequest{Name: "deploy/scopes/dev/environments/alpha/apps/gateway/services/api/endpoints"})

	// then
	assertStatusCode(t, err, codes.NotFound)
	assertErrorInfo(t, err, "SERVICE_ENDPOINTS_NOT_FOUND", nil)
	if len(query.calls) != 1 {
		t.Fatalf("QueryServiceEndpoints() call count = %d, want 1", len(query.calls))
	}
	if len(query.statefulCalls) != 0 {
		t.Fatalf("QueryStatefulServiceEndpoints() call count = %d, want 0", len(query.statefulCalls))
	}
}

func TestHandler_GetServiceEndpoints_ResolutionView(t *testing.T) {
	ctx := context.Background()

	// given
	env := mustReadyDomainEnvironment(t, "prod", "alpha")
	query := &fakeServiceEndpointQuery{
		results: map[string]*domain.ServiceQueryResult{
			serviceQueryKey("prod.alpha", "gateway", "api"): {
				Endpoints: []string{"10.0.0.1:8080"},
				Ports: map[string]int32{
					"http": 8080,
				},
			},
		},
	}
	handler := NewHandler(newFakeRepository(env), newFakeQueue(), query)
	req := &GetServiceEndpointsRequest{
		Name: "deploy/scopes/prod/environments/alpha/apps/gateway/services/api/endpoints",
		View: ServiceEndpointsView_SERVICE_ENDPOINTS_VIEW_RESOLUTION,
	}

	// when
	got, err := handler.GetServiceEndpoints(ctx, req)

	// then
	assertStatusCode(t, err, codes.OK)
	if got.GetResolvedScope() != "prod" {
		t.Fatalf("GetServiceEndpoints() resolved_scope = %q, want %q", got.GetResolvedScope(), "prod")
	}
	if got.GetResolvedEnvironment() != "alpha" {
		t.Fatalf("GetServiceEndpoints() resolved_environment = %q, want %q", got.GetResolvedEnvironment(), "alpha")
	}
	if got.GetResolutionMode() != ResolutionMode_RESOLUTION_MODE_SAME_ENV {
		t.Fatalf("GetServiceEndpoints() resolution_mode = %v, want SAME_ENV", got.GetResolutionMode())
	}
}

func Test_newServiceEndpointsResponse_StatefulInstances(t *testing.T) {
	name, err := domain.NewServiceEndpointsName("prod", "alpha", "gateway", "api")
	if err != nil {
		t.Fatalf("NewServiceEndpointsName() unexpected error: %v", err)
	}

	tests := []struct {
		name           string
		result         *domain.ServiceQueryResult
		wantIsStateful bool
		wantInstances  []*StatefulServiceInstance
	}{
		{
			name: "stateful result",
			result: &domain.ServiceQueryResult{
				IsStateful: true,
				StatefulInstances: []*domain.StatefulInstance{
					{Index: 0, Hostname: "demo-api-0", Endpoints: []string{"10.0.0.1:50051"}},
					{Index: 1, Hostname: "demo-api-1", Endpoints: nil},
				},
			},
			wantIsStateful: true,
			wantInstances: []*StatefulServiceInstance{
				{Index: 0, Hostname: "demo-api-0", Endpoints: []string{"10.0.0.1:50051"}},
				{Index: 1, Hostname: "demo-api-1", Endpoints: nil},
			},
		},
		{
			name:           "non-stateful result",
			result:         &domain.ServiceQueryResult{IsStateful: false},
			wantIsStateful: false,
			wantInstances:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newServiceEndpointsResponse(name, tt.result, domain.EnvironmentName{}, ResolutionMode(0), ServiceEndpointsView_SERVICE_ENDPOINTS_VIEW_BASIC)

			if got.IsStateful != tt.wantIsStateful {
				t.Fatalf("newServiceEndpointsResponse() is_stateful = %v, want %v", got.IsStateful, tt.wantIsStateful)
			}
			if tt.wantInstances == nil {
				if got.StatefulInstances != nil {
					t.Fatalf("newServiceEndpointsResponse() stateful_instances = %v, want nil", got.StatefulInstances)
				}
				return
			}
			if len(got.StatefulInstances) != len(tt.wantInstances) {
				t.Fatalf("newServiceEndpointsResponse() stateful_instances len = %d, want %d", len(got.StatefulInstances), len(tt.wantInstances))
			}
			for i := range tt.wantInstances {
				if got.StatefulInstances[i].Index != tt.wantInstances[i].Index {
					t.Fatalf("newServiceEndpointsResponse() stateful_instances[%d].index = %d, want %d", i, got.StatefulInstances[i].Index, tt.wantInstances[i].Index)
				}
				if got.StatefulInstances[i].Hostname != tt.wantInstances[i].Hostname {
					t.Fatalf("newServiceEndpointsResponse() stateful_instances[%d].hostname = %q, want %q", i, got.StatefulInstances[i].Hostname, tt.wantInstances[i].Hostname)
				}
				if len(got.StatefulInstances[i].Endpoints) != len(tt.wantInstances[i].Endpoints) {
					t.Fatalf("newServiceEndpointsResponse() stateful_instances[%d].endpoints = %v, want %v", i, got.StatefulInstances[i].Endpoints, tt.wantInstances[i].Endpoints)
				}
				for j := range tt.wantInstances[i].Endpoints {
					if got.StatefulInstances[i].Endpoints[j] != tt.wantInstances[i].Endpoints[j] {
						t.Fatalf("newServiceEndpointsResponse() stateful_instances[%d].endpoints[%d] = %q, want %q", i, j, got.StatefulInstances[i].Endpoints[j], tt.wantInstances[i].Endpoints[j])
					}
				}
			}
		})
	}
}

func Test_isStatefulService(t *testing.T) {
	tests := []struct {
		name    string
		env     *domain.Environment
		app     string
		service string
		want    bool
	}{
		{
			name: "matches stateful artifact",
			env: mustReadyDomainEnvironmentWithDesiredState(t, "prod", "alpha", domain.EnvironmentTypeProd, domain.DesiredState{
				Artifacts: []*domain.ArtifactSpec{{
					Name:         "api",
					App:          "gateway",
					Image:        "example.com/gateway:v1",
					Ports:        []domain.ArtifactPortSpec{{Name: "http", Port: 8080}},
					WorkloadKind: domain.WorkloadKindStateful,
				}},
			}),
			app:     "gateway",
			service: "api",
			want:    true,
		},
		{
			name:    "stateless artifact",
			env:     mustReadyDomainEnvironment(t, "prod", "alpha"),
			app:     "gateway",
			service: "api",
			want:    false,
		},
		{
			name:    "missing artifact",
			env:     mustReadyDomainEnvironment(t, "prod", "alpha"),
			app:     "gateway",
			service: "worker",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isStatefulService(tt.env, tt.app, tt.service); got != tt.want {
				t.Fatalf("isStatefulService() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHandler_GetServiceEndpoints_InvalidName(t *testing.T) {
	ctx := context.Background()

	// given
	handler := NewHandler(newFakeRepository(), newFakeQueue(), &fakeServiceEndpointQuery{})

	// when
	_, err := handler.GetServiceEndpoints(ctx, &GetServiceEndpointsRequest{Name: "bad-name"})

	// then
	assertStatusCode(t, err, codes.InvalidArgument)
}

func TestHandler_GetServiceEndpoints_FallbackNoCandidates(t *testing.T) {
	ctx := context.Background()

	// given
	primary := mustReadyDomainEnvironment(t, "prod", "alpha")
	query := &fakeServiceEndpointQuery{
		errs: map[string]error{
			serviceQueryKey("prod.alpha", "gateway", "api"): domain.ErrServiceNotFound,
		},
	}
	handler := NewHandler(newFakeRepository(primary), newFakeQueue(), query)

	// when
	_, err := handler.GetServiceEndpoints(ctx, &GetServiceEndpointsRequest{Name: "deploy/scopes/prod/environments/alpha/apps/gateway/services/api/endpoints"})

	// then
	assertStatusCode(t, err, codes.NotFound)
	assertErrorInfo(t, err, "SERVICE_ENDPOINTS_NOT_FOUND", map[string]string{
		"resource_name": "deploy/scopes/prod/environments/alpha/apps/gateway/services/api/endpoints",
		"app":           "gateway",
		"service":       "api",
		"environment":   "alpha",
	})
}

func TestHandler_GetServiceEndpoints_ServicePortMapUnavailable(t *testing.T) {
	ctx := context.Background()

	// given
	env := mustReadyDomainEnvironment(t, "prod", "alpha")
	query := &fakeServiceEndpointQuery{
		errs: map[string]error{
			serviceQueryKey("prod.alpha", "gateway", "api"): domain.ErrServicePortMapUnavailable,
		},
	}
	handler := NewHandler(newFakeRepository(env), newFakeQueue(), query)

	// when
	_, err := handler.GetServiceEndpoints(ctx, &GetServiceEndpointsRequest{Name: "deploy/scopes/prod/environments/alpha/apps/gateway/services/api/endpoints"})

	// then
	assertStatusCode(t, err, codes.FailedPrecondition)
	assertErrorInfo(t, err, "SERVICE_PORT_MAP_UNAVAILABLE", nil)
}

func TestHandler_CreateEnvironment_WithValidType(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		envType  EnvironmentType
		wantType EnvironmentType
	}{
		{
			name:     "prod type",
			envType:  EnvironmentType_ENVIRONMENT_TYPE_PROD,
			wantType: EnvironmentType_ENVIRONMENT_TYPE_PROD,
		},
		{
			name:     "test type",
			envType:  EnvironmentType_ENVIRONMENT_TYPE_TEST,
			wantType: EnvironmentType_ENVIRONMENT_TYPE_TEST,
		},
		{
			name:     "dev type",
			envType:  EnvironmentType_ENVIRONMENT_TYPE_DEV,
			wantType: EnvironmentType_ENVIRONMENT_TYPE_DEV,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			repo := newFakeRepository()
			handler := NewHandler(repo, newFakeQueue(), &fakeServiceEndpointQuery{})
			req := &CreateEnvironmentRequest{
				Parent:      "deploy/scopes/dev",
				EnvName:     "alpha",
				Environment: &Environment{Description: "alpha", DesiredState: newProtoDesiredState(), Type: tt.envType},
			}

			// when
			got, err := handler.CreateEnvironment(ctx, req)

			// then
			if err != nil {
				t.Fatalf("CreateEnvironment() error = %v", err)
			}
			if got.GetType() != tt.wantType {
				t.Fatalf("CreateEnvironment() type = %v, want %v", got.GetType(), tt.wantType)
			}
		})
	}
}

func TestHandler_CreateEnvironment_RejectUnspecified(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newFakeRepository()
	handler := NewHandler(repo, newFakeQueue(), &fakeServiceEndpointQuery{})
	req := &CreateEnvironmentRequest{
		Parent:      "deploy/scopes/dev",
		EnvName:     "alpha",
		Environment: &Environment{Description: "alpha", DesiredState: newProtoDesiredState(), Type: EnvironmentType_ENVIRONMENT_TYPE_UNSPECIFIED},
	}

	// when
	_, err := handler.CreateEnvironment(ctx, req)

	// then
	assertStatusCode(t, err, codes.InvalidArgument)
}

func TestHandler_UpdateEnvironment_RejectTypeModification(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newFakeRepository()
	seed := mustNewDomainEnvironment(t, "dev", "alpha", newDesiredState())
	if err := seed.MarkReconciling(); err != nil {
		t.Fatalf("MarkReconciling() error = %v", err)
	}
	if err := seed.MarkReady(seed.Generation()); err != nil {
		t.Fatalf("MarkReady() error = %v", err)
	}
	if err := repo.Save(ctx, seed); err != nil {
		t.Fatalf("repo.Save() error = %v", err)
	}
	handler := NewHandler(repo, newFakeQueue(), &fakeServiceEndpointQuery{})
	req := &UpdateEnvironmentRequest{
		Environment: &Environment{
			Name:         "deploy/scopes/dev/environments/alpha",
			DesiredState: newUpdatedProtoDesiredState(),
			Type:         EnvironmentType_ENVIRONMENT_TYPE_TEST,
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"desired_state"}},
	}

	// when
	_, err := handler.UpdateEnvironment(ctx, req)

	// then
	assertStatusCode(t, err, codes.InvalidArgument)
}

func TestHandler_UpdateEnvironment_AllowWithoutType(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newFakeRepository()
	seed := mustNewDomainEnvironment(t, "dev", "alpha", newDesiredState())
	if err := seed.MarkReconciling(); err != nil {
		t.Fatalf("MarkReconciling() error = %v", err)
	}
	if err := seed.MarkReady(seed.Generation()); err != nil {
		t.Fatalf("MarkReady() error = %v", err)
	}
	if err := repo.Save(ctx, seed); err != nil {
		t.Fatalf("repo.Save() error = %v", err)
	}
	handler := NewHandler(repo, newFakeQueue(), &fakeServiceEndpointQuery{})
	req := &UpdateEnvironmentRequest{
		Environment: &Environment{
			Name:         "deploy/scopes/dev/environments/alpha",
			DesiredState: newUpdatedProtoDesiredState(),
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"desired_state"}},
	}

	// when
	got, err := handler.UpdateEnvironment(ctx, req)

	// then
	if err != nil {
		t.Fatalf("UpdateEnvironment() error = %v", err)
	}
	if got.GetStatus().GetState() != EnvironmentState_ENVIRONMENT_STATE_PENDING {
		t.Fatalf("UpdateEnvironment() state = %v, want PENDING", got.GetStatus().GetState())
	}
}

func Test_fromProtoEnvironmentType(t *testing.T) {
	tests := []struct {
		input EnvironmentType
		want  domain.EnvironmentType
	}{
		{EnvironmentType_ENVIRONMENT_TYPE_UNSPECIFIED, domain.EnvironmentTypeUnspecified},
		{EnvironmentType_ENVIRONMENT_TYPE_PROD, domain.EnvironmentTypeProd},
		{EnvironmentType_ENVIRONMENT_TYPE_TEST, domain.EnvironmentTypeTest},
		{EnvironmentType_ENVIRONMENT_TYPE_DEV, domain.EnvironmentTypeDev},
		{EnvironmentType(99), domain.EnvironmentTypeUnspecified},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("proto_%d", tt.input), func(t *testing.T) {
			// when
			got := fromProtoEnvironmentType(tt.input)

			// then
			if got != tt.want {
				t.Fatalf("fromProtoEnvironmentType(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func Test_toProtoEnvironmentType(t *testing.T) {
	tests := []struct {
		input domain.EnvironmentType
		want  EnvironmentType
	}{
		{domain.EnvironmentTypeUnspecified, EnvironmentType_ENVIRONMENT_TYPE_UNSPECIFIED},
		{domain.EnvironmentTypeProd, EnvironmentType_ENVIRONMENT_TYPE_PROD},
		{domain.EnvironmentTypeTest, EnvironmentType_ENVIRONMENT_TYPE_TEST},
		{domain.EnvironmentTypeDev, EnvironmentType_ENVIRONMENT_TYPE_DEV},
		{domain.EnvironmentType(99), EnvironmentType_ENVIRONMENT_TYPE_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("domain_%d", tt.input), func(t *testing.T) {
			// when
			got := toProtoEnvironmentType(tt.input)

			// then
			if got != tt.want {
				t.Fatalf("toProtoEnvironmentType(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func Test_normalizeEnv(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want map[string]string
	}{
		{
			name: "nil maps to nil",
			env:  nil,
			want: nil,
		},
		{
			name: "empty map normalizes to nil",
			env:  map[string]string{},
			want: nil,
		},
		{
			name: "non-empty map preserved",
			env:  map[string]string{"FOO": "bar", "BAZ": "qux"},
			want: map[string]string{"FOO": "bar", "BAZ": "qux"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeEnv(tt.env)
			if (got == nil) != (tt.want == nil) {
				t.Fatalf("normalizeEnv() = %v, want %v", got, tt.want)
			}
			if got != nil && len(got) != len(tt.want) {
				t.Fatalf("normalizeEnv() len = %d, want %d", len(got), len(tt.want))
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Fatalf("normalizeEnv()[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func Test_toProtoArtifacts_fromProtoArtifacts_envRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		domain  []*domain.ArtifactSpec
		wantEnv map[string]string
	}{
		{
			name: "env round-trip preserves values",
			domain: []*domain.ArtifactSpec{{
				Name:  "api",
				App:   "gateway",
				Image: "example.com/gateway:v1",
				Env:   map[string]string{"LOG_LEVEL": "debug", "PORT": "8080"},
			}},
			wantEnv: map[string]string{"LOG_LEVEL": "debug", "PORT": "8080"},
		},
		{
			name: "nil env stays nil through round-trip",
			domain: []*domain.ArtifactSpec{{
				Name:  "api",
				App:   "gateway",
				Image: "example.com/gateway:v1",
				Env:   nil,
			}},
			wantEnv: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proto := toProtoArtifacts(tt.domain)
			got, err := fromProtoArtifacts(proto)
			if err != nil {
				t.Fatalf("fromProtoArtifacts() error = %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("fromProtoArtifacts() len = %d, want 1", len(got))
			}
			if (got[0].Env == nil) != (tt.wantEnv == nil) {
				t.Fatalf("env nil-ness = %v, want %v", got[0].Env == nil, tt.wantEnv == nil)
			}
			for k, v := range tt.wantEnv {
				if got[0].Env[k] != v {
					t.Fatalf("env[%q] = %q, want %q", k, got[0].Env[k], v)
				}
			}
		})
	}
}

func Test_toProtoArtifacts_fromProtoArtifacts_ossEnabledRoundTrip(t *testing.T) {
	tests := []struct {
		name       string
		domain     []*domain.ArtifactSpec
		wantOSS    bool
		checkRound bool
	}{
		{
			name: "oss enabled round-trip preserves true",
			domain: []*domain.ArtifactSpec{{
				Name:       "api",
				App:        "gateway",
				Image:      "example.com/gateway:v1",
				OSSEnabled: true,
			}},
			wantOSS:    true,
			checkRound: true,
		},
		{
			name: "oss disabled round-trip preserves false",
			domain: []*domain.ArtifactSpec{{
				Name:       "api",
				App:        "gateway",
				Image:      "example.com/gateway:v1",
				OSSEnabled: false,
			}},
			wantOSS:    false,
			checkRound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			proto := toProtoArtifacts(tt.domain)
			got, err := fromProtoArtifacts(proto)

			// then
			if err != nil {
				t.Fatalf("fromProtoArtifacts() error = %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("fromProtoArtifacts() len = %d, want 1", len(got))
			}
			if got[0].OSSEnabled != tt.wantOSS {
				t.Fatalf("OSSEnabled = %v, want %v", got[0].OSSEnabled, tt.wantOSS)
			}
			if tt.checkRound && got[0].OSSEnabled != tt.domain[0].OSSEnabled {
				t.Fatalf("round trip OSSEnabled = %v, want %v", got[0].OSSEnabled, tt.domain[0].OSSEnabled)
			}
		})
	}
}

func Test_fromProtoArtifacts_emptyEnvMapNormalizedToNil(t *testing.T) {
	proto := []*ArtifactSpec{{
		Name:  "api",
		App:   "gateway",
		Image: "example.com/gateway:v1",
		Env:   map[string]string{},
	}}

	got, err := fromProtoArtifacts(proto)
	if err != nil {
		t.Fatalf("fromProtoArtifacts() error = %v", err)
	}
	if got[0].Env != nil {
		t.Fatalf("env = %v, want nil (empty proto map should normalize to nil)", got[0].Env)
	}
}

func TestHandler_CreateEnvironment_ReservedEnvConflict(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		reserved    []string
		reservedErr error
		wantCode    codes.Code
		wantSaved   bool
	}{
		{
			name:      "reserved var conflict returns invalid argument and does not save",
			reserved:  []string{"PORT"},
			wantCode:  codes.InvalidArgument,
			wantSaved: false,
		},
		{
			name:      "no conflict succeeds and saves",
			reserved:  []string{"RESERVED_VAR", "S3_ACCESS_KEY", "S3_SECRET_KEY"},
			wantCode:  codes.OK,
			wantSaved: true,
		},
		{
			name:        "reserved var lookup error returns internal error",
			reservedErr: errors.New("runtime unavailable"),
			wantCode:    codes.Internal,
			wantSaved:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			repo := newFakeRepository()
			runtime := &fakeServiceEndpointQuery{
				reservedVars: tt.reserved,
				reservedErr:  tt.reservedErr,
			}
			handler := NewHandler(repo, newFakeQueue(), runtime)
			req := &CreateEnvironmentRequest{
				Parent:  "deploy/scopes/dev",
				EnvName: "alpha",
				Environment: &Environment{
					Description: "test env with env vars",
					DesiredState: &EnvironmentDesiredState{
						Artifacts: []*ArtifactSpec{{
							Name:     "api",
							App:      "gateway",
							Image:    "example.com/gateway:v1",
							Ports:    []*ArtifactPortSpec{{Name: "http", Port: 8080}},
							Replicas: 1,
							Env:      map[string]string{"PORT": "9090"},
						}},
					},
					Type: EnvironmentType_ENVIRONMENT_TYPE_PROD,
				},
			}

			// when
			_, err := handler.CreateEnvironment(ctx, req)

			// then
			assertStatusCode(t, err, tt.wantCode)
			envName, _ := domain.NewEnvironmentName("dev", "alpha")
			_, getErr := repo.Get(ctx, envName)
			saved := getErr == nil
			if saved != tt.wantSaved {
				t.Fatalf("saved = %v, want %v", saved, tt.wantSaved)
			}
		})
	}
}

func TestHandler_UpdateEnvironment_ReservedEnvConflict(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		reserved    []string
		reservedErr error
		wantCode    codes.Code
		wantSaves   int
	}{
		{
			name:      "reserved var conflict returns invalid argument and does not save",
			reserved:  []string{"PORT"},
			wantCode:  codes.InvalidArgument,
			wantSaves: 1,
		},
		{
			name:      "no conflict succeeds and saves",
			reserved:  []string{"RESERVED_VAR", "S3_ACCESS_KEY", "S3_SECRET_KEY"},
			wantCode:  codes.OK,
			wantSaves: 2,
		},
		{
			name:        "reserved var lookup error returns internal error",
			reservedErr: errors.New("runtime unavailable"),
			wantCode:    codes.Internal,
			wantSaves:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			repo := newFakeRepository()
			seed := mustNewDomainEnvironment(t, "dev", "alpha", newDesiredState())
			if err := seed.MarkReconciling(); err != nil {
				t.Fatalf("MarkReconciling() error = %v", err)
			}
			if err := seed.MarkReady(seed.Generation()); err != nil {
				t.Fatalf("MarkReady() error = %v", err)
			}
			if err := repo.Save(ctx, seed); err != nil {
				t.Fatalf("repo.Save() error = %v", err)
			}
			runtime := &fakeServiceEndpointQuery{
				reservedVars: tt.reserved,
				reservedErr:  tt.reservedErr,
			}
			handler := NewHandler(repo, newFakeQueue(), runtime)
			req := &UpdateEnvironmentRequest{
				Environment: &Environment{
					Name: "deploy/scopes/dev/environments/alpha",
					DesiredState: &EnvironmentDesiredState{
						Artifacts: []*ArtifactSpec{{
							Name:     "api",
							App:      "gateway",
							Image:    "example.com/gateway:v2",
							Ports:    []*ArtifactPortSpec{{Name: "http", Port: 8080}},
							Replicas: 2,
							Env:      map[string]string{"PORT": "9090"},
						}},
					},
				},
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"desired_state"}},
			}

			// when
			_, err := handler.UpdateEnvironment(ctx, req)

			// then
			assertStatusCode(t, err, tt.wantCode)
			if repo.saveCalls != tt.wantSaves {
				t.Fatalf("saveCalls = %d, want %d", repo.saveCalls, tt.wantSaves)
			}
		})
	}
}

func Test_toProtoArtifacts_envNilBackwardCompatible(t *testing.T) {
	artifacts := []*domain.ArtifactSpec{{
		Name:  "api",
		App:   "gateway",
		Image: "example.com/gateway:v1",
		Env:   nil,
	}}

	proto := toProtoArtifacts(artifacts)
	if len(proto) != 1 {
		t.Fatalf("len = %d, want 1", len(proto))
	}
	if proto[0].Env != nil {
		t.Fatalf("proto env = %v, want nil for nil domain env", proto[0].Env)
	}
}

func TestHandler_errorMapping(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		handler  *Handler
		call     func(context.Context, *Handler) error
		wantCode codes.Code
	}{
		{
			name:    "not found maps to not found",
			handler: NewHandler(&errorRepository{getErr: domain.ErrNotFound}, newFakeQueue(), &fakeServiceEndpointQuery{}),
			call: func(ctx context.Context, handler *Handler) error {
				_, err := handler.GetEnvironment(ctx, &GetEnvironmentRequest{Name: "deploy/scopes/dev/environments/alpha"})
				return err
			},
			wantCode: codes.NotFound,
		},
		{
			name:    "already exists maps to already exists",
			handler: NewHandler(&errorRepository{getEnv: mustNewDomainEnvironment(t, "dev", "alpha", newDesiredState())}, newFakeQueue(), &fakeServiceEndpointQuery{}),
			call: func(ctx context.Context, handler *Handler) error {
				_, err := handler.CreateEnvironment(ctx, &CreateEnvironmentRequest{Parent: "deploy/scopes/dev", EnvName: "alpha", Environment: &Environment{DesiredState: newProtoDesiredState(), Type: EnvironmentType_ENVIRONMENT_TYPE_PROD}})
				return err
			},
			wantCode: codes.AlreadyExists,
		},
		{
			name:    "invalid state maps to failed precondition",
			handler: NewHandler(&errorRepository{getEnv: mustDeletingEnvironment(t, "dev", "alpha")}, newFakeQueue(), &fakeServiceEndpointQuery{}),
			call: func(ctx context.Context, handler *Handler) error {
				_, err := handler.UpdateEnvironment(ctx, &UpdateEnvironmentRequest{Environment: &Environment{Name: "deploy/scopes/dev/environments/alpha", DesiredState: newUpdatedProtoDesiredState()}})
				return err
			},
			wantCode: codes.FailedPrecondition,
		},
		{
			name:    "invalid name maps to invalid argument",
			handler: NewHandler(newFakeRepository(), newFakeQueue(), &fakeServiceEndpointQuery{}),
			call: func(ctx context.Context, handler *Handler) error {
				_, err := handler.GetEnvironment(ctx, &GetEnvironmentRequest{Name: "bad-name"})
				return err
			},
			wantCode: codes.InvalidArgument,
		},
		{
			name:    "invalid spec maps to invalid argument",
			handler: NewHandler(newFakeRepository(), newFakeQueue(), &fakeServiceEndpointQuery{}),
			call: func(ctx context.Context, handler *Handler) error {
				_, err := handler.CreateEnvironment(ctx, &CreateEnvironmentRequest{Parent: "deploy/scopes/dev", EnvName: "alpha", Environment: &Environment{}})
				return err
			},
			wantCode: codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given/when
			err := tt.call(ctx, tt.handler)

			// then
			assertStatusCode(t, err, tt.wantCode)
		})
	}
}

type errorRepository struct {
	getEnv    *domain.Environment
	listEnvs  []*domain.Environment
	getErr    error
	listErr   error
	saveErr   error
	deleteErr error
}

type fakeServiceEndpointQuery struct {
	results         map[string]*domain.ServiceQueryResult
	errs            map[string]error
	statefulResults map[string]*domain.ServiceQueryResult
	statefulErrs    map[string]error
	err             error
	calls           []serviceEndpointQueryCall
	statefulCalls   []serviceEndpointQueryCall
	reservedVars    []string
	reservedErr     error
}

type serviceEndpointQueryCall struct {
	envLabel string
	app      string
	service  string
}

func (q *fakeServiceEndpointQuery) QueryServiceEndpoints(_ context.Context, envLabel string, app string, service string) (*domain.ServiceQueryResult, error) {
	q.calls = append(q.calls, serviceEndpointQueryCall{
		envLabel: envLabel,
		app:      app,
		service:  service,
	})

	if q.err != nil {
		return nil, q.err
	}

	key := serviceQueryKey(envLabel, app, service)
	if err, ok := q.errs[key]; ok {
		if err != nil {
			return nil, err
		}
	}

	if result, ok := q.results[key]; ok {
		return result, nil
	}

	return nil, domain.ErrServiceNotFound
}

func (q *fakeServiceEndpointQuery) QueryStatefulServiceEndpoints(_ context.Context, envLabel string, app string, service string) (*domain.ServiceQueryResult, error) {
	q.statefulCalls = append(q.statefulCalls, serviceEndpointQueryCall{
		envLabel: envLabel,
		app:      app,
		service:  service,
	})

	if q.err != nil {
		return nil, q.err
	}

	key := serviceQueryKey(envLabel, app, service)
	if err, ok := q.statefulErrs[key]; ok {
		if err != nil {
			return nil, err
		}
	}

	if result, ok := q.statefulResults[key]; ok {
		return result, nil
	}

	return nil, domain.ErrServiceNotFound
}

func (q *fakeServiceEndpointQuery) Apply(_ context.Context, _ *domain.Environment, _ func(msg string)) error {
	return nil
}

func (q *fakeServiceEndpointQuery) Delete(_ context.Context, _ domain.EnvironmentName) error {
	return nil
}

func (q *fakeServiceEndpointQuery) ReservedEnvironmentVariableNames(_ context.Context) ([]string, error) {
	if q.reservedErr != nil {
		return nil, q.reservedErr
	}
	return q.reservedVars, nil
}

func serviceQueryKey(envLabel, app, service string) string {
	return fmt.Sprintf("%s/%s/%s", envLabel, app, service)
}

func (r *errorRepository) Get(_ context.Context, _ domain.EnvironmentName) (*domain.Environment, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	if r.getEnv != nil {
		return r.getEnv, nil
	}
	return nil, domain.ErrNotFound
}

func (r *errorRepository) ListByStates(_ context.Context, _ ...domain.EnvironmentState) ([]*domain.Environment, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.listEnvs, nil
}

func (r *errorRepository) ListNeedingReconcile(_ context.Context) ([]*domain.Environment, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.listEnvs, nil
}

func (r *errorRepository) ListByScope(_ context.Context, _ string, _ int32, _ string) ([]*domain.Environment, string, error) {
	if r.listErr != nil {
		return nil, "", r.listErr
	}
	return r.listEnvs, "", nil
}

func (r *errorRepository) Save(_ context.Context, _ *domain.Environment) error {
	return r.saveErr
}

func (r *errorRepository) Delete(_ context.Context, _ domain.EnvironmentName) error {
	return r.deleteErr
}

func mustNewDomainEnvironment(t *testing.T, scope, envName string, desiredState domain.DesiredState) *domain.Environment {
	t.Helper()

	name, err := domain.NewEnvironmentName(scope, envName)
	if err != nil {
		t.Fatalf("NewEnvironmentName() error = %v", err)
	}

	env, err := domain.NewEnvironment(name, domain.EnvironmentTypeProd, envName, &desiredState)
	if err != nil {
		t.Fatalf("NewEnvironment() error = %v", err)
	}

	return env
}

func mustDeletingEnvironment(t *testing.T, scope, envName string) *domain.Environment {
	t.Helper()

	env := mustNewDomainEnvironment(t, scope, envName, newDesiredState())
	if err := env.SetDesiredAbsent(); err != nil {
		t.Fatalf("SetDesiredAbsent() error = %v", err)
	}
	if err := env.MarkDeleting(); err != nil {
		t.Fatalf("MarkDeleting() error = %v", err)
	}

	return env
}

func mustReadyDomainEnvironment(t *testing.T, scope, envName string) *domain.Environment {
	t.Helper()

	return mustReadyDomainEnvironmentWithType(t, scope, envName, domain.EnvironmentTypeProd)
}

func mustReadyDomainEnvironmentWithType(t *testing.T, scope, envName string, envType domain.EnvironmentType) *domain.Environment {
	t.Helper()
	desiredState := newDesiredState()

	return mustReadyDomainEnvironmentWithDesiredState(t, scope, envName, envType, domain.DesiredState{
		Artifacts: desiredState.Artifacts,
		Infras:    desiredState.Infras,
	})
}

func mustReadyDomainEnvironmentWithDesiredState(t *testing.T, scope, envName string, envType domain.EnvironmentType, desiredState domain.DesiredState) *domain.Environment {
	t.Helper()

	name, err := domain.NewEnvironmentName(scope, envName)
	if err != nil {
		t.Fatalf("NewEnvironmentName() error = %v", err)
	}

	env, err := domain.NewEnvironment(name, envType, envName, &domain.DesiredState{
		Artifacts: desiredState.Artifacts,
		Infras:    desiredState.Infras,
	})
	if err != nil {
		t.Fatalf("NewEnvironment() error = %v", err)
	}
	if err := env.MarkReconciling(); err != nil {
		t.Fatalf("MarkReconciling() error = %v", err)
	}
	if err := env.MarkReady(env.Generation()); err != nil {
		t.Fatalf("MarkReady() error = %v", err)
	}

	return env
}

func newDesiredState() domain.DesiredState {
	return domain.DesiredState{
		Artifacts: []*domain.ArtifactSpec{{
			Name:       "api",
			App:        "gateway",
			Image:      "example.com/gateway:v1",
			Ports:      []domain.ArtifactPortSpec{{Name: "http", Port: 8080}},
			Replicas:   1,
			TLSEnabled: true,
			HTTP: &domain.ArtifactHTTPSpec{
				Hostnames: []string{"dev.example.com"},
				Matches: []domain.HTTPRouteRule{{
					Backend: "http",
					Path: domain.HTTPPathRule{
						Type:  domain.HTTPPathRuleTypePathPrefix,
						Value: "/",
					},
				}},
			},
		}},
		Infras: []*domain.InfraSpec{{
			Resource: "redis",
			Profile:  "cache",
			Name:     "redis-main",
			App:      "gateway",
			Persistence: domain.InfraPersistenceSpec{
				Enabled: true,
			},
		}},
	}
}

func newUpdatedProtoDesiredState() *EnvironmentDesiredState {
	state := newProtoDesiredState()
	state.Artifacts[0].Image = "example.com/gateway:v2"
	state.Artifacts[0].Replicas = 2
	return state
}

func newProtoDesiredState() *EnvironmentDesiredState {
	return &EnvironmentDesiredState{
		Artifacts: []*ArtifactSpec{{
			Name:       "api",
			App:        "gateway",
			Image:      "example.com/gateway:v1",
			Ports:      []*ArtifactPortSpec{{Name: "http", Port: 8080}},
			Replicas:   1,
			TlsEnabled: true,
			Http: &ArtifactHTTPSpec{
				Hostnames: []string{"dev.example.com"},
				Matches: []*HTTPRouteRule{{
					Backend: "http",
					Path: &HTTPPathRule{
						Type:  HTTPPathRuleType_HTTP_PATH_RULE_TYPE_PATH_PREFIX,
						Value: "/",
					},
				}},
			},
		}},
		Infras: []*InfraSpec{{
			Resource: "redis",
			Profile:  "cache",
			Name:     "redis-main",
			App:      "gateway",
			Persistence: &InfraPersistenceSpec{
				Enabled: true,
			},
		}},
	}
}

func assertStatusCode(t *testing.T, err error, want codes.Code) {
	t.Helper()

	if want == codes.OK {
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		return
	}

	if err == nil {
		t.Fatalf("error = nil, want code %v", want)
	}
	if status.Code(err) != want {
		t.Fatalf("status.Code() = %v, want %v", status.Code(err), want)
	}
	if !errors.As(err, new(interface{ GRPCStatus() *status.Status })) {
		_ = err
	}
}

func assertStatusMessageContains(t *testing.T, err error, wantSubstring string) {
	t.Helper()

	st := status.Convert(err)
	if !strings.Contains(st.Message(), wantSubstring) {
		t.Fatalf("status message = %q, want substring %q", st.Message(), wantSubstring)
	}
}

func assertErrorInfo(t *testing.T, err error, wantReason string, wantMetadata map[string]string) {
	t.Helper()

	st := status.Convert(err)
	for _, detail := range st.Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok {
			if info.Reason != wantReason {
				t.Fatalf("ErrorInfo.Reason = %q, want %q", info.Reason, wantReason)
			}
			for k, v := range wantMetadata {
				if got, ok := info.Metadata[k]; !ok || got != v {
					t.Fatalf("ErrorInfo.Metadata[%q] = %q, want %q", k, got, v)
				}
			}
			return
		}
	}
	t.Fatalf("status details do not contain ErrorInfo")
}
