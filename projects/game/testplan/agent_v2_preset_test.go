// Package testplan contains the agent_v2 team configuration large tests: the
// preset CRUD closed loop over the stateless configuration face, the team
// materialization/refresh semantics over the team singleton, the
// unmaterialized rejection family, and the empty-persona fallback — the US2
// configuration concerns (specs/059-agent-v2-team-mode/contracts/team-api.md
// §2/§6, preset-api.md). Cases are one per concern —
// style/large_test.md §测试组织. Preset state lives in the agent_v2 Mongo
// store, so every assertion round-trips through the gateway's direct
// PresetService routes; the team singleton routes ride the proxy owner
// affinity.
package testplan

import (
	"net/http"
	"strings"
	"testing"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"

	"google.golang.org/protobuf/proto"
)

// presetName builds the full preset resource name under the saolei template.
func presetName(presetID string) string {
	return "templates/" + saoleiTemplateID + "/presets/" + presetID
}

// listContainsPreset returns the listed preset with the given resource name.
func listContainsPreset(list *game.ListPresetsResponse, name string) *game.Preset {
	for _, p := range list.GetPresets() {
		if p.GetName() == name {
			return p
		}
	}
	return nil
}

// TestAgentV2TeamPresetCrudRoundTrip covers the role-pooled preset CRUD
// closed loop through the gateway (US3 场景 1 / preset-api.md): create with a
// REQUIRED role → Get equals the created resource → List carries it and the
// role filter partitions the pools → duplicate caller-id → 409
// ALREADY_EXISTS → update (update_mask=persona) keeps the role → delete →
// Get 404 → List without.
func TestAgentV2TeamPresetCrudRoundTrip(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	id := "preset-crud-" + uniqueSuffix()
	name := presetName(id)

	// create → the response is the stored resource with its server-assigned
	// name, the immutable role, and OUTPUT_ONLY timestamps.
	created := createAgentV2TeamPreset(t, ctx, sutHostURL, sutEnvName, id, "你是扫雷 player（crud persona v1）", "player")
	if created.GetName() != name {
		t.Fatalf("created preset name = %q, want %q", created.GetName(), name)
	}
	if created.GetRole() != "player" {
		t.Errorf("created role = %q, want \"player\" (the scene vocabulary string)", created.GetRole())
	}
	if created.GetPersona() != "你是扫雷 player（crud persona v1）" {
		t.Errorf("created persona = %q, want the posted persona", created.GetPersona())
	}
	if created.GetCreateTime() == nil || created.GetUpdateTime() == nil {
		t.Errorf("created preset timestamps = %q / %q, want server-maintained values", created.GetCreateTime(), created.GetUpdateTime())
	}

	if got := getAgentV2Preset(t, ctx, sutHostURL, sutEnvName, name); got.GetPersona() != created.GetPersona() || got.GetRole() != created.GetRole() {
		t.Errorf("Get after create = {%v %q}, want the created resource", got.GetRole(), got.GetPersona())
	}
	if got := listContainsPreset(listAgentV2Presets(t, ctx, sutHostURL, sutEnvName), name); got == nil {
		t.Fatalf("ListPresets does not contain %q", name)
	}
	// The role filter partitions the pools (ListPresetsRequest.role — a scene
	// role string, preset-api.md §1).
	if got := listContainsPreset(listAgentV2PresetsByRole(t, ctx, sutHostURL, sutEnvName, "player"), name); got == nil {
		t.Errorf("player-pool list does not contain the player preset %q", name)
	}
	if got := listContainsPreset(listAgentV2PresetsByRole(t, ctx, sutHostURL, sutEnvName, "planner"), name); got != nil {
		t.Errorf("planner-pool list contains the player preset %q", name)
	}

	// Duplicate caller-id → 409 ALREADY_EXISTS.
	if _, dupStatus, dupBody := createAgentV2PresetWithRole(t, ctx, sutHostURL, sutEnvName, id, "duplicate", "player"); dupStatus != http.StatusConflict {
		t.Errorf("duplicate create status = %d (body: %s), want 409 ALREADY_EXISTS", dupStatus, dupBody)
	}

	// Update patches only persona; the role is immutable.
	updated := updateAgentV2Preset(t, ctx, sutHostURL, sutEnvName, name, "你是扫雷 player（crud persona v2）")
	if updated.GetPersona() != "你是扫雷 player（crud persona v2）" || updated.GetName() != name {
		t.Errorf("updated preset = {%s %q}, want the patched persona", updated.GetName(), updated.GetPersona())
	}
	if updated.GetRole() != "player" {
		t.Errorf("updated role = %q, want the immutable \"player\"", updated.GetRole())
	}

	// Delete removes the resource: Get 404, List without.
	if status, delBody := deleteAgentV2PresetWithStatus(t, ctx, sutHostURL, sutEnvName, name); status != http.StatusOK {
		t.Fatalf("DELETE preset status = %d (body: %s), want 200", status, delBody)
	}
	if _, status := getAgentV2PresetWithStatus(t, ctx, sutHostURL, sutEnvName, name); status != http.StatusNotFound {
		t.Errorf("Get after delete status = %d, want 404 NOT_FOUND", status)
	}
	if got := listContainsPreset(listAgentV2Presets(t, ctx, sutHostURL, sutEnvName), name); got != nil {
		t.Errorf("List after delete still contains %q", name)
	}
}

// TestAgentV2TeamPresetRoleVocabulary covers the scene role vocabulary on the
// preset face (preset-api.md §1/§2): role is REQUIRED on create and must be a
// known saolei scene role string — a missing or unknown role is 400
// INVALID_ARGUMENT and creates nothing — and the list role filter validates
// the same vocabulary.
func TestAgentV2TeamPresetRoleVocabulary(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	base := "preset-vocab-" + uniqueSuffix()
	tests := []struct {
		name      string
		presetID  string
		roleQuery string
	}{
		{name: "missing role", presetID: base + "-missing"},
		{name: "unknown role", presetID: base + "-unknown", roleQuery: "referee"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := postAgentV2PresetCreate(t, ctx, sutHostURL, sutEnvName, tt.presetID, "你是扫雷 player（vocab）", tt.roleQuery)
			if status != http.StatusBadRequest {
				t.Fatalf("create preset status = %d (body: %s), want 400 INVALID_ARGUMENT", status, body)
			}
			if !strings.Contains(strings.ToLower(string(body)), "known saolei scene role") {
				t.Errorf("rejection body = %s, want the scene-role vocabulary message", body)
			}
			if _, getStatus := getAgentV2PresetWithStatus(t, ctx, sutHostURL, sutEnvName, presetName(tt.presetID)); getStatus != http.StatusNotFound {
				t.Errorf("Get rejected preset status = %d, want 404 (nothing was created)", getStatus)
			}
		})
	}

	// The list filter validates the same vocabulary.
	status, body := listAgentV2PresetsWithStatus(t, ctx, sutHostURL, sutEnvName, "referee")
	if status != http.StatusBadRequest {
		t.Fatalf("list presets with unknown role status = %d (body: %s), want 400 INVALID_ARGUMENT", status, body)
	}
	if !strings.Contains(strings.ToLower(string(body)), "role filter") {
		t.Errorf("list rejection body = %s, want the role-filter message", body)
	}
}

// TestAgentV2TeamMaterializationValidation covers US2 场景 2 and the
// UpdateTeam fail-fast checks (team-api.md §2): the members list carries one
// {role, preset, model?} per member (scene-agnostic proto), an explicit
// catalog model is honored (an empty one resolves to the default glm-5.3),
// the scene's role/preset/model validation rejects with INVALID_ARGUMENT
// without replacing the standing configuration, GetTeam/GetTeamMember agree
// with the stored configuration, and a re-Apply is idempotent.
func TestAgentV2TeamMaterializationValidation(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	sessionName := ensureAgentV2Session(t, sutHostURL, sutEnvName, "team-mat-"+uniqueSuffix())
	player, planner := createAgentV2TeamPresetPair(t, ctx, sutHostURL, sutEnvName, "team-mat", "materialization")

	models := listAgentV2Models(t, ctx, sutHostURL, sutEnvName)
	if len(models.GetModels()) != 2 {
		t.Fatalf("ListModels returned %d entries, want 2 (glm-5.3 + glm-5.3-flash)", len(models.GetModels()))
	}
	if models.GetModels()[0].GetId() != "glm-5.3" {
		t.Errorf("catalog[0] = %q, want glm-5.3 (the default)", models.GetModels()[0].GetId())
	}

	// An explicit catalog id for the player member is honored; the planner
	// resolves to the default.
	model := models.GetModels()[1].GetId()
	team := updateAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName,
		player.GetName(), planner.GetName(), model, "")
	if team.GetName() != agentV2TeamName(sessionName) {
		t.Fatalf("materialized team name = %q, want %q", team.GetName(), agentV2TeamName(sessionName))
	}
	if teamMemberPreset(team, "player") != player.GetName() || teamMemberPreset(team, "planner") != planner.GetName() {
		t.Errorf("team presets = {%q %q}, want {%q %q}", teamMemberPreset(team, "player"), teamMemberPreset(team, "planner"), player.GetName(), planner.GetName())
	}
	if playerMember := teamMemberByRole(team, "player"); playerMember == nil || playerMember.GetModel() != model {
		t.Errorf("player member = %+v, want model %q", playerMember, model)
	}
	if plannerMember := teamMemberByRole(team, "planner"); plannerMember == nil || plannerMember.GetModel() != "glm-5.3" {
		t.Errorf("planner member = %+v, want the default glm-5.3", plannerMember)
	}
	if team.GetDesktopConnected() {
		t.Error("desktop_connected = true with no flow connection, want false")
	}

	// GetTeam / GetTeamMember agree with the stored configuration.
	stored := getAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName)
	if teamMemberPreset(stored, "player") != player.GetName() || teamMemberModel(stored, "player") != model {
		t.Errorf("GetTeam player config = {%q %q}, want {%q %q}", teamMemberPreset(stored, "player"), teamMemberModel(stored, "player"), player.GetName(), model)
	}
	member := getAgentV2TeamMember(t, ctx, sutHostURL, sutEnvName, sessionName, "player")
	if member.GetName() != agentV2MemberName(sessionName, "player") || member.GetRole() != "player" {
		t.Errorf("GetTeamMember(worker) = {%q %v}, want the player member resource", member.GetName(), member.GetRole())
	}
	if member.GetPreset() != player.GetName() || member.GetModel() != model {
		t.Errorf("GetTeamMember player config = {%q %q}, want {%q %q}", member.GetPreset(), member.GetModel(), player.GetName(), model)
	}

	// An empty model resolves to the process default at materialization.
	defaulted := updateAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName, player.GetName(), planner.GetName(), "", "")
	if teamMemberModel(defaulted, "player") != "glm-5.3" || teamMemberModel(defaulted, "planner") != "glm-5.3" {
		t.Errorf("default-materialized models = {%q %q}, want both glm-5.3", teamMemberModel(defaulted, "player"), teamMemberModel(defaulted, "planner"))
	}

	// Re-Apply of the same configuration is idempotent.
	again := updateAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName, player.GetName(), planner.GetName(), "", "")
	if teamMemberPreset(again, "player") != player.GetName() || teamMemberModel(again, "player") != "glm-5.3" {
		t.Errorf("re-Apply config = {%q %q}, want the unchanged configuration", teamMemberPreset(again, "player"), teamMemberModel(again, "player"))
	}

	// Fail-fast validation (Layer 1 structure + Layer 2 scene): unknown
	// model, scene role mismatch, an empty member preset, an unknown preset
	// id, and a non-"members" update_mask all reject with 400
	// INVALID_ARGUMENT and leave the standing configuration untouched
	// (team-api.md §2/§6).
	scene := func(playerMember, plannerMember *game.TeamMember) []*game.TeamMember {
		return []*game.TeamMember{playerMember, plannerMember}
	}
	tests := []struct {
		name       string
		members    []*game.TeamMember
		mask       string
		wantSubstr string
	}{
		{
			name:       "unknown model",
			members:    scene(teamMember("player", player.GetName(), "no-such-model"), teamMember("planner", planner.GetName(), "")),
			wantSubstr: "unknown model",
		},
		{
			name:       "scene role mismatch",
			members:    scene(teamMember("player", planner.GetName(), ""), teamMember("planner", planner.GetName(), "")),
			wantSubstr: "scene check failed",
		},
		{
			name:       "empty member preset",
			members:    scene(teamMember("player", "", ""), teamMember("planner", planner.GetName(), "")),
			wantSubstr: "preset resource name",
		},
		{
			name:       "unknown preset id",
			members:    scene(teamMember("player", presetName("no-such-preset-"+uniqueSuffix()), ""), teamMember("planner", planner.GetName(), "")),
			wantSubstr: "unknown preset",
		},
		{
			name:       "invalid update mask",
			members:    scene(teamMember("player", player.GetName(), ""), teamMember("planner", planner.GetName(), "")),
			mask:       "player_preset",
			wantSubstr: "update_mask paths must be",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, status, body := updateAgentV2TeamMembers(t, ctx, sutHostURL, sutEnvName, sessionName, tt.members, tt.mask)
			if status != http.StatusBadRequest {
				t.Fatalf("UpdateTeam status = %d (body: %s), want 400 INVALID_ARGUMENT", status, body)
			}
			if !strings.Contains(strings.ToLower(string(body)), strings.ToLower(tt.wantSubstr)) {
				t.Errorf("rejection body = %s, want substring %q", body, tt.wantSubstr)
			}
		})
	}
	if after := getAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName); teamMemberPreset(after, "player") != player.GetName() || teamMemberModel(after, "player") != "glm-5.3" {
		t.Errorf("standing config after the rejected updates = {%q %q}, want the untouched materialization", teamMemberPreset(after, "player"), teamMemberModel(after, "player"))
	}
}

// TestAgentV2TeamMembersListRejections covers the generalized materialization
// input's structural and scene validation on a FRESH session (team-api.md §2):
// empty members, a wrong member count, an empty role, an unknown/duplicate
// scene role, a foreign-template preset, and a non-"members" update_mask are
// all rejected with 400 INVALID_ARGUMENT — and because validation is fail-fast
// before any teardown, every reject leaves the session unmaterialized
// (GetTeam NOT_FOUND, no half materialization); a valid members list then
// materializes the same session and echoes the server-constructed roster.
func TestAgentV2TeamMembersListRejections(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	sessionName := ensureAgentV2Session(t, sutHostURL, sutEnvName, "team-members-"+uniqueSuffix())
	player, planner := createAgentV2TeamPresetPair(t, ctx, sutHostURL, sutEnvName, "team-members", "members")

	validRoster := func() []*game.TeamMember {
		return []*game.TeamMember{
			teamMember("player", player.GetName(), ""),
			teamMember("planner", planner.GetName(), ""),
		}
	}
	// Structure: nil/empty list, wrong member count, empty role. Scene:
	// exactly {"player", "planner"} — unknown and duplicate roles both fail.
	// Resource: a foreign-template (or malformed) preset name fails the
	// resource-name check. Mask: only "members" is a legal path.
	tests := []struct {
		name       string
		members    []*game.TeamMember
		mask       string
		wantSubstr string
	}{
		{name: "empty members", members: nil, wantSubstr: "members must not be empty"},
		{name: "single member", members: []*game.TeamMember{teamMember("player", player.GetName(), "")}, wantSubstr: "exactly 2 members"},
		{name: "three members", members: append(validRoster(), teamMember("player", player.GetName(), "")), wantSubstr: "exactly 2 members"},
		{name: "empty role", members: []*game.TeamMember{teamMember("", player.GetName(), ""), teamMember("planner", planner.GetName(), "")}, wantSubstr: "non-empty role"},
		{name: "unknown scene role", members: []*game.TeamMember{teamMember("player", player.GetName(), ""), teamMember("referee", planner.GetName(), "")}, wantSubstr: "must be exactly"},
		{name: "duplicate scene role", members: []*game.TeamMember{teamMember("player", player.GetName(), ""), teamMember("player", player.GetName(), "")}, wantSubstr: "must be exactly"},
		{name: "foreign template preset", members: []*game.TeamMember{teamMember("player", "templates/other/presets/x", ""), teamMember("planner", planner.GetName(), "")}, wantSubstr: "preset resource name"},
		{name: "invalid update mask", members: validRoster(), mask: "player_preset", wantSubstr: "update_mask paths must be"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, status, body := updateAgentV2TeamMembers(t, ctx, sutHostURL, sutEnvName, sessionName, tt.members, tt.mask)
			if status != http.StatusBadRequest {
				t.Fatalf("UpdateTeam status = %d (body: %s), want 400 INVALID_ARGUMENT", status, body)
			}
			if !strings.Contains(strings.ToLower(string(body)), strings.ToLower(tt.wantSubstr)) {
				t.Errorf("rejection body = %s, want substring %q", body, tt.wantSubstr)
			}
			if getStatus, getBody := getAgentV2TeamWithStatus(t, ctx, sutHostURL, sutEnvName, sessionName); getStatus != http.StatusNotFound {
				t.Errorf("GetTeam after the rejected update status = %d (body: %s), want 404 NOT_FOUND (无半物化)", getStatus, getBody)
			}
		})
	}

	// A valid members list materializes the same session and echoes the
	// roster with the server-constructed member names.
	team := updateAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName, player.GetName(), planner.GetName(), "", "")
	if got := team.GetMembers(); len(got) != 2 {
		t.Fatalf("materialized members = %d, want 2", len(got))
	}
	for _, member := range team.GetMembers() {
		if member.GetRole() != "player" && member.GetRole() != "planner" {
			t.Errorf("materialized member role = %q, want a scene role string", member.GetRole())
		}
		if member.GetName() != agentV2MemberName(sessionName, member.GetRole()) {
			t.Errorf("materialized member name = %q, want %q", member.GetName(), agentV2MemberName(sessionName, member.GetRole()))
		}
		if member.GetPreset() == "" {
			t.Errorf("materialized member %q carries an empty preset", member.GetRole())
		}
	}
}

// TestAgentV2TeamUpdateRefreshRebuilds covers the refresh memory-clear
// (US2 场景 7 first half): a conversed team's history is wiped by re-Update
// while the singleton stays materialized with the preserved create_time, and
// the rebuilt team is conversational again.
func TestAgentV2TeamUpdateRefreshRebuilds(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName, first := teamPrep(t, sutHostURL, sutEnvName, "team-rebuild-"+uniqueSuffix(), "rebuild")

	// One Send populates the merged history (planner + player turns).
	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamStartMessage)
	drainTeamStream(t, stream)
	if got := listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName); len(got) == 0 {
		t.Fatal("history is empty before the refresh — the refresh assertion would be vacuous")
	}

	refreshed := updateAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName,
		teamMemberPreset(first, "player"), teamMemberPreset(first, "planner"), "", "")
	if !proto.Equal(first.GetCreateTime(), refreshed.GetCreateTime()) {
		t.Errorf("create_time after refresh = %v, want the preserved %v", refreshed.GetCreateTime(), first.GetCreateTime())
	}
	if got := listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName); len(got) != 0 {
		t.Errorf("history after refresh = %d entries, want 0 (短期记忆清空)", len(got))
	}
	stored := getAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName)
	if teamMemberPreset(stored, "player") != teamMemberPreset(first, "player") || teamMemberPreset(stored, "planner") != teamMemberPreset(first, "planner") {
		t.Errorf("stored presets after refresh = {%q %q}, want the unchanged configuration", teamMemberPreset(stored, "player"), teamMemberPreset(stored, "planner"))
	}

	// The rebuilt team is conversational again.
	stream2 := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamStartMessage)
	events2 := drainTeamStream(t, stream2)
	assertTeamStreamWellFormed(t, sessionName, events2)
	if turns := groupTeamMemberTurns(events2); len(turns) == 0 || turns[0].member != "planner" {
		t.Fatalf("post-refresh turns = %v, want the planner opening", turns)
	}
}

// TestAgentV2TeamSendUnmaterializedRejected covers US2 场景 1 / the
// unmaterialized rejection family (team-api.md §3/§6): a session that never
// touched UpdateTeam has no owner — Send/GetTeam/ListTeamMessages/
// ListMemberMessages/Cancel all answer 404 NOT_FOUND; a session whose owner
// WAS allocated by a failed fail-fast UpdateTeam answers GetTeam 404 and
// Send/Cancel 400 FAILED_PRECONDITION.
func TestAgentV2TeamSendUnmaterializedRejected(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	// Layer 1 — never materialized: no owner exists.
	ghost := ensureAgentV2Session(t, sutHostURL, sutEnvName, "team-ghost-"+uniqueSuffix())
	if status, body := postTeamSendStatus(t, ctx, sutHostURL, sutEnvName, ghost, teamStartMessage); status != http.StatusNotFound {
		t.Errorf("Send on never-materialized session status = %d (body: %s), want 404 NOT_FOUND", status, body)
	}
	if status, _ := getAgentV2TeamWithStatus(t, ctx, sutHostURL, sutEnvName, ghost); status != http.StatusNotFound {
		t.Errorf("GetTeam on never-materialized session status = %d, want 404 NOT_FOUND", status)
	}
	if status, _ := listTeamMessagesWithStatus(t, ctx, sutHostURL, sutEnvName, ghost); status != http.StatusNotFound {
		t.Errorf("ListTeamMessages on never-materialized session status = %d, want 404 NOT_FOUND", status)
	}
	if status, _ := listMemberMessagesWithStatus(t, ctx, sutHostURL, sutEnvName, ghost, "player"); status != http.StatusNotFound {
		t.Errorf("ListMemberMessages on never-materialized session status = %d, want 404 NOT_FOUND", status)
	}
	if status, body := postTeamCancel(t, ctx, sutHostURL, sutEnvName, ghost); status != http.StatusNotFound {
		t.Errorf("Cancel on never-materialized session status = %d (body: %s), want 404 NOT_FOUND", status, body)
	}

	// Layer 2 — owner without a materialized team: the fail-fast unknown-model
	// UpdateTeam allocates the owner, then rejects without materializing.
	sessionName := ensureAgentV2Session(t, sutHostURL, sutEnvName, "team-unmat-"+uniqueSuffix())
	player, planner := createAgentV2TeamPresetPair(t, ctx, sutHostURL, sutEnvName, "team-unmat", "unmaterialized")
	_, failedStatus, failedBody := updateAgentV2TeamWithStatus(t, ctx, sutHostURL, sutEnvName, sessionName,
		player.GetName(), planner.GetName(), "no-such-model", "")
	if failedStatus != http.StatusBadRequest {
		t.Fatalf("UpdateTeam with unknown model status = %d (body: %s), want 400 INVALID_ARGUMENT", failedStatus, failedBody)
	}
	if status, _ := getAgentV2TeamWithStatus(t, ctx, sutHostURL, sutEnvName, sessionName); status != http.StatusNotFound {
		t.Errorf("GetTeam after failed materialization status = %d, want 404 (无半物化)", status)
	}
	status, body := postTeamSendStatus(t, ctx, sutHostURL, sutEnvName, sessionName, teamStartMessage)
	if status != http.StatusBadRequest {
		t.Errorf("Send on owner-without-team status = %d (body: %s), want 400 FAILED_PRECONDITION", status, body)
	} else if !strings.Contains(strings.ToLower(string(body)), "materialized") {
		t.Errorf("unmaterialized Send body = %s, want the materialization precondition message", body)
	}
	if status, body := postTeamCancel(t, ctx, sutHostURL, sutEnvName, sessionName); status != http.StatusBadRequest {
		t.Errorf("Cancel on owner-without-team status = %d (body: %s), want 400 FAILED_PRECONDITION", status, body)
	}
}

// TestAgentV2TeamEmptyPersonaFallback covers US3 场景 3 (quickstart §2
// agent-v2-preset: 空 prompt 回退 base): a preset with an empty persona
// materializes fine (empty triggers the role default base) and the fallback
// persona still carries the role's system-prompt anchor, so the deterministic
// team chain completes.
func TestAgentV2TeamEmptyPersonaFallback(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	sessionName := ensureAgentV2Session(t, sutHostURL, sutEnvName, "team-empty-"+uniqueSuffix())
	player := createAgentV2TeamPreset(t, ctx, sutHostURL, sutEnvName, "team-empty-player-"+uniqueSuffix(), "", "player")
	planner := createAgentV2TeamPreset(t, ctx, sutHostURL, sutEnvName, "team-empty-planner-"+uniqueSuffix(), "", "planner")
	if player.GetPersona() != "" || planner.GetPersona() != "" {
		t.Fatalf("created personas = {%q %q}, want empty", player.GetPersona(), planner.GetPersona())
	}
	team := updateAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName, player.GetName(), planner.GetName(), "", "")
	if len(team.GetMembers()) != 2 {
		t.Fatalf("materialized members = %d, want 2 (empty personas must fall back, not fail)", len(team.GetMembers()))
	}

	// The fallback personas ride the model context end to end: the planner
	// opening entry (system_keyword 你是扫雷 planner) matches, and the
	// structurally driven player completes its turn (nodesktop branch).
	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamStartMessage)
	events := drainTeamStream(t, stream)
	assertTeamStreamWellFormed(t, sessionName, events)
	turns := groupTeamMemberTurns(events)
	if len(turns) < 2 {
		t.Fatalf("member turns = %d, want the planner opening + player continuation", len(turns))
	}
	if _, text := teamTurnBlocks(turns[0]); text != teamPlannerOpeningText {
		t.Errorf("opening text = %q, want %q (the fallback planner persona must match)", text, teamPlannerOpeningText)
	}
	if _, text := teamTurnBlocks(turns[1]); text != agentV2NodesktopSummary {
		t.Errorf("player turn text = %q, want %q (the fallback player persona must match)", text, agentV2NodesktopSummary)
	}
}

// TestAgentV2TeamDesktopConnected covers the GetTeam desktop_connected fact
// (team-api.md §1): a materialized session with no flow connection reports
// false; attaching a flow connection flips it to true.
func TestAgentV2TeamDesktopConnected(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName, _ := teamPrep(t, sutHostURL, sutEnvName, "team-conn-"+uniqueSuffix(), "connection")

	if got := getAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName); got.GetDesktopConnected() {
		t.Error("desktop_connected = true with no flow connection, want false")
	}

	flow, _ := dialAgentV2FlowProbed(t, ctx, sutHostURL, sutEnvName, sessionIDFromName(t, sessionName))
	defer flow.Close()
	if got := getAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName); !got.GetDesktopConnected() {
		t.Error("desktop_connected = false with a live flow connection attached, want true")
	}
}
