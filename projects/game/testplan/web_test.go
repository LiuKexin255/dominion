// Package testplan contains web hosting integration tests. These tests
// validate the web service's static hosting surface (GET / entry HTML and
// the built assets it references) and the page's management closed loop at
// the HTTP layer the frontend itself drives (session CRUD over /api/v1 plus
// the /api/v2 conversation surface) — no browser required
// (specs/049-agent-v2-dsh-init/quickstart.md §2 用例 1/10).
package testplan

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"dominion/common/gopkg/testtool"
	game "dominion/projects/game"
)

// assetsReferenceRe matches the built asset URLs vite rewrites into the
// entry HTML (hashed /assets/<name>-<hash>.js / .css).
var assetsReferenceRe = regexp.MustCompile(`/assets/[^"'() ]+`)

// fetchWeb is doHTTPTrace restricted to the web module: every request here
// exercises the web service's hosting surface at the public endpoint root.
func fetchWeb(t *testing.T, ctx context.Context, sutHostURL, sutEnvName, path string) (*http.Response, []byte) {
	t.Helper()
	return doHTTPTrace(t, ctx, http.MethodGet, sutHostURL+path, sutEnvName, nil)
}

// TestWebServesEntrypageHTML covers quickstart §2 用例 10 first half
// (FR-013): GET / returns the built entry HTML — the #root mount node and
// the module script referencing the hashed asset tree. The attributes are
// asserted separately: the vite build emits
// <script type="module" crossorigin src="/assets/index-<hash>.js"> and the
// assertion must not depend on attribute adjacency.
func TestWebServesEntrypageHTML(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	resp, body := fetchWeb(t, ctx, sutHostURL, sutEnvName, "/")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200 (body: %s)", resp.StatusCode, body)
	}
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("GET / Content-Type = %q, want text/html", contentType)
	}
	html := string(body)
	if !strings.Contains(html, `<div id="root"></div>`) {
		t.Errorf("entry HTML lacks the #root mount node:\n%s", html)
	}
	if !strings.Contains(html, `<script type="module"`) {
		t.Errorf("entry HTML lacks the module script:\n%s", html)
	}
	if !strings.Contains(html, `src="/assets/`) {
		t.Errorf("entry HTML script does not reference /assets:\n%s", html)
	}
}

// TestWebServesStaticAssets covers quickstart §2 用例 10 second half: every
// /assets/* reference in the entry HTML resolves (200, non-empty body) —
// the page's JS and CSS are hosted by the web service, not just the HTML
// shell.
func TestWebServesStaticAssets(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	_, body := fetchWeb(t, ctx, sutHostURL, sutEnvName, "/")
	refs := assetsReferenceRe.FindAllString(string(body), -1)
	if len(refs) == 0 {
		t.Fatalf("entry HTML carries no /assets references:\n%s", body)
	}
	sawScript := false
	sawStylesheet := false
	for _, ref := range refs {
		resp, asset := fetchWeb(t, ctx, sutHostURL, sutEnvName, ref)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", ref, resp.StatusCode)
			continue
		}
		if len(asset) == 0 {
			t.Errorf("GET %s returned an empty body", ref)
		}
		switch {
		case strings.HasSuffix(ref, ".js"):
			sawScript = true
		case strings.HasSuffix(ref, ".css"):
			sawStylesheet = true
		}
	}
	if !sawScript {
		t.Errorf("no .js asset resolved from refs %v — the page cannot boot", refs)
	}
	if !sawStylesheet {
		t.Errorf("no .css asset resolved from refs %v — the page renders unstyled", refs)
	}
}

// TestWebManagementLoopSmoke covers quickstart §2 用例 1 (US4) at the HTTP
// layer the page drives: 新建 → 物化 agent（preset + UpdateAgent）→ 发一轮
// 对话 → 回填可见 → 删除 闭环 (specs/051-agent-v2-dsh-migration/
// contracts/agent-api.md §2/§5; web-frontend.md §2/§3 — the page guides the
// unmaterialized session through the preset/model panel before sending, and
// the delete orchestration is the bare session DELETE with no dispose hop).
func TestWebManagementLoopSmoke(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	// 新建：AIP-133 create under the saolei template.
	sessionID, _ := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)
	sessionName := agentV2SessionName(sessionID)
	t.Logf("created session %s", sessionName)

	// 物化：the page's agent panel applies a preset (agent-api.md §2.1 —
	// Send has no lazy materialization).
	preset := createAgentV2Preset(t, ctx, sutHostURL, sutEnvName, "web-loop-"+uniqueSuffix(), "web smoke persona")
	updateAgentV2Agent(t, ctx, sutHostURL, sutEnvName, sessionName, preset.GetName(), "")

	// 列表可见：the new session appears in the template listing.
	listBody := listSessions(t, sutHostURL, sutEnvName, saoleiTemplateID, 50)
	var list listSessionsResponse
	if err := json.Unmarshal(listBody, &list); err != nil {
		t.Fatalf("unmarshal ListSessionsResponse: %v (raw: %s)", err, listBody)
	}
	found := false
	for _, s := range list.Sessions {
		if strings.HasSuffix(s.Name, "/"+sessionID) {
			found = true
			if s.CreateTime == "" {
				t.Errorf("session %s listed without createTime", sessionID)
			}
		}
	}
	if !found {
		t.Errorf("created session %s is absent from the listing (US4 场景 1)", sessionID)
	}

	// 进入对话发一轮：a full think+text turn through /api/v2.
	stream := startAgentV2Send(t, ctx, sutHostURL, sutEnvName, sessionName,
		agentV2TriggerThink+" web loop smoke")
	defer stream.Close()
	events := drainAgentV2Turn(t, stream)
	assertAgentV2TurnWellFormed(t, sessionName, events)
	if end := events[len(events)-1].GetTurnEnd(); end.GetStatus() != game.TurnStatus_TURN_STATUS_COMPLETED {
		t.Fatalf("smoke turn ended %v, want COMPLETED", end.GetStatus())
	}
	if got := agentV2TerminalBlocksFromEvents(events).text; got != agentV2GreetText {
		t.Errorf("smoke turn text = %q, want %q", got, agentV2GreetText)
	}

	// 回填：the materialized agent's history is queryable through the
	// standard List method (agent-api.md §2.3) for the page refresh path.
	hist := listAgentV2Messages(t, ctx, sutHostURL, sutEnvName, sessionName)
	if len(hist.GetMessages()) != 2 {
		t.Errorf("history messages = %d, want 2", len(hist.GetMessages()))
	}

	// 删除：the delete orchestration is ONLY the session DELETE — Dispose
	// is gone and the agent is not fanned out (FR-007, web-frontend.md §5).
	delResp := deleteSession(t, sutHostURL, sutEnvName, saoleiTemplateID, sessionID)
	if delResp.StatusCode != http.StatusOK && delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE session status = %d, want 200 or 204", delResp.StatusCode)
	}

	listBody = listSessions(t, sutHostURL, sutEnvName, saoleiTemplateID, 50)
	list = listSessionsResponse{}
	if err := json.Unmarshal(listBody, &list); err != nil {
		t.Fatalf("unmarshal ListSessionsResponse: %v (raw: %s)", err, listBody)
	}
	for _, s := range list.Sessions {
		if strings.HasSuffix(s.Name, "/"+sessionID) {
			t.Errorf("deleted session %s still present in the listing (US4 场景 2)", sessionID)
		}
	}
}
