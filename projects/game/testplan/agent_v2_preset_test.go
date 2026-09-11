// Package testplan contains the agent_v2 team configuration large tests: the
// preset CRUD closed loop over the stateless configuration face, the team
// materialization/refresh semantics over the team singleton, the
// unmaterialized rejection family, the store-derivation chain, and the
// empty-persona fallback — the US2 configuration concerns
// (specs/059-agent-v2-team-mode/contracts/team-api.md §2/§6, preset-api.md;
// the 060 revision of the authoring/compose semantics:
// specs/060-agent-v2-team-optimize/contracts/preset-derivation.md). Cases
// are one per concern — style/large_test.md §测试组织. Preset state lives in
// the agent_v2 Mongo store, so every assertion round-trips through the
// gateway's direct PresetService routes; the team singleton routes ride the
// proxy owner affinity.
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

// TestAgentV2TeamPresetStoreDerivationChain covers quickstart V2 场景 2 and
// the 060 preset-derivation contract
// (specs/060-agent-v2-team-optimize/contracts/preset-derivation.md §1/§2):
// the API create writes only the Mongo store record, UpdateTeam references
// that record, and the materialized member's effective system prompt carries
// exactly the stored persona. Editing the record and re-materializing
// re-derives the prompt from the new record — no separately maintained
// composition copy can diverge from the store (the absence of a roster copy
// path is pinned at the unit level by preset-authoring's derive/index tests).
func TestAgentV2TeamPresetStoreDerivationChain(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	sessionName := ensureAgentV2Session(t, sutHostURL, sutEnvName, "team-derive-"+uniqueSuffix())
	personaV1 := "你是扫雷 player（store 派生 v1）"
	personaV2 := "你是扫雷 player（store 派生 v2）"
	plannerPersona := "你是扫雷 planner（store 派生）"
	player := createAgentV2TeamPreset(t, ctx, sutHostURL, sutEnvName, "team-derive-player-"+uniqueSuffix(), personaV1, "player")
	planner := createAgentV2TeamPreset(t, ctx, sutHostURL, sutEnvName, "team-derive-planner-"+uniqueSuffix(), plannerPersona, "planner")

	// The create is store-only: the read-back record is the sole persistent
	// fact the materialization consumes.
	if got := getAgentV2Preset(t, ctx, sutHostURL, sutEnvName, player.GetName()); got.GetPersona() != personaV1 || got.GetRole() != "player" {
		t.Fatalf("store record after create = {%q %q}, want {%q player}", got.GetPersona(), got.GetRole(), personaV1)
	}

	// UpdateTeam references the record; the materialized member's effective
	// system prompt carries the stored persona (derived at use time).
	updateAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName, player.GetName(), planner.GetName(), "", "")
	playerPrompt := getAgentV2TeamMember(t, ctx, sutHostURL, sutEnvName, sessionName, "player").GetSystemPrompt()
	if !strings.Contains(playerPrompt, personaV1) {
		t.Errorf("materialized player system_prompt lacks the stored persona %q:\n%s", personaV1, playerPrompt)
	}
	plannerPrompt := getAgentV2TeamMember(t, ctx, sutHostURL, sutEnvName, sessionName, "planner").GetSystemPrompt()
	if !strings.Contains(plannerPrompt, plannerPersona) {
		t.Errorf("materialized planner system_prompt lacks the stored persona %q:\n%s", plannerPersona, plannerPrompt)
	}

	// Edit the record only, then re-materialize: the fresh prompt carries the
	// new persona and not the replaced one — the composition was re-derived
	// from the store rather than restored from a maintained copy.
	updated := updateAgentV2Preset(t, ctx, sutHostURL, sutEnvName, player.GetName(), personaV2)
	if updated.GetPersona() != personaV2 {
		t.Fatalf("updated store record persona = %q, want %q", updated.GetPersona(), personaV2)
	}
	updateAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName, player.GetName(), planner.GetName(), "", "")
	refreshedPrompt := getAgentV2TeamMember(t, ctx, sutHostURL, sutEnvName, sessionName, "player").GetSystemPrompt()
	if !strings.Contains(refreshedPrompt, personaV2) {
		t.Errorf("re-materialized player system_prompt lacks the edited persona %q:\n%s", personaV2, refreshedPrompt)
	}
	if strings.Contains(refreshedPrompt, personaV1) {
		t.Errorf("re-materialized player system_prompt still carries the replaced persona %q:\n%s", personaV1, refreshedPrompt)
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
	// Both projections reset with the lifecycle: the member views are empty
	// too (刷新后历史按新生命周期重建).
	for _, member := range []string{"player", "planner"} {
		if got := listMemberMessages(t, ctx, sutHostURL, sutEnvName, sessionName, member); len(got) != 0 {
			t.Errorf("%s view after refresh = %d entries, want 0 (new lifecycle)", member, len(got))
		}
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

	// The rebuilt projections start their own lifecycle: the merged sequence
	// restarts at seq 1 (the pre-refresh anchors do not leak) and both member
	// views are populated again.
	rebuilt := listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName)
	if len(rebuilt) == 0 {
		t.Fatal("rebuilt merge sequence is empty after the post-refresh Send")
	}
	if rebuilt[0].GetSeq() != 1 {
		t.Errorf("rebuilt merge first seq = %d, want 1 (new lifecycle)", rebuilt[0].GetSeq())
	}
	for _, member := range []string{"player", "planner"} {
		if got := listMemberMessages(t, ctx, sutHostURL, sutEnvName, sessionName, member); len(got) == 0 {
			t.Errorf("%s view after the rebuilt Send is empty, want the new lifecycle's entries", member)
		}
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

	// The fallback also still carries the saolei guidance row: the role-lock
	// entry's system keywords require BOTH the player persona anchor and the
	// guidance heading, and it answers the player's next activation.
	stream2 := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamRoleLockMessage)
	events2 := drainTeamStream(t, stream2)
	assertTeamStreamWellFormed(t, sessionName, events2)
	playerTurns2 := teamTurnsForMember(events2, "player")
	if len(playerTurns2) != 1 {
		t.Fatalf("role-lock player turns = %d, want 1", len(playerTurns2))
	}
	if _, text := teamTurnBlocks(playerTurns2[0]); text != teamPlayerRoleLockText {
		t.Errorf("role-lock reply = %q, want %q (the fallback player prompt carries the guidance)", text, teamPlayerRoleLockText)
	}
}

// TestAgentV2TeamPlayerRoleLockGuidance covers the player half of the role
// lock (SC-004 positive, T023): after the opening cycle leaves the player
// activated, a trigger message is answered by the fixture entry whose system
// keywords require BOTH the player persona anchor and the saolei guidance
// heading — it fires only when the mounted player composition (persona +
// saolei tool-plugin row guidance) reached the model context. The player's
// saolei tools are additionally proven by every game case's tool chain. The
// reverse absence assertions (no memory traces in the player prompt, no
// saolei guidance in the planner prompt) are the T034 system-prompt cases
// above, read through GetTeamMember.
func TestAgentV2TeamPlayerRoleLockGuidance(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx, sessionName := teamPlayerActivation(t, sutHostURL, sutEnvName, "team-lock-"+uniqueSuffix(), "role-lock")

	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamRoleLockMessage)
	events := drainTeamStream(t, stream)
	assertTeamStreamWellFormed(t, sessionName, events)
	playerTurns := teamTurnsForMember(events, "player")
	if len(playerTurns) != 1 {
		t.Fatalf("role-lock player turns = %d, want 1", len(playerTurns))
	}
	if _, text := teamTurnBlocks(playerTurns[0]); text != teamPlayerRoleLockText {
		t.Errorf("role-lock reply = %q, want %q (persona + saolei guidance in the assembled prompt)", text, teamPlayerRoleLockText)
	}
}

// TestAgentV2TeamMemoryReviewPersistsAndSnapshotReloads covers the T023
// memory assertions end to end: the lost-game review calls the memory tool
// (the planner preset's memory row supplies it — a missing row would answer
// `memory failed: …` and the review continuation rule would not match), the
// add is immediately persisted through the memory service's public route,
// and a REFRESHED team loads the written observation into the fresh
// planner's system prompt — the snapshot-entry fixture fires only then,
// proving the materialization-time prefetch reached the model context (the
// previous instance's snapshot was fixed at its own start; fixation is
// pinned at the unit level).
func TestAgentV2TeamMemoryReviewPersistsAndSnapshotReloads(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	sessionID := "team-memory-" + uniqueSuffix()
	ctx, sessionName, team := teamPrep(t, sutHostURL, sutEnvName, sessionID, "team-memory")

	// The review-path write: a lost game ends with the planner's memory add
	// and the review body from the tool-result continuation.
	flow, _ := dialAgentV2FlowProbed(t, ctx, sutHostURL, sutEnvName, sessionID)
	defer flow.Close()
	scriptCh := serveTeamFlowScript(flow, sessionID, teamFlowScript{
		initBoards: [][]byte{saoleiBoardInitPNG},
		stepBoards: [][]byte{saoleiBoardLossPNG},
	}, wsReadTimeout)
	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamStartMessage)
	events := drainTeamStream(t, stream)
	waitTeamFlowScript(t, scriptCh, wsReadTimeout)
	assertTeamStreamWellFormed(t, sessionName, events)
	turns := groupTeamMemberTurns(events)
	if len(turns) != 4 {
		t.Fatalf("member turns = %d, want 4 (opening, game, memory review, stop ack)", len(turns))
	}
	reviewResults := teamTurnToolResults(turns[2])
	if len(reviewResults) != 1 {
		t.Fatalf("review tool results = %d, want 1 (the memory add)", len(reviewResults))
	}
	if reviewResults[0].GetStatus() != game.ToolStatus_TOOL_STATUS_SUCCEEDED || reviewResults[0].GetResult() != teamMemoryAddedResult {
		t.Errorf("review memory result = %v %q, want SUCCEEDED %q (the planner preset's memory row executed)",
			reviewResults[0].GetStatus(), reviewResults[0].GetResult(), teamMemoryAddedResult)
	}
	if _, text := teamTurnBlocks(turns[2]); text != teamPlannerReviewStopText {
		t.Errorf("review text = %q, want %q (the tool-result continuation)", text, teamPlannerReviewStopText)
	}

	// The add is immediately persisted: the entry is visible through the
	// memory service's public /api/v1 route (FR-007).
	listed := listMemories(t, ctx, sutHostURL, sutEnvName, saoleiTemplateID, sessionID, 100, "")
	found := false
	for _, entry := range listed.GetMemories() {
		if entry.GetContent() == teamMemoryReviewContent {
			found = true
		}
	}
	if !found {
		t.Fatalf("session memories = %+v, want the review's %q", listed.GetMemories(), teamMemoryReviewContent)
	}

	// Refresh: the fresh planner prefetches the persisted entry in its setup,
	// so the snapshot fixture (system keywords = snapshot header + the
	// observation line) wins the opening specificity tie by Name and answers
	// the first Send.
	updateAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName,
		teamMemberPreset(team, "player"), teamMemberPreset(team, "planner"), "", "")
	stream2 := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamStartMessage)
	events2 := drainTeamStream(t, stream2)
	assertTeamStreamWellFormed(t, sessionName, events2)
	turns2 := groupTeamMemberTurns(events2)
	if len(turns2) == 0 || turns2[0].member != "planner" {
		t.Fatalf("post-refresh turns = %v, want the planner first", turns2)
	}
	if _, text := teamTurnBlocks(turns2[0]); text != teamMemorySnapshotText {
		t.Errorf("post-refresh planner reply = %q, want %q (the persisted observation must reach the fresh system prompt)", text, teamMemorySnapshotText)
	}
}

// TestAgentV2TeamMemberSystemPromptCompleteAndSplit covers US5 场景 1/2
// (quickstart V5-4, SC-005): GetTeamMember returns each member instance's
// complete effective system prompt — non-empty, carrying the preset persona
// and the shared team section (goal + roster) — and the two members are
// strictly split by role: the player carries the saolei tool guidance and no
// memory snapshot, while the planner carries neither the saolei guidance nor
// a snapshot on a fresh session (the empty snapshot section does not render).
func TestAgentV2TeamMemberSystemPromptCompleteAndSplit(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	sessionName := ensureAgentV2Session(t, sutHostURL, sutEnvName, "team-prompt-"+uniqueSuffix())
	playerMarker := "T034 完整可读 player persona"
	plannerMarker := "T034 完整可读 planner persona"
	player := createAgentV2TeamPreset(t, ctx, sutHostURL, sutEnvName, "team-prompt-player-"+uniqueSuffix(), "你是扫雷 player，"+playerMarker, "player")
	planner := createAgentV2TeamPreset(t, ctx, sutHostURL, sutEnvName, "team-prompt-planner-"+uniqueSuffix(), "你是扫雷 planner，"+plannerMarker, "planner")
	updateAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName, player.GetName(), planner.GetName(), "", "")

	playerPrompt := getAgentV2TeamMember(t, ctx, sutHostURL, sutEnvName, sessionName, "player").GetSystemPrompt()
	if playerPrompt == "" {
		t.Fatal("player system_prompt is empty, want the assembled prompt")
	}
	for _, want := range []string{
		"你是扫雷 player，" + playerMarker,
		"## 团队",
		"你所在的团队目标：协作完成多局扫雷游戏",
		"- [planner] 复盘对局与制定策略，不操作",
		"## saolei (Minesweeper tools)",
	} {
		if !strings.Contains(playerPrompt, want) {
			t.Errorf("player system_prompt lacks %q:\n%s", want, playerPrompt)
		}
	}
	for _, absent := range []string{"你是扫雷 planner，", "长期记忆："} {
		if strings.Contains(playerPrompt, absent) {
			t.Errorf("player system_prompt carries %q, want no planner/memory traces:\n%s", absent, playerPrompt)
		}
	}

	plannerPrompt := getAgentV2TeamMember(t, ctx, sutHostURL, sutEnvName, sessionName, "planner").GetSystemPrompt()
	if plannerPrompt == "" {
		t.Fatal("planner system_prompt is empty, want the assembled prompt")
	}
	for _, want := range []string{
		"你是扫雷 planner，" + plannerMarker,
		"## 团队",
		"你所在的团队目标：协作完成多局扫雷游戏",
		"- [player] 执行扫雷操作，独占桌面控制",
	} {
		if !strings.Contains(plannerPrompt, want) {
			t.Errorf("planner system_prompt lacks %q:\n%s", want, plannerPrompt)
		}
	}
	for _, absent := range []string{"你是扫雷 player，", "## saolei (Minesweeper tools)", "长期记忆："} {
		if strings.Contains(plannerPrompt, absent) {
			t.Errorf("planner system_prompt carries %q, want no player/guidance/snapshot traces:\n%s", absent, plannerPrompt)
		}
	}
}

// TestAgentV2TeamSystemPromptSnapshotFixationAndPersonaRefresh covers the
// snapshot half of SC-004 and US5 场景 3 (quickstart V2-2/V5-4): the memory
// snapshot is prefetched at materialization and FIXED for the instance's
// lifetime — the review's freshly written observation does not appear in the
// running planner's system_prompt — and a refresh (same presets, one player
// persona edited) rebuilds both prompts: the fresh planner's carries the
// reloaded snapshot, the player's carries the new persona and still no
// snapshot.
func TestAgentV2TeamSystemPromptSnapshotFixationAndPersonaRefresh(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	sessionID := "team-snapshot-" + uniqueSuffix()
	sessionName := ensureAgentV2Session(t, sutHostURL, sutEnvName, sessionID)
	playerPersonaV1 := "T034 persona v1"
	playerPersonaV2 := "T034 persona v2"
	plannerMarker := "T034 快照 planner persona"
	player := createAgentV2TeamPreset(t, ctx, sutHostURL, sutEnvName, "team-snapshot-player-"+uniqueSuffix(), "你是扫雷 player，"+playerPersonaV1, "player")
	planner := createAgentV2TeamPreset(t, ctx, sutHostURL, sutEnvName, "team-snapshot-planner-"+uniqueSuffix(), "你是扫雷 planner，"+plannerMarker, "planner")
	updateAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName, player.GetName(), planner.GetName(), "", "")

	// A fresh session has no memory: the empty snapshot section does not render.
	if got := getAgentV2TeamMember(t, ctx, sutHostURL, sutEnvName, sessionName, "planner").GetSystemPrompt(); strings.Contains(got, "长期记忆：") {
		t.Fatalf("fresh planner system_prompt carries a snapshot section:\n%s", got)
	}

	// One lost game drives the review's memory write (the T023 persistence
	// path; here the write is the snapshot reload's prerequisite).
	flow, _ := dialAgentV2FlowProbed(t, ctx, sutHostURL, sutEnvName, sessionID)
	defer flow.Close()
	scriptCh := serveTeamFlowScript(flow, sessionID, teamFlowScript{
		initBoards: [][]byte{saoleiBoardInitPNG},
		stepBoards: [][]byte{saoleiBoardLossPNG},
	}, wsReadTimeout)
	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamStartMessage)
	events := drainTeamStream(t, stream)
	waitTeamFlowScript(t, scriptCh, wsReadTimeout)
	assertTeamStreamWellFormed(t, sessionName, events)
	turns := groupTeamMemberTurns(events)
	if len(turns) != 4 {
		t.Fatalf("member turns = %d, want 4 (opening, game, memory review, stop ack)", len(turns))
	}
	reviewResults := teamTurnToolResults(turns[2])
	if len(reviewResults) != 1 || reviewResults[0].GetResult() != teamMemoryAddedResult {
		t.Fatalf("review tool results = %+v, want the single memory add", reviewResults)
	}

	// Fixation: the running planner's prompt still has no snapshot — the
	// write takes effect on the NEXT materialization only.
	midPrompt := getAgentV2TeamMember(t, ctx, sutHostURL, sutEnvName, sessionName, "planner").GetSystemPrompt()
	if strings.Contains(midPrompt, "长期记忆：") {
		t.Errorf("running planner system_prompt picked up the write inside the instance lifetime:\n%s", midPrompt)
	}

	// Persona edit + refresh: the new player persona and the reloaded
	// planner snapshot land in the fresh instances' prompts.
	updateAgentV2Preset(t, ctx, sutHostURL, sutEnvName, player.GetName(), "你是扫雷 player，"+playerPersonaV2)
	updateAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName, player.GetName(), planner.GetName(), "", "")

	playerPrompt := getAgentV2TeamMember(t, ctx, sutHostURL, sutEnvName, sessionName, "player").GetSystemPrompt()
	if !strings.Contains(playerPrompt, playerPersonaV2) {
		t.Errorf("refreshed player system_prompt lacks the edited persona %q:\n%s", playerPersonaV2, playerPrompt)
	}
	if strings.Contains(playerPrompt, playerPersonaV1) {
		t.Errorf("refreshed player system_prompt still carries the old persona %q:\n%s", playerPersonaV1, playerPrompt)
	}
	if strings.Contains(playerPrompt, "长期记忆：") {
		t.Errorf("refreshed player system_prompt carries a memory snapshot, want none:\n%s", playerPrompt)
	}

	plannerPrompt := getAgentV2TeamMember(t, ctx, sutHostURL, sutEnvName, sessionName, "planner").GetSystemPrompt()
	if !strings.Contains(plannerPrompt, "长期记忆：") || !strings.Contains(plannerPrompt, teamMemoryReviewContent) {
		t.Errorf("refreshed planner system_prompt lacks the reloaded snapshot (header %q + %q):\n%s", "长期记忆：", teamMemoryReviewContent, plannerPrompt)
	}
	if !strings.Contains(plannerPrompt, plannerMarker) {
		t.Errorf("refreshed planner system_prompt lost its persona %q:\n%s", plannerMarker, plannerPrompt)
	}
	if strings.Contains(plannerPrompt, "## saolei (Minesweeper tools)") {
		t.Errorf("refreshed planner system_prompt carries the saolei guidance:\n%s", plannerPrompt)
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
