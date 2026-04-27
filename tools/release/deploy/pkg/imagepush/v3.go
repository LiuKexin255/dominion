package imagepush

import (
	"context"
	"fmt"
)

const defaultRegistry = "registry.liukexin.com"

// Metadata holds the parsed image metadata from a v3 build target.
type Metadata struct {
	SchemaVersion string `json:"schema_version"`
	App           string `json:"app"`
	Service       string `json:"service"`
	Binary        string `json:"binary"`
	Entrypoint    string `json:"entrypoint"`
	ImageTarget   string `json:"image_target"`
	PushTarget    string `json:"push_target"`
	Repository    string `json:"repository"`
	Tag           string `json:"tag"`
}

// Validate checks whether the metadata matches the expected app, service, repository, tag, and has a non-empty push target.
func (m *Metadata) Validate(app, service string) error {
	if m.App != app {
		return fmt.Errorf("metadata app %q does not match expected app %q", m.App, app)
	}
	if m.Service != service {
		return fmt.Errorf("metadata service %q does not match expected service %q", m.Service, service)
	}
	wantRepository := defaultRegistry + "/" + app + "/" + service
	if m.Repository != wantRepository {
		return fmt.Errorf("metadata repository %q does not match expected %q", m.Repository, wantRepository)
	}
	if m.Tag != "latest" {
		return fmt.Errorf("metadata tag %q does not match expected %q", m.Tag, "latest")
	}
	if m.PushTarget == "" {
		return fmt.Errorf("metadata push target is empty")
	}
	return nil
}

// V3Runner executes build, push, and digest operations for v3 image targets.
type V3Runner interface {
	BuildAndReadMetadata(ctx context.Context, target string) (*Metadata, error)
	RunPush(ctx context.Context, pushTarget string) error
	ReadDigest(ctx context.Context, imageTarget string) (string, error)
}

// V3Resolver resolves v3 artifact targets to image results with validation.
type V3Resolver struct {
	runner V3Runner
}

// NewV3Resolver creates a V3Resolver backed by the provided V3Runner.
func NewV3Resolver(runner V3Runner) *V3Resolver {
	return &V3Resolver{runner: runner}
}

// Resolve resolves a v3 artifact target to an image repository and digest.
// It validates metadata fields, pushes the image, and reads the digest.
func (r *V3Resolver) Resolve(ctx context.Context, artifactTarget string, app string, service string) (*Result, error) {
	if r == nil {
		return nil, fmt.Errorf("v3 resolver is nil")
	}

	metadata, err := r.runner.BuildAndReadMetadata(ctx, artifactTarget)
	if err != nil {
		return nil, fmt.Errorf("build and read metadata: %w", err)
	}

	if err := metadata.Validate(app, service); err != nil {
		return nil, err
	}

	if err := r.runner.RunPush(ctx, metadata.PushTarget); err != nil {
		return nil, fmt.Errorf("run push: %w", err)
	}

	digest, err := r.runner.ReadDigest(ctx, metadata.ImageTarget)
	if err != nil {
		return nil, fmt.Errorf("read digest: %w", err)
	}

	return cloneResult(&Result{URL: metadata.Repository, Dest: digest}), nil
}
