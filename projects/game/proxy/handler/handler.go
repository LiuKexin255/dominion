// Package handler implements the TeamService gRPC server interface (spec
// 031-team-template-mode: ProxyService/AgentService merged into TeamService).
//
// The handler owns owner resolution, agent-client routing, and bidirectional
// stream binding directly. There is no separate service layer: GetTeam/
// Connect/ListMessages/RefreshTeam require an existing owner (CreateTeam is
// the only RPC that allocates one) — the Team must be created explicitly via
// CreateTeam before any other TeamService RPC (no lazy creation on Connect,
// spec 031-team-template-mode design decision: Agent 移除懒加载模式).
package handler

import (
	"context"
	"errors"
	"strings"
	"time"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	game "dominion/projects/game"
	"dominion/projects/game/pkg/bind"
	"dominion/projects/game/proxy/domain"
	"dominion/projects/game/proxy/runtime/agentclient"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TeamHandler implements game.TeamServiceServer.
type TeamHandler struct {
	game.UnimplementedTeamServiceServer

	ownerStore  domain.OwnerStore
	ownerPicker domain.OwnerPicker
	manager     agentclient.Manager
	binder      bind.Binder
}

// NewTeamHandler creates a new TeamHandler.
func NewTeamHandler(
	ownerStore domain.OwnerStore,
	ownerPicker domain.OwnerPicker,
	manager agentclient.Manager,
	binder bind.Binder,
) *TeamHandler {
	return &TeamHandler{
		ownerStore:  ownerStore,
		ownerPicker: ownerPicker,
		manager:     manager,
		binder:      binder,
	}
}

// CreateTeam creates the Team of a Session explicitly (AIP-133:
// https://google.aip.dev/133). This is the ONLY RPC that allocates an owner
// for a session: an owner is picked and persisted here (assignOwner), then
// the request — carrying the TeamProfile full resource name — is forwarded
// to the agent owning the session, which builds the team graph. All other
// TeamService RPCs (GetTeam/Connect/ListMessages/RefreshTeam) require the
// owner to already exist: the former lazy owner allocation on Connect was
// removed (Agent 移除懒加载模式 — spec 031-team-template-mode design decision).
func (h *TeamHandler) CreateTeam(ctx context.Context, req *game.CreateTeamRequest) (*game.Team, error) {
	name, err := game.ParseSessionName(req.GetParent())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	owner, err := h.assignOwner(ctx, name.TemplateID, name.SessionID)
	if err != nil {
		return nil, err
	}

	client, err := h.agentClient(ctx, owner)
	if err != nil {
		return nil, err
	}

	team, err := client.CreateTeam(ctx, req)
	if err != nil {
		logs.Error(ctx, "create team: downstream call failed",
			event.String("session_id", name.SessionID),
			event.Err(err),
		)
		return nil, propagateAgentError(err, "create team")
	}
	return team, nil
}

// GetTeam returns the Team resource identified by name
// (templates/{template}/sessions/{session}/team). The owner must already
// exist; it is created only by CreateTeam.
func (h *TeamHandler) GetTeam(ctx context.Context, req *game.GetTeamRequest) (*game.Team, error) {
	name, err := req.ParseName()
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	owner, err := h.lookupOwner(ctx, name.TemplateID, name.SessionID)
	if err != nil {
		return nil, err
	}

	client, err := h.agentClient(ctx, owner)
	if err != nil {
		return nil, err
	}

	team, err := client.GetTeam(ctx, req)
	if err != nil {
		logs.Error(ctx, "get team: downstream call failed",
			event.String("session_id", name.SessionID),
			event.Err(err),
		)
		return nil, propagateAgentError(err, "get team")
	}
	return team, nil
}

// ListMessages lists messages of one team agent's partition
// (parent templates/{template}/sessions/{session}/team/agents/{agent}).
// The owner must already exist.
func (h *TeamHandler) ListMessages(ctx context.Context, req *game.ListMessagesRequest) (*game.ListMessagesResponse, error) {
	template, sessionID, _, err := parseMessagesParent(req.GetParent())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	owner, err := h.lookupOwner(ctx, template, sessionID)
	if err != nil {
		return nil, err
	}

	client, err := h.agentClient(ctx, owner)
	if err != nil {
		return nil, err
	}

	resp, err := client.ListMessages(ctx, req)
	if err != nil {
		logs.Error(ctx, "list messages: downstream call failed",
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return nil, propagateAgentError(err, "list messages")
	}
	return resp, nil
}

// Connect establishes a bidirectional streaming channel for team
// communication (spec 031-team-template-mode FR-004). The owner must already
// exist (CreateTeam is the only RPC that allocates one): Connect does NOT
// allocate an owner anymore — a session without a created team yields
// NotFound, consistent with GetTeam/ListMessages/RefreshTeam. The first
// UserFrame carries the routing pair template_id/session_id (both bare
// segments, injected by the gateway from the connect URL path —
// specs/031-team-template-mode/contracts/api-contract.md §2.2); the session
// resource name is reconstructed from the pair without parsing. The inbound
// direction is UserFrame and the outbound direction is TeamFrame
// (specs/035-proto-contract-refine/contracts/frame-split.md §6.3).
func (h *TeamHandler) Connect(stream game.TeamService_ConnectServer) error {
	ctx := stream.Context()

	frame, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "failed to receive initial frame: %v", err)
	}

	if frame.GetTemplateId() == "" || frame.GetSessionId() == "" {
		return status.Error(codes.InvalidArgument, "frame must carry both template_id and session_id")
	}
	name := game.SessionName{TemplateID: frame.GetTemplateId(), SessionID: frame.GetSessionId()}

	owner, err := h.lookupOwner(ctx, name.TemplateID, name.SessionID)
	if err != nil {
		return err
	}

	client, err := h.agentClient(ctx, owner)
	if err != nil {
		return err
	}

	agentStream, err := client.Connect(ctx)
	if err != nil {
		logs.Error(ctx, "connect team: open stream failed",
			event.String("session_id", name.SessionID),
			event.Err(err),
		)
		return status.Errorf(codes.Internal, "connect to agent: %v", err)
	}

	logs.Info(ctx, "team stream connected",
		event.String("session_id", name.SessionID),
		event.Int("agent_index", owner.OwnerIndex),
	)

	prefixed := bind.WithFirstFrame(stream, frame)
	if err := h.binder.Bind(prefixed, agentStream); err != nil {
		logs.Error(ctx, "connect team: bind failed",
			event.String("session_id", name.SessionID),
			event.Int("agent_index", owner.OwnerIndex),
			event.Err(err),
		)
		return err
	}
	return nil
}

// RefreshTeam forwards a RefreshTeam request to the agent owning the session.
// The owner must already exist (CreateTeam must have run first).
func (h *TeamHandler) RefreshTeam(ctx context.Context, req *game.RefreshTeamRequest) (*emptypb.Empty, error) {
	name, err := req.ParseName()
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	owner, err := h.lookupOwner(ctx, name.TemplateID, name.SessionID)
	if err != nil {
		return nil, err
	}

	client, err := h.agentClient(ctx, owner)
	if err != nil {
		return nil, err
	}

	resp, err := client.RefreshTeam(ctx, req)
	if err != nil {
		logs.Error(ctx, "refresh team: downstream call failed",
			event.String("session_id", name.SessionID),
			event.Err(err),
		)
		return nil, propagateAgentError(err, "refresh team")
	}
	return resp, nil
}

// parseMessagesParent validates a ListMessages parent of the form
// "templates/{template}/sessions/{session}/team/agents/{agent}" (FR-005) and
// returns the template, session ID and agent name.
func parseMessagesParent(parent string) (template, sessionID, agent string, err error) {
	segments := strings.Split(parent, "/")
	if len(segments) != 7 || segments[0] != "templates" || segments[2] != "sessions" ||
		segments[4] != "team" || segments[5] != "agents" ||
		segments[1] == "" || segments[3] == "" || segments[6] == "" {
		return "", "", "", errors.New("parent must be of the form templates/{template}/sessions/{session}/team/agents/{agent}")
	}
	return segments[1], segments[3], segments[6], nil
}

// lookupOwner returns the existing owner for a (templateID, sessionID) pair
// or a mapped status error. It does NOT create an owner; only CreateTeam
// allocates one.
func (h *TeamHandler) lookupOwner(ctx context.Context, templateID, sessionID string) (*domain.AgentOwner, error) {
	owner, err := h.ownerStore.Get(ctx, templateID, sessionID)
	if err != nil {
		logs.Error(ctx, "owner lookup failed",
			event.String("template_id", templateID),
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return nil, mapDomainError(err)
	}
	return owner, nil
}

// assignOwner returns the existing owner for a (templateID, sessionID) pair,
// or picks and persists a new one when no owner exists yet. Used only by
// CreateTeam (idempotent: a repeated CreateTeam for an already-created session
// reuses its owner). Under a concurrent-create race the persisted owner wins:
// ErrOwnerAlreadyExists from Create re-reads the existing owner instead of
// erroring (S1).
func (h *TeamHandler) assignOwner(ctx context.Context, templateID, sessionID string) (*domain.AgentOwner, error) {
	owner, err := h.ownerStore.Get(ctx, templateID, sessionID)
	if err == nil {
		return owner, nil
	}
	if !errors.Is(err, domain.ErrOwnerNotFound) {
		logs.Error(ctx, "assign owner: store lookup failed",
			event.String("template_id", templateID),
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return nil, mapDomainError(err)
	}

	conns, err := h.manager.List(ctx)
	if err != nil {
		logs.Error(ctx, "assign owner: list connections failed",
			event.String("template_id", templateID),
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return nil, status.Errorf(codes.Internal, "list agent connections: %v", err)
	}

	pickedRef, err := h.ownerPicker.Pick(ctx, sessionID, conns)
	if err != nil {
		return nil, mapDomainError(err)
	}

	now := time.Now()
	owner = &domain.AgentOwner{
		TemplateID: templateID,
		SessionID:  sessionID,
		OwnerIndex: pickedRef.OwnerIndex,
		Owner:      pickedRef.Owner,
		CreateTime: now,
	}
	if err := h.ownerStore.Create(ctx, owner); err != nil {
		// Concurrent CreateTeam race: another request already persisted an
		// owner for this session. The proxy-layer owner allocation is
		// idempotent — re-read the winner's owner instead of surfacing
		// AlreadyExists (S1; the agent-side profile check is independent).
		if errors.Is(err, domain.ErrOwnerAlreadyExists) {
			existing, getErr := h.ownerStore.Get(ctx, templateID, sessionID)
			if getErr != nil {
				logs.Error(ctx, "assign owner: re-read after race failed",
					event.String("template_id", templateID),
					event.String("session_id", sessionID),
					event.Err(getErr),
				)
				return nil, mapDomainError(getErr)
			}
			logs.Info(ctx, "owner already allocated by concurrent create team; reusing it",
				event.String("template_id", templateID),
				event.String("session_id", sessionID),
				event.Int("agent_index", existing.OwnerIndex),
			)
			return existing, nil
		}
		logs.Error(ctx, "assign owner: create record failed",
			event.String("template_id", templateID),
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return nil, mapDomainError(err)
	}

	logs.Info(ctx, "owner created on create team",
		event.String("template_id", templateID),
		event.String("session_id", sessionID),
		event.String("owner", pickedRef.Owner),
		event.Int("agent_index", pickedRef.OwnerIndex),
	)
	return owner, nil
}

// agentClient resolves the agent connection for an owner and wraps it as a client.
func (h *TeamHandler) agentClient(ctx context.Context, owner *domain.AgentOwner) (agentclient.Client, error) {
	connRef, err := h.manager.Get(ctx, owner.OwnerIndex)
	if err != nil {
		logs.Error(ctx, "get agent connection failed",
			event.String("session_id", owner.SessionID),
			event.Int("agent_index", owner.OwnerIndex),
			event.Err(err),
		)
		return nil, status.Errorf(codes.Internal, "get agent connection: %v", err)
	}
	return agentclient.NewAgentClient(connRef.Conn), nil
}

// propagateAgentError returns a downstream gRPC status error unchanged, or wraps
// a non-status error as Internal so the proxy does not mask agent-level codes.
func propagateAgentError(err error, msg string) error {
	if err == nil {
		return nil
	}
	if st, ok := status.FromError(err); ok {
		return st.Err()
	}
	return status.Errorf(codes.Internal, "%s: %v", msg, err)
}

// mapDomainError converts domain errors to gRPC status errors.
func mapDomainError(err error) error {
	switch {
	case errors.Is(err, domain.ErrOwnerNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, domain.ErrOwnerAlreadyExists):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, domain.ErrNoAgentInstances):
		return status.Error(codes.Unavailable, err.Error())
	default:
		return err
	}
}
