package stream

import (
	"context"
	"io"
	"sync"
	"testing"

	game "dominion/projects/game"
)

type mockStream struct {
	mu     sync.Mutex
	closed bool
	recvCh <-chan *game.AgentFrame
	sent   []*game.AgentFrame
	errCh  <-chan error
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

type ctxAwareStream struct {
	inner *mockStream
	ctx   context.Context
}

func (c *ctxAwareStream) Recv() (*game.AgentFrame, error) {
	type result struct {
		frame *game.AgentFrame
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		f, err := c.inner.Recv()
		ch <- result{f, err}
	}()
	select {
	case r := <-ch:
		return r.frame, r.err
	case <-c.ctx.Done():
		return nil, c.ctx.Err()
	}
}

func (c *ctxAwareStream) Send(f *game.AgentFrame) error {
	return c.inner.Send(f)
}

type streamFixture struct {
	left    *mockStream
	right   *mockStream
	leftIn  chan *game.AgentFrame
	rightIn chan *game.AgentFrame
}

func newFixture(err error) *streamFixture {
	leftIn := make(chan *game.AgentFrame, 8)
	rightIn := make(chan *game.AgentFrame, 8)

	var leftErrCh, rightErrCh chan error
	if err != nil {
		leftErrCh = make(chan error, 1)
		rightErrCh = make(chan error, 1)
		leftErrCh <- err
		rightErrCh <- err
	}

	return &streamFixture{
		left: &mockStream{
			recvCh: leftIn,
			errCh:  leftErrCh,
		},
		right: &mockStream{
			recvCh: rightIn,
			errCh:  rightErrCh,
		},
		leftIn:  leftIn,
		rightIn: rightIn,
	}
}

func TestBind(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
		setup   func(t *testing.T) (fx *streamFixture, cancel context.CancelFunc)
		verify  func(t *testing.T, fx *streamFixture)
	}{
		{
			name:    "left to right forwarding",
			wantErr: false,
			setup: func(t *testing.T) (*streamFixture, context.CancelFunc) {
				fx := newFixture(io.EOF)
				_, cancel := context.WithCancel(context.Background())
				go func() {
					fx.leftIn <- &game.AgentFrame{Type: "text", Payload: []byte("hello")}
					fx.leftIn <- &game.AgentFrame{Type: "text", Payload: []byte("hello")}
					fx.leftIn <- &game.AgentFrame{Type: "text", Payload: []byte("hello")}
					close(fx.leftIn)
					close(fx.rightIn)
				}()
				return fx, cancel
			},
			verify: func(t *testing.T, fx *streamFixture) {
				sent := fx.right.SentFrames()
				if len(sent) != 3 {
					t.Fatalf("expected 3 frames forwarded to right, got %d", len(sent))
				}
			},
		},
		{
			name:    "right to left forwarding",
			wantErr: false,
			setup: func(t *testing.T) (*streamFixture, context.CancelFunc) {
				fx := newFixture(io.EOF)
				_, cancel := context.WithCancel(context.Background())
				go func() {
					fx.rightIn <- &game.AgentFrame{Type: "text", Payload: []byte("reply")}
					fx.rightIn <- &game.AgentFrame{Type: "text", Payload: []byte("reply")}
					close(fx.rightIn)
					close(fx.leftIn)
				}()
				return fx, cancel
			},
			verify: func(t *testing.T, fx *streamFixture) {
				sent := fx.left.SentFrames()
				if len(sent) != 2 {
					t.Fatalf("expected 2 frames forwarded to left, got %d", len(sent))
				}
			},
		},
		{
			name:    "left EOF is clean close",
			wantErr: false,
			setup: func(t *testing.T) (*streamFixture, context.CancelFunc) {
				fx := newFixture(io.EOF)
				_, cancel := context.WithCancel(context.Background())
				go func() {
					close(fx.leftIn)
					close(fx.rightIn)
				}()
				return fx, cancel
			},
			verify: func(t *testing.T, fx *streamFixture) {},
		},
		{
			name:    "right EOF is clean close",
			wantErr: false,
			setup: func(t *testing.T) (*streamFixture, context.CancelFunc) {
				fx := newFixture(io.EOF)
				_, cancel := context.WithCancel(context.Background())
				go func() {
					close(fx.rightIn)
					close(fx.leftIn)
				}()
				return fx, cancel
			},
			verify: func(t *testing.T, fx *streamFixture) {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx, cancel := tt.setup(t)
			defer cancel()

			b := NewBinder()
			err := b.Bind(context.Background(), fx.left, fx.right)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			tt.verify(t, fx)
		})
	}
}

func TestBind_contextCancellation(t *testing.T) {
	fx := newFixture(nil)
	ctx, cancel := context.WithCancel(context.Background())

	ctxLeft := &ctxAwareStream{inner: fx.left, ctx: ctx}
	ctxRight := &ctxAwareStream{inner: fx.right, ctx: ctx}

	errCh := make(chan error, 1)
	go func() {
		b := NewBinder()
		errCh <- b.Bind(ctx, ctxLeft, ctxRight)
	}()

	cancel()

	err := <-errCh
	if err != nil {
		t.Fatalf("expected nil on context cancellation, got: %v", err)
	}

	close(fx.leftIn)
	close(fx.rightIn)
}

func TestBind_bidirectionalForwarding(t *testing.T) {
	fx := newFixture(io.EOF)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		fx.leftIn <- &game.AgentFrame{Type: "text", Payload: []byte("L1")}
		fx.leftIn <- &game.AgentFrame{Type: "text", Payload: []byte("L2")}
		close(fx.leftIn)
	}()

	go func() {
		fx.rightIn <- &game.AgentFrame{Type: "binary", Payload: []byte("R1")}
		close(fx.rightIn)
	}()

	b := NewBinder()
	err := b.Bind(ctx, fx.left, fx.right)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rightSent := fx.right.SentFrames()
	leftSent := fx.left.SentFrames()
	if len(rightSent) != 2 {
		t.Fatalf("expected 2 frames forwarded to right, got %d", len(rightSent))
	}
	if len(leftSent) != 1 {
		t.Fatalf("expected 1 frame forwarded to left, got %d", len(leftSent))
	}
}
