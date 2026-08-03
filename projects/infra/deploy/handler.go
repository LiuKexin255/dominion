// Package deploy contains the deploy service implementation.
package deploy

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	"dominion/projects/infra/deploy/domain"
	"dominion/projects/infra/deploy/service"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	deployParentPrefix              = "deploy/scopes/"
	errorDomain                     = "infra.liukexin.com"
	invalidViewReason               = "INVALID_VIEW"
	serviceEndpointsNotFoundReason  = "SERVICE_ENDPOINTS_NOT_FOUND"
	servicePortMapUnavailableReason = "SERVICE_PORT_MAP_UNAVAILABLE"

	logFieldEnvName      = "env_name"
	logFieldGeneration   = "generation"
	logFieldDesiredState = "desired_state"
	logFieldError        = "error"
)

// Handler implements DeployServiceServer.
type Handler struct {
	UnimplementedDeployServiceServer

	repo           domain.Repository
	runtime        domain.EnvironmentRuntime
	commandService *service.EnvironmentCommandService
}

// NewHandler creates a deploy gRPC handler.
func NewHandler(repo domain.Repository, runtime domain.EnvironmentRuntime, commandService *service.EnvironmentCommandService) *Handler {
	return &Handler{
		repo:           repo,
		runtime:        runtime,
		commandService: commandService,
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

// GetServiceEndpoints returns the effective runtime endpoints for a logical service.
func (h *Handler) GetServiceEndpoints(ctx context.Context, req *GetServiceEndpointsRequest) (*ServiceEndpoints, error) {
	name, err := domain.ParseServiceEndpointsName(req.GetName())
	if err != nil {
		return nil, toStatusError(err)
	}

	view, err := normalizeServiceEndpointsView(req.GetView())
	if err != nil {
		return nil, err
	}

	envName, err := name.EnvironmentName()
	if err != nil {
		return nil, toStatusError(err)
	}

	env, err := h.repo.Get(ctx, envName)
	if err != nil {
		return nil, toStatusError(err)
	}

	queryEndpoints := h.runtime.QueryServiceEndpoints
	if isStatefulService(env, name.App(), name.Service()) {
		queryEndpoints = h.runtime.QueryStatefulServiceEndpoints
	}

	result, err := queryEndpoints(ctx, name.EnvLabel(), name.App(), name.Service())
	if err == nil {
		logs.Info(ctx, "service endpoints resolved (same env)",
			event.String(logFieldEnvName, env.Name().String()),
			event.String("app", name.App()),
			event.String("service", name.Service()),
			event.Int("endpoint_count", len(result.Endpoints)+len(result.StatefulInstances)),
		)
		return newServiceEndpointsResponse(name, result, env.Name(), ResolutionMode_RESOLUTION_MODE_SAME_ENV, view), nil
	}
	if errors.Is(err, domain.ErrServicePortMapUnavailable) {
		logs.Warn(ctx, "service endpoints port map unavailable",
			event.String(logFieldEnvName, env.Name().String()),
			event.String("app", name.App()),
			event.String("service", name.Service()),
		)
		return nil, newStatusErrorWithReason(codes.FailedPrecondition, servicePortMapUnavailableReason, err.Error(), nil)
	}

	switch {
	case !errors.Is(err, domain.ErrServiceNotFound):
		logs.Error(ctx, "failed to query service endpoints",
			event.String(logFieldEnvName, env.Name().String()),
			event.String("app", name.App()),
			event.String("service", name.Service()),
			event.Err(err),
		)
		return nil, toStatusError(err)
	case env.Type() != domain.EnvironmentTypeProd:
		logs.Warn(ctx, "service endpoints not found in non-prod environment",
			event.String(logFieldEnvName, env.Name().String()),
			event.String("app", name.App()),
			event.String("service", name.Service()),
		)
		return nil, newServiceEndpointsNotFoundError(name)
	}

	fallbackEnvs, err := h.repo.ListByStates(ctx, domain.StateReady)
	if err != nil {
		return nil, toStatusError(err)
	}

	prodCandidates := filterProdCandidates(fallbackEnvs, env.Name())
	if len(prodCandidates) == 0 {
		logs.Warn(ctx, "no prod candidates for service endpoints fallback",
			event.String(logFieldEnvName, env.Name().String()),
			event.String("app", name.App()),
			event.String("service", name.Service()),
		)
		return nil, newServiceEndpointsNotFoundError(name)
	}

	sort.Slice(prodCandidates, func(i, j int) bool {
		return prodCandidates[i].Name().String() < prodCandidates[j].Name().String()
	})

	for _, candidate := range prodCandidates {
		candidateQuery := h.runtime.QueryServiceEndpoints
		if isStatefulService(candidate, name.App(), name.Service()) {
			candidateQuery = h.runtime.QueryStatefulServiceEndpoints
		}

		result, err = candidateQuery(ctx, candidate.Name().Label(), name.App(), name.Service())
		if err == nil {
			logs.Info(ctx, "service endpoints resolved (prod fallback)",
				event.String(logFieldEnvName, env.Name().String()),
				event.String("app", name.App()),
				event.String("service", name.Service()),
				event.Int("endpoint_count", len(result.Endpoints)+len(result.StatefulInstances)),
			)
			return newServiceEndpointsResponse(name, result, candidate.Name(), ResolutionMode_RESOLUTION_MODE_PROD_FALLBACK, view), nil
		}
		if errors.Is(err, domain.ErrServicePortMapUnavailable) {
			logs.Warn(ctx, "fallback service endpoints port map unavailable",
				event.String(logFieldEnvName, env.Name().String()),
				event.String("app", name.App()),
				event.String("service", name.Service()),
			)
			return nil, newStatusErrorWithReason(codes.FailedPrecondition, servicePortMapUnavailableReason, err.Error(), nil)
		}
		if errors.Is(err, domain.ErrServiceNotFound) {
			continue
		}
		logs.Error(ctx, "failed to query fallback service endpoints",
			event.String(logFieldEnvName, env.Name().String()),
			event.String("app", name.App()),
			event.String("service", name.Service()),
			event.Err(err),
		)
		return nil, toStatusError(err)
	}

	logs.Warn(ctx, "service endpoints not found in any environment",
		event.String(logFieldEnvName, env.Name().String()),
		event.String("app", name.App()),
		event.String("service", name.Service()),
	)
	return nil, newServiceEndpointsNotFoundError(name)
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

	envType := fromProtoEnvironmentType(req.GetEnvironment().GetType())
	description := req.GetEnvironment().GetDescription()
	desiredState, err := fromProtoDesiredState(req.GetEnvironment().GetDesiredState())
	if err != nil {
		return nil, toStatusError(err)
	}

	env, err := h.commandService.Create(ctx, envName, envType, description, desiredState)
	if err != nil {
		return nil, toStatusError(err)
	}

	logs.Info(ctx, "environment created",
		event.String(logFieldEnvName, envName.String()),
		event.String(logFieldDesiredState, env.Status().Desired.String()),
	)

	return toProtoEnvironment(env), nil
}

// UpdateEnvironment updates desired state and returns the pre-reconcile snapshot.
func (h *Handler) UpdateEnvironment(ctx context.Context, req *UpdateEnvironmentRequest) (*Environment, error) {
	if req.GetEnvironment() == nil {
		return nil, status.Error(codes.InvalidArgument, "environment is required")
	}

	if req.GetEnvironment().GetType() != EnvironmentType_ENVIRONMENT_TYPE_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "type is immutable")
	}

	envName, err := domain.ParseResourceName(req.GetEnvironment().GetName())
	if err != nil {
		return nil, toStatusError(err)
	}

	desiredState, err := fromProtoDesiredState(req.GetEnvironment().GetDesiredState())
	if err != nil {
		return nil, toStatusError(err)
	}

	env, err := h.commandService.Update(ctx, envName, desiredState)
	if err != nil {
		return nil, toStatusError(err)
	}

	logs.Info(ctx, "environment updated",
		event.String(logFieldEnvName, envName.String()),
		event.Int64(logFieldGeneration, env.Generation()),
		event.String(logFieldDesiredState, env.Status().Desired.String()),
	)

	return toProtoEnvironment(env), nil
}

// DeleteEnvironment marks an environment for deletion and enqueues reconciliation.
func (h *Handler) DeleteEnvironment(ctx context.Context, req *DeleteEnvironmentRequest) (*emptypb.Empty, error) {
	envName, err := domain.ParseResourceName(req.GetName())
	if err != nil {
		return nil, toStatusError(err)
	}

	if err := h.commandService.Delete(ctx, envName); err != nil {
		return nil, toStatusError(err)
	}

	logs.Info(ctx, "environment marked for deletion",
		event.String(logFieldEnvName, envName.String()),
	)

	return new(emptypb.Empty), nil
}

func toProtoEnvironment(env *domain.Environment) *Environment {
	if env == nil {
		return nil
	}

	return &Environment{
		Name:         env.Name().String(),
		Type:         toProtoEnvironmentType(env.Type()),
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

	return domain.NewEnvironment(envName, fromProtoEnvironmentType(env.GetType()), env.GetDescription(), desiredState)
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
	case domain.StateWaitingRollout:
		return EnvironmentState_ENVIRONMENT_STATE_WAITING_ROLLOUT
	default:
		return EnvironmentState_ENVIRONMENT_STATE_UNSPECIFIED
	}
}

func fromProtoEnvironmentType(t EnvironmentType) domain.EnvironmentType {
	switch t {
	case EnvironmentType_ENVIRONMENT_TYPE_PROD:
		return domain.EnvironmentTypeProd
	case EnvironmentType_ENVIRONMENT_TYPE_TEST:
		return domain.EnvironmentTypeTest
	case EnvironmentType_ENVIRONMENT_TYPE_DEV:
		return domain.EnvironmentTypeDev
	default:
		return domain.EnvironmentTypeUnspecified
	}
}

func toProtoEnvironmentType(t domain.EnvironmentType) EnvironmentType {
	switch t {
	case domain.EnvironmentTypeProd:
		return EnvironmentType_ENVIRONMENT_TYPE_PROD
	case domain.EnvironmentTypeTest:
		return EnvironmentType_ENVIRONMENT_TYPE_TEST
	case domain.EnvironmentTypeDev:
		return EnvironmentType_ENVIRONMENT_TYPE_DEV
	default:
		return EnvironmentType_ENVIRONMENT_TYPE_UNSPECIFIED
	}
}

func toProtoDesiredState(state *domain.DesiredState) *EnvironmentDesiredState {
	if state == nil {
		return nil
	}
	return &EnvironmentDesiredState{
		Artifacts: toProtoArtifacts(state.Artifacts),
		Infras:    toProtoInfras(state.Infras),
	}
}

func fromProtoDesiredState(state *EnvironmentDesiredState) (*domain.DesiredState, error) {
	if state == nil {
		return nil, domain.ErrInvalidSpec
	}

	artifacts, err := fromProtoArtifacts(state.GetArtifacts())
	if err != nil {
		return nil, err
	}

	infras, err := fromProtoInfras(state.GetInfras())
	if err != nil {
		return nil, err
	}

	return &domain.DesiredState{
		Artifacts: artifacts,
		Infras:    infras,
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
		Services:          toProtoServices(statusValue.Services),
	}
}

func toProtoServices(services []*domain.ServiceStatus) []*ServiceStatus {
	if len(services) == 0 {
		return nil
	}

	result := make([]*ServiceStatus, 0, len(services))
	for _, service := range services {
		result = append(result, &ServiceStatus{
			Name:    service.Name,
			App:     service.App,
			Kind:    serviceKindToProto(service.Kind),
			State:   serviceRolloutStateToProto(service.State),
			Message: service.Message,
		})
	}

	return result
}

func serviceKindToProto(kind domain.ServiceKind) ServiceKind {
	switch kind {
	case domain.ServiceKindArtifact:
		return ServiceKind_SERVICE_KIND_ARTIFACT
	case domain.ServiceKindInfra:
		return ServiceKind_SERVICE_KIND_INFRA
	default:
		return ServiceKind_SERVICE_KIND_UNSPECIFIED
	}
}

func serviceRolloutStateToProto(state domain.ServiceRolloutState) ServiceRolloutState {
	switch state {
	case domain.ServiceRolloutStatePending:
		return ServiceRolloutState_SERVICE_ROLLOUT_STATE_PENDING
	case domain.ServiceRolloutStateReady:
		return ServiceRolloutState_SERVICE_ROLLOUT_STATE_READY
	case domain.ServiceRolloutStateWaiting:
		return ServiceRolloutState_SERVICE_ROLLOUT_STATE_WAITING
	case domain.ServiceRolloutStateFailed:
		return ServiceRolloutState_SERVICE_ROLLOUT_STATE_FAILED
	default:
		return ServiceRolloutState_SERVICE_ROLLOUT_STATE_UNSPECIFIED
	}
}

func toProtoArtifacts(artifacts []*domain.ArtifactSpec) []*ArtifactSpec {
	if len(artifacts) == 0 {
		return nil
	}

	result := make([]*ArtifactSpec, 0, len(artifacts))
	for _, artifact := range artifacts {
		result = append(result, &ArtifactSpec{
			Name:           artifact.Name,
			App:            artifact.App,
			Image:          artifact.Image,
			Ports:          toProtoArtifactPorts(artifact.Ports),
			Replicas:       artifact.Replicas,
			TlsEnabled:     artifact.TLSEnabled,
			OssEnabled:     artifact.OSSEnabled,
			WorkloadKind:   workloadKindToProto(artifact.WorkloadKind),
			Http:           toProtoArtifactHTTP(artifact.HTTP),
			Env:            artifact.Env,
			SecretBindings: toProtoSecretBindings(artifact.SecretBindings),
		})
	}

	return result
}

func fromProtoArtifacts(artifacts []*ArtifactSpec) ([]*domain.ArtifactSpec, error) {
	if len(artifacts) == 0 {
		return nil, nil
	}

	result := make([]*domain.ArtifactSpec, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifact == nil {
			return nil, domain.ErrInvalidSpec
		}
		result = append(result, &domain.ArtifactSpec{
			Name:           artifact.GetName(),
			App:            artifact.GetApp(),
			Image:          artifact.GetImage(),
			Ports:          fromProtoArtifactPorts(artifact.GetPorts()),
			Replicas:       artifact.GetReplicas(),
			TLSEnabled:     artifact.GetTlsEnabled(),
			OSSEnabled:     artifact.GetOssEnabled(),
			WorkloadKind:   workloadKindFromProto(artifact.GetWorkloadKind()),
			HTTP:           fromProtoArtifactHTTP(artifact.GetHttp()),
			Env:            normalizeEnv(artifact.GetEnv()),
			SecretBindings: fromProtoSecretBindings(artifact.GetSecretBindings()),
		})
	}

	return result, nil
}

// normalizeEnv 将 proto map 转为 domain env，空 map 归一化为 nil。
func normalizeEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	return env
}

func workloadKindToProto(kind domain.WorkloadKind) WorkloadKind {
	switch kind {
	case domain.WorkloadKindStateful:
		return WorkloadKind_WORKLOAD_KIND_STATEFUL
	default:
		return WorkloadKind_WORKLOAD_KIND_STATELESS
	}
}

func workloadKindFromProto(kind WorkloadKind) domain.WorkloadKind {
	switch kind {
	case WorkloadKind_WORKLOAD_KIND_STATEFUL:
		return domain.WorkloadKindStateful
	default:
		return domain.WorkloadKindStateless
	}
}

func toProtoArtifactPorts(ports []domain.ArtifactPortSpec) []*ArtifactPortSpec {
	if len(ports) == 0 {
		return nil
	}

	result := make([]*ArtifactPortSpec, 0, len(ports))
	for _, port := range ports {
		result = append(result, &ArtifactPortSpec{
			Name: port.Name,
			Port: port.Port,
		})
	}

	return result
}

func fromProtoArtifactPorts(ports []*ArtifactPortSpec) []domain.ArtifactPortSpec {
	if len(ports) == 0 {
		return nil
	}

	result := make([]domain.ArtifactPortSpec, 0, len(ports))
	for _, port := range ports {
		if port == nil {
			continue
		}
		result = append(result, domain.ArtifactPortSpec{
			Name: port.GetName(),
			Port: port.GetPort(),
		})
	}

	return result
}

func toProtoSecretBindings(bindings []*domain.SecretBinding) []*SecretBinding {
	if len(bindings) == 0 {
		return nil
	}

	result := make([]*SecretBinding, 0, len(bindings))
	for _, b := range bindings {
		result = append(result, &SecretBinding{
			LogicalName: b.LogicalName,
			SecretName:  b.SecretName,
			Key:         b.Key,
		})
	}

	return result
}

func fromProtoSecretBindings(bindings []*SecretBinding) []*domain.SecretBinding {
	if len(bindings) == 0 {
		return nil
	}

	result := make([]*domain.SecretBinding, 0, len(bindings))
	for _, b := range bindings {
		if b == nil {
			continue
		}
		result = append(result, &domain.SecretBinding{
			LogicalName: b.GetLogicalName(),
			SecretName:  b.GetSecretName(),
			Key:         b.GetKey(),
		})
	}

	return result
}

func toProtoArtifactHTTP(http *domain.ArtifactHTTPSpec) *ArtifactHTTPSpec {
	if http == nil {
		return nil
	}

	return &ArtifactHTTPSpec{
		Hostnames: append([]string(nil), http.Hostnames...),
		Matches:   toProtoHTTPRouteRules(http.Matches),
	}
}

func fromProtoArtifactHTTP(http *ArtifactHTTPSpec) *domain.ArtifactHTTPSpec {
	if http == nil {
		return nil
	}

	return &domain.ArtifactHTTPSpec{
		Hostnames: append([]string(nil), http.GetHostnames()...),
		Matches:   fromProtoHTTPRouteRules(http.GetMatches()),
	}
}

func toProtoInfras(infras []*domain.InfraSpec) []*InfraSpec {
	if len(infras) == 0 {
		return nil
	}

	result := make([]*InfraSpec, 0, len(infras))
	for _, infra := range infras {
		result = append(result, &InfraSpec{
			Resource: infra.Resource,
			Profile:  infra.Profile,
			Name:     infra.Name,
			App:      infra.App,
			Persistence: &InfraPersistenceSpec{
				Enabled:  infra.Persistence.Enabled,
				Capacity: infra.Persistence.Capacity,
			},
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
			Resource: infra.GetResource(),
			Profile:  infra.GetProfile(),
			Name:     infra.GetName(),
			App:      infra.GetApp(),
			Persistence: domain.InfraPersistenceSpec{
				Enabled:  infra.GetPersistence().GetEnabled(),
				Capacity: infra.GetPersistence().GetCapacity(),
			},
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
	case errors.Is(err, domain.ErrInvalidName), errors.Is(err, domain.ErrInvalidSpec), errors.Is(err, domain.ErrInvalidType):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return status.Error(codes.Internal, fmt.Sprintf("deploy handler: %v", err))
	}
}

func normalizeServiceEndpointsView(view ServiceEndpointsView) (ServiceEndpointsView, error) {
	switch view {
	case ServiceEndpointsView_SERVICE_ENDPOINTS_VIEW_UNSPECIFIED:
		return ServiceEndpointsView_SERVICE_ENDPOINTS_VIEW_BASIC, nil
	case ServiceEndpointsView_SERVICE_ENDPOINTS_VIEW_BASIC, ServiceEndpointsView_SERVICE_ENDPOINTS_VIEW_RESOLUTION:
		return view, nil
	default:
		return ServiceEndpointsView_SERVICE_ENDPOINTS_VIEW_UNSPECIFIED, newStatusErrorWithReason(codes.InvalidArgument, invalidViewReason, fmt.Sprintf("invalid service endpoints view: %v", view), nil)
	}
}

func newServiceEndpointsResponse(name domain.ServiceEndpointsName, result *domain.ServiceQueryResult, resolvedEnv domain.EnvironmentName, mode ResolutionMode, view ServiceEndpointsView) *ServiceEndpoints {
	resp := &ServiceEndpoints{
		Name:              name.String(),
		Endpoints:         cloneStringSlice(result.Endpoints),
		Ports:             cloneInt32Map(result.Ports),
		StatefulInstances: cloneStatefulInstances(result.StatefulInstances),
		IsStateful:        result.IsStateful,
	}

	if view == ServiceEndpointsView_SERVICE_ENDPOINTS_VIEW_RESOLUTION {
		resp.ResolvedScope = resolvedEnv.Scope()
		resp.ResolvedEnvironment = resolvedEnv.EnvName()
		resp.ResolutionMode = mode
	}

	return resp
}

func newServiceEndpointsNotFoundError(name domain.ServiceEndpointsName) error {
	return newStatusErrorWithReason(
		codes.NotFound,
		serviceEndpointsNotFoundReason,
		fmt.Sprintf("service endpoints not found for %s", name.String()),
		map[string]string{
			"resource_name": name.String(),
			"app":           name.App(),
			"service":       name.Service(),
			"environment":   name.EnvName(),
		},
	)
}

func filterProdCandidates(envs []*domain.Environment, self domain.EnvironmentName) []*domain.Environment {
	var candidates []*domain.Environment
	for _, env := range envs {
		if env == nil {
			continue
		}
		if env.Type() != domain.EnvironmentTypeProd || env.Name() == self {
			continue
		}
		candidates = append(candidates, env)
	}

	return candidates
}

func isStatefulService(env *domain.Environment, app string, service string) bool {
	if env == nil {
		return false
	}
	state := env.DesiredState()
	if state == nil {
		return false
	}

	for _, artifact := range state.Artifacts {
		if artifact == nil {
			continue
		}
		if artifact.App == app && artifact.Name == service {
			return artifact.WorkloadKind == domain.WorkloadKindStateful
		}
	}

	return false
}

func newStatusErrorWithReason(code codes.Code, reason string, message string, metadata map[string]string) error {
	st, err := status.New(code, message).WithDetails(&errdetails.ErrorInfo{
		Reason:   reason,
		Domain:   errorDomain,
		Metadata: metadata,
	})
	if err != nil {
		return status.Error(code, message)
	}
	return st.Err()
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	return append([]string(nil), values...)
}

func cloneInt32Map(values map[string]int32) map[string]int32 {
	if len(values) == 0 {
		return nil
	}

	cloned := make(map[string]int32, len(values))
	for key, value := range values {
		cloned[key] = value
	}

	return cloned
}

func cloneStatefulInstances(in []*domain.StatefulInstance) []*StatefulServiceInstance {
	if in == nil {
		return nil
	}

	out := make([]*StatefulServiceInstance, 0, len(in))
	for _, inst := range in {
		if inst == nil {
			continue
		}
		out = append(out, &StatefulServiceInstance{
			Index:     int32(inst.Index),
			Hostname:  inst.Hostname,
			Endpoints: cloneStringSlice(inst.Endpoints),
		})
	}

	return out
}
