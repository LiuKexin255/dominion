// Package handler implements the proxy's gRPC forwarding surface: the
// agent_v2 AgentService face (UpdateAgent/GetAgent/ListAgentMessages/Send/
// Cancel) and the DesktopBridgeService flow stream. Both route to the
// agent_v2 instance owning the (template, session) pair through the owner
// store — owner affinity, because the agent/game state lives in the serving
// instance's process memory and must not drift across instances
// (specs/051-agent-v2-dsh-migration/research.md D9). The handlers own owner
// resolution, agent-client routing, and stream binding directly; there is no
// separate service layer.
package handler

import (
	"context"
	"errors"
	"time"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	game "dominion/projects/game"
	"dominion/projects/game/pkg/bind"
	gameconst "dominion/projects/game/pkg/gameconst"
	"dominion/projects/game/proxy/domain"
	"dominion/projects/game/proxy/runtime/agentclient"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// newAgentClient wraps the generated client constructor as a package-level
// variable so tests can drive the forwarding branches against fake streams
// (the agentclient.NewAgentClient precedent).
var newAgentClient = func(conn *grpc.ClientConn) game.AgentServiceClient {
	return game.NewAgentServiceClient(conn)
}

// AgentHandler implements game.AgentServiceServer: the forwarding surface
// that routes the /api/v2 agent RPCs to the agent_v2 instance owning the
// session — owner affinity, because the agent/queue/game state lives in the
// serving instance's process memory and must not drift across instances
// (specs/051-agent-v2-dsh-migration/research.md D9). UpdateAgent is the only
// owner allocation point (get-or-create — materialization lands the owner,
// so a desktop flow connection and the conversation that follow reach the
// same instance); GetAgent/ListAgentMessages/Send/Cancel only look the owner
// up and answer NOT_FOUND when absent (Send has no lazy materialization —
// specs/051-agent-v2-dsh-migration/contracts/agent-api.md §2.4). The
// stateless configuration face (PresetService) is not routed here: preset
// state lives in Mongo and the model catalog is static, so the gateway dials
// agent_v2 directly for it
// (specs/051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md §3.4).
type AgentHandler struct {
	game.UnimplementedAgentServiceServer

	ownerStore  domain.OwnerStore
	ownerPicker domain.OwnerPicker
	manager     agentclient.Manager
	binder      bind.ServerStreamBinder[game.ChatEvent]
}

// NewAgentHandler creates a new AgentHandler.
func NewAgentHandler(
	ownerStore domain.OwnerStore,
	ownerPicker domain.OwnerPicker,
	manager agentclient.Manager,
	binder bind.ServerStreamBinder[game.ChatEvent],
) *AgentHandler {
	return &AgentHandler{
		ownerStore:  ownerStore,
		ownerPicker: ownerPicker,
		manager:     manager,
		binder:      binder,
	}
}

// UpdateAgent forwards the materialization request to the agent_v2 instance
// owning the session and is the ONLY owner allocation point on the agent
// surface (get-or-create; ErrOwnerAlreadyExists races re-read the winner).
// The proxy is a routing layer: preset/model validation and the
// create-or-update semantics (AIP-134 create-or-update,
// https://google.aip.dev/134#create-or-update) belong to agent_v2
// (specs/051-agent-v2-dsh-migration/contracts/agent-api.md §2.1).
func (h *AgentHandler) UpdateAgent(ctx context.Context, req *game.UpdateAgentRequest) (*game.Agent, error) {
	name, err := parseAgentResourceName(req.GetAgent().GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	owner, err := assignAgentOwner(ctx, h.ownerStore, h.ownerPicker, h.manager, name.TemplateID, name.SessionID)
	if err != nil {
		return nil, err
	}

	connRef, err := agentV2Conn(ctx, h.manager, owner)
	if err != nil {
		return nil, err
	}

	agent, err := newAgentClient(connRef.Conn).UpdateAgent(ctx, req)
	if err != nil {
		logs.Error(ctx, "update agent: downstream call failed",
			event.String("session_id", name.SessionID),
			event.Err(err),
		)
		return nil, propagateAgentError(err, "update agent")
	}
	return agent, nil
}

// GetAgent returns the materialized agent of a session. The owner must
// already exist (UpdateAgent allocates it): no owner → NOT_FOUND — the
// agent is not materialized for routing purposes either
// (specs/051-agent-v2-dsh-migration/contracts/agent-api.md §2.2).
func (h *AgentHandler) GetAgent(ctx context.Context, req *game.GetAgentRequest) (*game.Agent, error) {
	name, err := parseAgentResourceName(req.GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	owner, err := lookupAgentOwner(ctx, h.ownerStore, name.TemplateID, name.SessionID)
	if err != nil {
		return nil, err
	}

	connRef, err := agentV2Conn(ctx, h.manager, owner)
	if err != nil {
		return nil, err
	}

	agent, err := newAgentClient(connRef.Conn).GetAgent(ctx, req)
	if err != nil {
		logs.Error(ctx, "get agent: downstream call failed",
			event.String("session_id", name.SessionID),
			event.Err(err),
		)
		return nil, propagateAgentError(err, "get agent")
	}
	return agent, nil
}

// ListAgentMessages lists the agent's in-memory history. The owner must
// already exist: no owner → NOT_FOUND (a never-materialized agent has no
// history to list, specs/051-agent-v2-dsh-migration/contracts/
// agent-api.md §2.2/§2.3).
func (h *AgentHandler) ListAgentMessages(ctx context.Context, req *game.ListAgentMessagesRequest) (*game.ListAgentMessagesResponse, error) {
	name, err := parseAgentResourceName(req.GetParent())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	owner, err := lookupAgentOwner(ctx, h.ownerStore, name.TemplateID, name.SessionID)
	if err != nil {
		return nil, err
	}

	connRef, err := agentV2Conn(ctx, h.manager, owner)
	if err != nil {
		return nil, err
	}

	resp, err := newAgentClient(connRef.Conn).ListAgentMessages(ctx, req)
	if err != nil {
		logs.Error(ctx, "list agent messages: downstream call failed",
			event.String("session_id", name.SessionID),
			event.Err(err),
		)
		return nil, propagateAgentError(err, "list agent messages")
	}
	return resp, nil
}

// Cancel forwards the cancel request to the agent_v2 instance owning the
// session. The owner is looked up, never allocated (lookup-only family:
// GetAgent/ListAgentMessages/Send/Cancel): no owner → NOT_FOUND — for
// routing purposes there is no agent to cancel. All cancel semantics
// (in-flight turn termination, queue landing, idempotent no-op) live in
// agent_v2 (specs/054-agent-v2-bugfixes/contracts/agent-api-changes.md §3);
// the proxy is a pure routing layer.
func (h *AgentHandler) Cancel(ctx context.Context, req *game.CancelRequest) (*game.CancelResponse, error) {
	name, err := parseAgentResourceName(req.GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	owner, err := lookupAgentOwner(ctx, h.ownerStore, name.TemplateID, name.SessionID)
	if err != nil {
		return nil, err
	}

	connRef, err := agentV2Conn(ctx, h.manager, owner)
	if err != nil {
		return nil, err
	}

	resp, err := newAgentClient(connRef.Conn).Cancel(ctx, req)
	if err != nil {
		logs.Error(ctx, "cancel agent: downstream call failed",
			event.String("session_id", name.SessionID),
			event.Err(err),
		)
		return nil, propagateAgentError(err, "cancel agent")
	}
	return resp, nil
}

// Send forwards one user message to the agent_v2 instance owning the session
// and relays the ChatEvent stream until the turn ends. The owner is looked
// up, never allocated: no owner → NOT_FOUND (the first layer of the
// unmaterialized-Send rejection; the second — owner present but agent not
// materialized, e.g. after an agent_v2 restart — is answered by agent_v2
// with FAILED_PRECONDITION, specs/051-agent-v2-dsh-migration/
// contracts/agent-api.md §2.4). The routing-layer validation rejects
// malformed resource names and empty text with INVALID_ARGUMENT before any
// owner lookup.
func (h *AgentHandler) Send(req *game.SendRequest, stream game.AgentService_SendServer) error {
	ctx := stream.Context()

	name, err := parseAgentSession(req.GetSession())
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	if req.GetText() == "" {
		// Same routing-layer rule as agent_v2's own handler (the proxy
		// checks first, agent_v2 re-validates as the backstop):
		// contracts/agent-api.md §2.4.
		return status.Error(codes.InvalidArgument, "text must be non-empty")
	}

	owner, err := lookupAgentOwner(ctx, h.ownerStore, name.TemplateID, name.SessionID)
	if err != nil {
		return err
	}

	connRef, err := agentV2Conn(ctx, h.manager, owner)
	if err != nil {
		return err
	}

	upstream, err := newAgentClient(connRef.Conn).Send(ctx, req)
	if err != nil {
		logs.Error(ctx, "agent send: open upstream stream failed",
			event.String("template_id", name.TemplateID),
			event.String("session_id", name.SessionID),
			event.Int("agent_index", owner.OwnerIndex),
			event.Err(err),
		)
		// Request-level rejections from agent_v2 (e.g. empty-text
		// INVALID_ARGUMENT) keep their gRPC status so the front end sees the
		// mapped HTTP code; only a non-status transport failure (conn broken
		// while opening) is a proxy→agent_v2 hop break → UNAVAILABLE
		// (contracts/agent-api.md §3).
		if st, ok := status.FromError(err); ok {
			return st.Err()
		}
		return status.Errorf(codes.Unavailable, "open agent stream: %v", err)
	}

	logs.Info(ctx, "agent stream connected",
		event.String("session_id", name.SessionID),
		event.Int("agent_index", owner.OwnerIndex),
	)

	if err := h.binder.BindServerStream(stream, upstream); err != nil {
		logs.Error(ctx, "agent send: bind failed",
			event.String("session_id", name.SessionID),
			event.Int("agent_index", owner.OwnerIndex),
			event.Err(err),
		)
		// Downstream/upstream stream errors keep their gRPC status: the
		// proxy does not rewrite agent-level codes (propagateAgentError
		// semantics — the pump returns status-bearing errors unchanged).
		return err
	}
	return nil
}

// parseAgentSession validates a game session resource name of the
// form templates/{template}/sessions/{session} with a known template
// (same rule as agent_v2's own handler — the proxy checks first, agent_v2
// re-validates as the backstop, specs/051-agent-v2-dsh-migration/
// data-model.md §3).
func parseAgentSession(name string) (game.SessionName, error) {
	parsed, err := game.ParseSessionName(name)
	if err != nil {
		return game.SessionName{}, err
	}
	if !gameconst.IsKnownTemplateID(parsed.TemplateID) {
		return game.SessionName{}, errors.New("unknown template " + parsed.TemplateID)
	}
	return parsed, nil
}

// parseAgentResourceName validates an agent singleton resource name of the
// form templates/{template}/sessions/{session}/agent and returns the parsed
// name whose fields are the owner key. The name shape (5 segments, the
// templates/sessions/agent literals, non-empty variables) is carried by the
// generated parser; the known-template check is a business rule owned by
// gameconst — codegen does not carry it (same rule as agent_v2's own
// handler: the proxy checks first, agent_v2 re-validates as the backstop,
// specs/051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md §2.4).
func parseAgentResourceName(name string) (game.AgentName, error) {
	parsed, err := game.ParseAgentName(name)
	if err != nil {
		return game.AgentName{}, err
	}
	if !gameconst.IsKnownTemplateID(parsed.TemplateID) {
		return game.AgentName{}, errors.New("unknown template " + parsed.TemplateID)
	}
	return parsed, nil
}

// lookupAgentOwner returns the existing agent_v2 owner for a
// (templateID, sessionID) pair or a mapped status error. It does NOT create
// an owner; only UpdateAgent (and the desktop bridge's Connect) allocate one.
func lookupAgentOwner(ctx context.Context, store domain.OwnerStore, templateID, sessionID string) (*domain.AgentOwner, error) {
	owner, err := store.Get(ctx, templateID, sessionID)
	if err != nil {
		logs.Error(ctx, "agent owner lookup failed",
			event.String("template_id", templateID),
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return nil, mapDomainError(err)
	}
	return owner, nil
}

// assignAgentOwner returns the existing agent_v2 owner for a
// (templateID, sessionID) pair, or picks and persists a new one when no
// owner exists yet. Under a concurrent-allocation race the persisted owner
// wins (ErrOwnerAlreadyExists re-reads the winner) — same semantics as the
// v1 assignOwner (specs/040-team-singleton-conformance/research.md §R10).
// Used by UpdateAgent's materialization path and the desktop bridge's
// Connect (specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md §4).
func assignAgentOwner(ctx context.Context, store domain.OwnerStore, picker domain.OwnerPicker, manager agentclient.Manager, templateID, sessionID string) (*domain.AgentOwner, error) {
	owner, err := store.Get(ctx, templateID, sessionID)
	if err == nil {
		return owner, nil
	}
	if !errors.Is(err, domain.ErrOwnerNotFound) {
		logs.Error(ctx, "assign agent owner: store lookup failed",
			event.String("template_id", templateID),
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return nil, mapDomainError(err)
	}

	conns, err := manager.List(ctx)
	if err != nil {
		logs.Error(ctx, "assign agent owner: list connections failed",
			event.String("template_id", templateID),
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return nil, status.Errorf(codes.Internal, "list agent_v2 connections: %v", err)
	}

	pickedRef, err := picker.Pick(ctx, sessionID, conns)
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
	if err := store.Create(ctx, owner); err != nil {
		if errors.Is(err, domain.ErrOwnerAlreadyExists) {
			// Concurrent allocation race: another request already persisted
			// an owner for this session — reuse the winner's owner.
			existing, getErr := store.Get(ctx, templateID, sessionID)
			if getErr != nil {
				logs.Error(ctx, "assign agent owner: re-read after race failed",
					event.String("template_id", templateID),
					event.String("session_id", sessionID),
					event.Err(getErr),
				)
				return nil, mapDomainError(getErr)
			}
			logs.Info(ctx, "agent owner already allocated by a concurrent request; reusing it",
				event.String("template_id", templateID),
				event.String("session_id", sessionID),
				event.Int("agent_index", existing.OwnerIndex),
			)
			return existing, nil
		}
		logs.Error(ctx, "assign agent owner: create record failed",
			event.String("template_id", templateID),
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return nil, mapDomainError(err)
	}

	logs.Info(ctx, "agent owner created",
		event.String("template_id", templateID),
		event.String("session_id", sessionID),
		event.String("owner", pickedRef.Owner),
		event.Int("agent_index", pickedRef.OwnerIndex),
	)
	return owner, nil
}

// agentV2Conn resolves the agent_v2 instance connection for an owner.
// A request against an offline instance is retryable, and the two-hop
// failure table keeps proxy→agent_v2 breaks on 503
// (specs/051-agent-v2-dsh-migration/contracts/agent-api.md §3).
func agentV2Conn(ctx context.Context, manager agentclient.Manager, owner *domain.AgentOwner) (*agentclient.ConnRef, error) {
	connRef, err := manager.Get(ctx, owner.OwnerIndex)
	if err != nil {
		logs.Error(ctx, "get agent_v2 connection failed",
			event.String("session_id", owner.SessionID),
			event.Int("agent_index", owner.OwnerIndex),
			event.Err(err),
		)
		return nil, status.Errorf(codes.Unavailable, "agent_v2 instance %d unreachable: %v", owner.OwnerIndex, err)
	}
	return connRef, nil
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

// mapDomainError converts domain errors to gRPC status errors. The default
// branch is an unexpected owner-store failure (e.g. Mongo unreachable) — it
// maps to Internal so the two-hop failure table's store-failure row holds
// instead of grpc-go's Unknown fallback for a bare error
// (specs/051-agent-v2-dsh-migration/contracts/agent-api.md §3).
func mapDomainError(err error) error {
	switch {
	case errors.Is(err, domain.ErrOwnerNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, domain.ErrOwnerAlreadyExists):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, domain.ErrNoAgentInstances):
		return status.Error(codes.Unavailable, err.Error())
	default:
		return status.Errorf(codes.Internal, "%v", err)
	}
}
