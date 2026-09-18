package testplan

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"

	"dominion/common/gopkg/otel/tracecontext"
)

const (
	// headerEnv carries the run's environment name so the shared apitest
	// ingress routes the request into this deployment — the documented
	// test-environment routing header (tools/release/deploy/README.md
	// §环境类型) that every existing testplan in the repo sends.
	headerEnv = "env"
	// headerXDominionEnv is the header spelling named by
	// specs/047-dsh-chat-demo/quickstart.md §4 and specs/047-dsh-chat-demo/tasks.md T019. It rides
	// along with headerEnv so the request stays valid under an ingress
	// configured to match either name; unmatched headers are ignored.
	headerXDominionEnv = "x-dominion-env"
)

// sendMessageResponse mirrors the SendMessageResponse JSON body returned by
// the gateway (specs/047-dsh-chat-demo/contracts/chat-api.md §1).
type sendMessageResponse struct {
	Name  string `json:"name"`
	Reply string `json:"reply"`
}

// traceContext returns a context carrying a W3C trace context for the test
// (style/large_test.md §测试用例 — set and print trace_id for log/trace
// correlation). It continues the TRACEPARENT injected by `guitar run` into
// the test process env when present, else starts a fresh trace; the
// trace_id is printed so an operator can correlate the test's HTTP traffic
// in signoz.
func traceContext(t *testing.T) context.Context {
	t.Helper()
	ctx := tracecontext.FromEnv(context.Background())
	t.Logf("trace_id: %s", tracecontext.ID(ctx))
	return ctx
}

// postChatTurn POSTs one sendMessage custom-method request for resourceName
// (the `conversations/{id}` value) against the public HTTP entry and returns
// the HTTP status plus the raw response body. The request runs on ctx (see
// traceContext) with the traceparent header injected, so the SUT's spans
// join the test's trace.
func postChatTurn(t *testing.T, ctx context.Context, baseURL, envName, resourceName string, body []byte) (int, []byte) {
	t.Helper()

	reqURL := fmt.Sprintf("%s/experimental/dsh-demo/%s:sendMessage", baseURL, resourceName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("http.NewRequestWithContext %s %s: %v", http.MethodPost, reqURL, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(headerEnv, envName)
	req.Header.Set(headerXDominionEnv, envName)

	client := &http.Client{Transport: tracecontext.NewHTTPTransport(http.DefaultTransport)}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", http.MethodPost, reqURL, err)
	}

	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read response %s %s: %v", http.MethodPost, reqURL, err)
	}
	return resp.StatusCode, respBody
}
