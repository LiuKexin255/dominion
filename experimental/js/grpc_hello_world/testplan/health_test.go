package testplan

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"dominion/common/gopkg/otel/tracecontext"
	"dominion/common/gopkg/testtool"
)

// Self-heal path observation (specs/052-deploy-health-probe/contracts/
// verification-testplan.md §2): the deploy_selfheal.yaml suite injects
// HEALTH_STOP_AFTER_MS=60000, so the service stops serving its health
// endpoint ~60s after its process starts (while the gRPC server keeps the
// process alive). The k8s liveness probe then declares the container dead
// (10s × 3 ≈ 30s, specs/052-deploy-health-probe/contracts/deploy-probe.md §2)
// and restarts it; traffic through the public gateway fails only during the
// kill+restart window, because the gRPC server itself stays up while the
// process is alive.
const (
	// envHeader routes the request into this run's deployment through the
	// shared apitest ingress — the documented test-environment routing
	// header (tools/release/deploy/README.md §环境类型) that every existing
	// testplan in the repo sends.
	envHeader = "env"
	// healthPathPrefix is the public HTTP entry of the service backend
	// (deploy_selfheal.yaml gateway PathPrefix + service gateway route).
	healthPathPrefix = "/experimental/ts/grpc-hello-world/say-hello"

	// This file is compiled into its own go_largetest binary (target
	// health_test), separate from interface_test.go (target testplan_test),
	// so the failure/recovery polling below never runs in the default suite,
	// where HEALTH_STOP_AFTER_MS is not set and no failure window would
	// ever appear.

	// probeInterval is the polling cadence. The shortest measured failure
	// window is 0.5s, which a 2s cadence can miss entirely; 1s samples even
	// the shortest window, and across failureWindow it spans several of the
	// ~90s recurring kill cycles (the stop timer re-arms on each restart).
	probeInterval = 1 * time.Second
	// initialWindow covers the first successful request after deploy READY.
	initialWindow = 60 * time.Second
	// failureWindow must span the liveness kill with margin. The stop timer
	// fires before the case starts: deploy waits for READY plus the 60s
	// settle, pushing case start past process start + HEALTH_STOP_AFTER_MS,
	// so liveness 判死 (≈30s after the health endpoint stopped,
	// deploy-probe.md §2) and the container kill+restart window are what the
	// polls observe — measured 0.5s–28s through the public gateway. The
	// timer re-arms on every container restart, so failure windows recur
	// about every 90s; the contract requires observing "≥1 failure within
	// the window" rather than at an exact instant (verification-testplan.md
	// §2 时序约束), tolerating scheduling jitter.
	failureWindow = 150 * time.Second
	// recoveryWindow covers container restart + startupProbe re-pass
	// (seconds to ~20s) plus ingress/endpoint propagation, with margin.
	recoveryWindow = 150 * time.Second
	// requestTimeout bounds a single probe request; during the kill+restart
	// window the gateway answers with 5xx quickly rather than hanging.
	requestTimeout = 5 * time.Second
)

// probeOnce sends one GET against the public entry and returns the HTTP
// status code, or -1 when the transport itself failed. Returning the raw
// outcome keeps every assertion in the test function.
func probeOnce(t *testing.T, ctx context.Context, client *http.Client, baseURL, envName, name string) int {
	t.Helper()

	reqURL := fmt.Sprintf("%s%s?name=%s", baseURL, healthPathPrefix, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		t.Fatalf("http.NewRequestWithContext(%q) unexpected error: %v", reqURL, err)
	}
	req.Header.Set(envHeader, envName)

	resp, err := client.Do(req)
	if err != nil {
		t.Logf("probe %s: transport error: %v", reqURL, err)
		return -1
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// pollForSuccess polls until a probe returns 200 or the window expires.
func pollForSuccess(t *testing.T, ctx context.Context, client *http.Client, baseURL, envName string, window time.Duration) {
	t.Helper()

	deadline := time.Now().Add(window)
	for {
		status := probeOnce(t, ctx, client, baseURL, envName, "probe")
		if status == http.StatusOK {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no success within %v (last status = %d)", window, status)
		}
		time.Sleep(probeInterval)
	}
}

// pollForFailure polls until a probe returns any non-200 outcome or the
// window expires. The contract observes the failure as "≥1 failure within
// the window" (verification-testplan.md §2), not at an exact instant.
// A transport error (-1) counts as a failure observation: through the
// black-box public gateway, "endpoint unavailable" is the failure signal in
// any form. That also admits a false positive from the test environment's
// own networking — a trade-off accepted by polling the public entry.
func pollForFailure(t *testing.T, ctx context.Context, client *http.Client, baseURL, envName string, window time.Duration) {
	t.Helper()

	deadline := time.Now().Add(window)
	for {
		status := probeOnce(t, ctx, client, baseURL, envName, "probe")
		if status != http.StatusOK {
			t.Logf("failure window observed (status = %d)", status)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no failure observed within %v — liveness restart not detected", window)
		}
		time.Sleep(probeInterval)
	}
}

// TestHealthSelfHeal observes the deployed service going through
// "serving → liveness-killed → restarted and serving again" purely through
// the public endpoint (SC-003, specs/052-deploy-health-probe/spec.md):
// the deploy-selfheal suite injects HEALTH_STOP_AFTER_MS so the container
// is restarted by the k8s liveness probe with no human intervention.
func TestHealthSelfHeal(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()

	// given: the deployment is READY (deploy step passed the startupProbe)
	// and the test carries a trace context so the traffic is correlated in
	// signoz (style/large_test.md §测试用例).
	ctx := tracecontext.FromEnv(context.Background())
	t.Logf("trace_id: %s", tracecontext.ID(ctx))
	client := &http.Client{
		Transport: tracecontext.NewHTTPTransport(http.DefaultTransport),
		Timeout:   requestTimeout,
	}

	// when/then: initial success right after READY.
	pollForSuccess(t, ctx, client, sutHostURL, sutEnvName, initialWindow)

	// then: at least one failure once HEALTH_STOP_AFTER_MS elapsed and the
	// liveness probe declared the container dead.
	pollForFailure(t, ctx, client, sutHostURL, sutEnvName, failureWindow)

	// then: the container restarted and serves again.
	pollForSuccess(t, ctx, client, sutHostURL, sutEnvName, recoveryWindow)
}
