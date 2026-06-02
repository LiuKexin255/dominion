package handler

import (
	"context"
	"sync"
	"testing"
	"time"

	game "dominion/projects/game"
	"dominion/projects/game/prompt/domain"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// inMemoryAgentProfileRepo implements domain.AgentProfileRepository for testing.
type inMemoryAgentProfileRepo struct {
	mu       sync.Mutex
	profiles map[string]*domain.AgentProfile
}

func newInMemoryAgentProfileRepo() *inMemoryAgentProfileRepo {
	return &inMemoryAgentProfileRepo{profiles: make(map[string]*domain.AgentProfile)}
}

func (r *inMemoryAgentProfileRepo) Create(_ context.Context, profile *domain.AgentProfile) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.profiles[profile.AgentProfileName]; exists {
		return domain.ErrAlreadyExists
	}
	r.profiles[profile.AgentProfileName] = profile
	return nil
}

func (r *inMemoryAgentProfileRepo) Get(_ context.Context, profileName string) (*domain.AgentProfile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.profiles[profileName]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return p, nil
}

func (r *inMemoryAgentProfileRepo) List(_ context.Context, pageSize int, pageToken string) ([]*domain.AgentProfile, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]*domain.AgentProfile, 0, len(r.profiles))
	for _, p := range r.profiles {
		result = append(result, p)
	}
	return result, "", nil
}

func (r *inMemoryAgentProfileRepo) Delete(_ context.Context, profileName string) error {
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

func (r *inMemorySkillRepo) Create(_ context.Context, skill *domain.Skill) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.skills[skill.SkillName]; exists {
		return domain.ErrAlreadyExists
	}
	r.skills[skill.SkillName] = skill
	return nil
}

func (r *inMemorySkillRepo) Get(_ context.Context, skillName string) (*domain.Skill, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.skills[skillName]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return s, nil
}

func (r *inMemorySkillRepo) List(_ context.Context, pageSize int, pageToken string) ([]*domain.Skill, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]*domain.Skill, 0, len(r.skills))
	for _, s := range r.skills {
		result = append(result, s)
	}
	return result, "", nil
}

func (r *inMemorySkillRepo) Delete(_ context.Context, skillName string) error {
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
		AgentProfileName: "test-profile",
		Model:            "gpt-4",
		SystemPrompt:     "You are a helpful assistant.",
		SkillNames:       []string{"skill-a"},
		McpNames:         []string{"mcp-server-1"},
		Enabled:          true,
	}

	// when — create
	created, err := h.CreateAgentProfile(ctx, createReq)

	// then — create succeeds
	assertStatusCode(t, err, codes.OK)
	if created.GetName() != "agentProfiles/test-profile" {
		t.Fatalf("CreateAgentProfile() name = %q, want %q", created.GetName(), "agentProfiles/test-profile")
	}
	if created.GetAgentProfileName() != "test-profile" {
		t.Fatalf("CreateAgentProfile() agent_profile_name = %q, want %q", created.GetAgentProfileName(), "test-profile")
	}
	if created.GetModel() != "gpt-4" {
		t.Fatalf("CreateAgentProfile() model = %q, want %q", created.GetModel(), "gpt-4")
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
	getReq := &game.GetAgentProfileRequest{AgentProfileName: "test-profile"}
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
		SkillName: "my-skill",
		Content:   "You know how to browse the web.",
		Enabled:   true,
	}

	// when — create
	created, err := h.CreateSkill(ctx, createReq)

	// then — create succeeds
	assertStatusCode(t, err, codes.OK)
	if created.GetName() != "skills/my-skill" {
		t.Fatalf("CreateSkill() name = %q, want %q", created.GetName(), "skills/my-skill")
	}
	if created.GetSkillName() != "my-skill" {
		t.Fatalf("CreateSkill() skill_name = %q, want %q", created.GetSkillName(), "my-skill")
	}
	if created.GetContent() != "You know how to browse the web." {
		t.Fatalf("CreateSkill() content = %q, want %q", created.GetContent(), "You know how to browse the web.")
	}
	if created.GetEnabled() != true {
		t.Fatalf("CreateSkill() enabled = %v, want true", created.GetEnabled())
	}

	// when — get
	getReq := &game.GetSkillRequest{SkillName: "my-skill"}
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
	_, err := h.GetAgentProfile(ctx, &game.GetAgentProfileRequest{AgentProfileName: "nonexistent"})

	// then — returns NotFound
	assertStatusCode(t, err, codes.NotFound)
}

func TestPromptService_DeleteSuccess(t *testing.T) {
	ctx := context.Background()

	// given — create a profile first
	profileRepo := newInMemoryAgentProfileRepo()
	skillRepo := newInMemorySkillRepo()
	h := NewHandler(profileRepo, skillRepo)

	_, err := h.CreateAgentProfile(ctx, &game.CreateAgentProfileRequest{
		AgentProfileName: "to-delete",
		Model:            "gpt-4",
		Enabled:          true,
	})
	assertStatusCode(t, err, codes.OK)

	// when — delete
	_, err = h.DeleteAgentProfile(ctx, &game.DeleteAgentProfileRequest{AgentProfileName: "to-delete"})

	// then — delete succeeds
	assertStatusCode(t, err, codes.OK)

	// when — get deleted profile
	_, err = h.GetAgentProfile(ctx, &game.GetAgentProfileRequest{AgentProfileName: "to-delete"})

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
				Name:             "agentProfiles/test",
				AgentProfileName: "test",
				Model:            "gpt-4",
				Enabled:          true,
				CreateTime:       time.Date(2025, 3, 20, 8, 0, 0, 0, time.UTC),
				UpdateTime:       time.Date(2025, 3, 20, 8, 0, 0, 0, time.UTC),
			},
			wantNil:  false,
			wantName: "agentProfiles/test",
		},
		{
			name: "profile with zero times has no timestamps",
			profile: &domain.AgentProfile{
				Name:             "agentProfiles/notime",
				AgentProfileName: "notime",
			},
			wantNil:  false,
			wantName: "agentProfiles/notime",
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
				Name:       "skills/test",
				SkillName:  "test",
				Content:    "some content",
				Enabled:    true,
				CreateTime: time.Date(2025, 3, 20, 8, 0, 0, 0, time.UTC),
			},
			wantNil:  false,
			wantName: "skills/test",
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
