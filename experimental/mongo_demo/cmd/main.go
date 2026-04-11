package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"

	mongodemo "dominion/experimental/mongo_demo"
	"dominion/experimental/mongo_demo/storage"
	pgrpc "dominion/pkg/grpc"
	mongo "dominion/pkg/mongo"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	grpcgo "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	reflection "google.golang.org/grpc/reflection"
	status "google.golang.org/grpc/status"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
)

const (
	// mongoTarget is the dominion target used to resolve the MongoDB endpoint.
	mongoTarget = "mongo-demo/mongo"
)

var (
	// grpcPort is the listening port for the Mongo demo gRPC service.
	grpcPort = flag.String("grpc-port", "50051", "gRPC port to listen on")
	// httpPort is the listening port for the Mongo demo HTTP gateway.
	httpPort = flag.String("http-port", "80", "HTTP port to listen on")
)

// mongoDemoServer implements mongodemo.MongoDemoServiceServer.
type mongoDemoServer struct {
	mongodemo.UnimplementedMongoDemoServiceServer
	store storage.MongoRecordStore
}

// GetMongoRecord returns a Mongo record by resource name.
func (s *mongoDemoServer) GetMongoRecord(ctx context.Context, req *mongodemo.GetMongoRecordRequest) (*mongodemo.MongoRecord, error) {
	record, err := s.store.Get(ctx, req.GetName())
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "mongo record %q not found", req.GetName())
		}
		return nil, status.Errorf(codes.Internal, "get mongo record %q: %v", req.GetName(), err)
	}

	return record, nil
}

// ListMongoRecords returns Mongo records under the given parent.
func (s *mongoDemoServer) ListMongoRecords(ctx context.Context, req *mongodemo.ListMongoRecordsRequest) (*mongodemo.ListMongoRecordsResponse, error) {
	records, nextPageToken, err := s.store.List(ctx, req.GetParent(), req.GetPageSize(), req.GetPageToken(), req.GetShowArchived())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list mongo records for %q: %v", req.GetParent(), err)
	}

	return &mongodemo.ListMongoRecordsResponse{MongoRecords: records, NextPageToken: nextPageToken}, nil
}

// CreateMongoRecord creates a Mongo record under the given parent.
func (s *mongoDemoServer) CreateMongoRecord(ctx context.Context, req *mongodemo.CreateMongoRecordRequest) (*mongodemo.MongoRecord, error) {
	record := req.GetMongoRecord()
	if record == nil {
		return nil, status.Errorf(codes.InvalidArgument, "mongo record is empty")
	}

	createdRecord, err := s.store.Create(ctx, record)
	if err != nil {
		if errors.Is(err, storage.ErrAlreadyExists) {
			return nil, status.Errorf(codes.AlreadyExists, "mongo record %q already exists", record.GetName())
		}
		return nil, status.Errorf(codes.Internal, "create mongo record %q: %v", req.GetMongoRecordId(), err)
	}

	return createdRecord, nil
}

// UpdateMongoRecord updates selected fields on a Mongo record.
func (s *mongoDemoServer) UpdateMongoRecord(ctx context.Context, req *mongodemo.UpdateMongoRecordRequest) (*mongodemo.MongoRecord, error) {
	if len(req.GetUpdateMask().GetPaths()) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "update_mask paths cannot be empty")
	}
	if req.GetMongoRecord() == nil {
		return nil, status.Errorf(codes.InvalidArgument, "mongo_record is required")
	}

	record, err := s.store.Update(ctx, req.GetMongoRecord(), req.GetUpdateMask())
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "mongo record %q not found", req.GetMongoRecord().GetName())
		}
		return nil, status.Errorf(codes.Internal, "update mongo record %q: %v", req.GetMongoRecord().GetName(), err)
	}

	return record, nil
}

// DeleteMongoRecord deletes a Mongo record by resource name.
func (s *mongoDemoServer) DeleteMongoRecord(ctx context.Context, req *mongodemo.DeleteMongoRecordRequest) (*emptypb.Empty, error) {
	err := s.store.Delete(ctx, req.GetName())
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "mongo record %q not found", req.GetName())
		}
		return nil, status.Errorf(codes.Internal, "delete mongo record %q: %v", req.GetName(), err)
	}

	return new(emptypb.Empty), nil
}

func main() {
	flag.Parse()

	client, err := mongo.NewClient(mongoTarget)
	if err != nil {
		log.Fatalf("failed to create mongo client: %v", err)
	}

	server := &mongoDemoServer{store: storage.NewMongoStore(client)}

	grpcListener, err := net.Listen("tcp", ":"+*grpcPort)
	if err != nil {
		log.Fatalf("failed to listen on gRPC port: %v", err)
	}

	grpcServer := grpcgo.NewServer(pgrpc.ServiceDefault()...)
	mongodemo.RegisterMongoDemoServiceServer(grpcServer, server)
	reflection.Register(grpcServer)

	mux := runtime.NewServeMux()
	if err := mongodemo.RegisterMongoDemoServiceHandlerServer(context.Background(), mux, server); err != nil {
		log.Fatalf("failed to register gateway handler: %v", err)
	}

	errCh := make(chan error, 2)
	go func() {
		log.Printf("mongo demo gRPC service listening: %s", *grpcPort)
		errCh <- grpcServer.Serve(grpcListener)
	}()

	go func() {
		log.Printf("mongo demo gateway listening :%s", *httpPort)
		errCh <- http.ListenAndServe(":"+*httpPort, mux)
	}()

	if err := <-errCh; err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
