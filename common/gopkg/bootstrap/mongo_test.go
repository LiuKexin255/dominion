package bootstrap

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ---------------------------------------------------------------------------
// MongoClient tests
// ---------------------------------------------------------------------------

// TestMongoClient_Stage verifies that MongoClient returns StageClient.
func TestMongoClient_Stage(t *testing.T) {
	c := MongoClient("test-mongo", nil)

	if got := c.Stage(); got != StageClient {
		t.Fatalf("Stage() = %v, want %v", got, StageClient)
	}
}

// TestMongoClient_Start verifies that Start returns nil without doing any
// network work.
func TestMongoClient_Start(t *testing.T) {
	c := MongoClient("test-mongo", nil)

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
}

// TestMongoClient_Stop verifies that Stop calls Disconnect on the underlying
// *mongo.Client without error. It creates a client with a dummy URI — Connect
// is asynchronous so a valid client object is returned even without a running
// Mongo daemon.
func TestMongoClient_Stop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
	if err != nil {
		t.Fatalf("mongo.Connect: %v", err)
	}

	c := MongoClient("test-mongo", client)

	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
}

// TestMongoClient_Name verifies that Name returns the configured name.
func TestMongoClient_Name(t *testing.T) {
	c := MongoClient("my-mongo", nil)

	if got := c.Name(); got != "my-mongo" {
		t.Fatalf("Name() = %q, want %q", got, "my-mongo")
	}
}
