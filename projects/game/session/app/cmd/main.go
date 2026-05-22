package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"dominion/common/gopkg/bootstrap"
	pgrpc "dominion/common/gopkg/grpc"
	"dominion/common/gopkg/mongo"
	"dominion/common/gopkg/otel"
	"dominion/projects/game/pkg/const"
	"dominion/projects/game/session"
	"dominion/projects/game/session/runtime/runtimeclient"
	"dominion/projects/game/session/runtime/storage"
	"dominion/projects/game/session/service"

	"google.golang.org/grpc"
)

const (
	envGRPCPort = "GRPC_PORT"

	defaultGRPCListenAddr   = ":9081"
	defaultMongoDatabase    = "game"
	defaultShutdownDeadline = 5 * time.Second
)

var grpcPort = flag.String("grpc-port", envOrDefault(envGRPCPort, defaultGRPCListenAddr), "gRPC listen address")

func main() {
	flag.Parse()

	grpcAddr := normalizeListenAddr(*grpcPort)

	// Create Mongo client and repo.
	client, err := mongo.NewClient(gameconst.TargetMongo)
	if err != nil {
		log.Fatalf("create mongo client: %v", err)
	}

	ctx := context.Background()
	repo, err := storage.NewMongoRepository(ctx, client.Database(defaultMongoDatabase))
	if err != nil {
		log.Fatalf("create session repository: %v", err)
	}

	// Create gRPC runtime client.
	runtimeClient, err := runtimeclient.NewGRPCRuntimeClient(ctx, gameconst.TargetRuntimeGRPC)
	if err != nil {
		log.Fatalf("create grpc runtime client: %v", err)
	}

	// Create handler.
	svc := service.NewSessionService(repo, runtimeClient)
	handler := session.NewHandler(svc)

	// Create gRPC server.
	grpcServer := grpc.NewServer(pgrpc.ServiceDefault()...)
	session.RegisterSessionServiceServer(grpcServer, handler)

	// Create gRPC listener.
	grpcListener, err := netListen(grpcAddr)
	if err != nil {
		log.Fatalf("create gRPC listener: %v", err)
	}

	bs := bootstrap.New(bootstrap.WithShutdownTimeout(defaultShutdownDeadline))
	bs.Register(otel.Component(otel.WithLoggerName("dominion/projects/game/session")))
	bs.Register(bootstrap.MongoClient("mongo", client))
	bs.Register(bootstrap.GRPCConn("runtime-client", runtimeClient.Conn()))
	bs.Register(bootstrap.GRPCServer("session-grpc", grpcServer, grpcListener))
	if err := bs.Run(context.Background()); err != nil {
		log.Fatalf("run session: %v", err)
	}
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}

	return fallback
}

func normalizeListenAddr(value string) string {
	if strings.HasPrefix(value, ":") {
		return value
	}

	return ":" + value
}

func netListen(addr string) (net.Listener, error) {
	return (&net.ListenConfig{}).Listen(context.Background(), "tcp", addr)
}
