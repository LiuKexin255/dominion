package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"dominion/common/gopkg/bootstrap"
	phttp "dominion/common/gopkg/http"
	"dominion/common/gopkg/otel"
)

var port = flag.String("port", "8080", "Port to listen on")

type chatCompletionRequest struct {
	Model    string         `json:"model"`
	Stream   bool           `json:"stream"`
	Messages []messageParam `json:"messages"`
}

type messageParam struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []choice `json:"choices"`
}

type choice struct {
	Index        int      `json:"index"`
	Message      *message `json:"message,omitempty"`
	Delta        *message `json:"delta,omitempty"`
	FinishReason *string  `json:"finish_reason"`
}

type message struct {
	Role             string `json:"role,omitempty"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
	Content          string `json:"content,omitempty"`
}

type streamChunk struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []choice `json:"choices"`
}

const (
	reasoningText = "thinking..."
	responseText  = "hello from fake llm"
	fakeModel     = "fake-model"
)

func handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req chatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.Stream {
		serveStreamingResponse(w)
		return
	}
	serveNonStreamingResponse(w)
}

func serveNonStreamingResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	stop := "stop"
	resp := chatCompletionResponse{
		ID:      "fake-1",
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   fakeModel,
		Choices: []choice{
			{
				Index: 0,
				Message: &message{
					Role:             "assistant",
					ReasoningContent: reasoningText,
					Content:          responseText,
				},
				FinishReason: &stop,
			},
		},
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("failed to encode non-streaming response: %v", err)
	}
}

func serveStreamingResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	now := time.Now().Unix()
	chunks := []streamChunk{
		{
			ID:      "fake-1",
			Object:  "chat.completion.chunk",
			Created: now,
			Model:   fakeModel,
			Choices: []choice{
				{
					Index: 0,
					Delta: &message{
						Role:             "assistant",
						ReasoningContent: reasoningText,
						Content:          "",
					},
					FinishReason: nil,
				},
			},
		},
		{
			ID:      "fake-1",
			Object:  "chat.completion.chunk",
			Created: now,
			Model:   fakeModel,
			Choices: []choice{
				{
					Index: 0,
					Delta: &message{
						ReasoningContent: "",
						Content:          responseText,
					},
					FinishReason: nil,
				},
			},
		},
		{
			ID:      "fake-1",
			Object:  "chat.completion.chunk",
			Created: now,
			Model:   fakeModel,
			Choices: []choice{
				{
					Index:        0,
					Delta:        &message{ReasoningContent: "", Content: ""},
					FinishReason: strPtr("stop"),
				},
			},
		},
	}

	for _, chunk := range chunks {
		data, err := json.Marshal(chunk)
		if err != nil {
			log.Printf("failed to marshal stream chunk: %v", err)
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		// Small delay is not strictly required but mimics real streaming.
		time.Sleep(10 * time.Millisecond)
	}

	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func strPtr(s string) *string {
	return &s
}

func main() {
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", handleChatCompletions)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:    ":" + *port,
		Handler: phttp.Handler(mux, "openai-llm-fake-service"),
	}

	log.Printf("openai-llm-fake-service listening on :%s", *port)

	b := bootstrap.New()
	b.Register(otel.Component())
	b.Register(bootstrap.HTTPServer("http", srv))
	log.Fatal(b.Run(context.Background()))
}
