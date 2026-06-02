package invoke

import (
	"context"
	"errors"
	"testing"

	"dominion/projects/game/agent/domain"
)

func TestCreateWithProfile_Valid(t *testing.T) {
	// given
	rt := New(nil)
	ctx := context.Background()
	config := &domain.InvokeRuntimeConfig{
		ProfileName: "test-profile",
		Skills: []domain.SkillConfig{
			{SkillName: "skill1", Content: "content1"},
		},
		MCPNames: []string{"mcp1"},
	}

	// when
	status, err := rt.CreateWithProfile(ctx, "sess1", config)

	// then
	if err != nil {
		t.Fatalf("CreateWithProfile() unexpected error: %v", err)
	}
	if status.SessionId != "sess1" {
		t.Errorf("SessionId = %q, want sess1", status.SessionId)
	}
	if status.Status != "created" {
		t.Errorf("Status = %q, want created", status.Status)
	}
	if status.ProfileName != "test-profile" {
		t.Errorf("ProfileName = %q, want test-profile", status.ProfileName)
	}
	if status.CreateTime.IsZero() {
		t.Error("CreateTime is zero, want non-zero")
	}
}

func TestCreateWithProfile_EmptyProfileName(t *testing.T) {
	// given
	rt := New(nil)
	ctx := context.Background()
	config := &domain.InvokeRuntimeConfig{
		ProfileName: "",
	}

	// when
	_, err := rt.CreateWithProfile(ctx, "sess1", config)

	// then
	if err == nil {
		t.Fatal("CreateWithProfile() expected error for empty profile name, got nil")
	}
}

func TestInvokeRuntime_ReceiveScreenshot_StartsInvoke(t *testing.T) {
	// given
	rt := New(nil)
	ctx := context.Background()
	config := &domain.InvokeRuntimeConfig{
		ProfileName: "test-profile",
	}
	_, err := rt.CreateWithProfile(ctx, "sess1", config)
	if err != nil {
		t.Fatalf("CreateWithProfile() unexpected error: %v", err)
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

	// verify state transition — should be Invoking, not waiting_for_operation_result
	status, err := rt.Status(ctx, "sess1")
	if err != nil {
		t.Fatalf("Status() unexpected error: %v", err)
	}
	if status.Status != "invoking" {
		t.Errorf("status = %q, want invoking", status.Status)
	}
}

func TestInvokeRuntime_ReceiveScreenshot_ContinueInvoking(t *testing.T) {
	// given
	rt := New(nil)
	ctx := context.Background()
	config := &domain.InvokeRuntimeConfig{
		ProfileName: "test-profile",
	}
	_, _ = rt.CreateWithProfile(ctx, "sess1", config)

	input := &domain.ScreenshotInput{
		SessionId: "sess1",
		CaptureId: "cap-1",
		WidthPx:   1920,
		HeightPx:  1080,
	}

	// when — first screenshot starts invoke
	frames1, _ := rt.ReceiveScreenshot(ctx, "sess1", input)
	seq1 := frames1[1].Sequence

	// when — second screenshot continues invoke
	frames2, err := rt.ReceiveScreenshot(ctx, "sess1", input)

	// then
	if err != nil {
		t.Fatalf("ReceiveScreenshot() unexpected error: %v", err)
	}
	if len(frames2) != 2 {
		t.Fatalf("ReceiveScreenshot() frame count = %d, want 2", len(frames2))
	}
	seq2 := frames2[1].Sequence
	if seq2 <= seq1 {
		t.Errorf("sequence should increment: seq1=%d, seq2=%d", seq1, seq2)
	}

	// verify state is still Invoking
	status, _ := rt.Status(ctx, "sess1")
	if status.Status != "invoking" {
		t.Errorf("status = %q, want invoking", status.Status)
	}
}

func TestInvokeRuntime_ReceiveScreenshot_AgentNotFound(t *testing.T) {
	// given
	rt := New(nil)
	ctx := context.Background()

	// when
	_, err := rt.ReceiveScreenshot(ctx, "nonexistent", &domain.ScreenshotInput{})

	// then
	if err == nil {
		t.Fatal("ReceiveScreenshot() expected error for non-existent agent, got nil")
	}
}

func TestInvokeRuntime_Delete_EmptyAgent(t *testing.T) {
	// given
	rt := New(nil)
	ctx := context.Background()

	// when — delete a non-existent agent
	err := rt.Delete(ctx, "nonexistent")

	// then — idempotent, should not error
	if err != nil {
		t.Fatalf("Delete() unexpected error for non-existent agent: %v", err)
	}
}

func TestInvokeRuntime_Delete_ExistingAgent(t *testing.T) {
	// given
	rt := New(nil)
	ctx := context.Background()
	config := &domain.InvokeRuntimeConfig{
		ProfileName: "test-profile",
	}
	_, _ = rt.CreateWithProfile(ctx, "sess1", config)

	// when
	err := rt.Delete(ctx, "sess1")

	// then
	if err != nil {
		t.Fatalf("Delete() unexpected error: %v", err)
	}

	// verify agent is removed
	_, err = rt.Status(ctx, "sess1")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Status() after delete: error = %v, want ErrNotFound", err)
	}
}

func TestInvokeRuntime_Status(t *testing.T) {
	// given
	rt := New(nil)
	ctx := context.Background()
	config := &domain.InvokeRuntimeConfig{
		ProfileName: "test-profile",
	}
	_, _ = rt.CreateWithProfile(ctx, "sess1", config)

	// when
	status, err := rt.Status(ctx, "sess1")

	// then
	if err != nil {
		t.Fatalf("Status() unexpected error: %v", err)
	}
	if status.Status != "idle" {
		t.Errorf("Status = %q, want idle", status.Status)
	}
	if status.ProfileName != "test-profile" {
		t.Errorf("ProfileName = %q, want test-profile", status.ProfileName)
	}
	if status.CreateTime.IsZero() {
		t.Error("CreateTime is zero, want non-zero")
	}
}
