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
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// inMemoryTeamProfileRepo implements domain.TeamProfileRepository for testing.
type inMemoryTeamProfileRepo struct {
	mu       sync.Mutex
	profiles map[string]*domain.TeamProfile
}

func newInMemoryTeamProfileRepo() *inMemoryTeamProfileRepo {
	return &inMemoryTeamProfileRepo{profiles: make(map[string]*domain.TeamProfile)}
}

func (r *inMemoryTeamProfileRepo) CreateTeamProfile(_ context.Context, profile *domain.TeamProfile) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.profiles[profile.TeamProfileName]; exists {
		return domain.ErrAlreadyExists
	}
	r.profiles[profile.TeamProfileName] = profile
	return nil
}

func (r *inMemoryTeamProfileRepo) GetTeamProfile(_ context.Context, template, profileName string) (*domain.TeamProfile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.profiles[profileName]
	if !ok || p.Template != template {
		return nil, domain.ErrNotFound
	}
	return p, nil
}

func (r *inMemoryTeamProfileRepo) UpdateTeamProfile(_ context.Context, profile *domain.TeamProfile) (*domain.TeamProfile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.profiles[profile.TeamProfileName]
	if !ok {
		return nil, domain.ErrNotFound
	}
	clone := *profile
	clone.CreateTime = existing.CreateTime
	r.profiles[profile.TeamProfileName] = &clone
	return &clone, nil
}

func (r *inMemoryTeamProfileRepo) ListTeamProfiles(_ context.Context, template string, pageSize int, pageToken string) ([]*domain.TeamProfile, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]*domain.TeamProfile, 0, len(r.profiles))
	for _, p := range r.profiles {
		if p.Template == template {
			result = append(result, p)
		}
	}
	return result, "", nil
}

func (r *inMemoryTeamProfileRepo) DeleteTeamProfile(_ context.Context, template, profileName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.profiles[profileName]
	if !ok || p.Template != template {
		return domain.ErrNotFound
	}
	delete(r.profiles, profileName)
	return nil
}

// saoleiProfile returns a proto TeamProfile with the saolei spec variant set.
// The template is derived from the request parent, not the resource body.
func saoleiProfile() *game.TeamProfile {
	return &game.TeamProfile{
		Spec: &game.TeamProfile_Saolei{Saolei: &game.SaoleiProfile{
			PlayerModel:   "opencode-go/deepseek-v4-pro",
			PlannerModel:  "opencode-go/deepseek-v4-pro",
			PlayerPrompt:  "player base prompt",
			PlannerPrompt: "planner base prompt",
		}},
	}
}

func TestPromptService_CreateGetTeamProfile(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newInMemoryTeamProfileRepo()
	h := NewHandler(repo)

	createReq := &game.CreateTeamProfileRequest{
		Parent:        "templates/saolei",
		TeamProfileId: "default",
		TeamProfile:   saoleiProfile(),
	}

	// when — create
	created, err := h.CreateTeamProfile(ctx, createReq)

	// then — create succeeds
	assertStatusCode(t, err, codes.OK)
	if created.GetName() != "templates/saolei/profiles/default" {
		t.Fatalf("CreateTeamProfile() name = %q, want %q", created.GetName(), "templates/saolei/profiles/default")
	}
	if created.GetSaolei().GetPlayerModel() != "opencode-go/deepseek-v4-pro" {
		t.Fatalf("CreateTeamProfile() player_model = %q, want %q", created.GetSaolei().GetPlayerModel(), "opencode-go/deepseek-v4-pro")
	}
	if created.GetSaolei().GetPlannerModel() != "opencode-go/deepseek-v4-pro" {
		t.Fatalf("CreateTeamProfile() planner_model = %q, want %q", created.GetSaolei().GetPlannerModel(), "opencode-go/deepseek-v4-pro")
	}
	if created.GetSaolei().GetPlayerPrompt() != "player base prompt" {
		t.Fatalf("CreateTeamProfile() player_prompt = %q, want %q", created.GetSaolei().GetPlayerPrompt(), "player base prompt")
	}
	if created.GetSaolei().GetPlannerPrompt() != "planner base prompt" {
		t.Fatalf("CreateTeamProfile() planner_prompt = %q, want %q", created.GetSaolei().GetPlannerPrompt(), "planner base prompt")
	}
	if created.GetCreateTime() == nil {
		t.Fatal("CreateTeamProfile() create_time is nil, want non-nil")
	}

	// when — get
	getReq := &game.GetTeamProfileRequest{Name: "templates/saolei/profiles/default"}
	got, err := h.GetTeamProfile(ctx, getReq)

	// then — get returns same profile
	assertStatusCode(t, err, codes.OK)
	if got.GetName() != created.GetName() {
		t.Fatalf("GetTeamProfile() name = %q, want %q", got.GetName(), created.GetName())
	}
	if got.GetSaolei().GetPlayerModel() != created.GetSaolei().GetPlayerModel() {
		t.Fatalf("GetTeamProfile() player_model = %q, want %q", got.GetSaolei().GetPlayerModel(), created.GetSaolei().GetPlayerModel())
	}
	if got.GetSaolei().GetPlayerPrompt() != created.GetSaolei().GetPlayerPrompt() {
		t.Fatalf("GetTeamProfile() player_prompt = %q, want %q", got.GetSaolei().GetPlayerPrompt(), created.GetSaolei().GetPlayerPrompt())
	}
	if got.GetSaolei().GetPlannerPrompt() != created.GetSaolei().GetPlannerPrompt() {
		t.Fatalf("GetTeamProfile() planner_prompt = %q, want %q", got.GetSaolei().GetPlannerPrompt(), created.GetSaolei().GetPlannerPrompt())
	}
}

func TestPromptService_CreateTeamProfileValidation(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *game.CreateTeamProfileRequest
		wantErr bool
	}{
		{
			name: "invalid parent - no templates prefix",
			req: &game.CreateTeamProfileRequest{
				Parent:        "profiles",
				TeamProfileId: "default",
				TeamProfile:   saoleiProfile(),
			},
			wantErr: true,
		},
		{
			name: "invalid parent - unknown template",
			req: &game.CreateTeamProfileRequest{
				Parent:        "templates/unknown-template",
				TeamProfileId: "default",
				TeamProfile:   saoleiProfile(),
			},
			wantErr: true,
		},
		{
			name: "saolei template requires saolei spec variant",
			req: &game.CreateTeamProfileRequest{
				Parent:        "templates/saolei",
				TeamProfileId: "default",
				TeamProfile:   &game.TeamProfile{},
			},
			wantErr: true,
		},
		{
			name: "profile id with slash",
			req: &game.CreateTeamProfileRequest{
				Parent:        "templates/saolei",
				TeamProfileId: "bad/id",
				TeamProfile:   saoleiProfile(),
			},
			wantErr: true,
		},
		{
			name: "empty team_profile_id is rejected (REQUIRED per proto)",
			req: &game.CreateTeamProfileRequest{
				Parent:      "templates/saolei",
				TeamProfile: saoleiProfile(),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			h := NewHandler(newInMemoryTeamProfileRepo())

			// when
			_, err := h.CreateTeamProfile(ctx, tt.req)

			// then
			assertStatusCode(t, err, codes.InvalidArgument)
			if !tt.wantErr {
				t.Fatalf("CreateTeamProfile() expected success, got error: %v", err)
			}
		})
	}
}

func TestPromptService_TeamProfileNotFound(t *testing.T) {
	ctx := context.Background()

	// given
	h := NewHandler(newInMemoryTeamProfileRepo())

	// when — get missing profile
	_, err := h.GetTeamProfile(ctx, &game.GetTeamProfileRequest{Name: "templates/saolei/profiles/nonexistent"})

	// then — returns NotFound
	assertStatusCode(t, err, codes.NotFound)
}

func TestPromptService_ListTeamProfiles(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newInMemoryTeamProfileRepo()
	h := NewHandler(repo)

	_, err := h.CreateTeamProfile(ctx, &game.CreateTeamProfileRequest{
		Parent:        "templates/saolei",
		TeamProfileId: "default",
		TeamProfile:   saoleiProfile(),
	})
	assertStatusCode(t, err, codes.OK)

	// when
	listResp, err := h.ListTeamProfiles(ctx, &game.ListTeamProfilesRequest{Parent: "templates/saolei"})

	// then
	assertStatusCode(t, err, codes.OK)
	if len(listResp.GetTeamProfiles()) != 1 {
		t.Fatalf("ListTeamProfiles() got %d profiles, want 1", len(listResp.GetTeamProfiles()))
	}
	if listResp.GetTeamProfiles()[0].GetName() != "templates/saolei/profiles/default" {
		t.Fatalf("ListTeamProfiles()[0] name = %q, want %q", listResp.GetTeamProfiles()[0].GetName(), "templates/saolei/profiles/default")
	}

	// when — invalid parent
	_, err = h.ListTeamProfiles(ctx, &game.ListTeamProfilesRequest{Parent: "profiles"})

	// then
	assertStatusCode(t, err, codes.InvalidArgument)
}

func TestPromptService_UpdateTeamProfileViaFieldMask(t *testing.T) {
	ctx := context.Background()

	// given — seed profile
	repo := newInMemoryTeamProfileRepo()
	h := NewHandler(repo)

	_, err := h.CreateTeamProfile(ctx, &game.CreateTeamProfileRequest{
		Parent:        "templates/saolei",
		TeamProfileId: "mask-profile",
		TeamProfile:   saoleiProfile(),
	})
	assertStatusCode(t, err, codes.OK)

	// when — update player_model only via FieldMask (oneof member path)
	updateReq := &game.UpdateTeamProfileRequest{
		TeamProfile: &game.TeamProfile{
			Name: "templates/saolei/profiles/mask-profile",
			Spec: &game.TeamProfile_Saolei{Saolei: &game.SaoleiProfile{
				PlayerModel: "opencode-go/gpt-5",
			}},
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"saolei.player_model"}},
	}
	updated, err := h.UpdateTeamProfile(ctx, updateReq)

	// then — player_model updated, planner_model and prompts preserved
	assertStatusCode(t, err, codes.OK)
	if updated.GetSaolei().GetPlayerModel() != "opencode-go/gpt-5" {
		t.Fatalf("UpdateTeamProfile() player_model = %q, want %q", updated.GetSaolei().GetPlayerModel(), "opencode-go/gpt-5")
	}
	if updated.GetSaolei().GetPlannerModel() != "opencode-go/deepseek-v4-pro" {
		t.Fatalf("UpdateTeamProfile() planner_model = %q, want %q (FieldMask should not touch)", updated.GetSaolei().GetPlannerModel(), "opencode-go/deepseek-v4-pro")
	}
	if updated.GetSaolei().GetPlayerPrompt() != "player base prompt" {
		t.Fatalf("UpdateTeamProfile() player_prompt = %q, want %q (FieldMask should not touch)", updated.GetSaolei().GetPlayerPrompt(), "player base prompt")
	}
	if updated.GetSaolei().GetPlannerPrompt() != "planner base prompt" {
		t.Fatalf("UpdateTeamProfile() planner_prompt = %q, want %q (FieldMask should not touch)", updated.GetSaolei().GetPlannerPrompt(), "planner base prompt")
	}
	if updated.GetUpdateTime() == nil {
		t.Fatal("UpdateTeamProfile() update_time is nil, want non-nil")
	}
	if updated.GetCreateTime() == nil {
		t.Fatal("UpdateTeamProfile() create_time is nil, want non-nil")
	}

	// when — re-fetch
	got, err := h.GetTeamProfile(ctx, &game.GetTeamProfileRequest{Name: "templates/saolei/profiles/mask-profile"})

	// then — persisted
	assertStatusCode(t, err, codes.OK)
	if got.GetSaolei().GetPlayerModel() != "opencode-go/gpt-5" {
		t.Fatalf("GetTeamProfile() after update player_model = %q, want %q", got.GetSaolei().GetPlayerModel(), "opencode-go/gpt-5")
	}
}

func TestPromptService_UpdateTeamProfilePromptViaFieldMask(t *testing.T) {
	ctx := context.Background()

	// given — seed profile with models and prompts
	repo := newInMemoryTeamProfileRepo()
	h := NewHandler(repo)

	_, err := h.CreateTeamProfile(ctx, &game.CreateTeamProfileRequest{
		Parent:        "templates/saolei",
		TeamProfileId: "prompt-profile",
		TeamProfile:   saoleiProfile(),
	})
	assertStatusCode(t, err, codes.OK)

	// when — update player_prompt only via FieldMask (oneof member path)
	updateReq := &game.UpdateTeamProfileRequest{
		TeamProfile: &game.TeamProfile{
			Name: "templates/saolei/profiles/prompt-profile",
			Spec: &game.TeamProfile_Saolei{Saolei: &game.SaoleiProfile{
				PlayerPrompt: "custom player base",
			}},
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"saolei.player_prompt"}},
	}
	updated, err := h.UpdateTeamProfile(ctx, updateReq)

	// then — player_prompt updated, other fields preserved
	assertStatusCode(t, err, codes.OK)
	if updated.GetSaolei().GetPlayerPrompt() != "custom player base" {
		t.Fatalf("UpdateTeamProfile() player_prompt = %q, want %q", updated.GetSaolei().GetPlayerPrompt(), "custom player base")
	}
	if updated.GetSaolei().GetPlayerModel() != "opencode-go/deepseek-v4-pro" {
		t.Fatalf("UpdateTeamProfile() player_model = %q, want %q (FieldMask should not touch)", updated.GetSaolei().GetPlayerModel(), "opencode-go/deepseek-v4-pro")
	}
	if updated.GetSaolei().GetPlannerModel() != "opencode-go/deepseek-v4-pro" {
		t.Fatalf("UpdateTeamProfile() planner_model = %q, want %q (FieldMask should not touch)", updated.GetSaolei().GetPlannerModel(), "opencode-go/deepseek-v4-pro")
	}
	if updated.GetSaolei().GetPlannerPrompt() != "planner base prompt" {
		t.Fatalf("UpdateTeamProfile() planner_prompt = %q, want %q (FieldMask should not touch)", updated.GetSaolei().GetPlannerPrompt(), "planner base prompt")
	}
	if updated.GetUpdateTime() == nil {
		t.Fatal("UpdateTeamProfile() update_time is nil, want non-nil")
	}

	// when — re-fetch
	got, err := h.GetTeamProfile(ctx, &game.GetTeamProfileRequest{Name: "templates/saolei/profiles/prompt-profile"})

	// then — persisted
	assertStatusCode(t, err, codes.OK)
	if got.GetSaolei().GetPlayerPrompt() != "custom player base" {
		t.Fatalf("GetTeamProfile() after update player_prompt = %q, want %q", got.GetSaolei().GetPlayerPrompt(), "custom player base")
	}
	if got.GetSaolei().GetPlannerPrompt() != "planner base prompt" {
		t.Fatalf("GetTeamProfile() after update planner_prompt = %q, want %q (FieldMask should not touch)", got.GetSaolei().GetPlannerPrompt(), "planner base prompt")
	}
}

func TestPromptService_UpdateTeamProfileWholeSpec(t *testing.T) {
	ctx := context.Background()

	// given — seed profile
	repo := newInMemoryTeamProfileRepo()
	h := NewHandler(repo)

	_, err := h.CreateTeamProfile(ctx, &game.CreateTeamProfileRequest{
		Parent:        "templates/saolei",
		TeamProfileId: "whole-spec",
		TeamProfile:   saoleiProfile(),
	})
	assertStatusCode(t, err, codes.OK)

	// when — update the whole saolei spec via FieldMask path "saolei"
	updateReq := &game.UpdateTeamProfileRequest{
		TeamProfile: &game.TeamProfile{
			Name: "templates/saolei/profiles/whole-spec",
			Spec: &game.TeamProfile_Saolei{Saolei: &game.SaoleiProfile{
				PlayerModel:  "model-p",
				PlannerModel: "model-l",
			}},
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"saolei"}},
	}
	updated, err := h.UpdateTeamProfile(ctx, updateReq)

	// then
	assertStatusCode(t, err, codes.OK)
	if updated.GetSaolei().GetPlayerModel() != "model-p" {
		t.Fatalf("UpdateTeamProfile() player_model = %q, want %q", updated.GetSaolei().GetPlayerModel(), "model-p")
	}
	if updated.GetSaolei().GetPlannerModel() != "model-l" {
		t.Fatalf("UpdateTeamProfile() planner_model = %q, want %q", updated.GetSaolei().GetPlannerModel(), "model-l")
	}
}

func TestPromptService_UpdateTeamProfileValidation(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newInMemoryTeamProfileRepo()
	h := NewHandler(repo)

	_, err := h.CreateTeamProfile(ctx, &game.CreateTeamProfileRequest{
		Parent:        "templates/saolei",
		TeamProfileId: "unknown-path-profile",
		TeamProfile:   saoleiProfile(),
	})
	assertStatusCode(t, err, codes.OK)

	// when — update with an unknown FieldMask path
	_, err = h.UpdateTeamProfile(ctx, &game.UpdateTeamProfileRequest{
		TeamProfile: &game.TeamProfile{
			Name: "templates/saolei/profiles/unknown-path-profile",
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"nonexistent_field"}},
	})

	// then — returns InvalidArgument
	assertStatusCode(t, err, codes.InvalidArgument)

	// when — update with a matching oneof-member mask path: must succeed
	_, err = h.UpdateTeamProfile(ctx, &game.UpdateTeamProfileRequest{
		TeamProfile: &game.TeamProfile{
			Name: "templates/saolei/profiles/unknown-path-profile",
			Spec: &game.TeamProfile_Saolei{Saolei: &game.SaoleiProfile{
				PlayerModel: "x",
			}},
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"saolei.player_model"}},
	})

	// then — returns OK
	assertStatusCode(t, err, codes.OK)
}

func TestPromptService_UpdateTeamProfileNotFound(t *testing.T) {
	ctx := context.Background()

	// given
	h := NewHandler(newInMemoryTeamProfileRepo())

	// when — update missing profile
	updateReq := &game.UpdateTeamProfileRequest{
		TeamProfile: &game.TeamProfile{
			Name: "templates/saolei/profiles/ghost",
			Spec: &game.TeamProfile_Saolei{Saolei: &game.SaoleiProfile{
				PlayerModel: "opencode-go/deepseek-v4-pro",
			}},
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"saolei.player_model"}},
	}
	_, err := h.UpdateTeamProfile(ctx, updateReq)

	// then — returns NotFound
	assertStatusCode(t, err, codes.NotFound)
}

func Test_applyTeamProfileMask(t *testing.T) {
	existing := &domain.TeamProfile{
		TeamProfileName:     "p",
		Template:            "saolei",
		SaoleiPlayerModel:   "old-player",
		SaoleiPlannerModel:  "old-planner",
		SaoleiPlayerPrompt:  "old-player-prompt",
		SaoleiPlannerPrompt: "old-planner-prompt",
	}

	tests := []struct {
		name              string
		patch             *game.TeamProfile
		mask              *fieldmaskpb.FieldMask
		wantPlayer        string
		wantPlanner       string
		wantPlayerPrompt  string
		wantPlannerPrompt string
		wantErr           bool
	}{
		{
			name:              "nil mask with saolei patch replaces whole spec",
			patch:             &game.TeamProfile{Spec: &game.TeamProfile_Saolei{Saolei: &game.SaoleiProfile{PlayerModel: "new-p", PlannerModel: "new-l"}}},
			mask:              nil,
			wantPlayer:        "new-p",
			wantPlanner:       "new-l",
			wantPlayerPrompt:  "",
			wantPlannerPrompt: "",
		},
		{
			name:              "nil mask without spec leaves existing unchanged",
			patch:             &game.TeamProfile{},
			mask:              nil,
			wantPlayer:        "old-player",
			wantPlanner:       "old-planner",
			wantPlayerPrompt:  "old-player-prompt",
			wantPlannerPrompt: "old-planner-prompt",
		},
		{
			name:              "empty mask paths without spec leaves existing unchanged",
			patch:             &game.TeamProfile{},
			mask:              &fieldmaskpb.FieldMask{Paths: nil},
			wantPlayer:        "old-player",
			wantPlanner:       "old-planner",
			wantPlayerPrompt:  "old-player-prompt",
			wantPlannerPrompt: "old-planner-prompt",
		},
		{
			name:              "saolei.player_model only",
			patch:             &game.TeamProfile{Spec: &game.TeamProfile_Saolei{Saolei: &game.SaoleiProfile{PlayerModel: "new-p"}}},
			mask:              &fieldmaskpb.FieldMask{Paths: []string{"saolei.player_model"}},
			wantPlayer:        "new-p",
			wantPlanner:       "old-planner",
			wantPlayerPrompt:  "old-player-prompt",
			wantPlannerPrompt: "old-planner-prompt",
		},
		{
			name:              "saolei.planner_model only",
			patch:             &game.TeamProfile{Spec: &game.TeamProfile_Saolei{Saolei: &game.SaoleiProfile{PlannerModel: "new-l"}}},
			mask:              &fieldmaskpb.FieldMask{Paths: []string{"saolei.planner_model"}},
			wantPlayer:        "old-player",
			wantPlanner:       "new-l",
			wantPlayerPrompt:  "old-player-prompt",
			wantPlannerPrompt: "old-planner-prompt",
		},
		{
			name:              "both oneof member paths",
			patch:             &game.TeamProfile{Spec: &game.TeamProfile_Saolei{Saolei: &game.SaoleiProfile{PlayerModel: "p2", PlannerModel: "l2"}}},
			mask:              &fieldmaskpb.FieldMask{Paths: []string{"saolei.player_model", "saolei.planner_model"}},
			wantPlayer:        "p2",
			wantPlanner:       "l2",
			wantPlayerPrompt:  "old-player-prompt",
			wantPlannerPrompt: "old-planner-prompt",
		},
		{
			name:              "saolei.player_prompt only",
			patch:             &game.TeamProfile{Spec: &game.TeamProfile_Saolei{Saolei: &game.SaoleiProfile{PlayerPrompt: "new-prompt"}}},
			mask:              &fieldmaskpb.FieldMask{Paths: []string{"saolei.player_prompt"}},
			wantPlayer:        "old-player",
			wantPlanner:       "old-planner",
			wantPlayerPrompt:  "new-prompt",
			wantPlannerPrompt: "old-planner-prompt",
		},
		{
			name:              "saolei.planner_prompt only",
			patch:             &game.TeamProfile{Spec: &game.TeamProfile_Saolei{Saolei: &game.SaoleiProfile{PlannerPrompt: "new-planner-prompt"}}},
			mask:              &fieldmaskpb.FieldMask{Paths: []string{"saolei.planner_prompt"}},
			wantPlayer:        "old-player",
			wantPlanner:       "old-planner",
			wantPlayerPrompt:  "old-player-prompt",
			wantPlannerPrompt: "new-planner-prompt",
		},
		{
			name:              "whole saolei path replaces both",
			patch:             &game.TeamProfile{Spec: &game.TeamProfile_Saolei{Saolei: &game.SaoleiProfile{PlayerModel: "p3", PlannerModel: "l3", PlayerPrompt: "pp3", PlannerPrompt: "lp3"}}},
			mask:              &fieldmaskpb.FieldMask{Paths: []string{"saolei"}},
			wantPlayer:        "p3",
			wantPlanner:       "l3",
			wantPlayerPrompt:  "pp3",
			wantPlannerPrompt: "lp3",
		},
		{
			name:    "unknown path returns error",
			patch:   &game.TeamProfile{},
			mask:    &fieldmaskpb.FieldMask{Paths: []string{"bogus"}},
			wantErr: true,
		},
		{
			name:    "unknown path mixed with valid still errors",
			patch:   &game.TeamProfile{Spec: &game.TeamProfile_Saolei{Saolei: &game.SaoleiProfile{PlayerModel: "new"}}},
			mask:    &fieldmaskpb.FieldMask{Paths: []string{"saolei.player_model", "bogus"}},
			wantErr: true,
		},
		{
			name:    "whole saolei path without spec returns error",
			patch:   &game.TeamProfile{},
			mask:    &fieldmaskpb.FieldMask{Paths: []string{"saolei"}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, err := applyTeamProfileMask(existing, tt.patch, tt.mask)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("applyTeamProfileMask() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("applyTeamProfileMask() unexpected error: %v", err)
			}
			if got.SaoleiPlayerModel != tt.wantPlayer {
				t.Fatalf("applyTeamProfileMask() player_model = %q, want %q", got.SaoleiPlayerModel, tt.wantPlayer)
			}
			if got.SaoleiPlannerModel != tt.wantPlanner {
				t.Fatalf("applyTeamProfileMask() planner_model = %q, want %q", got.SaoleiPlannerModel, tt.wantPlanner)
			}
			if got.SaoleiPlayerPrompt != tt.wantPlayerPrompt {
				t.Fatalf("applyTeamProfileMask() player_prompt = %q, want %q", got.SaoleiPlayerPrompt, tt.wantPlayerPrompt)
			}
			if got.SaoleiPlannerPrompt != tt.wantPlannerPrompt {
				t.Fatalf("applyTeamProfileMask() planner_prompt = %q, want %q", got.SaoleiPlannerPrompt, tt.wantPlannerPrompt)
			}
			// Ensure existing was not mutated.
			if existing.SaoleiPlayerModel != "old-player" {
				t.Fatalf("applyTeamProfileMask() mutated existing: player_model = %q", existing.SaoleiPlayerModel)
			}
			if existing.SaoleiPlannerModel != "old-planner" {
				t.Fatalf("applyTeamProfileMask() mutated existing: planner_model = %q", existing.SaoleiPlannerModel)
			}
			if existing.SaoleiPlayerPrompt != "old-player-prompt" {
				t.Fatalf("applyTeamProfileMask() mutated existing: player_prompt = %q", existing.SaoleiPlayerPrompt)
			}
			if existing.SaoleiPlannerPrompt != "old-planner-prompt" {
				t.Fatalf("applyTeamProfileMask() mutated existing: planner_prompt = %q", existing.SaoleiPlannerPrompt)
			}
		})
	}
}

func TestPromptService_DeleteTeamProfile(t *testing.T) {
	ctx := context.Background()

	// given — create a profile first
	repo := newInMemoryTeamProfileRepo()
	h := NewHandler(repo)

	_, err := h.CreateTeamProfile(ctx, &game.CreateTeamProfileRequest{
		Parent:        "templates/saolei",
		TeamProfileId: "to-delete",
		TeamProfile:   saoleiProfile(),
	})
	assertStatusCode(t, err, codes.OK)

	// when — delete
	_, err = h.DeleteTeamProfile(ctx, &game.DeleteTeamProfileRequest{Name: "templates/saolei/profiles/to-delete"})

	// then — delete succeeds
	assertStatusCode(t, err, codes.OK)

	// when — get deleted profile
	_, err = h.GetTeamProfile(ctx, &game.GetTeamProfileRequest{Name: "templates/saolei/profiles/to-delete"})

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

func Test_teamProfileToProto(t *testing.T) {
	tests := []struct {
		name     string
		profile  *domain.TeamProfile
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
			profile: &domain.TeamProfile{
				TeamProfileName:     "test",
				Template:            "saolei",
				SaoleiPlayerModel:   "opencode-go/deepseek-v4-pro",
				SaoleiPlannerModel:  "opencode-go/deepseek-v4-pro",
				SaoleiPlayerPrompt:  "player base prompt",
				SaoleiPlannerPrompt: "planner base prompt",
				CreateTime:          time.Date(2025, 3, 20, 8, 0, 0, 0, time.UTC),
				UpdateTime:          time.Date(2025, 3, 20, 8, 0, 0, 0, time.UTC),
			},
			wantNil:  false,
			wantName: "templates/saolei/profiles/test",
		},
		{
			name: "profile with zero times has no timestamps",
			profile: &domain.TeamProfile{
				TeamProfileName: "notime",
				Template:        "saolei",
			},
			wantNil:  false,
			wantName: "templates/saolei/profiles/notime",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := teamProfileToProto(tt.profile)

			// then
			if tt.wantNil {
				if got != nil {
					t.Fatalf("teamProfileToProto() = %v, want nil", got)
				}
				return
			}
			if got.GetName() != tt.wantName {
				t.Fatalf("teamProfileToProto() name = %q, want %q", got.GetName(), tt.wantName)
			}
			if got.GetSaolei() == nil {
				t.Fatalf("teamProfileToProto() saolei spec is nil, want set")
			}
			if got.GetSaolei().GetPlayerPrompt() != tt.profile.SaoleiPlayerPrompt {
				t.Fatalf("teamProfileToProto() player_prompt = %q, want %q", got.GetSaolei().GetPlayerPrompt(), tt.profile.SaoleiPlayerPrompt)
			}
			if got.GetSaolei().GetPlannerPrompt() != tt.profile.SaoleiPlannerPrompt {
				t.Fatalf("teamProfileToProto() planner_prompt = %q, want %q", got.GetSaolei().GetPlannerPrompt(), tt.profile.SaoleiPlannerPrompt)
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
