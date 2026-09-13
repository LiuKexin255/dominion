// Package testplan contains the agent_v2 stream-stall watchdog large test:
// the adapter-level idle watchdog of the GLM Responses wire
// (specs/063-llm-reliability-opencode-go/contracts/llm-failure-taxonomy.md
// §1 义务 4) exercised over the dedicated game-stall topology
// (projects/game/testplan/deploy_agent_v2_stall.yaml, which pins
// GLM_STREAM_IDLE_TIMEOUT_MS=2000). The module is the stall guard; it keeps
// its own binary because its 2s window cannot share the main topology's
// legitimate 3s/4s chunk gaps (specs/063-llm-reliability-opencode-go/research.md D13/D15).
// Cases are grouped by tested concern, one test per concern —
// style/large_test.md §测试组织.
package testplan

import (
	"strings"
	"testing"
	"time"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
)

// stallConvergenceBound is the generous upper bound on the whole stall
// turn's convergence: the six-attempt budget (1 initial + the default five
// retries) pays 6 × 2s watchdog windows plus the 0.5s→10s bounded backoff
// (≈30s), so 60s rules out an unbounded wait without pinning the retry
// timing (SC-004a: 有界收敛, not exact retry counts).
const stallConvergenceBound = 60 * time.Second

// TestAgentV2TeamStreamStallConverges covers SC-004a
// (specs/063-llm-reliability-opencode-go/spec.md SC-004a; quickstart.md §2
// SC-004a): the agent-v2-stall fixture emits one reasoning delta and then
// blocks with the connection alive forever, so the turn can only settle
// through the adapter's idle watchdog converting the stall into a retryable
// TIMEOUT and the existing retry semantics converging it to a visible
// failure — bounded, never an infinite hang. The drain's read window is the
// unbounded-hang guard: a missing or oversized watchdog would leave the
// stream open and fail the case at wsReadTimeout.
func TestAgentV2TeamStreamStallConverges(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName, _ := teamPrep(t, sutHostURL, sutEnvName, "team-stall-"+uniqueSuffix(), "stall")

	start := time.Now()
	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, agentV2TriggerStall+" plan the opening")
	events := drainTeamStream(t, stream)
	elapsed := time.Since(start)

	// The retried attempts re-announce their step-local block indexes, so the
	// settled-frame invariant checker (assertTeamStreamWellFormed) does not
	// apply to this failure path; the targeted assertions below are the
	// contract.
	turns := groupTeamMemberTurns(events)
	if len(turns) != 1 || turns[0].member != "planner" {
		t.Fatalf("stall stream turns = %v, want exactly one planner turn", turns)
	}
	end := turns[0].events[len(turns[0].events)-1].GetTurnEnd()
	if end.GetStatus() != game.TurnStatus_TURN_STATUS_ERROR {
		t.Fatalf("stall turn ended %v, want ERROR (the timeout class converges as a visible failure)", end.GetStatus())
	}
	if code := end.GetError().GetCode(); code != agentV2FailureTimeout {
		t.Errorf("turn_end.error.code = %q, want %q (the watchdog's retryable timeout classification)", code, agentV2FailureTimeout)
	}
	if message := end.GetError().GetMessage(); !strings.Contains(message, "idle timeout") {
		t.Errorf("turn_end.error.message = %q, want the watchdog's idle-timeout text", message)
	}
	if elapsed >= stallConvergenceBound {
		t.Errorf("stall turn converged after %s, want < %s (bounded retries, no unbounded wait)", elapsed, stallConvergenceBound)
	}
	// The failure retains the planner activation (the US2 retention semantics
	// every member turn failure enters).
	if got := teamActiveMember(t, ctx, sutHostURL, sutEnvName, sessionName); got != "planner" {
		t.Errorf("active_member after the stall failure = %q, want \"planner\"", got)
	}
}
