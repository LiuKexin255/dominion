// Package testplan contains web hosting integration tests. These tests
// validate the web service's static hosting surface (GET / entry HTML and
// the built assets it references) and the page's management closed loop at
// the HTTP layer the frontend itself drives (session CRUD over /api/v1 plus
// the /api/v2 team surface) — no browser required
// (specs/059-agent-v2-team-mode/contracts/web-views.md §1: the page guides an
// unmaterialized session through the team panel before sending).
package testplan

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"dominion/common/gopkg/testtool"
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
// the module script referencing the hashed asset tree.
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

// TestWebManagementLoopSmoke covers the page's management closed loop at the
// HTTP layer the frontend drives: 新建 → 物化 team（两个 preset + UpdateTeam）
// → 发一轮 team 对话 → 回填可见 → 删除 (web-views.md §1/§2; team-api.md §2/§5
// — the page guides an unmaterialized session through the team panel before
// sending, and the delete orchestration is the bare session DELETE with no
// dispose hop).
func TestWebManagementLoopSmoke(t *testing.T) {
	sutHostURL := testtool.MustEndpoint("http", "public")
	sutEnvName := testtool.MustEnv()
	ctx := traceContext(t)

	// 新建：AIP-133 create under the saolei template.
	sessionID, _ := createSession(t, sutHostURL, sutEnvName, saoleiTemplateID)
	sessionName := agentV2SessionName(sessionID)
	t.Logf("created session %s", sessionName)

	// 物化：the page's team panel applies two role-pooled presets and both
	// members (team-api.md §2 — Send has no lazy materialization).
	player, planner := createAgentV2TeamPresetPair(t, ctx, sutHostURL, sutEnvName, "web-loop", "web smoke")
	team := updateAgentV2Team(t, ctx, sutHostURL, sutEnvName, sessionName, player.GetName(), planner.GetName(), "", "")
	if len(team.GetMembers()) != 2 {
		t.Fatalf("materialized members = %d, want 2", len(team.GetMembers()))
	}

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
		t.Errorf("created session %s is absent from the listing", sessionID)
	}

	// 进入对话发一轮：the first Send drives the planner opening, the
	// structural continuation drives the player (no desktop → readable
	// failure), all within one team stream.
	stream := startTeamSend(t, ctx, sutHostURL, sutEnvName, sessionName, teamStartMessage)
	events := drainTeamStream(t, stream)
	assertTeamStreamWellFormed(t, sessionName, events)
	turns := groupTeamMemberTurns(events)
	if len(turns) < 2 || turns[0].member != "planner" {
		t.Fatalf("smoke turns = %v, want the planner opening first", turns)
	}
	if _, text := teamTurnBlocks(turns[0]); text != teamPlannerOpeningText {
		t.Errorf("smoke opening text = %q, want %q", text, teamPlannerOpeningText)
	}

	// 回填：the team history is queryable through the standard List method
	// (team-api.md §5) for the page refresh path.
	if got := listTeamMessages(t, ctx, sutHostURL, sutEnvName, sessionName); len(got) < 2 {
		t.Errorf("backfilled team messages = %d, want the user + member entries", len(got))
	}

	// 删除：the delete orchestration is ONLY the session DELETE — no
	// fan-out to the team (web-views.md §6).
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
			t.Errorf("deleted session %s still present in the listing", sessionID)
		}
	}
}
