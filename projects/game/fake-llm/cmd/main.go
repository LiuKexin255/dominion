package main

import (
	"context"
	"flag"
	"log"
	"net/http"

	"dominion/common/gopkg/bootstrap"
	phttp "dominion/common/gopkg/http"
	"dominion/common/gopkg/otel"
	"dominion/projects/game/fake-llm/service"
)

var port = flag.String("port", "8080", "Port to listen on")

func main() {
	flag.Parse()

	store, err := service.NewMessageStore()
	if err != nil {
		log.Fatalf("failed to load message store: %v", err)
	}
	log.Printf("fake-llm loaded %d messages", len(store.Messages()))

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", handleChatCompletions)
	mux.HandleFunc("/health", handleHealth)

	srv := &http.Server{
		Addr:    ":" + *port,
		Handler: phttp.Handler(mux, "fake-llm"),
	}

	log.Printf("fake-llm listening on :%s", *port)

	b := bootstrap.New()
	b.Register(otel.Component())
	b.Register(bootstrap.HTTPServer("http", srv))
	log.Fatal(b.Run(context.Background()))
}

// handleHealth is the liveness probe. It returns the literal body "ok".
func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// handleChatCompletions is a stub for the OpenAI-compatible endpoint.
// It always returns 200 with {"status":"ok"}; the keyword-matching
// implementation backed by the MessageStore arrives in T2.
func handleChatCompletions(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
