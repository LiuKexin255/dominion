package main

import (
	"testing"

	"dominion/projects/game/proxy/handler"

	grpcgo "google.golang.org/grpc"
)

// TestRegisterServices asserts the proxy server's registration surface
// carries every forwarded service: the v1 TeamService face plus the agent_v2
// AgentService and DesktopBridgeService faces (the desktop flow stream,
// specs/051-agent-v2-dsh-migration/contracts/desktop-bridge.md §1). The
// forwarding behavior itself is covered by the handler package tests; the
// zero-value handlers here are registration placeholders — registration and
// service-info lookup never invoke them.
func TestRegisterServices(t *testing.T) {
	srv := grpcgo.NewServer()
	registerServices(
		srv,
		&handler.TeamHandler{},
		&handler.AgentHandler{},
		&handler.DesktopBridgeHandler{},
	)

	info := srv.GetServiceInfo()
	for _, svc := range []string{
		"projects.game.TeamService",
		"projects.game.v2.AgentService",
		"projects.game.v2.DesktopBridgeService",
	} {
		if _, ok := info[svc]; !ok {
			t.Fatalf("service %q not registered; registered services = %v", svc, serviceNames(info))
		}
	}
}

// serviceNames lists the registered service names for failure messages.
func serviceNames(info map[string]grpcgo.ServiceInfo) []string {
	names := make([]string, 0, len(info))
	for name := range info {
		names = append(names, name)
	}
	return names
}
