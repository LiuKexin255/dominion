// Package testplan contains prompt/profile-only integration tests that
// exercise the prompt service without deploying agent, proxy, or session.
// This file is compiled as its own test binary for the prompt suite.
package testplan

import (
	"fmt"
	"net/http"
	"testing"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
	"dominion/projects/game/pkg/gameconst"
)

// ─── Test 1: Prompt Profile Create → Get ─────────────────────────────────────

// TestPromptProfileCreateGet verifies that an agent profile can be created
// via POST /api/v1/prompts/agentProfiles and retrieved via GET with matching
// fields.
func TestPromptProfileCreateGet(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	profileName := fmt.Sprintf("test-profile-%s", uniqueSuffix())

	// given: create the profile
	createReq := &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: profileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "You are a test agent.",
			SkillNames:   []string{"navigation"},
			McpNames:     []string{"screenshot-tool"},
			Enabled:      true,
		},
	}

	created := createAgentProfile(t, sutHostURL, sutEnvName, createReq)

	// then: verify created profile fields
	if created.GetName() != "prompts/agentProfiles/"+profileName {
		t.Errorf("created Name = %q, want %q", created.GetName(), "prompts/agentProfiles/"+profileName)
	}
	if created.GetModel() != "gpt-4" {
		t.Errorf("created Model = %q, want %q", created.GetModel(), "gpt-4")
	}
	if created.GetName() == "" {
		t.Error("created Name is empty, want non-empty")
	}

	// when: get the profile
	fetched := getAgentProfile(t, sutHostURL, sutEnvName, profileName)

	// then: verify fetched fields match
	if fetched.GetName() != "prompts/agentProfiles/"+profileName {
		t.Errorf("fetched Name = %q, want %q", fetched.GetName(), "prompts/agentProfiles/"+profileName)
	}
	if fetched.GetModel() != "gpt-4" {
		t.Errorf("fetched Model = %q, want %q", fetched.GetModel(), "gpt-4")
	}
	if fetched.GetSystemPrompt() != "You are a test agent." {
		t.Errorf("fetched SystemPrompt = %q, want %q", fetched.GetSystemPrompt(), "You are a test agent.")
	}
	if fetched.GetEnabled() != true {
		t.Errorf("fetched Enabled = %v, want true", fetched.GetEnabled())
	}
}

// ─── Test 3: UpdateAgentProfile tool_names via FieldMask ─────────────────────

// TestPromptUpdateAgentProfileToolNames verifies that UpdateAgentProfile
// (HTTP PATCH with FieldMask) adds tool_names to an existing profile and
// the change is observable via a subsequent GET. Also verifies the initial
// tool_names supplied at create time is preserved.
func TestPromptUpdateAgentProfileToolNames(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	toolProfileName := fmt.Sprintf("up-tool-%s", uniqueSuffix())
	chatProfileName := fmt.Sprintf("up-chat-%s", uniqueSuffix())

	// given: tool-profile created with tool_names=["mouse"]
	toolProfile := createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: toolProfileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "Tool-enabled profile.",
			ToolNames:    []string{"mouse"},
			Enabled:      true,
		},
	})
	if got := toolProfile.GetToolNames(); len(got) != 1 || got[0] != "mouse" {
		t.Fatalf("tool-profile tool_names = %v, want [mouse]", got)
	}

	// given: chat-profile created with no tool_names
	chatProfile := createAgentProfile(t, sutHostURL, sutEnvName, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: chatProfileName,
		AgentProfile: &game.AgentProfile{
			Model:        "gpt-4",
			SystemPrompt: "Chat-only profile.",
			Enabled:      true,
		},
	})
	if got := chatProfile.GetToolNames(); len(got) != 0 {
		t.Fatalf("chat-profile tool_names = %v, want empty", got)
	}

	// when: PATCH chat-profile adding tool_names=["mouse"]
	status, body := updateAgentProfileTools(t, sutHostURL, sutEnvName, chatProfileName, []string{"mouse"})
	if status != http.StatusOK {
		t.Fatalf("UpdateAgentProfile status=%d, body=%s", status, string(body))
	}

	// then: GET chat-profile reflects the update
	updated := getAgentProfile(t, sutHostURL, sutEnvName, chatProfileName)
	if got := updated.GetToolNames(); len(got) != 1 || got[0] != "mouse" {
		t.Fatalf("chat-profile tool_names after update = %v, want [mouse]", got)
	}
}

// TestPromptSkillCreateGet verifies that a skill can be created via POST
// /api/v1/prompts/skills and retrieved via GET with matching fields.
func TestPromptSkillCreateGet(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	skillName := fmt.Sprintf("test-skill-%s", uniqueSuffix())
	skillContent := "Navigate efficiently through the game world."

	// given: create the skill
	createReq := &game.CreateSkillRequest{
		Parent:  gameconst.PromptsParent,
		SkillId: skillName,
		Skill: &game.Skill{
			Content: skillContent,
			Enabled: true,
		},
	}

	created := createSkill(t, sutHostURL, sutEnvName, createReq)

	// then: verify created skill fields
	if created.GetName() != "prompts/skills/"+skillName {
		t.Errorf("created Name = %q, want %q", created.GetName(), "prompts/skills/"+skillName)
	}
	if created.GetContent() != skillContent {
		t.Errorf("created Content = %q, want %q", created.GetContent(), skillContent)
	}
	if created.GetName() == "" {
		t.Error("created Name is empty, want non-empty")
	}

	// when: get the skill
	fetched := getSkill(t, sutHostURL, sutEnvName, skillName)

	// then: verify fetched fields match
	if fetched.GetName() != "prompts/skills/"+skillName {
		t.Errorf("fetched Name = %q, want %q", fetched.GetName(), "prompts/skills/"+skillName)
	}
	if fetched.GetContent() != skillContent {
		t.Errorf("fetched Content = %q, want %q", fetched.GetContent(), skillContent)
	}
	if fetched.GetEnabled() != true {
		t.Errorf("fetched Enabled = %v, want true", fetched.GetEnabled())
	}
}
