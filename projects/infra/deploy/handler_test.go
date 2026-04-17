package deploy

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"dominion/projects/infra/deploy/domain"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

type fakeQueue struct {
	normalEnqueued   []domain.EnvironmentName
	priorityEnqueued []domain.EnvironmentName
	err              error
}

func newFakeQueue() *fakeQueue {
	return &fakeQueue{}
}

func (q *fakeQueue) Enqueue(_ context.Context, envName domain.EnvironmentName) error {
	if q.err != nil {
		return q.err
	}
	q.normalEnqueued = append(q.normalEnqueued, envName)
	return nil
}

func (q *fakeQueue) EnqueueWithPriority(_ context.Context, envName domain.EnvironmentName) error {
	if q.err != nil {
		return q.err
	}
	q.priorityEnqueued = append(q.priorityEnqueued, envName)
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
			handler := NewHandler(repo, newFakeQueue())

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
			handler := NewHandler(repo, newFakeQueue())

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
			name: "success returns reconciling and enqueues",
			request: &CreateEnvironmentRequest{
				Parent:      "deploy/scopes/dev",
				EnvName:     "alpha",
				Environment: &Environment{Description: "alpha", DesiredState: newProtoDesiredState(), Type: EnvironmentType_ENVIRONMENT_TYPE_PROD},
			},
			wantState:    EnvironmentState_ENVIRONMENT_STATE_RECONCILING,
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
			handler := NewHandler(repo, q)

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
			if len(q.normalEnqueued) != tt.wantEnqueued {
				t.Fatalf("CreateEnvironment() enqueued = %d, want %d", len(q.normalEnqueued), tt.wantEnqueued)
			}
		})
	}
}

func TestHandler_CreateEnvironmentThenGet(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newFakeRepository()
	handler := NewHandler(repo, newFakeQueue())
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
	if created.GetStatus().GetState() != EnvironmentState_ENVIRONMENT_STATE_RECONCILING {
		t.Fatalf("CreateEnvironment() state = %v, want %v", created.GetStatus().GetState(), EnvironmentState_ENVIRONMENT_STATE_RECONCILING)
	}
	if got.GetStatus().GetState() != EnvironmentState_ENVIRONMENT_STATE_RECONCILING {
		t.Fatalf("GetEnvironment() state = %v, want %v (async: no worker to reconcile)", got.GetStatus().GetState(), EnvironmentState_ENVIRONMENT_STATE_RECONCILING)
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
			name: "success returns reconciling and enqueues",
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
			wantState:    EnvironmentState_ENVIRONMENT_STATE_RECONCILING,
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
			handler := NewHandler(repo, q)

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
			if len(q.normalEnqueued) != tt.wantEnqueued {
				t.Fatalf("UpdateEnvironment() enqueued = %d, want %d", len(q.normalEnqueued), tt.wantEnqueued)
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
	handler := NewHandler(repo, newFakeQueue())
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
	if got.GetStatus().GetState() != EnvironmentState_ENVIRONMENT_STATE_RECONCILING {
		t.Fatalf("GetEnvironment() state = %v, want %v (async: no worker to reconcile)", got.GetStatus().GetState(), EnvironmentState_ENVIRONMENT_STATE_RECONCILING)
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
			name:         "success marks deleting and enqueues with priority",
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
			handler := NewHandler(repo, q)

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
			if len(q.priorityEnqueued) != tt.wantEnqueued {
				t.Fatalf("DeleteEnvironment() priority enqueued = %d, want %d", len(q.priorityEnqueued), tt.wantEnqueued)
			}
		})
	}
}

func TestHandler_DeleteEnvironmentKeepsEnvInRepo(t *testing.T) {
	ctx := context.Background()

	// given
	env := mustNewDomainEnvironment(t, "dev", "alpha", newDesiredState())
	repo := newFakeRepository(env)
	handler := NewHandler(repo, newFakeQueue())

	// when
	_, err := handler.DeleteEnvironment(ctx, &DeleteEnvironmentRequest{Name: "deploy/scopes/dev/environments/alpha"})
	if err != nil {
		t.Fatalf("DeleteEnvironment() error = %v", err)
	}

	// then - env still exists with deleting state
	got, err := repo.Get(ctx, env.Name())
	if err != nil {
		t.Fatalf("Get() error = %v, want env to still exist in repo", err)
	}
	if got.Status().State != domain.StateDeleting {
		t.Fatalf("env state = %v, want %v", got.Status().State, domain.StateDeleting)
	}
}

func TestCreateEnvironment_WithValidType(t *testing.T) {
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
			handler := NewHandler(repo, newFakeQueue())
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

func TestCreateEnvironment_RejectUnspecified(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newFakeRepository()
	handler := NewHandler(repo, newFakeQueue())
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

func TestUpdateEnvironment_RejectTypeModification(t *testing.T) {
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
	handler := NewHandler(repo, newFakeQueue())
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

func TestUpdateEnvironment_AllowWithoutType(t *testing.T) {
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
	handler := NewHandler(repo, newFakeQueue())
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
	if got.GetStatus().GetState() != EnvironmentState_ENVIRONMENT_STATE_RECONCILING {
		t.Fatalf("UpdateEnvironment() state = %v, want RECONCILING", got.GetStatus().GetState())
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
			handler: NewHandler(&errorRepository{getErr: domain.ErrNotFound}, newFakeQueue()),
			call: func(ctx context.Context, handler *Handler) error {
				_, err := handler.GetEnvironment(ctx, &GetEnvironmentRequest{Name: "deploy/scopes/dev/environments/alpha"})
				return err
			},
			wantCode: codes.NotFound,
		},
		{
			name:    "already exists maps to already exists",
			handler: NewHandler(&errorRepository{getEnv: mustNewDomainEnvironment(t, "dev", "alpha", newDesiredState())}, newFakeQueue()),
			call: func(ctx context.Context, handler *Handler) error {
				_, err := handler.CreateEnvironment(ctx, &CreateEnvironmentRequest{Parent: "deploy/scopes/dev", EnvName: "alpha", Environment: &Environment{DesiredState: newProtoDesiredState(), Type: EnvironmentType_ENVIRONMENT_TYPE_PROD}})
				return err
			},
			wantCode: codes.AlreadyExists,
		},
		{
			name:    "invalid state maps to failed precondition",
			handler: NewHandler(&errorRepository{getEnv: mustDeletingEnvironment(t, "dev", "alpha")}, newFakeQueue()),
			call: func(ctx context.Context, handler *Handler) error {
				_, err := handler.UpdateEnvironment(ctx, &UpdateEnvironmentRequest{Environment: &Environment{Name: "deploy/scopes/dev/environments/alpha", DesiredState: newUpdatedProtoDesiredState()}})
				return err
			},
			wantCode: codes.FailedPrecondition,
		},
		{
			name:    "invalid name maps to invalid argument",
			handler: NewHandler(newFakeRepository(), newFakeQueue()),
			call: func(ctx context.Context, handler *Handler) error {
				_, err := handler.GetEnvironment(ctx, &GetEnvironmentRequest{Name: "bad-name"})
				return err
			},
			wantCode: codes.InvalidArgument,
		},
		{
			name:    "invalid spec maps to invalid argument",
			handler: NewHandler(newFakeRepository(), newFakeQueue()),
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

func (r *errorRepository) ListByStates(_ context.Context, _ ...domain.EnvironmentState) ([]*domain.Environment, error) {
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
	if err := env.MarkDeleting(); err != nil {
		t.Fatalf("MarkDeleting() error = %v", err)
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
