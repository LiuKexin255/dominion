package bootstrap

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestHTTPServer_StageIsServer(t *testing.T) {
	srv := &http.Server{}
	c := HTTPServer("test-http", srv)

	if got := c.Stage(); got != StageServer {
		t.Fatalf("Stage() = %v, want %v", got, StageServer)
	}
}

func TestHTTPServer_StartLaunchesListenAndServe(t *testing.T) {
	// given: a real HTTP server on a random port.
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	}
	c := HTTPServer("test-http", srv)

	// when: Start is called. The adapter uses ListenAndServe which creates
	// its own listener, so we close ours and let ListenAndServe bind to a
	// new random port. Instead, we use Serve with our listener for testing.
	//
	// To test with ListenAndServe, we set srv.Addr to a random port.
	// Close the pre-created listener and set Addr to ":0".
	listener.Close()
	srv.Addr = ":0"

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// then: give the goroutine time to start listening, then verify server
	// is reachable by connecting to it.
	// Since we used ":0", we need to check srv.Addr after ListenAndServe
	// starts. Instead, we just verify the done channel has not received
	// an error after a brief wait.
	time.Sleep(50 * time.Millisecond)

	select {
	case err := <-c.(*httpServerComponent).done:
		t.Fatalf("unexpected error from done channel: %v", err)
	default:
		// No error — server is running.
	}

	// Cleanup.
	_ = c.Stop(context.Background())
}

func TestHTTPServer_StopCallsShutdown(t *testing.T) {
	// given: a running HTTP server on a random port.
	srv := &http.Server{
		Addr: ":0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	}
	c := HTTPServer("test-http", srv)

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// when: Stop is called.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := c.Stop(ctx)

	// then: Shutdown returns nil or http.ErrServerClosed — both are acceptable
	// because the server was successfully shut down.
	if err != nil && err != http.ErrServerClosed {
		t.Fatalf("Stop() error: %v", err)
	}
}

func TestHTTPServer_ErrServerClosedAfterShutdown(t *testing.T) {
	// given: an HTTP server that will be shut down, causing ListenAndServe
	// to return http.ErrServerClosed.
	srv := &http.Server{
		Addr:    ":0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}),
	}
	c := HTTPServer("test-http", srv)

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// when: server is shut down, ListenAndServe returns ErrServerClosed.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = c.Stop(ctx)

	time.Sleep(50 * time.Millisecond)

	select {
	case err := <-c.(*httpServerComponent).done:
		if err != nil {
			t.Fatalf("expected nil for clean shutdown, got: %v", err)
		}
	default:
		t.Fatalf("expected signal on done channel after clean shutdown, got nothing")
	}
}

func TestHTTPServer_StartedLog(t *testing.T) {
	buf := captureSlog(t)
	srv := &http.Server{
		Addr:    ":0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}),
	}
	c := HTTPServer("log-http", srv)

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	output := buf.String()
	if !strings.Contains(output, "http server started") {
		t.Fatal("expected 'http server started' in log output")
	}
	if !strings.Contains(output, "component=log-http") {
		t.Fatal("expected component=log-http in log output")
	}

	_ = c.Stop(context.Background())
}

func TestHTTPServer_ExitErrorLog(t *testing.T) {
	buf := captureSlog(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	srv := &http.Server{Addr: ln.Addr().String()}
	c := HTTPServer("log-http-err", srv)

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	select {
	case err := <-c.(*httpServerComponent).done:
		if err == nil {
			t.Fatal("expected error from duplicate bind, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ListenAndServe error")
	}

	output := buf.String()
	if !strings.Contains(output, "http server exited") {
		t.Fatal("expected 'http server exited' in log output")
	}
	if !strings.Contains(output, "component=log-http-err") {
		t.Fatal("expected component=log-http-err in log output")
	}
}
