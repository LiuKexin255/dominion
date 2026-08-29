package bind

import (
	"context"
	"errors"
	"io"
)

// ServerSendStream is the downstream half of a server-streaming forward: it
// emits response frames to the original caller (grpc-go's generated
// XxxServer stream interfaces satisfy this structurally).
type ServerSendStream[Resp any] interface {
	Send(*Resp) error
}

// UpstreamStream is the upstream half: the response stream of a
// server-streaming call whose request was already sent and half-closed by
// the stream-open call (grpc's ServerStreamingClient[Resp] satisfies this
// structurally).
type UpstreamStream[Resp any] interface {
	Recv() (*Resp, error)
}

// ServerStreamBinder forwards one server-streaming RPC's response half:
// relay response frames downstream until the upstream ends.
type ServerStreamBinder[Resp any] interface {
	BindServerStream(downstream ServerSendStream[Resp], upstream UpstreamStream[Resp]) error
}

// serverStreamBinder implements ServerStreamBinder.
type serverStreamBinder[Resp any] struct{}

// NewServerStreamBinder creates a new ServerStreamBinder instance.
func NewServerStreamBinder[Resp any]() ServerStreamBinder[Resp] {
	return new(serverStreamBinder[Resp])
}

// BindServerStream pumps one server-streaming call's response half: it
// relays every response frame downstream until the upstream ends. The
// request's send and half-close are the caller's business, carried by the
// stream-open call (protoc-gen-go-grpc v1.5.1 emits Send(ctx, in) as
// SendMsg(in) + CloseSend() on open).
//
// io.EOF and context.Canceled are treated as clean closes (nil), matching the
// v1 Binder.Bind report semantics (projects/game/pkg/bind/binder.go); every
// other error is returned unchanged so the caller can forward its gRPC status.
// A downstream disconnect rides the shared call context: the cancellation
// surfaces as context.Canceled on the upstream Recv, which normalizes to nil.
func (b *serverStreamBinder[Resp]) BindServerStream(downstream ServerSendStream[Resp], upstream UpstreamStream[Resp]) error {
	for {
		resp, err := upstream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
		if err := downstream.Send(resp); err != nil {
			return err
		}
	}
}
