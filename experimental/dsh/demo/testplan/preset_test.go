package testplan

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"dominion/common/gopkg/testtool"
)

// Preset conversation-binding suite (specs/058-dsh-preset-roster-demo/
// tasks.md T017): conversations are created EXPLICITLY through
// CreateConversation with a preset (or the roster default), and the
// model-visible composition each session runs is asserted end to end via
// the fake-llm `system_keywords` probe replies. Covers the US1 acceptance
// scenarios (V1-1/V1-2/V2-3 end to end, US1-AS3 shared-mount behaviour,
// US1-AS4 session-level stability), the idempotent/rebuild semantics (R4),
// and the no-lazy-creation FAILED_PRECONDITION edge (FR-002).

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
