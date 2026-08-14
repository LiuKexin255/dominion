package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"strings"

	"dominion/common/gopkg/bootstrap"
	"dominion/common/gopkg/config"
	pgrpc "dominion/common/gopkg/grpc"
	"dominion/common/gopkg/logs"
	"dominion/common/gopkg/logs/event"
	"dominion/common/gopkg/otel"
	"dominion/experimental/golang/grpc_hello_world"

	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var port = flag.String("port", "50051", "Port to listen on")

// configBlock and configEntry are the SDK addressing parameters for the
// greeting entry declared in service.yaml
// (specs/045-deploy-config/contracts/yaml-schema.md §1).
const (
	configBlock = "service_config"
	configEntry = "greeting"
)

// envGreetingSuffix is a user-supplied env var appended to the greeting; it is
// independent of the config mechanism (FR-016 specs/045-deploy-config/spec.md).
const envGreetingSuffix = "GREETING_SUFFIX"

// Greeting is the greeting template provided by the deploy config
// (block service_config, entry greeting).
type Greeting struct {
	Message string `yaml:"message"`
	Times   int    `yaml:"times"`
}

// defaultGreeting is the deep-merge baseline passed to config.Read; fields
// absent from the config keep these values
// (specs/045-deploy-config/contracts/sdk-go.md §1).
var defaultGreeting = Greeting{Message: "hello", Times: 1}

// greeterServer implements the Greeter service; greeting carries the
// config-merged values used to build responses.
type greeterServer struct {
	grpc_hello_world.UnimplementedGreeterServer
	greeting Greeting
}

func (s *greeterServer) GetHello(ctx context.Context, req *grpc_hello_world.HelloRequest) (*grpc_hello_world.Hello, error) {
	name := req.GetName()
	if name == "" {
		name = "world"
	}

	logs.Info(ctx, "handle GetHello", event.String("name", name))

	// The greeting repeats the configured message Times times so the response
	// exposes both the message and the times override; the optional user env
	// suffix is appended afterwards (FR-016 specs/045-deploy-config/spec.md).
	message := strings.TrimSpace(strings.Repeat(s.greeting.Message+" ", s.greeting.Times))
	if suffix := os.Getenv(envGreetingSuffix); suffix != "" {
		message += suffix
	}

	return &grpc_hello_world.Hello{Name: name, Message: message}, nil
}

func main() {
	flag.Parse()

	// The config is read once at startup. Deployments that do not select the
	// config block run without DOMINION_CONFIG_DIR; the SDK reports an error
	// and the service keeps the defaults so it stays deployable unchanged
	// (specs/045-deploy-config/quickstart.md 场景 4).
	greeting := defaultGreeting
	if cfg, err := config.Read(configBlock, configEntry, defaultGreeting); err != nil {
		logs.Warn(context.Background(), "config not applied, keep default greeting", event.Err(err))
	} else {
		greeting = cfg
	}

	listener, err := net.Listen("tcp", ":"+*port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpcgo.NewServer(pgrpc.ServiceDefault()...)

	grpc_hello_world.RegisterGreeterServer(grpcServer, &greeterServer{greeting: greeting})
	reflection.Register(grpcServer)

	log.Printf("gRPC hello world server listening: %s", *port)

	b := bootstrap.New()
	b.Register(otel.Component())
	b.Register(bootstrap.GRPCServer("grpc", grpcServer, listener))
	log.Fatal(b.Run(context.Background()))
}
