package main

import (
	"context"
	"flag"
	"log"
	"math/rand/v2"
	"net/http"
	"time"

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

	// Shared *rand.Rand backs the no-match fallback for every request.
	// *rand.Rand is not concurrency-safe; the worst-case outcome of a
	// race is a degraded distribution on simultaneous fallbacks, never
	// an invalid response, so we deliberately accept it over a mutex.
	rng := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0))

	mux := http.NewServeMux()
	mux.Handle("/v1/chat/completions", service.NewChatHandler(store, rng))
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
