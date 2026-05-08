package bootstrap

import (
	"context"

	"go.mongodb.org/mongo-driver/mongo"
)

// mongoClientComponent adapts a *mongo.Client into a bootstrap Component.
type mongoClientComponent struct {
	name   string
	client *mongo.Client
}

// MongoClient returns a Component that wraps the given *mongo.Client.
// It is a StageClient component — Start is a no-op and Stop calls
// client.Disconnect(ctx).
func MongoClient(name string, client *mongo.Client) Component {
	return &mongoClientComponent{
		name:   name,
		client: client,
	}
}

// Name returns the component name.
func (c *mongoClientComponent) Name() string {
	return c.name
}

// Stage returns StageClient because a Mongo client connection belongs to the
// client lifecycle stage.
func (c *mongoClientComponent) Stage() Stage {
	return StageClient
}

// Start is a no-op for client connections (the connection is managed externally).
func (c *mongoClientComponent) Start(_ context.Context) error {
	return nil
}

// Stop disconnects the underlying mongo.Client.
func (c *mongoClientComponent) Stop(ctx context.Context) error {
	return c.client.Disconnect(ctx)
}
