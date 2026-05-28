package bind_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	game "dominion/projects/game"
	"dominion/projects/game/pkg/bind"
)

// mockStream is a test double for bind.AgentFrameStream.
type mockStream struct {
	mu      sync.Mutex
	closed  bool
	recvCh  <-chan *game.AgentFrame
	sent    []*game.AgentFrame
	errCh   <-chan error
	sendErr error
}

func (m *mockStream) Recv() (*game.AgentFrame, error) {
	f, ok := <-m.recvCh
	if !ok {
		if m.errCh != nil {
			return nil, <-m.errCh
		}
		return nil, io.EOF
	}
	return f, nil
}

func (m *mockStream) Send(f *game.AgentFrame) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	if m.closed {
		return io.EOF
	}
	m.sent = append(m.sent, f)
	return nil
}

func (m *mockStream) SentFrames() []*game.AgentFrame {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*game.AgentFrame, len(m.sent))
	copy(result, m.sent)
	return result
}

// streamFixture holds test streams and their input channels.
type streamFixture struct {
	left    *mockStream
	right   *mockStream
	leftIn  chan *game.AgentFrame
	rightIn chan *game.AgentFrame
}

// newFixture creates a streamFixture. If recvErr is non-nil, both streams will
// return it from Recv() after their input channels close.
func newFixture(recvErr error) *streamFixture {
	leftIn := make(chan *game.AgentFrame, 8)
	rightIn := make(chan *game.AgentFrame, 8)

	var leftErrCh, rightErrCh chan error
	if recvErr != nil {
		leftErrCh = make(chan error, 1)
		rightErrCh = make(chan error, 1)
		leftErrCh <- recvErr
		rightErrCh <- recvErr
	}

	return &streamFixture{
		left:    &mockStream{recvCh: leftIn, errCh: leftErrCh},
		right:   &mockStream{recvCh: rightIn, errCh: rightErrCh},
		leftIn:  leftIn,
		rightIn: rightIn,
	}
}

// newMockStream creates an empty mockStream for WithFirstFrame tests.
func newMockStream() *mockStream {
	return &mockStream{
		recvCh: make(chan *game.AgentFrame, 8),
	}
}

// sendAndBind sends frames to the given input channel, starts Bind, then
// closes both channels to trigger clean shutdown. Returns the error from Bind.
func sendAndBind(t *testing.T, fx *streamFixture, b bind.Binder) error {
	t.Helper()

	// Close channels after a brief delay so recv goroutines can process buffered
	// frames before the EOF triggers report/done.
	go func() {
		time.Sleep(10 * time.Millisecond)
		close(fx.leftIn)
		close(fx.rightIn)
	}()

	return b.Bind(fx.left, fx.right)
}

func TestBind(t *testing.T) {
	// given: two streams with bidirectional forwarding
	// when: Bind is called and streams close normally
	// then: no error is returned and frames are forwarded correctly
	tests := []struct {
		name   string
		setup  func(fx *streamFixture)
		verify func(t *testing.T, fx *streamFixture)
	}{
		{
			name: "left to right forwarding",
			setup: func(fx *streamFixture) {
				fx.leftIn <- &game.AgentFrame{Type: "text", Payload: []byte("hello")}
				fx.leftIn <- &game.AgentFrame{Type: "text", Payload: []byte("hello")}
				fx.leftIn <- &game.AgentFrame{Type: "text", Payload: []byte("hello")}
			},
			verify: func(t *testing.T, fx *streamFixture) {
				sent := fx.right.SentFrames()
				if len(sent) != 3 {
					t.Fatalf("expected 3 frames forwarded to right, got %d", len(sent))
				}
			},
		},
		{
			name: "right to left forwarding",
			setup: func(fx *streamFixture) {
				fx.rightIn <- &game.AgentFrame{Type: "text", Payload: []byte("reply")}
				fx.rightIn <- &game.AgentFrame{Type: "text", Payload: []byte("reply")}
			},
			verify: func(t *testing.T, fx *streamFixture) {
				sent := fx.left.SentFrames()
				if len(sent) != 2 {
					t.Fatalf("expected 2 frames forwarded to left, got %d", len(sent))
				}
			},
		},
		{
			name: "bidirectional simultaneous forwarding",
			setup: func(fx *streamFixture) {
				fx.leftIn <- &game.AgentFrame{Type: "text", Payload: []byte("L1")}
				fx.leftIn <- &game.AgentFrame{Type: "text", Payload: []byte("L2")}
				fx.rightIn <- &game.AgentFrame{Type: "binary", Payload: []byte("R1")}
			},
			verify: func(t *testing.T, fx *streamFixture) {
				time.Sleep(20 * time.Millisecond)
				rightSent := fx.right.SentFrames()
				leftSent := fx.left.SentFrames()
				if len(rightSent) != 2 {
					t.Fatalf("expected 2 frames forwarded to right, got %d", len(rightSent))
				}
				if len(leftSent) != 1 {
					t.Fatalf("expected 1 frame forwarded to left, got %d", len(leftSent))
				}
			},
		},
		{
			name:   "left EOF is clean close",
			setup:  func(fx *streamFixture) {},
			verify: func(t *testing.T, fx *streamFixture) {},
		},
		{
			name:   "right EOF is clean close",
			setup:  func(fx *streamFixture) {},
			verify: func(t *testing.T, fx *streamFixture) {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newFixture(io.EOF)
			tt.setup(fx)

			b := bind.NewBinder()
			err := sendAndBind(t, fx, b)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Allow send goroutines to finish processing.
			time.Sleep(10 * time.Millisecond)
			tt.verify(t, fx)
		})
	}
}

func TestBind_recvReturnsCanceled_isCleanClose(t *testing.T) {
	// given: streams that return context.Canceled from Recv when closed
	// when: Bind is called and input channels close
	// then: errCh returns nil (context.Canceled is normalized)
	fx := newFixture(context.Canceled)

	b := bind.NewBinder()
	err := sendAndBind(t, fx, b)
	if err != nil {
		t.Fatalf("expected nil for context.Canceled, got: %v", err)
	}
}

func TestBind_recvReturnsError_propagatesError(t *testing.T) {
	// given: streams that return a custom error from Recv when closed
	// when: Bind is called and input channels close
	// then: errCh returns the custom error
	wantErr := errors.New("recv failure")
	fx := newFixture(wantErr)

	b := bind.NewBinder()
	err := sendAndBind(t, fx, b)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected error %v, got: %v", wantErr, err)
	}
}

func TestBind_sendReturnsError_propagatesError(t *testing.T) {
	// given: a stream whose Send returns a custom error
	// when: a frame is forwarded to that stream
	// then: errCh returns the send error (before any Recv EOF)
	sendErr := errors.New("send failure")
	fx := newFixture(io.EOF)
	fx.right.sendErr = sendErr

	// Send a frame to left, which will be forwarded to right where Send fails.
	fx.leftIn <- &game.AgentFrame{Type: "text", Payload: []byte("hello")}

	b := bind.NewBinder()

	// The send goroutine will call report(sendErr) before any Recv goroutine
	// hits EOF, so the send error should be first and propagated.
	err := b.Bind(fx.left, fx.right)
	if !errors.Is(err, sendErr) {
		t.Fatalf("expected send error %v, got: %v", sendErr, err)
	}

	// Clean up remaining goroutines.
	close(fx.leftIn)
	close(fx.rightIn)
}

func TestBind_doneFlagRespected(t *testing.T) {
	// given: one stream errors immediately, the other still produces data
	// when: the healthy stream reads data after done is set
	// then: the goroutine exits without forwarding, no deadlock
	wantErr := errors.New("left recv error")
	leftIn := make(chan *game.AgentFrame, 8)
	rightIn := make(chan *game.AgentFrame, 8)

	leftErrCh := make(chan error, 1)
	leftErrCh <- wantErr

	left := &mockStream{recvCh: leftIn, errCh: leftErrCh}
	right := &mockStream{recvCh: rightIn}

	// Trigger left error immediately by closing its input channel.
	close(leftIn)

	b := bind.NewBinder()
	err := b.Bind(left, right)

	// Wait for left recv goroutine to call report and set done.
	time.Sleep(10 * time.Millisecond)

	// Send a frame to right — it should be ignored since done is true.
	rightIn <- &game.AgentFrame{Type: "text", Payload: []byte("should not forward")}
	close(rightIn)

	if !errors.Is(err, wantErr) {
		t.Fatalf("expected error %v, got: %v", wantErr, err)
	}

	// The frame sent to right should NOT have been forwarded to left.
	time.Sleep(10 * time.Millisecond)
	if len(left.SentFrames()) > 0 {
		t.Fatalf("expected no frames forwarded to left after error, got %d", len(left.SentFrames()))
	}
}

func TestBind_concurrentErrors_firstWins(t *testing.T) {
	// given: both streams return errors when closed
	// when: both input channels close simultaneously
	// then: errCh returns one of the errors, no deadlock or panic
	wantErr := errors.New("concurrent error")
	fx := newFixture(wantErr)

	b := bind.NewBinder()
	err := sendAndBind(t, fx, b)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected error %v, got: %v", wantErr, err)
	}
}
