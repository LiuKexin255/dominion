package mongo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strings"

	"dominion/pkg/solver"

	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	lookupEnv      = os.LookupEnv
	nonDNSLabel    = regexp.MustCompile(`[^a-z0-9-]+`)
	newMongoClient = func(uri string) (*mongodriver.Client, error) {
		return mongodriver.Connect(context.Background(), options.Client().ApplyURI(uri))
	}
	newResolver = func() (solver.Resolver, error) {
		return solver.NewK8sResolver()
	}
)

const (
	dominionEnvironmentEnvKey = "DOMINION_ENVIRONMENT"
)

// ClientOption configures NewClient.
type ClientOption func()

// NewClient creates a MongoDB client for the given dominion target.
func NewClient(rawTarget string, opts ...ClientOption) (*mongodriver.Client, error) {
	target, err := solver.ParseTarget(rawTarget)
	if err != nil {
		return nil, fmt.Errorf("invalid target %q: want app/name", rawTarget)
	}

	resolver, err := newResolver()
	if err != nil {
		return nil, err
	}

	addresses, err := resolver.Resolve(context.Background(), target)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("resolve mongo endpoint for target %q/%q: no ready endpoints found", target.App, target.Service)
	}
	address := addresses[0]

	uri := buildMongoURI(target, address)
	client, err := newMongoClient(uri)
	if err != nil {
		return nil, fmt.Errorf("create mongo client for %q: %w", rawTarget, err)
	}

	return client, nil
}

func buildMongoURI(target *solver.Target, address string) string {
	envName := lookupEnvOrDefault(dominionEnvironmentEnvKey, "default")
	password := generateStablePassword(target.App, envName, target.Service)

	return fmt.Sprintf(
		"mongodb://%s:%s@%s/%s?authSource=%s",
		defaultMongoUsername,
		password,
		address,
		defaultMongoAuthDatabase,
		defaultMongoAuthDatabase,
	)
}

func lookupEnvOrDefault(key, defaultValue string) string {
	if value, ok := lookupEnv(key); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return defaultValue
}

func newObjectName(kind string, app string, dominionApp string, serviceName string, environmentName string) string {
	if kind == "" {
		kind = "unknown"
	}

	parts := []string{kind, environmentName, serviceName, shortNameHash(app, dominionApp)}
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		part = sanitizeNamePart(part)
		if part != "" {
			normalized = append(normalized, part)
		}
	}

	return strings.Join(normalized, "-")
}

func sanitizeNamePart(part string) string {
	part = strings.TrimSpace(strings.ToLower(part))
	part = nonDNSLabel.ReplaceAllString(part, "-")
	part = strings.Trim(part, "-")
	return part
}

func shortNameHash(app string, dominionApp string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(app) + "\x00" + strings.TrimSpace(dominionApp)))
	return hex.EncodeToString(sum[:4])
}
