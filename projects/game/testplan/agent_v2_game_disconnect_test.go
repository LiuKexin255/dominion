// Package testplan contains the agent_v2 mid-game disconnect large test:
// the US1 disconnect/recovery branch of the game loop, in its own case
// binary because it needs the drop topology (the fake-desktop deployed with
// the progressive + disconnect-after-ops env — deploy_agent_v2_drop.yaml)
// while the remaining game cases need the won topology; guitar runs whole
// bazel targets as cases with no per-suite test-function filter, so the two
// suites cannot share a binary
// (specs/051-agent-v2-dsh-migration/revisions/directive-2026-09-01.md §1.4,
// style/large_test.md §测试组织).
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
// connection down once per connection lifetime and reconnects after its
// fixed reconnectDelay (projects/game/fake-desktop/service/session.go), so
// the wait must cover one reconnect cycle (2s delay + dial + probe).
const gameFlowReconnectWait = 8 * time.Second

// TestAgentV2GameDisconnectMidGameThenRecover covers US1 scenario 5 on the
// desktop-e2e-drop session. The executor closes its flow connection once
// per connection lifetime, right after the third operation receipt (the
// init F2 press + the two progressive cell ops), which yields a
// three-game sequence:
//
//	game 1: the full progressive chain SUCCEEDS — the disconnect fires after
//	        the last receipt, so the conversation stream never notices it
//	        (两流独立性 — flow 故障不影响对话流, US1 场景 6);
//	game 2: the connection is gone, saolei_init's dispatch fails with the
//	        model-visible "desktop disconnected" error and the turn still
//	        completes (回合存活);
//	game 3: after the executor reconnects, the same session plays a full
//	        game again with the board state re-seeded by init
//	        (desktop-bridge.md §6 验收锚点 3 — 重连后游戏可继续).
func TestAgentV2GameDisconnectMidGameThenRecover(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName, _ := agentV2GamePrep(t, sutHostURL, sutEnvName,
		agentV2DesktopDropID, "game-drop-"+uniqueSuffix(), "recover after the drop")

	// Game 1: the whole chain lands while the disconnect injection fires
	// invisibly behind the last receipt.
	stream1 := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerSaoleiProgressive+" first attempt")
	events1 := drainAgentV2Turn(t, stream1)
	assertAgentV2TurnWellFormed(t, sessionName, events1)
	if events1[len(events1)-1].GetTurnEnd().GetStatus() != game.TurnStatus_TURN_STATUS_COMPLETED {
		t.Fatalf("game 1 ended %v, want COMPLETED", events1[len(events1)-1].GetTurnEnd().GetStatus())
	}
	results1 := collectAgentV2GameEvents(events1)
	if len(results1) != 2 {
		t.Fatalf("game 1 tool_result count = %d, want 2", len(results1))
	}
	for i, r := range results1 {
		if r.GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED {
			t.Fatalf("game 1 tool_result[%d] status = %v, want SUCCEEDED (the fault fires only after the last receipt)", i, r.GetStatus())
		}
	}
	if !strings.Contains(results1[1].GetResult(), agentV2ProgExecContains) {
		t.Errorf("game 1 operate result = %q, want %q", results1[1].GetResult(), agentV2ProgExecContains)
	}
	if got := agentV2TerminalBlocksFromEvents(events1).text; got != agentV2ProgSummaryText {
		t.Errorf("game 1 terminal text = %q, want %q", got, agentV2ProgSummaryText)
	}

	// Game 2, right after: the connection is gone — the init dispatch fails
	// with the readable cause and the turn survives.
	stream2 := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerSaoleiProgressive+" while disconnected")
	events2 := drainAgentV2Turn(t, stream2)
	assertAgentV2TurnWellFormed(t, sessionName, events2)
	if events2[len(events2)-1].GetTurnEnd().GetStatus() != game.TurnStatus_TURN_STATUS_COMPLETED {
		t.Fatalf("game 2 ended %v, want COMPLETED (回合存活, US1 场景 4/5)", events2[len(events2)-1].GetTurnEnd().GetStatus())
	}
	results2 := collectAgentV2GameEvents(events2)
	if len(results2) != 1 {
		t.Fatalf("game 2 tool_result count = %d, want 1 (the failed saolei_init)", len(results2))
	}
	if results2[0].GetStatus() != game.ToolStatus_TOOL_STATUS_FAILED {
		t.Fatalf("game 2 saolei_init status = %v, want FAILED (the flow connection was torn down)", results2[0].GetStatus())
	}
	if !strings.Contains(results2[0].GetResult(), agentV2DisconnectedContain) {
		t.Errorf("game 2 init error = %q, want %q", results2[0].GetResult(), agentV2DisconnectedContain)
	}
	if got := agentV2TerminalBlocksFromEvents(events2).text; got != agentV2NodesktopSummary {
		t.Errorf("game 2 terminal text = %q, want %q", got, agentV2NodesktopSummary)
	}

	// The executor re-dials its session after the fixed reconnect delay;
	// the fresh connection attaches and game 3 runs the full chain again.
	time.Sleep(gameFlowReconnectWait)

	stream3 := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerSaoleiProgressive+" after reconnect")
	defer stream3.Close()
	events3 := drainAgentV2Turn(t, stream3)
	assertAgentV2TurnWellFormed(t, sessionName, events3)
	if events3[len(events3)-1].GetTurnEnd().GetStatus() != game.TurnStatus_TURN_STATUS_COMPLETED {
		t.Fatalf("game 3 ended %v, want COMPLETED", events3[len(events3)-1].GetTurnEnd().GetStatus())
	}
	results3 := collectAgentV2GameEvents(events3)
	if len(results3) != 2 {
		t.Fatalf("game 3 tool_result count = %d, want 2", len(results3))
	}
	if results3[0].GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED || results3[1].GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED {
		t.Fatalf("game 3 statuses = %v / %v, want both SUCCEEDED (desktop reconnected, US1 场景 5 恢复)", results3[0].GetStatus(), results3[1].GetStatus())
	}
	if !strings.Contains(results3[1].GetResult(), agentV2ProgExecContains) || !strings.Contains(results3[1].GetResult(), agentV2ProgStatusContains) {
		t.Errorf("game 3 operate result = %q, want %q + %q", results3[1].GetResult(), agentV2ProgExecContains, agentV2ProgStatusContains)
	}
	if got := agentV2TerminalBlocksFromEvents(events3).text; got != agentV2ProgSummaryText {
		t.Errorf("game 3 terminal text = %q, want %q", got, agentV2ProgSummaryText)
	}
	if events1[0].GetTurnId() == events3[0].GetTurnId() {
		t.Errorf("game 1 and game 3 share turn_id %q", events1[0].GetTurnId())
	}
}
