package testplan

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"dominion/common/gopkg/testtool"
)

// Preset module suite over the public HTTP entry, two faces. The suite runs
// on the 060 preset derivation semantics
// (specs/060-agent-v2-team-optimize/contracts/preset-derivation.md): the
// store is the single source of truth, a conversation's preset MUST name a
// store record (template ids are derivation sources, no longer directly
// composable), and the composition is derived per session.
//
//   - The conversation-binding face: conversations are created EXPLICITLY
//     through CreateConversation with a store preset the suite authored from
//     a deployment template (mustCreatePreset), and the model-visible
//     composition each session runs is asserted end to end via the fake-llm
//     `system_keywords` probe replies. Covers the 058 US1 acceptance
//     scenarios re-based on store presets (composition difference + guidance
//     presence, per-session derived mounts, session-level stability), the
//     mandatory-preset rejection, the idempotent/rebuild semantics (R4), and
//     the no-lazy-creation FAILED_PRECONDITION edge (FR-002).
//   - The resource face: the PresetService CRUD closed loop over store-only
//     authoring. Covers hot creation, the generation switch on persona
//     update, the full lifecycle create→bind→update→delete, delete
//     semantics, and the creation rejection edges with no residue.
//
// Broken-template presentation is NOT carried by large-test steps: the plugin
// refuses a broken template at create (INVALID_ARGUMENT) and derives
// compositions at use time, so the case lives in the plugin unit tests
// (common/js/dsh-plugins/preset-authoring/src/{derive,index}.test.ts).

// TestPresetConversationComposition verifies that sessions bound to two
// store presets derived from different templates present DIFFERENT
// model-visible compositions, and the demo-echo guidance is present exactly
// when the template carries its row. Each case creates a fresh conversation
// bound to its preset and sends the probe keyword: the fake-llm answers with
// the template whose system_keywords match the session's persona/guidance, so
// the reply names the composition the model actually received. A
// standard-derived session probed for guidance falls to the deterministic
// farewell fallback — the guidance-absent proof.
func TestPresetConversationComposition(t *testing.T) {
	baseURL := testtool.MustEndpoint("http", "public")
	envName := testtool.MustEnv()
	ctx := traceContext(t)

	// given: one store preset per template, carrying the template's own
	// persona so the model-visible composition matches the template's.
	mustCreatePreset(t, ctx, baseURL, envName, authoredToolsPresetID, "demo-tools", templatePersonaTools)
	mustCreatePreset(t, ctx, baseURL, envName, authoredStandardPresetID, "demo-standard", templatePersonaStandard)

	tests := []struct {
		name      string
		preset    string
		probe     string
		wantReply string
	}{
		{
			name:      "tools-derived preset probe hits the tools persona",
			preset:    authoredToolsPresetID,
			probe:     "preset-probe",
			wantReply: personaToolsReply,
		},
		{
			name:      "standard-derived preset probe hits the standard persona",
			preset:    authoredStandardPresetID,
			probe:     "preset-probe",
			wantReply: personaStandardReply,
		},
		{
			name:      "tools-derived preset sees the demo_echo guidance",
			preset:    authoredToolsPresetID,
			probe:     "guidance-probe",
			wantReply: guidanceReply,
		},
		{
			name:      "standard-derived preset answers the guidance probe with the fallback",
			preset:    authoredStandardPresetID,
			probe:     "guidance-probe",
			wantReply: farewellText,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given: one conversation explicitly bound to the store preset.
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
			if created.Preset != tt.preset {
				t.Errorf("resolved preset = %q, want %q", created.Preset, tt.preset)
			}
			if got.Reply != tt.wantReply {
				t.Errorf("reply = %q, want %q (model-visible composition must match the preset)", got.Reply, tt.wantReply)
			}
		})
	}
}

// TestPresetWithoutPresetRejected verifies the mandatory-preset semantics
// (060 preset derivation; the 058 roster default is gone): CreateConversation
// with no preset field at all reaches compose(undefined), which is rejected
// INVALID_ARGUMENT (HTTP 400), and no conversation exists behind the failure.
func TestPresetWithoutPresetRejected(t *testing.T) {
	baseURL := testtool.MustEndpoint("http", "public")
	envName := testtool.MustEnv()
	ctx := traceContext(t)

	// when: a conversation is created with no preset field.
	const conversationID = "preset-without-preset"
	status, respBody := createConversationWithoutPreset(t, ctx, baseURL, envName, conversationID)

	// then: the mandatory-preset rejection surfaces (HTTP 400).
	if status != http.StatusBadRequest {
		t.Fatalf("createConversation status = %d, want %d (body: %s)", status, http.StatusBadRequest, respBody)
	}
	if !strings.Contains(string(respBody), "preset id is required") {
		t.Errorf("createConversation body = %s, want the mandatory-preset message", respBody)
	}

	// and: no conversation exists behind the failed create — a subsequent
	// send is the not-created FAILED_PRECONDITION (its body names the
	// CreateConversation remedy).
	status, respBody = postChatTurn(t, ctx, baseURL, envName, conversationName(conversationID), []byte(`{"message": "hello"}`))
	if status != http.StatusBadRequest {
		t.Errorf("follow-up send status = %d, want %d (body: %s)", status, http.StatusBadRequest, respBody)
	}
	if !strings.Contains(string(respBody), "CreateConversation") {
		t.Errorf("follow-up send body = %s, want it to name the CreateConversation remedy (FAILED_PRECONDITION message)", respBody)
	}
}

// TestPresetSamePresetSharedBehaviour verifies that two conversations bound
// to the SAME store preset behave identically: the composition is derived per
// session (each session holds its own mount), and both report the same
// resolved preset and answer the probe with the same composition.
func TestPresetSamePresetSharedBehaviour(t *testing.T) {
	baseURL := testtool.MustEndpoint("http", "public")
	envName := testtool.MustEnv()
	ctx := traceContext(t)

	// given: two conversations bound to the tools-derived store preset.
	mustCreatePreset(t, ctx, baseURL, envName, authoredToolsPresetID, "demo-tools", templatePersonaTools)
	const (
		conversationA = "preset-shared-a"
		conversationB = "preset-shared-b"
	)
	resolved := map[string]string{}
	for _, id := range []string{conversationA, conversationB} {
		status, respBody := createConversation(t, ctx, baseURL, envName, id, authoredToolsPresetID)
		if status != http.StatusOK {
			t.Fatalf("createConversation(%s) status = %d, want %d (body: %s)", id, status, http.StatusOK, respBody)
		}
		created := new(conversationResponse)
		if err := json.Unmarshal(respBody, created); err != nil {
			t.Fatalf("createConversation(%s): json.Unmarshal(%s) unexpected error: %v", id, respBody, err)
		}
		if created.Preset != authoredToolsPresetID {
			t.Errorf("createConversation(%s) resolved preset = %q, want %q", id, created.Preset, authoredToolsPresetID)
		}
		resolved[id] = created.Preset
	}

	// when: both conversations send the probe turn.
	replies := map[string]string{}
	for _, id := range []string{conversationA, conversationB} {
		status, respBody := postChatTurn(t, ctx, baseURL, envName, conversationName(id), []byte(`{"message": "preset-probe"}`))
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
		if presetID != authoredToolsPresetID {
			t.Errorf("conversation %s binding = %q, want %q", id, presetID, authoredToolsPresetID)
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

	// given: one tools-derived store preset and a conversation bound to it.
	mustCreatePreset(t, ctx, baseURL, envName, authoredToolsPresetID, "demo-tools", templatePersonaTools)
	const conversationID = "preset-stability"
	status, respBody := createConversation(t, ctx, baseURL, envName, conversationID, authoredToolsPresetID)
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
		status, respBody := postChatTurn(t, ctx, baseURL, envName, conversationName(conversationID), []byte(`{"message": "`+tt.message+`"}`))

		// then: the reply matches the turn's expectation — the binding
		// stays on the store preset across the conversation's lifetime.
		if status != http.StatusOK {
			t.Fatalf("turn %d: status = %d, want %d (body: %s)", tt.turn, status, http.StatusOK, respBody)
		}
		got := new(sendMessageResponse)
		if err := json.Unmarshal(respBody, got); err != nil {
			t.Fatalf("turn %d: json.Unmarshal(%s) unexpected error: %v", tt.turn, respBody, err)
		}
		if got.Reply != tt.wantReply {
			t.Errorf("turn %d reply = %q, want %q (composition must stay bound to the store preset)", tt.turn, got.Reply, tt.wantReply)
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

	// given: the two template-derived store presets.
	mustCreatePreset(t, ctx, baseURL, envName, authoredToolsPresetID, "demo-tools", templatePersonaTools)
	mustCreatePreset(t, ctx, baseURL, envName, authoredStandardPresetID, "demo-standard", templatePersonaStandard)

	// given: a conversation bound to the tools-derived preset.
	const conversationID = "preset-rebuild"
	status, respBody := createConversation(t, ctx, baseURL, envName, conversationID, authoredToolsPresetID)
	if status != http.StatusOK {
		t.Fatalf("create tools preset: status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	first := new(conversationResponse)
	if err := json.Unmarshal(respBody, first); err != nil {
		t.Fatalf("create tools preset: json.Unmarshal(%s) unexpected error: %v", respBody, err)
	}

	// when/then: the same id + same preset answers idempotently.
	status, respBody = createConversation(t, ctx, baseURL, envName, conversationID, authoredToolsPresetID)
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

	// when: the same id is recreated on the standard-derived preset (rebuild).
	status, respBody = createConversation(t, ctx, baseURL, envName, conversationID, authoredStandardPresetID)
	if status != http.StatusOK {
		t.Fatalf("rebuild on standard preset: status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	rebuilt := new(conversationResponse)
	if err := json.Unmarshal(respBody, rebuilt); err != nil {
		t.Fatalf("rebuild: json.Unmarshal(%s) unexpected error: %v", respBody, err)
	}
	if rebuilt.Preset != authoredStandardPresetID {
		t.Fatalf("rebuild resolved preset = %q, want %q", rebuilt.Preset, authoredStandardPresetID)
	}

	// then: the rebuilt session runs the NEW composition — the standard
	// persona answers the probe (and the tools persona cannot).
	status, respBody = postChatTurn(t, ctx, baseURL, envName, conversationName(conversationID), []byte(`{"message": "preset-probe"}`))
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
			// when: the turn fires on the never-created conversation. The
			// full resource name is required for the request to reach the
			// handler at all — a bare id would miss the gateway route
			// (404) instead of exercising the FAILED_PRECONDITION edge.
			status, respBody := postChatTurn(t, ctx, baseURL, envName, conversationName(tt.conversationID), []byte(`{"message": "hello"}`))

			// then: FAILED_PRECONDITION maps to HTTP 400 (grpc-gateway
			// status mapping) and the reply body names the remedy.
			if status != http.StatusBadRequest {
				t.Errorf("status = %d, want %d (body: %s)", status, http.StatusBadRequest, respBody)
			}
		})
	}
}

// TestPresetUnknownPresetRejected verifies the unknown-preset edge: a
// conversation naming a preset the store does not hold is rejected with
// NOT_FOUND (HTTP 404) naming the id, and no conversation is created behind
// the failure.
func TestPresetUnknownPresetRejected(t *testing.T) {
	baseURL := testtool.MustEndpoint("http", "public")
	envName := testtool.MustEnv()
	ctx := traceContext(t)

	// given: a conversation id free to use and an unknown preset id.
	const conversationID = "preset-unknown"

	// when: the creation names the unknown preset.
	status, respBody := createConversation(t, ctx, baseURL, envName, conversationID, "no-such-preset")

	// then: the store miss surfaces as NOT_FOUND (HTTP 404) naming the id.
	if status != http.StatusNotFound {
		t.Errorf("status = %d, want %d (body: %s)", status, http.StatusNotFound, respBody)
	}
	if !strings.Contains(string(respBody), "no-such-preset") {
		t.Errorf("body = %s, want it to name the unknown preset id", respBody)
	}

	// and: no conversation exists behind the failed create — a subsequent
	// send is still FAILED_PRECONDITION, proving nothing was lazily
	// created. The 400s above are ambiguous between INVALID_ARGUMENT and
	// FAILED_PRECONDITION, so the body is pinned to the not-created
	// message ("call CreateConversation first") which only the
	// FAILED_PRECONDITION path produces.
	status, respBody = postChatTurn(t, ctx, baseURL, envName, conversationName(conversationID), []byte(`{"message": "hello"}`))
	if status != http.StatusBadRequest {
		t.Errorf("follow-up send status = %d, want %d (body: %s)", status, http.StatusBadRequest, respBody)
	}
	if !strings.Contains(string(respBody), "CreateConversation") {
		t.Errorf("follow-up send body = %s, want it to name the CreateConversation remedy (FAILED_PRECONDITION message)", respBody)
	}
}

// TestPresetAuthoringLifecycle walks the full store-only authoring closed
// loop (V4-1, the US2 independent test in specs/058-dsh-preset-roster-demo/
// spec.md, re-based on 060 derivation): create a store record from a template
// → a new conversation runs the authored persona → update the persona → the
// joined session keeps its derived composition while a new session lands on
// the new one (V3-2) → delete → the joined session stays servable while new
// binds are rejected NOT_FOUND. Phase 1 additionally proves hot creation
// (V3-1): the record is bindable with no restart, because compose reads the
// store per use.
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
	// restart in between (hot store read).
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
	status, respBody = postChatTurn(t, ctx, baseURL, envName, conversationName(oldSession), []byte(`{"message": "authored-probe"}`))
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

	// then: the joined session keeps its derived composition — the probe
	// still answers with marker ONE.
	status, respBody = postChatTurn(t, ctx, baseURL, envName, conversationName(oldSession), []byte(`{"message": "authored-probe"}`))
	if status != http.StatusOK {
		t.Fatalf("old-session probe status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	got = new(sendMessageResponse)
	if err := json.Unmarshal(respBody, got); err != nil {
		t.Fatalf("old-session probe: json.Unmarshal(%s) unexpected error: %v", respBody, err)
	}
	if got.Reply != personaAuthoredOneReply {
		t.Errorf("old-session reply = %q, want %q (a joined session must keep its composition)", got.Reply, personaAuthoredOneReply)
	}

	// and: a NEW session lands on the new generation — marker TWO answers.
	status, respBody = createConversation(t, ctx, baseURL, envName, newSession, presetID)
	if status != http.StatusOK {
		t.Fatalf("new-session create status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	status, respBody = postChatTurn(t, ctx, baseURL, envName, conversationName(newSession), []byte(`{"message": "authored-probe"}`))
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

	// then: the joined session keeps its derived composition — the probe
	// still answers with its generation (marker ONE).
	status, respBody = postChatTurn(t, ctx, baseURL, envName, conversationName(oldSession), []byte(`{"message": "authored-probe"}`))
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

	// and: a new bind to the deleted id is rejected — the store no longer
	// holds the record, so this is NOT_FOUND (HTTP 404). The body is pinned
	// to the rejected preset id: the store-miss message carries the id,
	// while the not-created FAILED_PRECONDITION message does not (the
	// body-pin disambiguation pattern of TestPresetUnknownPresetRejected).
	status, respBody = createConversation(t, ctx, baseURL, envName, "preset-authored-after-delete", presetID)
	if status != http.StatusNotFound {
		t.Errorf("createConversation on deleted preset status = %d, want %d (body: %s)", status, http.StatusNotFound, respBody)
	}
	if !strings.Contains(string(respBody), presetID) {
		t.Errorf("createConversation on deleted preset body = %s, want it to name the rejected preset id %q (NOT_FOUND store miss)", respBody, presetID)
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
// resource intact, a deployment template id cannot be claimed → 409, an
// unknown template / malformed id / empty persona → INVALID_ARGUMENT
// (HTTP 400), and every rejection leaves NO residue — the store never
// records the id (GetPreset → 404), the collection size never moves, and
// the retry of a rejected id with a VALID template succeeds.
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

	// when: a deployment template id is claimed by a fresh create.
	// then: ALREADY_EXISTS (HTTP 409) — the roster supplies it as system
	// trust, so a store record can never shadow it.
	templateIDBody := fmt.Sprintf(`{"preset_id": %q, "template": %q, "persona": %q}`,
		"demo-tools", demoDefault, personaAuthoredOne)
	status, respBody = presetRequest(t, ctx, baseURL, envName, http.MethodPost, "", []byte(templateIDBody))
	if status != http.StatusConflict {
		t.Errorf("template-id createPreset status = %d, want %d (body: %s)", status, http.StatusConflict, respBody)
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

	// and: no residue — the rejected id was never recorded, so the retry of
	// the SAME id with a VALID template succeeds.
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

	// when/then: a malformed id is rejected before any store write.
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
// DeletePreset row): the plugin refuses to remove system-trust data and the
// refusal surfaces as FAILED_PRECONDITION (HTTP 400). The template stays
// fully servable as a derivation source afterwards — a store preset authored
// from it resolves and answers its persona probe.
func TestPresetTemplateDeleteRefused(t *testing.T) {
	baseURL := testtool.MustEndpoint("http", "public")
	envName := testtool.MustEnv()
	ctx := traceContext(t)

	// when: the template id is deleted.
	status, respBody := presetRequest(t, ctx, baseURL, envName, http.MethodDelete, "/demo-standard", nil)

	// then: the system-trust refusal surfaces (HTTP 400, the
	// FAILED_PRECONDITION mapping).
	if status != http.StatusBadRequest {
		t.Errorf("deletePreset(template) status = %d, want %d (body: %s)", status, http.StatusBadRequest, respBody)
	}

	// and: the template is untouched — a preset authored from it resolves
	// and the template persona answers the probe.
	const intactPresetID = "preset-template-intact"
	mustCreatePreset(t, ctx, baseURL, envName, intactPresetID, "demo-standard", templatePersonaStandard)
	status, respBody = createConversation(t, ctx, baseURL, envName, "preset-template-intact-conv", intactPresetID)
	if status != http.StatusOK {
		t.Fatalf("createConversation on template-derived preset status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	status, respBody = postChatTurn(t, ctx, baseURL, envName, conversationName("preset-template-intact-conv"), []byte(`{"message": "preset-probe"}`))
	if status != http.StatusOK {
		t.Fatalf("template-derived probe status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	got := new(sendMessageResponse)
	if err := json.Unmarshal(respBody, got); err != nil {
		t.Fatalf("template-derived probe: json.Unmarshal(%s) unexpected error: %v", respBody, err)
	}
	if got.Reply != personaStandardReply {
		t.Errorf("template-derived probe reply = %q, want %q (the template must keep serving as a derivation source)", got.Reply, personaStandardReply)
	}
}
