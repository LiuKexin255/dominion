// Package testplan contains the agent_v2 team materialization fail-loud
// branch: the planner-memory prefetch against an unreachable memory service
// (specs/059-agent-v2-team-mode/tasks.md T023; quickstart.md V7-2). The case
// lives in its own binary because it needs the memory-down topology
// (projects/game/testplan/deploy_agent_v2_memory_down.yaml omits the memory
// service) while the remaining suites need it deployed; guitar runs whole
// bazel targets as suite cases with no per-suite test-function filtering
// (style/large_test.md §测试组织).
package testplan

import (
	"net/http"
	"testing"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
)

// TestAgentV2TeamMemoryUnreachableRollsBackMaterialization covers quickstart
// V7-2 (US3 / decision ⑧): with the memory service absent from the
// deployment, UpdateTeam reaches the planner member's `plannerMemory.load`
// prefetch, which fails loud (the gRPC call cannot resolve its dominion
// endpoint); the materialization rolls back with NO half-materialized team
// (GetTeam NOT_FOUND) and the failure is retryable — the second attempt fails
// the same way without residue. The configuration face (Mongo-backed presets,
// static model catalog) keeps serving, proving the process stays healthy.
//
// The retry-SUCCESS half of V7-2 is covered where it is observable: the unit
// seam (session.test.ts, materialization failure rollback then retry with a
// healthy double) and the memory-present suite (every team case materializes
// successfully). This topology cannot restore the memory service mid-suite —
// guitar/deploy has no per-service restart — so the case asserts the failure
// + no-half-materialization half only.
func TestAgentV2TeamMemoryUnreachableRollsBackMaterialization(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	sessionName := ensureAgentV2Session(t, sutHostURL, sutEnvName, "team-memdown-"+uniqueSuffix())
	player, planner := createAgentV2TeamPresetPair(t, ctx, sutHostURL, sutEnvName, "team-memdown", "memory-down")

	// Materialization fails at the planner's memory prefetch. Two attempts
	// also prove the failure is cleanly retryable (no poisoned session/owner
	// state); each attempt pays the client's 10s wait-for-ready deadline.
	for attempt := 1; attempt <= 2; attempt++ {
		_, status, body := updateAgentV2TeamWithStatus(t, ctx, sutHostURL, sutEnvName, sessionName,
			player.GetName(), planner.GetName(), "", "")
		if status == http.StatusOK {
			t.Fatalf("UpdateTeam attempt %d status = 200 (body: %s), want the memory-prefetch failure", attempt, body)
		}
		if status < http.StatusInternalServerError {
			t.Fatalf("UpdateTeam attempt %d status = %d (body: %s), want a 5xx mapping of the memory outage", attempt, status, body)
		}
		t.Logf("UpdateTeam attempt %d failed as expected: status=%d body=%s", attempt, status, body)

		// No half-materialized team: the fail-fast rollback never publishes
		// the team singleton.
		if getStatus, getBody := getAgentV2TeamWithStatus(t, ctx, sutHostURL, sutEnvName, sessionName); getStatus != http.StatusNotFound {
			t.Fatalf("GetTeam after attempt %d status = %d (body: %s), want 404 NOT_FOUND (无半物化)", attempt, getStatus, getBody)
		}
	}

	// The configuration face keeps serving: the Mongo-backed presets survive
	// the failed materializations and the static catalog stays readable.
	for _, preset := range []*game.Preset{player, planner} {
		if got := getAgentV2Preset(t, ctx, sutHostURL, sutEnvName, preset.GetName()); got.GetRole() == "" {
			t.Errorf("preset %s lost its role after the failed materializations", preset.GetName())
		}
	}
	if models := listAgentV2Models(t, ctx, sutHostURL, sutEnvName); len(models.GetModels()) == 0 {
		t.Error("ListModels returned no models after the failed materializations")
	}
}
