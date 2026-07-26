package bind_test

import (
	"io"
	"testing"

	game "dominion/projects/game"
	"dominion/projects/game/pkg/bind"
)

// testStream wraps a mockStream with a bidirectional input channel for tests
// that need to push frames into the stream.
type testStream struct {
	*mockStream
	in chan *game.AgentFrame
}

func newTestStream() *testStream {
	ch := make(chan *game.AgentFrame, 8)
	return &testStream{
		mockStream: &mockStream{recvCh: ch},
		in:         ch,
	}
}

func TestWithFirstFrame_FirstRecvReturnsFirst(t *testing.T) {
	first := &game.AgentFrame{
		SessionId: "first",
		Payload: &game.AgentFrame_FlowParts{FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
			{Kind: &game.FlowPart_Status{Status: &game.StatusSignal{Status: game.StatusSignalStatus_STATUS_SIGNAL_STATUS_IDLE}}},
		}}},
	}
	inner := newTestStream()
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
	first := &game.AgentFrame{SessionId: "first"}
	inner := newTestStream()
	wrapped := bind.WithFirstFrame(inner, first)

	// First recv returns first
	wrapped.Recv()

	// Second recv should come from inner
	inner.in <- &game.AgentFrame{SessionId: "second"}
	frame, err := wrapped.Recv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if frame.SessionId != "second" {
		t.Fatalf("expected 'second', got %q", frame.SessionId)
	}
}

func TestWithFirstFrame_SendDelegates(t *testing.T) {
	first := &game.AgentFrame{SessionId: "first"}
	inner := newTestStream()
	wrapped := bind.WithFirstFrame(inner, first)

	frame := &game.AgentFrame{SessionId: "sent"}
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
	inner := newTestStream()
	bind.WithFirstFrame(inner, nil)
}

func TestWithFirstFrame_EOFAfterFirst(t *testing.T) {
	first := &game.AgentFrame{SessionId: "first"}
	inner := newTestStream()
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
