package handler

import (
	"context"
	"sync"
	"testing"
	"time"

	game "dominion/projects/game"
	"dominion/projects/game/memory/domain"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// inMemoryMemoryRepo implements domain.MemoryRepository for testing. The
// identity is the (template, session_id, memory_id) tuple.
type inMemoryMemoryRepo struct {
	mu       sync.Mutex
	memories map[string]*domain.Memory
}

func newInMemoryMemoryRepo() *inMemoryMemoryRepo {
	return &inMemoryMemoryRepo{memories: make(map[string]*domain.Memory)}
}

func memoryKey(template, session, memoryID string) string {
	return template + "\x00" + session + "\x00" + memoryID
}

func (r *inMemoryMemoryRepo) CreateMemory(_ context.Context, memory *domain.Memory) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := memoryKey(memory.Template, memory.SessionID, memory.MemoryID)
	if _, exists := r.memories[key]; exists {
		return domain.ErrAlreadyExists
	}
	r.memories[key] = memory
	return nil
}

func (r *inMemoryMemoryRepo) UpdateMemory(_ context.Context, memory *domain.Memory) (*domain.Memory, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := memoryKey(memory.Template, memory.SessionID, memory.MemoryID)
	existing, ok := r.memories[key]
	if !ok {
		return nil, domain.ErrNotFound
	}
	clone := *memory
	clone.CreateTime = existing.CreateTime
	r.memories[key] = &clone
	return &clone, nil
}

func (r *inMemoryMemoryRepo) DeleteMemory(_ context.Context, template, session, memoryID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := memoryKey(template, session, memoryID)
	if _, ok := r.memories[key]; !ok {
		return domain.ErrNotFound
	}
	delete(r.memories, key)
	return nil
}

func (r *inMemoryMemoryRepo) ListMemories(_ context.Context, template, session string, _ int, _ string) ([]*domain.Memory, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]*domain.Memory, 0, len(r.memories))
	for _, m := range r.memories {
		if m.Template == template && m.SessionID == session {
			result = append(result, m)
		}
	}
	return result, "", nil
}

// memoryBody returns a proto Memory with the given content.
func memoryBody(content string) *game.Memory {
	return &game.Memory{Content: content}
}

func TestMemoryService_CreateListMemory(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newInMemoryMemoryRepo()
	h := NewHandler(repo)

	createReq := &game.CreateMemoryRequest{
		Parent:   "templates/saolei/sessions/session-1",
		MemoryId: "mem-1",
		Memory:   memoryBody("player repeats the same mistake"),
	}

	// when — create
	created, err := h.CreateMemory(ctx, createReq)

	// then — create succeeds
	assertStatusCode(t, err, codes.OK)
	if created.GetName() != "templates/saolei/sessions/session-1/memories/mem-1" {
		t.Fatalf("CreateMemory() name = %q, want %q", created.GetName(), "templates/saolei/sessions/session-1/memories/mem-1")
	}
	if created.GetMemoryId() != "mem-1" {
		t.Fatalf("CreateMemory() memory_id = %q, want %q", created.GetMemoryId(), "mem-1")
	}
	if created.GetContent() != "player repeats the same mistake" {
		t.Fatalf("CreateMemory() content = %q, want %q", created.GetContent(), "player repeats the same mistake")
	}
	if created.GetCreateTime() == nil {
		t.Fatal("CreateMemory() create_time is nil, want non-nil")
	}
	if created.GetUpdateTime() == nil {
		t.Fatal("CreateMemory() update_time is nil, want non-nil")
	}

	// when — list
	listResp, err := h.ListMemories(ctx, &game.ListMemoriesRequest{Parent: "templates/saolei/sessions/session-1"})

	// then — list returns the created memory
	assertStatusCode(t, err, codes.OK)
	if len(listResp.GetMemories()) != 1 {
		t.Fatalf("ListMemories() got %d memories, want 1", len(listResp.GetMemories()))
	}
	if listResp.GetMemories()[0].GetName() != created.GetName() {
		t.Fatalf("ListMemories()[0] name = %q, want %q", listResp.GetMemories()[0].GetName(), created.GetName())
	}
	if listResp.GetMemories()[0].GetMemoryId() != "mem-1" {
		t.Fatalf("ListMemories()[0] memory_id = %q, want %q", listResp.GetMemories()[0].GetMemoryId(), "mem-1")
	}
	if listResp.GetMemories()[0].GetContent() != created.GetContent() {
		t.Fatalf("ListMemories()[0] content = %q, want %q", listResp.GetMemories()[0].GetContent(), created.GetContent())
	}
}

func TestMemoryService_CreateMemoryValidation(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *game.CreateMemoryRequest
		wantErr bool
	}{
		{
			name: "invalid parent - no templates prefix",
			req: &game.CreateMemoryRequest{
				Parent:   "sessions/session-1",
				MemoryId: "mem-1",
				Memory:   memoryBody("content"),
			},
			wantErr: true,
		},
		{
			name: "invalid parent - unknown template",
			req: &game.CreateMemoryRequest{
				Parent:   "templates/unknown-template/sessions/session-1",
				MemoryId: "mem-1",
				Memory:   memoryBody("content"),
			},
			wantErr: true,
		},
		{
			name: "memory_id with uppercase is rejected",
			req: &game.CreateMemoryRequest{
				Parent:   "templates/saolei/sessions/session-1",
				MemoryId: "Mem-1",
				Memory:   memoryBody("content"),
			},
			wantErr: true,
		},
		{
			name: "memory_id with slash is rejected",
			req: &game.CreateMemoryRequest{
				Parent:   "templates/saolei/sessions/session-1",
				MemoryId: "bad/id",
				Memory:   memoryBody("content"),
			},
			wantErr: true,
		},
		{
			name: "memory_id with dot is rejected",
			req: &game.CreateMemoryRequest{
				Parent:   "templates/saolei/sessions/session-1",
				MemoryId: "mem.1",
				Memory:   memoryBody("content"),
			},
			wantErr: true,
		},
		{
			name: "empty memory_id is rejected",
			req: &game.CreateMemoryRequest{
				Parent: "templates/saolei/sessions/session-1",
				Memory: memoryBody("content"),
			},
			wantErr: true,
		},
		{
			name: "missing memory body is rejected",
			req: &game.CreateMemoryRequest{
				Parent:   "templates/saolei/sessions/session-1",
				MemoryId: "mem-1",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			h := NewHandler(newInMemoryMemoryRepo())

			// when
			_, err := h.CreateMemory(ctx, tt.req)

			// then
			assertStatusCode(t, err, codes.InvalidArgument)
			if !tt.wantErr {
				t.Fatalf("CreateMemory() expected success, got error: %v", err)
			}
		})
	}
}

func TestMemoryService_CreateMemoryAlreadyExists(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newInMemoryMemoryRepo()
	h := NewHandler(repo)

	req := &game.CreateMemoryRequest{
		Parent:   "templates/saolei/sessions/session-1",
		MemoryId: "mem-1",
		Memory:   memoryBody("content"),
	}
	_, err := h.CreateMemory(ctx, req)
	assertStatusCode(t, err, codes.OK)

	// when — create duplicate
	_, err = h.CreateMemory(ctx, req)

	// then — returns AlreadyExists
	assertStatusCode(t, err, codes.AlreadyExists)
}

func TestMemoryService_UpdateMemory(t *testing.T) {
	ctx := context.Background()

	// given — seed memory
	repo := newInMemoryMemoryRepo()
	h := NewHandler(repo)

	created, err := h.CreateMemory(ctx, &game.CreateMemoryRequest{
		Parent:   "templates/saolei/sessions/session-1",
		MemoryId: "mem-1",
		Memory:   memoryBody("old content"),
	})
	assertStatusCode(t, err, codes.OK)

	// when — update content via FieldMask
	updated, err := h.UpdateMemory(ctx, &game.UpdateMemoryRequest{
		Memory: &game.Memory{
			Name:    "templates/saolei/sessions/session-1/memories/mem-1",
			Content: "new content",
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"content"}},
	})

	// then — content updated, create_time preserved
	assertStatusCode(t, err, codes.OK)
	if updated.GetContent() != "new content" {
		t.Fatalf("UpdateMemory() content = %q, want %q", updated.GetContent(), "new content")
	}
	if updated.GetUpdateTime() == nil {
		t.Fatal("UpdateMemory() update_time is nil, want non-nil")
	}
	if updated.GetCreateTime() == nil {
		t.Fatal("UpdateMemory() create_time is nil, want non-nil")
	}
	if !updated.GetCreateTime().AsTime().Equal(created.GetCreateTime().AsTime()) {
		t.Fatalf("UpdateMemory() create_time changed: got %v, want %v", updated.GetCreateTime(), created.GetCreateTime())
	}

	// when — re-list
	listResp, err := h.ListMemories(ctx, &game.ListMemoriesRequest{Parent: "templates/saolei/sessions/session-1"})

	// then — persisted
	assertStatusCode(t, err, codes.OK)
	if len(listResp.GetMemories()) != 1 {
		t.Fatalf("ListMemories() got %d memories, want 1", len(listResp.GetMemories()))
	}
	if listResp.GetMemories()[0].GetContent() != "new content" {
		t.Fatalf("ListMemories() after update content = %q, want %q", listResp.GetMemories()[0].GetContent(), "new content")
	}
}

func TestMemoryService_UpdateMemoryValidation(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newInMemoryMemoryRepo()
	h := NewHandler(repo)

	_, err := h.CreateMemory(ctx, &game.CreateMemoryRequest{
		Parent:   "templates/saolei/sessions/session-1",
		MemoryId: "mem-1",
		Memory:   memoryBody("content"),
	})
	assertStatusCode(t, err, codes.OK)

	// when — update with an unknown FieldMask path
	_, err = h.UpdateMemory(ctx, &game.UpdateMemoryRequest{
		Memory: &game.Memory{
			Name:    "templates/saolei/sessions/session-1/memories/mem-1",
			Content: "x",
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"nonexistent_field"}},
	})

	// then — returns InvalidArgument
	assertStatusCode(t, err, codes.InvalidArgument)

	// when — update with a malformed resource name
	_, err = h.UpdateMemory(ctx, &game.UpdateMemoryRequest{
		Memory: &game.Memory{
			Name:    "templates/saolei/sessions/session-1",
			Content: "x",
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"content"}},
	})

	// then — returns InvalidArgument
	assertStatusCode(t, err, codes.InvalidArgument)
}

func TestMemoryService_UpdateMemoryNotFound(t *testing.T) {
	ctx := context.Background()

	// given
	h := NewHandler(newInMemoryMemoryRepo())

	// when — update missing memory
	_, err := h.UpdateMemory(ctx, &game.UpdateMemoryRequest{
		Memory: &game.Memory{
			Name:    "templates/saolei/sessions/session-1/memories/ghost",
			Content: "x",
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"content"}},
	})

	// then — returns NotFound
	assertStatusCode(t, err, codes.NotFound)
}

func TestMemoryService_DeleteMemory(t *testing.T) {
	ctx := context.Background()

	// given — create a memory first
	repo := newInMemoryMemoryRepo()
	h := NewHandler(repo)

	_, err := h.CreateMemory(ctx, &game.CreateMemoryRequest{
		Parent:   "templates/saolei/sessions/session-1",
		MemoryId: "to-delete",
		Memory:   memoryBody("content"),
	})
	assertStatusCode(t, err, codes.OK)

	// when — delete
	_, err = h.DeleteMemory(ctx, &game.DeleteMemoryRequest{Name: "templates/saolei/sessions/session-1/memories/to-delete"})

	// then — delete succeeds
	assertStatusCode(t, err, codes.OK)

	// when — list after delete
	listResp, err := h.ListMemories(ctx, &game.ListMemoriesRequest{Parent: "templates/saolei/sessions/session-1"})

	// then — empty
	assertStatusCode(t, err, codes.OK)
	if len(listResp.GetMemories()) != 0 {
		t.Fatalf("ListMemories() after delete got %d memories, want 0", len(listResp.GetMemories()))
	}

	// when — delete again
	_, err = h.DeleteMemory(ctx, &game.DeleteMemoryRequest{Name: "templates/saolei/sessions/session-1/memories/to-delete"})

	// then — returns NotFound
	assertStatusCode(t, err, codes.NotFound)
}

func TestMemoryService_ListMemoriesPagination(t *testing.T) {
	ctx := context.Background()

	// given
	repo := newInMemoryMemoryRepo()
	h := NewHandler(repo)

	// when — list with page_size above the maximum
	_, err := h.ListMemories(ctx, &game.ListMemoriesRequest{Parent: "templates/saolei/sessions/session-1", PageSize: 1001})

	// then — returns InvalidArgument
	assertStatusCode(t, err, codes.InvalidArgument)

	// when — list with an invalid parent
	_, err = h.ListMemories(ctx, &game.ListMemoriesRequest{Parent: "sessions/session-1"})

	// then — returns InvalidArgument
	assertStatusCode(t, err, codes.InvalidArgument)

	// when — list an empty session
	listResp, err := h.ListMemories(ctx, &game.ListMemoriesRequest{Parent: "templates/saolei/sessions/session-1"})

	// then — empty page, no next token
	assertStatusCode(t, err, codes.OK)
	if len(listResp.GetMemories()) != 0 {
		t.Fatalf("ListMemories() got %d memories, want 0", len(listResp.GetMemories()))
	}
	if listResp.GetNextPageToken() != "" {
		t.Fatalf("ListMemories() next_page_token = %q, want empty", listResp.GetNextPageToken())
	}
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

func Test_applyMemoryMask(t *testing.T) {
	tests := []struct {
		name    string
		patch   *game.Memory
		mask    *fieldmaskpb.FieldMask
		want    string
		wantErr bool
	}{
		{
			name:  "nil mask with content patch applies content",
			patch: memoryBody("new"),
			mask:  nil,
			want:  "new",
		},
		{
			name:  "empty mask paths with content patch applies content",
			patch: memoryBody("new"),
			mask:  &fieldmaskpb.FieldMask{Paths: nil},
			want:  "new",
		},
		{
			name:  "content path only",
			patch: memoryBody("new"),
			mask:  &fieldmaskpb.FieldMask{Paths: []string{"content"}},
			want:  "new",
		},
		{
			name:    "unknown path returns error",
			patch:   memoryBody("new"),
			mask:    &fieldmaskpb.FieldMask{Paths: []string{"bogus"}},
			wantErr: true,
		},
		{
			name:    "unknown path mixed with valid still errors",
			patch:   memoryBody("new"),
			mask:    &fieldmaskpb.FieldMask{Paths: []string{"content", "bogus"}},
			wantErr: true,
		},
		{
			name: "nil patch returns empty content",
			mask: &fieldmaskpb.FieldMask{Paths: []string{"content"}},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, err := applyMemoryMask(tt.patch, tt.mask)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("applyMemoryMask() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("applyMemoryMask() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("applyMemoryMask() content = %q, want %q", got, tt.want)
			}
		})
	}
}

func Test_memoryToProto(t *testing.T) {
	now := time.Date(2025, 3, 20, 8, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		memory   *domain.Memory
		wantNil  bool
		wantName string
	}{
		{
			name:    "nil memory returns nil",
			memory:  nil,
			wantNil: true,
		},
		{
			name: "memory with fields",
			memory: &domain.Memory{
				Template:   "saolei",
				SessionID:  "session-1",
				MemoryID:   "mem-1",
				Content:    "content",
				CreateTime: now,
				UpdateTime: now,
			},
			wantNil:  false,
			wantName: "templates/saolei/sessions/session-1/memories/mem-1",
		},
		{
			name: "memory with zero times has no timestamps",
			memory: &domain.Memory{
				Template:  "saolei",
				SessionID: "session-1",
				MemoryID:  "notime",
			},
			wantNil:  false,
			wantName: "templates/saolei/sessions/session-1/memories/notime",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := memoryToProto(tt.memory)

			// then
			if tt.wantNil {
				if got != nil {
					t.Fatalf("memoryToProto() = %v, want nil", got)
				}
				return
			}
			if got.GetName() != tt.wantName {
				t.Fatalf("memoryToProto() name = %q, want %q", got.GetName(), tt.wantName)
			}
			if got.GetMemoryId() != tt.memory.MemoryID {
				t.Fatalf("memoryToProto() memory_id = %q, want %q", got.GetMemoryId(), tt.memory.MemoryID)
			}
			if got.GetContent() != tt.memory.Content {
				t.Fatalf("memoryToProto() content = %q, want %q", got.GetContent(), tt.memory.Content)
			}
			if !tt.memory.CreateTime.IsZero() && got.GetCreateTime() == nil {
				t.Fatal("memoryToProto() create_time is nil, want set")
			}
			if !tt.memory.UpdateTime.IsZero() && got.GetUpdateTime() == nil {
				t.Fatal("memoryToProto() update_time is nil, want set")
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
