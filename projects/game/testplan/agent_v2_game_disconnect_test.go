// Package testplan contains the agent_v2 team mid-game disconnect large test:
// the US2 场景 8 branch of the team game loop, in its own case binary because
// it needs the drop topology (the fake-desktop deployed with the progressive
// + disconnect-after-ops env — deploy_agent_v2_drop.yaml) while the remaining
// game cases need the won topology; guitar runs whole bazel targets as cases
// with no per-suite test-function filter, so the two suites cannot share a
// binary (style/large_test.md §测试组织).
package testplan

import (
	"strings"
	"testing"
	"time"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
)

// gameFlowReconnectWait is the pause before re-driving a game on the
// desktop-e2e-drop session: the executor's fault injection tears the flow
// connection down once per connection lifetime and reconnects after its fixed
// reconnectDelay (projects/game/fake-desktop/service/session.go), so the wait
// must cover one reconnect cycle (2s delay + dial + probe).
const gameFlowReconnectWait = 8 * time.Second

// TestAgentV2TeamGameDisconnectMidGameThenRecover covers US2 场景 8 on the
// desktop-e2e-drop session. The executor closes its flow connection once per
// connection lifetime, right after the third operation receipt (the init F2
// press + the two progressive cell ops), which yields a three-game sequence
// for the team:
//
//	game 1: the planner opening structurally drives the player through the
//	        full progressive chain SUCCEEDED — the disconnect fires after the
//	        last receipt, so the team stream never notices it (流与编排解耦);
//	game 2: the connection is gone, the player's saolei_init fails with the
//	        model-visible "desktop disconnected" error and the turn still
//	        completes (回合存活、进程存活);
//	game 3: after the executor reconnects, the same team plays a full game
//	        again with the board state re-seeded by init (重连后可继续).
func TestAgentV2TeamGameDisconnectMidGameThenRecover(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName, _ := teamPrep(t, sutHostURL, sutEnvName, agentV2DesktopDropID, "team-drop")

	// Game 1: planner opening → player progressive chain (init + two ops),
	// while the disconnect injection fires invisibly behind the last receipt.
	stream1 := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamStartMessage)
	events1 := drainTeamStream(t, stream1)
	assertTeamStreamWellFormed(t, sessionName, events1)
	playerTurns1 := teamTurnsForMember(events1, "player")
	if len(playerTurns1) != 1 {
		t.Fatalf("game 1 player turns = %d, want 1", len(playerTurns1))
	}
	results1 := teamTurnToolResults(playerTurns1[0])
	if len(results1) != 2 {
		t.Fatalf("game 1 tool results = %d, want 2 (init + operate)", len(results1))
	}
	for i, result := range results1 {
		if result.GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED {
			t.Fatalf("game 1 tool result[%d] status = %v, want SUCCEEDED (the fault fires only after the last receipt)", i, result.GetStatus())
		}
	}
	if !strings.Contains(results1[1].GetResult(), agentV2ProgExecContains) {
		t.Errorf("game 1 operate result = %q, want %q", results1[1].GetResult(), agentV2ProgExecContains)
	}
	if _, text := teamTurnBlocks(playerTurns1[0]); text != agentV2ProgSummaryText {
		t.Errorf("game 1 terminal text = %q, want %q", text, agentV2ProgSummaryText)
	}

	// Game 2, right after: the connection is gone — the next init (driven by
	// the player anchor) fails with the readable cause and the turn survives.
	stream2 := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamNextGameMessage)
	events2 := drainTeamStream(t, stream2)
	assertTeamStreamWellFormed(t, sessionName, events2)
	playerTurns2 := teamTurnsForMember(events2, "player")
	if len(playerTurns2) != 1 {
		t.Fatalf("game 2 player turns = %d, want 1", len(playerTurns2))
	}
	if status := teamTurnEndStatus(playerTurns2[0]); status != game.TurnStatus_TURN_STATUS_COMPLETED {
		t.Fatalf("game 2 ended %v, want COMPLETED (回合存活)", status)
	}
	results2 := teamTurnToolResults(playerTurns2[0])
	if len(results2) != 1 || results2[0].GetStatus() != game.ToolStatus_TOOL_STATUS_FAILED {
		t.Fatalf("game 2 tool results = %+v, want one FAILED saolei_init", results2)
	}
	if !strings.Contains(results2[0].GetResult(), agentV2DisconnectedContain) {
		t.Errorf("game 2 init error = %q, want %q", results2[0].GetResult(), agentV2DisconnectedContain)
	}
	if _, text := teamTurnBlocks(playerTurns2[0]); text != agentV2NodesktopSummary {
		t.Errorf("game 2 terminal text = %q, want %q", text, agentV2NodesktopSummary)
	}

	// The executor re-dials its session after the fixed reconnect delay; the
	// fresh connection attaches and game 3 runs the full chain again.
	time.Sleep(gameFlowReconnectWait)
	stream3 := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamNextGameMessage)
	events3 := drainTeamStream(t, stream3)
	assertTeamStreamWellFormed(t, sessionName, events3)
	playerTurns3 := teamTurnsForMember(events3, "player")
	if len(playerTurns3) != 1 {
		t.Fatalf("game 3 player turns = %d, want 1", len(playerTurns3))
	}
	results3 := teamTurnToolResults(playerTurns3[0])
	if len(results3) != 2 {
		t.Fatalf("game 3 tool results = %d, want 2 (init + operate)", len(results3))
	}
	if results3[0].GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED || results3[1].GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED {
		t.Fatalf("game 3 statuses = %v / %v, want both SUCCEEDED (desktop reconnected)", results3[0].GetStatus(), results3[1].GetStatus())
	}
	if !strings.Contains(results3[1].GetResult(), agentV2ProgExecContains) || !strings.Contains(results3[1].GetResult(), agentV2ProgStatusContains) {
		t.Errorf("game 3 operate result = %q, want %q + %q", results3[1].GetResult(), agentV2ProgExecContains, agentV2ProgStatusContains)
	}
	if _, text := teamTurnBlocks(playerTurns3[0]); text != agentV2ProgSummaryText {
		t.Errorf("game 3 terminal text = %q, want %q", text, agentV2ProgSummaryText)
	}
	if playerTurns1[0].turnID == playerTurns3[0].turnID {
		t.Errorf("game 1 and game 3 share turn_id %q", playerTurns1[0].turnID)
	}
}
