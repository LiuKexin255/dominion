package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"dominion/projects/game/agent/domain"
)

// mockPromptClient implements domain.PromptServiceClient for testing.
type mockPromptClient struct {
	profile       *domain.ProfileInfo
	skills        map[string]*domain.SkillInfo
	getProfileErr error
	getSkillErr   error
}

func (m *mockPromptClient) GetProfile(_ context.Context, _ string) (*domain.ProfileInfo, error) {
	if m.getProfileErr != nil {
		return nil, m.getProfileErr
	}
	return m.profile, nil
}

func (m *mockPromptClient) GetSkill(_ context.Context, _ string) (*domain.SkillInfo, error) {
	if m.getSkillErr != nil {
		return nil, m.getSkillErr
	}
	// Return the first skill in the map (single-skill tests).
	for _, sk := range m.skills {
		return sk, nil
	}
	return nil, errors.New("skill not found")
}

func TestInvokeRuntime_CreateWithProfile_ProfileNotFound(t *testing.T) {
	// given
	mock := &mockPromptClient{
		getProfileErr: errors.New("profile not found"),
	}
	rt := NewInvokeRuntime(mock)
	ctx := context.Background()

	// when
	_, err := rt.CreateWithProfile(ctx, "sess1", "nonexistent")

	// then
	if err == nil {
		t.Fatal("CreateWithProfile() expected error, got nil")
	}
}

func TestInvokeRuntime_CreateWithProfile_ProfileDisabled(t *testing.T) {
	// given
	mock := &mockPromptClient{
		profile: &domain.ProfileInfo{
			AgentProfileName: "disabled-prof",
			Enabled:          false,
		},
	}
	rt := NewInvokeRuntime(mock)
	ctx := context.Background()

	// when
	_, err := rt.CreateWithProfile(ctx, "sess1", "disabled-prof")

	// then
	if err == nil {
		t.Fatal("CreateWithProfile() expected error for disabled profile, got nil")
	}
}

func TestInvokeRuntime_CreateWithProfile_SkillMissing(t *testing.T) {
	// given
	mock := &mockPromptClient{
		profile: &domain.ProfileInfo{
			AgentProfileName: "prof",
			Enabled:          true,
			SkillNames:       []string{"missing-skill"},
		},
		getSkillErr: errors.New("skill not found"),
	}
	rt := NewInvokeRuntime(mock)
	ctx := context.Background()

	// when
	_, err := rt.CreateWithProfile(ctx, "sess1", "prof")

	// then
	if err == nil {
		t.Fatal("CreateWithProfile() expected error for missing skill, got nil")
	}
}

func TestInvokeRuntime_CreateWithProfile_MCPInvalid(t *testing.T) {
	// given
	mock := &mockPromptClient{
		profile: &domain.ProfileInfo{
			AgentProfileName: "prof",
			Enabled:          true,
			MCPNames:         []string{"non-existent-mcp"},
		},
	}
	rt := NewInvokeRuntime(mock)
	ctx := context.Background()

	// when
	_, err := rt.CreateWithProfile(ctx, "sess1", "prof")

	// then
	if err == nil {
		t.Fatal("CreateWithProfile() expected error for invalid MCP, got nil")
	}
}

func TestInvokeRuntime_ReceiveScreenshot_StartsInvoke(t *testing.T) {
	// given
	mock := &mockPromptClient{
		profile: &domain.ProfileInfo{
			AgentProfileName: "default",
			Enabled:          true,
		},
	}
	rt := NewInvokeRuntime(mock)
	ctx := context.Background()
	_, err := rt.Create(ctx, "sess1")
	if err != nil {
		t.Fatalf("Create() unexpected error: %v", err)
	}

	input := &domain.ScreenshotInput{
		SessionId: "sess1",
		CaptureId: "cap-1",
		WidthPx:   1920,
		HeightPx:  1080,
	}

	// when
	frames, err := rt.ReceiveScreenshot(ctx, "sess1", input)

	// then
	if err != nil {
		t.Fatalf("ReceiveScreenshot() unexpected error: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("ReceiveScreenshot() frame count = %d, want 2", len(frames))
	}
	if frames[0].Type != domain.FrameTypeText {
		t.Errorf("first frame type = %d, want FrameTypeText", frames[0].Type)
	}
	if frames[1].Type != domain.FrameTypeOperation {
		t.Errorf("second frame type = %d, want FrameTypeOperation", frames[1].Type)
	}
	if frames[1].XPx != 960 {
		t.Errorf("operation X = %d, want 960 (center click)", frames[1].XPx)
	}
	if frames[1].YPx != 540 {
		t.Errorf("operation Y = %d, want 540 (center click)", frames[1].YPx)
	}
	if !frames[1].IsMouse {
		t.Error("operation should be a mouse operation")
	}
	if frames[1].Button != 1 {
		t.Errorf("button = %d, want 1 (LEFT)", frames[1].Button)
	}

	// verify state transition
	status, err := rt.Status(ctx, "sess1")
	if err != nil {
		t.Fatalf("Status() unexpected error: %v", err)
	}
	if status.Status != "waiting_for_operation_result" {
		t.Errorf("status = %q, want waiting_for_operation_result", status.Status)
	}
}

func TestInvokeRuntime_ReceiveOperationResult_CompletesInvoke(t *testing.T) {
	// given
	mock := &mockPromptClient{
		profile: &domain.ProfileInfo{
			AgentProfileName: "default",
			Enabled:          true,
		},
	}
	rt := NewInvokeRuntime(mock)
	ctx := context.Background()
	_, _ = rt.Create(ctx, "sess1")

	input := &domain.ScreenshotInput{
		SessionId: "sess1",
		CaptureId: "cap-1",
		WidthPx:   1920,
		HeightPx:  1080,
	}
	frames, _ := rt.ReceiveScreenshot(ctx, "sess1", input)

	opID := frames[1].OperationID

	// when
	_, err := rt.ReceiveOperationResult(ctx, "sess1", &domain.OperationResult{
		OperationID: opID,
		Status:      operationResultExecuted,
	})

	// then
	if err != nil {
		t.Fatalf("ReceiveOperationResult() unexpected error: %v", err)
	}

	status, err := rt.Status(ctx, "sess1")
	if err != nil {
		t.Fatalf("Status() unexpected error: %v", err)
	}
	if status.Status != "invoking" {
		t.Errorf("status = %q, want invoking", status.Status)
	}
}

func TestInvokeRuntime_ReceiveOperationResult_StaleSequence(t *testing.T) {
	// given
	mock := &mockPromptClient{
		profile: &domain.ProfileInfo{
			AgentProfileName: "default",
			Enabled:          true,
		},
	}
	rt := NewInvokeRuntime(mock)
	ctx := context.Background()
	_, _ = rt.Create(ctx, "sess1")

	input := &domain.ScreenshotInput{
		SessionId: "sess1",
		CaptureId: "cap-1",
		WidthPx:   1920,
		HeightPx:  1080,
	}
	frames, _ := rt.ReceiveScreenshot(ctx, "sess1", input)
	opID := frames[1].OperationID

	// when — first result is accepted
	_, _ = rt.ReceiveOperationResult(ctx, "sess1", &domain.OperationResult{
		OperationID: opID,
		Status:      operationResultExecuted,
	})
	// when — second result with same ID is stale
	warnFrames, err := rt.ReceiveOperationResult(ctx, "sess1", &domain.OperationResult{
		OperationID: opID,
		Status:      operationResultExecuted,
	})

	// then
	if err != nil {
		t.Fatalf("ReceiveOperationResult() unexpected error for stale: %v", err)
	}
	if len(warnFrames) == 0 || warnFrames[0].Type != domain.FrameTypeWarn {
		t.Errorf("expected warn frame for stale sequence, got %v", warnFrames)
	}
	// state should still be Invoking (not Failed)
	status, err := rt.Status(ctx, "sess1")
	if err != nil {
		t.Fatalf("Status() unexpected error: %v", err)
	}
	if status.Status != "invoking" {
		t.Errorf("status = %q, want invoking (stale result should not change state)", status.Status)
	}
}

func TestInvokeRuntime_ReceiveOperationResult_WrongInvokeID(t *testing.T) {
	// given
	mock := &mockPromptClient{
		profile: &domain.ProfileInfo{
			AgentProfileName: "default",
			Enabled:          true,
		},
	}
	rt := NewInvokeRuntime(mock)
	ctx := context.Background()
	_, _ = rt.Create(ctx, "sess1")

	input := &domain.ScreenshotInput{
		SessionId: "sess1",
		CaptureId: "cap-1",
		WidthPx:   1920,
		HeightPx:  1080,
	}
	_, _ = rt.ReceiveScreenshot(ctx, "sess1", input)

	// when — send result with an operation ID from a different invoke
	_, err := rt.ReceiveOperationResult(ctx, "sess1", &domain.OperationResult{
		OperationID: "op-other-invoke-99",
		Status:      operationResultExecuted,
	})

	// then
	if err != nil {
		t.Fatalf("ReceiveOperationResult() unexpected error: %v", err)
	}
	// state should be unchanged (still waiting for operation result)
	status, err := rt.Status(ctx, "sess1")
	if err != nil {
		t.Fatalf("Status() unexpected error: %v", err)
	}
	if status.Status != "waiting_for_operation_result" {
		t.Errorf("status = %q, want waiting_for_operation_result", status.Status)
	}
}

func TestInvokeRuntime_Delete_EmptyAgent(t *testing.T) {
	// given
	mock := &mockPromptClient{
		profile: &domain.ProfileInfo{
			AgentProfileName: "default",
			Enabled:          true,
		},
	}
	rt := NewInvokeRuntime(mock)
	ctx := context.Background()

	// when — delete a non-existent agent
	err := rt.Delete(ctx, "nonexistent")

	// then — idempotent, should not error
	if err != nil {
		t.Fatalf("Delete() unexpected error for non-existent agent: %v", err)
	}
}

func TestInvokeRuntime_ReceiveOperationResult_OutOfOrderSequence(t *testing.T) {
	// given
	mock := &mockPromptClient{
		profile: &domain.ProfileInfo{
			AgentProfileName: "default",
			Enabled:          true,
		},
	}
	rt := NewInvokeRuntime(mock)
	ctx := context.Background()
	_, _ = rt.Create(ctx, "sess1")

	input := &domain.ScreenshotInput{
		SessionId: "sess1",
		CaptureId: "cap-1",
		WidthPx:   1920,
		HeightPx:  1080,
	}
	frames, _ := rt.ReceiveScreenshot(ctx, "sess1", input)
	expectedOpID := frames[1].OperationID

	// when — send result with a sequence that jumps ahead
	_, err := rt.ReceiveOperationResult(ctx, "sess1", &domain.OperationResult{
		OperationID: fmt.Sprintf("op-sess1-%d", 999),
		Status:      operationResultExecuted,
	})

	// then
	if err != nil {
		t.Fatalf("ReceiveOperationResult() unexpected error: %v", err)
	}
	// The out-of-order result should be rejected; pending operation should still
	// be accepted if sent after.
	_, err = rt.ReceiveOperationResult(ctx, "sess1", &domain.OperationResult{
		OperationID: expectedOpID,
		Status:      operationResultExecuted,
	})
	if err != nil {
		t.Fatalf("ReceiveOperationResult() unexpected error for expected op: %v", err)
	}
	status, err := rt.Status(ctx, "sess1")
	if err != nil {
		t.Fatalf("Status() unexpected error: %v", err)
	}
	if status.Status != "invoking" {
		t.Errorf("status = %q, want invoking", status.Status)
	}
}
