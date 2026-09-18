package handler

import (
	"context"

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

// newBridgeClient wraps the generated client constructor as a package-level
// variable so tests can drive the forwarding branch against a fake stream
// (the newAgentClient precedent).
var newBridgeClient = func(conn *grpc.ClientConn) game.DesktopBridgeServiceClient {
	return game.NewDesktopBridgeServiceClient(conn)
}

// DesktopBridgeHandler implements game.DesktopBridgeServiceServer: it
// routes the desktop flow-control face to the agent_v2 instance owning the
// session (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §4).
//
// Stream is the session downlink: the request carries the session resource
// name from the URL path, the owner is resolved get-or-create, and every
// agent_v2 response frame is relayed downstream. SendFrame is the uplink
// single-shot forwarder: the owner is looked up only (a receipt never
// allocates an owner). Connect is the legacy bidirectional relay still
// carried by the gateway's WebSocket endpoint; its first UserFrame carries
// the routing pair template_id/session_id (both bare segments, injected by
// the gateway from the connect URL path) and the pre-read first frame is
// replayed to the upstream. The stream paths resolve the owner get-or-create
// — a desktop may connect before the conversation's UpdateTeam materializes
// the team, and the shared owner guarantees the flow stream and the
// conversation land on the same instance (the one holding the game state)
// (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §4).
type DesktopBridgeHandler struct {
	game.UnimplementedDesktopBridgeServiceServer

	ownerStore   domain.OwnerStore
	ownerPicker  domain.OwnerPicker
	manager      agentclient.Manager
	binder       bind.Binder
	streamBinder bind.ServerStreamBinder[game.TeamFrame]
}

// NewDesktopBridgeHandler creates a new DesktopBridgeHandler.
func NewDesktopBridgeHandler(
	ownerStore domain.OwnerStore,
	ownerPicker domain.OwnerPicker,
	manager agentclient.Manager,
	binder bind.Binder,
	streamBinder bind.ServerStreamBinder[game.TeamFrame],
) *DesktopBridgeHandler {
	return &DesktopBridgeHandler{
		ownerStore:   ownerStore,
		ownerPicker:  ownerPicker,
		manager:      manager,
		binder:       binder,
		streamBinder: streamBinder,
	}
}

// Stream opens the session's desktop downlink by forwarding the
// DesktopBridgeService.Stream request to the agent_v2 instance owning the
// session and relaying every response frame until the upstream ends. The
// session identity comes from the request path via parseAgentSession, and
// the owner is allocated get-or-create so the stream and the conversation
// share one instance (specs/066-desktop-bridge-split/contracts/
// desktop-bridge-v2.md §2.1, §4). Error mapping matches Connect/Send: a
// status-bearing rejection keeps its code; a non-status transport failure on
// the proxy→agent_v2 hop maps to UNAVAILABLE.
func (h *DesktopBridgeHandler) Stream(req *game.StreamRequest, stream game.DesktopBridgeService_StreamServer) error {
	ctx := stream.Context()

	name, err := parseAgentSession(req.GetSession())
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	owner, err := assignAgentOwner(ctx, h.ownerStore, h.ownerPicker, h.manager, name.TemplateID, name.SessionID)
	if err != nil {
		return err
	}

	connRef, err := agentV2Conn(ctx, h.manager, owner)
	if err != nil {
		return err
	}

	upstream, err := newBridgeClient(connRef.Conn).Stream(ctx, req)
	if err != nil {
		logs.Error(ctx, "desktop bridge stream: open upstream stream failed",
			event.String("template_id", name.TemplateID),
			event.String("session_id", name.SessionID),
			event.Int("agent_index", owner.OwnerIndex),
			event.Err(err),
		)
		// Same split as AgentHandler.Send: a status-bearing rejection keeps
		// its code; only a non-status transport failure is a proxy→agent_v2
		// hop break → UNAVAILABLE
		// (specs/066-desktop-bridge-split/contracts/desktop-bridge-v2.md §4).
		if st, ok := status.FromError(err); ok {
			return st.Err()
		}
		return status.Errorf(codes.Unavailable, "open desktop bridge stream: %v", err)
	}

	logs.Info(ctx, "desktop bridge stream: connected",
		event.String("template_id", name.TemplateID),
		event.String("session_id", name.SessionID),
		event.Int("agent_index", owner.OwnerIndex),
	)

	if err := h.streamBinder.BindServerStream(stream, upstream); err != nil {
		logs.Error(ctx, "desktop bridge stream: bind failed",
			event.String("template_id", name.TemplateID),
			event.String("session_id", name.SessionID),
			event.Int("agent_index", owner.OwnerIndex),
			event.Err(err),
		)
		return err
	}
	return nil
}

// SendFrame forwards one desktop uplink frame to the agent_v2 instance
// owning the session. The owner is looked up only — a receipt never
// allocates an owner, and no owner → NOT_FOUND (the desktop re-establishes
// the downlink before retrying; the plugin answers the no-active-stream case
// with FAILED_PRECONDITION once the owner is reached). The synchronous
// response carries no information (correlation rides on
// flow_result.tool_id), so a downstream error is passed through with its
// code preserved (specs/066-desktop-bridge-split/contracts/
// desktop-bridge-v2.md §2.6/§2.7/§4; the Cancel forwarding shape).
func (h *DesktopBridgeHandler) SendFrame(ctx context.Context, req *game.SendFrameRequest) (*game.SendFrameResponse, error) {
	name, err := parseAgentSession(req.GetSession())
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

	resp, err := newBridgeClient(connRef.Conn).SendFrame(ctx, req)
	if err != nil {
		logs.Error(ctx, "desktop bridge sendframe: downstream call failed",
			event.String("template_id", name.TemplateID),
			event.String("session_id", name.SessionID),
			event.Err(err),
		)
		return nil, propagateAgentError(err, "send frame")
	}
	return resp, nil
}

// Connect establishes the bidirectional relay for one desktop flow stream.
// Error mapping follows the bridge contract
// (specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md §4):
// malformed first-frame identity → INVALID_ARGUMENT, no instance to
// allocate (Mongo/store failures) → INTERNAL via mapDomainError, no live
// instance or unreachable owner instance → UNAVAILABLE.
func (h *DesktopBridgeHandler) Connect(stream game.DesktopBridgeService_ConnectServer) error {
	ctx := stream.Context()

	frame, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "failed to receive initial frame: %v", err)
	}

	templateID := frame.GetTemplateId()
	sessionID := frame.GetSessionId()
	if templateID == "" || sessionID == "" || !gameconst.IsKnownTemplateID(templateID) {
		return status.Error(codes.InvalidArgument, "first frame must carry a known template_id and a non-empty session_id")
	}

	owner, err := assignAgentOwner(ctx, h.ownerStore, h.ownerPicker, h.manager, templateID, sessionID)
	if err != nil {
		return err
	}

	connRef, err := agentV2Conn(ctx, h.manager, owner)
	if err != nil {
		return err
	}

	upstream, err := newBridgeClient(connRef.Conn).Connect(ctx)
	if err != nil {
		logs.Error(ctx, "desktop bridge: open upstream stream failed",
			event.String("template_id", templateID),
			event.String("session_id", sessionID),
			event.Int("agent_index", owner.OwnerIndex),
			event.Err(err),
		)
		// Same split as AgentHandler.Send: a status-bearing rejection keeps
		// its code; only a non-status transport failure is a proxy→agent_v2
		// hop break → UNAVAILABLE (contracts/agent-api.md §3).
		if st, ok := status.FromError(err); ok {
			return st.Err()
		}
		return status.Errorf(codes.Unavailable, "open desktop bridge stream: %v", err)
	}

	logs.Info(ctx, "desktop bridge stream connected",
		event.String("template_id", templateID),
		event.String("session_id", sessionID),
		event.Int("agent_index", owner.OwnerIndex),
	)

	prefixed := bind.WithFirstFrame(stream, frame)
	if err := h.binder.Bind(prefixed, upstream); err != nil {
		logs.Error(ctx, "desktop bridge: bind failed",
			event.String("template_id", templateID),
			event.String("session_id", sessionID),
			event.Int("agent_index", owner.OwnerIndex),
			event.Err(err),
		)
		return err
	}
	return nil
}
