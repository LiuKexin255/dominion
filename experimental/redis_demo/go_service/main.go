package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"

	"dominion/common/gopkg/bootstrap"
	pgrpc "dominion/common/gopkg/grpc"
	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	"dominion/common/gopkg/otel"
	"dominion/common/gopkg/redis"
	"dominion/experimental/redis_demo"

	goredis "github.com/redis/go-redis/v9"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	// redisTarget names the redis instance declared in the testplan deploy.yaml;
	// common/gopkg/redis resolves the endpoint and derives the password
	// (specs/067-redis-infra-support/contracts/redis-demo.md §1).
	redisTarget = "redis-demo/redis"

	logFieldKvEntryName = "kv_entry_name"
)

var port = flag.String("port", "50051", "Port to listen on")

// kvServer implements redis_demo.KvServiceServer as a direct pass-through to
// redis. The resource name is the redis key verbatim, which is the mapping the
// TypeScript service uses as well (contracts/redis-demo.md §2).
type kvServer struct {
	redis_demo.UnimplementedKvServiceServer
	client *goredis.Client
}

// GetKvEntry reads the entry and maps the driver's missing-key sentinel to
// NOT_FOUND (AIP-131).
func (s *kvServer) GetKvEntry(ctx context.Context, req *redis_demo.GetKvEntryRequest) (*redis_demo.KvEntry, error) {
	name := req.GetName()

	value, err := s.client.Get(ctx, name).Result()
	if errors.Is(err, goredis.Nil) {
		return nil, status.Errorf(codes.NotFound, "kv entry %q not found", name)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get kv entry %q: %v", name, err)
	}
	logs.Info(ctx, "get kv entry", event.String(logFieldKvEntryName, name))

	return &redis_demo.KvEntry{Name: name, Value: value}, nil
}

// SetKvEntry upserts the entry (custom method :set, AIP-136).
func (s *kvServer) SetKvEntry(ctx context.Context, req *redis_demo.SetKvEntryRequest) (*redis_demo.KvEntry, error) {
	name := req.GetName()

	if err := s.client.Set(ctx, name, req.GetValue(), 0).Err(); err != nil {
		return nil, status.Errorf(codes.Internal, "set kv entry %q: %v", name, err)
	}
	logs.Info(ctx, "set kv entry", event.String(logFieldKvEntryName, name))

	return &redis_demo.KvEntry{Name: name, Value: req.GetValue()}, nil
}

// DeleteKvEntry reports NOT_FOUND when nothing was deleted (AIP-135).
func (s *kvServer) DeleteKvEntry(ctx context.Context, req *redis_demo.DeleteKvEntryRequest) (*emptypb.Empty, error) {
	name := req.GetName()

	deleted, err := s.client.Del(ctx, name).Result()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "delete kv entry %q: %v", name, err)
	}
	if deleted == 0 {
		return nil, status.Errorf(codes.NotFound, "kv entry %q not found", name)
	}
	logs.Info(ctx, "delete kv entry", event.String(logFieldKvEntryName, name))

	return new(emptypb.Empty), nil
}

// redisClientComponent closes the redis client on shutdown. Start is a no-op
// because go-redis dials lazily, matching common/gopkg/mongo
// (specs/067-redis-infra-support/research.md D7/D10).
type redisClientComponent struct {
	client *goredis.Client
}

// Name returns the component name.
func (c *redisClientComponent) Name() string {
	return "redis"
}

// Stage returns StageClient: the connection is torn down after the server
// stage, so in-flight requests keep their client until they finish.
func (c *redisClientComponent) Stage() bootstrap.Stage {
	return bootstrap.StageClient
}

// Start is a no-op for lazy clients.
func (c *redisClientComponent) Start(_ context.Context) error {
	return nil
}

// Stop closes the underlying redis client.
func (c *redisClientComponent) Stop(_ context.Context) error {
	return c.client.Close()
}

func main() {
	flag.Parse()

	client, err := redis.NewClient(redisTarget)
	if err != nil {
		log.Fatalf("failed to create redis client: %v", err)
	}

	listener, err := net.Listen("tcp", ":"+*port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpcgo.NewServer(pgrpc.ServiceDefault()...)
	redis_demo.RegisterKvServiceServer(grpcServer, &kvServer{client: client})
	reflection.Register(grpcServer)

	log.Printf("redis-demo go service listening: %s", *port)

	b := bootstrap.New()
	b.Register(otel.Component())
	b.Register(&redisClientComponent{client: client})
	b.Register(bootstrap.GRPCServer("grpc", grpcServer, listener))
	log.Fatal(b.Run(context.Background()))
}
