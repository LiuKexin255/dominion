// Package testplan pins the sts2 minimal-wiring acceptance vocabulary for the
// game-sts2 suite: the fake-llm trigger and the expected reply of
// projects/game/fake-llm/service/testdata/sts2_main.yaml. The cases are added
// by the suite task (specs/080-host-main-sub-model/tasks.md T016).
//
// Contract: specs/080-host-main-sub-model/contracts/testplan-game-sts2.md
// §3/§4.
package testplan

const (
	// sts2PingTrigger is the user-message anchor that selects the
	// agent-v2-sts2-ping template (its `keywords` entry).
	sts2PingTrigger = "sts2-ping"
	// sts2PingReply is the agent-v2-sts2-ping template's `text`, asserted as
	// a substring of the completed main member turn.
	sts2PingReply = "sts2 主 agent 应答：sts2-ping 已收到，对话回合正常完成。"
)
