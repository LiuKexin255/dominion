package bootstrap

import (
	"context"
	"log/slog"
	"net/http"
)

// httpServerComponent adapts an *http.Server into a bootstrap Component.
// It launches the server in a goroutine during Start and gracefully shuts
// it down during Stop.
type httpServerComponent struct {
	name   string
	server *http.Server
	done   chan error
}

// HTTPServer creates a Component that wraps the given *http.Server.
//
// Start launches server.ListenAndServe() in a background goroutine and
// returns nil immediately. Stop calls server.Shutdown(ctx).
//
// When ListenAndServe exits, a value is sent to the internal done channel:
// nil for clean shutdown (http.ErrServerClosed), or the error otherwise.
func HTTPServer(name string, server *http.Server) Component {
	return &httpServerComponent{
		name:   name,
		server: server,
		done:   make(chan error, 1),
	}
}

// Name returns the component name.
func (c *httpServerComponent) Name() string {
	return c.name
}

// Stage returns StageServer.
func (c *httpServerComponent) Stage() Stage {
	return StageServer
}

// Start launches server.ListenAndServe() in a goroutine and returns nil
// immediately. A value is always sent to the done channel when the
// goroutine exits: nil for clean shutdown, or the error otherwise.
func (c *httpServerComponent) Start(_ context.Context) error {
	go func() {
		err := c.server.ListenAndServe()
		if err == http.ErrServerClosed {
			c.done <- nil
		} else {
			slog.Error("http server exited", "component", c.name, "error", err)
			c.done <- err
		}
	}()
	slog.Info("http server started", "component", c.name)
	return nil
}

// Stop calls server.Shutdown(ctx) and returns its error.
func (c *httpServerComponent) Stop(ctx context.Context) error {
	return c.server.Shutdown(ctx)
}

// Done returns a channel that receives a value when the server exits.
// The value is nil for clean shutdown (http.ErrServerClosed) or the error otherwise.
// This satisfies the exitWatcher interface used by Bootstrap to monitor
// unexpected component exits.
func (c *httpServerComponent) Done() <-chan error {
	return c.done
}
