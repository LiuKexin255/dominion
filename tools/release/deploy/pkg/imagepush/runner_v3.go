package imagepush

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dominion/tools/release/deploy/pkg/workspace"
)

// V3BazelRunner executes Bazel commands for v3 image build, push, and digest operations.
type V3BazelRunner struct {
	workspaceRoot string
	exec          commandExecutor
}

// NewV3BazelRunner creates a production V3Runner backed by Bazel.
func NewV3BazelRunner() (*V3BazelRunner, error) {
	return &V3BazelRunner{
		workspaceRoot: workspace.MustRoot(),
		exec:          osCommandExecutor{},
	}, nil
}

func (r *V3BazelRunner) BuildAndReadMetadata(ctx context.Context, target string) (*Metadata, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, fmt.Errorf("target is empty")
	}
	if err := r.validate(); err != nil {
		return nil, err
	}

	if output, err := r.exec.CombinedOutput(ctx, r.workspaceRoot, bazelBinary, bazelBuild, bazelNoProgress, bazelOutputFilter, target); err != nil {
		return nil, bazelCommandError(bazelBuild, target, output, err)
	}

	output, err := r.exec.Output(ctx, r.workspaceRoot, bazelBinary, bazelQuery, bazelNoProgress, bazelOutputFilter, target, "--output=files")
	if err != nil {
		return nil, bazelCommandError(bazelQuery, target, output, err)
	}

	lines := splitNonEmptyLines(string(output))
	if len(lines) == 0 {
		return nil, fmt.Errorf("target %s did not expose a metadata file", target)
	}

	metadataPath := filepath.Join(r.workspaceRoot, filepath.FromSlash(lines[0]))
	raw, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("read metadata %s: %w", metadataPath, err)
	}

	metadata := new(Metadata)
	if err := json.Unmarshal(raw, metadata); err != nil {
		return nil, fmt.Errorf("parse metadata %s: %w", metadataPath, err)
	}
	return metadata, nil
}

func (r *V3BazelRunner) RunPush(ctx context.Context, pushTarget string) error {
	pushTarget = strings.TrimSpace(pushTarget)
	if pushTarget == "" {
		return fmt.Errorf("push target is empty")
	}
	if err := r.validate(); err != nil {
		return err
	}

	if output, err := r.exec.CombinedOutput(ctx, r.workspaceRoot, bazelBinary, bazelRun, bazelNoProgress, bazelOutputFilter, pushTarget); err != nil {
		return bazelCommandError(bazelRun, pushTarget, output, err)
	}
	return nil
}

func (r *V3BazelRunner) ReadDigest(ctx context.Context, imageTarget string) (string, error) {
	imageTarget = strings.TrimSpace(imageTarget)
	if imageTarget == "" {
		return "", fmt.Errorf("image target is empty")
	}
	if err := r.validate(); err != nil {
		return "", err
	}

	output, err := r.exec.Output(ctx, r.workspaceRoot, bazelBinary, bazelQuery, bazelNoProgress, bazelOutputFilter, imageTarget, "--output=files")
	if err != nil {
		return "", bazelCommandError(bazelQuery, imageTarget, output, err)
	}

	lines := splitNonEmptyLines(string(output))
	if len(lines) == 0 {
		return "", fmt.Errorf("image target %s did not expose an OCI layout directory", imageTarget)
	}

	indexPath := filepath.Join(r.workspaceRoot, filepath.FromSlash(lines[0]), imageIndexFileName)
	return readV3Digest(indexPath)
}

func (r *V3BazelRunner) validate() error {
	if r == nil {
		return fmt.Errorf("v3 bazel runner is nil")
	}
	if strings.TrimSpace(r.workspaceRoot) == "" {
		return fmt.Errorf("workspace root is empty")
	}
	if r.exec == nil {
		return fmt.Errorf("command executor is nil")
	}
	return nil
}

func readV3Digest(indexPath string) (string, error) {
	raw, err := os.ReadFile(indexPath)
	if err != nil {
		return "", fmt.Errorf("read image index %s: %w", indexPath, err)
	}

	index := new(struct {
		Manifests []struct {
			Digest string `json:"digest"`
		} `json:"manifests"`
	})
	if err := json.Unmarshal(raw, index); err != nil {
		return "", fmt.Errorf("parse image index %s: %w", indexPath, err)
	}
	if len(index.Manifests) == 0 {
		return "", fmt.Errorf("digest is empty")
	}

	digest := strings.TrimSpace(index.Manifests[0].Digest)
	if digest == "" {
		return "", fmt.Errorf("digest is empty")
	}
	return digest, nil
}
