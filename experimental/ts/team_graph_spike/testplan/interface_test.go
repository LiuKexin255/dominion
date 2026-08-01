package testplan

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"dominion/common/gopkg/otel/tracecontext"
	"dominion/common/gopkg/testtool"
)

const pathPrefix = "/experimental/team-graph/invoke"

// TestTeamGraphInvoke verifies hypothesis A5: a ChatOpenAI pointed at the
// deployed game fake-llm drives the LangGraph team graph end-to-end — the
// player createAgent calls make_move then stops (fake-llm tool config), the
// structured gameEnded signal routes to the planner, and the planner calls
// update_strategy. The endpoint returns the final gameEnded, the persisted
// strategy, and per-agent message counts.
func TestTeamGraphInvoke(t *testing.T) {
	host := testtool.MustEndpoint("http", "public")
	env := testtool.MustEnv()

	body, err := json.Marshal(map[string]string{
		"prompt":    "play one move",
		"sessionId": "spike-large",
	})
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	reqURL := host + pathPrefix
	req, err := http.NewRequest(http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("http.NewRequest(%q) failed: %v", reqURL, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("env", env)

	// style/large_test.md: set a trace context and log the trace_id so
	// failures can be correlated in SigNoz.
	ctx := tracecontext.Ensure(context.Background())
	req = req.WithContext(ctx)
	client := &http.Client{Transport: tracecontext.NewHTTPTransport(http.DefaultTransport)}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("http.Do(%q) failed: %v (trace_id=%s)", reqURL, err, tracecontext.ID(ctx))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d (trace_id=%s)", resp.StatusCode, http.StatusOK, tracecontext.ID(ctx))
	}

	var result struct {
		GameEnded           string `json:"gameEnded"`
		Strategy            string `json:"strategy"`
		PlayerMessageCount  int    `json:"playerMessageCount"`
		PlannerMessageCount int    `json:"plannerMessageCount"`
		BaseURL             string `json:"baseURL"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("json.Decode failed: %v (trace_id=%s)", err, tracecontext.ID(ctx))
	}

	t.Logf("trace_id=%s gameEnded=%q strategy=%q playerMsgs=%d plannerMsgs=%d baseURL=%q",
		tracecontext.ID(ctx), result.GameEnded, result.Strategy,
		result.PlayerMessageCount, result.PlannerMessageCount, result.BaseURL)

	// A5: the player loop ran a tool then stopped — playerMessages accumulated
	// (human + AI(tool_call) + tool result + AI(stop)).
	if result.PlayerMessageCount < 3 {
		t.Errorf("playerMessageCount = %d, want >= 3 (tool loop ran)", result.PlayerMessageCount)
	}
	// A5: the planner ran — routed via the gameEnded conditional edge. That it
	// ran at all proves the player's structured game-won signal was read by the
	// outer node and the conditional edge routed on the non-messages field.
	if result.PlannerMessageCount == 0 {
		t.Errorf("plannerMessageCount = %d, want > 0 (planner was routed to)", result.PlannerMessageCount)
	}
	// A5: the planner called update_strategy (strategy persisted). This is the
	// end-to-end proof that ChatOpenAI → fake-llm drove the planner's tool call.
	if result.Strategy == "" {
		t.Errorf("expected non-empty strategy after planner called update_strategy")
	}
	// Note: the FINAL gameEnded is "" (null) because the planner node clears it
	// unconditionally after running (D6 step 6) — that is the correct, expected
	// behaviour, NOT a routing failure. The transient "won" value is proven by
	// the planner having run (plannerMessageCount > 0).
}
