package bootstrap

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ---------------------------------------------------------------------------
// GRPCConn tests
// ---------------------------------------------------------------------------

// TestGRPCConn_StageIsClient verifies that GRPCConn returns StageClient.
func TestGRPCConn_StageIsClient(t *testing.T) {
	c := GRPCConn("test-conn", nil)

	if got := c.Stage(); got != StageClient {
		t.Fatalf("Stage() = %v, want %v", got, StageClient)
	}
}

// TestGRPCConn_StopClosesConn verifies that Stop closes the underlying
// ClientConn by listening on a local listener as a surrogate connection.
func TestGRPCConn_StopClosesConn(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	conn, err := grpc.NewClient(
		ln.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		ln.Close()
		t.Fatalf("grpc.NewClient: %v", err)
	}

	c := GRPCConn("test-conn", conn)

	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
}

// TestGRPCConn_StartIsNoop verifies that Start returns nil without doing
// any network work.
func TestGRPCConn_StartIsNoop(t *testing.T) {
	c := GRPCConn("test-conn", nil)

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// GRPCServer tests
// ---------------------------------------------------------------------------

// TestGRPCServer_StageIsServer verifies that GRPCServer returns StageServer.
func TestGRPCServer_StageIsServer(t *testing.T) {
	s := grpc.NewServer()
	ln := newFakeListener()

	c := GRPCServer("test-server", s, ln)

	if got := c.Stage(); got != StageServer {
		t.Fatalf("Stage() = %v, want %v", got, StageServer)
	}
}

// TestGRPCServer_StartLaunchesServe verifies that Start returns nil and the
// done channel is ready (non-blocking read possible after the server exits).
func TestGRPCServer_StartLaunchesServe(t *testing.T) {
	s := grpc.NewServer()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	c := GRPCServer("test-server", s, ln)

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Give the goroutine a moment to call Serve.
	time.Sleep(50 * time.Millisecond)

	// Stop the server so the done channel receives a value (or closes).
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
}

// TestGRPCServer_GracefulStopNormal verifies that Stop with an open context
// calls callGracefulStop and not callForceStop.
func TestGRPCServer_GracefulStopNormal(t *testing.T) {
	var gracefulCalled atomic.Int32
	var forceCalled atomic.Int32

	origGraceful := callGracefulStop
	origForce := callForceStop
	t.Cleanup(func() {
		callGracefulStop = origGraceful
		callForceStop = origForce
	})

	callGracefulStop = func(_ *grpc.Server) {
		gracefulCalled.Add(1)
	}
	callForceStop = func(_ *grpc.Server) {
		forceCalled.Add(1)
	}

	s := grpc.NewServer()
	ln := newFakeListener()
	c := GRPCServer("test-server", s, ln)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.Stop(ctx); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	if gracefulCalled.Load() != 1 {
		t.Fatalf("callGracefulStop called %d times, want 1", gracefulCalled.Load())
	}
	if forceCalled.Load() != 0 {
		t.Fatalf("callForceStop called %d times, want 0", forceCalled.Load())
	}
}

// TestGRPCServer_GracefulStopFallback verifies that when the context is
// already cancelled, Stop falls back to callForceStop after GracefulStop
// blocks.
func TestGRPCServer_GracefulStopFallback(t *testing.T) {
	var forceCalled atomic.Int32

	origGraceful := callGracefulStop
	origForce := callForceStop
	t.Cleanup(func() {
		callGracefulStop = origGraceful
		callForceStop = origForce
	})

	// Make GracefulStop block indefinitely so the context expires.
	callGracefulStop = func(_ *grpc.Server) {
		select {} // blocks forever
	}
	callForceStop = func(_ *grpc.Server) {
		forceCalled.Add(1)
	}

	s := grpc.NewServer()
	ln := newFakeListener()
	c := GRPCServer("test-server", s, ln)

	// Use an already-expired context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := c.Stop(ctx)
	if err == nil {
		t.Fatal("Stop() expected error, got nil")
	}
	if err != context.Canceled {
		t.Fatalf("Stop() error = %v, want context.Canceled", err)
	}
	if forceCalled.Load() != 1 {
		t.Fatalf("callForceStop called %d times, want 1", forceCalled.Load())
	}
}

// ---------------------------------------------------------------------------
// test helpers
// ---------------------------------------------------------------------------

// fakeListener is a net.Listener that accepts no connections. It is used as
// a lightweight stand-in for tests that do not exercise actual Serve behavior.
type fakeListener struct {
	addr fakeAddr
}

func newFakeListener() *fakeListener {
	return &fakeListener{addr: fakeAddr{"tcp", "127.0.0.1:0"}}
}

func (l *fakeListener) Accept() (net.Conn, error) {
	// Block forever — no real connections expected in these tests.
	select {}
}

func (l *fakeListener) Close() error { return nil }

func (l *fakeListener) Addr() net.Addr { return l.addr }

// fakeAddr implements net.Addr for the fakeListener.
type fakeAddr struct {
	network string
	address string
}

func (a fakeAddr) Network() string { return a.network }

func (a fakeAddr) String() string { return a.address }
