// The fake-llm command serves the dsh-demo scripted model endpoint:
// an OpenAI chat-completions compatible fake (template matching with a
// deterministic fallback) consumed by the official dsh-llm-deepseek
// adapter over SSE (specs/047-dsh-chat-demo/contracts/fake-llm-wire.md).
package main

import (
	"context"
	"flag"
	"log"
	"net/http"

	"dominion/common/gopkg/bootstrap"
	phttp "dominion/common/gopkg/http"
	"dominion/common/gopkg/otel"
	"dominion/experimental/dsh/demo/fake-llm/service"
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
	mux.Handle("/v1/chat/completions", service.NewChatHandler(store))
	mux.HandleFunc("/health", service.HandleHealth)

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
