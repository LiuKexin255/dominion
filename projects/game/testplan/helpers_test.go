// Package testplan contains shared types and helpers used across all
// game agent integration test files.
package testplan

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	game "dominion/projects/game"
	"dominion/projects/game/pkg/gameconst"

	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// ─── Constants ──────────────────────────────────────────────────────────────

const (
	headerEnv     = "env"
	pathPrefix    = "/api/v1/"
	wsReadTimeout = 30 * time.Second
)

// saoleiTemplateID is the saolei template path segment — the only known
// template (gameconst.SaoleiTemplate, spec 031-team-template-mode FR-001).
// Every resource in the new hierarchy hangs off "templates/saolei/..." per
// contracts/api-contract.md §1.
var saoleiTemplateID = gameconst.SaoleiTemplate.TemplateID

// Expected fake-llm response content. These MUST be kept in sync with
// projects/game/fake-llm/service/testdata/. The T1 unit test
// TestNewMessageStore_LoadsEmbeddedSamples pins the embedded testdata as the
// single source of truth — if testdata changes, that test fails first and
// reminds you to update the constants below in lockstep. See
// projects/game/testplan/README.md §5 for the full workflow.
const (
	expectedGreetingReasoning = "The user is greeting me, I should respond warmly."
	expectedGreetingText      = "Hello! How can I help you today?"
	expectedFarewellReasoning = "The user is saying goodbye."
	expectedFarewellText      = "Goodbye! Have a great day!"
	expectedChatReasoning     = "Responding with text only, no tools needed."
	expectedChatText          = "Sure, let's chat!"
)

// expectedPlannerStrategyText is the strategy content the fake-LLM's
// planner-update-strategy Message returns as the update_strategy tool_call
// argument (sample_planner_strategy.yaml). The saolei_team suite asserts it
// in the planner's tool_call args_json to prove the strategy was written
// (spec 031-team-template-mode FR-012/FR-014).
const expectedPlannerStrategyText = "优先翻开角落与边缘格子，命中数字 1 时先标记周围雷。"

// expectedPlannerUpdateText is the terminal text fake-LLM returns once the
// update_strategy tool-result loop closes (sample_update_strategy_tools.yaml
// update-strategy-success-text). It proves the planner agent's turn ended
// deterministically after writing the strategy.
const expectedPlannerUpdateText = "策略已更新，下一局将按新策略执行。"

// expectedPlayerCompressionSummary / expectedPlannerCompressionSummary are
// the plain-text responses the fake-LLM returns to the team graph COMPRESS
// node's two summary calls (sample_compression_player.yaml /
// sample_compression_planner.yaml). After the 5th game the compress node
// (team/compress.ts summarizeChannel) invokes the player/planner models
// directly with the summary prompts; the response text becomes the single
// post-compression channel message AND the live summary frame
// (specs/037-saolei-team-optimize FR-008/FR-011). The compression large
// tests assert both. MUST be kept in sync with the testdata — the T1 unit
// test TestNewMessageStore_LoadsEmbeddedSamples pins the embedded testdata.
const (
	expectedPlayerCompressionSummary  = "已玩 5 局，其中 4 局失败。策略：优先翻开角落与边缘格子，命中数字 1 时先标记周围雷。"
	expectedPlannerCompressionSummary = "已复盘 5 局，策略更新正常，每局均按新策略执行。"
)

// reviewInputPrefix is the fixed prefix of the planner's review input
// (team/planner.ts buildReviewInput renders the gameLog under "本局游戏过程：",
// specs/036-team-mode-bugfix/contracts/team-graph-fix-contract.md §2.2). It
// also keys the fake-LLM's planner-update-strategy config (sample_planner_
// strategy.yaml). The 037 large tests locate the real-time review frame and
// the reloaded review message by this prefix (FR-001/FR-002).
const reviewInputPrefix = "本局游戏过程"

// smallScreenshotData is a minimal 1×1 PNG used as screenshot payload in
// multimodal-turn tests. The fake-LLM ignores image bytes (only text blocks
// drive keyword matching), so the actual pixel content is irrelevant — tests
// only verify the server accepts and processes the multimodal frame.
var smallScreenshotData = mustBase64PNG()

// largeScreenshotData is a PNG whose encoded size exceeds the pre-025
// `coder/websocket` default ReadLimit of 32 KiB. It exercises the binary-
// protobuf WS path (spec 025 FR-007/FR-008/FR-010) — under the old protojson
// text + 32 KiB limit regime this frame would have torn the WS session down
// with `ErrMessageTooBig`. Built from a deterministic noise pattern so PNG
// compression cannot shrink it below the threshold.
var largeScreenshotData = mustLargePNG()

// mustBase64PNG decodes a minimal 1×1 transparent PNG. It panics on failure,
// which would indicate a bug in the test itself rather than the SUT.
func mustBase64PNG() []byte {
	raw, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVQI12NgAAIABQABNjN9GQAAAABJRf5ErkJggg==",
	)
	if err != nil {
		panic(fmt.Sprintf("decode test png: %v", err))
	}
	return raw
}

// mustLargePNG builds a deterministic PNG whose encoded size exceeds the
// pre-025 `coder/websocket` default ReadLimit (32 KiB). A high-entropy RGB
// pattern is used so PNG compression cannot deflate it below the threshold.
// Panics on encode failure (a bug in the test itself).
//
// Used by the large-image round-trip test (spec 025 FR-007/FR-010): under
// the old default 32 KiB read limit + protojson text regime, a frame carrying
// this image would have torn the WS session down with `ErrMessageTooBig`.
func mustLargePNG() []byte {
	const w, h = 128, 128 // 128×128 RGB ≈ 49 KiB raw, well above 32 KiB encoded
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// Deterministic high-entropy pattern (no LFSR/RNG dependency):
			// mix the coordinates so neighbouring pixels differ widely,
			// defeating PNG's DEFLATE.
			r := uint8((x*73 + y*151) ^ (x ^ y))
			g := uint8((x*197 + y*31) ^ (x * y))
			b := uint8((x*11 + y*223) ^ (x | y))
			img.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(fmt.Sprintf("encode large test png: %v", err))
	}
	return buf.Bytes()
}

// ─── JSON-response types (mirroring proto messages) ─────────────────────────

// sessionResponse mirrors the Session proto message returned via gRPC-gateway
// with protojson camelCase field names. template and session_id are carried by
// the name path segments (AIP-124; specs/035-proto-contract-refine/
// data-model.md §1.1), so the JSON carries only name + createTime.
type sessionResponse struct {
	Name       string `json:"name"`
	CreateTime string `json:"createTime"`
}

// listSessionsResponse mirrors the ListSessionsResponse proto message.
type listSessionsResponse struct {
	Sessions      []sessionResponse `json:"sessions"`
	NextPageToken string            `json:"nextPageToken"`
}

// ─── General Helpers ────────────────────────────────────────────────────────

// uniqueSuffix returns a short timestamp-based suffix to make resource names
// unique across test runs.
func uniqueSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano()%10000000)
}

// doHTTP executes an HTTP request and returns the response and body.
// Calls t.Fatal on connection or read errors.
func doHTTP(t *testing.T, method, rawurl, envName string, body []byte) (*http.Response, []byte) {
	t.Helper()

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, rawurl, bodyReader)
	if err != nil {
		t.Fatalf("http.NewRequest %s %s: %v", method, rawurl, err)
	}
	req.Header.Set(headerEnv, envName)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, rawurl, err)
	}

	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read response %s %s: %v", method, rawurl, err)
	}

	return resp, respBody
}

// buildWSURL constructs a WebSocket URL from the HTTP endpoint by replacing
// the scheme with ws or wss.
func buildWSURL(sutHostURL, path string) string {
	u, err := url.Parse(sutHostURL)
	if err != nil {
		panic(fmt.Sprintf("parse sutHostURL %q: %v", sutHostURL, err))
	}
	host := u.Host
	scheme := "ws"
	if u.Scheme == "https" {
		scheme = "wss"
	}
	return fmt.Sprintf("%s://%s%s", scheme, host, path)
}

// ─── Session Helpers (JSON-based) ───────────────────────────────────────────

// createSession sends a POST request with an empty CreateSessionRequest
// body ({}) to /api/v1/templates/{template}/sessions (AIP-133 — the parent
// template lives in the URI path, spec 031-team-template-mode
// contracts/api-contract.md §2.1) and returns the server-generated session
// ID together with the raw response body. The session ID is parsed from the
// response's name path segment (Session.session_id was removed,
// specs/035-proto-contract-refine/data-model.md §1.1). Calls t.Fatal on
// non-200 responses.
func createSession(t *testing.T, sutHostURL, sutEnvName, template string) (string, []byte) {
	t.Helper()

	reqBody := []byte("{}")
	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions", sutHostURL, pathPrefix, template)

	resp, respBody := doHTTP(t, http.MethodPost, reqURL, sutEnvName, reqBody)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST createSession status=%d, body=%s", resp.StatusCode, respBody)
	}

	sess := new(sessionResponse)
	if err := json.Unmarshal(respBody, sess); err != nil {
		t.Fatalf("json.Unmarshal createSession response: %v", err)
	}
	name, err := game.ParseSessionName(sess.Name)
	if err != nil {
		t.Fatalf("parse createSession response name %q: %v", sess.Name, err)
	}
	if name.SessionID == "" {
		t.Fatal("createSession: server returned empty session id in name")
	}
	return name.SessionID, respBody
}

// listSessions sends a GET request to list sessions of a template with the
// given page size and returns the raw response body. Calls t.Fatal on
// non-200 responses.
func listSessions(t *testing.T, sutHostURL, sutEnvName, template string, pageSize int) []byte {
	t.Helper()

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions?page_size=%d", sutHostURL, pathPrefix, template, pageSize)
	resp, respBody := doHTTP(t, http.MethodGet, reqURL, sutEnvName, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET listSessions status=%d, body=%s", resp.StatusCode, respBody)
	}
	return respBody
}

// getSession sends a GET request for a session and returns the response body.
// Calls t.Fatal on non-200 responses.
func getSession(t *testing.T, sutHostURL, sutEnvName, template, sessionID string) []byte {
	t.Helper()

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions/%s", sutHostURL, pathPrefix, template, sessionID)
	resp, respBody := doHTTP(t, http.MethodGet, reqURL, sutEnvName, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET session status=%d, body=%s", resp.StatusCode, respBody)
	}
	return respBody
}

// getSessionWithStatus sends a GET request for a session and returns the HTTP
// status code and response body. Does NOT fatal on non-200 responses.
func getSessionWithStatus(t *testing.T, sutHostURL, sutEnvName, template, sessionID string) (int, []byte) {
	t.Helper()

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions/%s", sutHostURL, pathPrefix, template, sessionID)
	resp, respBody := doHTTP(t, http.MethodGet, reqURL, sutEnvName, nil)

	return resp.StatusCode, respBody
}

// deleteSession sends a DELETE request for a session. Does NOT fatal on
// non-200 responses.
func deleteSession(t *testing.T, sutHostURL, sutEnvName, template, sessionID string) *http.Response {
	t.Helper()

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions/%s", sutHostURL, pathPrefix, template, sessionID)
	resp, _ := doHTTP(t, http.MethodDelete, reqURL, sutEnvName, nil)

	return resp
}

// ─── TeamProfile Helpers (proto-based) ──────────────────────────────────────

// createTeamProfile creates a saolei TeamProfile via HTTP POST to
// /api/v1/templates/{template}/profiles (AIP-133; game.proto
// PromptService.CreateTeamProfile, spec 031-team-template-mode
// contracts/api-contract.md §2.3). Per the grpc-gateway body binding
// ("body: team_profile"), the HTTP body is the TeamProfile JSON while parent
// comes from the URI path and team_profile_id from the query string. Calls
// t.Fatal on any error.
func createTeamProfile(t *testing.T, sutHostURL, sutEnvName, template, profileID, playerModel, plannerModel string) *game.TeamProfile {
	t.Helper()

	profile := &game.TeamProfile{
		Spec: &game.TeamProfile_Saolei{Saolei: &game.SaoleiProfile{
			PlayerModel:  playerModel,
			PlannerModel: plannerModel,
		}},
	}
	body, err := protojson.Marshal(profile)
	if err != nil {
		t.Fatalf("protojson.Marshal TeamProfile: %v", err)
	}

	reqURL := fmt.Sprintf("%s%stemplates/%s/profiles?team_profile_id=%s",
		sutHostURL, pathPrefix, template, profileID)
	resp, respBody := doHTTP(t, http.MethodPost, reqURL, sutEnvName, body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST createTeamProfile status=%d, body=%s", resp.StatusCode, respBody)
	}

	created := new(game.TeamProfile)
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(respBody, created); err != nil {
		t.Fatalf("Unmarshal TeamProfile: %v (raw: %s)", err, string(respBody))
	}
	return created
}

// getTeamProfile retrieves a TeamProfile by name via HTTP GET. Calls t.Fatal
// on non-200 responses.
func getTeamProfile(t *testing.T, sutHostURL, sutEnvName, template, profileName string) *game.TeamProfile {
	t.Helper()

	reqURL := fmt.Sprintf("%s%stemplates/%s/profiles/%s", sutHostURL, pathPrefix, template, profileName)
	resp, respBody := doHTTP(t, http.MethodGet, reqURL, sutEnvName, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET teamProfile status=%d, body=%s", resp.StatusCode, respBody)
	}

	profile := new(game.TeamProfile)
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(respBody, profile); err != nil {
		t.Fatalf("Unmarshal TeamProfile: %v (raw: %s)", err, string(respBody))
	}
	return profile
}

// getTeamProfileWithStatus sends a GET request for a TeamProfile and returns
// the HTTP status code and response body. Does NOT fatal on non-200 responses.
func getTeamProfileWithStatus(t *testing.T, sutHostURL, sutEnvName, template, profileName string) (int, []byte) {
	t.Helper()

	reqURL := fmt.Sprintf("%s%stemplates/%s/profiles/%s", sutHostURL, pathPrefix, template, profileName)
	resp, respBody := doHTTP(t, http.MethodGet, reqURL, sutEnvName, nil)

	return resp.StatusCode, respBody
}

// listTeamProfiles lists TeamProfiles of a template via HTTP GET. Calls
// t.Fatal on non-200 responses.
func listTeamProfiles(t *testing.T, sutHostURL, sutEnvName, template string, pageSize int) *game.ListTeamProfilesResponse {
	t.Helper()

	reqURL := fmt.Sprintf("%s%stemplates/%s/profiles?page_size=%d", sutHostURL, pathPrefix, template, pageSize)
	resp, respBody := doHTTP(t, http.MethodGet, reqURL, sutEnvName, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET listTeamProfiles status=%d, body=%s", resp.StatusCode, respBody)
	}

	ltp := new(game.ListTeamProfilesResponse)
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(respBody, ltp); err != nil {
		t.Fatalf("Unmarshal ListTeamProfilesResponse: %v (raw: %s)", err, string(respBody))
	}
	return ltp
}

// updateTeamProfile sends an HTTP PATCH to update a TeamProfile field via
// UpdateTeamProfile (AIP-134). updateMask is a single writable path
// ("saolei.player_model" / "saolei.planner_model", AIP-161 — game.proto
// UpdateTeamProfileRequest). The body is the TeamProfile JSON whose
// saolei variant carries only the field being patched; the resource name is
// surfaced via {team_profile.name=...} in the URI path. Returns the HTTP
// status code and response body.
func updateTeamProfile(t *testing.T, sutHostURL, sutEnvName, template, profileName, updateMask, playerModel, plannerModel string) (int, []byte) {
	t.Helper()

	patch := &game.TeamProfile{
		Name: game.TeamProfileName{TemplateID: template, ProfileID: profileName}.String(),
		Spec: &game.TeamProfile_Saolei{Saolei: &game.SaoleiProfile{
			PlayerModel:  playerModel,
			PlannerModel: plannerModel,
		}},
	}
	body, err := protojson.Marshal(patch)
	if err != nil {
		t.Fatalf("protojson.Marshal patch TeamProfile: %v", err)
	}

	reqURL := fmt.Sprintf("%s%stemplates/%s/profiles/%s?update_mask=%s",
		sutHostURL, pathPrefix, template, profileName, updateMask)
	resp, respBody := doHTTP(t, http.MethodPatch, reqURL, sutEnvName, body)
	return resp.StatusCode, respBody
}

// deleteTeamProfile deletes a TeamProfile by name via HTTP DELETE.
// Returns the HTTP status code.
func deleteTeamProfile(t *testing.T, sutHostURL, sutEnvName, template, profileName string) int {
	t.Helper()

	reqURL := fmt.Sprintf("%s%stemplates/%s/profiles/%s", sutHostURL, pathPrefix, template, profileName)
	resp, _ := doHTTP(t, http.MethodDelete, reqURL, sutEnvName, nil)

	return resp.StatusCode
}

// ─── Team Helpers (proto-based) ─────────────────────────────────────────────

// createTeam creates the per-session singleton Team via HTTP POST to
// /api/v1/templates/{template}/sessions/{sessionID}/team (AIP-133; game.proto
// TeamService.CreateTeam, spec 031-team-template-mode contracts/api-contract.md
// §2.2). The body carries the parent Session resource name and the TeamProfile
// full resource name ("templates/{template}/profiles/{profile}"); the server
// validates the profile's template segment against the parent. CreateTeam is
// the ONLY Team creation point — GetTeam/Connect/ListMessages/RefreshTeam
// require it first (no lazy creation, FR-033). Calls t.Fatal on non-200
// responses.
func createTeam(t *testing.T, sutHostURL, sutEnvName, template, sessionID, profileName string) *game.Team {
	t.Helper()

	reqBody := &game.CreateTeamRequest{
		Parent:  game.SessionName{TemplateID: template, SessionID: sessionID}.String(),
		Profile: game.TeamProfileName{TemplateID: template, ProfileID: profileName}.String(),
	}
	body, err := protojson.Marshal(reqBody)
	if err != nil {
		t.Fatalf("protojson.Marshal CreateTeamRequest: %v", err)
	}

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions/%s/team", sutHostURL, pathPrefix, template, sessionID)
	resp, respBody := doHTTP(t, http.MethodPost, reqURL, sutEnvName, body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST createTeam status=%d, body=%s", resp.StatusCode, respBody)
	}

	team := new(game.Team)
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(respBody, team); err != nil {
		t.Fatalf("Unmarshal Team: %v (raw: %s)", err, string(respBody))
	}
	return team
}

// createTeamWithStatus sends a CreateTeam request and returns the HTTP status
// code and response body. Does NOT fatal on non-200 responses — used to
// assert the FR-033 idempotency/ALREADY_EXISTS re-entry contract.
func createTeamWithStatus(t *testing.T, sutHostURL, sutEnvName, template, sessionID, profileName string) (int, []byte) {
	t.Helper()

	reqBody := &game.CreateTeamRequest{
		Parent:  game.SessionName{TemplateID: template, SessionID: sessionID}.String(),
		Profile: game.TeamProfileName{TemplateID: template, ProfileID: profileName}.String(),
	}
	body, err := protojson.Marshal(reqBody)
	if err != nil {
		t.Fatalf("protojson.Marshal CreateTeamRequest: %v", err)
	}

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions/%s/team", sutHostURL, pathPrefix, template, sessionID)
	resp, respBody := doHTTP(t, http.MethodPost, reqURL, sutEnvName, body)
	return resp.StatusCode, respBody
}

// getTeam retrieves the Team of a session via HTTP GET. Calls t.Fatal on
// non-200 responses.
func getTeam(t *testing.T, sutHostURL, sutEnvName, template, sessionID string) *game.Team {
	t.Helper()

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions/%s/team", sutHostURL, pathPrefix, template, sessionID)
	resp, respBody := doHTTP(t, http.MethodGet, reqURL, sutEnvName, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET team status=%d, body=%s", resp.StatusCode, respBody)
	}

	team := new(game.Team)
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(respBody, team); err != nil {
		t.Fatalf("Unmarshal Team: %v (raw: %s)", err, string(respBody))
	}
	return team
}

// getTeamWithStatus sends a GET request for the Team and returns the HTTP
// status code and response body. Does NOT fatal on non-200 responses — used
// to assert the not-created NOT_FOUND contract (FR-033).
func getTeamWithStatus(t *testing.T, sutHostURL, sutEnvName, template, sessionID string) (int, []byte) {
	t.Helper()

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions/%s/team", sutHostURL, pathPrefix, template, sessionID)
	resp, respBody := doHTTP(t, http.MethodGet, reqURL, sutEnvName, nil)

	return resp.StatusCode, respBody
}

// refreshTeam triggers RefreshTeam for a session's team via HTTP POST to
// /api/v1/templates/{template}/sessions/{sessionID}/team:refresh (AIP-136
// custom method; game.proto TeamService.RefreshTeam, FR-018). The body is
// "{}" — the `name` field is captured from the URI path by grpc-gateway.
// After refresh the session's short-term message channels are cleared; the
// long-term strategy is unaffected. Calls t.Fatal on non-2xx responses.
func refreshTeam(t *testing.T, sutHostURL, sutEnvName, template, sessionID string) {
	t.Helper()

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions/%s/team:refresh", sutHostURL, pathPrefix, template, sessionID)
	resp, respBody := doHTTP(t, http.MethodPost, reqURL, sutEnvName, []byte("{}"))

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		t.Fatalf("POST refreshTeam status=%d, body=%s", resp.StatusCode, respBody)
	}
}

// setupTeamSession creates the full team stack for one test: Session →
// saolei TeamProfile → CreateTeam. Returns the session ID. The caller then
// connects the WebSocket via connectAgentWS (CreateTeam MUST precede
// Connect — no lazy creation, FR-033).
func setupTeamSession(t *testing.T, sutHostURL, sutEnvName, template, profileID, playerModel, plannerModel string) string {
	t.Helper()

	createTeamProfile(t, sutHostURL, sutEnvName, template, profileID, playerModel, plannerModel)
	sessionID, _ := createSession(t, sutHostURL, sutEnvName, template)
	createTeam(t, sutHostURL, sutEnvName, template, sessionID, profileID)
	return sessionID
}

// ─── Message Helpers (proto-based) ────────────────────────────────────────

// listMessages sends a GET request to list messages of one team agent's
// partition and returns the parsed ListMessagesResponse. The parent is
// "templates/{template}/sessions/{session}/team/agents/{agent}" (AIP-132;
// game.proto TeamService.ListMessages, FR-005 — messages are partitioned per
// team agent). Calls t.Fatal on non-200 responses.
func listMessages(t *testing.T, sutHostURL, sutEnvName, template, sessionID, agent string) *game.ListMessagesResponse {
	t.Helper()

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions/%s/team/agents/%s/messages",
		sutHostURL, pathPrefix, template, sessionID, agent)
	resp, respBody := doHTTP(t, http.MethodGet, reqURL, sutEnvName, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET listMessages status=%d, body=%s", resp.StatusCode, respBody)
	}

	lmr := new(game.ListMessagesResponse)
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := opts.Unmarshal(respBody, lmr); err != nil {
		t.Fatalf("Unmarshal ListMessagesResponse: %v (raw: %s)", err, string(respBody))
	}
	return lmr
}

// listMessagesWithStatus sends a GET request for one agent's message
// partition and returns the HTTP status code and response body. Does NOT
// fatal on non-200 responses — used to assert the not-created NOT_FOUND
// contract (FR-033).
func listMessagesWithStatus(t *testing.T, sutHostURL, sutEnvName, template, sessionID, agent string) (int, []byte) {
	t.Helper()

	reqURL := fmt.Sprintf("%s%stemplates/%s/sessions/%s/team/agents/%s/messages",
		sutHostURL, pathPrefix, template, sessionID, agent)
	resp, respBody := doHTTP(t, http.MethodGet, reqURL, sutEnvName, nil)

	return resp.StatusCode, respBody
}

// ─── WebSocket Helpers (proto-based) ────────────────────────────────────────

// connectAgentWS connects to the session WebSocket endpoint
// /api/v1/templates/{template}/sessions/{session}/connect (spec
// 031-team-template-mode FR-004 — the WS endpoint mirrors the Team resource
// hierarchy) and returns the connection. The caller MUST have created the
// team via CreateTeam first: Connect requires an existing team (no lazy
// creation, FR-033); a frame sent on a not-created session closes the
// connection. Calls t.Fatal on any error.
func connectAgentWS(t *testing.T, sutHostURL, sutEnvName, template, sessionID string) *websocket.Conn {
	t.Helper()

	wsPath := fmt.Sprintf("/api/v1/templates/%s/sessions/%s/connect", template, sessionID)
	wsURL := buildWSURL(sutHostURL, wsPath)

	header := http.Header{}
	header.Set(headerEnv, sutEnvName)

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		t.Fatalf("WS upgrade status = %d, want %d", resp.StatusCode, http.StatusSwitchingProtocols)
	}
	return conn
}

// writeWSFrame marshals a proto frame to binary protobuf and writes it over
// the WebSocket connection. The test writes USER frames (the desktop→server
// direction: user messages, operation results, connectivity probes — the
// frame split of specs/035-proto-contract-refine/contracts/frame-split.md
// §2). Calls t.Fatal on marshal or write errors.
//
// spec 025 FR-011 / image-transport-contract.md §2: the desktop↔gateway WS
// leg now carries binary protobuf frames (was protojson text). The large
// tests connect through the gateway and MUST speak the same wire format,
// otherwise the gateway's `proto.Unmarshal` on a text frame fails and the
// connection is closed. `proto.Marshal` is also the compact representation
// required by FR-008 (no base64 inflation of image bytes).
func writeWSFrame(t *testing.T, conn *websocket.Conn, frame *game.UserFrame) {
	t.Helper()

	data, err := proto.Marshal(frame)
	if err != nil {
		t.Fatalf("proto.Marshal frame: %v", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
}

// readWSFrame reads a single WebSocket message and unmarshals it into a
// TeamFrame — the server→desktop direction (agent display content, control
// signals, operation requests; specs/035-proto-contract-refine/contracts/
// frame-split.md §3). Calls t.Fatal on timeout or parse error.
//
// The WS leg is binary protobuf (see writeWSFrame doc); `proto.Unmarshal`
// preserves unknown fields per the proto spec, matching the forward-
// compatibility behaviour that `protojson.Unmarshal{DiscardUnknown:true}`
// provided before spec 025.
func readWSFrame(t *testing.T, conn *websocket.Conn) *game.TeamFrame {
	t.Helper()

	conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	frame := new(game.TeamFrame)
	if err := proto.Unmarshal(data, frame); err != nil {
		t.Fatalf("Unmarshal TeamFrame: %v (raw len=%d)", err, len(data))
	}
	return frame
}

// readWSFrameNoFatal reads a single WebSocket message with a bounded
// deadline and returns it unmarshalled as a TeamFrame, or an error when
// the read times out or the peer closed the connection. Unlike readWSFrame
// it does NOT call t.Fatal — used to assert negative WS outcomes (a session
// whose team was not created closes the connection; a superseded connection
// receives no turn output).
func readWSFrameNoFatal(conn *websocket.Conn, timeout time.Duration) (*game.TeamFrame, error) {
	conn.SetReadDeadline(time.Now().Add(timeout))
	_, data, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	frame := new(game.TeamFrame)
	if err := proto.Unmarshal(data, frame); err != nil {
		return nil, err
	}
	return frame, nil
}

// buildTextFrame constructs a UserFrame whose message_parts payload carries
// a single TextPart (specs/023-saolei-mcp-refine/contracts/content-model-contract.md
// §3/§4 — display channel). It sets the session ID and the target team agent
// (UserFrame.agent, spec 031-team-template-mode FR-023). UserFrame has no
// sender field — the inbound direction is inherently the user
// (specs/035-proto-contract-refine/contracts/frame-split.md §2).
func buildTextFrame(sessionID, agent, content string) *game.UserFrame {
	return &game.UserFrame{
		SessionId: sessionID,
		Agent:     agent,
		Payload: &game.UserFrame_MessageParts{
			MessageParts: &game.MessageParts{Parts: []*game.MessagePart{
				{Kind: &game.MessagePart_Text{Text: &game.TextPart{Content: content}}},
			}},
		},
	}
}

// sendText builds a user-text frame targeting the player agent (the only
// accepts-user-input agent, FR-031/FR-032) and sends it over the WebSocket
// connection. Calls t.Fatal on write errors.
func sendText(t *testing.T, conn *websocket.Conn, sessionID, text string) {
	t.Helper()
	frame := buildTextFrame(sessionID, "player", text)
	writeWSFrame(t, conn, frame)
}

// sendStatusFrame writes a flow_parts UserFrame over the WebSocket carrying a
// single StatusSignal FlowPart (specs/023-saolei-mcp-refine/contracts/content-model-contract.md
// §2 — status became a FlowPart kind per spec 023 C3 / FR-003). The desktop
// sends this on session (re-)entry to probe the agent's working state; the
// agent responds with a derived StatusSignal (ACTIVE/IDLE/UNSPECIFIED)
// (specs/021-agent-session-resync/contracts/agent-desktop-channel-contract.md §1).
func sendStatusFrame(t *testing.T, conn *websocket.Conn, sessionID string, status game.StatusSignalStatus) {
	t.Helper()
	frame := &game.UserFrame{
		SessionId: sessionID,
		Payload: &game.UserFrame_FlowParts{
			FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
				{Kind: &game.FlowPart_Status{Status: &game.StatusSignal{Status: status}}},
			}},
		},
	}
	writeWSFrame(t, conn, frame)
}

// buildImageFrame constructs a minimal ImagePart carrying a 1×1 PNG
// (smallScreenshotData) plus the metadata required by the proto. The returned
// part is embedded in a user-turn MessageParts by buildUserTurnFrame.
func buildImageFrame(sessionID string) *game.ImagePart {
	return &game.ImagePart{
		Encoding:    game.ImageEncoding_IMAGE_ENCODING_PNG,
		Data:        smallScreenshotData,
		WidthPx:     1,
		HeightPx:    1,
		ScaleFactor: 1.0,
		WindowTitle: "Test Window",
	}
}

// buildLargeImageFrame constructs an ImagePart whose encoded PNG size exceeds
// the pre-025 `coder/websocket` default ReadLimit of 32 KiB
// (largeScreenshotData). Used by the large-image round-trip test to prove the
// binary-protobuf WS path (spec 025 FR-007/FR-008/FR-010) delivers an
// oversized frame intact (would have torn the session down under the old
// protojson-text + 32 KiB regime).
func buildLargeImageFrame(sessionID string) *game.ImagePart {
	return &game.ImagePart{
		Encoding:    game.ImageEncoding_IMAGE_ENCODING_PNG,
		Data:        largeScreenshotData,
		WidthPx:     128,
		HeightPx:    128,
		ScaleFactor: 1.0,
		WindowTitle: "Large Test Window",
	}
}

// buildSaoleiFlowResultScreenshot constructs an ImagePart carrying a real
// Minesweeper screenshot (saoleiBoardInitPNG or saoleiBoardRevealedPNG) — the
// data the test "playing the desktop" attaches to a FlowResultPart so the
// agent's real @dominion/game-saolei-board recognition engine can decode the
// board (spec 025 FR-012/FR-013). Dimensions/ScaleFactor mirror a full-window
// capture; only `data` feeds recognition (origin 24/200, cell 32×32px per the
// saolei-board README).
func buildSaoleiFlowResultScreenshot(pngData []byte) *game.ImagePart {
	return &game.ImagePart{
		Encoding:    game.ImageEncoding_IMAGE_ENCODING_PNG,
		Data:        pngData,
		ScaleFactor: 1.0,
		WindowTitle: "Minesweeper",
	}
}

// buildUserTurnFrame constructs a UserFrame whose message_parts payload
// carries [TextPart, (optional) ImagePart], targeted at the player agent.
// Pass a nil image for a text-only user turn
// (specs/023-saolei-mcp-refine/contracts/content-model-contract.md §3).
func buildUserTurnFrame(sessionID, text string, image *game.ImagePart) *game.UserFrame {
	parts := []*game.MessagePart{
		{Kind: &game.MessagePart_Text{Text: &game.TextPart{Content: text}}},
	}
	if image != nil {
		parts = append(parts, &game.MessagePart{Kind: &game.MessagePart_Image{Image: image}})
	}
	return &game.UserFrame{
		SessionId: sessionID,
		Agent:     "player",
		Payload: &game.UserFrame_MessageParts{
			MessageParts: &game.MessageParts{Parts: parts},
		},
	}
}

// sendUserTurn builds and writes a message_parts user-turn frame over the
// WebSocket. Pass a nil image for a text-only turn.
func sendUserTurn(t *testing.T, conn *websocket.Conn, sessionID, text string, image *game.ImagePart) {
	t.Helper()
	writeWSFrame(t, conn, buildUserTurnFrame(sessionID, text, image))
}

// buildOperationResultFrame constructs a UserFrame whose flow_parts payload
// carries a single FlowResultPart. Used to simulate a desktop-executed tool
// operation result delivered back to the agent on the CONTROL channel
// (spec 025 FR-023/FR-024 — the desktop reports operation outcomes as a
// flow_result FlowPart, NOT a display tool_result MessagePart; the agent's
// handler.ts routes flowParts/flow_result to OperationBridge.handleResult,
// resolving the pending dispatch). tool_id matches the bridge-minted
// operation-channel id stamped on the originating FlowPart.
//
// This variant carries no screenshot; use buildFlowResultFrame for the
// screenshot-bearing variant (saolei tests).
func buildOperationResultFrame(sessionID, toolID string, status game.ToolResultStatus, message string) *game.UserFrame {
	return buildFlowResultFrame(sessionID, toolID, status, message, nil)
}

// buildFlowResultFrame is the screenshot-bearing variant of
// buildOperationResultFrame. The screenshot is attached to the FlowResultPart
// (spec 025 FR-026 — control-channel carrier for the post-action screenshot);
// pass nil to omit it. Saolei tests use this so the agent's recognition engine
// (@dominion/game-saolei-board) can consume the screenshot.
func buildFlowResultFrame(sessionID, toolID string, status game.ToolResultStatus, message string, screenshot *game.ImagePart) *game.UserFrame {
	part := &game.FlowResultPart{
		ToolId:  toolID,
		Status:  status,
		Message: message,
	}
	if screenshot != nil {
		part.Screenshot = screenshot
	}
	return &game.UserFrame{
		SessionId: sessionID,
		Payload: &game.UserFrame_FlowParts{
			FlowParts: &game.FlowParts{Parts: []*game.FlowPart{
				{Kind: &game.FlowPart_FlowResult{FlowResult: part}},
			}},
		},
	}
}

// ─── Content-projection helpers ─────────────────────────────────────────────
//
// The content-model split (specs/023-saolei-mcp-refine/contracts/content-model-contract.md)
// carries display blocks in TeamFrame.message_parts / Message.content
// (MessageParts) and control blocks in TeamFrame.flow_parts (FlowParts).
// These helpers project a MessagePart/FlowPart variant out of a frame or
// Message the way the old frame.GetThinking() / frame.GetText() /
// frame.GetWarn() accessors did before the split. Frames here are TeamFrames
// (the server→desktop direction; specs/035-proto-contract-refine/contracts/
// frame-split.md §3).

// frameMessageParts returns the MessageParts payload of a frame, or nil when
// the frame carries no display channel (e.g. it is a flow_parts frame).
func frameMessageParts(f *game.TeamFrame) *game.MessageParts {
	if f == nil {
		return nil
	}
	return f.GetMessageParts()
}

// frameFlowParts returns the FlowParts payload of a frame, or nil when the
// frame carries no control channel (e.g. it is a message_parts frame).
func frameFlowParts(f *game.TeamFrame) *game.FlowParts {
	if f == nil {
		return nil
	}
	return f.GetFlowParts()
}

// frameHasThinking reports whether a message_parts frame carries a
// ThinkingPart.
func frameHasThinking(f *game.TeamFrame) bool {
	mp := frameMessageParts(f)
	if mp == nil {
		return false
	}
	for _, p := range mp.GetParts() {
		if p.GetThinking() != nil {
			return true
		}
	}
	return false
}

// frameHasText reports whether a message_parts frame carries a TextPart.
func frameHasText(f *game.TeamFrame) bool {
	mp := frameMessageParts(f)
	if mp == nil {
		return false
	}
	for _, p := range mp.GetParts() {
		if p.GetText() != nil {
			return true
		}
	}
	return false
}

// frameThinking returns the content of the first ThinkingPart in a
// message_parts frame, or "" if the frame has no thinking part.
func frameThinking(f *game.TeamFrame) string {
	mp := frameMessageParts(f)
	if mp == nil {
		return ""
	}
	for _, p := range mp.GetParts() {
		if t := p.GetThinking(); t != nil {
			return t.GetContent()
		}
	}
	return ""
}

// frameText returns the content of the first TextPart in a message_parts
// frame, or "" if the frame has no text part.
func frameText(f *game.TeamFrame) string {
	mp := frameMessageParts(f)
	if mp == nil {
		return ""
	}
	for _, p := range mp.GetParts() {
		if t := p.GetText(); t != nil {
			return t.GetContent()
		}
	}
	return ""
}

// frameWarn returns the WarnSignal in a flow_parts frame, or nil when the
// frame carries no warn FlowPart. Replaces the removed TeamFrame.warn
// payload accessor — warn is now a FlowPart kind (spec 023 C3 / FR-003).
func frameWarn(f *game.TeamFrame) *game.WarnSignal {
	fp := frameFlowParts(f)
	if fp == nil {
		return nil
	}
	for _, p := range fp.GetParts() {
		if w := p.GetWarn(); w != nil {
			return w
		}
	}
	return nil
}

// frameWait returns the WaitSignal in a flow_parts frame, or nil when the
// frame carries no wait FlowPart. Replaces the removed TeamFrame.wait
// payload accessor — wait is now a FlowPart kind (spec 023 C3 / FR-003).
// Tests drain for a wait frame to detect turn completion (the agent emits
// a wait FlowPart when its turn ends).
func frameWait(f *game.TeamFrame) *game.WaitSignal {
	fp := frameFlowParts(f)
	if fp == nil {
		return nil
	}
	for _, p := range fp.GetParts() {
		if w := p.GetWait(); w != nil {
			return w
		}
	}
	return nil
}

// frameStatus returns the StatusSignal in a flow_parts frame, or nil when
// the frame carries no status FlowPart. Replaces the removed TeamFrame.status
// payload accessor — status is now a FlowPart kind (spec 023 C3 / FR-003).
// Used by the session-agent lifecycle suite to assert IDLE/ACTIVE probes.
func frameStatus(f *game.TeamFrame) *game.StatusSignal {
	fp := frameFlowParts(f)
	if fp == nil {
		return nil
	}
	for _, p := range fp.GetParts() {
		if s := p.GetStatus(); s != nil {
			return s
		}
	}
	return nil
}

// frameQueueSignal returns the QueueSignal in a flow_parts frame, or nil when
// the frame carries no queue FlowPart. The agent pushes a QueueSignal whenever
// the per-session queue depth changes (submit⇒new depth, drain⇒0, abort⇒0)
// per specs/030-queued-chat-input/contracts/queue-channel-contract.md §2.
// Used by the agent-dialog queue-while-running suite to assert depth changes.
func frameQueueSignal(f *game.TeamFrame) *game.QueueSignal {
	fp := frameFlowParts(f)
	if fp == nil {
		return nil
	}
	for _, p := range fp.GetParts() {
		if q := p.GetQueue(); q != nil {
			return q
		}
	}
	return nil
}

// frameHasQueueSignal reports whether a flow_parts frame carries a QueueSignal
// FlowPart. Used as a drainWSFrame predicate.
func frameHasQueueSignal(f *game.TeamFrame) bool {
	return frameQueueSignal(f) != nil
}

// drainUntilWait reads frames until a wait FlowPart is observed (turn/loop
// idle) or the read limit is exhausted, returning ALL frames read in order.
// The wait FlowPart marks the end of a turn or the full drain of the
// per-session TurnLoop (specs/030-queued-chat-input/contracts/turn-loop-contract.md).
// Used by the queue-while-running suite to collect the full frame stream for
// depth-sequence and turn-count analysis.
func drainUntilWait(t *testing.T, conn *websocket.Conn) []*game.TeamFrame {
	t.Helper()
	var frames []*game.TeamFrame
	for i := 0; i < 60; i++ {
		frame := readWSFrame(t, conn)
		frames = append(frames, frame)
		if frameWait(frame) != nil {
			return frames
		}
	}
	return frames
}

// queueSignalDepths extracts the ordered sequence of QueueSignal.queued_count
// values from a slice of frames (specs/030-queued-chat-input/contracts/
// queue-channel-contract.md §2). Used to assert the depth-change emission
// rules: submit⇒+1/new depth, drain-to-next-turn⇒0.
func queueSignalDepths(frames []*game.TeamFrame) []int32 {
	var depths []int32
	for _, f := range frames {
		if q := frameQueueSignal(f); q != nil {
			depths = append(depths, q.GetQueuedCount())
		}
	}
	return depths
}

// countWaitFrames counts how many frames in the slice carry a wait FlowPart.
// Each wait marks a turn/loop boundary; the queue-while-running suite asserts
// exactly ONE terminal wait when messages are queued and auto-handed-off
// (specs/030-queued-chat-input/spec.md FR-006).
func countWaitFrames(frames []*game.TeamFrame) int {
	count := 0
	for _, f := range frames {
		if frameWait(f) != nil {
			count++
		}
	}
	return count
}

// collectTextContents returns the text content of every agent-sent text
// MessagePart frame in the slice, in order. The role filter keeps only
// agent-role frames (TeamFrame.role; specs/035-proto-contract-refine/
// contracts/frame-split.md §3.2). Used to verify which queued messages
// produced turns and in what order.
func collectTextContents(frames []*game.TeamFrame) []string {
	var texts []string
	for _, f := range frames {
		if f.GetRole() != game.MessageRole_MESSAGE_ROLE_AGENT {
			continue
		}
		if !frameHasText(f) {
			continue
		}
		texts = append(texts, frameText(f))
	}
	return texts
}

// messageKind returns the MessagePart-kind string of the first part in a
// Message's content MessageParts ("text", "thinking", "image", "toolCall",
// "toolResult"), or "" if the message has no content. Only MessagePart kinds
// appear here — Message.content is typed as MessageParts so a FlowPart can
// never appear (spec 023 FR-004).
func messageKind(m *game.Message) string {
	if m.GetContent() == nil || len(m.GetContent().GetParts()) == 0 {
		return ""
	}
	p := m.GetContent().GetParts()[0]
	switch {
	case p.GetText() != nil:
		return "text"
	case p.GetThinking() != nil:
		return "thinking"
	case p.GetImage() != nil:
		return "image"
	case p.GetToolCall() != nil:
		return "toolCall"
	case p.GetToolResult() != nil:
		return "toolResult"
	}
	return ""
}

// messageText returns the content of the first TextPart in a Message's
// content MessageParts, or "" if none.
func messageText(m *game.Message) string {
	if m.GetContent() == nil {
		return ""
	}
	for _, p := range m.GetContent().GetParts() {
		if t := p.GetText(); t != nil {
			return t.GetContent()
		}
	}
	return ""
}

// messagePartKind returns the active variant name of a MessagePart
// ("text"/"thinking"/"image"/"toolCall"/"toolResult"), or "" when no variant
// is set. Used to assert no control-only FlowPart kind leaks into
// Message.content (which is typed MessageParts so the proto layer already
// forbids FlowParts structurally, but the test asserts the rendered kinds so
// a future regression that reintroduces an operation-shaped MessagePart is
// caught — spec 023 FR-005).
func messagePartKind(p *game.MessagePart) string {
	if p == nil {
		return ""
	}
	switch {
	case p.GetText() != nil:
		return "text"
	case p.GetThinking() != nil:
		return "thinking"
	case p.GetImage() != nil:
		return "image"
	case p.GetToolCall() != nil:
		return "toolCall"
	case p.GetToolResult() != nil:
		return "toolResult"
	}
	return ""
}

// isDisplayOnlyMessagePartKind reports whether kind is one of the display-only
// MessagePart variants. Any other value (including the empty string or a
// FlowPart kind that should never appear here) is a leak — used with
// messagePartKind to guard spec 023 FR-005 (operations must not appear in
// Message.content).
func isDisplayOnlyMessagePartKind(kind string) bool {
	switch kind {
	case "text", "thinking", "image", "toolCall", "toolResult":
		return true
	}
	return false
}

// messagesContainToolCall reports whether any Message's content MessageParts
// carries a ToolCallPart whose name matches.
func messagesContainToolCall(messages []*game.Message, name string) bool {
	for _, m := range messages {
		if messageHasToolCall(m, name) {
			return true
		}
	}
	return false
}

// messagesContainText reports whether any Message's content MessageParts
// carries a TextPart whose content contains the substring. Used as a sanity
// check that user/agent text survived history reconstruction.
func messagesContainText(messages []*game.Message, substring string) bool {
	for _, m := range messages {
		if strings.Contains(messageText(m), substring) {
			return true
		}
	}
	return false
}

// messagesContainToolResultStatus reports whether any Message's content
// MessageParts carries a ToolResultPart whose status matches. Used to assert
// the real status survives a leave/re-enter cycle for native tools
// (research.md D4; data-model.md §6).
func messagesContainToolResultStatus(messages []*game.Message, status game.ToolResultStatus) bool {
	for _, m := range messages {
		for _, s := range messageToolResultStatuses(m) {
			if s == status {
				return true
			}
		}
	}
	return false
}

// firstToolCallArgsJSON returns the args_json of the first ToolCallPart with
// the given tool name across the messages, or "" if none.
func firstToolCallArgsJSON(messages []*game.Message, name string) string {
	for _, m := range messages {
		if args := messageToolCallArgsJSON(m, name); args != "" {
			return args
		}
	}
	return ""
}

// assertMessageContentDisplayOnly fails the test if any Message's content
// carries a non-display-only MessagePart kind (i.e. an operation FlowPart
// leaked into history). spec 023 FR-004/FR-005 — Message.content is typed
// MessageParts, so this guard catches a future regression that reintroduces
// an operation-shaped entry.
func assertMessageContentDisplayOnly(t *testing.T, messages []*game.Message) {
	t.Helper()
	for i, m := range messages {
		if m.GetContent() == nil {
			continue
		}
		for j, p := range m.GetContent().GetParts() {
			kind := messagePartKind(p)
			if !isDisplayOnlyMessagePartKind(kind) {
				t.Errorf("message[%d].parts[%d]: kind = %q is not a display-only MessagePart kind (spec 023 FR-005 — operations must not appear in Message.content)", i, j, kind)
			}
		}
	}
}

// drainWSFrame reads and discards frames until a frame matches the predicate,
// or all frames from the timeout are exhausted. Returns the first matching
// TeamFrame, or nil if none found. Reads up to 20 frames.
func drainWSFrame(t *testing.T, conn *websocket.Conn, match func(*game.TeamFrame) bool) *game.TeamFrame {
	t.Helper()

	for i := 0; i < 20; i++ {
		frame := readWSFrame(t, conn)
		if match(frame) {
			return frame
		}
	}
	return nil
}

// roleString returns a human-readable name for a MessageRole value (for test
// diagnostics).
func roleString(role game.MessageRole) string {
	switch role {
	case game.MessageRole_MESSAGE_ROLE_USER:
		return "USER"
	case game.MessageRole_MESSAGE_ROLE_AGENT:
		return "AGENT"
	default:
		return "UNSPECIFIED"
	}
}

// ─── Operation-dispatch helpers ─────────────────────────────────────────────
//
// When the model emits a tool_call, the agent executes the tool, which calls
// OperationBridge.dispatch. The bridge wraps the operation FlowPart
// (MouseMovePart, MouseClickPart, KeyboardPressPart, or MouseMoveAndClickPart)
// in a flow_parts TeamFrame and writes it to the session WebSocket sink
// (specs/023-saolei-mcp-refine/contracts/content-model-contract.md §2;
// research.md D10 — the FlowPart carries a bridge-minted operation-channel
// tool_id, decoupled from the conversation tool_call.id). A large test that
// "plays the desktop" reads that operation frame, echoes the stamped tool_id
// back in a ToolResultPart, and the bridge resolves the pending dispatch.
// The helpers below project the operation Part out of a flow_parts frame and
// reply with a matching ToolResultPart — they are shared by the
// agent_operation and agent_saolei suites so neither copies the logic
// (style/large_test.md §反模式3).

// frameOperationToolID returns the tool_id stamped on the first tool-operation
// FlowPart in a flow_parts frame (keyboard_press / mouse_move_and_click /
// mouse_move / mouse_click), or "" when the frame carries no operation Part.
// tool_id is the bridge-minted operation-channel id the bridge matches against
// the ToolResultPart (projects/game/agent/src/operation-bridge.ts
// dispatch/handleResult; research.md D10).
func frameOperationToolID(f *game.TeamFrame) string {
	fp := frameFlowParts(f)
	if fp == nil {
		return ""
	}
	for _, p := range fp.GetParts() {
		if kp := p.GetKeyboardPress(); kp != nil {
			return kp.GetToolId()
		}
		if mmc := p.GetMouseMoveAndClick(); mmc != nil {
			return mmc.GetToolId()
		}
		if mm := p.GetMouseMove(); mm != nil {
			return mm.GetToolId()
		}
		if mc := p.GetMouseClick(); mc != nil {
			return mc.GetToolId()
		}
	}
	return ""
}

// frameKeyboardPress returns the first KeyboardPressPart in a flow_parts
// frame, or nil. Used by the saolei suite to assert saolei_init dispatched an
// F2 key (specs/018-saolei-mcp/contracts/proto-operation-contract.md §2).
func frameKeyboardPress(f *game.TeamFrame) *game.KeyboardPressPart {
	fp := frameFlowParts(f)
	if fp == nil {
		return nil
	}
	for _, p := range fp.GetParts() {
		if kp := p.GetKeyboardPress(); kp != nil {
			return kp
		}
	}
	return nil
}

// frameMouseMoveAndClick returns the first MouseMoveAndClickPart in a
// flow_parts frame, or nil. Used by the saolei suite to assert a cell
// operation dispatched the correct window-message mouse Part
// (specs/018-saolei-mcp/contracts/proto-operation-contract.md §3).
func frameMouseMoveAndClick(f *game.TeamFrame) *game.MouseMoveAndClickPart {
	fp := frameFlowParts(f)
	if fp == nil {
		return nil
	}
	for _, p := range fp.GetParts() {
		if mmc := p.GetMouseMoveAndClick(); mmc != nil {
			return mmc
		}
	}
	return nil
}

// frameMouseMove returns the first MouseMovePart in a flow_parts frame, or
// nil. Used by the agent_operation suite to assert a mouse_move tool_call
// dispatched.
func frameMouseMove(f *game.TeamFrame) *game.MouseMovePart {
	fp := frameFlowParts(f)
	if fp == nil {
		return nil
	}
	for _, p := range fp.GetParts() {
		if mm := p.GetMouseMove(); mm != nil {
			return mm
		}
	}
	return nil
}

// readOperationFrame drains frames until it finds a flow_parts frame carrying
// a tool-operation Part, and returns it. Fails the test if no operation frame
// arrives within the drain limit — the model→tool_call→dispatch chain is the
// behaviour under test, so its absence is a real failure, not a timeout.
func readOperationFrame(t *testing.T, conn *websocket.Conn) *game.TeamFrame {
	t.Helper()
	f := drainWSFrame(t, conn, func(f *game.TeamFrame) bool {
		return frameOperationToolID(f) != ""
	})
	if f == nil {
		t.Fatal("did not receive an operation FlowPart frame from the agent " +
			"(model→tool_call→dispatch chain did not fire)")
	}
	return f
}

// readToolCallAndOperation drains WS frames until BOTH a tool_call MessagePart
// frame and an operation FlowPart frame have been observed, returning both.
//
// The agent emits the two frames CONCURRENTLY and their relative order is NOT
// guaranteed: OperationBridge.dispatch sink-writes the FlowPart envelope
// synchronously inside the tool fn (operation-bridge.ts dispatch →
// sink(envelope)), while the tool_call MessagePart frame is yielded
// asynchronously through LangGraph's stream.toolCalls transformer pipeline
// (llm.ts consumeToolCalls → mergeIterables → handler for-await). In practice
// the operation FlowPart usually arrives FIRST (synchronous sink vs. several
// microtask hops through the transformer), so a test that drains one frame
// kind and then the other can drop the earlier frame and time out.
//
// This helper reads frames in a single loop, collecting each kind into its
// slot without discarding the other, and returns once both are present. Used
// by the agent_operation and agent_checkpoint suites (style/large_test.md
// §反模式3 — shared helper, not copied).
func readToolCallAndOperation(t *testing.T, conn *websocket.Conn) (toolCallFrame, opFrame *game.TeamFrame) {
	t.Helper()
	for i := 0; i < 40; i++ {
		if toolCallFrame != nil && opFrame != nil {
			return toolCallFrame, opFrame
		}
		frame := readWSFrame(t, conn)
		if toolCallFrame == nil && frameHasToolCall(frame) {
			toolCallFrame = frame
		}
		if opFrame == nil && frameOperationToolID(frame) != "" {
			opFrame = frame
		}
	}
	if toolCallFrame == nil {
		t.Fatal("did not receive a tool_call MessagePart frame from the agent (FR-006)")
	}
	if opFrame == nil {
		t.Fatal("did not receive an operation FlowPart frame from the agent (model→tool_call→dispatch chain did not fire)")
	}
	return toolCallFrame, opFrame
}

// respondToOperation writes a FlowResultPart back over the WebSocket whose
// tool_id matches the operation frame's stamped bridge-minted id, simulating
// a desktop that executed the operation (spec 025 FR-023/FR-024 — control
// channel). The bridge's handleResult resolves the pending dispatch so the
// model's tool-call loop continues. No screenshot is attached (mouse-tool
// tests rely on the agent emitting its own display tool_result).
func respondToOperation(t *testing.T, conn *websocket.Conn, sessionID string, opFrame *game.TeamFrame, status game.ToolResultStatus, message string) {
	t.Helper()
	toolID := frameOperationToolID(opFrame)
	if toolID == "" {
		t.Fatalf("respondToOperation: operation frame has no tool_id")
	}
	writeWSFrame(t, conn, buildOperationResultFrame(sessionID, toolID, status, message))
}

// respondToOperationWithScreenshot is the screenshot-bearing variant of
// respondToOperation. The screenshot rides on the FlowResultPart (spec 025
// FR-026 — control-channel carrier); saolei tests use it so the agent's
// recognition engine can decode the board. Pass nil to omit the screenshot.
func respondToOperationWithScreenshot(t *testing.T, conn *websocket.Conn, sessionID string, opFrame *game.TeamFrame, status game.ToolResultStatus, message string, screenshot *game.ImagePart) {
	t.Helper()
	toolID := frameOperationToolID(opFrame)
	if toolID == "" {
		t.Fatalf("respondToOperationWithScreenshot: operation frame has no tool_id")
	}
	writeWSFrame(t, conn, buildFlowResultFrame(sessionID, toolID, status, message, screenshot))
}

// ─── Tool-call / tool-result MessagePart helpers ────────────────────────────
//
// The model's tool invocation (name + args_json) and the tool's LLM result
// both surface as MessageParts — `tool_call` and `tool_result` — grouped by
// the conversation-channel tool_id (the LangChain tool_call.id). These
// helpers project those parts out of a live frame or a history Message for
// the agent_saolei / agent_operation / agent_dialog / agent_checkpoint suites
// (specs/023-saolei-mcp-refine/contracts/content-model-contract.md §1/§2;
// data-model.md §4/§6).

// frameHasToolCall reports whether a message_parts frame carries a
// ToolCallPart.
func frameHasToolCall(f *game.TeamFrame) bool {
	return frameToolCall(f) != nil
}

// frameToolCall returns the first ToolCallPart in a message_parts frame, or
// nil.
func frameToolCall(f *game.TeamFrame) *game.ToolCallPart {
	mp := frameMessageParts(f)
	if mp == nil {
		return nil
	}
	for _, p := range mp.GetParts() {
		if tc := p.GetToolCall(); tc != nil {
			return tc
		}
	}
	return nil
}

// frameHasToolResult reports whether a message_parts frame carries a
// ToolResultPart.
func frameHasToolResult(f *game.TeamFrame) bool {
	return frameToolResult(f) != nil
}

// frameToolResult returns the first ToolResultPart in a message_parts frame,
// or nil.
func frameToolResult(f *game.TeamFrame) *game.ToolResultPart {
	mp := frameMessageParts(f)
	if mp == nil {
		return nil
	}
	for _, p := range mp.GetParts() {
		if tr := p.GetToolResult(); tr != nil {
			return tr
		}
	}
	return nil
}

// messageHasToolCall reports whether a Message's content MessageParts contains
// a ToolCallPart whose name matches. The tool_call part is the model's
// semantic tool invocation rendered as a conversation bubble (spec 023 FR-002;
// data-model.md §2).
func messageHasToolCall(m *game.Message, name string) bool {
	if m.GetContent() == nil {
		return false
	}
	for _, p := range m.GetContent().GetParts() {
		if tc := p.GetToolCall(); tc != nil && tc.GetName() == name {
			return true
		}
	}
	return false
}

// messageToolCallNames returns the names of every ToolCallPart in a Message's
// content MessageParts, in order.
func messageToolCallNames(m *game.Message) []string {
	if m.GetContent() == nil {
		return nil
	}
	var names []string
	for _, p := range m.GetContent().GetParts() {
		if tc := p.GetToolCall(); tc != nil {
			names = append(names, tc.GetName())
		}
	}
	return names
}

// messageToolResultStatuses returns the ToolResultStatus of every
// ToolResultPart in a Message's content MessageParts, in order. Used to
// assert the real status survives a leave/re-enter cycle for native tools and
// that saolei (MCP) results read neutral (research.md D12; data-model.md §6).
func messageToolResultStatuses(m *game.Message) []game.ToolResultStatus {
	if m.GetContent() == nil {
		return nil
	}
	var statuses []game.ToolResultStatus
	for _, p := range m.GetContent().GetParts() {
		if tr := p.GetToolResult(); tr != nil {
			statuses = append(statuses, tr.GetStatus())
		}
	}
	return statuses
}

// messageToolCallArgsJSON returns the args_json of the first ToolCallPart in a
// Message whose name matches, or "" when no such part exists. The args_json
// is the model's arguments verbatim (research.md D3).
func messageToolCallArgsJSON(m *game.Message, name string) string {
	if m.GetContent() == nil {
		return ""
	}
	for _, p := range m.GetContent().GetParts() {
		if tc := p.GetToolCall(); tc != nil && tc.GetName() == name {
			return tc.GetArgsJson()
		}
	}
	return ""
}
