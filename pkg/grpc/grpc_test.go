package grpc

import (
	"testing"

	"dominion/pkg/grpc/solver"
	grpcgo "google.golang.org/grpc"
	grpcresolver "google.golang.org/grpc/resolver"
)

func TestServiceDefault(t *testing.T) {
	// when
	got := ServiceDefault()

	// then
	if got != nil {
		t.Fatalf("ServiceDefault() = %#v, want nil", got)
	}

	server := grpcgo.NewServer(ServiceDefault()...)
	if server == nil {
		t.Fatal("grpc.NewServer(ServiceDefault()...) = nil, want server")
	}
}

func TestClientDefault(t *testing.T) {
	if got := grpcresolver.Get(solver.Scheme); got != nil {
		t.Fatalf("resolver.Get(%q) before ClientDefault() = %#v, want nil", solver.Scheme, got)
	}

	// when
	got := ClientDefault()

	// then
	if len(got) != len(DefaultDialOptions()) {
		t.Fatalf("len(ClientDefault()) = %d, want %d", len(got), len(DefaultDialOptions()))
	}
	if grpcresolver.Get(solver.Scheme) == nil {
		t.Fatalf("resolver.Get(%q) = nil, want registered builder", solver.Scheme)
	}

	if conn, err := grpcgo.NewClient("dominion:///app/service:50051", got...); err != nil {
		t.Fatalf("grpc.NewClient with ClientDefault() unexpected error: %v", err)
	} else if conn == nil {
		t.Fatal("grpc.NewClient with ClientDefault() = nil, want conn")
	}
}
