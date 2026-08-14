// Package handler implements the MemoryServiceServer gRPC interface for
// Memory CRUD (spec 039-planner-memory-calibration FR-006;
// specs/039-planner-memory-calibration/contracts/memory-service-contract.md
// §2/§6).
package handler

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	game "dominion/projects/game"
	"dominion/projects/game/memory/domain"
	"dominion/projects/game/pkg/gameconst"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// memoryIDPattern is the allowed character set for the memory resource id
// ({memory} pattern segment,
// specs/039-planner-memory-calibration/contracts/memory-service-contract.md
// §2/§6). The agent generates memory_id internally on add (spec 039 Session
// 2026-08-08 / FR-008); the service rejects anything outside [a-z0-9_-].
var memoryIDPattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

// Handler implements MemoryServiceServer for Memory CRUD operations.
type Handler struct {
	game.UnimplementedMemoryServiceServer

	memoryRepo domain.MemoryRepository
}

// NewHandler creates a new Handler with the given repository.
func NewHandler(memoryRepo domain.MemoryRepository) *Handler {
	return &Handler{
		memoryRepo: memoryRepo,
	}
}

// ─── Memory RPCs ─────────────────────────────────────────────────────────

// CreateMemory creates a Memory under a session (AIP-133:
// https://google.aip.dev/133). The resource is embedded in the request — the
// memory text is carried by req.memory.content
// (specs/039-planner-memory-calibration/contracts/memory-service-contract.md
// §2), not a top-level request field. The template
// and session are derived from the request parent; the caller supplies the
// memory_id (REQUIRED per the proto contract, agent-generated on add).
func (h *Handler) CreateMemory(ctx context.Context, req *game.CreateMemoryRequest) (*game.Memory, error) {
	sessName, err := game.ParseSessionName(req.GetParent())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := gameconst.ValidateTemplateName(sessName.Parent()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	memoryID := req.GetMemoryId()
	if !memoryIDPattern.MatchString(memoryID) {
		return nil, status.Errorf(codes.InvalidArgument, "memory_id %q must match %s", memoryID, memoryIDPattern.String())
	}

	body := req.GetMemory()
	if body == nil {
		return nil, status.Error(codes.InvalidArgument, "memory is required")
	}

	now := time.Now()
	memory := &domain.Memory{
		Template:   sessName.TemplateID,
		SessionID:  sessName.SessionID,
		MemoryID:   memoryID,
		Content:    body.GetContent(),
		CreateTime: now,
		UpdateTime: now,
	}

	if err := h.memoryRepo.CreateMemory(ctx, memory); err != nil {
		return nil, toStatusError(err)
	}

	return memoryToProto(memory), nil
}

// UpdateMemory applies a partial update described by update_mask to an
// existing Memory (AIP-134: https://google.aip.dev/134). Identity is carried
// on the resource itself (Memory.name), surfaced via
// req.GetMemory().GetName(). The only mutable Memory field is content
// (specs/039-planner-memory-calibration/contracts/memory-service-contract.md
// §2); unknown mask paths return
// INVALID_ARGUMENT, missing memories return NOT_FOUND (no create-or-update).
// The server-managed create_time is preserved by the repository.
func (h *Handler) UpdateMemory(ctx context.Context, req *game.UpdateMemoryRequest) (*game.Memory, error) {
	name, err := game.ParseMemoryName(req.GetMemory().GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := gameconst.ValidateTemplateName(name.Parent().Parent()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	content, err := applyMemoryMask(req.GetMemory(), req.GetUpdateMask())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	memory := &domain.Memory{
		Template:   name.TemplateID,
		SessionID:  name.SessionID,
		MemoryID:   name.MemoryID,
		Content:    content,
		UpdateTime: time.Now(),
	}

	persisted, err := h.memoryRepo.UpdateMemory(ctx, memory)
	if err != nil {
		return nil, toStatusError(err)
	}

	return memoryToProto(persisted), nil
}

// DeleteMemory deletes a Memory by its resource name (AIP-135:
// https://google.aip.dev/135). A missing memory errors with NOT_FOUND.
func (h *Handler) DeleteMemory(ctx context.Context, req *game.DeleteMemoryRequest) (*emptypb.Empty, error) {
	name, err := req.ParseName()
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := gameconst.ValidateTemplateName(name.Parent().Parent()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	if err := h.memoryRepo.DeleteMemory(ctx, name.TemplateID, name.SessionID, name.MemoryID); err != nil {
		return nil, toStatusError(err)
	}
	return new(emptypb.Empty), nil
}

// ListMemories retrieves a paginated list of Memory resources under a session
// (AIP-132: https://google.aip.dev/132; AIP-158 pagination:
// https://google.aip.dev/158). page_size defaults to
// domain.DefaultListMemoriesPageSize and is capped at
// domain.MaxListMemoriesPageSize.
func (h *Handler) ListMemories(ctx context.Context, req *game.ListMemoriesRequest) (*game.ListMemoriesResponse, error) {
	sessName, err := game.ParseSessionName(req.GetParent())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := gameconst.ValidateTemplateName(sessName.Parent()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	pageSize := int(req.GetPageSize())
	if pageSize <= 0 {
		pageSize = domain.DefaultListMemoriesPageSize
	}
	if pageSize > domain.MaxListMemoriesPageSize {
		return nil, status.Errorf(codes.InvalidArgument, "page_size exceeds maximum of %d", domain.MaxListMemoriesPageSize)
	}

	memories, nextPageToken, err := h.memoryRepo.ListMemories(ctx, sessName.TemplateID, sessName.SessionID, pageSize, req.GetPageToken())
	if err != nil {
		return nil, toStatusError(err)
	}

	protos := make([]*game.Memory, 0, len(memories))
	for _, m := range memories {
		protos = append(protos, memoryToProto(m))
	}

	return &game.ListMemoriesResponse{
		Memories:      protos,
		NextPageToken: nextPageToken,
	}, nil
}

// ─── Validation helpers ───────────────────────────────────────────────────

// memoryMaskFields enumerates the writable Memory fields addressable via
// update_mask (AIP-161 https://google.aip.dev/161). The only mutable Memory
// field is content (specs/039-planner-memory-calibration/contracts/
// memory-service-contract.md §1.2/§2).
var memoryMaskFields = []string{"content"}

// applyMemoryMask validates update_mask against the writable Memory fields
// and returns the resulting content. An error is returned if update_mask
// references a path outside memoryMaskFields. A nil update_mask (or one with
// no paths) is treated as "all populated fields": the whole content field
// (AIP-134 https://google.aip.dev/134).
func applyMemoryMask(patch *game.Memory, mask *fieldmaskpb.FieldMask) (string, error) {
	paths := mask.GetPaths()
	if len(paths) == 0 {
		paths = []string{"content"}
	}

	for _, path := range paths {
		if !slices.Contains(memoryMaskFields, path) {
			return "", fmt.Errorf("update_mask path %q is not a writable Memory field", path)
		}
	}

	if patch == nil {
		return "", nil
	}
	return patch.GetContent(), nil
}

// ─── Conversion helpers ───────────────────────────────────────────────────

// memoryToProto converts a domain Memory to a proto Memory.
func memoryToProto(m *domain.Memory) *game.Memory {
	if m == nil {
		return nil
	}

	pb := &game.Memory{
		Name:     game.MemoryName{TemplateID: m.Template, SessionID: m.SessionID, MemoryID: m.MemoryID}.String(),
		MemoryId: m.MemoryID,
		Content:  m.Content,
	}
	if !m.CreateTime.IsZero() {
		pb.CreateTime = timestamppb.New(m.CreateTime)
	}
	if !m.UpdateTime.IsZero() {
		pb.UpdateTime = timestamppb.New(m.UpdateTime)
	}

	return pb
}

// toStatusError maps domain errors to gRPC status errors (AIP-193:
// https://google.aip.dev/193).
func toStatusError(err error) error {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, domain.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, err.Error())
	default:
		return status.Error(codes.Internal, fmt.Sprintf("memory handler: %v", err))
	}
}
