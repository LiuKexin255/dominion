// Package handler implements the PromptServiceServer gRPC interface.
package handler

import (
	"context"
	"errors"
	"fmt"
	"time"

	game "dominion/projects/game"
	"dominion/projects/game/prompt/domain"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Handler implements PromptServiceServer for AgentProfile and Skill CRUD operations.
type Handler struct {
	game.UnimplementedPromptServiceServer

	agentProfileRepo domain.AgentProfileRepository
	skillRepo        domain.SkillRepository
}

// NewHandler creates a new Handler with the given repositories.
func NewHandler(agentProfileRepo domain.AgentProfileRepository, skillRepo domain.SkillRepository) *Handler {
	return &Handler{
		agentProfileRepo: agentProfileRepo,
		skillRepo:        skillRepo,
	}
}

// ─── AgentProfile RPCs ────────────────────────────────────────────────────

// CreateAgentProfile creates a new AgentProfile resource.
func (h *Handler) CreateAgentProfile(ctx context.Context, req *game.CreateAgentProfileRequest) (*game.AgentProfile, error) {
	now := time.Now()
	profileName := req.GetAgentProfileName()
	profile := &domain.AgentProfile{
		Name:             "agentProfiles/" + profileName,
		AgentProfileName: profileName,
		Model:            req.GetModel(),
		SystemPrompt:     req.GetSystemPrompt(),
		SkillNames:       req.GetSkillNames(),
		MCPNames:         req.GetMcpNames(),
		Enabled:          req.GetEnabled(),
		CreateTime:       now,
		UpdateTime:       now,
	}

	if err := h.agentProfileRepo.Create(ctx, profile); err != nil {
		return nil, toStatusError(err)
	}

	return agentProfileToProto(profile), nil
}

// GetAgentProfile retrieves an AgentProfile by its profile name.
func (h *Handler) GetAgentProfile(ctx context.Context, req *game.GetAgentProfileRequest) (*game.AgentProfile, error) {
	profile, err := h.agentProfileRepo.Get(ctx, req.GetAgentProfileName())
	if err != nil {
		return nil, toStatusError(err)
	}
	return agentProfileToProto(profile), nil
}

// ListAgentProfiles retrieves a paginated list of AgentProfile resources.
func (h *Handler) ListAgentProfiles(ctx context.Context, req *game.ListAgentProfilesRequest) (*game.ListAgentProfilesResponse, error) {
	pageSize := int(req.GetPageSize())
	if pageSize <= 0 {
		pageSize = 100
	}

	profiles, nextPageToken, err := h.agentProfileRepo.List(ctx, pageSize, req.GetPageToken())
	if err != nil {
		return nil, toStatusError(err)
	}

	protos := make([]*game.AgentProfile, 0, len(profiles))
	for _, p := range profiles {
		protos = append(protos, agentProfileToProto(p))
	}

	return &game.ListAgentProfilesResponse{
		AgentProfiles: protos,
		NextPageToken: nextPageToken,
	}, nil
}

// DeleteAgentProfile deletes an AgentProfile by its profile name.
func (h *Handler) DeleteAgentProfile(ctx context.Context, req *game.DeleteAgentProfileRequest) (*emptypb.Empty, error) {
	if err := h.agentProfileRepo.Delete(ctx, req.GetAgentProfileName()); err != nil {
		return nil, toStatusError(err)
	}
	return new(emptypb.Empty), nil
}

// ─── Skill RPCs ───────────────────────────────────────────────────────────

// CreateSkill creates a new Skill resource.
func (h *Handler) CreateSkill(ctx context.Context, req *game.CreateSkillRequest) (*game.Skill, error) {
	now := time.Now()
	skillName := req.GetSkillName()
	skill := &domain.Skill{
		Name:       "skills/" + skillName,
		SkillName:  skillName,
		Content:    req.GetContent(),
		Enabled:    req.GetEnabled(),
		CreateTime: now,
		UpdateTime: now,
	}

	if err := h.skillRepo.Create(ctx, skill); err != nil {
		return nil, toStatusError(err)
	}

	return skillToProto(skill), nil
}

// GetSkill retrieves a Skill by its skill name.
func (h *Handler) GetSkill(ctx context.Context, req *game.GetSkillRequest) (*game.Skill, error) {
	skill, err := h.skillRepo.Get(ctx, req.GetSkillName())
	if err != nil {
		return nil, toStatusError(err)
	}
	return skillToProto(skill), nil
}

// ListSkills retrieves a paginated list of Skill resources.
func (h *Handler) ListSkills(ctx context.Context, req *game.ListSkillsRequest) (*game.ListSkillsResponse, error) {
	pageSize := int(req.GetPageSize())
	if pageSize <= 0 {
		pageSize = 100
	}

	skills, nextPageToken, err := h.skillRepo.List(ctx, pageSize, req.GetPageToken())
	if err != nil {
		return nil, toStatusError(err)
	}

	protos := make([]*game.Skill, 0, len(skills))
	for _, s := range skills {
		protos = append(protos, skillToProto(s))
	}

	return &game.ListSkillsResponse{
		Skills:        protos,
		NextPageToken: nextPageToken,
	}, nil
}

// DeleteSkill deletes a Skill by its skill name.
func (h *Handler) DeleteSkill(ctx context.Context, req *game.DeleteSkillRequest) (*emptypb.Empty, error) {
	if err := h.skillRepo.Delete(ctx, req.GetSkillName()); err != nil {
		return nil, toStatusError(err)
	}
	return new(emptypb.Empty), nil
}

// ─── Conversion helpers ───────────────────────────────────────────────────

// agentProfileToProto converts a domain AgentProfile to a proto AgentProfile.
func agentProfileToProto(p *domain.AgentProfile) *game.AgentProfile {
	if p == nil {
		return nil
	}

	pb := &game.AgentProfile{
		Name:             p.Name,
		AgentProfileName: p.AgentProfileName,
		Model:            p.Model,
		SystemPrompt:     p.SystemPrompt,
		SkillNames:       p.SkillNames,
		McpNames:         p.MCPNames,
		Enabled:          p.Enabled,
	}
	if !p.CreateTime.IsZero() {
		pb.CreateTime = timestamppb.New(p.CreateTime)
	}
	if !p.UpdateTime.IsZero() {
		pb.UpdateTime = timestamppb.New(p.UpdateTime)
	}

	return pb
}

// skillToProto converts a domain Skill to a proto Skill.
func skillToProto(s *domain.Skill) *game.Skill {
	if s == nil {
		return nil
	}

	pb := &game.Skill{
		Name:      s.Name,
		SkillName: s.SkillName,
		Content:   s.Content,
		Enabled:   s.Enabled,
	}
	if !s.CreateTime.IsZero() {
		pb.CreateTime = timestamppb.New(s.CreateTime)
	}
	if !s.UpdateTime.IsZero() {
		pb.UpdateTime = timestamppb.New(s.UpdateTime)
	}

	return pb
}

// toStatusError maps domain errors to gRPC status errors.
func toStatusError(err error) error {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, domain.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, err.Error())
	default:
		return status.Error(codes.Internal, fmt.Sprintf("prompt handler: %v", err))
	}
}
