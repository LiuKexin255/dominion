package deploy

import (
	"context"
	"errors"
	"testing"

	"dominion/projects/infra/deploy/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

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
			handler := NewHandler(repo, NewReconciler(repo))

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
			handler := NewHandler(repo, NewReconciler(repo))

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
		name      string
		seed      []*domain.Environment
		repoSetup func(*fakeRepository)
		request   *CreateEnvironmentRequest
		wantState EnvironmentState
		wantCode  codes.Code
	}{
		{
			name: "success returns pending",
			request: &CreateEnvironmentRequest{
				Parent:      "deploy/scopes/dev",
				EnvName:     "alpha",
				Environment: &Environment{Description: "alpha", DesiredState: newProtoDesiredState()},
			},
			wantState: EnvironmentState_ENVIRONMENT_STATE_PENDING,
			wantCode:  codes.OK,
		},
		{
			name: "duplicate",
			seed: []*domain.Environment{mustNewDomainEnvironment(t, "dev", "alpha", newDesiredState())},
			request: &CreateEnvironmentRequest{
				Parent:      "deploy/scopes/dev",
				EnvName:     "alpha",
				Environment: &Environment{Description: "alpha", DesiredState: newProtoDesiredState()},
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
				Environment: &Environment{Description: "alpha", DesiredState: newProtoDesiredState()},
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
				Environment: &Environment{Description: "alpha", DesiredState: newProtoDesiredState()},
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
			handler := NewHandler(repo, NewReconciler(repo))

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
		})
	}
}

func TestHandler_CreateEnvironmentThenGet(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newFakeRepository()
	handler := NewHandler(repo, NewReconciler(repo))
	createReq := &CreateEnvironmentRequest{
		Parent:      "deploy/scopes/dev",
		EnvName:     "alpha",
		Environment: &Environment{Description: "alpha", DesiredState: newProtoDesiredState()},
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
	if got.GetStatus().GetState() != EnvironmentState_ENVIRONMENT_STATE_READY {
		t.Fatalf("GetEnvironment() state = %v, want %v", got.GetStatus().GetState(), EnvironmentState_ENVIRONMENT_STATE_READY)
	}
}

func TestHandler_UpdateEnvironment(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		seed      func(t *testing.T) *domain.Environment
		request   *UpdateEnvironmentRequest
		wantState EnvironmentState
		wantCode  codes.Code
	}{
		{
			name: "success returns reconciling",
			seed: func(t *testing.T) *domain.Environment {
				env := mustNewDomainEnvironment(t, "dev", "alpha", newDesiredState())
				if err := env.MarkReconciling(); err != nil {
					t.Fatalf("MarkReconciling() error = %v", err)
				}
				if err := env.MarkReady(); err != nil {
					t.Fatalf("MarkReady() error = %v", err)
				}
				return env
			},
			request: &UpdateEnvironmentRequest{
				Environment: &Environment{Name: "deploy/scopes/dev/environments/alpha", DesiredState: newUpdatedProtoDesiredState()},
				UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"desired_state"}},
			},
			wantState: EnvironmentState_ENVIRONMENT_STATE_RECONCILING,
			wantCode:  codes.OK,
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
			handler := NewHandler(repo, NewReconciler(repo))

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
	if err := seed.MarkReady(); err != nil {
		t.Fatalf("MarkReady() error = %v", err)
	}
	if err := repo.Save(ctx, seed); err != nil {
		t.Fatalf("repo.Save() error = %v", err)
	}
	handler := NewHandler(repo, NewReconciler(repo))
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
	if updated.GetStatus().GetState() != EnvironmentState_ENVIRONMENT_STATE_RECONCILING {
		t.Fatalf("UpdateEnvironment() state = %v, want %v", updated.GetStatus().GetState(), EnvironmentState_ENVIRONMENT_STATE_RECONCILING)
	}
	if got.GetStatus().GetState() != EnvironmentState_ENVIRONMENT_STATE_READY {
		t.Fatalf("GetEnvironment() state = %v, want %v", got.GetStatus().GetState(), EnvironmentState_ENVIRONMENT_STATE_READY)
	}
}

func TestHandler_DeleteEnvironment(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		seed     []*domain.Environment
		request  *DeleteEnvironmentRequest
		wantResp *emptypb.Empty
		wantCode codes.Code
	}{
		{
			name:     "success",
			seed:     []*domain.Environment{mustNewDomainEnvironment(t, "dev", "alpha", newDesiredState())},
			request:  &DeleteEnvironmentRequest{Name: "deploy/scopes/dev/environments/alpha"},
			wantResp: new(emptypb.Empty),
			wantCode: codes.OK,
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
			handler := NewHandler(repo, NewReconciler(repo))

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
		})
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
			handler: NewHandler(&errorRepository{getErr: domain.ErrNotFound}, &Reconciler{repo: &errorRepository{getErr: domain.ErrNotFound}}),
			call: func(ctx context.Context, handler *Handler) error {
				_, err := handler.GetEnvironment(ctx, &GetEnvironmentRequest{Name: "deploy/scopes/dev/environments/alpha"})
				return err
			},
			wantCode: codes.NotFound,
		},
		{
			name:    "already exists maps to already exists",
			handler: NewHandler(&errorRepository{getEnv: mustNewDomainEnvironment(t, "dev", "alpha", newDesiredState())}, nil),
			call: func(ctx context.Context, handler *Handler) error {
				_, err := handler.CreateEnvironment(ctx, &CreateEnvironmentRequest{Parent: "deploy/scopes/dev", EnvName: "alpha", Environment: &Environment{DesiredState: newProtoDesiredState()}})
				return err
			},
			wantCode: codes.AlreadyExists,
		},
		{
			name:    "invalid state maps to failed precondition",
			handler: NewHandler(&errorRepository{getEnv: mustDeletingEnvironment(t, "dev", "alpha")}, nil),
			call: func(ctx context.Context, handler *Handler) error {
				_, err := handler.UpdateEnvironment(ctx, &UpdateEnvironmentRequest{Environment: &Environment{Name: "deploy/scopes/dev/environments/alpha", DesiredState: newUpdatedProtoDesiredState()}})
				return err
			},
			wantCode: codes.FailedPrecondition,
		},
		{
			name:    "invalid name maps to invalid argument",
			handler: NewHandler(newFakeRepository(), nil),
			call: func(ctx context.Context, handler *Handler) error {
				_, err := handler.GetEnvironment(ctx, &GetEnvironmentRequest{Name: "bad-name"})
				return err
			},
			wantCode: codes.InvalidArgument,
		},
		{
			name:    "invalid spec maps to invalid argument",
			handler: NewHandler(newFakeRepository(), nil),
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

func (r *errorRepository) Get(_ context.Context, _ domain.EnvironmentName) (*domain.Environment, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	if r.getEnv != nil {
		return r.getEnv, nil
	}
	return nil, domain.ErrNotFound
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

	env, err := domain.NewEnvironment(name, envName, &desiredState)
	if err != nil {
		t.Fatalf("NewEnvironment() error = %v", err)
	}

	return env
}

func mustDeletingEnvironment(t *testing.T, scope, envName string) *domain.Environment {
	t.Helper()

	env := mustNewDomainEnvironment(t, scope, envName, newDesiredState())
	if err := env.MarkDeleting(); err != nil {
		t.Fatalf("MarkDeleting() error = %v", err)
	}

	return env
}

func newDesiredState() domain.DesiredState {
	return domain.DesiredState{
		Services: []*domain.ServiceSpec{{
			Name:       "api",
			App:        "gateway",
			Image:      "example.com/gateway:v1",
			Ports:      []domain.ServicePortSpec{{Name: "http", Port: 8080}},
			Replicas:   1,
			TLSEnabled: true,
		}},
		Infras: []*domain.InfraSpec{{
			Resource:           "redis",
			Profile:            "cache",
			Name:               "redis-main",
			App:                "gateway",
			PersistenceEnabled: true,
		}},
		HTTPRoutes: []*domain.HTTPRouteSpec{{
			Hostnames: []string{"dev.example.com"},
			Rules: []domain.HTTPRouteRule{{
				Backend: "api",
				Path: domain.HTTPPathRule{
					Type:  domain.HTTPPathRuleTypePathPrefix,
					Value: "/",
				},
			}},
		}},
	}
}

func newUpdatedProtoDesiredState() *EnvironmentDesiredState {
	state := newProtoDesiredState()
	state.Services[0].Image = "example.com/gateway:v2"
	state.Services[0].Replicas = 2
	return state
}

func newProtoDesiredState() *EnvironmentDesiredState {
	return &EnvironmentDesiredState{
		Services: []*ServiceSpec{{
			Name:       "api",
			App:        "gateway",
			Image:      "example.com/gateway:v1",
			Ports:      []*ServicePortSpec{{Name: "http", Port: 8080}},
			Replicas:   1,
			TlsEnabled: true,
		}},
		Infras: []*InfraSpec{{
			Resource:           "redis",
			Profile:            "cache",
			Name:               "redis-main",
			App:                "gateway",
			PersistenceEnabled: true,
		}},
		HttpRoutes: []*HTTPRouteSpec{{
			Hostnames: []string{"dev.example.com"},
			Matches: []*HTTPRouteRule{{
				Backend: "api",
				Path: &HTTPPathRule{
					Type:  HTTPPathRuleType_HTTP_PATH_RULE_TYPE_PATH_PREFIX,
					Value: "/",
				},
			}},
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
