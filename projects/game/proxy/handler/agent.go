package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

// newAgentClient wraps the generated client constructor as a package-level
// variable so tests can drive the forwarding branches against fake streams
// (the agentclient.NewAgentClient precedent).
var newAgentClient = func(conn *grpc.ClientConn) gamev2.AgentServiceClient {
	return gamev2.NewAgentServiceClient(conn)
}

// listModelsPickKey is the hash key for the deployment-level ListModels RPC:
// the model catalog has no parent resource, so a fixed key keeps the
// request-to-instance mapping stable across callers
// (specs/051-agent-v2-dsh-migration/data-model.md §2.9).
const listModelsPickKey = "models"

// AgentHandler implements gamev2.AgentServiceServer: the forwarding surface
// that routes /api/v2 agent RPCs to the agent_v2 stateful instances
// (specs/051-agent-v2-dsh-migration/research.md D9). Routing is split by
// where the RPC's state lives
// (specs/051-agent-v2-dsh-migration/data-model.md §2.9):
//   - owner-affinity (the in-memory agent state must not drift across
//     instances): UpdateAgent is the only owner allocation point
//     (get-or-create — materialization lands the owner, so a desktop flow
//     connection and the conversation that follows reach the same instance);
//     GetAgent/ListAgentMessages/Send only look the owner up and answer
//     NOT_FOUND when absent (Send has no lazy materialization —
//     specs/051-agent-v2-dsh-migration/contracts/agent-api.md §2.4).
//   - affinity-free (state lives in Mongo, any live instance can serve):
//     preset CRUD and ListModels pick an instance by stable-hashing a
//     request-derived key (the preset resource name; the parent collection
//     for lists) with no owner record.
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

// UpdateAgent forwards the materialization request to the agent_v2 instance
// owning the session and is the ONLY owner allocation point on the agent
// surface (get-or-create; ErrOwnerAlreadyExists races re-read the winner).
// The proxy is a routing layer: preset/model validation and the
// create-or-update semantics (AIP-134 create-or-update,
// https://google.aip.dev/134#create-or-update) belong to agent_v2
// (specs/051-agent-v2-dsh-migration/contracts/agent-api.md §2.1).
func (h *AgentHandler) UpdateAgent(ctx context.Context, req *gamev2.UpdateAgentRequest) (*gamev2.Agent, error) {
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
func (h *AgentHandler) GetAgent(ctx context.Context, req *gamev2.GetAgentRequest) (*gamev2.Agent, error) {
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
func (h *AgentHandler) ListAgentMessages(ctx context.Context, req *gamev2.ListAgentMessagesRequest) (*gamev2.ListAgentMessagesResponse, error) {
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

// Send forwards one user message to the agent_v2 instance owning the session
// and relays the ChatEvent stream until the turn ends. The owner is looked
// up, never allocated: no owner → NOT_FOUND (the first layer of the
// unmaterialized-Send rejection; the second — owner present but agent not
// materialized, e.g. after an agent_v2 restart — is answered by agent_v2
// with FAILED_PRECONDITION, specs/051-agent-v2-dsh-migration/
// contracts/agent-api.md §2.4). The routing-layer validation rejects
// malformed resource names and empty text with INVALID_ARGUMENT before any
// owner lookup.
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

// CreatePreset forwards preset creation to an arbitrary live agent_v2
// instance (affinity-free: preset state lives in Mongo). The routing layer
// validates the parent template and the caller-supplied id fail-fast
// (specs/051-agent-v2-dsh-migration/data-model.md §3).
func (h *AgentHandler) CreatePreset(ctx context.Context, req *gamev2.CreatePresetRequest) (*gamev2.Preset, error) {
	templateID, err := parseTemplateParent(req.GetParent())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if req.GetPresetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "preset_id must be non-empty")
	}

	connRef, err := h.affinityFreeConn(ctx, req.GetParent()+"/presets/"+req.GetPresetId())
	if err != nil {
		return nil, err
	}

	preset, err := newAgentClient(connRef.Conn).CreatePreset(ctx, req)
	if err != nil {
		logs.Error(ctx, "create preset: downstream call failed",
			event.String("template_id", templateID),
			event.Err(err),
		)
		return nil, propagateAgentError(err, "create preset")
	}
	return preset, nil
}

// ListPresets lists a template's presets on an arbitrary live instance
// (affinity-free; hashed by the parent collection name).
func (h *AgentHandler) ListPresets(ctx context.Context, req *gamev2.ListPresetsRequest) (*gamev2.ListPresetsResponse, error) {
	templateID, err := parseTemplateParent(req.GetParent())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	connRef, err := h.affinityFreeConn(ctx, req.GetParent())
	if err != nil {
		return nil, err
	}

	resp, err := newAgentClient(connRef.Conn).ListPresets(ctx, req)
	if err != nil {
		logs.Error(ctx, "list presets: downstream call failed",
			event.String("template_id", templateID),
			event.Err(err),
		)
		return nil, propagateAgentError(err, "list presets")
	}
	return resp, nil
}

// GetPreset gets a preset on an arbitrary live instance (affinity-free;
// hashed by the preset resource name).
func (h *AgentHandler) GetPreset(ctx context.Context, req *gamev2.GetPresetRequest) (*gamev2.Preset, error) {
	if _, err := parsePresetName(req.GetName()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	connRef, err := h.affinityFreeConn(ctx, req.GetName())
	if err != nil {
		return nil, err
	}

	preset, err := newAgentClient(connRef.Conn).GetPreset(ctx, req)
	if err != nil {
		logs.Error(ctx, "get preset: downstream call failed",
			event.Err(err),
		)
		return nil, propagateAgentError(err, "get preset")
	}
	return preset, nil
}

// UpdatePreset updates a preset on an arbitrary live instance
// (affinity-free; hashed by the preset resource name). The routing layer
// rejects a missing resource name fail-fast
// (specs/051-agent-v2-dsh-migration/data-model.md §3); the FieldMask
// application is agent_v2 semantics (AIP-134, https://google.aip.dev/134).
func (h *AgentHandler) UpdatePreset(ctx context.Context, req *gamev2.UpdatePresetRequest) (*gamev2.Preset, error) {
	if _, err := parsePresetName(req.GetPreset().GetName()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	connRef, err := h.affinityFreeConn(ctx, req.GetPreset().GetName())
	if err != nil {
		return nil, err
	}

	preset, err := newAgentClient(connRef.Conn).UpdatePreset(ctx, req)
	if err != nil {
		logs.Error(ctx, "update preset: downstream call failed",
			event.Err(err),
		)
		return nil, propagateAgentError(err, "update preset")
	}
	return preset, nil
}

// DeletePreset deletes a preset on an arbitrary live instance
// (affinity-free; hashed by the preset resource name). Deletion does not
// cascade to materialized agents — that is agent_v2 semantics
// (specs/051-agent-v2-dsh-migration/contracts/agent-api.md §2.5).
func (h *AgentHandler) DeletePreset(ctx context.Context, req *gamev2.DeletePresetRequest) (*emptypb.Empty, error) {
	if _, err := parsePresetName(req.GetName()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	connRef, err := h.affinityFreeConn(ctx, req.GetName())
	if err != nil {
		return nil, err
	}

	resp, err := newAgentClient(connRef.Conn).DeletePreset(ctx, req)
	if err != nil {
		logs.Error(ctx, "delete preset: downstream call failed",
			event.Err(err),
		)
		return nil, propagateAgentError(err, "delete preset")
	}
	return resp, nil
}

// ListModels forwards the read-only model catalog query to an arbitrary live
// instance (affinity-free; the catalog is deployment-level with no parent
// resource, so a fixed key keeps the mapping stable).
func (h *AgentHandler) ListModels(ctx context.Context, req *gamev2.ListModelsRequest) (*gamev2.ListModelsResponse, error) {
	connRef, err := h.affinityFreeConn(ctx, listModelsPickKey)
	if err != nil {
		return nil, err
	}

	resp, err := newAgentClient(connRef.Conn).ListModels(ctx, req)
	if err != nil {
		logs.Error(ctx, "list models: downstream call failed",
			event.Err(err),
		)
		return nil, propagateAgentError(err, "list models")
	}
	return resp, nil
}

// affinityFreeConn resolves an agent_v2 connection for an affinity-free RPC:
// the hash picker spreads requests by the request-derived stable key across
// the live instances, then the connection is resolved by instance index. No
// owner record is read or written. Failures are logged by the pick key
// (not session_id) — these RPCs are not session-scoped, so a session field
// would always be empty and misleading.
func (h *AgentHandler) affinityFreeConn(ctx context.Context, key string) (*agentclient.ConnRef, error) {
	conns, err := h.manager.List(ctx)
	if err != nil {
		logs.Error(ctx, "pick agent_v2 instance: list connections failed",
			event.String("route_key", key),
			event.Err(err),
		)
		return nil, status.Errorf(codes.Internal, "list agent_v2 connections: %v", err)
	}

	pickedRef, err := h.ownerPicker.Pick(ctx, key, conns)
	if err != nil {
		return nil, mapDomainError(err)
	}

	connRef, err := h.manager.Get(ctx, pickedRef.OwnerIndex)
	if err != nil {
		logs.Error(ctx, "get agent_v2 connection failed",
			event.String("route_key", key),
			event.Int("agent_index", pickedRef.OwnerIndex),
			event.Err(err),
		)
		return nil, status.Errorf(codes.Unavailable, "agent_v2 instance %d unreachable: %v", pickedRef.OwnerIndex, err)
	}
	return connRef, nil
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
// form templates/{template}/sessions/{session}/agent and returns the
// underlying session (the owner key); the template check follows
// parseAgentSession's rule.
func parseAgentResourceName(name string) (game.SessionName, error) {
	segments := strings.Split(name, "/")
	if len(segments) != 5 || segments[4] != "agent" {
		return game.SessionName{}, errors.New("agent resource name must be of the form templates/{template}/sessions/{session}/agent")
	}
	return parseAgentSession(strings.Join(segments[:4], "/"))
}

// parseTemplateParent validates a template parent resource name
// (templates/{template}) with a known template.
func parseTemplateParent(parent string) (string, error) {
	parsed, err := game.ParseTemplateName(parent)
	if err != nil {
		return "", err
	}
	if !gameconst.IsKnownTemplateID(parsed.TemplateID) {
		return "", fmt.Errorf("unknown template %s", parsed.TemplateID)
	}
	return parsed.TemplateID, nil
}

// parsePresetName validates a preset resource name of the form
// templates/{template}/presets/{preset} with a known template and non-empty
// preset id (AIP-122, https://google.aip.dev/122; the preset resource has no
// proto resource annotation, so the pattern is parsed here).
func parsePresetName(name string) (string, error) {
	segments := strings.Split(name, "/")
	if len(segments) != 4 || segments[0] != "templates" || segments[2] != "presets" {
		return "", errors.New("preset resource name must be of the form templates/{template}/presets/{preset}")
	}
	if segments[3] == "" {
		return "", errors.New("preset resource name must have a non-empty preset id")
	}
	if _, err := parseTemplateParent("templates/" + segments[1]); err != nil {
		return "", err
	}
	return name, nil
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
