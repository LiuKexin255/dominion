package bootstrap

import (
	"context"
	"net"

	"google.golang.org/grpc"
)

// callGracefulStop performs a graceful stop on the gRPC server. Package-level
// variable allows tests to override with a stub.
var callGracefulStop = func(s *grpc.Server) { s.GracefulStop() }

// callForceStop performs an immediate stop on the gRPC server. Package-level
// variable allows tests to override with a stub.
var callForceStop = func(s *grpc.Server) { s.Stop() }

// grpcConnComponent adapts a gRPC client connection into a bootstrap Component.
type grpcConnComponent struct {
	name string
	conn *grpc.ClientConn
}

// GRPCConn returns a client-stage Component that wraps an existing gRPC
// ClientConn. Start is a no-op (the connection is already established) and
// Stop closes the underlying ClientConn.
func GRPCConn(name string, conn *grpc.ClientConn) Component {
	return &grpcConnComponent{
		name: name,
		conn: conn,
	}
}

// Name returns the component name.
func (c *grpcConnComponent) Name() string {
	return c.name
}

// Stage returns StageClient because a gRPC client connection belongs to the
// client lifecycle stage.
func (c *grpcConnComponent) Stage() Stage {
	return StageClient
}

// Start is a no-op for client connections (the dial is done externally).
func (c *grpcConnComponent) Start(_ context.Context) error {
	return nil
}

// Stop closes the underlying gRPC ClientConn.
func (c *grpcConnComponent) Stop(_ context.Context) error {
	return c.conn.Close()
}

// grpcServerComponent adapts a gRPC server into a bootstrap Component.
type grpcServerComponent struct {
	name     string
	server   *grpc.Server
	listener net.Listener
	done     chan error
}

// GRPCServer returns a server-stage Component that wraps a gRPC Server and
// its Listener. Start launches server.Serve in a background goroutine and
// Stop performs a two-phase shutdown: GracefulStop with context deadline,
// falling back to Stop if the context expires.
func GRPCServer(name string, server *grpc.Server, listener net.Listener) Component {
	return &grpcServerComponent{
		name:     name,
		server:   server,
		listener: listener,
		done:     make(chan error, 1),
	}
}

// Name returns the component name.
func (c *grpcServerComponent) Name() string {
	return c.name
}

// Stage returns StageServer because a gRPC server belongs to the server
// lifecycle stage.
func (c *grpcServerComponent) Stage() Stage {
	return StageServer
}

// Start launches server.Serve in a background goroutine and returns
// immediately. The result of Serve (nil or error) is pushed to the done
// channel so the monitor goroutine never blocks.
func (c *grpcServerComponent) Start(_ context.Context) error {
	go func() {
		c.done <- c.server.Serve(c.listener)
	}()
	return nil
}

// Stop performs a two-phase shutdown of the gRPC server. It first attempts
// GracefulStop in a goroutine. If the context expires before GracefulStop
// completes, it falls back to a forced Stop.
func (c *grpcServerComponent) Stop(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		callGracefulStop(c.server)
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		callForceStop(c.server)
		return ctx.Err()
	}
}

// Done returns a channel that receives the error result when the gRPC
// server exits. This satisfies the exitWatcher interface used by Bootstrap
// to monitor unexpected component exits.
func (c *grpcServerComponent) Done() <-chan error {
	return c.done
}
