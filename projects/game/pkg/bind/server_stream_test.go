package bind

import (
	"context"
	"errors"
	"io"
	"testing"
)

// fakeUpstream is a configurable UpstreamStream for the pump tests: the
// response half of a server-streaming call whose request was already sent
// by the stream-open call.
type fakeUpstream struct {
	frames  []string
	recvErr error

	recvCalled bool
}

func (f *fakeUpstream) Recv() (*string, error) {
	f.recvCalled = true
	if len(f.frames) > 0 {
		frame := f.frames[0]
		f.frames = f.frames[1:]
		return &frame, nil
	}
	return nil, f.recvErr
}

// fakeDownstream is a configurable ServerSendStream for the pump tests.
type fakeDownstream struct {
	frames []string
	err    error

	sendCalls int
}

func (f *fakeDownstream) Send(resp *string) error {
	f.sendCalls++
	if f.err != nil {
		return f.err
	}
	f.frames = append(f.frames, *resp)
	return nil
}

// TestServerStreamBinder_BindServerStream drives the pump's outcome branches
// (given/when/then per case, style/golang.md §单元测试):
//
//  1. clean finish: all frames relayed in order, io.EOF → nil
//  2. upstream error passthrough: a non-EOF Recv error is returned unchanged
//  3. downstream disconnect normalization: context.Canceled from the shared
//     call context surfaces on Recv → nil
//  4. downstream send failure passthrough: the downstream Send error is
//     returned unchanged and stops the pump
func TestServerStreamBinder_BindServerStream(t *testing.T) {
	boom := errors.New("upstream recv boom")
	downBoom := errors.New("downstream send boom")

	tests := []struct {
		name       string
		upstream   *fakeUpstream
		downstream *fakeDownstream
		wantErr    error
		wantFrames []string
	}{
		{
			name: "clean finish",
			upstream: &fakeUpstream{
				frames:  []string{"evt-1", "evt-2"},
				recvErr: io.EOF,
			},
			downstream: &fakeDownstream{},
			wantErr:    nil,
			wantFrames: []string{"evt-1", "evt-2"},
		},
		{
			name:       "upstream recv error passthrough",
			upstream:   &fakeUpstream{recvErr: boom},
			downstream: &fakeDownstream{},
			wantErr:    boom,
			wantFrames: nil,
		},
		{
			name:       "downstream disconnect normalized",
			upstream:   &fakeUpstream{recvErr: context.Canceled},
			downstream: &fakeDownstream{},
			wantErr:    nil,
			wantFrames: nil,
		},
		{
			name: "downstream send failure passthrough",
			upstream: &fakeUpstream{
				frames:  []string{"evt-1", "evt-2"},
				recvErr: io.EOF,
			},
			downstream: &fakeDownstream{err: downBoom},
			wantErr:    downBoom,
			wantFrames: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binder := NewServerStreamBinder[string]()

			err := binder.BindServerStream(tt.downstream, tt.upstream)

			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("BindServerStream() error = %v, want nil", err)
				}
			} else if !errors.Is(err, tt.wantErr) {
				t.Fatalf("BindServerStream() error = %v, want %v", err, tt.wantErr)
			}

			if got, want := len(tt.downstream.frames), len(tt.wantFrames); got != want {
				t.Fatalf("relayed frames = %v, want %v", tt.downstream.frames, tt.wantFrames)
			}
			for i, want := range tt.wantFrames {
				if tt.downstream.frames[i] != want {
					t.Fatalf("relayed frames[%d] = %q, want %q", i, tt.downstream.frames[i], want)
				}
			}

			// The pump always starts from the upstream Recv.
			if !tt.upstream.recvCalled {
				t.Fatal("upstream.Recv was not called")
			}
			// A failing downstream Send stops the pump immediately.
			if tt.downstream.err != nil {
				if tt.downstream.sendCalls != 1 {
					t.Fatalf("downstream Send calls = %d, want 1 (pump stops at the failure)", tt.downstream.sendCalls)
				}
			}
		})
	}
}
