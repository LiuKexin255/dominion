package grpc

import (
	"time"

	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

const (
	// longLivedClientPingTime is the keepalive ping interval for long-lived
	// streaming connections (e.g. the game TeamService.Connect bidi stream).
	// MUST stay below the peer server's EnforcementPolicy.MinTime, or the
	// server sends GOAWAY with ENHANCE_YOUR_CALM/"too_many_pings" (grpc-go
	// server default MinTime is 5 minutes). Paired with
	// WithLongLivedServerKeepalive on the serving side.
	longLivedClientPingTime = 30 * time.Second
	// longLivedServerMinTime permits keepalive pings as frequent as the
	// paired client's longLivedClientPingTime (a safety margin keeps the
	// server from sending GOAWAY under jitter). Do NOT raise it above the
	// client ping interval.
	longLivedServerMinTime = 10 * time.Second
)

// WithLongLivedClientKeepalive enables keepalive pings for long-lived
// streaming connections (opt-in; the default ClientDefault does not ping).
// Timeout is deliberately omitted — grpc-go fills its 20s default
// (internal/transport/http2_client.go) — so the caller only tunes the ping
// interval. Callers MUST pair this with WithLongLivedServerKeepalive on the
// serving side to avoid ENHANCE_YOUR_CALM/"too_many_pings" GOAWAY.
func WithLongLivedClientKeepalive() grpcgo.DialOption {
	return grpcgo.WithKeepaliveParams(keepalive.ClientParameters{
		Time:                longLivedClientPingTime,
		PermitWithoutStream: true,
	})
}

// WithLongLivedServerKeepalive relaxes the server's keepalive enforcement so
// clients using WithLongLivedClientKeepalive are not torn down: grpc-go's
// server default EnforcementPolicy.MinTime is 5 minutes, which rejects any
// client pinging faster than that during idle periods of a long-lived stream
// (internal/transport/http2_server.go). Callers MUST pair this with
// WithLongLivedClientKeepalive on the client side.
func WithLongLivedServerKeepalive() grpcgo.ServerOption {
	return grpcgo.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
		MinTime:             longLivedServerMinTime,
		PermitWithoutStream: true,
	})
}
