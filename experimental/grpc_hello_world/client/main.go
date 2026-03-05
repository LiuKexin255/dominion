package main

import (
	"context"
	"flag"
	"log"
	"time"

	"dominion/experimental/grpc_hello_world"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	addr = flag.String("addr", "localhost:50051", "gRPC server address")
	name = flag.String("name", "world", "Name to greet")
)

func main() {
	flag.Parse()

	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("failed to connect to %s: %v", *addr, err)
	}
	defer conn.Close()

	client := grpc_hello_world.NewGreeterClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := client.SayHello(ctx, &grpc_hello_world.HelloRequest{Name: *name})
	if err != nil {
		log.Fatalf("SayHello failed: %v", err)
	}

	log.Printf("server reply: %s", resp.GetMessage())
}
