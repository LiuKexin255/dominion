// Package promptclient provides an adapter that wraps a gRPC PromptServiceClient
// to satisfy the domain.PromptServiceClient interface.
package promptclient

import (
	"context"

	"dominion/projects/game"
	"dominion/projects/game/agent/domain"
)

// Adapter adapts a gRPC PromptServiceClient to the domain.PromptServiceClient
// interface. It wraps the generated gRPC client and translates between proto
// types and domain types.
type Adapter struct {
	// Client is the underlying gRPC PromptServiceClient.
	Client game.PromptServiceClient
}

// GetProfile retrieves an agent profile by name via gRPC.
func (a *Adapter) GetProfile(ctx context.Context, profileName string) (*domain.ProfileInfo, error) {
	resp, err := a.Client.GetAgentProfile(ctx, &game.GetAgentProfileRequest{
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
func (a *Adapter) GetSkill(ctx context.Context, skillName string) (*domain.SkillInfo, error) {
	resp, err := a.Client.GetSkill(ctx, &game.GetSkillRequest{
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
