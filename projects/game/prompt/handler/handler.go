// Package handler implements the PromptServiceServer gRPC interface.
package handler

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	game "dominion/projects/game"
	"dominion/projects/game/pkg/gameconst"
	"dominion/projects/game/prompt/domain"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
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

// CreateAgentProfile creates an AgentProfile under the prompts singleton
// namespace (AIP-133: https://google.aip.dev/133). The resource body is read
// from req.GetAgentProfile(); the caller-supplied ID from req.GetAgentProfileId().
func (h *Handler) CreateAgentProfile(ctx context.Context, req *game.CreateAgentProfileRequest) (*game.AgentProfile, error) {
	if req.GetParent() != gameconst.PromptsParent {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("parent must be %q, got %q", gameconst.PromptsParent, req.GetParent()))
	}

	now := time.Now()
	ap := req.GetAgentProfile()
	profile := &domain.AgentProfile{
		AgentProfileName: req.GetAgentProfileId(),
		Model:            ap.GetModel(),
		SystemPrompt:     ap.GetSystemPrompt(),
		SkillNames:       ap.GetSkillNames(),
		MCPNames:         ap.GetMcpNames(),
		Enabled:          ap.GetEnabled(),
		ToolNames:        ap.GetToolNames(),
		CreateTime:       now,
		UpdateTime:       now,
	}

	if err := h.agentProfileRepo.CreateAgentProfile(ctx, profile); err != nil {
		return nil, toStatusError(err)
	}

	return agentProfileToProto(profile), nil
}

// GetAgentProfile retrieves an AgentProfile by its resource name.
func (h *Handler) GetAgentProfile(ctx context.Context, req *game.GetAgentProfileRequest) (*game.AgentProfile, error) {
	profileID, err := gameconst.AgentProfileID(req.GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	profile, err := h.agentProfileRepo.GetAgentProfile(ctx, profileID)
	if err != nil {
		return nil, toStatusError(err)
	}
	return agentProfileToProto(profile), nil
}

// UpdateAgentProfile applies a partial update described by update_mask to an existing AgentProfile.
// Paths in update_mask must reference writable AgentProfile fields. Unknown paths return
// INVALID_ARGUMENT; missing profiles return NOT_FOUND.
// AIP-134: https://google.aip.dev/134. Identity is carried on the resource itself
// (AgentProfile.name), surfaced via req.GetAgentProfile().GetName().
func (h *Handler) UpdateAgentProfile(ctx context.Context, req *game.UpdateAgentProfileRequest) (*game.AgentProfile, error) {
	profileID, err := gameconst.AgentProfileID(req.GetAgentProfile().GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	existing, err := h.agentProfileRepo.GetAgentProfile(ctx, profileID)
	if err != nil {
		return nil, toStatusError(err)
	}

	updated, err := applyAgentProfileMask(existing, req.GetAgentProfile(), req.GetUpdateMask())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	updated.UpdateTime = time.Now()

	persisted, err := h.agentProfileRepo.UpdateAgentProfile(ctx, updated)
	if err != nil {
		return nil, toStatusError(err)
	}

	return agentProfileToProto(persisted), nil
}

// ListAgentProfiles retrieves a paginated list of AgentProfile resources.
func (h *Handler) ListAgentProfiles(ctx context.Context, req *game.ListAgentProfilesRequest) (*game.ListAgentProfilesResponse, error) {
	pageSize := int(req.GetPageSize())
	if pageSize <= 0 {
		pageSize = 100
	}

	profiles, nextPageToken, err := h.agentProfileRepo.ListAgentProfiles(ctx, pageSize, req.GetPageToken())
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

// DeleteAgentProfile deletes an AgentProfile by its resource name.
func (h *Handler) DeleteAgentProfile(ctx context.Context, req *game.DeleteAgentProfileRequest) (*emptypb.Empty, error) {
	profileID, err := gameconst.AgentProfileID(req.GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := h.agentProfileRepo.DeleteAgentProfile(ctx, profileID); err != nil {
		return nil, toStatusError(err)
	}
	return new(emptypb.Empty), nil
}

// ─── Skill RPCs ───────────────────────────────────────────────────────────

// CreateSkill creates a Skill under the prompts singleton namespace
// (AIP-133: https://google.aip.dev/133). The resource body is read from
// req.GetSkill(); the caller-supplied ID from req.GetSkillId().
func (h *Handler) CreateSkill(ctx context.Context, req *game.CreateSkillRequest) (*game.Skill, error) {
	if req.GetParent() != gameconst.PromptsParent {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("parent must be %q, got %q", gameconst.PromptsParent, req.GetParent()))
	}

	now := time.Now()
	s := req.GetSkill()
	skill := &domain.Skill{
		SkillName:  req.GetSkillId(),
		Content:    s.GetContent(),
		Enabled:    s.GetEnabled(),
		CreateTime: now,
		UpdateTime: now,
	}

	if err := h.skillRepo.CreateSkill(ctx, skill); err != nil {
		return nil, toStatusError(err)
	}

	return skillToProto(skill), nil
}

// GetSkill retrieves a Skill by its resource name.
func (h *Handler) GetSkill(ctx context.Context, req *game.GetSkillRequest) (*game.Skill, error) {
	skillID, err := gameconst.SkillID(req.GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	skill, err := h.skillRepo.GetSkill(ctx, skillID)
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

	skills, nextPageToken, err := h.skillRepo.ListSkills(ctx, pageSize, req.GetPageToken())
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

// DeleteSkill deletes a Skill by its resource name.
func (h *Handler) DeleteSkill(ctx context.Context, req *game.DeleteSkillRequest) (*emptypb.Empty, error) {
	skillID, err := gameconst.SkillID(req.GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := h.skillRepo.DeleteSkill(ctx, skillID); err != nil {
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
		Name:         gameconst.AgentProfileName(p.AgentProfileName),
		Model:        p.Model,
		SystemPrompt: p.SystemPrompt,
		SkillNames:   p.SkillNames,
		McpNames:     p.MCPNames,
		Enabled:      p.Enabled,
		ToolNames:    p.ToolNames,
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
		Name:    gameconst.SkillName(s.SkillName),
		Content: s.Content,
		Enabled: s.Enabled,
	}
	if !s.CreateTime.IsZero() {
		pb.CreateTime = timestamppb.New(s.CreateTime)
	}
	if !s.UpdateTime.IsZero() {
		pb.UpdateTime = timestamppb.New(s.UpdateTime)
	}

	return pb
}

// agentProfileMaskFields enumerates the writable AgentProfile fields addressable via update_mask.
// Order is irrelevant; the slice is searched with slices.Contains.
var agentProfileMaskFields = []string{
	"model", "system_prompt", "skill_names", "mcp_names", "enabled", "tool_names",
}

// applyAgentProfileMask returns a copy of existing with the masked fields overwritten by patch.
// An error is returned if update_mask references a path outside agentProfileMaskFields.
// A nil update_mask (or one with no paths) leaves existing unchanged.
func applyAgentProfileMask(existing *domain.AgentProfile, patch *game.AgentProfile, mask *fieldmaskpb.FieldMask) (*domain.AgentProfile, error) {
	if mask == nil || len(mask.GetPaths()) == 0 {
		return existing, nil
	}

	for _, path := range mask.GetPaths() {
		if !slices.Contains(agentProfileMaskFields, path) {
			return nil, fmt.Errorf("update_mask path %q is not a writable AgentProfile field", path)
		}
	}

	updated := *existing
	if patch == nil {
		return &updated, nil
	}

	for _, path := range mask.GetPaths() {
		switch path {
		case "model":
			updated.Model = patch.GetModel()
		case "system_prompt":
			updated.SystemPrompt = patch.GetSystemPrompt()
		case "skill_names":
			updated.SkillNames = patch.GetSkillNames()
		case "mcp_names":
			updated.MCPNames = patch.GetMcpNames()
		case "enabled":
			updated.Enabled = patch.GetEnabled()
		case "tool_names":
			updated.ToolNames = patch.GetToolNames()
		}
	}

	return &updated, nil
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
