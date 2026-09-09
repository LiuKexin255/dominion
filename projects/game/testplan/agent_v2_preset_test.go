// Package testplan contains the agent_v2 preset/materialization large tests:
// the US2 configuration closed loop over the /api/v2 surface
// (specs/051-agent-v2-dsh-migration — quickstart.md §2 agent-v2-preset row).
// Cases are grouped by tested concern (preset CRUD round trip with Mongo
// persistence, materialized-config consistency, Update refresh clearing the
// history, unmaterialized-Send rejection, empty-prompt persona fallback),
// one test per concern — style/large_test.md §测试组织. Preset state lives in
// the agent_v2 Mongo store, so every assertion here round-trips through the
// gateway's direct PresetService routes (agent-api.md §4); the restart-level
// persistence check (FR-005) is the T037 full-acceptance run's job and is
// deliberately out of this suite's scope.
package testplan

import (
	"net/http"
	"strings"
	"testing"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
)

// presetName builds the full preset resource name under the saolei template
// (templates/{template}/presets/{preset}, agent-api.md §1).
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

// TestAgentV2PresetCrudRoundTrip covers the preset CRUD closed loop through
// the gateway (quickstart §2 agent-v2-preset: preset CRUD；US2 场景 1) —
// create → Get equals the created resource → List carries it → duplicate
// caller-id → 409 ALREADY_EXISTS → update (update_mask=persona) →
// Get/List reflect the new prompt with server-maintained timestamps →
// delete → Get 404 → List without. Mongo-backed persistence is what makes
// every read-back here agree with the write (data-model.md §2.1); restart
// survival is T037's acceptance item.
func TestAgentV2PresetCrudRoundTrip(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	id := "preset-crud-" + uniqueSuffix()
	name := presetName(id)

	// create → the response is the stored resource with its server-assigned
	// name and OUTPUT_ONLY timestamps.
	created := createAgentV2Preset(t, ctx, sutHostURL, sutEnvName, id, "你是扫雷 player（crud persona v1）")
	if created.GetName() != name {
		t.Fatalf("created preset name = %q, want %q", created.GetName(), name)
	}
	if created.GetPersona() != "你是扫雷 player（crud persona v1）" {
		t.Errorf("created persona = %q, want %q", created.GetPersona(), "你是扫雷 player（crud persona v1）")
	}
	if created.GetCreateTime() == nil || created.GetUpdateTime() == nil {
		t.Errorf("created preset timestamps = %q / %q, want server-maintained values", created.GetCreateTime(), created.GetUpdateTime())
	}

	// Get round-trips the stored state (Mongo 读回).
	if got := getAgentV2Preset(t, ctx, sutHostURL, sutEnvName, name); got.GetPersona() != "你是扫雷 player（crud persona v1）" {
		t.Errorf("Get after create persona = %q, want %q", got.GetPersona(), "你是扫雷 player（crud persona v1）")
	}

	// List carries the created preset.
	if got := listContainsPreset(listAgentV2Presets(t, ctx, sutHostURL, sutEnvName), name); got == nil {
		t.Fatalf("ListPresets does not contain %q", name)
	} else if got.GetPersona() != "你是扫雷 player（crud persona v1）" {
		t.Errorf("Listed persona = %q, want %q", got.GetPersona(), "你是扫雷 player（crud persona v1）")
	}

	// Duplicate caller-id → 409 ALREADY_EXISTS (agent-api.md §2.5).
	_, dupStatus, dupBody := createAgentV2PresetWithStatus(t, ctx, sutHostURL, sutEnvName, id, "duplicate")
	if dupStatus != http.StatusConflict {
		t.Errorf("duplicate create status = %d (body: %s), want 409 ALREADY_EXISTS", dupStatus, dupBody)
	}

	// Update patches only persona (AIP-134 update_mask) and both read
	// paths reflect it.
	updated := updateAgentV2Preset(t, ctx, sutHostURL, sutEnvName, name, "你是扫雷 player（crud persona v2）")
	if updated.GetPersona() != "你是扫雷 player（crud persona v2）" || updated.GetName() != name {
		t.Errorf("updated preset = {%s %q}, want {%s %q}", updated.GetName(), updated.GetPersona(), name, "你是扫雷 player（crud persona v2）")
	}
	if got := getAgentV2Preset(t, ctx, sutHostURL, sutEnvName, name); got.GetPersona() != "你是扫雷 player（crud persona v2）" {
		t.Errorf("Get after update persona = %q, want %q", got.GetPersona(), "你是扫雷 player（crud persona v2）")
	}
	if got := listContainsPreset(listAgentV2Presets(t, ctx, sutHostURL, sutEnvName), name); got == nil || got.GetPersona() != "你是扫雷 player（crud persona v2）" {
		t.Errorf("List after update carries %q with prompt %v, want the updated prompt", name, got)
	}

	// Delete removes the resource: Get 404, List without.
	if status, body := deleteAgentV2PresetWithStatus(t, ctx, sutHostURL, sutEnvName, name); status != http.StatusOK {
		t.Fatalf("DELETE preset status = %d (body: %s), want 200", status, body)
	}
	if _, status := getAgentV2PresetWithStatus(t, ctx, sutHostURL, sutEnvName, name); status != http.StatusNotFound {
		t.Errorf("Get after delete status = %d, want 404 NOT_FOUND", status)
	}
	if got := listContainsPreset(listAgentV2Presets(t, ctx, sutHostURL, sutEnvName), name); got != nil {
		t.Errorf("List after delete still contains %q", name)
	}
}

// TestAgentV2MaterializedConfigConsistency covers US2 场景 2/7 (quickstart §2
// agent-v2-preset: 物化与模型选择): UpdateAgent materializes the agent
// singleton with the chosen preset and an explicit model id honored against
// the pinned model catalog, an empty model resolves to the default glm-5.3,
// and GetAgent reports exactly that configuration (agent-api.md
// §2.1/§2.2 — create-or-update on the AIP-156 singleton, get answers the
// stored config). The catalog is pinned per agent-api-changes.md §5:
// glm-5.3 and glm-5.3-flash, glm-5.3 first (the default), and the same
// source UpdateAgent validates against (模型目录同源, agent-api.md §2.6).
func TestAgentV2MaterializedConfigConsistency(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	sessionName := ensureAgentV2Session(t, sutHostURL, sutEnvName, "preset-mat-"+uniqueSuffix())
	preset := createAgentV2Preset(t, ctx, sutHostURL, sutEnvName, "preset-mat-"+uniqueSuffix(), "你是扫雷 player，materialization persona")

	catalog := listAgentV2Models(t, ctx, sutHostURL, sutEnvName)
	models := catalog.GetModels()
	if len(models) != 2 {
		t.Fatalf("ListModels returned %d entries, want 2 (glm-5.3 + glm-5.3-flash, agent-api-changes.md §5)", len(models))
	}
	if models[0].GetId() != "glm-5.3" {
		t.Errorf("catalog[0] = %q, want glm-5.3 (first entry = the default model)", models[0].GetId())
	}
	if models[1].GetId() != "glm-5.3-flash" {
		t.Errorf("catalog[1] = %q, want glm-5.3-flash", models[1].GetId())
	}

	// An explicit catalog id is honored: materialize with glm-5.3-flash.
	model := models[1].GetId()
	materialized := updateAgentV2Agent(t, ctx, sutHostURL, sutEnvName, sessionName, preset.GetName(), model)
	if want := sessionName + "/agent"; materialized.GetName() != want {
		t.Fatalf("materialized agent name = %q, want %q", materialized.GetName(), want)
	}
	if materialized.GetPreset() != preset.GetName() {
		t.Errorf("materialized preset = %q, want %q", materialized.GetPreset(), preset.GetName())
	}
	if materialized.GetModel() != model {
		t.Errorf("materialized model = %q, want catalog entry %q", materialized.GetModel(), model)
	}

	// GetAgent reports the same configuration (agent-api.md §2.2).
	stored := getAgentV2Agent(t, ctx, sutHostURL, sutEnvName, sessionName)
	if stored.GetPreset() != preset.GetName() || stored.GetModel() != model {
		t.Errorf("GetAgent config = {%q %q}, want {%q %q}", stored.GetPreset(), stored.GetModel(), preset.GetName(), model)
	}
	if stored.GetCreateTime() == nil || stored.GetUpdateTime() == nil {
		t.Errorf("GetAgent timestamps = %q / %q, want server-maintained values", stored.GetCreateTime(), stored.GetUpdateTime())
	}

	// An empty model resolves to the process default glm-5.3 at
	// materialization (agent-api-changes.md §5: GLM_MODEL || "glm-5.3"; the
	// test cluster sets no GLM_MODEL).
	defaulted := updateAgentV2Agent(t, ctx, sutHostURL, sutEnvName, sessionName, preset.GetName(), "")
	if defaulted.GetModel() != "glm-5.3" {
		t.Errorf("default-materialized model = %q, want glm-5.3", defaulted.GetModel())
	}

	// Idempotent re-Apply of the same config yields the same configuration
	// (agent-api.md §2.1 幂等：同配置重复 Update → 同配置干净 agent).
	again := updateAgentV2Agent(t, ctx, sutHostURL, sutEnvName, sessionName, preset.GetName(), "")
	if again.GetPreset() != preset.GetName() || again.GetModel() != "glm-5.3" {
		t.Errorf("re-Apply config = {%q %q}, want the unchanged {%q %q}", again.GetPreset(), again.GetModel(), preset.GetName(), "glm-5.3")
	}

	// 未知 id 拒绝 (US2 场景 7): a model outside the catalog fails the
	// fail-fast validation with 400 INVALID_ARGUMENT and leaves the standing
	// configuration untouched (no half-materialization).
	_, failedStatus, failedBody := updateAgentV2AgentWithStatus(t, ctx, sutHostURL, sutEnvName, sessionName, preset.GetName(), "no-such-model")
	if failedStatus != http.StatusBadRequest {
		t.Errorf("UpdateAgent with unknown model status = %d (body: %s), want 400 INVALID_ARGUMENT", failedStatus, failedBody)
	}
	if after := getAgentV2Agent(t, ctx, sutHostURL, sutEnvName, sessionName); after.GetModel() != "glm-5.3" {
		t.Errorf("GetAgent after the rejected update = %q, want the standing glm-5.3 (fail-fast leaves no half-materialized state)", after.GetModel())
	}
}

// TestAgentV2UpdateRefreshClearsHistory covers US2 场景 4 (quickstart §2
// agent-v2-preset: Update 刷新记忆清空): a conversed session's agent holds
// the turn in memory; re-applying the SAME configuration tears the agent
// down and rebuilds it, so ListAgentMessages comes back empty while GetAgent
// still reports the unchanged configuration (refresh semantics merged into
// Update, data-model.md §2.2), and the session is conversational again.
func TestAgentV2UpdateRefreshClearsHistory(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	sessionName := ensureAgentV2Session(t, sutHostURL, sutEnvName, "preset-refresh-"+uniqueSuffix())
	preset := createAgentV2Preset(t, ctx, sutHostURL, sutEnvName, "preset-refresh-"+uniqueSuffix(), "你是扫雷 player，refresh persona")
	updateAgentV2Agent(t, ctx, sutHostURL, sutEnvName, sessionName, preset.GetName(), "")

	// One completed turn puts the user message and the agent reply into the
	// in-memory history.
	stream := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerPlain+" before refresh")
	events := drainAgentV2Turn(t, stream)
	stream.Close()
	assertAgentV2TurnWellFormed(t, sessionName, events)
	if hist := listAgentV2Messages(t, ctx, sutHostURL, sutEnvName, sessionName); len(hist.GetMessages()) == 0 {
		t.Fatal("history is empty before the refresh — the refresh assertion would be vacuous")
	}

	// Re-Apply the same configuration = refresh: the short-term memory is
	// cleared (FR-006 清空短期记忆).
	updateAgentV2Agent(t, ctx, sutHostURL, sutEnvName, sessionName, preset.GetName(), "")
	hist := listAgentV2Messages(t, ctx, sutHostURL, sutEnvName, sessionName)
	if len(hist.GetMessages()) != 0 {
		t.Errorf("history after refresh = %d message(s), want 0 (Update 刷新清空短期记忆, data-model.md §2.2)", len(hist.GetMessages()))
	}

	// The agent is still materialized with the same configuration.
	stored := getAgentV2Agent(t, ctx, sutHostURL, sutEnvName, sessionName)
	if stored.GetPreset() != preset.GetName() {
		t.Errorf("GetAgent preset after refresh = %q, want %q", stored.GetPreset(), preset.GetName())
	}

	// And the rebuilt agent is conversational again.
	stream2 := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerPlain+" after refresh")
	defer stream2.Close()
	events2 := drainAgentV2Turn(t, stream2)
	assertAgentV2TurnWellFormed(t, sessionName, events2)
	if got := agentV2TerminalBlocksFromEvents(events2).text; got != agentV2PlainText {
		t.Errorf("post-refresh turn text = %q, want %q", got, agentV2PlainText)
	}
}

// TestAgentV2SendUnmaterializedRejected covers US2 场景 5's rejection layer
// (quickstart §2 agent-v2-preset: 未物化 Send 拒绝): the never-materialized
// session's Send is answered NOT_FOUND (404 — the proxy looks the owner up
// and never allocates), and a session whose owner WAS allocated by a
// failed-fail-fast UpdateAgent gets FAILED_PRECONDITION (400 — owner present
// but no materialized agent; the same state an agent_v2 restart leaves,
// agent-api.md §2.4). The unknown-model materialization itself is the US2
// 场景 7 fail-fast: 400 INVALID_ARGUMENT with no half-materialized state.
func TestAgentV2SendUnmaterializedRejected(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	// Layer 1 — a session that never touched UpdateAgent has no owner:
	// Send → 404 NOT_FOUND (Send 只查不分配, data-model.md §2.9).
	ghost := ensureAgentV2Session(t, sutHostURL, sutEnvName, "preset-ghost-"+uniqueSuffix())
	status, body := postAgentV2SendStatus(t, ctx, sutHostURL, sutEnvName, ghost, agentV2TriggerPlain+" no agent here")
	if status != http.StatusNotFound {
		t.Errorf("Send on never-materialized session status = %d (body: %s), want 404 NOT_FOUND", status, body)
	}

	// Layer 2 — owner without a materialized agent. UpdateAgent allocates
	// the owner (get-or-create) BEFORE agent_v2 validates: an unknown model
	// fails the fail-fast check with 400 INVALID_ARGUMENT (US2 场景 7) and
	// leaves owner-without-agent behind. GetAgent still answers 404 (no
	// half-materialization) and Send is rejected with FAILED_PRECONDITION
	// mapped to 400.
	sessionName := ensureAgentV2Session(t, sutHostURL, sutEnvName, "preset-unmat-"+uniqueSuffix())
	preset := createAgentV2Preset(t, ctx, sutHostURL, sutEnvName, "preset-unmat-"+uniqueSuffix(), "你是扫雷 player，unmaterialized persona")

	_, failedStatus, failedBody := updateAgentV2AgentWithStatus(t, ctx, sutHostURL, sutEnvName, sessionName, preset.GetName(), "no-such-model")
	if failedStatus != http.StatusBadRequest {
		t.Errorf("UpdateAgent with unknown model status = %d (body: %s), want 400 INVALID_ARGUMENT (US2 场景 7)", failedStatus, failedBody)
	} else if !strings.Contains(string(failedBody), "unknown model") {
		t.Errorf("unknown-model rejection body = %s, want the fail-fast validation message", failedBody)
	}

	if unmatGetStatus, _ := getAgentV2AgentWithStatus(t, ctx, sutHostURL, sutEnvName, sessionName); unmatGetStatus != http.StatusNotFound {
		t.Errorf("GetAgent after failed materialization status = %d, want 404 (无半物化, agent-api.md §2.1)", unmatGetStatus)
	}

	unmatStatus, unmatBody := postAgentV2SendStatus(t, ctx, sutHostURL, sutEnvName, sessionName, agentV2TriggerPlain+" still unmaterialized")
	if unmatStatus != http.StatusBadRequest {
		t.Errorf("Send on owner-without-agent status = %d (body: %s), want 400 FAILED_PRECONDITION", unmatStatus, unmatBody)
	} else if !strings.Contains(string(unmatBody), "materialized") {
		t.Errorf("unmaterialized Send body = %s, want the materialization precondition message", unmatBody)
	}
}

// TestAgentV2EmptyPromptPersonaFallback covers US2 场景 6 (quickstart §2
// agent-v2-preset: 空 prompt 回退 base): a preset with an empty persona
// materializes fine (empty is the documented fallback trigger, data-model.md
// §2.1) and the rebuilt agent answers a turn — the copy's persona row rides
// the model context end to end carrying the pool template's persona row
// text (the anchor line 「你是扫雷 player。」). The persona CONTENT
// itself is observable operator-side in the fake-llm logs (the fake logs the
// request's system prompt; the Responses endpoint ignores it for matching,
// responses.go), so the machine-checkable half is the successful model
// round-trip pinned by the deterministic plain-template reply.
func TestAgentV2EmptyPromptPersonaFallback(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	sessionName := ensureAgentV2Session(t, sutHostURL, sutEnvName, "preset-empty-"+uniqueSuffix())
	// Empty persona: materialization must accept it (场景 6 前提).
	preset := createAgentV2Preset(t, ctx, sutHostURL, sutEnvName, "preset-empty-"+uniqueSuffix(), "")
	if preset.GetPersona() != "" {
		t.Fatalf("created persona = %q, want empty", preset.GetPersona())
	}
	materialized := updateAgentV2Agent(t, ctx, sutHostURL, sutEnvName, sessionName, preset.GetName(), "")
	if materialized.GetPreset() != preset.GetName() {
		t.Fatalf("materialized preset = %q, want %q", materialized.GetPreset(), preset.GetName())
	}

	// The fallback-persona agent completes a model round-trip (persona 经
	// fake-llm 链路回显：the deterministic plain-template reply is the echo).
	stream := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerPlain+" empty persona check")
	defer stream.Close()
	events := drainAgentV2Turn(t, stream)
	assertAgentV2TurnWellFormed(t, sessionName, events)
	if end := events[len(events)-1].GetTurnEnd(); end.GetStatus() != game.TurnStatus_TURN_STATUS_COMPLETED {
		t.Fatalf("turn ended %v, want COMPLETED (empty-prompt persona must fall back, not fail)", end.GetStatus())
	}
	if got := agentV2TerminalBlocksFromEvents(events).text; got != agentV2PlainText {
		t.Errorf("turn text = %q, want %q", got, agentV2PlainText)
	}
}
