package handler

import (
	"context"
	"sync"
	"testing"
	"time"

	game "dominion/projects/game"
	"dominion/projects/game/pkg/gameconst"
	"dominion/projects/game/prompt/domain"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// inMemoryAgentProfileRepo implements domain.AgentProfileRepository for testing.
type inMemoryAgentProfileRepo struct {
	mu       sync.Mutex
	profiles map[string]*domain.AgentProfile
}

func newInMemoryAgentProfileRepo() *inMemoryAgentProfileRepo {
	return &inMemoryAgentProfileRepo{profiles: make(map[string]*domain.AgentProfile)}
}

func (r *inMemoryAgentProfileRepo) CreateAgentProfile(_ context.Context, profile *domain.AgentProfile) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.profiles[profile.AgentProfileName]; exists {
		return domain.ErrAlreadyExists
	}
	r.profiles[profile.AgentProfileName] = profile
	return nil
}

func (r *inMemoryAgentProfileRepo) GetAgentProfile(_ context.Context, profileName string) (*domain.AgentProfile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.profiles[profileName]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return p, nil
}

func (r *inMemoryAgentProfileRepo) UpdateAgentProfile(_ context.Context, profile *domain.AgentProfile) (*domain.AgentProfile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.profiles[profile.AgentProfileName]
	if !ok {
		return nil, domain.ErrNotFound
	}
	clone := *profile
	clone.CreateTime = existing.CreateTime
	r.profiles[profile.AgentProfileName] = &clone
	return &clone, nil
}

func (r *inMemoryAgentProfileRepo) ListAgentProfiles(_ context.Context, pageSize int, pageToken string) ([]*domain.AgentProfile, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]*domain.AgentProfile, 0, len(r.profiles))
	for _, p := range r.profiles {
		result = append(result, p)
	}
	return result, "", nil
}

func (r *inMemoryAgentProfileRepo) DeleteAgentProfile(_ context.Context, profileName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.profiles[profileName]; !ok {
		return domain.ErrNotFound
	}
	delete(r.profiles, profileName)
	return nil
}

// inMemorySkillRepo implements domain.SkillRepository for testing.
type inMemorySkillRepo struct {
	mu     sync.Mutex
	skills map[string]*domain.Skill
}

func newInMemorySkillRepo() *inMemorySkillRepo {
	return &inMemorySkillRepo{skills: make(map[string]*domain.Skill)}
}

func (r *inMemorySkillRepo) CreateSkill(_ context.Context, skill *domain.Skill) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.skills[skill.SkillName]; exists {
		return domain.ErrAlreadyExists
	}
	r.skills[skill.SkillName] = skill
	return nil
}

func (r *inMemorySkillRepo) GetSkill(_ context.Context, skillName string) (*domain.Skill, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.skills[skillName]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return s, nil
}

func (r *inMemorySkillRepo) ListSkills(_ context.Context, pageSize int, pageToken string) ([]*domain.Skill, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]*domain.Skill, 0, len(r.skills))
	for _, s := range r.skills {
		result = append(result, s)
	}
	return result, "", nil
}

func (r *inMemorySkillRepo) DeleteSkill(_ context.Context, skillName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.skills[skillName]; !ok {
		return domain.ErrNotFound
	}
	delete(r.skills, skillName)
	return nil
}

func TestPromptService_CreateGetAgentProfile(t *testing.T) {
	ctx := context.Background()

	// given
	profileRepo := newInMemoryAgentProfileRepo()
	skillRepo := newInMemorySkillRepo()
	h := NewHandler(profileRepo, skillRepo)

	createReq := &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: "test-profile",
		AgentProfile: &game.AgentProfile{
			Model:        "opencode-go/deepseek-v4-pro",
			SystemPrompt: "You are a helpful assistant.",
			SkillNames:   []string{"skill-a"},
			McpNames:     []string{"mcp-server-1"},
			Enabled:      true,
		},
	}

	// when — create
	created, err := h.CreateAgentProfile(ctx, createReq)

	// then — create succeeds
	assertStatusCode(t, err, codes.OK)
	if created.GetName() != "prompts/agentProfiles/test-profile" {
		t.Fatalf("CreateAgentProfile() name = %q, want %q", created.GetName(), "prompts/agentProfiles/test-profile")
	}
	if created.GetModel() != "opencode-go/deepseek-v4-pro" {
		t.Fatalf("CreateAgentProfile() model = %q, want %q", created.GetModel(), "opencode-go/deepseek-v4-pro")
	}
	if created.GetSystemPrompt() != "You are a helpful assistant." {
		t.Fatalf("CreateAgentProfile() system_prompt = %q, want %q", created.GetSystemPrompt(), "You are a helpful assistant.")
	}
	if len(created.GetSkillNames()) != 1 || created.GetSkillNames()[0] != "skill-a" {
		t.Fatalf("CreateAgentProfile() skill_names = %v, want [skill-a]", created.GetSkillNames())
	}
	if created.GetEnabled() != true {
		t.Fatalf("CreateAgentProfile() enabled = %v, want true", created.GetEnabled())
	}
	if created.GetCreateTime() == nil {
		t.Fatal("CreateAgentProfile() create_time is nil, want non-nil")
	}

	// when — get
	getReq := &game.GetAgentProfileRequest{Name: "prompts/agentProfiles/test-profile"}
	got, err := h.GetAgentProfile(ctx, getReq)

	// then — get returns same profile
	assertStatusCode(t, err, codes.OK)
	if got.GetName() != created.GetName() {
		t.Fatalf("GetAgentProfile() name = %q, want %q", got.GetName(), created.GetName())
	}
	if got.GetModel() != created.GetModel() {
		t.Fatalf("GetAgentProfile() model = %q, want %q", got.GetModel(), created.GetModel())
	}
}

func TestPromptService_CreateGetSkill(t *testing.T) {
	ctx := context.Background()

	// given
	profileRepo := newInMemoryAgentProfileRepo()
	skillRepo := newInMemorySkillRepo()
	h := NewHandler(profileRepo, skillRepo)

	createReq := &game.CreateSkillRequest{
		Parent:  gameconst.PromptsParent,
		SkillId: "my-skill",
		Skill: &game.Skill{
			Content: "You know how to browse the web.",
			Enabled: true,
		},
	}

	// when — create
	created, err := h.CreateSkill(ctx, createReq)

	// then — create succeeds
	assertStatusCode(t, err, codes.OK)
	if created.GetName() != "prompts/skills/my-skill" {
		t.Fatalf("CreateSkill() name = %q, want %q", created.GetName(), "prompts/skills/my-skill")
	}
	if created.GetContent() != "You know how to browse the web." {
		t.Fatalf("CreateSkill() content = %q, want %q", created.GetContent(), "You know how to browse the web.")
	}
	if created.GetEnabled() != true {
		t.Fatalf("CreateSkill() enabled = %v, want true", created.GetEnabled())
	}

	// when — get
	getReq := &game.GetSkillRequest{Name: "prompts/skills/my-skill"}
	got, err := h.GetSkill(ctx, getReq)

	// then — get returns same skill
	assertStatusCode(t, err, codes.OK)
	if got.GetName() != created.GetName() {
		t.Fatalf("GetSkill() name = %q, want %q", got.GetName(), created.GetName())
	}
	if got.GetContent() != created.GetContent() {
		t.Fatalf("GetSkill() content = %q, want %q", got.GetContent(), created.GetContent())
	}
}

func TestPromptService_ProfileNotFound(t *testing.T) {
	ctx := context.Background()

	// given
	profileRepo := newInMemoryAgentProfileRepo()
	skillRepo := newInMemorySkillRepo()
	h := NewHandler(profileRepo, skillRepo)

	// when — get missing profile
	_, err := h.GetAgentProfile(ctx, &game.GetAgentProfileRequest{Name: "prompts/agentProfiles/nonexistent"})

	// then — returns NotFound
	assertStatusCode(t, err, codes.NotFound)
}

func TestPromptService_CreateGetAgentProfileWithToolNames(t *testing.T) {
	ctx := context.Background()

	// given
	profileRepo := newInMemoryAgentProfileRepo()
	skillRepo := newInMemorySkillRepo()
	h := NewHandler(profileRepo, skillRepo)

	createReq := &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: "tools-profile",
		AgentProfile: &game.AgentProfile{
			Model:        "opencode-go/deepseek-v4-pro",
			SystemPrompt: "You can click things.",
			ToolNames:    []string{"mouse", "keyboard"},
			Enabled:      true,
		},
	}

	// when — create
	created, err := h.CreateAgentProfile(ctx, createReq)

	// then — tool_names echoed on create response
	assertStatusCode(t, err, codes.OK)
	if len(created.GetToolNames()) != 2 {
		t.Fatalf("CreateAgentProfile() tool_names len = %d, want 2", len(created.GetToolNames()))
	}
	if created.GetToolNames()[0] != "mouse" || created.GetToolNames()[1] != "keyboard" {
		t.Fatalf("CreateAgentProfile() tool_names = %v, want [mouse keyboard]", created.GetToolNames())
	}

	// when — get
	got, err := h.GetAgentProfile(ctx, &game.GetAgentProfileRequest{Name: "prompts/agentProfiles/tools-profile"})

	// then — tool_names persisted and returned
	assertStatusCode(t, err, codes.OK)
	if len(got.GetToolNames()) != 2 {
		t.Fatalf("GetAgentProfile() tool_names len = %d, want 2", len(got.GetToolNames()))
	}
	if got.GetToolNames()[0] != "mouse" || got.GetToolNames()[1] != "keyboard" {
		t.Fatalf("GetAgentProfile() tool_names = %v, want [mouse keyboard]", got.GetToolNames())
	}
}

func TestPromptService_UpdateAgentProfileToolNamesViaFieldMask(t *testing.T) {
	ctx := context.Background()

	// given — seed profile with tool_names=["mouse"]
	profileRepo := newInMemoryAgentProfileRepo()
	skillRepo := newInMemorySkillRepo()
	h := NewHandler(profileRepo, skillRepo)

	_, err := h.CreateAgentProfile(ctx, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: "mask-profile",
		AgentProfile: &game.AgentProfile{
			Model:        "opencode-go/deepseek-v4-pro",
			SystemPrompt: "original prompt",
			ToolNames:    []string{"mouse"},
			Enabled:      true,
		},
	})
	assertStatusCode(t, err, codes.OK)

	// when — update tool_names to [] via FieldMask
	updateReq := &game.UpdateAgentProfileRequest{
		AgentProfile: &game.AgentProfile{
			Name:      "prompts/agentProfiles/mask-profile",
			ToolNames: []string{},
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"tool_names"}},
	}
	updated, err := h.UpdateAgentProfile(ctx, updateReq)

	// then — tool_names cleared, other fields preserved
	assertStatusCode(t, err, codes.OK)
	if len(updated.GetToolNames()) != 0 {
		t.Fatalf("UpdateAgentProfile() tool_names = %v, want empty", updated.GetToolNames())
	}
	if updated.GetSystemPrompt() != "original prompt" {
		t.Fatalf("UpdateAgentProfile() system_prompt = %q, want %q (FieldMask should not touch)", updated.GetSystemPrompt(), "original prompt")
	}
	if updated.GetModel() != "opencode-go/deepseek-v4-pro" {
		t.Fatalf("UpdateAgentProfile() model = %q, want %q (FieldMask should not touch)", updated.GetModel(), "opencode-go/deepseek-v4-pro")
	}
	if !updated.GetEnabled() {
		t.Fatalf("UpdateAgentProfile() enabled = false, want true (FieldMask should not touch)")
	}
	if updated.GetUpdateTime() == nil {
		t.Fatal("UpdateAgentProfile() update_time is nil, want non-nil")
	}
	if updated.GetCreateTime() == nil {
		t.Fatal("UpdateAgentProfile() create_time is nil, want non-nil")
	}

	// when — re-fetch
	got, err := h.GetAgentProfile(ctx, &game.GetAgentProfileRequest{Name: "prompts/agentProfiles/mask-profile"})

	// then — persisted
	assertStatusCode(t, err, codes.OK)
	if len(got.GetToolNames()) != 0 {
		t.Fatalf("GetAgentProfile() after update tool_names = %v, want empty", got.GetToolNames())
	}
}

func TestPromptService_UpdateAgentProfileUnknownFieldMaskPath(t *testing.T) {
	ctx := context.Background()

	// given — seed profile
	profileRepo := newInMemoryAgentProfileRepo()
	skillRepo := newInMemorySkillRepo()
	h := NewHandler(profileRepo, skillRepo)

	_, err := h.CreateAgentProfile(ctx, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: "unknown-path-profile",
		AgentProfile: &game.AgentProfile{
			Model: "opencode-go/deepseek-v4-pro",
		},
	})
	assertStatusCode(t, err, codes.OK)

	// when — update with unknown FieldMask path
	updateReq := &game.UpdateAgentProfileRequest{
		AgentProfile: &game.AgentProfile{
			Name: "prompts/agentProfiles/unknown-path-profile",
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"nonexistent_field"}},
	}
	_, err = h.UpdateAgentProfile(ctx, updateReq)

	// then — returns InvalidArgument
	assertStatusCode(t, err, codes.InvalidArgument)
}

func TestPromptService_UpdateAgentProfileNotFound(t *testing.T) {
	ctx := context.Background()

	// given
	profileRepo := newInMemoryAgentProfileRepo()
	skillRepo := newInMemorySkillRepo()
	h := NewHandler(profileRepo, skillRepo)

	// when — update missing profile
	updateReq := &game.UpdateAgentProfileRequest{
		AgentProfile: &game.AgentProfile{
			Name:  "prompts/agentProfiles/ghost",
			Model: "opencode-go/deepseek-v4-pro",
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"model"}},
	}
	_, err := h.UpdateAgentProfile(ctx, updateReq)

	// then — returns NotFound
	assertStatusCode(t, err, codes.NotFound)
}

func TestPromptService_UpdateAgentProfileMultipleFields(t *testing.T) {
	ctx := context.Background()

	// given — seed
	profileRepo := newInMemoryAgentProfileRepo()
	skillRepo := newInMemorySkillRepo()
	h := NewHandler(profileRepo, skillRepo)

	_, err := h.CreateAgentProfile(ctx, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: "multi-mask",
		AgentProfile: &game.AgentProfile{
			Model:        "opencode-go/deepseek-v4-pro",
			SystemPrompt: "before",
			Enabled:      true,
		},
	})
	assertStatusCode(t, err, codes.OK)

	// when — update model + system_prompt + enabled simultaneously
	updateReq := &game.UpdateAgentProfileRequest{
		AgentProfile: &game.AgentProfile{
			Name:         "prompts/agentProfiles/multi-mask",
			Model:        "opencode-go/gpt-5",
			SystemPrompt: "after",
			Enabled:      false,
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"model", "system_prompt", "enabled"}},
	}
	updated, err := h.UpdateAgentProfile(ctx, updateReq)

	// then — all masked fields updated
	assertStatusCode(t, err, codes.OK)
	if updated.GetModel() != "opencode-go/gpt-5" {
		t.Fatalf("UpdateAgentProfile() model = %q, want %q", updated.GetModel(), "opencode-go/gpt-5")
	}
	if updated.GetSystemPrompt() != "after" {
		t.Fatalf("UpdateAgentProfile() system_prompt = %q, want %q", updated.GetSystemPrompt(), "after")
	}
	if updated.GetEnabled() {
		t.Fatalf("UpdateAgentProfile() enabled = true, want false")
	}
}

func Test_applyAgentProfileMask(t *testing.T) {
	existing := &domain.AgentProfile{
		AgentProfileName: "p",
		Model:            "old-model",
		SystemPrompt:     "old",
		SkillNames:       []string{"s1"},
		MCPNames:         []string{"m1"},
		Enabled:          true,
		ToolNames:        []string{"t1"},
	}

	tests := []struct {
		name      string
		patch     *game.AgentProfile
		mask      *fieldmaskpb.FieldMask
		wantModel string
		wantTools []string
		wantErr   bool
	}{
		{
			name:      "nil mask leaves existing unchanged",
			patch:     &game.AgentProfile{Model: "ignored"},
			mask:      nil,
			wantModel: "old-model",
			wantTools: []string{"t1"},
		},
		{
			name:      "empty mask paths leaves existing unchanged",
			patch:     &game.AgentProfile{Model: "ignored"},
			mask:      &fieldmaskpb.FieldMask{Paths: nil},
			wantModel: "old-model",
			wantTools: []string{"t1"},
		},
		{
			name:      "single field model",
			patch:     &game.AgentProfile{Model: "new-model"},
			mask:      &fieldmaskpb.FieldMask{Paths: []string{"model"}},
			wantModel: "new-model",
			wantTools: []string{"t1"},
		},
		{
			name:      "tool_names cleared to empty slice",
			patch:     &game.AgentProfile{ToolNames: []string{}},
			mask:      &fieldmaskpb.FieldMask{Paths: []string{"tool_names"}},
			wantModel: "old-model",
			wantTools: []string{},
		},
		{
			name:      "tool_names replaced",
			patch:     &game.AgentProfile{ToolNames: []string{"a", "b"}},
			mask:      &fieldmaskpb.FieldMask{Paths: []string{"tool_names"}},
			wantModel: "old-model",
			wantTools: []string{"a", "b"},
		},
		{
			name:      "multiple fields masked",
			patch:     &game.AgentProfile{Model: "new", ToolNames: []string{"x"}, Enabled: false},
			mask:      &fieldmaskpb.FieldMask{Paths: []string{"model", "tool_names", "enabled"}},
			wantModel: "new",
			wantTools: []string{"x"},
		},
		{
			name:    "unknown path returns error",
			patch:   &game.AgentProfile{},
			mask:    &fieldmaskpb.FieldMask{Paths: []string{"bogus"}},
			wantErr: true,
		},
		{
			name:    "unknown path mixed with valid still errors",
			patch:   &game.AgentProfile{Model: "new"},
			mask:    &fieldmaskpb.FieldMask{Paths: []string{"model", "bogus"}},
			wantErr: true,
		},
		{
			name:      "nil patch with valid mask returns copy of existing",
			patch:     nil,
			mask:      &fieldmaskpb.FieldMask{Paths: []string{"model"}},
			wantModel: "old-model",
			wantTools: []string{"t1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, err := applyAgentProfileMask(existing, tt.patch, tt.mask)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("applyAgentProfileMask() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("applyAgentProfileMask() unexpected error: %v", err)
			}
			if got.Model != tt.wantModel {
				t.Fatalf("applyAgentProfileMask() model = %q, want %q", got.Model, tt.wantModel)
			}
			if tt.wantTools == nil {
				if got.ToolNames != nil {
					t.Fatalf("applyAgentProfileMask() tool_names = %v, want nil", got.ToolNames)
				}
			} else {
				if len(got.ToolNames) != len(tt.wantTools) {
					t.Fatalf("applyAgentProfileMask() tool_names len = %d, want %d", len(got.ToolNames), len(tt.wantTools))
				}
				for i, want := range tt.wantTools {
					if got.ToolNames[i] != want {
						t.Fatalf("applyAgentProfileMask() tool_names[%d] = %q, want %q", i, got.ToolNames[i], want)
					}
				}
			}
			// Ensure existing was not mutated.
			if existing.Model != "old-model" {
				t.Fatalf("applyAgentProfileMask() mutated existing: model = %q", existing.Model)
			}
			if len(existing.ToolNames) != 1 || existing.ToolNames[0] != "t1" {
				t.Fatalf("applyAgentProfileMask() mutated existing tool_names = %v", existing.ToolNames)
			}
		})
	}
}

func TestPromptService_DeleteSuccess(t *testing.T) {
	ctx := context.Background()

	// given — create a profile first
	profileRepo := newInMemoryAgentProfileRepo()
	skillRepo := newInMemorySkillRepo()
	h := NewHandler(profileRepo, skillRepo)

	_, err := h.CreateAgentProfile(ctx, &game.CreateAgentProfileRequest{
		Parent:         gameconst.PromptsParent,
		AgentProfileId: "to-delete",
		AgentProfile: &game.AgentProfile{
			Model:   "opencode-go/deepseek-v4-pro",
			Enabled: true,
		},
	})
	assertStatusCode(t, err, codes.OK)

	// when — delete
	_, err = h.DeleteAgentProfile(ctx, &game.DeleteAgentProfileRequest{Name: "prompts/agentProfiles/to-delete"})

	// then — delete succeeds
	assertStatusCode(t, err, codes.OK)

	// when — get deleted profile
	_, err = h.GetAgentProfile(ctx, &game.GetAgentProfileRequest{Name: "prompts/agentProfiles/to-delete"})

	// then — returns NotFound
	assertStatusCode(t, err, codes.NotFound)
}

func Test_toStatusError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode codes.Code
	}{
		{
			name:     "ErrNotFound maps to NotFound",
			err:      domain.ErrNotFound,
			wantCode: codes.NotFound,
		},
		{
			name:     "ErrAlreadyExists maps to AlreadyExists",
			err:      domain.ErrAlreadyExists,
			wantCode: codes.AlreadyExists,
		},
		{
			name:     "unknown error maps to Internal",
			err:      newCustomError("something broke"),
			wantCode: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := toStatusError(tt.err)

			// then
			s, ok := status.FromError(got)
			if !ok {
				t.Fatalf("toStatusError() did not return a status error, got %v", got)
			}
			if s.Code() != tt.wantCode {
				t.Fatalf("toStatusError() code = %v, want %v", s.Code(), tt.wantCode)
			}
		})
	}
}

func Test_agentProfileToProto(t *testing.T) {
	tests := []struct {
		name     string
		profile  *domain.AgentProfile
		wantNil  bool
		wantName string
	}{
		{
			name:    "nil profile returns nil",
			profile: nil,
			wantNil: true,
		},
		{
			name: "profile with fields",
			profile: &domain.AgentProfile{
				AgentProfileName: "test",
				Model:            "opencode-go/deepseek-v4-pro",
				Enabled:          true,
				CreateTime:       time.Date(2025, 3, 20, 8, 0, 0, 0, time.UTC),
				UpdateTime:       time.Date(2025, 3, 20, 8, 0, 0, 0, time.UTC),
			},
			wantNil:  false,
			wantName: "prompts/agentProfiles/test",
		},
		{
			name: "profile with zero times has no timestamps",
			profile: &domain.AgentProfile{
				AgentProfileName: "notime",
			},
			wantNil:  false,
			wantName: "prompts/agentProfiles/notime",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := agentProfileToProto(tt.profile)

			// then
			if tt.wantNil {
				if got != nil {
					t.Fatalf("agentProfileToProto() = %v, want nil", got)
				}
				return
			}
			if got.GetName() != tt.wantName {
				t.Fatalf("agentProfileToProto() name = %q, want %q", got.GetName(), tt.wantName)
			}
		})
	}
}

func Test_skillToProto(t *testing.T) {
	tests := []struct {
		name     string
		skill    *domain.Skill
		wantNil  bool
		wantName string
	}{
		{
			name:    "nil skill returns nil",
			skill:   nil,
			wantNil: true,
		},
		{
			name: "skill with fields",
			skill: &domain.Skill{
				SkillName:  "test",
				Content:    "some content",
				Enabled:    true,
				CreateTime: time.Date(2025, 3, 20, 8, 0, 0, 0, time.UTC),
			},
			wantNil:  false,
			wantName: "prompts/skills/test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := skillToProto(tt.skill)

			// then
			if tt.wantNil {
				if got != nil {
					t.Fatalf("skillToProto() = %v, want nil", got)
				}
				return
			}
			if got.GetName() != tt.wantName {
				t.Fatalf("skillToProto() name = %q, want %q", got.GetName(), tt.wantName)
			}
		})
	}
}

// assertStatusCode checks that the gRPC status code of err matches want.
func assertStatusCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if want == codes.OK {
		if err != nil {
			t.Fatalf("expected OK, got error: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("expected code %v, got nil error", want)
	}
	got := status.Code(err)
	if got != want {
		t.Fatalf("status code = %v, want %v (error: %v)", got, want, err)
	}
}

// newCustomError creates a simple error for testing.
func newCustomError(msg string) error { return &customError{msg: msg} }

type customError struct{ msg string }

func (e *customError) Error() string { return e.msg }
