package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	pb "dominion/experimental/ts/grpc_hello_world"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	serviceAddr = "dns:///grpc-hello-world-ts-service:50051"
	defaultName = "World"
)

func main() {
	conn, err := grpc.NewClient(serviceAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	client := pb.NewGreeterClient(conn)

	http.HandleFunc("/say-hello", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if name == "" {
			name = defaultName
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		resp, err := client.SayHello(ctx, &pb.HelloRequest{Name: name})
		if err != nil {
			log.Printf("SayHello failed: %v", err)
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprintf(w, "gRPC error: %v", err)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(resp.GetMessage()))
	})

	log.Print("wrapper listening on :80")
	log.Fatal(http.ListenAndServe(":80", nil))
}
