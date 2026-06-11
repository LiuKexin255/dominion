// Package testplan contains prompt/profile-only integration tests that
// exercise the prompt service without deploying agent, proxy, or session.
// This file is compiled as its own test binary for the prompt suite.
package testplan

import (
	"fmt"
	"testing"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
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
		AgentProfileName: profileName,
		Model:            "gpt-4",
		SystemPrompt:     "You are a test agent.",
		SkillNames:       []string{"navigation"},
		McpNames:         []string{"screenshot-tool"},
		Enabled:          true,
	}

	created := createAgentProfile(t, sutHostURL, sutEnvName, createReq)

	// then: verify created profile fields
	if created.GetAgentProfileName() != profileName {
		t.Errorf("created AgentProfileName = %q, want %q", created.GetAgentProfileName(), profileName)
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
	if fetched.GetAgentProfileName() != profileName {
		t.Errorf("fetched AgentProfileName = %q, want %q", fetched.GetAgentProfileName(), profileName)
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

// ─── Test 2: Prompt Skill Create → Get ───────────────────────────────────────

// TestPromptSkillCreateGet verifies that a skill can be created via POST
// /api/v1/prompts/skills and retrieved via GET with matching fields.
func TestPromptSkillCreateGet(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	skillName := fmt.Sprintf("test-skill-%s", uniqueSuffix())
	skillContent := "Navigate efficiently through the game world."

	// given: create the skill
	createReq := &game.CreateSkillRequest{
		SkillName: skillName,
		Content:   skillContent,
		Enabled:   true,
	}

	created := createSkill(t, sutHostURL, sutEnvName, createReq)

	// then: verify created skill fields
	if created.GetSkillName() != skillName {
		t.Errorf("created SkillName = %q, want %q", created.GetSkillName(), skillName)
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
	if fetched.GetSkillName() != skillName {
		t.Errorf("fetched SkillName = %q, want %q", fetched.GetSkillName(), skillName)
	}
	if fetched.GetContent() != skillContent {
		t.Errorf("fetched Content = %q, want %q", fetched.GetContent(), skillContent)
	}
	if fetched.GetEnabled() != true {
		t.Errorf("fetched Enabled = %v, want true", fetched.GetEnabled())
	}
}
