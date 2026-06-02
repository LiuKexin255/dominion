package main

import (
	"context"
	"flag"
	"log"
	"net"

	"dominion/common/gopkg/bootstrap"
	pgrpc "dominion/common/gopkg/grpc"
	"dominion/common/gopkg/grpc/solver"
	"dominion/common/gopkg/otel"
	game "dominion/projects/game"
	"dominion/projects/game/agent/domain"
	"dominion/projects/game/agent/handler"
	"dominion/projects/game/agent/runtime"
	gameconst "dominion/projects/game/pkg/gameconst"

	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var port = flag.String("port", "50051", "Port to listen on")

// promptClientAdapter adapts a gRPC PromptServiceClient to the domain
// PromptServiceClient interface.
type promptClientAdapter struct {
	client game.PromptServiceClient
}

// GetProfile retrieves an agent profile by name via gRPC.
func (a *promptClientAdapter) GetProfile(ctx context.Context, profileName string) (*domain.ProfileInfo, error) {
	resp, err := a.client.GetAgentProfile(ctx, &game.GetAgentProfileRequest{
		AgentProfileName: profileName,
	})
	if err != nil {
		return nil, err
	}
	return &domain.ProfileInfo{
		AgentProfileName: resp.GetAgentProfileName(),
		Model:            resp.GetModel(),
		SystemPrompt:     resp.GetSystemPrompt(),
		SkillNames:       resp.GetSkillNames(),
		MCPNames:         resp.GetMcpNames(),
		Enabled:          resp.GetEnabled(),
	}, nil
}

// GetSkill retrieves a skill by name via gRPC.
func (a *promptClientAdapter) GetSkill(ctx context.Context, skillName string) (*domain.SkillInfo, error) {
	resp, err := a.client.GetSkill(ctx, &game.GetSkillRequest{
		SkillName: skillName,
	})
	if err != nil {
		return nil, err
	}
	return &domain.SkillInfo{
		SkillName: resp.GetSkillName(),
		Content:   resp.GetContent(),
		Enabled:   resp.GetEnabled(),
	}, nil
}

func main() {
	flag.Parse()

	listener, err := net.Listen("tcp", ":"+*port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	promptConn, err := grpcgo.NewClient(solver.URI(gameconst.PromptTarget), pgrpc.ClientDefault()...)
	if err != nil {
		log.Fatalf("prompt dial: %v", err)
	}

	promptClient := &promptClientAdapter{client: game.NewPromptServiceClient(promptConn)}
	rt := runtime.NewInvokeRuntime(promptClient)
	h := handler.NewAgentHandler(rt)

	grpcServer := grpcgo.NewServer(pgrpc.ServiceDefault()...)
	game.RegisterAgentServiceServer(grpcServer, h)
	reflection.Register(grpcServer)

	log.Printf("agent gRPC server listening on :%s", *port)

	b := bootstrap.New()
	b.Register(otel.Component())
	b.Register(bootstrap.GRPCServer("grpc", grpcServer, listener))
	log.Fatal(b.Run(context.Background()))
}
