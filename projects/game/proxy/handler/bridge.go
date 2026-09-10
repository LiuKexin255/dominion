package handler

import (
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
// relays the desktop flow-control stream from the gateway's
// /api/v2 WebSocket endpoint to the agent_v2 instance owning the session
// (specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md §4).
//
// The first UserFrame carries the routing pair template_id/session_id
// (both bare segments, injected by the gateway from the connect URL path).
// The owner is resolved get-or-create — a desktop may connect before the
// conversation's UpdateTeam materializes the team, and the shared owner
// guarantees the flow stream and the conversation land on the same instance
// (the one holding the game state). Frames are relayed verbatim in both
// directions; the pre-read first frame is replayed to the upstream so the
// agent_v2 bridge sees the probe that bound the connection.
type DesktopBridgeHandler struct {
	game.UnimplementedDesktopBridgeServiceServer

	ownerStore  domain.OwnerStore
	ownerPicker domain.OwnerPicker
	manager     agentclient.Manager
	binder      bind.Binder
}

// NewDesktopBridgeHandler creates a new DesktopBridgeHandler.
func NewDesktopBridgeHandler(
	ownerStore domain.OwnerStore,
	ownerPicker domain.OwnerPicker,
	manager agentclient.Manager,
	binder bind.Binder,
) *DesktopBridgeHandler {
	return &DesktopBridgeHandler{
		ownerStore:  ownerStore,
		ownerPicker: ownerPicker,
		manager:     manager,
		binder:      binder,
	}
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
