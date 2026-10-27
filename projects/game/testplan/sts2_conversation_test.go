// Package testplan pins the sts2 minimal-wiring acceptance suite: the
// single-main-agent conversation face on the slim fake-llm topology —
// materialization and roster, the fake-llm ping turn with its merged and
// member-view projections, the quiescence/refresh/cancel windows, and the
// fixed-roster NOT_FOUND face. The fake-llm trigger and expected reply of
// projects/game/fake-llm/service/testdata/sts2_main.yaml are pinned below.
//
// Contract: specs/080-host-main-sub-model/contracts/testplan-game-sts2.md
// §3/§4; face semantics: specs/080-host-main-sub-model/contracts/face-main-sub.md.
package testplan

import (
	"context"
	"net/http"
	"testing"
	"time"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"

	"google.golang.org/protobuf/proto"
)

const (
	// sts2PingTrigger is the user-message anchor that selects the
	// agent-v2-sts2-ping template (its `keywords` entry).
	sts2PingTrigger = "sts2-ping"
	// sts2PingReply is the agent-v2-sts2-ping template's `text`, asserted as
	// the completed main member turn's text.
	sts2PingReply = "sts2 主 agent 应答：sts2-ping 已收到，对话回合正常完成。"
	// sts2MainMember is the sts2 scene's single face role and preset-pool
	// vocabulary (projects/game/agent_v2/src/scenes.ts;
	// specs/080-host-main-sub-model/contracts/face-main-sub.md §1).
	sts2MainMember = "main"
	// sts2StallTrigger selects the shared stall-mid-reasoning template of
	// projects/game/fake-llm/service/testdata/stall_recovery.yaml. That
	// template is persona-free, so it matches the sts2 main agent, and it
	// blocks after its reasoning delta until the caller intervenes — the
	// deterministic in-flight window the refresh/cancel variants need
	// (specs/080-host-main-sub-model/contracts/testplan-game-sts2.md §3 case 3).
	sts2StallTrigger = "stall now"
)

// TestSts2MainMaterializeAndRoster covers
// specs/080-host-main-sub-model/contracts/testplan-game-sts2.md §3 case 1: the
// single `main` member materializes onto the sts2 scene (no server-added
// member), the roster echoes the configured name/role/preset/model with
// `main` as the static activation, the unmaterialized face answers NOT_FOUND,
// and every roster shape outside the sts2 scene is INVALID_ARGUMENT with no
// half-materialized team left behind.
func TestSts2MainMaterializeAndRoster(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	// given: an sts2 session and one `main`-pool preset created with an EMPTY
	// persona. The empty persona keeps the main pool template's prefix — the
	// anchor the fake-llm `system_keywords` matches on
	// (specs/080-host-main-sub-model/contracts/testplan-game-sts2.md §4;
	// common/js/dsh-plugins/preset-authoring/src/derive.ts).
	sessionName := ensureAgentV2Session(t, sutHostURL, sutEnvName, sts2TemplateID, "sts2-roster-"+uniqueSuffix())
	preset, presetStatus, presetBody := createAgentV2PresetWithRole(t, ctx, sutHostURL, sutEnvName, sts2TemplateID, "sts2-main-"+uniqueSuffix(), "", sts2MainMember)
	if presetStatus != http.StatusOK {
		t.Fatalf("create sts2 main preset status=%d, body=%s", presetStatus, presetBody)
	}
	models := listAgentV2Models(t, ctx, sutHostURL, sutEnvName)
	if len(models.GetModels()) == 0 {
		t.Fatal("model catalog is empty, want the deployment's configured providers")
	}
	model := models.GetModels()[0].GetId()

	// then: before materialization the team singleton does not exist.
	if status, body := getAgentV2TeamWithStatus(t, ctx, sutHostURL, sutEnvName, sessionName); status != http.StatusNotFound {
		t.Fatalf("GetTeam before materialization status=%d (body: %s), want 404 NOT_FOUND", status, body)
	}

	// when: UpdateTeam carries a roster shape outside the sts2 scene (exactly
	// one `main` member). A same-template preset-role mismatch is unreachable
	// through the public API: the sts2 preset pool only admits the `main` role
	// (projects/game/agent_v2/src/server.ts scene role vocabulary), so every
	// creatable sts2 preset already matches. The closest reachable negative is
	// a cross-template preset, which the structural layer rejects on the
	// template mismatch before any preset read; the role-equality branch itself
	// is covered by projects/game/agent_v2/src/session.test.ts.
	saoleiPreset := createAgentV2Preset(t, ctx, sutHostURL, sutEnvName, "sts2-cross-"+uniqueSuffix(), "")
	rejections := []struct {
		name    string
		members []*game.TeamMember
	}{
		{
			name: "two members",
			members: []*game.TeamMember{
				teamMember(sts2MainMember, preset.GetName(), ""),
				teamMember(sts2MainMember, preset.GetName(), ""),
			},
		},
		{
			name:    "non-main role",
			members: []*game.TeamMember{teamMember("player", preset.GetName(), "")},
		},
		{
			name:    "cross-template preset",
			members: []*game.TeamMember{teamMember(sts2MainMember, saoleiPreset.GetName(), "")},
		},
	}
	for _, rejection := range rejections {
		t.Run(rejection.name, func(t *testing.T) {
			// then: the scene validation rejects the roster with
			// INVALID_ARGUMENT before any teardown.
			_, status, body := updateAgentV2TeamMembers(t, ctx, sutHostURL, sutEnvName, sessionName, rejection.members, "")
			if status != http.StatusBadRequest {
				t.Fatalf("UpdateTeam (%s) status=%d (body: %s), want 400 INVALID_ARGUMENT", rejection.name, status, body)
			}
			// and: the rejected attempt leaves no half-materialized team.
			if status, body := getAgentV2TeamWithStatus(t, ctx, sutHostURL, sutEnvName, sessionName); status != http.StatusNotFound {
				t.Errorf("GetTeam after the rejected %s status=%d (body: %s), want 404 NOT_FOUND", rejection.name, status, body)
			}
		})
	}

	// when: the single `main` member materializes with a catalog model.
	team := updateSts2MainTeam(t, ctx, sutHostURL, sutEnvName, sessionName, preset.GetName(), model)

	// then: the roster is exactly the configured member (no announcer) and
	// echoes every configured field, with `main` as the static activation
	// (specs/080-host-main-sub-model/contracts/face-main-sub.md §1/§3).
	member := assertSts2SingleMainRoster(t, sessionName, team)
	if got := member.GetPreset(); got != preset.GetName() {
		t.Errorf("main member preset = %q, want %q", got, preset.GetName())
	}
	if got := member.GetModel(); got != model {
		t.Errorf("main member model = %q, want the configured %q", got, model)
	}
}

// TestSts2MainConversationTurn covers
// specs/080-host-main-sub-model/contracts/testplan-game-sts2.md §3 case 2: one
// Send carrying the fixture trigger produces exactly one COMPLETED `main` turn
// whose text is the fixture reply, the stream fixes the user message as a
// team_message frame, and the merged and member-view List faces are the same
// projections.
func TestSts2MainConversationTurn(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	// given: a materialized single-main sts2 session.
	ctx, sessionName, _ := sts2MainPrep(t, sutHostURL, sutEnvName, "sts2-turn-"+uniqueSuffix())

	// when: the user sends the fake-llm trigger.
	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, sts2PingTrigger)
	events := drainTeamStream(t, stream)

	// then: the stream carries exactly the completed main turn.
	assertTeamStreamWellFormed(t, sessionName, events)
	assertSts2SingleCompletedMainTurn(t, events, sts2PingReply)
	assertSingleModelStep(t, groupTeamMemberTurns(events)[0])

	// then: the user message is fixed at acceptance and the main output is the
	// next merged entry.
	messages := teamStreamMessages(events)
	if len(messages) != 2 {
		t.Fatalf("team_message frames = %d, want 2 (user input + main output)", len(messages))
	}
	if got := messages[0].GetMember(); got != "user" {
		t.Errorf("team_message[0] member = %q, want %q", got, "user")
	}
	if got := agentV2MessageText(messages[0].GetMessage()); got != sts2PingTrigger {
		t.Errorf("team_message[0] text = %q, want %q", got, sts2PingTrigger)
	}
	if got := messages[1].GetMember(); got != sts2MainMember {
		t.Errorf("team_message[1] member = %q, want %q", got, sts2MainMember)
	}
	if got := agentV2MessageText(messages[1].GetMessage()); got != sts2PingReply {
		t.Errorf("team_message[1] text = %q, want %q", got, sts2PingReply)
	}
	assertTeamStreamMessagesMatchList(t, ctx, sutHostURL, sutEnvName, sessionName, events)

	// then: the main member's view consumed the input live and holds the same
	// conversation; its own output is the same message as the merged entry.
	assertTeamMemberViewLive(t, ctx, sutHostURL, sutEnvName, sessionName, events, sts2MainMember, "user")
	view := listMemberMessages(t, ctx, sutHostURL, sutEnvName, sessionName, sts2MainMember)
	assertMemberViewPerspective(t, "ListMemberMessages(main)", view, sts2MainMember)
	if len(view) != 2 {
		t.Fatalf("main view entries = %d, want 2 (user input + own output)", len(view))
	}
	if got := view[0].GetSender(); got != "user" {
		t.Errorf("main view[0] sender = %q, want %q", got, "user")
	}
	if got := agentV2MessageText(view[0].GetMessage()); got != sts2PingTrigger {
		t.Errorf("main view[0] text = %q, want %q", got, sts2PingTrigger)
	}
	if got := view[1].GetSender(); got != sts2MainMember {
		t.Errorf("main view[1] sender = %q, want %q", got, sts2MainMember)
	}
	if got := agentV2MessageText(view[1].GetMessage()); got != sts2PingReply {
		t.Errorf("main view[1] text = %q, want %q", got, sts2PingReply)
	}
	assertMergeMatchesMemberViews(t, listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName), map[string][]*game.MemberViewMessage{
		sts2MainMember: view,
	})
}

// TestSts2MainQuiescenceAndRefresh covers
// specs/080-host-main-sub-model/contracts/testplan-game-sts2.md §3 case 3: the
// static point emits nothing on its own; a quiescent refresh preserves
// create_time, updates update_time, clears both history projections and
// rebuilds a conversational roster; an in-flight refresh settles the running
// turn ABORTED; a cancel settles it CANCELED, freezes the orchestration, and
// the next Send resumes the single main member.
func TestSts2MainQuiescenceAndRefresh(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	// given: a materialized single-main sts2 session that completed one turn.
	ctx, sessionName, preset := sts2MainPrep(t, sutHostURL, sutEnvName, "sts2-quiescence-"+uniqueSuffix())
	first := getAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName)

	events := drainTeamStream(t, startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, sts2PingTrigger))
	assertTeamStreamWellFormed(t, sessionName, events)
	assertSts2SingleCompletedMainTurn(t, events, sts2PingReply)

	// then: the static point emits nothing on its own — the sts2 scene has no
	// structural continuation, so the merged sequence stays put.
	staticCount := len(listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName))
	time.Sleep(3 * time.Second)
	if got := len(listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName)); got != staticCount {
		t.Errorf("merged entries after the static wait = %d, want the unchanged %d (no self-driven turn)", got, staticCount)
	}
	if got := teamActiveMember(t, ctx, sutHostURL, sutEnvName, sessionName); got != sts2MainMember {
		t.Errorf("active_member at the static point = %q, want the %q activation", got, sts2MainMember)
	}

	// when: a quiescent refresh re-materializes with the same preset.
	refreshed := updateSts2MainTeam(t, ctx, sutHostURL, sutEnvName, sessionName, preset.GetName(), "")

	// then: create_time survives, update_time advances, both List faces start
	// a new lifecycle, and the single-main roster is rebuilt.
	if !proto.Equal(first.GetCreateTime(), refreshed.GetCreateTime()) {
		t.Errorf("create_time after refresh = %v, want the preserved %v", refreshed.GetCreateTime(), first.GetCreateTime())
	}
	if refreshed.GetUpdateTime().AsTime().Before(first.GetUpdateTime().AsTime()) {
		t.Errorf("update_time after refresh = %v, want >= the first materialization's %v", refreshed.GetUpdateTime(), first.GetUpdateTime())
	}
	assertSts2SingleMainRoster(t, sessionName, refreshed)
	if got := listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName); len(got) != 0 {
		t.Errorf("merged entries after refresh = %d, want 0 (short-term memory cleared)", len(got))
	}
	if got := listMemberMessages(t, ctx, sutHostURL, sutEnvName, sessionName, sts2MainMember); len(got) != 0 {
		t.Errorf("main view entries after refresh = %d, want 0 (new lifecycle)", len(got))
	}

	// and: the rebuilt session is conversational again.
	events = drainTeamStream(t, startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, sts2PingTrigger))
	assertTeamStreamWellFormed(t, sessionName, events)
	assertSts2SingleCompletedMainTurn(t, events, sts2PingReply)

	// when: a refresh lands while a main turn is in flight (the stall template
	// keeps the turn running until the caller intervenes).
	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, sts2StallTrigger)
	waitSts2TurnStart(t, stream)
	if got := teamActiveMember(t, ctx, sutHostURL, sutEnvName, sessionName); got != sts2MainMember {
		t.Errorf("active_member during the in-flight turn = %q, want the driving %q member", got, sts2MainMember)
	}
	refreshedInFlight := updateSts2MainTeam(t, ctx, sutHostURL, sutEnvName, sessionName, preset.GetName(), "")

	// then: the old lifecycle's in-flight turn settles ABORTED and the rebuilt
	// lifecycle starts empty.
	events = waitTeamStream(t, drainTeamStreamAsync(stream), "sts2 refresh-aborted stream")
	if last := events[len(events)-1].GetTurnEnd(); last == nil || last.GetStatus() != game.TurnStatus_TURN_STATUS_ABORTED {
		t.Fatalf("in-flight main turn terminal after refresh = %+v, want turn_end{ABORTED}", last)
	}
	assertSts2SingleMainRoster(t, sessionName, refreshedInFlight)
	if got := listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName); len(got) != 0 {
		t.Errorf("merged entries after the in-flight refresh = %d, want 0 (voided lifecycle)", len(got))
	}

	// when: a cancel lands while a main turn is in flight.
	stream = startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, sts2StallTrigger)
	waitSts2TurnStart(t, stream)
	if status, body := postTeamCancel(t, ctx, sutHostURL, sutEnvName, sessionName); status != http.StatusOK {
		t.Fatalf("cancel status=%d (body: %s), want 200", status, body)
	}

	// then: the in-flight turn settles CANCELED; the cancel froze the
	// orchestration (the unconsumed input stays history only) and the next
	// Send resumes the single main member.
	events = waitTeamStream(t, drainTeamStreamAsync(stream), "sts2 canceled stream")
	if last := events[len(events)-1].GetTurnEnd(); last == nil || last.GetStatus() != game.TurnStatus_TURN_STATUS_CANCELED {
		t.Fatalf("in-flight main turn terminal after cancel = %+v, want turn_end{CANCELED}", last)
	}
	if got := teamActiveMember(t, ctx, sutHostURL, sutEnvName, sessionName); got != sts2MainMember {
		t.Errorf("active_member after cancel = %q, want the %q activation", got, sts2MainMember)
	}
	// and: the frozen orchestration does not drive the held input on its own.
	frozenCount := len(listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName))
	time.Sleep(2 * time.Second)
	if got := len(listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName)); got != frozenCount {
		t.Errorf("merged entries while frozen = %d, want the unchanged %d (no self-driven turn)", got, frozenCount)
	}
	events = drainTeamStream(t, startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, sts2PingTrigger))
	assertTeamStreamWellFormed(t, sessionName, events)
	assertSts2SingleCompletedMainTurn(t, events, sts2PingReply)
}

// TestSts2NonRosterMemberNotFound covers
// specs/080-host-main-sub-model/contracts/testplan-game-sts2.md §3 case 4: the
// materialized roster is fixed to `main`, so every other role — the saolei
// members and the announcer included — answers NOT_FOUND on both member faces
// (specs/080-host-main-sub-model/contracts/face-main-sub.md §2).
func TestSts2NonRosterMemberNotFound(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	// given: a materialized single-main sts2 session.
	ctx, sessionName, _ := sts2MainPrep(t, sutHostURL, sutEnvName, "sts2-roster-404-"+uniqueSuffix())

	// then: only the `main` member resource exists.
	if _, status, body := getAgentV2TeamMemberWithStatus(t, ctx, sutHostURL, sutEnvName, sessionName, sts2MainMember); status != http.StatusOK {
		t.Fatalf("GetTeamMember(main) status=%d (body: %s), want 200", status, body)
	}

	for _, role := range []string{agentV2AnnouncerMember, "player", "planner", "room-1"} {
		t.Run(role, func(t *testing.T) {
			// when/then: a non-roster role has no member resource.
			if _, status, body := getAgentV2TeamMemberWithStatus(t, ctx, sutHostURL, sutEnvName, sessionName, role); status != http.StatusNotFound {
				t.Errorf("GetTeamMember(%s) status=%d (body: %s), want 404 NOT_FOUND", role, status, body)
			}
			// and: it has no member view either.
			if status, body := listMemberMessagesWithStatus(t, ctx, sutHostURL, sutEnvName, sessionName, role); status != http.StatusNotFound {
				t.Errorf("ListMemberMessages(%s) status=%d (body: %s), want 404 NOT_FOUND", role, status, body)
			}
		})
	}
}

// sts2MainPrep arranges one sts2 session end to end: the session resource, one
// `main`-pool preset created with an EMPTY persona (the empty persona keeps
// the main pool template's prefix — the fake-llm `system_keywords` anchor; a
// non-empty persona replaces it and the fixture silently misses,
// specs/080-host-main-sub-model/contracts/testplan-game-sts2.md §4), and the
// single-member materialization with the deployment default model.
func sts2MainPrep(t *testing.T, sutHostURL, sutEnvName, sessionID string) (context.Context, string, *game.Preset) {
	t.Helper()

	ctx := traceContext(t)
	sessionName := ensureAgentV2Session(t, sutHostURL, sutEnvName, sts2TemplateID, sessionID)
	preset, status, body := createAgentV2PresetWithRole(t, ctx, sutHostURL, sutEnvName, sts2TemplateID, "sts2-main-"+uniqueSuffix(), "", sts2MainMember)
	if status != http.StatusOK {
		t.Fatalf("create sts2 main preset status=%d, body=%s", status, body)
	}
	updateSts2MainTeam(t, ctx, sutHostURL, sutEnvName, sessionName, preset.GetName(), "")
	return ctx, sessionName, preset
}

// waitSts2TurnStart reads stream frames until the in-flight main turn's start
// frame arrives, so the caller can intervene (refresh/cancel) while the turn
// runs.
func waitSts2TurnStart(t *testing.T, stream *teamStream) {
	t.Helper()

	for {
		if event := nextTeamEvent(t, stream.Scanner); event.GetTurnStart() != nil {
			return
		}
	}
}

// assertSts2SingleMainRoster checks the sts2 face roster: exactly the one
// `main` agent member (no server-added announcer), its member resource name,
// and the static `main` activation
// (specs/080-host-main-sub-model/contracts/face-main-sub.md §1/§2). It
// returns the roster member for the caller's configuration assertions.
func assertSts2SingleMainRoster(t *testing.T, sessionName string, team *game.Team) *game.TeamMember {
	t.Helper()

	if got := team.GetName(); got != agentV2TeamName(sessionName) {
		t.Errorf("team name = %q, want %q", got, agentV2TeamName(sessionName))
	}
	if got := len(team.GetMembers()); got != 1 {
		t.Fatalf("team members = %d, want 1 (the single %q member, no announcer)", got, sts2MainMember)
	}
	member := team.GetMembers()[0]
	if got := member.GetRole(); got != sts2MainMember {
		t.Errorf("main member role = %q, want %q", got, sts2MainMember)
	}
	if got := member.GetName(); got != agentV2MemberName(sessionName, sts2MainMember) {
		t.Errorf("main member name = %q, want %q", got, agentV2MemberName(sessionName, sts2MainMember))
	}
	if member.GetPreset() == "" {
		t.Error("main member preset is empty, want the configured preset resource name")
	}
	if got := team.GetActiveMember(); got != sts2MainMember {
		t.Errorf("active_member = %q, want %q", got, sts2MainMember)
	}
	return member
}

// assertSts2SingleCompletedMainTurn checks the sts2 main turn shape: exactly
// one `main` turn, settled COMPLETED, whose text is wantText
// (specs/080-host-main-sub-model/contracts/testplan-game-sts2.md §3 case 2).
func assertSts2SingleCompletedMainTurn(t *testing.T, events []*game.ChatEvent, wantText string) {
	t.Helper()

	turns := groupTeamMemberTurns(events)
	if len(turns) != 1 {
		t.Fatalf("member turns = %d, want 1 (the single main turn)", len(turns))
	}
	if got := turns[0].member; got != sts2MainMember {
		t.Fatalf("turn member = %q, want %q", got, sts2MainMember)
	}
	if status := teamTurnEndStatus(turns[0]); status != game.TurnStatus_TURN_STATUS_COMPLETED {
		t.Errorf("main turn terminal = %v, want COMPLETED", status)
	}
	if _, text := teamTurnBlocks(turns[0]); text != wantText {
		t.Errorf("main turn text = %q, want %q", text, wantText)
	}
}
