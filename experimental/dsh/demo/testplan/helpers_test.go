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

// The fake-llm template texts asserted by the testplan suites — the
// single-source-of-truth anchors pinned by
// experimental/dsh/demo/fake-llm/service/message_store_test.go
// (TestNewMessageStore_LoadsEmbeddedChat), defined in
// experimental/dsh/demo/fake-llm/service/testdata/{chat,preset}.yaml and
// mapped to the acceptance scenarios by
// specs/047-dsh-chat-demo/contracts/fake-llm-templates.md §4 and
// specs/058-dsh-preset-roster-demo/contracts/fake-llm-system-keywords.md §3.
// Shared here because multiple largetest targets in this directory assert
// them (style/large_test.md — shared constants live in the helper file).
const (
	greetingText      = "Hello! How can I help you today?"
	greetingAgainText = "Hello again! We have already met."
	farewellText      = "I'm sorry, I didn't catch that."
	// Preset scenario replies: the fake-llm answers a probe turn with the
	// template whose system_keywords match the session's model-visible
	// composition (persona text / demo_echo guidance heading).
	personaStandardReply = "persona-standard-hit"
	personaToolsReply    = "persona-tools-hit"
	guidanceReply        = "tool-guidance-hit"
	// Authored-preset persona replies: the test composes the persona prose
	// with the paired marker string, so the reply names the authored
	// generation the model actually received (preset resource-face tests).
	personaAuthoredOneReply = "persona-authored-one-hit"
	personaAuthoredTwoReply = "persona-authored-two-hit"
)

// The authored persona proses the resource-face tests inject through
// CreatePreset/UpdatePreset. Each carries exactly one marker string matched
// by its paired fake-llm template in testdata/preset.yaml, and the two
// markers are mutually exclusive, so a probe turn identifies the generation
// the session runs on.
const (
	personaAuthoredOne = "You are the AUTHORED PERSONA MARKER ONE assistant."
	personaAuthoredTwo = "You are the AUTHORED PERSONA MARKER TWO assistant."
)

// sendMessageResponse mirrors the SendMessageResponse JSON body returned by
// the gateway (specs/047-dsh-chat-demo/contracts/chat-api.md §1).
type sendMessageResponse struct {
	Name  string `json:"name"`
	Reply string `json:"reply"`
}

// conversationResponse mirrors the Conversation resource JSON body returned
// by the gateway for CreateConversation (specs/058-dsh-preset-roster-demo/
// contracts/chat-api.md §1.1): the resource name, the RESOLVED preset id the
// conversation is bound to, and the creation timestamp.
type conversationResponse struct {
	Name       string `json:"name"`
	Preset     string `json:"preset"`
	CreateTime string `json:"createTime"`
}

// presetResponse mirrors the Preset resource JSON body returned by the
// gateway for the PresetService CRUD face (specs/058-dsh-preset-roster-demo/
// contracts/chat-api.md §2).
type presetResponse struct {
	Name        string `json:"name"`
	Template    string `json:"template"`
	Persona     string `json:"persona"`
	DisplayName string `json:"displayName"`
	CreateTime  string `json:"createTime"`
	UpdateTime  string `json:"updateTime"`
}

// listPresetsResponse mirrors the ListPresetsResponse JSON body (chat-api.md
// §2; demo scale — the full collection, no page token).
type listPresetsResponse struct {
	Presets []presetResponse `json:"presets"`
}

// createConversation POSTs one CreateConversation request against the public
// HTTP entry and returns the HTTP status plus the raw response body. presetID
// may be empty (the roster default); it is only included in the body when
// non-empty so the default-selection case exercises the absent field.
func createConversation(t *testing.T, ctx context.Context, baseURL, envName, conversationID, presetID string) (int, []byte) {
	t.Helper()

	reqURL := fmt.Sprintf("%s/experimental/dsh-demo/conversations", baseURL)
	bodyFields := fmt.Sprintf(`"conversation_id": %q`, conversationID)
	if presetID != "" {
		bodyFields += fmt.Sprintf(`, "preset": %q`, presetID)
	}
	body := []byte("{" + bodyFields + "}")
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

// presetRequest issues one PresetService HTTP request (specs/
// 058-dsh-preset-roster-demo/contracts/chat-api.md §2 HTTP annotations)
// against the public HTTP entry and returns the HTTP status plus the raw
// response body. pathSuffix is the path below the presets collection ("" for
// the collection itself, "/{id}" for one resource); body is the JSON payload
// for POST/PATCH and nil for GET/DELETE. The request runs on ctx (see
// traceContext) with the traceparent header injected, so the SUT's spans
// join the test's trace.
func presetRequest(t *testing.T, ctx context.Context, baseURL, envName, method, pathSuffix string, body []byte) (int, []byte) {
	t.Helper()

	reqURL := baseURL + "/experimental/dsh-demo/presets" + pathSuffix
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, reader)
	if err != nil {
		t.Fatalf("http.NewRequestWithContext %s %s: %v", method, reqURL, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(headerEnv, envName)
	req.Header.Set(headerXDominionEnv, envName)

	client := &http.Client{Transport: tracecontext.NewHTTPTransport(http.DefaultTransport)}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, reqURL, err)
	}

	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read response %s %s: %v", method, reqURL, err)
	}
	return resp.StatusCode, respBody
}
