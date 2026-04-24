package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"dominion/pkg/grpc"
	"dominion/projects/game/gateway"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	grpcgo "google.golang.org/grpc"
)

const shutdownTimeout = 5 * time.Second

func Serve(ctx context.Context, handler *gateway.Handler, wsHandler *gateway.WebSocketHandler, httpAddr string) error {
	grpcServer := grpcgo.NewServer(grpc.ServiceDefault()...)
	gateway.RegisterGameGatewayServiceServer(grpcServer, handler)

	httpMux := runtime.NewServeMux()
	if err := gateway.RegisterGameGatewayServiceHandlerServer(ctx, httpMux, handler); err != nil {
		return fmt.Errorf("register HTTP gateway: %w", err)
	}

	router := &gatewayRouter{
		wsHandler: wsHandler,
		grpcMux:   httpMux,
	}

	httpServer := &http.Server{Addr: httpAddr, Handler: router}

	errCh := make(chan error, 2)
	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("shutdown HTTP gateway: %w", err)
		}
	}()

	go func() {
		errCh <- httpServer.ListenAndServe()
	}()

	for i := 0; i < cap(errCh); i++ {
		if err := <-errCh; err != nil {
			return err
		}
	}

	return nil
}

// gatewayRouter routes WebSocket paths to wsHandler and all other paths to
// the grpc-gateway mux.
type gatewayRouter struct {
	wsHandler http.Handler
	grpcMux   http.Handler
}

func (r *gatewayRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if isWebSocketPath(req.URL.Path) {
		r.wsHandler.ServeHTTP(w, req)
		return
	}
	r.grpcMux.ServeHTTP(w, req)
}

func isWebSocketPath(path string) bool {
	return strings.HasPrefix(path, "/v1/sessions/") && strings.HasSuffix(path, "/game/connect")
}
