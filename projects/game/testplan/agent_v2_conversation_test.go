// Package testplan contains the agent_v2 team conversation large tests: the
// team stream (member event frames + team_message frames until quiescence),
// the merged/member-view histories, the queue/cancel/refresh orchestration
// windows, and the per-member turn error paths — over the gateway /api/v2
// NDJSON surface with the deterministic fake /v1/responses endpoint
// (specs/059-agent-v2-team-mode/contracts/team-api.md §3; quickstart.md
// V3/V4/V6). Cases are grouped by tested concern, one test per concern —
// style/large_test.md §测试组织. The game chain with a real desktop lives in
// agent_v2_game_test.go; the disconnect branch in
// agent_v2_game_disconnect_test.go.
package testplan

import (
	"net/http"
	"strings"
	"testing"
	"time"

	game "dominion/projects/game"

	"dominion/common/gopkg/testtool"

	"google.golang.org/protobuf/proto"
)

// TestAgentV2TeamStaticWaitAndFirstDrive covers V3 (US2 场景 1/2): after
// UpdateTeam the team rests — no member is driven and the merged sequence
// stays empty until the user's first Send; that Send drives the initial
// planner activation, whose opening strategy then structurally drives the
// player (no synthesized drive message anywhere, FR-009/FR-010), and the
// stream carries the member-labelled frames plus the team_message entries
// whose seq the List face shares (SC-003). The 060 increments: the planner's
// consumption of the user input arrives live as a member_view frame
// (specs/060-agent-v2-team-optimize/quickstart.md V4-1) and the tool chain
// keeps the use-time frame order block_end → team_message → tool_result
// (contracts/team-api.md §3).
func TestAgentV2TeamStaticWaitAndFirstDrive(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName, team := teamPrep(t, sutHostURL, sutEnvName, "team-static-"+uniqueSuffix(), "static")
	teamName := agentV2TeamName(sessionName)

	// The materialized singleton carries the fixed roster and the effective
	// member configuration (team-api.md §2).
	if team.GetName() != teamName {
		t.Fatalf("materialized team name = %q, want %q", team.GetName(), teamName)
	}
	if len(team.GetMembers()) != 2 {
		t.Fatalf("team members = %d, want 2 (player + planner)", len(team.GetMembers()))
	}
	if player := teamMemberByRole(team, "player"); player == nil || player.GetPreset() == "" {
		t.Fatalf("player member = %+v, want a configured player member", player)
	}
	if planner := teamMemberByRole(team, "planner"); planner == nil || planner.GetPreset() == "" {
		t.Fatalf("planner member = %+v, want a configured planner member", planner)
	}

	// 静止等待: no user Send means no drive — the merged sequence and both
	// member views stay empty (the orchestration synthesizes no message).
	time.Sleep(3 * time.Second)
	if got := listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName); len(got) != 0 {
		t.Fatalf("team produced %d message(s) before the first Send, want 0 (静止等待, FR-009)", len(got))
	}
	for _, member := range []string{"player", "planner"} {
		if got := listMemberMessages(t, ctx, sutHostURL, sutEnvName, sessionName, member); len(got) != 0 {
			t.Errorf("%s view has %d message(s) before the first Send, want 0", member, len(got))
		}
	}

	// 用户首驱: the first Send drives the planner (the initial activation)
	// and the same stream covers the structural player continuation.
	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamStartMessage)
	events := drainTeamStream(t, stream)
	assertTeamStreamWellFormed(t, sessionName, events)

	// The planner consumed the user input live: the member_view frame arrived
	// no later than the planner's first content frame, carries the user as
	// its source, and its projection is the one ListMemberMessages serves. The
	// player did not receive the raw input (消费前不出现 — the broadcast relay
	// is a separate consumption).
	consumed := assertTeamMemberViewLive(t, ctx, sutHostURL, sutEnvName, sessionName, events, "planner", "user")
	if got := agentV2MessageText(consumed.GetMessage()); got != teamStartMessage {
		t.Errorf("member_view planner/user text = %q, want the sent message %q", got, teamStartMessage)
	}
	for _, event := range events {
		if view := event.GetMemberView(); view != nil && view.GetMember() == "player" && view.GetSender() == "user" {
			t.Errorf("player received the raw user input live before consuming it: %+v", view)
		}
	}

	// The player's tool chain keeps the use-time frame order: block_end (tool
	// id) → team_message fixation → tool_result settlement.
	assertTeamToolResultWireOrder(t, sessionName, events)

	turns := groupTeamMemberTurns(events)
	if len(turns) != 2 {
		t.Fatalf("member turns = %d, want 2 (planner opening + player continuation)", len(turns))
	}
	if turns[0].member != "planner" {
		t.Fatalf("first driven member = %v, want 'planner' (initial activation)", turns[0].member)
	}
	if _, text := teamTurnBlocks(turns[0]); text != teamPlannerOpeningText {
		t.Errorf("planner opening text = %q, want %q", text, teamPlannerOpeningText)
	}
	if status := teamTurnEndStatus(turns[0]); status != game.TurnStatus_TURN_STATUS_COMPLETED {
		t.Errorf("planner opening turn ended %v, want COMPLETED", status)
	}
	if turns[1].member != "player" {
		t.Fatalf("second driven member = %v, want 'player' (structural continuation)", turns[1].member)
	}
	results := teamTurnToolResults(turns[1])
	if len(results) != 1 || results[0].GetStatus() != game.ToolStatus_TOOL_STATUS_FAILED {
		t.Fatalf("player continuation tool results = %+v, want one FAILED saolei_init (no desktop)", results)
	}
	if !strings.Contains(results[0].GetResult(), agentV2DisconnectedContain) {
		t.Errorf("saolei_init error = %q, want the readable cause %q", results[0].GetResult(), agentV2DisconnectedContain)
	}
	if _, text := teamTurnBlocks(turns[1]); text != agentV2NodesktopSummary {
		t.Errorf("player continuation text = %q, want %q", text, agentV2NodesktopSummary)
	}

	// No orchestration-synthesized drives: the only USER merge entry is the
	// message this test sent.
	entries := listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName)
	userEntries := teamMessagesForMember(entries, "user")
	if len(userEntries) != 1 || agentV2MessageText(userEntries[0].GetMessage()) != teamStartMessage {
		t.Fatalf("USER merge entries = %d, want exactly the sent message (FR-010)", len(userEntries))
	}

	// The stream's team_message frames and ListTeamMessages are the same
	// sequence with the same seq anchor.
	assertTeamStreamMessagesMatchList(t, ctx, sutHostURL, sutEnvName, sessionName, events)

	// Member view (team-api.md §5): the player consumed the planner's
	// opening as a sender-annotated relay; the team view renders only
	// native output (no broadcast wrapper).
	playerView := listMemberMessages(t, ctx, sutHostURL, sutEnvName, sessionName, "player")
	sawPlannerRelay := false
	for _, entry := range playerView {
		if entry.GetSender() == "planner" {
			sawPlannerRelay = true
			// The relay body is the injection original: the tag pair around
			// the verbatim speech, no head line and the body exactly once
			// (specs/060-agent-v2-team-optimize/contracts/team-api.md §4
			// 成员视角 relay 呈现).
			want := "<planner-message>\n" + teamPlannerOpeningText + "\n</planner-message>"
			if text := agentV2MessageText(entry.GetMessage()); text != want {
				t.Errorf("planner relay in the player view = %q, want %q", text, want)
			}
		}
	}
	if !sawPlannerRelay {
		t.Error("player view carries no planner relay after the opening drive")
	}
	for _, entry := range entries {
		if text := agentV2MessageText(entry.GetMessage()); strings.Contains(text, "<planner-message>") || strings.Contains(text, "<player-message>") || strings.Contains(text, "<player-tool-call>") {
			t.Errorf("team view entry (member %v) carries the relay wrapper: %q", entry.GetMember(), text)
		}
	}
}

// TestAgentV2TeamViewDataProjections covers the two List projections side by
// side (US4 / quickstart V5-1/V5-2, SC-003): ListTeamMessages returns the
// merged sequence with producer labels ("user"/"player"/"planner"), strictly
// monotonic seq, and the members' NATIVE output (settled tool-call blocks, no
// relay wrapper); ListMemberMessages follows the perspective contract per
// member (own output = AGENT, user input = USER/sender "user", the other
// member's relay = USER/sender <role>); every merged member output is the
// same message (messageId + body) as its entry in that member's own view;
// both envelopes leave the pagination compat slot empty; and a later
// player-handled Send puts the user input in the player view too (user→user
// on both sides).
func TestAgentV2TeamViewDataProjections(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	sessionID := "team-views-" + uniqueSuffix()
	ctx, sessionName, _ := teamPrep(t, sutHostURL, sutEnvName, sessionID, "team-views")

	// A won game that continues into a second one on the test's own desktop
	// half (V4): the merged sequence then carries both members' native
	// output. Game 1 settles init + operate; game 2's init already
	// recognizes the win board, so that turn concludes at the init result
	// (specs/062-team-game-end-handoff/spec.md FR-002 ①) and no operate
	// follows.
	flow, _ := dialAgentV2FlowProbed(t, ctx, sutHostURL, sutEnvName, sessionID)
	defer flow.Close()
	scriptCh := serveTeamFlowScript(flow, sessionID, teamFlowScript{
		initBoards: [][]byte{saoleiBoardCompatWinPNG, saoleiBoardWinPNG},
		stepBoards: [][]byte{saoleiBoardWinPNG},
	}, wsReadTimeout)
	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamStartMessage)
	events := drainTeamStream(t, stream)
	waitTeamFlowScript(t, scriptCh, wsReadTimeout)
	assertTeamStreamWellFormed(t, sessionName, events)

	// ListTeamMessages: labels, seq monotonicity, native output.
	teamEnvelope := listTeamMessagesResponse(t, ctx, sutHostURL, sutEnvName, sessionName)
	if token := teamEnvelope.GetNextPageToken(); token != "" {
		t.Errorf("ListTeamMessages next_page_token = %q, want the empty pagination compat slot", token)
	}
	entries := teamEnvelope.GetMessages()
	counts := map[string]int{}
	lastSeq := int64(0)
	for i, entry := range entries {
		counts[entry.GetMember()]++
		if entry.GetSeq() <= lastSeq {
			t.Errorf("merge entry[%d] seq = %d, want > previous %d (seq 单调)", i, entry.GetSeq(), lastSeq)
		}
		lastSeq = entry.GetSeq()
		if text := agentV2MessageText(entry.GetMessage()); strings.Contains(text, "<player-") || strings.Contains(text, "<planner-") {
			t.Errorf("merge entry[%d] (member %q) carries a relay wrapper: %q (团队视图取原生输出)", i, entry.GetMember(), text)
		}
	}
	if counts["user"] != 1 {
		t.Errorf("merge USER entries = %d, want exactly the initial Send", counts["user"])
	}
	if counts["player"] == 0 || counts["planner"] == 0 {
		t.Errorf("merge member entries = player:%d planner:%d, want both members present", counts["player"], counts["planner"])
	}
	userEntries := teamMessagesForMember(entries, "user")
	if len(userEntries) != 1 || agentV2MessageText(userEntries[0].GetMessage()) != teamStartMessage {
		t.Fatalf("merge USER entries = %+v, want exactly the sent message", userEntries)
	}
	var toolCalls []*game.ToolCallBlock
	for _, entry := range teamMessagesForMember(entries, "player") {
		for _, block := range entry.GetMessage().GetBlocks() {
			if call := block.GetToolCall(); call != nil {
				toolCalls = append(toolCalls, call)
			}
		}
	}
	if len(toolCalls) != 3 {
		t.Fatalf("merge player tool-call blocks = %d, want 3 (game 1 init + operate; game 2 init-terminal, no operate)", len(toolCalls))
	}
	for i, call := range toolCalls {
		if call.GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED || call.GetResult() == "" {
			t.Errorf("merge tool block[%d] (%s) = %v/%q, want a settled SUCCEEDED result", i, call.GetName(), call.GetStatus(), call.GetResult())
		}
		if call.GetName() != "saolei_init" && call.GetName() != "saolei_operate" {
			t.Errorf("merge tool block[%d] name = %q, want a saolei tool", i, call.GetName())
		}
	}

	// ListMemberMessages: the perspective contract per member.
	plannerEnvelope := listMemberMessagesResponse(t, ctx, sutHostURL, sutEnvName, sessionName, "planner")
	playerEnvelope := listMemberMessagesResponse(t, ctx, sutHostURL, sutEnvName, sessionName, "player")
	for name, envelope := range map[string]*game.ListMemberMessagesResponse{
		"planner": plannerEnvelope,
		"player":  playerEnvelope,
	} {
		if token := envelope.GetNextPageToken(); token != "" {
			t.Errorf("ListMemberMessages(%s) next_page_token = %q, want the empty pagination compat slot", name, token)
		}
	}
	plannerView := plannerEnvelope.GetMessages()
	playerView := playerEnvelope.GetMessages()
	assertMemberViewPerspective(t, "planner", plannerView, "planner")
	assertMemberViewPerspective(t, "player", playerView, "player")

	// The planner consumed the user's first Send directly, and the chain
	// relayed both members' output to the other side.
	sawPlannerUserInput, sawPlannerPlayerRelay := false, false
	for _, entry := range plannerView {
		switch {
		case entry.GetSender() == "user" && agentV2MessageText(entry.GetMessage()) == teamStartMessage:
			sawPlannerUserInput = true
		case entry.GetSender() == "player":
			sawPlannerPlayerRelay = true
		}
	}
	if !sawPlannerUserInput {
		t.Error("planner view has no user input entry (user→user)")
	}
	if !sawPlannerPlayerRelay {
		t.Error("planner view has no relayed player output")
	}
	sawPlayerRelay := false
	for _, entry := range playerView {
		if entry.GetSender() == "planner" {
			sawPlayerRelay = true
		}
	}
	if !sawPlayerRelay {
		t.Error("player view has no relayed planner output")
	}

	// Cross-view consistency: every merged member output is the same message
	// (messageId + body) as its entry in that member's own view.
	assertMergeMatchesMemberViews(t, entries, map[string][]*game.MemberViewMessage{
		"planner": plannerView,
		"player":  playerView,
	})

	// A later Send reaches the player (the current activation after the last
	// game), so the player view carries the user input as well.
	stream2 := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamQueueMessage)
	events2 := drainTeamStream(t, stream2)
	assertTeamStreamWellFormed(t, sessionName, events2)
	playerView = listMemberMessages(t, ctx, sutHostURL, sutEnvName, sessionName, "player")
	sawPlayerUserInput := false
	for _, entry := range playerView {
		if entry.GetSender() == "user" && agentV2MessageText(entry.GetMessage()) == teamQueueMessage {
			sawPlayerUserInput = true
		}
	}
	if !sawPlayerUserInput {
		t.Fatal("player view has no user input entry after the player-handled Send (user→user)")
	}
}

// TestAgentV2TeamQueueDigestPriority covers V6-1/2 (FR-011): a message sent
// while the planner's long turn is in flight queues behind it (queued frame
// first on its own stream), and after the running turn ends the CURRENT
// activation digests the queued message BEFORE the orchestrator switches to
// the player — the queued digest is an ordinary member turn driven by the
// user message itself, no synthesized prompt.
func TestAgentV2TeamQueueDigestPriority(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName, _ := teamPrep(t, sutHostURL, sutEnvName, "team-queue-"+uniqueSuffix(), "queue")

	// Send A runs the long planner turn; read until turn_start proves it is
	// in flight before Send B arrives.
	streamA := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamWaitMessage)
	var preA []*game.ChatEvent
	for {
		event := nextTeamEvent(t, streamA.Scanner)
		preA = append(preA, event)
		if event.GetTurnStart() != nil {
			break
		}
	}
	if member := preA[len(preA)-1].GetMember(); member != "planner" {
		t.Fatalf("running turn member = %v, want 'planner'", member)
	}

	// Send B queues behind the running turn: queued first, then the enqueue
	// fixation as a team_message{USER} frame.
	streamB := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamQueueMessage)
	firstB := nextTeamEvent(t, streamB.Scanner)
	if firstB.GetQueued() == nil {
		t.Fatalf("Send B first frame payload = %T, want queued{position}", firstB.GetPayload())
	}
	if pos := firstB.GetQueued().GetPosition(); pos < 1 {
		t.Errorf("queued position = %d, want >= 1", pos)
	}

	eventsA := append(preA, waitTeamStream(t, drainTeamStreamAsync(streamA), "wait-turn stream")...)
	eventsB := append([]*game.ChatEvent{firstB}, waitTeamStream(t, drainTeamStreamAsync(streamB), "queued stream")...)
	assertTeamStreamWellFormed(t, sessionName, eventsA)

	turnsA := groupTeamMemberTurns(eventsA)
	if len(turnsA) != 3 {
		t.Fatalf("member turns = %d, want 3 (planner wait, planner digest, player switch)", len(turnsA))
	}
	if turnsA[0].member != "planner" {
		t.Fatalf("turn 1 member = %v, want 'planner' (the long turn)", turnsA[0].member)
	}
	if _, text := teamTurnBlocks(turnsA[0]); text != teamPlannerWaitText {
		t.Errorf("long planner turn text = %q, want %q", text, teamPlannerWaitText)
	}
	// 消化优先于切换: the queued message is digested by the current planner
	// activation, not deferred to the player switch.
	if turnsA[1].member != "planner" {
		t.Fatalf("turn 2 member = %v, want 'planner' (the queued digest, FR-011)", turnsA[1].member)
	}
	if _, text := teamTurnBlocks(turnsA[1]); text != teamPlannerUserReplyText {
		t.Errorf("queued digest text = %q, want %q", text, teamPlannerUserReplyText)
	}
	// Only after the digest does the structural switch drive the player.
	if turnsA[2].member != "player" {
		t.Errorf("turn 3 member = %v, want 'player' (the switch after digestion)", turnsA[2].member)
	}

	// Every active stream sees the fanned-out digest (team-api.md §3.3).
	sawDigestB := false
	for _, turn := range groupTeamMemberTurns(eventsB) {
		if _, text := teamTurnBlocks(turn); turn.member == "planner" && text == teamPlannerUserReplyText {
			sawDigestB = true
		}
	}
	if !sawDigestB {
		t.Error("the queued stream missed the digest turn fanned out on the session")
	}

	// The queue was consumed in order: both user messages and no synthetic
	// USER entry.
	userEntries := teamMessagesForMember(listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName), "user")
	if len(userEntries) != 2 {
		t.Fatalf("USER merge entries = %d, want 2 (both sends)", len(userEntries))
	}
	if agentV2MessageText(userEntries[0].GetMessage()) != teamWaitMessage || agentV2MessageText(userEntries[1].GetMessage()) != teamQueueMessage {
		t.Errorf("USER entries = [%q %q], want the two sent messages in order",
			agentV2MessageText(userEntries[0].GetMessage()), agentV2MessageText(userEntries[1].GetMessage()))
	}
}

// TestAgentV2TeamCancelPausesAndSendResumes covers V6-3 (FR-017): Cancel
// terminates the in-flight member turn (turn_end{CANCELED}), pauses the
// automatic continuation, keeps the queued message in the history without
// driving it, is idempotent, and a later Send resumes the loop with the
// current activation.
func TestAgentV2TeamCancelPausesAndSendResumes(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName, _ := teamPrep(t, sutHostURL, sutEnvName, "team-cancel-"+uniqueSuffix(), "cancel")

	// A long planner turn is in flight.
	streamA := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamWaitMessage)
	for {
		if event := nextTeamEvent(t, streamA.Scanner); event.GetTurnStart() != nil {
			break
		}
	}
	// A second message queues behind it.
	streamB := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamQueueMessage)
	firstB := nextTeamEvent(t, streamB.Scanner)
	if firstB.GetQueued() == nil {
		t.Fatalf("queued Send first frame payload = %T, want queued{position}", firstB.GetPayload())
	}

	if status, body := postTeamCancel(t, ctx, sutHostURL, sutEnvName, sessionName); status != http.StatusOK {
		t.Fatalf("cancel status = %d (body: %s), want 200", status, body)
	}

	eventsA := waitTeamStream(t, drainTeamStreamAsync(streamA), "canceled stream")
	if last := eventsA[len(eventsA)-1].GetTurnEnd(); last == nil || last.GetStatus() != game.TurnStatus_TURN_STATUS_CANCELED {
		t.Fatalf("in-flight turn terminal = %+v, want turn_end{CANCELED}", last)
	}
	// The queued stream attached mid-turn: it sees no turn_start of its own
	// (the queued message never drove a turn), and any member frame it
	// observed belongs to the canceled in-flight turn.
	eventsB := append([]*game.ChatEvent{firstB}, waitTeamStream(t, drainTeamStreamAsync(streamB), "queued stream")...)
	for _, event := range eventsB {
		if event.GetTurnStart() != nil {
			t.Errorf("queued stream saw a turn_start after Cancel: %+v (排队消息不触发新驱动)", event)
		}
		if end := event.GetTurnEnd(); end != nil && end.GetStatus() != game.TurnStatus_TURN_STATUS_CANCELED {
			t.Errorf("queued stream saw turn_end %v, want only the canceled in-flight terminal", end.GetStatus())
		}
	}

	// The queued message stays fixed in the merged sequence but produced no
	// reply; Cancel is idempotent.
	entries := listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName)
	if got := len(teamMessagesForMember(entries, "user")); got != 2 {
		t.Fatalf("USER merge entries after cancel = %d, want 2 (both sends fixed)", got)
	}
	for _, entry := range entries {
		if strings.Contains(agentV2MessageText(entry.GetMessage()), teamPlannerUserReplyText) {
			t.Error("the canceled queued message was driven — Cancel must keep it as history only")
		}
	}
	if status, body := postTeamCancel(t, ctx, sutHostURL, sutEnvName, sessionName); status != http.StatusOK {
		t.Errorf("second cancel status = %d (body: %s), want 200 (idempotent no-op)", status, body)
	}

	// A later Send resumes: the message is digested by the current planner
	// activation (Send 即有输入即驱动, FR-017).
	streamC := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamQueueMessage)
	eventsC := drainTeamStream(t, streamC)
	assertTeamStreamWellFormed(t, sessionName, eventsC)
	sawDigest := false
	for _, turn := range groupTeamMemberTurns(eventsC) {
		if turn.member == "planner" {
			if _, text := teamTurnBlocks(turn); text == teamPlannerUserReplyText {
				sawDigest = true
			}
		}
	}
	if !sawDigest {
		t.Fatalf("post-cancel Send did not drive the planner digest; turns = %v", groupTeamMemberTurns(eventsC))
	}
}

// TestAgentV2TeamRefreshTerminatesInFlightAndClears covers US2 场景 7: a
// refresh (UpdateTeam again) terminates the in-flight member turn
// (turn_end{ABORTED}), voids the queued messages, clears both members'
// short-term memory (a fresh empty history), rebuilds with the new
// configuration, and preserves create_time.
func TestAgentV2TeamRefreshTerminatesInFlightAndClears(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName, first := teamPrep(t, sutHostURL, sutEnvName, "team-refresh-"+uniqueSuffix(), "refresh")

	models := listAgentV2Models(t, ctx, sutHostURL, sutEnvName)
	if len(models.GetModels()) < 2 {
		t.Fatalf("model catalog = %d entries, want the pinned 2", len(models.GetModels()))
	}
	nextModel := models.GetModels()[1].GetId()

	// An in-flight turn plus a queued message: both must be voided.
	streamA := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamWaitMessage)
	for {
		if event := nextTeamEvent(t, streamA.Scanner); event.GetTurnStart() != nil {
			break
		}
	}
	streamB := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamQueueMessage)
	firstB := nextTeamEvent(t, streamB.Scanner)
	if firstB.GetQueued() == nil {
		t.Fatalf("queued Send first frame payload = %T, want queued{position}", firstB.GetPayload())
	}

	// Refresh with a different player model (same presets).
	refreshed := updateAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName,
		teamMemberPreset(first, "player"), teamMemberPreset(first, "planner"), nextModel, "")
	if teamMemberModel(refreshed, "player") != nextModel {
		t.Errorf("refreshed player model = %q, want %q", teamMemberModel(refreshed, "player"), nextModel)
	}
	if !proto.Equal(first.GetCreateTime(), refreshed.GetCreateTime()) {
		t.Errorf("create_time after refresh = %v, want the preserved %v", refreshed.GetCreateTime(), first.GetCreateTime())
	}
	if refreshed.GetUpdateTime().AsTime().Before(first.GetUpdateTime().AsTime()) {
		t.Errorf("update_time after refresh = %v, want >= the first materialization's %v", refreshed.GetUpdateTime(), first.GetUpdateTime())
	}

	// The old lifecycle's streams end at the teardown: the in-flight turn as
	// ABORTED, the queued stream without a turn.
	eventsA := waitTeamStream(t, drainTeamStreamAsync(streamA), "refresh-aborted stream")
	if last := eventsA[len(eventsA)-1].GetTurnEnd(); last == nil || last.GetStatus() != game.TurnStatus_TURN_STATUS_ABORTED {
		t.Fatalf("in-flight turn terminal after refresh = %+v, want turn_end{ABORTED}", last)
	}
	eventsB := append([]*game.ChatEvent{firstB}, waitTeamStream(t, drainTeamStreamAsync(streamB), "refresh-queued stream")...)
	for _, event := range eventsB {
		if event.GetTurnStart() != nil {
			t.Errorf("queued stream saw a turn_start across the refresh: %+v", event)
		}
		if end := event.GetTurnEnd(); end != nil && end.GetStatus() != game.TurnStatus_TURN_STATUS_ABORTED {
			t.Errorf("queued stream saw turn_end %v, want only the aborted in-flight terminal", end.GetStatus())
		}
	}

	// The rebuilt team starts empty (memory cleared) and the voided queue
	// never drives it.
	if got := listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName); len(got) != 0 {
		t.Fatalf("history after refresh = %d entries, want 0 (短期记忆清空)", len(got))
	}
	time.Sleep(3 * time.Second)
	if got := listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName); len(got) != 0 {
		t.Fatalf("history 3s after refresh = %d entries, want 0 (queued messages voided, no drive)", len(got))
	}

	// The rebuilt team is materialized with the new configuration.
	stored := getAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName)
	if teamMemberModel(stored, "player") != nextModel {
		t.Errorf("stored player model = %q, want %q", teamMemberModel(stored, "player"), nextModel)
	}
	if teamMemberPreset(stored, "player") != teamMemberPreset(first, "player") || teamMemberPreset(stored, "planner") != teamMemberPreset(first, "planner") {
		t.Errorf("stored presets = {%q %q}, want the unchanged {%q %q}",
			teamMemberPreset(stored, "player"), teamMemberPreset(stored, "planner"), teamMemberPreset(first, "player"), teamMemberPreset(first, "planner"))
	}
}

// TestAgentV2TeamPlayerStreamedTurnAndContinuity covers the streamed
// think+text member turn and multi-turn continuity on the player activation
// (US1-1/US2): the greet template streams two progressive THINK deltas then
// the TEXT delta, and the second turn is served by the followup template
// because the turn-1 reply already sits in the member history
// (history_keywords, responses.go matchResponsesMultiTurn).
func TestAgentV2TeamPlayerStreamedTurnAndContinuity(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName := teamPlayerActivation(t, sutHostURL, sutEnvName, "team-stream-"+uniqueSuffix(), "stream")

	stream1 := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, agentV2TriggerThink+" introduce yourself")
	events1 := drainTeamStream(t, stream1)
	assertTeamStreamWellFormed(t, sessionName, events1)
	turns1 := groupTeamMemberTurns(events1)
	if len(turns1) != 1 || turns1[0].member != "player" {
		t.Fatalf("player stream turns = %v, want exactly one player turn", turns1)
	}
	think, text := teamTurnBlocks(turns1[0])
	if think != agentV2GreetThink1+agentV2GreetThink2 {
		t.Errorf("streamed think = %q, want %q", think, agentV2GreetThink1+agentV2GreetThink2)
	}
	if text != agentV2GreetText {
		t.Errorf("streamed text = %q, want %q", text, agentV2GreetText)
	}

	stream2 := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, agentV2TriggerThink+" say that again")
	events2 := drainTeamStream(t, stream2)
	assertTeamStreamWellFormed(t, sessionName, events2)
	turns2 := groupTeamMemberTurns(events2)
	if len(turns2) != 1 || turns2[0].member != "player" {
		t.Fatalf("second player stream turns = %v, want exactly one player turn", turns2)
	}
	if _, text := teamTurnBlocks(turns2[0]); text != agentV2FollowupText {
		t.Errorf("second turn text = %q, want %q (the history keyword condition)", text, agentV2FollowupText)
	}
	if turns1[0].turnID == turns2[0].turnID {
		t.Errorf("both player turns report turn_id %q", turns1[0].turnID)
	}

	// Backfill agrees with the streamed terminal blocks. The activation
	// prelude left earlier player entries behind (the failed init tool entry
	// and its summary), so search the text replies instead of counting all
	// player entries.
	entries := listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName)
	sawGreet, sawFollowup := false, false
	for _, entry := range teamMessagesForMember(entries, "player") {
		switch agentV2MessageText(entry.GetMessage()) {
		case agentV2GreetText:
			sawGreet = true
		case agentV2FollowupText:
			sawFollowup = true
		}
	}
	if !sawGreet || !sawFollowup {
		t.Errorf("backfilled player replies = greet:%v followup:%v, want both turns' terminal text", sawGreet, sawFollowup)
	}
}

// TestAgentV2TeamMemberFailureRecovers covers the Edge-member-failure branch:
// an injected model failure ends the member turn ERROR with a structured
// error (the session stays usable), and a follow-up turn completes normally.
func TestAgentV2TeamMemberFailureRecovers(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName := teamPlayerActivation(t, sutHostURL, sutEnvName, "team-fail-"+uniqueSuffix(), "failure")

	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, agentV2TriggerFail+" break this turn")
	events := drainTeamStream(t, stream)
	assertTeamStreamWellFormed(t, sessionName, events)
	turns := groupTeamMemberTurns(events)
	if len(turns) != 1 || turns[0].member != "player" {
		t.Fatalf("failed-turn stream turns = %v, want exactly one player turn", turns)
	}
	for i, event := range turns[0].events {
		if event.GetBlockStart() != nil {
			t.Errorf("frame %d starts a block — a pre-content failure must produce none", i)
		}
	}
	end := turns[0].events[len(turns[0].events)-1].GetTurnEnd()
	if end.GetStatus() != game.TurnStatus_TURN_STATUS_ERROR {
		t.Fatalf("failed turn ended %v, want ERROR", end.GetStatus())
	}
	if end.GetError() == nil || end.GetError().GetMessage() == "" {
		t.Errorf("turn_end.error = %+v, want a structured error payload", end.GetError())
	}

	// The member survives: a follow-up turn completes.
	stream2 := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, agentV2TriggerPlain+" recover now")
	events2 := drainTeamStream(t, stream2)
	assertTeamStreamWellFormed(t, sessionName, events2)
	turns2 := groupTeamMemberTurns(events2)
	if len(turns2) != 1 || teamTurnEndStatus(turns2[0]) != game.TurnStatus_TURN_STATUS_COMPLETED {
		t.Fatalf("recovery turn = %v, want one COMPLETED player turn", turns2)
	}
	if _, text := teamTurnBlocks(turns2[0]); text != agentV2PlainText {
		t.Errorf("recovery text = %q, want %q", text, agentV2PlainText)
	}
}

// TestAgentV2TeamInterruptedTurnBackfillsTail covers the partial-content
// failure backfill (specs/054-agent-v2-bugfixes/contracts/
// agent-api-changes.md §6 preserved through the team model): the member turn
// streams think+text and only then fails, so ListTeamMessages carries the
// interrupted tail with the streamed prefix and interrupted=true.
func TestAgentV2TeamInterruptedTurnBackfillsTail(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName := teamPlayerActivation(t, sutHostURL, sutEnvName, "team-midfail-"+uniqueSuffix(), "midfail")

	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, agentV2TriggerFailMid+" produce content, then break")
	events := drainTeamStream(t, stream)
	assertTeamStreamWellFormed(t, sessionName, events)
	turns := groupTeamMemberTurns(events)
	if len(turns) != 1 || turns[0].member != "player" {
		t.Fatalf("failed-turn stream turns = %v, want exactly one player turn", turns)
	}
	think, text := teamTurnBlocks(turns[0])
	if think != agentV2FailMidThink || text != agentV2FailMidText {
		t.Errorf("streamed prefix = (%q, %q), want (%q, %q)", think, text, agentV2FailMidThink, agentV2FailMidText)
	}
	if teamTurnEndStatus(turns[0]) != game.TurnStatus_TURN_STATUS_ERROR {
		t.Fatalf("turn ended %v, want ERROR", teamTurnEndStatus(turns[0]))
	}

	// Backfill: the trailing PLAYER entry is the interrupted tail with the
	// streamed prefix.
	entries := listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName)
	playerEntries := teamMessagesForMember(entries, "player")
	if len(playerEntries) == 0 {
		t.Fatal("history has no player entries after the interrupted turn")
	}
	tail := playerEntries[len(playerEntries)-1]
	if !tail.GetMessage().GetInterrupted() {
		t.Errorf("tail entry interrupted = false, want true (FR-005)")
	}
	if got := agentV2MessageThink(tail.GetMessage()); got != agentV2FailMidThink {
		t.Errorf("tail think = %q, want the streamed prefix %q", got, agentV2FailMidThink)
	}
	if got := agentV2MessageText(tail.GetMessage()); got != agentV2FailMidText {
		t.Errorf("tail text = %q, want the streamed prefix %q", got, agentV2FailMidText)
	}
}

// TestAgentV2TeamInvalidInputRejected covers the request-level rejection
// family: empty text and an unknown template map to 400 INVALID_ARGUMENT at
// the /api/v2 surface, and the service stays healthy afterwards.
func TestAgentV2TeamInvalidInputRejected(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName, _ := teamPrep(t, sutHostURL, sutEnvName, "team-invalid-"+uniqueSuffix(), "invalid")

	tests := []struct {
		name    string
		session string
		text    string
	}{
		// The proxy validates empty text before any owner work; an unknown
		// template fails resource-name parsing — both INVALID_ARGUMENT → 400
		// (team-api.md §6).
		{name: "empty text", session: sessionName, text: ""},
		{name: "unknown template", session: "templates/unknown-template/sessions/" + uniqueSuffix(), text: "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := postTeamSendStatus(t, ctx, sutHostURL, sutEnvName, tt.session, tt.text)
			if status != http.StatusBadRequest {
				t.Fatalf("invalid send status = %d (body: %s), want 400", status, body)
			}
		})
	}

	// The service is unaffected: a valid turn completes.
	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamStartMessage)
	events := drainTeamStream(t, stream)
	assertTeamStreamWellFormed(t, sessionName, events)
}

// TestAgentV2TeamConcurrentSessionIsolation covers the per-session isolation
// of the in-memory team state: two sessions materialize and converse
// CONCURRENTLY on the shared agent_v2 instance; each keeps its own turn
// identity and history, with no cross-session bleed.
func TestAgentV2TeamConcurrentSessionIsolation(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	name1 := ensureAgentV2Session(t, sutHostURL, sutEnvName, "team-iso-1-"+uniqueSuffix())
	player1, planner1 := createAgentV2TeamPresetPair(t, ctx, sutHostURL, sutEnvName, "team-iso-1", "isolation one")
	updateAgentV2Team(t, ctx, sutHostURL, sutEnvName, name1, player1.GetName(), planner1.GetName(), "", "")

	name2 := ensureAgentV2Session(t, sutHostURL, sutEnvName, "team-iso-2-"+uniqueSuffix())
	player2, planner2 := createAgentV2TeamPresetPair(t, ctx, sutHostURL, sutEnvName, "team-iso-2", "isolation two")
	updateAgentV2Team(t, ctx, sutHostURL, sutEnvName, name2, player2.GetName(), planner2.GetName(), "", "")

	text1 := teamStartMessage + " session one isolation marker"
	text2 := teamStartMessage + " session two isolation marker"
	stream1 := startTeamSend(t, ctx, sutHostURL, sutEnvName, name1, text1)
	stream2 := startTeamSend(t, ctx, sutHostURL, sutEnvName, name2, text2)
	events1 := waitTeamStream(t, drainTeamStreamAsync(stream1), "session 1 stream")
	events2 := waitTeamStream(t, drainTeamStreamAsync(stream2), "session 2 stream")
	assertTeamStreamWellFormed(t, name1, events1)
	assertTeamStreamWellFormed(t, name2, events2)

	turns1 := groupTeamMemberTurns(events1)
	turns2 := groupTeamMemberTurns(events2)
	if len(turns1) == 0 || len(turns2) == 0 {
		t.Fatalf("concurrent turns = %d/%d, want both sessions to complete", len(turns1), len(turns2))
	}
	if turns1[0].turnID == turns2[0].turnID {
		t.Errorf("both sessions report turn_id %q — turn identity leaked across sessions", turns1[0].turnID)
	}

	// Each history carries only its own marker.
	entries1 := listTeamMessages(t, ctx, sutHostURL, sutEnvName, name1)
	entries2 := listTeamMessages(t, ctx, sutHostURL, sutEnvName, name2)
	saw1, saw2 := false, false
	for _, entry := range entries1 {
		text := agentV2MessageText(entry.GetMessage())
		if text == text2 {
			t.Errorf("session 1 history carries session 2's marker %q", text2)
		}
		if text == text1 {
			saw1 = true
		}
	}
	for _, entry := range entries2 {
		text := agentV2MessageText(entry.GetMessage())
		if text == text1 {
			t.Errorf("session 2 history carries session 1's marker %q", text1)
		}
		if text == text2 {
			saw2 = true
		}
	}
	if !saw1 || !saw2 {
		t.Errorf("marker presence = %v/%v, want both histories to keep their own marker", saw1, saw2)
	}
}
