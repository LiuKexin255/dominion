// Package handler implements the PromptServiceServer gRPC interface for
// TeamProfile CRUD (spec 031-team-template-mode: AgentProfile/Skill resources
// removed, TeamProfile replaces them — clean break, FR-006/FR-007).
package handler

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
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

// Handler implements PromptServiceServer for TeamProfile CRUD operations.
type Handler struct {
	game.UnimplementedPromptServiceServer

	teamProfileRepo domain.TeamProfileRepository
}

// NewHandler creates a new Handler with the given repository.
func NewHandler(teamProfileRepo domain.TeamProfileRepository) *Handler {
	return &Handler{
		teamProfileRepo: teamProfileRepo,
	}
}

// ─── TeamProfile RPCs ─────────────────────────────────────────────────────

// CreateTeamProfile creates a TeamProfile under a template (AIP-133:
// https://google.aip.dev/133). The caller supplies the team_profile_id
// (REQUIRED per the proto contract, spec 031-team-template-mode §2.3). The
// template field of the resource body must agree with the parent, and the
// template must agree with the active oneof spec variant — the handler
// validates the consistency (no implicit rules, spec 031-team-template-mode
// directive 2).
func (h *Handler) CreateTeamProfile(ctx context.Context, req *game.CreateTeamProfileRequest) (*game.TeamProfile, error) {
	tplName, err := game.ParseTemplateName(req.GetParent())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := gameconst.ValidateTemplateName(tplName); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	profileID := req.GetTeamProfileId()
	if profileID == "" {
		return nil, status.Error(codes.InvalidArgument, "team_profile_id is required")
	}
	if strings.Contains(profileID, "/") {
		return nil, status.Error(codes.InvalidArgument, "team_profile_id must not contain '/'")
	}

	tp := req.GetTeamProfile()
	if err := validateTeamProfileBody(tp, tplName); err != nil {
		return nil, err
	}

	now := time.Now()
	profile := &domain.TeamProfile{
		TeamProfileName:    profileID,
		Template:           tplName.TemplateID,
		SaoleiPlayerModel:  tp.GetSaolei().GetPlayerModel(),
		SaoleiPlannerModel: tp.GetSaolei().GetPlannerModel(),
		CreateTime:         now,
		UpdateTime:         now,
	}

	if err := h.teamProfileRepo.CreateTeamProfile(ctx, profile); err != nil {
		return nil, toStatusError(err)
	}

	return teamProfileToProto(profile), nil
}

// GetTeamProfile retrieves a TeamProfile by its resource name.
func (h *Handler) GetTeamProfile(ctx context.Context, req *game.GetTeamProfileRequest) (*game.TeamProfile, error) {
	name, err := req.ParseName()
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := gameconst.ValidateTemplateName(name.Parent()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	profile, err := h.teamProfileRepo.GetTeamProfile(ctx, name.TemplateID, name.ProfileID)
	if err != nil {
		return nil, toStatusError(err)
	}
	return teamProfileToProto(profile), nil
}

// ListTeamProfiles retrieves a paginated list of TeamProfile resources under
// a template (AIP-132: https://google.aip.dev/132).
func (h *Handler) ListTeamProfiles(ctx context.Context, req *game.ListTeamProfilesRequest) (*game.ListTeamProfilesResponse, error) {
	tplName, err := game.ParseTemplateName(req.GetParent())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := gameconst.ValidateTemplateName(tplName); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	pageSize := int(req.GetPageSize())
	if pageSize <= 0 {
		pageSize = domain.DefaultListTeamProfilesPageSize
	}
	if pageSize > domain.MaxListTeamProfilesPageSize {
		return nil, status.Errorf(codes.InvalidArgument, "page_size exceeds maximum of %d", domain.MaxListTeamProfilesPageSize)
	}

	profiles, nextPageToken, err := h.teamProfileRepo.ListTeamProfiles(ctx, tplName.TemplateID, pageSize, req.GetPageToken())
	if err != nil {
		return nil, toStatusError(err)
	}

	protos := make([]*game.TeamProfile, 0, len(profiles))
	for _, p := range profiles {
		protos = append(protos, teamProfileToProto(p))
	}

	return &game.ListTeamProfilesResponse{
		TeamProfiles:  protos,
		NextPageToken: nextPageToken,
	}, nil
}

// UpdateTeamProfile applies a partial update described by update_mask to an
// existing TeamProfile (AIP-134: https://google.aip.dev/134). Identity is
// carried on the resource itself (TeamProfile.name), surfaced via
// req.GetTeamProfile().GetName(). update_mask supports oneof-member paths
// (saolei.player_model / saolei.planner_model, AIP-161). Unknown paths return
// INVALID_ARGUMENT; missing profiles return NOT_FOUND.
func (h *Handler) UpdateTeamProfile(ctx context.Context, req *game.UpdateTeamProfileRequest) (*game.TeamProfile, error) {
	name, err := game.ParseTeamProfileName(req.GetTeamProfile().GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := gameconst.ValidateTemplateName(name.Parent()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	patch := req.GetTeamProfile()
	if patch.GetTemplate() != "" {
		patchTpl, err := game.ParseTemplateName(patch.GetTemplate())
		if err != nil || patchTpl.TemplateID != name.TemplateID {
			return nil, status.Errorf(codes.InvalidArgument, "team_profile.template %q does not match the resource name template %q", patch.GetTemplate(), name.TemplateID)
		}
	}

	existing, err := h.teamProfileRepo.GetTeamProfile(ctx, name.TemplateID, name.ProfileID)
	if err != nil {
		return nil, toStatusError(err)
	}

	updated, err := applyTeamProfileMask(existing, patch, req.GetUpdateMask())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	updated.UpdateTime = time.Now()

	persisted, err := h.teamProfileRepo.UpdateTeamProfile(ctx, updated)
	if err != nil {
		return nil, toStatusError(err)
	}

	return teamProfileToProto(persisted), nil
}

// DeleteTeamProfile deletes a TeamProfile by its resource name.
func (h *Handler) DeleteTeamProfile(ctx context.Context, req *game.DeleteTeamProfileRequest) (*emptypb.Empty, error) {
	name, err := req.ParseName()
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := gameconst.ValidateTemplateName(name.Parent()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	if err := h.teamProfileRepo.DeleteTeamProfile(ctx, name.TemplateID, name.ProfileID); err != nil {
		return nil, toStatusError(err)
	}
	return new(emptypb.Empty), nil
}

// ─── Validation helpers ───────────────────────────────────────────────────

// validateTeamProfileBody enforces the template/oneof consistency rule: the
// resource body's template must equal the parent's template, and the template
// must agree with the active oneof spec variant (spec 031-team-template-mode
// directive 2 — no implicit rules).
func validateTeamProfileBody(tp *game.TeamProfile, parent game.TemplateName) error {
	if tp == nil {
		return status.Error(codes.InvalidArgument, "team_profile is required")
	}
	bodyTpl, err := game.ParseTemplateName(tp.GetTemplate())
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "team_profile.template must be a template resource name: %v", err)
	}
	if bodyTpl.TemplateID != parent.TemplateID {
		return status.Errorf(codes.InvalidArgument, "team_profile.template %q does not match the parent template %q", bodyTpl.String(), parent.String())
	}
	if err := validateSpecConsistency(bodyTpl, tp.GetSaolei() != nil); err != nil {
		return err
	}
	return nil
}

// validateSpecConsistency checks that the template refers to a known template
// and that its oneof spec variant is present (FR 禁潜规则: exactly one
// variant must be set, matching the template).
func validateSpecConsistency(template game.TemplateName, hasSaolei bool) error {
	if err := gameconst.ValidateTemplateName(template); err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	if template.TemplateID == gameconst.SaoleiTemplate.TemplateID && !hasSaolei {
		return status.Errorf(codes.InvalidArgument, "template %q requires the saolei spec variant to be set", template.String())
	}
	return nil
}

// ─── Conversion helpers ───────────────────────────────────────────────────

// teamProfileToProto converts a domain TeamProfile to a proto TeamProfile.
func teamProfileToProto(p *domain.TeamProfile) *game.TeamProfile {
	if p == nil {
		return nil
	}

	pb := &game.TeamProfile{
		Name:     game.TeamProfileName{TemplateID: p.Template, ProfileID: p.TeamProfileName}.String(),
		Template: game.TemplateName{TemplateID: p.Template}.String(),
		Spec: &game.TeamProfile_Saolei{Saolei: &game.SaoleiProfile{
			PlayerModel:  p.SaoleiPlayerModel,
			PlannerModel: p.SaoleiPlannerModel,
		}},
	}
	if !p.CreateTime.IsZero() {
		pb.CreateTime = timestamppb.New(p.CreateTime)
	}
	if !p.UpdateTime.IsZero() {
		pb.UpdateTime = timestamppb.New(p.UpdateTime)
	}

	return pb
}

// teamProfileMaskFields enumerates the writable TeamProfile fields addressable
// via update_mask, including the saolei oneof member paths (AIP-161
// https://google.aip.dev/161).
var teamProfileMaskFields = []string{
	"saolei", "saolei.player_model", "saolei.planner_model",
}

// applyTeamProfileMask returns a copy of existing with the masked fields
// overwritten by patch. An error is returned if update_mask references a path
// outside teamProfileMaskFields. A nil update_mask (or one with no paths) is
// treated as "all populated fields": the whole saolei spec when the patch
// carries it, otherwise no change (AIP-134).
func applyTeamProfileMask(existing *domain.TeamProfile, patch *game.TeamProfile, mask *fieldmaskpb.FieldMask) (*domain.TeamProfile, error) {
	paths := mask.GetPaths()
	if len(paths) == 0 {
		if patch.GetSaolei() != nil {
			paths = []string{"saolei"}
		} else {
			return existing, nil
		}
	}

	for _, path := range paths {
		if !slices.Contains(teamProfileMaskFields, path) {
			return nil, fmt.Errorf("update_mask path %q is not a writable TeamProfile field", path)
		}
	}

	updated := *existing
	if patch == nil {
		return &updated, nil
	}

	for _, path := range paths {
		switch path {
		case "saolei":
			s := patch.GetSaolei()
			if s == nil {
				return nil, fmt.Errorf("update_mask path %q requires the saolei spec variant to be set", path)
			}
			updated.SaoleiPlayerModel = s.GetPlayerModel()
			updated.SaoleiPlannerModel = s.GetPlannerModel()
		case "saolei.player_model":
			updated.SaoleiPlayerModel = patch.GetSaolei().GetPlayerModel()
		case "saolei.planner_model":
			updated.SaoleiPlannerModel = patch.GetSaolei().GetPlannerModel()
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
