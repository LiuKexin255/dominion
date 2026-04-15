// Package deploy contains the deploy service implementation.
package deploy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"dominion/projects/infra/deploy/domain"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const deployParentPrefix = "deploy/scopes/"

type Enqueuer interface {
	Enqueue(ctx context.Context, envName domain.EnvironmentName) error
	EnqueueWithPriority(ctx context.Context, envName domain.EnvironmentName) error
}

// Handler implements DeployServiceServer.
type Handler struct {
	UnimplementedDeployServiceServer

	repo  domain.Repository
	queue Enqueuer
}

// NewHandler creates a deploy gRPC handler.
func NewHandler(repo domain.Repository, queue Enqueuer) *Handler {
	return &Handler{
		repo:  repo,
		queue: queue,
	}
}

// GetEnvironment returns an environment by resource name.
func (h *Handler) GetEnvironment(ctx context.Context, req *GetEnvironmentRequest) (*Environment, error) {
	envName, err := domain.ParseResourceName(req.GetName())
	if err != nil {
		return nil, toStatusError(err)
	}

	env, err := h.repo.Get(ctx, envName)
	if err != nil {
		return nil, toStatusError(err)
	}

	return toProtoEnvironment(env), nil
}

// ListEnvironments lists environments under a scope.
func (h *Handler) ListEnvironments(ctx context.Context, req *ListEnvironmentsRequest) (*ListEnvironmentsResponse, error) {
	scope, err := parseParent(req.GetParent())
	if err != nil {
		return nil, toStatusError(err)
	}

	envs, nextPageToken, err := h.repo.ListByScope(ctx, scope, req.GetPageSize(), req.GetPageToken())
	if err != nil {
		return nil, toStatusError(err)
	}

	resp := new(ListEnvironmentsResponse)
	if len(envs) > 0 {
		resp.Environments = make([]*Environment, 0, len(envs))
		for _, env := range envs {
			resp.Environments = append(resp.Environments, toProtoEnvironment(env))
		}
	}
	resp.NextPageToken = nextPageToken

	return resp, nil
}

// CreateEnvironment creates a new environment and returns the pre-reconcile snapshot.
func (h *Handler) CreateEnvironment(ctx context.Context, req *CreateEnvironmentRequest) (*Environment, error) {
	if req.GetEnvironment() == nil {
		return nil, status.Error(codes.InvalidArgument, "environment is required")
	}

	scope, err := parseParent(req.GetParent())
	if err != nil {
		return nil, toStatusError(err)
	}

	envName, err := domain.NewEnvironmentName(scope, req.GetEnvName())
	if err != nil {
		return nil, toStatusError(err)
	}

	if _, err := h.repo.Get(ctx, envName); err == nil {
		return nil, toStatusError(domain.ErrAlreadyExists)
	} else if !errors.Is(err, domain.ErrNotFound) {
		return nil, toStatusError(err)
	}

	env, err := fromProtoEnvironment(&Environment{
		Name:         envName.String(),
		Description:  req.GetEnvironment().GetDescription(),
		DesiredState: req.GetEnvironment().GetDesiredState(),
	})
	if err != nil {
		return nil, toStatusError(err)
	}

	if err := env.MarkReconciling(); err != nil {
		return nil, toStatusError(err)
	}

	if err := h.repo.Save(ctx, env); err != nil {
		return nil, toStatusError(err)
	}

	if err := h.queue.Enqueue(ctx, envName); err != nil {
		return nil, toStatusError(err)
	}

	return toProtoEnvironment(env), nil
}

// UpdateEnvironment updates desired state and returns the pre-reconcile snapshot.
func (h *Handler) UpdateEnvironment(ctx context.Context, req *UpdateEnvironmentRequest) (*Environment, error) {
	if req.GetEnvironment() == nil {
		return nil, status.Error(codes.InvalidArgument, "environment is required")
	}

	envName, err := domain.ParseResourceName(req.GetEnvironment().GetName())
	if err != nil {
		return nil, toStatusError(err)
	}

	env, err := h.repo.Get(ctx, envName)
	if err != nil {
		return nil, toStatusError(err)
	}

	desiredState, err := fromProtoDesiredState(req.GetEnvironment().GetDesiredState())
	if err != nil {
		return nil, toStatusError(err)
	}

	if err := env.UpdateDesiredState(desiredState); err != nil {
		return nil, toStatusError(err)
	}

	if err := h.repo.Save(ctx, env); err != nil {
		return nil, toStatusError(err)
	}

	if err := h.queue.Enqueue(ctx, envName); err != nil {
		return nil, toStatusError(err)
	}

	return toProtoEnvironment(env), nil
}

func (h *Handler) DeleteEnvironment(ctx context.Context, req *DeleteEnvironmentRequest) (*emptypb.Empty, error) {
	envName, err := domain.ParseResourceName(req.GetName())
	if err != nil {
		return nil, toStatusError(err)
	}

	env, err := h.repo.Get(ctx, envName)
	if err != nil {
		return nil, toStatusError(err)
	}

	if err := env.MarkDeleting(); err != nil {
		return nil, toStatusError(err)
	}

	if err := h.repo.Save(ctx, env); err != nil {
		return nil, toStatusError(err)
	}

	if err := h.queue.EnqueueWithPriority(ctx, envName); err != nil {
		return nil, toStatusError(err)
	}

	return new(emptypb.Empty), nil
}

func toProtoEnvironment(env *domain.Environment) *Environment {
	if env == nil {
		return nil
	}

	return &Environment{
		Name:         env.Name().String(),
		Description:  env.Description(),
		DesiredState: toProtoDesiredState(env.DesiredState()),
		Status:       toProtoStatus(env.Status()),
		CreateTime:   toProtoTimestamp(env.CreateTime()),
		UpdateTime:   toProtoTimestamp(env.UpdateTime()),
		Etag:         env.ETag(),
	}
}

func fromProtoEnvironment(env *Environment) (*domain.Environment, error) {
	if env == nil {
		return nil, domain.ErrInvalidSpec
	}

	envName, err := domain.ParseResourceName(env.GetName())
	if err != nil {
		return nil, err
	}

	desiredState, err := fromProtoDesiredState(env.GetDesiredState())
	if err != nil {
		return nil, err
	}

	return domain.NewEnvironment(envName, env.GetDescription(), desiredState)
}

func toProtoState(state domain.EnvironmentState) EnvironmentState {
	switch state {
	case domain.StatePending:
		return EnvironmentState_ENVIRONMENT_STATE_PENDING
	case domain.StateReconciling:
		return EnvironmentState_ENVIRONMENT_STATE_RECONCILING
	case domain.StateReady:
		return EnvironmentState_ENVIRONMENT_STATE_READY
	case domain.StateFailed:
		return EnvironmentState_ENVIRONMENT_STATE_FAILED
	case domain.StateDeleting:
		return EnvironmentState_ENVIRONMENT_STATE_DELETING
	default:
		return EnvironmentState_ENVIRONMENT_STATE_UNSPECIFIED
	}
}

func toProtoDesiredState(state *domain.DesiredState) *EnvironmentDesiredState {
	if state == nil {
		return nil
	}
	return &EnvironmentDesiredState{
		Services:   toProtoServices(state.Services),
		Infras:     toProtoInfras(state.Infras),
		HttpRoutes: toProtoHTTPRoutes(state.HTTPRoutes),
	}
}

func fromProtoDesiredState(state *EnvironmentDesiredState) (*domain.DesiredState, error) {
	if state == nil {
		return nil, domain.ErrInvalidSpec
	}

	services, err := fromProtoServices(state.GetServices())
	if err != nil {
		return nil, err
	}

	infras, err := fromProtoInfras(state.GetInfras())
	if err != nil {
		return nil, err
	}

	routes, err := fromProtoHTTPRoutes(state.GetHttpRoutes())
	if err != nil {
		return nil, err
	}

	return &domain.DesiredState{
		Services:   services,
		Infras:     infras,
		HTTPRoutes: routes,
	}, nil
}

func toProtoStatus(statusValue *domain.EnvironmentStatus) *EnvironmentStatus {
	if statusValue == nil {
		return nil
	}
	return &EnvironmentStatus{
		State:             toProtoState(statusValue.State),
		Message:           statusValue.Message,
		LastReconcileTime: toProtoTimestamp(statusValue.LastReconcileTime),
		LastSuccessTime:   toProtoTimestamp(statusValue.LastSuccessTime),
	}
}

func toProtoServices(services []*domain.ServiceSpec) []*ServiceSpec {
	if len(services) == 0 {
		return nil
	}

	result := make([]*ServiceSpec, 0, len(services))
	for _, service := range services {
		result = append(result, &ServiceSpec{
			Name:       service.Name,
			App:        service.App,
			Image:      service.Image,
			Ports:      toProtoServicePorts(service.Ports),
			Replicas:   service.Replicas,
			TlsEnabled: service.TLSEnabled,
		})
	}

	return result
}

func fromProtoServices(services []*ServiceSpec) ([]*domain.ServiceSpec, error) {
	if len(services) == 0 {
		return nil, nil
	}

	result := make([]*domain.ServiceSpec, 0, len(services))
	for _, service := range services {
		if service == nil {
			return nil, domain.ErrInvalidSpec
		}
		result = append(result, &domain.ServiceSpec{
			Name:       service.GetName(),
			App:        service.GetApp(),
			Image:      service.GetImage(),
			Ports:      fromProtoServicePorts(service.GetPorts()),
			Replicas:   service.GetReplicas(),
			TLSEnabled: service.GetTlsEnabled(),
		})
	}

	return result, nil
}

func toProtoServicePorts(ports []domain.ServicePortSpec) []*ServicePortSpec {
	if len(ports) == 0 {
		return nil
	}

	result := make([]*ServicePortSpec, 0, len(ports))
	for _, port := range ports {
		result = append(result, &ServicePortSpec{
			Name: port.Name,
			Port: port.Port,
		})
	}

	return result
}

func fromProtoServicePorts(ports []*ServicePortSpec) []domain.ServicePortSpec {
	if len(ports) == 0 {
		return nil
	}

	result := make([]domain.ServicePortSpec, 0, len(ports))
	for _, port := range ports {
		if port == nil {
			continue
		}
		result = append(result, domain.ServicePortSpec{
			Name: port.GetName(),
			Port: port.GetPort(),
		})
	}

	return result
}

func toProtoInfras(infras []*domain.InfraSpec) []*InfraSpec {
	if len(infras) == 0 {
		return nil
	}

	result := make([]*InfraSpec, 0, len(infras))
	for _, infra := range infras {
		result = append(result, &InfraSpec{
			Resource:           infra.Resource,
			Profile:            infra.Profile,
			Name:               infra.Name,
			App:                infra.App,
			PersistenceEnabled: infra.PersistenceEnabled,
		})
	}

	return result
}

func fromProtoInfras(infras []*InfraSpec) ([]*domain.InfraSpec, error) {
	if len(infras) == 0 {
		return nil, nil
	}

	result := make([]*domain.InfraSpec, 0, len(infras))
	for _, infra := range infras {
		if infra == nil {
			return nil, domain.ErrInvalidSpec
		}
		result = append(result, &domain.InfraSpec{
			Resource:           infra.GetResource(),
			Profile:            infra.GetProfile(),
			Name:               infra.GetName(),
			App:                infra.GetApp(),
			PersistenceEnabled: infra.GetPersistenceEnabled(),
		})
	}

	return result, nil
}

func toProtoHTTPRoutes(routes []*domain.HTTPRouteSpec) []*HTTPRouteSpec {
	if len(routes) == 0 {
		return nil
	}

	result := make([]*HTTPRouteSpec, 0, len(routes))
	for _, route := range routes {
		result = append(result, &HTTPRouteSpec{
			ServiceName: route.ServiceName,
			Hostnames:   append([]string(nil), route.Hostnames...),
			Matches:     toProtoHTTPRouteRules(route.Rules),
		})
	}

	return result
}

func fromProtoHTTPRoutes(routes []*HTTPRouteSpec) ([]*domain.HTTPRouteSpec, error) {
	if len(routes) == 0 {
		return nil, nil
	}

	result := make([]*domain.HTTPRouteSpec, 0, len(routes))
	for _, route := range routes {
		if route == nil {
			return nil, domain.ErrInvalidSpec
		}
		result = append(result, &domain.HTTPRouteSpec{
			ServiceName: route.GetServiceName(),
			Hostnames:   append([]string(nil), route.GetHostnames()...),
			Rules:       fromProtoHTTPRouteRules(route.GetMatches()),
		})
	}

	return result, nil
}

func toProtoHTTPRouteRules(rules []domain.HTTPRouteRule) []*HTTPRouteRule {
	if len(rules) == 0 {
		return nil
	}

	result := make([]*HTTPRouteRule, 0, len(rules))
	for _, rule := range rules {
		result = append(result, &HTTPRouteRule{
			Backend: rule.Backend,
			Path:    toProtoHTTPPathRule(rule.Path),
		})
	}

	return result
}

func fromProtoHTTPRouteRules(rules []*HTTPRouteRule) []domain.HTTPRouteRule {
	if len(rules) == 0 {
		return nil
	}

	result := make([]domain.HTTPRouteRule, 0, len(rules))
	for _, rule := range rules {
		if rule == nil {
			continue
		}
		result = append(result, domain.HTTPRouteRule{
			Backend: rule.GetBackend(),
			Path:    fromProtoHTTPPathRule(rule.GetPath()),
		})
	}

	return result
}

func toProtoHTTPPathRule(rule domain.HTTPPathRule) *HTTPPathRule {
	return &HTTPPathRule{
		Type:  toProtoHTTPPathRuleType(rule.Type),
		Value: rule.Value,
	}
}

func fromProtoHTTPPathRule(rule *HTTPPathRule) domain.HTTPPathRule {
	if rule == nil {
		return domain.HTTPPathRule{}
	}

	return domain.HTTPPathRule{
		Type:  fromProtoHTTPPathRuleType(rule.GetType()),
		Value: rule.GetValue(),
	}
}

func toProtoHTTPPathRuleType(ruleType domain.HTTPPathRuleType) HTTPPathRuleType {
	switch ruleType {
	case domain.HTTPPathRuleTypePathPrefix:
		return HTTPPathRuleType_HTTP_PATH_RULE_TYPE_PATH_PREFIX
	default:
		return HTTPPathRuleType_HTTP_PATH_RULE_TYPE_UNSPECIFIED
	}
}

func fromProtoHTTPPathRuleType(ruleType HTTPPathRuleType) domain.HTTPPathRuleType {
	switch ruleType {
	case HTTPPathRuleType_HTTP_PATH_RULE_TYPE_PATH_PREFIX:
		return domain.HTTPPathRuleTypePathPrefix
	default:
		return domain.HTTPPathRuleTypeUnspecified
	}
}

func toProtoTimestamp(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}

	return timestamppb.New(value)
}

func parseParent(parent string) (string, error) {
	scope, ok := strings.CutPrefix(parent, deployParentPrefix)
	if !ok || scope == "" || strings.Contains(scope, "/") {
		return "", domain.ErrInvalidName
	}

	envName, err := domain.NewEnvironmentName(scope, "env")
	if err != nil {
		return "", err
	}

	return envName.Scope(), nil
}

func toStatusError(err error) error {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, domain.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, domain.ErrInvalidState):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, domain.ErrInvalidName), errors.Is(err, domain.ErrInvalidSpec):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return status.Error(codes.Internal, fmt.Sprintf("deploy handler: %v", err))
	}
}
