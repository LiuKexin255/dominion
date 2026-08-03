package bind_test

import (
	"io"
	"testing"

	game "dominion/projects/game"
	"dominion/projects/game/pkg/bind"
)

// testUserStream wraps a mockUserStream with a bidirectional input channel
// for tests that need to push frames into the stream.
type testUserStream struct {
	*mockUserStream
	in chan *game.UserFrame
}

func newTestUserStream() *testUserStream {
	ch := make(chan *game.UserFrame, 8)
	return &testUserStream{
		mockUserStream: &mockUserStream{recvCh: ch},
		in:             ch,
	}
}

func TestWithFirstFrame_FirstRecvReturnsFirst(t *testing.T) {
	first := &game.UserFrame{
		SessionId: "first",
		Payload: &game.UserFrame_FlowParts{FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
			{Kind: &game.FlowPart_Status{Status: &game.StatusSignal{Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE}}},
		}}},
	}
	inner := newTestUserStream()
	wrapped := bind.WithFirstFrame(inner, first)

	frame, err := wrapped.Recv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if frame.SessionId != "first" {
		t.Fatalf("expected first frame SessionId 'first', got %q", frame.SessionId)
	}
}

func TestWithFirstFrame_SecondRecvDelegates(t *testing.T) {
	first := &game.UserFrame{SessionId: "first"}
	inner := newTestUserStream()
	wrapped := bind.WithFirstFrame(inner, first)

	// First recv returns first
	wrapped.Recv()

	// Second recv should come from inner
	inner.in <- &game.UserFrame{SessionId: "second"}
	frame, err := wrapped.Recv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if frame.SessionId != "second" {
		t.Fatalf("expected 'second', got %q", frame.SessionId)
	}
}

func TestWithFirstFrame_SendDelegates(t *testing.T) {
	first := &game.UserFrame{SessionId: "first"}
	inner := newTestUserStream()
	wrapped := bind.WithFirstFrame(inner, first)

	frame := &game.TeamFrame{SessionId: "sent"}
	err := wrapped.Send(frame)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inner.SentFrames()) != 1 {
		t.Fatalf("expected 1 sent frame, got %d", len(inner.SentFrames()))
	}
}

func TestWithFirstFrame_NilFirstPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil first frame")
		}
	}()
	inner := newTestUserStream()
	bind.WithFirstFrame(inner, nil)
}

func TestWithFirstFrame_EOFAfterFirst(t *testing.T) {
	first := &game.UserFrame{SessionId: "first"}
	inner := newTestUserStream()
	wrapped := bind.WithFirstFrame(inner, first)

	// First recv returns first
	wrapped.Recv()
	// Close inner channel to get EOF
	close(inner.in)

	_, err := wrapped.Recv()
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}
