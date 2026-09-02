// The fake-desktop command runs the deterministic desktop executor used by
// the game testplan (spec specs/051-agent-v2-dsh-migration/research.md D15):
// it dials the gateway /api/v2 flow WebSocket for one session and replies to
// operation frames with recognizer-verifiable screenshots. It is referenced
// only by testplan deploys, never by the production game deployment.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"dominion/common/gopkg/bootstrap"
	phttp "dominion/common/gopkg/http"
	"dominion/common/gopkg/otel"
	"dominion/common/gopkg/solver"
	"dominion/projects/game/fake-desktop/service"
)

var port = flag.String("port", "8080", "Port for the health endpoint")

// Environment variables (injected by the testplan deploy):
//
//	FAKE_DESKTOP_GATEWAY_URL — gateway base URL, either a plain
//	                         http://host:port URL or a Dominion target
//	                         (dominion:///game/gateway:80) resolved through
//	                         the Dominion service registry
//	FAKE_DESKTOP_TEMPLATE    — flow template (default saolei)
//	FAKE_DESKTOP_SESSION     — flow session resource id (required)
//	FAKE_DESKTOP_SCENARIO    — won (default) | lost | progressive
//	FAKE_DESKTOP_FAULT_DISCONNECT_AFTER_OPS — close the connection after N receipts
//	FAKE_DESKTOP_FAULT_OMIT_SCREENSHOT      — reply without screenshots
//	FAKE_DESKTOP_FAULT_FAILED_STATUS        — reply FAILED to operations
func main() {
	flag.Parse()

	gatewayURL, err := resolveGatewayURL(os.Getenv("FAKE_DESKTOP_GATEWAY_URL"))
	if err != nil {
		log.Fatalf("resolve FAKE_DESKTOP_GATEWAY_URL: %v", err)
	}

	cfg := service.Config{
		GatewayURL: gatewayURL,
		Template:   envOr("FAKE_DESKTOP_TEMPLATE", "saolei"),
		Session:    os.Getenv("FAKE_DESKTOP_SESSION"),
		Scenario:   envOr("FAKE_DESKTOP_SCENARIO", "won"),
		Fault: service.Fault{
			DisconnectAfterOps: atoiDefault(os.Getenv("FAKE_DESKTOP_FAULT_DISCONNECT_AFTER_OPS")),
			OmitScreenshot:     os.Getenv("FAKE_DESKTOP_FAULT_OMIT_SCREENSHOT") == "1",
			ForceFailedStatus:  os.Getenv("FAKE_DESKTOP_FAULT_FAILED_STATUS") == "1",
		},
	}
	if cfg.GatewayURL == "" || cfg.Session == "" {
		log.Fatal("FAKE_DESKTOP_GATEWAY_URL and FAKE_DESKTOP_SESSION are required")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	srv := &http.Server{
		Addr:    ":" + *port,
		Handler: phttp.Handler(mux, "fake-desktop"),
	}

	log.Printf("fake-desktop dialing %s session %s/%s (scenario %s)",
		cfg.GatewayURL, cfg.Template, cfg.Session, cfg.Scenario)

	b := bootstrap.New()
	b.Register(otel.Component())
	b.Register(bootstrap.HTTPServer("http", srv))
	b.Register(service.Component(cfg, slog.Default()))
	log.Fatal(b.Run(context.Background()))
}

// resolveGatewayURL passes plain http(s) URLs through and resolves a
// dominion:/// target against the Dominion service registry into its first
// endpoint (the same registry the production services use for discovery).
func resolveGatewayURL(raw string) (string, error) {
	if !strings.HasPrefix(raw, "dominion:///") {
		return raw, nil
	}

	target, err := solver.ParseTarget(raw)
	if err != nil {
		return "", err
	}
	resolver, err := solver.NewDeployResolver()
	if err != nil {
		return "", err
	}
	addresses, err := resolver.Resolve(context.Background(), target)
	if err != nil {
		return "", err
	}
	if len(addresses) == 0 {
		return "", fmt.Errorf("no ready endpoints for %q", raw)
	}
	return "http://" + addresses[0], nil
}

// handleHealth is the liveness probe. It returns the literal body "ok".
func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func atoiDefault(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
