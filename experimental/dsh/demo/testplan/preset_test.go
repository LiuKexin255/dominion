package testplan

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"dominion/common/gopkg/testtool"
)

// Preset module suite over the public HTTP entry, two faces
// (specs/058-dsh-preset-roster-demo/tasks.md T017/T019):
//
//   - The conversation-binding face: conversations are created EXPLICITLY
//     through CreateConversation with a preset (or the roster default), and
//     the model-visible composition each session runs is asserted end to end
//     via the fake-llm `system_keywords` probe replies. Covers the US1
//     acceptance scenarios (V1-1/V1-2/V2-3 end to end, US1-AS3
//     shared-mount behaviour, US1-AS4 session-level stability), the
//     idempotent/rebuild semantics (R4), and the no-lazy-creation
//     FAILED_PRECONDITION edge (FR-002).
//   - The resource face: the PresetService CRUD closed loop over deployment
//     templates (C1 copy-then-patch). Covers hot creation (V3-1), the
//     generation switch on persona update (V3-2), the full lifecycle
//     create→bind→update→delete (V4-1), delete semantics (V4-3), and the
//     creation rejection edges with no half-materialized residue (US2-AS4).
//
// V3-3 (broken-preset presentation) is NOT carried by large-test steps: the
// materialized copies live inside the agent container's ephemeral writable
// layer, which the test process cannot reach. R11
// (specs/058-dsh-preset-roster-demo/research.md) assigns V3-3 to the plugin
// unit tests over the fs seam
// (common/js/dsh-plugins/preset-authoring/src/index.test.ts), where a broken
// copy is written deliberately.

// TestPresetConversationComposition verifies that two sessions bound to
// different presets present DIFFERENT model-visible compositions, and the
// demo-echo guidance is present exactly when the preset row is (V1-1/V2-3,
// specs/058-dsh-preset-roster-demo/spec.md US1 acceptance scenarios 1 and
// the guidance half of the row-level consistency). Each case creates a
// fresh conversation bound to its preset and sends the probe keyword: the
// fake-llm answers with the template whose system_keywords match the
// session's persona/guidance, so the reply names the composition the model
// actually received. A demo-standard session probed for guidance falls to
// the deterministic farewell fallback — the guidance-absent proof.
func TestPresetConversationComposition(t *testing.T) {
	baseURL := testtool.MustEndpoint("http", "public")
	envName := testtool.MustEnv()
	ctx := traceContext(t)

	tests := []struct {
		name            string
		preset          string
		probe           string
		wantReply       string
		wantBoundPreset string
	}{
		{
			name:            "demo-tools session probe hits the tools persona",
			preset:          "demo-tools",
			probe:           "preset-probe",
			wantReply:       personaToolsReply,
			wantBoundPreset: "demo-tools",
		},
		{
			name:            "demo-standard session probe hits the standard persona",
			preset:          "demo-standard",
			probe:           "preset-probe",
			wantReply:       personaStandardReply,
			wantBoundPreset: "demo-standard",
		},
		{
			name:            "demo-tools session sees the demo_echo guidance",
			preset:          "demo-tools",
			probe:           "guidance-probe",
			wantReply:       guidanceReply,
			wantBoundPreset: "demo-tools",
		},
		{
			name:            "demo-standard session answers the guidance probe with the fallback",
			preset:          "demo-standard",
			probe:           "guidance-probe",
			wantReply:       farewellText,
			wantBoundPreset: "demo-standard",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given: one conversation explicitly bound to the preset.
			conversationID := "preset-composition-" + tt.preset + "-" + tt.probe
			status, respBody := createConversation(t, ctx, baseURL, envName, conversationID, tt.preset)
			if status != http.StatusOK {
				t.Fatalf("createConversation status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
			}
			created := new(conversationResponse)
			if err := json.Unmarshal(respBody, created); err != nil {
				t.Fatalf("json.Unmarshal(%s) unexpected error: %v", respBody, err)
			}

			// when: the probe turn fires on the created conversation.
			status, respBody = postChatTurn(t, ctx, baseURL, envName, created.Name, []byte(`{"message": "`+tt.probe+`"}`))

			// then: the reply names the composition the session runs, and
			// the binding reported at creation matches the requested preset.
			if status != http.StatusOK {
				t.Fatalf("sendMessage status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
			}
			got := new(sendMessageResponse)
			if err := json.Unmarshal(respBody, got); err != nil {
				t.Fatalf("json.Unmarshal(%s) unexpected error: %v", respBody, err)
			}
			if created.Preset != tt.wantBoundPreset {
				t.Errorf("resolved preset = %q, want %q", created.Preset, tt.wantBoundPreset)
			}
			if got.Reply != tt.wantReply {
				t.Errorf("reply = %q, want %q (model-visible composition must match the preset)", got.Reply, tt.wantReply)
			}
		})
	}
}

// TestPresetDefaultSelection verifies that a conversation created WITHOUT a
// preset is bound to the roster default (demo-standard, V1-2, US1
// acceptance scenario 2): the CreateConversation view carries the RESOLVED
// default id, and the session's probe reply matches the default preset's
// persona.
func TestPresetDefaultSelection(t *testing.T) {
	baseURL := testtool.MustEndpoint("http", "public")
	envName := testtool.MustEnv()
	ctx := traceContext(t)

	// given: a conversation created with no preset field at all.
	const conversationID = "preset-default-selection"
	status, respBody := createConversation(t, ctx, baseURL, envName, conversationID, "")

	// then: the view reports the resolved roster default.
	if status != http.StatusOK {
		t.Fatalf("createConversation status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	created := new(conversationResponse)
	if err := json.Unmarshal(respBody, created); err != nil {
		t.Fatalf("json.Unmarshal(%s) unexpected error: %v", respBody, err)
	}
	if created.Preset != "demo-standard" {
		t.Errorf("resolved default preset = %q, want %q (cordis.yml agent-presets default)", created.Preset, "demo-standard")
	}

	// when: the probe turn fires.
	status, respBody = postChatTurn(t, ctx, baseURL, envName, conversationID, []byte(`{"message": "preset-probe"}`))

	// then: the default preset's persona answered.
	if status != http.StatusOK {
		t.Fatalf("sendMessage status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	got := new(sendMessageResponse)
	if err := json.Unmarshal(respBody, got); err != nil {
		t.Fatalf("json.Unmarshal(%s) unexpected error: %v", respBody, err)
	}
	if got.Reply != personaStandardReply {
		t.Errorf("reply = %q, want %q (the default preset's persona must be live)", got.Reply, personaStandardReply)
	}
}

// TestPresetSamePresetSharedBehaviour verifies that two conversations bound
// to the SAME preset behave identically (US1-AS3, the behavioural face of
// the shared standing mount — the mount itself exists once per process,
// asserted by the composition unit test): both report the same resolved
// preset and both answer the probe with the same composition.
func TestPresetSamePresetSharedBehaviour(t *testing.T) {
	baseURL := testtool.MustEndpoint("http", "public")
	envName := testtool.MustEnv()
	ctx := traceContext(t)

	// given: two conversations bound to demo-tools.
	const preset = "demo-tools"
	const conversationA = "preset-shared-a"
	const conversationB = "preset-shared-b"
	resolved := map[string]string{}
	for _, id := range []string{conversationA, conversationB} {
		status, respBody := createConversation(t, ctx, baseURL, envName, id, preset)
		if status != http.StatusOK {
			t.Fatalf("createConversation(%s) status = %d, want %d (body: %s)", id, status, http.StatusOK, respBody)
		}
		created := new(conversationResponse)
		if err := json.Unmarshal(respBody, created); err != nil {
			t.Fatalf("createConversation(%s): json.Unmarshal(%s) unexpected error: %v", id, respBody, err)
		}
		if created.Preset != preset {
			t.Errorf("createConversation(%s) resolved preset = %q, want %q", id, created.Preset, preset)
		}
		resolved[id] = created.Preset
	}

	// when: both conversations send the probe turn.
	replies := map[string]string{}
	for _, id := range []string{conversationA, conversationB} {
		status, respBody := postChatTurn(t, ctx, baseURL, envName, id, []byte(`{"message": "preset-probe"}`))
		if status != http.StatusOK {
			t.Fatalf("sendMessage(%s) status = %d, want %d (body: %s)", id, status, http.StatusOK, respBody)
		}
		got := new(sendMessageResponse)
		if err := json.Unmarshal(respBody, got); err != nil {
			t.Fatalf("sendMessage(%s): json.Unmarshal(%s) unexpected error: %v", id, respBody, err)
		}
		replies[id] = got.Reply
	}

	// then: identical bindings and identical composition behaviour.
	if replies[conversationA] != replies[conversationB] {
		t.Errorf("replies diverged: %s = %q, %s = %q, want identical (same preset, same composition)",
			conversationA, replies[conversationA], conversationB, replies[conversationB])
	}
	if replies[conversationA] != personaToolsReply {
		t.Errorf("reply = %q, want %q", replies[conversationA], personaToolsReply)
	}
	for id, presetID := range resolved {
		if presetID != preset {
			t.Errorf("conversation %s binding = %q, want %q", id, presetID, preset)
		}
	}
}

// TestPresetSessionStability verifies that a conversation's binding is
// fixed at creation and survives later turns (US1-AS4): probe → plain
// 047 chat turn → probe again, with the composition behaviour identical
// on both probes even though the plain turn changed the conversation
// history.
func TestPresetSessionStability(t *testing.T) {
	baseURL := testtool.MustEndpoint("http", "public")
	envName := testtool.MustEnv()
	ctx := traceContext(t)

	// given: one demo-tools conversation.
	const conversationID = "preset-stability"
	status, respBody := createConversation(t, ctx, baseURL, envName, conversationID, "demo-tools")
	if status != http.StatusOK {
		t.Fatalf("createConversation status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}

	turns := []struct {
		turn      int
		message   string
		wantReply string
	}{
		{turn: 1, message: "preset-probe", wantReply: personaToolsReply},
		{turn: 2, message: "hello", wantReply: greetingText},
		{turn: 3, message: "preset-probe", wantReply: personaToolsReply},
	}

	for _, tt := range turns {
		// when: each turn fires on the same conversation.
		status, respBody := postChatTurn(t, ctx, baseURL, envName, conversationID, []byte(`{"message": "`+tt.message+`"}`))

		// then: the reply matches the turn's expectation — the binding
		// stays demo-tools across the conversation's lifetime.
		if status != http.StatusOK {
			t.Fatalf("turn %d: status = %d, want %d (body: %s)", tt.turn, status, http.StatusOK, respBody)
		}
		got := new(sendMessageResponse)
		if err := json.Unmarshal(respBody, got); err != nil {
			t.Fatalf("turn %d: json.Unmarshal(%s) unexpected error: %v", tt.turn, respBody, err)
		}
		if got.Reply != tt.wantReply {
			t.Errorf("turn %d reply = %q, want %q (composition must stay bound to demo-tools)", tt.turn, got.Reply, tt.wantReply)
		}
	}
}

// TestPresetIdempotentAndRebuild verifies the repeat-create semantics (R4,
// specs/058-dsh-preset-roster-demo/contracts/chat-api.md §1.1): recreating
// a conversation with the SAME preset is an idempotent success, and
// recreating it with a DIFFERENT preset rebuilds the session on the new
// composition — the subsequent probe proves the new persona is live.
func TestPresetIdempotentAndRebuild(t *testing.T) {
	baseURL := testtool.MustEndpoint("http", "public")
	envName := testtool.MustEnv()
	ctx := traceContext(t)

	// given: a conversation bound to demo-tools.
	const conversationID = "preset-rebuild"
	status, respBody := createConversation(t, ctx, baseURL, envName, conversationID, "demo-tools")
	if status != http.StatusOK {
		t.Fatalf("create demo-tools: status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	first := new(conversationResponse)
	if err := json.Unmarshal(respBody, first); err != nil {
		t.Fatalf("create demo-tools: json.Unmarshal(%s) unexpected error: %v", respBody, err)
	}

	// when/then: the same id + same preset answers idempotently.
	status, respBody = createConversation(t, ctx, baseURL, envName, conversationID, "demo-tools")
	if status != http.StatusOK {
		t.Fatalf("idempotent recreate: status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	again := new(conversationResponse)
	if err := json.Unmarshal(respBody, again); err != nil {
		t.Fatalf("idempotent recreate: json.Unmarshal(%s) unexpected error: %v", respBody, err)
	}
	if again.Name != first.Name || again.Preset != first.Preset {
		t.Errorf("idempotent recreate view drifted: {%s %s} then {%s %s}, want the same view", first.Name, first.Preset, again.Name, again.Preset)
	}

	// when: the same id is recreated on demo-standard (rebuild).
	status, respBody = createConversation(t, ctx, baseURL, envName, conversationID, "demo-standard")
	if status != http.StatusOK {
		t.Fatalf("rebuild on demo-standard: status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	rebuilt := new(conversationResponse)
	if err := json.Unmarshal(respBody, rebuilt); err != nil {
		t.Fatalf("rebuild: json.Unmarshal(%s) unexpected error: %v", respBody, err)
	}
	if rebuilt.Preset != "demo-standard" {
		t.Fatalf("rebuild resolved preset = %q, want %q", rebuilt.Preset, "demo-standard")
	}

	// then: the rebuilt session runs the NEW composition — the standard
	// persona answers the probe (and the tools persona cannot).
	status, respBody = postChatTurn(t, ctx, baseURL, envName, conversationID, []byte(`{"message": "preset-probe"}`))
	if status != http.StatusOK {
		t.Fatalf("probe after rebuild: status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	got := new(sendMessageResponse)
	if err := json.Unmarshal(respBody, got); err != nil {
		t.Fatalf("probe after rebuild: json.Unmarshal(%s) unexpected error: %v", respBody, err)
	}
	if got.Reply != personaStandardReply {
		t.Errorf("probe after rebuild = %q, want %q (the rebuilt composition must be live)", got.Reply, personaStandardReply)
	}
}

// TestPresetSendMessageWithoutCreate verifies the no-lazy-creation edge
// (FR-002): sendMessage against conversations that were never created is
// rejected with FAILED_PRECONDITION — HTTP 400 through the gateway's
// status mapping — instead of implicitly creating the session.
func TestPresetSendMessageWithoutCreate(t *testing.T) {
	baseURL := testtool.MustEndpoint("http", "public")
	envName := testtool.MustEnv()
	ctx := traceContext(t)

	// Each case targets a distinct never-created conversation id, so the
	// rejection must fire on EVERY send rather than on the first alone.
	tests := []struct {
		name           string
		conversationID string
	}{
		{name: "first send on a never-created conversation", conversationID: "preset-never-created"},
		{name: "repeated send on never-created conversations", conversationID: "preset-never-created-2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when: the turn fires on the never-created conversation.
			status, respBody := postChatTurn(t, ctx, baseURL, envName, tt.conversationID, []byte(`{"message": "hello"}`))

			// then: FAILED_PRECONDITION maps to HTTP 400 (grpc-gateway
			// status mapping) and the reply body names the remedy.
			if status != http.StatusBadRequest {
				t.Errorf("status = %d, want %d (body: %s)", status, http.StatusBadRequest, respBody)
			}
		})
	}
}

// TestPresetUnknownPresetRejected verifies the unknown-preset edge: a
// conversation naming a preset no root supplies is rejected with
// INVALID_ARGUMENT (HTTP 400) carrying the roster's available-ids message,
// and no conversation is created behind the failure.
func TestPresetUnknownPresetRejected(t *testing.T) {
	baseURL := testtool.MustEndpoint("http", "public")
	envName := testtool.MustEnv()
	ctx := traceContext(t)

	// given: a conversation id free to use and an unknown preset id.
	const conversationID = "preset-unknown"

	// when: the creation names the unknown preset.
	status, respBody := createConversation(t, ctx, baseURL, envName, conversationID, "no-such-preset")

	// then: the request is rejected as INVALID_ARGUMENT (HTTP 400).
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want %d (body: %s)", status, http.StatusBadRequest, respBody)
	}

	// and: no conversation exists behind the failed create — a subsequent
	// send is still FAILED_PRECONDITION, proving nothing was lazily
	// created. The 400s above are ambiguous between INVALID_ARGUMENT and
	// FAILED_PRECONDITION, so the body is pinned to the not-created
	// message ("call CreateConversation first") which only the
	// FAILED_PRECONDITION path produces.
	status, respBody = postChatTurn(t, ctx, baseURL, envName, conversationID, []byte(`{"message": "hello"}`))
	if status != http.StatusBadRequest {
		t.Errorf("follow-up send status = %d, want %d (body: %s)", status, http.StatusBadRequest, respBody)
	}
	if !strings.Contains(string(respBody), "CreateConversation") {
		t.Errorf("follow-up send body = %s, want it to name the CreateConversation remedy (FAILED_PRECONDITION message)", respBody)
	}
}

// TestPresetAuthoringLifecycle walks the full C1 closed loop (V4-1, the
// US2 independent test in specs/058-dsh-preset-roster-demo/spec.md):
// create from a template → a new conversation runs the authored persona →
// update the persona → the joined session keeps its generation while a new
// session lands on the new one (V3-2) → delete → the joined session stays
// servable while new binds are rejected (V4-3). Phase 1 additionally proves
// hot creation (V3-1): the copy is bindable with no restart, because roster
// discovery re-reads the roots on every resolve.
func TestPresetAuthoringLifecycle(t *testing.T) {
	baseURL := testtool.MustEndpoint("http", "public")
	envName := testtool.MustEnv()
	ctx := traceContext(t)

	const (
		presetID     = "authored-lifecycle"
		oldSession   = "preset-authored-old"
		newSession   = "preset-authored-new"
		demoTemplate = "demo-tools"
	)

	// ── Phase 1 (V3-1 hot creation, US2-AS1).
	// given: a preset authored from demo-tools with persona marker ONE.
	createBody := fmt.Sprintf(`{"preset_id": %q, "template": %q, "persona": %q}`,
		presetID, demoTemplate, personaAuthoredOne)
	status, respBody := presetRequest(t, ctx, baseURL, envName, http.MethodPost, "", []byte(createBody))
	if status != http.StatusOK {
		t.Fatalf("createPreset status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	created := new(presetResponse)
	if err := json.Unmarshal(respBody, created); err != nil {
		t.Fatalf("createPreset: json.Unmarshal(%s) unexpected error: %v", respBody, err)
	}
	if created.Name != "presets/"+presetID || created.Template != demoTemplate || created.Persona != personaAuthoredOne {
		t.Errorf("createPreset view = %+v, want presets/%s on %s with persona marker one", created, presetID, demoTemplate)
	}

	// when: a NEW conversation binds the freshly created preset — no
	// restart in between (hot discovery).
	status, respBody = createConversation(t, ctx, baseURL, envName, oldSession, presetID)
	if status != http.StatusOK {
		t.Fatalf("createConversation status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	bound := new(conversationResponse)
	if err := json.Unmarshal(respBody, bound); err != nil {
		t.Fatalf("createConversation: json.Unmarshal(%s) unexpected error: %v", respBody, err)
	}
	if bound.Preset != presetID {
		t.Errorf("resolved preset = %q, want %q", bound.Preset, presetID)
	}

	// then: the authored persona reached the model — the marker-one
	// template answers the probe.
	status, respBody = postChatTurn(t, ctx, baseURL, envName, oldSession, []byte(`{"message": "authored-probe"}`))
	if status != http.StatusOK {
		t.Fatalf("sendMessage status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	got := new(sendMessageResponse)
	if err := json.Unmarshal(respBody, got); err != nil {
		t.Fatalf("sendMessage: json.Unmarshal(%s) unexpected error: %v", respBody, err)
	}
	if got.Reply != personaAuthoredOneReply {
		t.Errorf("reply = %q, want %q (the authored generation must be live)", got.Reply, personaAuthoredOneReply)
	}

	// ── Phase 2 (V3-2 generation switch, US2-AS2).
	// when: the persona is updated to marker TWO.
	updateBody := fmt.Sprintf(`{"updateMask": "persona", "persona": %q}`, personaAuthoredTwo)
	status, respBody = presetRequest(t, ctx, baseURL, envName, http.MethodPatch, "/"+presetID, []byte(updateBody))
	if status != http.StatusOK {
		t.Fatalf("updatePreset status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	updated := new(presetResponse)
	if err := json.Unmarshal(respBody, updated); err != nil {
		t.Fatalf("updatePreset: json.Unmarshal(%s) unexpected error: %v", respBody, err)
	}
	if updated.Persona != personaAuthoredTwo {
		t.Errorf("updatePreset persona = %q, want %q", updated.Persona, personaAuthoredTwo)
	}

	// then: the joined session keeps its generation — the probe still
	// answers with marker ONE.
	status, respBody = postChatTurn(t, ctx, baseURL, envName, oldSession, []byte(`{"message": "authored-probe"}`))
	if status != http.StatusOK {
		t.Fatalf("old-session probe status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	got = new(sendMessageResponse)
	if err := json.Unmarshal(respBody, got); err != nil {
		t.Fatalf("old-session probe: json.Unmarshal(%s) unexpected error: %v", respBody, err)
	}
	if got.Reply != personaAuthoredOneReply {
		t.Errorf("old-session reply = %q, want %q (a joined session must keep its generation)", got.Reply, personaAuthoredOneReply)
	}

	// and: a NEW session lands on the new generation — marker TWO answers.
	status, respBody = createConversation(t, ctx, baseURL, envName, newSession, presetID)
	if status != http.StatusOK {
		t.Fatalf("new-session create status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	status, respBody = postChatTurn(t, ctx, baseURL, envName, newSession, []byte(`{"message": "authored-probe"}`))
	if status != http.StatusOK {
		t.Fatalf("new-session probe status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	got = new(sendMessageResponse)
	if err := json.Unmarshal(respBody, got); err != nil {
		t.Fatalf("new-session probe: json.Unmarshal(%s) unexpected error: %v", respBody, err)
	}
	if got.Reply != personaAuthoredTwoReply {
		t.Errorf("new-session reply = %q, want %q (a new session must get the new generation)", got.Reply, personaAuthoredTwoReply)
	}

	// ── Phase 3 (V4-3 delete semantics, US2-AS3).
	// when: the preset is deleted.
	status, respBody = presetRequest(t, ctx, baseURL, envName, http.MethodDelete, "/"+presetID, nil)
	if status != http.StatusOK {
		t.Fatalf("deletePreset status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}

	// then: the joined session keeps its standing mount — the probe still
	// answers with its generation (marker ONE).
	status, respBody = postChatTurn(t, ctx, baseURL, envName, oldSession, []byte(`{"message": "authored-probe"}`))
	if status != http.StatusOK {
		t.Fatalf("post-delete probe status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	got = new(sendMessageResponse)
	if err := json.Unmarshal(respBody, got); err != nil {
		t.Fatalf("post-delete probe: json.Unmarshal(%s) unexpected error: %v", respBody, err)
	}
	if got.Reply != personaAuthoredOneReply {
		t.Errorf("post-delete reply = %q, want %q (a joined session must survive the delete)", got.Reply, personaAuthoredOneReply)
	}

	// and: a new bind to the deleted id is rejected — the roster no longer
	// resolves it, so this is INVALID_ARGUMENT (HTTP 400). The 400 alone is
	// ambiguous with FAILED_PRECONDITION, so the body is pinned to the
	// rejected preset id: the resolve-failure message carries the id, while
	// the not-created FAILED_PRECONDITION message does not (the body-pin
	// disambiguation pattern of TestPresetUnknownPresetRejected).
	status, respBody = createConversation(t, ctx, baseURL, envName, "preset-authored-after-delete", presetID)
	if status != http.StatusBadRequest {
		t.Errorf("createConversation on deleted preset status = %d, want %d (body: %s)", status, http.StatusBadRequest, respBody)
	}
	if !strings.Contains(string(respBody), presetID) {
		t.Errorf("createConversation on deleted preset body = %s, want it to name the rejected preset id %q (INVALID_ARGUMENT resolve failure)", respBody, presetID)
	}

	// and: the resource is gone.
	status, respBody = presetRequest(t, ctx, baseURL, envName, http.MethodGet, "/"+presetID, nil)
	if status != http.StatusNotFound {
		t.Errorf("getPreset after delete status = %d, want %d (body: %s)", status, http.StatusNotFound, respBody)
	}
}

// TestPresetCreateRejections covers the creation rejection edges (US2-AS4,
// specs/058-dsh-preset-roster-demo/contracts/chat-api.md §2 CreatePreset
// row): a duplicate id → ALREADY_EXISTS (HTTP 409) with the original
// resource intact, an unknown template / malformed id / empty persona →
// INVALID_ARGUMENT (HTTP 400), and every rejection leaves NO
// half-materialized state — the store never records the id (GetPreset →
// 404), the collection size never moves, and the retry of a rejected id
// with a VALID template succeeds (the roster copy refuses a taken id, so
// the success proves the writable root never held the directory either).
func TestPresetCreateRejections(t *testing.T) {
	baseURL := testtool.MustEndpoint("http", "public")
	envName := testtool.MustEnv()
	ctx := traceContext(t)

	const (
		presetID    = "authored-rejections"
		rejectedID  = "authored-reject-retry"
		ghostID     = "authored-reject-ghost"
		demoDefault = "demo-standard"
	)

	// given: one authored preset exists and the collection size is pinned.
	createBody := fmt.Sprintf(`{"preset_id": %q, "template": %q, "persona": %q}`,
		presetID, demoDefault, personaAuthoredOne)
	status, respBody := presetRequest(t, ctx, baseURL, envName, http.MethodPost, "", []byte(createBody))
	if status != http.StatusOK {
		t.Fatalf("createPreset status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	status, respBody = presetRequest(t, ctx, baseURL, envName, http.MethodGet, "", nil)
	if status != http.StatusOK {
		t.Fatalf("listPresets status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	baseline := new(listPresetsResponse)
	if err := json.Unmarshal(respBody, baseline); err != nil {
		t.Fatalf("listPresets: json.Unmarshal(%s) unexpected error: %v", respBody, err)
	}

	// when: the same id is created again.
	duplicateBody := fmt.Sprintf(`{"preset_id": %q, "template": %q, "persona": %q}`,
		presetID, demoDefault, personaAuthoredTwo)
	status, respBody = presetRequest(t, ctx, baseURL, envName, http.MethodPost, "", []byte(duplicateBody))

	// then: ALREADY_EXISTS (HTTP 409), the original resource is intact, and
	// the collection did not grow.
	if status != http.StatusConflict {
		t.Errorf("duplicate createPreset status = %d, want %d (body: %s)", status, http.StatusConflict, respBody)
	}
	status, respBody = presetRequest(t, ctx, baseURL, envName, http.MethodGet, "/"+presetID, nil)
	if status != http.StatusOK {
		t.Fatalf("getPreset after duplicate status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	original := new(presetResponse)
	if err := json.Unmarshal(respBody, original); err != nil {
		t.Fatalf("getPreset after duplicate: json.Unmarshal(%s) unexpected error: %v", respBody, err)
	}
	if original.Persona != personaAuthoredOne {
		t.Errorf("original persona = %q, want %q (a rejected create must not touch the resource)", original.Persona, personaAuthoredOne)
	}
	status, respBody = presetRequest(t, ctx, baseURL, envName, http.MethodGet, "", nil)
	if status != http.StatusOK {
		t.Fatalf("listPresets after duplicate status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	afterDuplicate := new(listPresetsResponse)
	if err := json.Unmarshal(respBody, afterDuplicate); err != nil {
		t.Fatalf("listPresets after duplicate: json.Unmarshal(%s) unexpected error: %v", respBody, err)
	}
	if len(afterDuplicate.Presets) != len(baseline.Presets) {
		t.Errorf("presets count after duplicate = %d, want %d (a rejected create must not record anything)",
			len(afterDuplicate.Presets), len(baseline.Presets))
	}

	// when: an unknown template is referenced by a fresh id.
	unknownBody := fmt.Sprintf(`{"preset_id": %q, "template": "no-such-template", "persona": %q}`,
		rejectedID, personaAuthoredOne)
	status, respBody = presetRequest(t, ctx, baseURL, envName, http.MethodPost, "", []byte(unknownBody))

	// then: INVALID_ARGUMENT (HTTP 400) and nothing behind it — no store
	// record, no collection growth.
	if status != http.StatusBadRequest {
		t.Errorf("unknown-template createPreset status = %d, want %d (body: %s)", status, http.StatusBadRequest, respBody)
	}
	status, respBody = presetRequest(t, ctx, baseURL, envName, http.MethodGet, "/"+rejectedID, nil)
	if status != http.StatusNotFound {
		t.Errorf("getPreset(%s) after rejection status = %d, want %d (body: %s)", rejectedID, status, http.StatusNotFound, respBody)
	}
	status, respBody = presetRequest(t, ctx, baseURL, envName, http.MethodGet, "", nil)
	if status != http.StatusOK {
		t.Fatalf("listPresets after unknown-template status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	afterUnknown := new(listPresetsResponse)
	if err := json.Unmarshal(respBody, afterUnknown); err != nil {
		t.Fatalf("listPresets after unknown-template: json.Unmarshal(%s) unexpected error: %v", respBody, err)
	}
	if len(afterUnknown.Presets) != len(baseline.Presets) {
		t.Errorf("presets count after unknown-template = %d, want %d (a rejected create must not record anything)",
			len(afterUnknown.Presets), len(baseline.Presets))
	}

	// and: no half-materialized residue — the retry of the SAME id with a
	// VALID template succeeds. The roster copy refuses an id a directory
	// already occupies, so this success proves the rejected create left
	// nothing in the writable root either.
	retryBody := fmt.Sprintf(`{"preset_id": %q, "template": %q, "persona": %q}`,
		rejectedID, demoDefault, personaAuthoredTwo)
	status, respBody = presetRequest(t, ctx, baseURL, envName, http.MethodPost, "", []byte(retryBody))
	if status != http.StatusOK {
		t.Fatalf("retry createPreset status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	retried := new(presetResponse)
	if err := json.Unmarshal(respBody, retried); err != nil {
		t.Fatalf("retry createPreset: json.Unmarshal(%s) unexpected error: %v", respBody, err)
	}
	if retried.Persona != personaAuthoredTwo {
		t.Errorf("retried persona = %q, want %q", retried.Persona, personaAuthoredTwo)
	}

	// when/then: a malformed id is rejected before any materialization.
	malformedBody := fmt.Sprintf(`{"preset_id": %q, "template": %q, "persona": %q}`,
		"Authored_X", demoDefault, personaAuthoredOne)
	status, respBody = presetRequest(t, ctx, baseURL, envName, http.MethodPost, "", []byte(malformedBody))
	if status != http.StatusBadRequest {
		t.Errorf("malformed-id createPreset status = %d, want %d (body: %s)", status, http.StatusBadRequest, respBody)
	}

	// when/then: an empty persona is rejected.
	emptyPersonaBody := fmt.Sprintf(`{"preset_id": %q, "template": %q, "persona": ""}`,
		"authored-reject-empty", demoDefault)
	status, respBody = presetRequest(t, ctx, baseURL, envName, http.MethodPost, "", []byte(emptyPersonaBody))
	if status != http.StatusBadRequest {
		t.Errorf("empty-persona createPreset status = %d, want %d (body: %s)", status, http.StatusBadRequest, respBody)
	}

	// when/then: updating an unknown id is NOT_FOUND (HTTP 404), and an
	// empty update_mask is INVALID_ARGUMENT (HTTP 400).
	patchBody := fmt.Sprintf(`{"updateMask": "persona", "persona": %q}`, personaAuthoredTwo)
	status, respBody = presetRequest(t, ctx, baseURL, envName, http.MethodPatch, "/"+ghostID, []byte(patchBody))
	if status != http.StatusNotFound {
		t.Errorf("updatePreset(unknown) status = %d, want %d (body: %s)", status, http.StatusNotFound, respBody)
	}
	status, respBody = presetRequest(t, ctx, baseURL, envName, http.MethodPatch, "/"+presetID, []byte(`{}`))
	if status != http.StatusBadRequest {
		t.Errorf("updatePreset(empty mask) status = %d, want %d (body: %s)", status, http.StatusBadRequest, respBody)
	}
}

// TestPresetTemplateDeleteRefused verifies that a deployment template is
// not deletable (specs/058-dsh-preset-roster-demo/contracts/chat-api.md §2
// DeletePreset row): the roster refuses to remove system-trust data and the
// refusal surfaces as FAILED_PRECONDITION (HTTP 400). The template stays
// fully servable afterwards — a conversation bound to it still resolves and
// answers its persona probe.
func TestPresetTemplateDeleteRefused(t *testing.T) {
	baseURL := testtool.MustEndpoint("http", "public")
	envName := testtool.MustEnv()
	ctx := traceContext(t)

	// when: the template id is deleted.
	status, respBody := presetRequest(t, ctx, baseURL, envName, http.MethodDelete, "/demo-standard", nil)

	// then: the roster's system-trust refusal surfaces (HTTP 400, the
	// FAILED_PRECONDITION mapping).
	if status != http.StatusBadRequest {
		t.Errorf("deletePreset(template) status = %d, want %d (body: %s)", status, http.StatusBadRequest, respBody)
	}

	// and: the template is untouched — a conversation bound to it resolves
	// and the template persona answers the probe.
	status, respBody = createConversation(t, ctx, baseURL, envName, "preset-template-intact", "demo-standard")
	if status != http.StatusOK {
		t.Fatalf("createConversation on template status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	status, respBody = postChatTurn(t, ctx, baseURL, envName, "preset-template-intact", []byte(`{"message": "preset-probe"}`))
	if status != http.StatusOK {
		t.Fatalf("template probe status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	got := new(sendMessageResponse)
	if err := json.Unmarshal(respBody, got); err != nil {
		t.Fatalf("template probe: json.Unmarshal(%s) unexpected error: %v", respBody, err)
	}
	if got.Reply != personaStandardReply {
		t.Errorf("template probe reply = %q, want %q (the template must keep serving)", got.Reply, personaStandardReply)
	}
}
