package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
)

// healthAddr is the fixed probe endpoint address. It listens on all
// interfaces instead of loopback because kubelet probes the Pod IP
// (specs/052-deploy-health-probe/contracts/bootstrap-health.md §1).
const (
	healthAddr = ":38080"
	healthPath = "/healthz"
	healthBody = "ok\n"
)

// logFieldPort is the log field key carrying the health endpoint address.
const logFieldPort = "port"

// healthService is the lifecycle contract RunSignal needs from the health
// server. Keeping it unexported confines health to an internal RunSignal
// step with no new public API (specs/052-deploy-health-probe/contracts/bootstrap-health.md §2).
type healthService interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// newHealthMux builds the mux serving the probe endpoint: healthPath returns
// 200 with healthBody, every other path is a 404
// (specs/052-deploy-health-probe/contracts/bootstrap-health.md §1).
func newHealthMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc(healthPath, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(healthBody))
	})
	return mux
}

// newHealthServer builds the health server consumed by RunSignal. It is a
// package variable so tests can substitute a recording stub; the real
// implementation serves healthPath on healthAddr.
var newHealthServer = func() healthService {
	return &healthServer{
		server: &http.Server{Addr: healthAddr, Handler: newHealthMux()},
	}
}

// healthServer serves the k8s health probe endpoint. It is an internal
// RunSignal step instead of a Component because the endpoint must start
// strictly after every component and stop strictly before all of them,
// which Stage+Name component ordering cannot guarantee
// (specs/052-deploy-health-probe/research.md D5).
//
// Start and Stop must be called sequentially from a single goroutine, as
// RunSignal does: the ln field has no synchronization of its own, mirroring
// the single-goroutine lifecycle convention of httpServerComponent
// (common/gopkg/bootstrap/http.go).
type healthServer struct {
	server *http.Server
	ln     net.Listener
}

// Start binds server.Addr synchronously and serves the endpoint in a
// background goroutine. The address comes from server.Addr so tests can bind
// an OS-assigned port instead of the fixed probe port; production always
// carries healthAddr. Binding happens in the caller's goroutine so a bind
// failure (e.g. the port is already taken) is returned as an error and gets
// the component-start-failure treatment: roll back and exit
// (specs/052-deploy-health-probe/spec.md FR-010).
func (h *healthServer) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", h.server.Addr)
	if err != nil {
		return fmt.Errorf("bootstrap: health server listen %s: %w", h.server.Addr, err)
	}
	h.ln = ln
	go func() {
		// Serve returns ErrServerClosed after Stop; anything else means the
		// endpoint is gone (k8s liveness will then restart the container).
		if serveErr := h.server.Serve(ln); serveErr != nil && serveErr != http.ErrServerClosed {
			logs.Error(context.Background(), "health server exited", event.String(logFieldPort, h.server.Addr), event.Err(serveErr))
		}
	}()
	logs.Info(ctx, "health server started", event.String(logFieldPort, h.server.Addr))
	return nil
}

// Stop gracefully shuts the server down, releasing the bound address. Health
// has no drain requirement, so what matters is the port being free when Stop
// returns; the error is logged here because health is not a Component and
// bypasses the bootstrap shutdown error logging.
func (h *healthServer) Stop(ctx context.Context) error {
	// server.Shutdown only closes listeners that Serve has already tracked.
	// When Stop follows Start immediately, Serve may not have run yet and the
	// port would stay bound, so the listener created in Start is closed here
	// as well. Close is idempotent: net.ErrClosed means Shutdown already
	// released it. Shutdown runs first so an Accept unblocked by this Close
	// exits as ErrServerClosed instead of an unexpected-exit error log.
	err := h.server.Shutdown(ctx)
	if h.ln != nil {
		lnErr := h.ln.Close()
		if !errors.Is(lnErr, net.ErrClosed) {
			err = errors.Join(err, lnErr)
		}
	}
	if err != nil {
		logs.Error(context.Background(), "health server stop failed", event.String(logFieldPort, h.server.Addr), event.Err(err))
		return err
	}
	logs.Info(context.Background(), "health server stopped", event.String(logFieldPort, h.server.Addr))
	return nil
}
