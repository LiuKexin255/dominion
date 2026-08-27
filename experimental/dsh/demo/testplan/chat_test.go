package testplan

import (
	"encoding/json"
	"net/http"
	"testing"

	"dominion/common/gopkg/testtool"
)

// TestChatReply verifies single-turn replies through the public HTTP entry:
// a keyword template hit returns its template text verbatim, and a no-match
// message returns the unique pure fallback template (US1-1/US1-3,
// specs/047-dsh-chat-demo/contracts/chat-api.md §4). Expected texts are the
// fake-llm testdata anchors,
// experimental/dsh/demo/fake-llm/service/testdata/chat.yaml
// (specs/047-dsh-chat-demo/contracts/fake-llm-templates.md §1/§4).
func TestChatReply(t *testing.T) {
	baseURL := testtool.MustEndpoint("http", "public")
	envName := testtool.MustEnv()
	ctx := traceContext(t)

	tests := []struct {
		name           string
		conversationID string
		message        string
		wantReply      string
	}{
		{
			name:           "keyword hit returns greeting template verbatim",
			conversationID: "us1-greeting",
			message:        "hello there",
			wantReply:      "Hello! How can I help you today?",
		},
		{
			name:           "keyword hit returns chat template verbatim",
			conversationID: "us1-chat",
			message:        "can we chat",
			wantReply:      "Sure, let's chat!",
		},
		{
			name:           "no keyword hit returns pure fallback template",
			conversationID: "us1-fallback",
			message:        "what is the weather",
			wantReply:      "I'm sorry, I didn't catch that.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given: one fresh conversation and the user message under test.
			resourceName := "conversations/" + tt.conversationID

			// when: one sendMessage round trip through the public entry.
			body := []byte(`{"message": "` + tt.message + `"}`)
			status, respBody := postChatTurn(t, ctx, baseURL, envName, resourceName, body)

			// then: the reply is the template text, verbatim, and the
			// resource name echoes back.
			if status != http.StatusOK {
				t.Fatalf("status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
			}
			got := new(sendMessageResponse)
			if err := json.Unmarshal(respBody, got); err != nil {
				t.Fatalf("json.Unmarshal(%s) unexpected error: %v", respBody, err)
			}
			if got.Name != resourceName {
				t.Errorf("name = %q, want %q (resource name echo)", got.Name, resourceName)
			}
			if got.Reply != tt.wantReply {
				t.Errorf("reply = %q, want %q (template text must match verbatim)", got.Reply, tt.wantReply)
			}
		})
	}
}

// TestChatReplyDeterminism verifies that repeating the identical request
// returns the identical reply (US1-2,
// specs/047-dsh-chat-demo/contracts/chat-api.md §4 "重复请求同 reply").
//
// The repeated message deliberately uses the chat keyword rather than
// "hello": once US2's greeting-again multi-turn template
// (keywords/histories [hello], specs/047-dsh-chat-demo/contracts/
// fake-llm-templates.md §4) joins the shared testdata, a repeated "hello"
// turn in one conversation would legitimately switch to that branch —
// breaking this assertion — while the chat keyword stays on the chat-only
// template for every turn.
func TestChatReplyDeterminism(t *testing.T) {
	baseURL := testtool.MustEndpoint("http", "public")
	envName := testtool.MustEnv()
	ctx := traceContext(t)

	// given: one conversation and one request body, sent as-is twice.
	const (
		resourceName = "conversations/us1-determinism"
		wantReply    = "Sure, let's chat!"
	)
	body := []byte(`{"message": "can we chat"}`)

	// when: the identical request is repeated in the same conversation.
	var replies []string
	for i := range 2 {
		status, respBody := postChatTurn(t, ctx, baseURL, envName, resourceName, body)
		if status != http.StatusOK {
			t.Fatalf("send %d: status = %d, want %d (body: %s)", i+1, status, http.StatusOK, respBody)
		}
		got := new(sendMessageResponse)
		if err := json.Unmarshal(respBody, got); err != nil {
			t.Fatalf("send %d: json.Unmarshal(%s) unexpected error: %v", i+1, respBody, err)
		}
		replies = append(replies, got.Reply)
	}

	// then: every reply equals the chat template text — identical and
	// verbatim across repeats.
	for i, reply := range replies {
		if reply != wantReply {
			t.Errorf("send %d reply = %q, want %q (repeats must be identical)", i+1, reply, wantReply)
		}
	}
}

// TestChatInvalidRequest verifies malformed requests are rejected with a
// clear error status while the chain stays up (spec Edge "畸形/非法请求",
// specs/047-dsh-chat-demo/spec.md Edge Cases).
//
// Two layers apply to the name field through the public entry:
//   - field validation: an empty or missing message reaches the agent as
//     message="" and maps to INVALID_ARGUMENT → HTTP 400
//     (specs/047-dsh-chat-demo/contracts/chat-api.md §1 error table);
//   - route validation: the {name=conversations/*} gateway pattern itself
//     rejects resource names outside the collection — or with an empty
//     conversation id — with HTTP 404 before the agent sees them
//     (grpc-gateway v2.27.6 runtime/mux.go routing error path,
//     https://github.com/grpc-ecosystem/grpc-gateway/blob/v2.27.6/runtime/mux.go).
//     The agent-side INVALID_ARGUMENT branch for names is therefore
//     exercised by the agent unit tests
//     (experimental/dsh/demo/agent/src/server.test.ts) — the same
//     unit-test/testplan split specs/047-dsh-chat-demo/tasks.md T019 notes for the
//     fake-llm-unreachable 500 row.
func TestChatInvalidRequest(t *testing.T) {
	baseURL := testtool.MustEndpoint("http", "public")
	envName := testtool.MustEnv()
	ctx := traceContext(t)

	tests := []struct {
		name         string
		resourceName string
		body         string
		wantStatus   int
	}{
		{
			name:         "empty message",
			resourceName: "conversations/us1-invalid",
			body:         `{"message": ""}`,
			wantStatus:   http.StatusBadRequest,
		},
		{
			name:         "missing message field",
			resourceName: "conversations/us1-invalid",
			body:         `{}`,
			wantStatus:   http.StatusBadRequest,
		},
		{
			name:         "resource name outside the conversations collection",
			resourceName: "sessions/us1-invalid",
			body:         `{"message": "hello"}`,
			wantStatus:   http.StatusNotFound,
		},
		{
			name:         "empty conversation id",
			resourceName: "conversations/",
			body:         `{"message": "hello"}`,
			wantStatus:   http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given: the malformed request from the table.

			// when: it reaches the public entry.
			status, respBody := postChatTurn(t, ctx, baseURL, envName, tt.resourceName, []byte(tt.body))

			// then: the request is rejected with the expected error status
			// and a readable body.
			if status != tt.wantStatus {
				t.Errorf("status = %d, want %d (body: %s)", status, tt.wantStatus, respBody)
			}
		})
	}
}
