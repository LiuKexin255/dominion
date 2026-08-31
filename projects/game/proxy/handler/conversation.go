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
	gamev2 "dominion/projects/game/v2"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// newAgentClient wraps the generated client constructor as a package-level
// variable so tests can drive the forwarding branches against fake streams
// (the agentclient.NewAgentClient precedent).
var newAgentClient = func(conn *grpc.ClientConn) gamev2.AgentServiceClient {
	return gamev2.NewAgentServiceClient(conn)
}

// AgentHandler implements gamev2.AgentServiceServer: the owner-affinity
// forwarding surface that routes /api/v2 agent RPCs to the agent_v2 stateful
// instance owning the (template, session) pair
// (specs/051-agent-v2-dsh-migration/research.md D9). agent_v2 keeps
// sessions, queues, and history in process memory, so requests must not
// drift across instances.
//
// Phase-2 wiring state: Send is the only implemented RPC — it forwards the
// stream and allocates the owner via assignConversationOwner (get-or-create,
// the 049 allocation semantics kept as a placeholder). Every other AgentService
// RPC (UpdateAgent, GetAgent, ListAgentMessages, preset CRUD, ListModels)
// answers UNIMPLEMENTED via the embedded server interface. The real proxy
// semantics — UpdateAgent as the owner allocation point, Send lookup without
// allocation (specs/051-agent-v2-dsh-migration/data-model.md §2.9) — and the
// DesktopBridgeService forwarding surface are defined by
// specs/051-agent-v2-dsh-migration/tasks.md T018 (host-side handler wiring:
// T014).
type AgentHandler struct {
	gamev2.UnimplementedAgentServiceServer

	ownerStore  domain.OwnerStore
	ownerPicker domain.OwnerPicker
	manager     agentclient.Manager
	binder      bind.ServerStreamBinder[gamev2.ChatEvent]
}

// NewAgentHandler creates a new AgentHandler.
func NewAgentHandler(
	ownerStore domain.OwnerStore,
	ownerPicker domain.OwnerPicker,
	manager agentclient.Manager,
	binder bind.ServerStreamBinder[gamev2.ChatEvent],
) *AgentHandler {
	return &AgentHandler{
		ownerStore:  ownerStore,
		ownerPicker: ownerPicker,
		manager:     manager,
		binder:      binder,
	}
}

// Send forwards one user message to the agent_v2 instance owning the session
// and relays the ChatEvent stream until the turn ends. First Send allocates
// the owner (get-or-create; ErrOwnerAlreadyExists races re-read the winner,
// same semantics as the v1 assignOwner); the routing-layer validation rejects
// malformed resource names with INVALID_ARGUMENT before any allocation.
func (h *AgentHandler) Send(req *gamev2.SendRequest, stream gamev2.AgentService_SendServer) error {
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

	owner, err := h.assignConversationOwner(ctx, name.TemplateID, name.SessionID)
	if err != nil {
		return err
	}

	connRef, err := h.agentConn(ctx, owner)
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

// assignConversationOwner returns the existing agent_v2 owner for a
// (templateID, sessionID) pair, or picks and persists a new one when no
// owner exists yet. Under a concurrent-allocation race the persisted owner
// wins (ErrOwnerAlreadyExists re-reads the winner) — same semantics as the
// v1 assignOwner (specs/040-team-singleton-conformance/research.md §R10).
func (h *AgentHandler) assignConversationOwner(ctx context.Context, templateID, sessionID string) (*domain.AgentOwner, error) {
	owner, err := h.ownerStore.Get(ctx, templateID, sessionID)
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

	conns, err := h.manager.List(ctx)
	if err != nil {
		logs.Error(ctx, "assign agent owner: list connections failed",
			event.String("template_id", templateID),
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return nil, status.Errorf(codes.Internal, "list agent_v2 connections: %v", err)
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
		if errors.Is(err, domain.ErrOwnerAlreadyExists) {
			// Concurrent allocation race: another request already persisted
			// an owner for this session — reuse the winner's owner.
			existing, getErr := h.ownerStore.Get(ctx, templateID, sessionID)
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

// agentConn resolves the agent_v2 instance connection for an owner.
// A chat request against an offline instance is retryable, and the two-hop
// failure table keeps proxy→agent_v2 breaks on 503
// (specs/051-agent-v2-dsh-migration/contracts/agent-api.md §3).
func (h *AgentHandler) agentConn(ctx context.Context, owner *domain.AgentOwner) (*agentclient.ConnRef, error) {
	connRef, err := h.manager.Get(ctx, owner.OwnerIndex)
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
