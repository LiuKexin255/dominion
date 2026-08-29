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
	"google.golang.org/protobuf/types/known/emptypb"
)

// newConversationClient wraps the generated client constructor as a
// package-level variable so tests can drive the forwarding branches against
// fake streams (the agentclient.NewAgentClient precedent).
var newConversationClient = func(conn *grpc.ClientConn) gamev2.ConversationServiceClient {
	return gamev2.NewConversationServiceClient(conn)
}

// ConversationHandler implements gamev2.ConversationServiceServer: the
// owner-affinity forwarding surface that routes /api/v2 conversation RPCs to
// the agent_v2 stateful instance owning the (template, session) pair
// (specs/049-agent-v2-dsh-init/research.md D4). agent_v2 keeps sessions,
// queues, and history in process memory, so requests must not drift across
// instances: Send is the only RPC that allocates an owner (get-or-create),
// while ListHistory/Dispose never allocate — a session that never sent a
// message short-circuits to an empty history / idempotent Empty at the proxy.
type ConversationHandler struct {
	gamev2.UnimplementedConversationServiceServer

	ownerStore  domain.OwnerStore
	ownerPicker domain.OwnerPicker
	manager     agentclient.Manager
	binder      bind.ServerStreamBinder[gamev2.ChatEvent]
}

// NewConversationHandler creates a new ConversationHandler.
func NewConversationHandler(
	ownerStore domain.OwnerStore,
	ownerPicker domain.OwnerPicker,
	manager agentclient.Manager,
	binder bind.ServerStreamBinder[gamev2.ChatEvent],
) *ConversationHandler {
	return &ConversationHandler{
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
func (h *ConversationHandler) Send(req *gamev2.SendRequest, stream gamev2.ConversationService_SendServer) error {
	ctx := stream.Context()

	name, err := parseConversationSession(req.GetSession())
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	if req.GetText() == "" {
		// Same routing-layer rule as agent_v2's own handler (the proxy
		// checks first, agent_v2 re-validates as the backstop):
		// contracts/conversation-api.md §2.
		return status.Error(codes.InvalidArgument, "text must be non-empty")
	}

	owner, err := h.assignConversationOwner(ctx, name.TemplateID, name.SessionID)
	if err != nil {
		return err
	}

	connRef, err := h.conversationConn(ctx, owner)
	if err != nil {
		return err
	}

	upstream, err := newConversationClient(connRef.Conn).Send(ctx, req)
	if err != nil {
		logs.Error(ctx, "conversation send: open upstream stream failed",
			event.String("template_id", name.TemplateID),
			event.String("session_id", name.SessionID),
			event.Int("agent_index", owner.OwnerIndex),
			event.Err(err),
		)
		// Request-level rejections from agent_v2 (e.g. empty-text
		// INVALID_ARGUMENT) keep their gRPC status so the front end sees the
		// mapped HTTP code; only a non-status transport failure (conn broken
		// while opening) is a proxy→agent_v2 hop break → UNAVAILABLE
		// (contracts/conversation-api.md §2.1).
		if st, ok := status.FromError(err); ok {
			return st.Err()
		}
		return status.Errorf(codes.Unavailable, "open conversation stream: %v", err)
	}

	logs.Info(ctx, "conversation stream connected",
		event.String("session_id", name.SessionID),
		event.Int("agent_index", owner.OwnerIndex),
	)

	if err := h.binder.BindServerStream(stream, upstream); err != nil {
		logs.Error(ctx, "conversation send: bind failed",
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

// ListHistory returns the session's in-memory conversation history for
// refresh/reconnect backfill. Read path: no owner (the session never sent a
// message) short-circuits to an empty response without allocating one.
func (h *ConversationHandler) ListHistory(ctx context.Context, req *gamev2.ListHistoryRequest) (*gamev2.ListHistoryResponse, error) {
	name, err := parseConversationSession(req.GetSession())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	owner, err := h.ownerStore.Get(ctx, name.TemplateID, name.SessionID)
	if err != nil {
		if errors.Is(err, domain.ErrOwnerNotFound) {
			// Never-conversed session: empty history, no allocation side effect.
			return &gamev2.ListHistoryResponse{}, nil
		}
		logs.Error(ctx, "conversation history: owner lookup failed",
			event.String("template_id", name.TemplateID),
			event.String("session_id", name.SessionID),
			event.Err(err),
		)
		return nil, mapDomainError(err)
	}

	connRef, err := h.conversationConn(ctx, owner)
	if err != nil {
		return nil, err
	}

	resp, err := newConversationClient(connRef.Conn).ListHistory(ctx, req)
	if err != nil {
		logs.Error(ctx, "conversation history: downstream call failed",
			event.String("session_id", name.SessionID),
			event.Err(err),
		)
		return nil, propagateAgentError(err, "list history")
	}
	return resp, nil
}

// Dispose releases the session's agent_v2 resources (in-flight turn aborted,
// queued messages dropped). Read path: no owner short-circuits to an
// idempotent Empty — an absent session is already released. The owner record
// itself is NOT deleted: the mapping is an affinity anchor, not session state
// (same lifecycle as the v1 owner; a fresh session re-created under the same
// resource name is guaranteed by agent_v2, FR-015).
func (h *ConversationHandler) Dispose(ctx context.Context, req *gamev2.DisposeRequest) (*emptypb.Empty, error) {
	name, err := parseConversationSession(req.GetSession())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	owner, err := h.ownerStore.Get(ctx, name.TemplateID, name.SessionID)
	if err != nil {
		if errors.Is(err, domain.ErrOwnerNotFound) {
			// Idempotent: an absent session is treated as already released.
			return &emptypb.Empty{}, nil
		}
		logs.Error(ctx, "conversation dispose: owner lookup failed",
			event.String("template_id", name.TemplateID),
			event.String("session_id", name.SessionID),
			event.Err(err),
		)
		return nil, mapDomainError(err)
	}

	connRef, err := h.conversationConn(ctx, owner)
	if err != nil {
		return nil, err
	}

	resp, err := newConversationClient(connRef.Conn).Dispose(ctx, req)
	if err != nil {
		logs.Error(ctx, "conversation dispose: downstream call failed",
			event.String("session_id", name.SessionID),
			event.Err(err),
		)
		return nil, propagateAgentError(err, "dispose")
	}
	return resp, nil
}

// parseConversationSession validates a game session resource name of the
// form templates/{template}/sessions/{session} with a known template
// (same rule as agent_v2's own handler — the proxy checks first, agent_v2
// re-validates as the backstop, specs/049-agent-v2-dsh-init/data-model.md
// §2.2).
func parseConversationSession(name string) (game.SessionName, error) {
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
// owner exists yet. Send is the only allocation point: under a
// concurrent-allocation race the persisted owner wins (ErrOwnerAlreadyExists
// re-reads the winner) — same semantics as the v1 assignOwner
// (specs/040-team-singleton-conformance/research.md §R10).
func (h *ConversationHandler) assignConversationOwner(ctx context.Context, templateID, sessionID string) (*domain.AgentOwner, error) {
	owner, err := h.ownerStore.Get(ctx, templateID, sessionID)
	if err == nil {
		return owner, nil
	}
	if !errors.Is(err, domain.ErrOwnerNotFound) {
		logs.Error(ctx, "assign conversation owner: store lookup failed",
			event.String("template_id", templateID),
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return nil, mapDomainError(err)
	}

	conns, err := h.manager.List(ctx)
	if err != nil {
		logs.Error(ctx, "assign conversation owner: list connections failed",
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
			// Concurrent Send race: another request already persisted an
			// owner for this session — reuse the winner's owner.
			existing, getErr := h.ownerStore.Get(ctx, templateID, sessionID)
			if getErr != nil {
				logs.Error(ctx, "assign conversation owner: re-read after race failed",
					event.String("template_id", templateID),
					event.String("session_id", sessionID),
					event.Err(getErr),
				)
				return nil, mapDomainError(getErr)
			}
			logs.Info(ctx, "conversation owner already allocated by concurrent send; reusing it",
				event.String("template_id", templateID),
				event.String("session_id", sessionID),
				event.Int("agent_index", existing.OwnerIndex),
			)
			return existing, nil
		}
		logs.Error(ctx, "assign conversation owner: create record failed",
			event.String("template_id", templateID),
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return nil, mapDomainError(err)
	}

	logs.Info(ctx, "conversation owner created on send",
		event.String("template_id", templateID),
		event.String("session_id", sessionID),
		event.String("owner", pickedRef.Owner),
		event.Int("agent_index", pickedRef.OwnerIndex),
	)
	return owner, nil
}

// conversationConn resolves the agent_v2 instance connection for an owner.
// Unlike the v1 team path this maps a missing connection to UNAVAILABLE: a
// chat request against an offline instance is retryable, and the two-hop
// failure table keeps proxy→agent_v2 breaks on 503
// (specs/049-agent-v2-dsh-init/contracts/conversation-api.md §2.1).
func (h *ConversationHandler) conversationConn(ctx context.Context, owner *domain.AgentOwner) (*agentclient.ConnRef, error) {
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
