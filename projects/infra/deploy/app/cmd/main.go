package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"dominion/common/gopkg/bootstrap"
	"dominion/common/gopkg/grpc"
	phttp "dominion/common/gopkg/http"
	"dominion/common/gopkg/mongo"
	"dominion/common/gopkg/otel"
	"dominion/projects/infra/deploy"
	"dominion/projects/infra/deploy/app"
	"dominion/projects/infra/deploy/domain"
	"dominion/projects/infra/deploy/runtime/k8s"
	"dominion/projects/infra/deploy/service"
	"dominion/projects/infra/deploy/storage"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
)

const (
	defaultHTTPListenAddr   = ":8081"
	deployMongoTarget       = "deploy/mongo"
	deployMongoDatabase     = "deploy"
	defaultShutdownDeadline = 5 * time.Second
)

var httpPort = flag.String("http-port", listenAddrFromEnv("HTTP_PORT", defaultHTTPListenAddr), "HTTP port or listen address")

func main() {
	flag.Parse()

	client, err := mongo.NewClient(deployMongoTarget, mongo.WithK8sResolver())
	if err != nil {
		log.Fatalf("create mongo client: %v", err)
	}

	repo, err := storage.NewMongoRepository(client.Database(deployMongoDatabase))
	if err != nil {
		log.Fatalf("create deploy repository: %v", err)
	}

	runtimeClient, err := k8s.NewRuntimeClient()
	if err != nil {
		log.Fatalf("create deploy runtime client: %v", err)
	}
	runtimeImpl := k8s.NewK8sRuntime(runtimeClient)

	// Create queue, command service, and handler.
	queue := domain.NewQueue()
	cmdSvc := service.NewEnvironmentCommandService(repo, queue, runtimeImpl)
	handler := deploy.NewHandler(repo, runtimeImpl, cmdSvc)

	// Server component.
	httpMux := runtime.NewServeMux(grpc.GatewayDefault()...)
	deploy.RegisterDeployServiceHandlerServer(context.Background(), httpMux, handler)
	httpServer := &http.Server{Addr: normalizeListenAddr(*httpPort), Handler: phttp.Handler(httpMux, "deploy-http")}
	server := bootstrap.HTTPServer("deploy-http", httpServer)

	// Daemon component.
	workerBuilder := &app.DeployWorkerBuilder{
		Repo:    repo,
		Runtime: runtimeImpl,
		Queue:   queue,
	}
	daemon := bootstrap.Daemon("deploy-worker", workerBuilder,
		bootstrap.WithDaemonErrorClassifier(workerBuilder.ClassifyError),
	)

	bs := bootstrap.New(bootstrap.WithShutdownTimeout(defaultShutdownDeadline))
	bs.Register(bootstrap.MongoClient("mongo", client))
	bs.Register(otel.Component())
	bs.Register(server)
	bs.Register(daemon)
	if err := bs.Run(context.Background()); err != nil {
		log.Fatalf("run deploy: %v", err)
	}
}

func listenAddrFromEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return normalizeListenAddr(value)
	}

	return fallback
}

func normalizeListenAddr(value string) string {
	if strings.HasPrefix(value, ":") {
		return value
	}

	return ":" + value
}
