package testplan

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"dominion/common/gopkg/testtool"
)

const pathPrefix = "/experimental/openai-llm/invoke"

func TestOpenAILLMInvoke(t *testing.T) {
	host := testtool.MustEndpoint("http", "public")
	env := testtool.MustEnv()

	body, err := json.Marshal(map[string]string{"prompt": "say hello and think out loud"})
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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http.Do(%q) failed: %v", reqURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result struct {
		Blocks []struct {
			Type      string `json:"type"`
			Content   string `json:"content"`
			Text      string `json:"text"`
			Reasoning string `json:"reasoning"`
		} `json:"blocks"`
		AdditionalKwargs struct {
			ReasoningContent string `json:"reasoning_content"`
		} `json:"additionalKwargs"`
		HasNativeReasoningBlock bool `json:"hasNativeReasoningBlock"`
		BaseURL                 string `json:"baseURL"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("json.Decode failed: %v", err)
	}

	t.Logf("hasNativeReasoningBlock=%v reasoning_content=%q blocks=%+v",
		result.HasNativeReasoningBlock,
		result.AdditionalKwargs.ReasoningContent,
		result.Blocks)

	if result.AdditionalKwargs.ReasoningContent == "" {
		t.Errorf("expected additionalKwargs.reasoning_content to be non-empty")
	}

	var hasText bool
	for _, b := range result.Blocks {
		if b.Type == "text" && b.Text != "" {
			hasText = true
			break
		}
	}
	if !hasText {
		t.Errorf("expected at least one non-empty text block")
	}
}
