package main

import (
	"context"
	"flag"
	"log"
	"net"

	"dominion/common/gopkg/bootstrap"
	pgrpc "dominion/common/gopkg/grpc"
	"dominion/common/gopkg/mongo"
	"dominion/common/gopkg/otel"

	game "dominion/projects/game"
	"dominion/projects/game/prompt/domain"
	"dominion/projects/game/prompt/handler"
	mongort "dominion/projects/game/prompt/runtime/mongo"

	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var port = flag.String("port", "50051", "Port to listen on")

// agentProfileAdapter adapts *mongort.Repository to domain.AgentProfileRepository.
type agentProfileAdapter struct {
	*mongort.Repository
}

func (a *agentProfileAdapter) Create(ctx context.Context, profile *domain.AgentProfile) error {
	return a.Repository.CreateAgentProfile(ctx, profile)
}

func (a *agentProfileAdapter) Get(ctx context.Context, profileName string) (*domain.AgentProfile, error) {
	return a.Repository.GetAgentProfile(ctx, profileName)
}

func (a *agentProfileAdapter) List(ctx context.Context, pageSize int, pageToken string) ([]*domain.AgentProfile, string, error) {
	return a.Repository.ListAgentProfiles(ctx, pageSize, pageToken)
}

func (a *agentProfileAdapter) Delete(ctx context.Context, profileName string) error {
	return a.Repository.DeleteAgentProfile(ctx, profileName)
}

// skillAdapter adapts *mongort.Repository to domain.SkillRepository.
type skillAdapter struct {
	*mongort.Repository
}

func (s *skillAdapter) Create(ctx context.Context, skill *domain.Skill) error {
	return s.Repository.CreateSkill(ctx, skill)
}

func (s *skillAdapter) Get(ctx context.Context, skillName string) (*domain.Skill, error) {
	return s.Repository.GetSkill(ctx, skillName)
}

func (s *skillAdapter) List(ctx context.Context, pageSize int, pageToken string) ([]*domain.Skill, string, error) {
	return s.Repository.ListSkills(ctx, pageSize, pageToken)
}

func (s *skillAdapter) Delete(ctx context.Context, skillName string) error {
	return s.Repository.DeleteSkill(ctx, skillName)
}

func main() {
	flag.Parse()

	listener, err := net.Listen("tcp", ":"+*port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	mongoClient, err := mongo.NewClient("game/mongo")
	if err != nil {
		log.Fatalf("failed to create mongo client: %v", err)
	}

	repo := mongort.NewRepository(mongoClient, "game_prompt")

	h := handler.NewHandler(&agentProfileAdapter{repo}, &skillAdapter{repo})

	grpcServer := grpcgo.NewServer(pgrpc.ServiceDefault()...)
	game.RegisterPromptServiceServer(grpcServer, h)
	reflection.Register(grpcServer)

	b := bootstrap.New()
	b.Register(otel.Component())
	b.Register(bootstrap.MongoClient("mongo", mongoClient))
	b.Register(bootstrap.GRPCServer("grpc", grpcServer, listener))
	log.Fatal(b.Run(context.Background()))
}
