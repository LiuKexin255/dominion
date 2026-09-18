package testplan

import (
	"encoding/json"
	"net/http"
	"testing"

	"dominion/common/gopkg/testtool"
)

// The fake-llm template texts asserted by the multi-turn suites — the
// single-source-of-truth anchors pinned by
// experimental/dsh/demo/fake-llm/service/message_store_test.go
// (TestNewMessageStore_LoadsEmbeddedChat), defined in
// experimental/dsh/demo/fake-llm/service/testdata/chat.yaml and mapped
// to the acceptance scenarios by
// specs/047-dsh-chat-demo/contracts/fake-llm-templates.md §4.
const (
	greetingText      = "Hello! How can I help you today?"
	greetingAgainText = "Hello again! We have already met."
)

// TestMultiturnBranch verifies the multi-turn branch through the public
// HTTP entry (US2-1, specs/047-dsh-chat-demo/contracts/chat-api.md §4):
// within ONE conversation the first "hello" turn takes the greeting
// template and the second — identical — "hello" turn takes the
// greeting-again multi-turn template. The repeated message is the point:
// the branch switch is carried entirely by the fake-llm's
// history_keywords/min_turn conditions over the conversation history
// the agent replays (specs/047-dsh-chat-demo/contracts/
// fake-llm-templates.md §4 测试设计要点), not by a different prompt.
func TestMultiturnBranch(t *testing.T) {
	baseURL := testtool.MustEndpoint("http", "public")
	envName := testtool.MustEnv()
	ctx := traceContext(t)

	// given: one fresh conversation and one request body reused by both
	// turns.
	const resourceName = "conversations/us2-multiturn-branch"
	body := []byte(`{"message": "hello"}`)
	turns := []struct {
		turn      int
		wantReply string
	}{
		{turn: 1, wantReply: greetingText},
		{turn: 2, wantReply: greetingAgainText},
	}

	for _, tt := range turns {
		// when: the turn fires against the same conversation.
		status, respBody := postChatTurn(t, ctx, baseURL, envName, resourceName, body)

		// then: the reply is this turn's branch template, verbatim.
		if status != http.StatusOK {
			t.Fatalf("turn %d: status = %d, want %d (body: %s)", tt.turn, status, http.StatusOK, respBody)
		}
		got := new(sendMessageResponse)
		if err := json.Unmarshal(respBody, got); err != nil {
			t.Fatalf("turn %d: json.Unmarshal(%s) unexpected error: %v", tt.turn, respBody, err)
		}
		if got.Reply != tt.wantReply {
			t.Errorf("turn %d reply = %q, want %q (branch template text must match verbatim)", tt.turn, got.Reply, tt.wantReply)
		}
	}
}

// TestMultiturnIsolation verifies cross-conversation isolation (US2-2,
// specs/047-dsh-chat-demo/contracts/chat-api.md §4): after conversation
// A has already driven the multi-turn branch, a FRESH conversation B
// sending the same "hello" message still receives the first-turn
// greeting reply — A's multi-turn history never leaks into B.
func TestMultiturnIsolation(t *testing.T) {
	baseURL := testtool.MustEndpoint("http", "public")
	envName := testtool.MustEnv()
	ctx := traceContext(t)

	// given: conversation A will complete two hello turns — first the
	// greeting branch, then the greeting-again multi-turn branch, making
	// A's multi-turn history real before B sends anything — and then a
	// fresh conversation B sends the same message.
	const (
		convA = "conversations/us2-isolation-a"
		convB = "conversations/us2-isolation-b"
	)
	body := []byte(`{"message": "hello"}`)
	turnsA := []struct {
		turn      int
		wantReply string
	}{
		{turn: 1, wantReply: greetingText},
		{turn: 2, wantReply: greetingAgainText},
	}

	// when: A's two turns run first, then B's first turn.
	for _, tt := range turnsA {
		status, respBody := postChatTurn(t, ctx, baseURL, envName, convA, body)
		if status != http.StatusOK {
			t.Fatalf("conversation A turn %d: status = %d, want %d (body: %s)", tt.turn, status, http.StatusOK, respBody)
		}
		got := new(sendMessageResponse)
		if err := json.Unmarshal(respBody, got); err != nil {
			t.Fatalf("conversation A turn %d: json.Unmarshal(%s) unexpected error: %v", tt.turn, respBody, err)
		}
		if got.Reply != tt.wantReply {
			t.Errorf("conversation A turn %d reply = %q, want %q (A itself follows the branch sequence)", tt.turn, got.Reply, tt.wantReply)
		}
	}
	status, respBody := postChatTurn(t, ctx, baseURL, envName, convB, body)

	// then: B's reply is the FIRST-turn branch — greeting, not
	// greeting-again.
	if status != http.StatusOK {
		t.Fatalf("conversation B: status = %d, want %d (body: %s)", status, http.StatusOK, respBody)
	}
	got := new(sendMessageResponse)
	if err := json.Unmarshal(respBody, got); err != nil {
		t.Fatalf("conversation B: json.Unmarshal(%s) unexpected error: %v", respBody, err)
	}
	if got.Reply != greetingText {
		t.Errorf("conversation B reply = %q, want %q (a fresh conversation starts on the first-turn branch)", got.Reply, greetingText)
	}
}

// TestMultiturnInterleaved verifies concurrent interleaved conversations
// (US2-3, specs/047-dsh-chat-demo/contracts/chat-api.md §4): two
// conversations alternate turns and each follows its own branch
// sequence — first turn greeting, second turn greeting-again —
// regardless of the other conversation's interleaved turns.
func TestMultiturnInterleaved(t *testing.T) {
	baseURL := testtool.MustEndpoint("http", "public")
	envName := testtool.MustEnv()
	ctx := traceContext(t)

	// given: two fresh conversations whose turns strictly alternate.
	const (
		convA = "conversations/us2-interleave-a"
		convB = "conversations/us2-interleave-b"
	)
	body := []byte(`{"message": "hello"}`)
	turns := []struct {
		conversation string
		wantReply    string
	}{
		{conversation: convA, wantReply: greetingText},
		{conversation: convB, wantReply: greetingText},
		{conversation: convA, wantReply: greetingAgainText},
		{conversation: convB, wantReply: greetingAgainText},
	}

	for i, tt := range turns {
		// when: the turn fires against the table's conversation.
		status, respBody := postChatTurn(t, ctx, baseURL, envName, tt.conversation, body)

		// then: the reply matches that conversation's own branch
		// sequence — no cross-conversation turn counting.
		if status != http.StatusOK {
			t.Fatalf("turn %d (%s): status = %d, want %d (body: %s)", i+1, tt.conversation, status, http.StatusOK, respBody)
		}
		got := new(sendMessageResponse)
		if err := json.Unmarshal(respBody, got); err != nil {
			t.Fatalf("turn %d (%s): json.Unmarshal(%s) unexpected error: %v", i+1, tt.conversation, respBody, err)
		}
		if got.Reply != tt.wantReply {
			t.Errorf("turn %d (%s) reply = %q, want %q (each conversation follows its own branch sequence)", i+1, tt.conversation, got.Reply, tt.wantReply)
		}
	}
}
