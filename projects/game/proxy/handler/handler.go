// Package handler implements the ProxyService gRPC server interface.
package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	"dominion/common/gopkg/solver"
	game "dominion/projects/game"
	"dominion/projects/game/proxy/domain"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	// parentPattern matches session resource names of the form "sessions/{id}".
	parentPattern = regexp.MustCompile(`^sessions/([^/]+)$`)
	// agentPattern matches agent resource names of the form "sessions/{id}/agent".
	agentPattern = regexp.MustCompile(`^sessions/([^/]+)/agent$`)
)

// agentTarget is the dominion target for resolving agent service instances.
var agentTarget = solver.MustParseTarget("game/agent:grpc")

// AgentClient performs operations against an agent service instance.
type AgentClient interface {
	// InitAgent initializes the agent for a given session.
	InitAgent(ctx context.Context, req *game.InitAgentRequest) (*game.AgentStatus, error)
	// GetAgentStatus returns the current status of the agent in a session.
	GetAgentStatus(ctx context.Context, req *game.GetAgentStatusRequest) (*game.AgentStatus, error)
	// Connect establishes a bidirectional stream to the agent service.
	Connect(ctx context.Context, opts ...grpc.CallOption) (game.AgentService_ConnectClient, error)
	// Close releases resources associated with the client.
	Close() error
}

// AgentClientFactory creates an AgentClient for a specific instance index.
type AgentClientFactory func(ctx context.Context, instanceIndex int) (AgentClient, error)

// ProxyHandler implements game.ProxyServiceServer.
type ProxyHandler struct {
	game.UnimplementedProxyServiceServer

	ownerStore         domain.OwnerStore
	ownerPicker        domain.OwnerPicker
	statefulResolver   solver.StatefulResolver
	agentClientFactory AgentClientFactory
}

// NewProxyHandler creates a new ProxyHandler.
func NewProxyHandler(
	ownerStore domain.OwnerStore,
	ownerPicker domain.OwnerPicker,
	statefulResolver solver.StatefulResolver,
	agentClientFactory AgentClientFactory,
) *ProxyHandler {
	return &ProxyHandler{
		ownerStore:         ownerStore,
		ownerPicker:        ownerPicker,
		statefulResolver:   statefulResolver,
		agentClientFactory: agentClientFactory,
	}
}

// CreateAgent creates an Agent resource under the specified parent Session.
func (h *ProxyHandler) CreateAgent(ctx context.Context, req *game.CreateAgentRequest) (*game.Agent, error) {
	sessionID, err := extractSessionID(req.GetParent(), parentPattern)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// Resolve available agent instances.
	instances, err := h.statefulResolver.Resolve(ctx, agentTarget)
	if err != nil {
		logs.Error(ctx, "resolve agent instances failed", event.Err(err))
		return nil, status.Errorf(codes.Internal, "resolve agent instances: %v", err)
	}

	// Pick an owner instance for this session.
	instanceIndex, err := h.ownerPicker.Pick(ctx, sessionID, instances)
	if err != nil {
		return nil, mapDomainError(err)
	}
	agentIndex := instances[instanceIndex].Index
	ownerName := fmt.Sprintf("agent-%d", agentIndex)

	// Initialize the agent on the selected instance.
	agentClient, err := h.agentClientFactory(ctx, agentIndex)
	if err != nil {
		logs.Error(ctx, "create agent client failed", event.Int("agent_index", agentIndex), event.Err(err))
		return nil, status.Errorf(codes.Internal, "create agent client: %v", err)
	}
	defer agentClient.Close()

	if _, err := agentClient.InitAgent(ctx, &game.InitAgentRequest{SessionId: sessionID}); err != nil {
		logs.Error(ctx, "init agent failed", event.String("session_id", sessionID), event.Int("agent_index", agentIndex), event.Err(err))
		return nil, status.Errorf(codes.Internal, "init agent: %v", err)
	}

	// Persist the owner record.
	now := time.Now()
	owner := &domain.AgentOwner{
		SessionID:  sessionID,
		OwnerIndex: agentIndex,
		Owner:      ownerName,
		CreateTime: now,
	}
	if err := h.ownerStore.Create(ctx, owner); err != nil {
		logs.Error(ctx, "create owner record failed", event.String("session_id", sessionID), event.Err(err))
		return nil, mapDomainError(err)
	}

	logs.Info(ctx, "agent created",
		event.String("session_id", sessionID),
		event.String("owner", ownerName),
		event.Int("agent_index", agentIndex),
	)

	return &game.Agent{
		Name:       fmt.Sprintf("sessions/%s/agent", sessionID),
		SessionId:  sessionID,
		OwnerIndex: int32(agentIndex),
		Owner:      ownerName,
		CreateTime: timestamppb.New(now),
	}, nil
}

// GetAgent returns the Agent resource identified by name.
func (h *ProxyHandler) GetAgent(ctx context.Context, req *game.GetAgentRequest) (*game.Agent, error) {
	sessionID, err := extractSessionID(req.GetName(), agentPattern)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	owner, err := h.ownerStore.Get(ctx, sessionID)
	if err != nil {
		return nil, mapDomainError(err)
	}

	// Verify the agent is alive by querying its status.
	agentClient, err := h.agentClientFactory(ctx, owner.OwnerIndex)
	if err != nil {
		logs.Error(ctx, "create agent client failed", event.Int("agent_index", owner.OwnerIndex), event.Err(err))
		return nil, status.Errorf(codes.Internal, "create agent client: %v", err)
	}
	defer agentClient.Close()

	if _, err := agentClient.GetAgentStatus(ctx, &game.GetAgentStatusRequest{SessionId: sessionID}); err != nil {
		logs.Error(ctx, "get agent status failed", event.String("session_id", sessionID), event.Err(err))
		return nil, status.Errorf(codes.Internal, "get agent status: %v", err)
	}

	return &game.Agent{
		Name:       req.GetName(),
		SessionId:  sessionID,
		OwnerIndex: int32(owner.OwnerIndex),
		Owner:      owner.Owner,
		CreateTime: timestamppb.New(owner.CreateTime),
	}, nil
}

// DeleteAgent deletes the Agent resource identified by name.
func (h *ProxyHandler) DeleteAgent(ctx context.Context, req *game.DeleteAgentRequest) (*emptypb.Empty, error) {
	sessionID, err := extractSessionID(req.GetName(), agentPattern)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	if err := h.ownerStore.Delete(ctx, sessionID); err != nil {
		return nil, mapDomainError(err)
	}

	logs.Info(ctx, "agent deleted", event.String("session_id", sessionID))
	return new(emptypb.Empty), nil
}

// ConnectAgent establishes a bidirectional streaming channel for agent communication.
func (h *ProxyHandler) ConnectAgent(stream game.ProxyService_ConnectAgentServer) error {
	ctx := stream.Context()

	// Receive the initial frame to identify the session.
	frame, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "failed to receive initial frame: %v", err)
	}

	sessionID := frame.GetSessionId()
	if sessionID == "" {
		return status.Error(codes.InvalidArgument, "session_id is required in the first frame")
	}

	// Look up the owner for this session.
	owner, err := h.ownerStore.Get(ctx, sessionID)
	if err != nil {
		return mapDomainError(err)
	}

	// Create a client to the owner agent instance.
	agentClient, err := h.agentClientFactory(ctx, owner.OwnerIndex)
	if err != nil {
		logs.Error(ctx, "connect agent: create client failed",
			event.String("session_id", sessionID),
			event.Int("agent_index", owner.OwnerIndex),
			event.Err(err),
		)
		return status.Errorf(codes.Internal, "create agent client: %v", err)
	}
	defer agentClient.Close()

	// Establish a bidirectional stream to the agent.
	agentStream, err := agentClient.Connect(ctx)
	if err != nil {
		logs.Error(ctx, "connect agent: open stream failed",
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return status.Errorf(codes.Internal, "connect to agent: %v", err)
	}

	// Forward the initial frame to the agent.
	if err := agentStream.Send(frame); err != nil {
		logs.Error(ctx, "connect agent: send initial frame failed",
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return status.Errorf(codes.Internal, "send initial frame to agent: %v", err)
	}

	logs.Info(ctx, "agent stream connected",
		event.String("session_id", sessionID),
		event.Int("agent_index", owner.OwnerIndex),
	)

	// Bidirectional forwarding between gateway and agent.
	g := new(errgroup.Group)

	// Upstream: gateway → agent.
	g.Go(func() error {
		for {
			frame, err := stream.Recv()
			if err != nil {
				return err
			}
			if err := agentStream.Send(frame); err != nil {
				return err
			}
		}
	})

	// Downstream: agent → gateway.
	g.Go(func() error {
		for {
			frame, err := agentStream.Recv()
			if err != nil {
				return err
			}
			if err := stream.Send(frame); err != nil {
				return err
			}
		}
	})

	// Wait for either goroutine to complete or err.
	// Context cancellation from errgroup will tear down the other goroutine.
	if err := g.Wait(); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
			logs.Info(ctx, "agent stream closed",
				event.String("session_id", sessionID),
				event.Err(err),
			)
			return nil
		}
		logs.Error(ctx, "agent stream error",
			event.String("session_id", sessionID),
			event.Err(err),
		)
		return status.Errorf(codes.Internal, "stream forwarding error: %v", err)
	}

	return nil
}

// extractSessionID extracts a session ID from a resource name using the given pattern.
func extractSessionID(name string, pattern *regexp.Regexp) (string, error) {
	matches := pattern.FindStringSubmatch(name)
	if len(matches) != 2 {
		return "", fmt.Errorf("invalid resource name %q: expected %s", name, pattern)
	}
	return matches[1], nil
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
