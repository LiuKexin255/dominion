package imagepush

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

var digestPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+._-]*:[A-Fa-f0-9]{32,}$`)

// Result describes a resolved image repository and digest.
type Result struct {
	URL  string
	Dest string
}

// ImageRef returns the canonical image reference in digest form: url@digest.
func (r Result) ImageRef() (string, error) {
	url := strings.TrimSpace(r.URL)
	if url == "" {
		return "", fmt.Errorf("image url is empty")
	}
	dest := strings.TrimSpace(r.Dest)
	if dest == "" {
		return "", fmt.Errorf("image digest is empty")
	}
	if !digestPattern.MatchString(dest) {
		return "", fmt.Errorf("image digest is invalid: %s", dest)
	}

	return url + "@" + dest, nil
}

// PushOutput captures the subset of bazel push output needed by the resolver.
type PushOutput struct {
	Repository     string
	RepositoryFile string
	Digest         string
}

// Runner executes the push target and returns the registry output.
type Runner interface {
	Run(ctx context.Context, pushTarget string) (*PushOutput, error)
}

// Resolver resolves artifact targets to image results and caches them in memory.
type Resolver struct {
	runner Runner
	mu     sync.Mutex
	cache  map[string]*Result
}

// NewResolver creates a Resolver backed by the provided Runner.
func NewResolver(runner Runner) *Resolver {
	return &Resolver{
		runner: runner,
		cache:  make(map[string]*Result),
	}
}

// Resolve resolves an artifact target to an image repository and digest.
func (r *Resolver) Resolve(ctx context.Context, artifactTarget string) (*Result, error) {
	if r == nil {
		return nil, fmt.Errorf("resolver is nil")
	}
	pushTarget, err := DerivePushTarget(artifactTarget)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	if cached, ok := r.cache[pushTarget]; ok {
		result := *cached
		r.mu.Unlock()
		return &result, nil
	}
	r.mu.Unlock()

	if r.runner == nil {
		return nil, fmt.Errorf("runner is nil")
	}
	output, err := r.runner.Run(ctx, pushTarget)
	if err != nil {
		return nil, err
	}
	result, err := resultFromOutput(output)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	if cached, ok := r.cache[pushTarget]; ok {
		result = cached
	} else {
		copyResult := *result
		r.cache[pushTarget] = &copyResult
		result = &copyResult
	}
	r.mu.Unlock()

	return cloneResult(result), nil
}

// DerivePushTarget converts a normalized artifact target into its push target.
func DerivePushTarget(artifactTarget string) (string, error) {
	target := strings.TrimSpace(artifactTarget)
	if target == "" {
		return "", fmt.Errorf("artifact target is empty")
	}
	if !strings.HasPrefix(target, "//") {
		return "", fmt.Errorf("artifact target must start with //: %s", target)
	}
	idx := strings.LastIndex(target, ":")
	if idx < 2 || idx == len(target)-1 {
		return "", fmt.Errorf("artifact target is missing label: %s", target)
	}

	return target + "_push", nil
}

func resultFromOutput(output *PushOutput) (*Result, error) {
	if output == nil {
		return nil, fmt.Errorf("push output is nil")
	}
	repository := strings.TrimSpace(output.Repository)
	if repository == "" {
		repository = strings.TrimSpace(output.RepositoryFile)
	}
	if repository == "" {
		return nil, fmt.Errorf("repository is empty")
	}
	digest := strings.TrimSpace(output.Digest)
	if digest == "" {
		return nil, fmt.Errorf("digest is empty")
	}
	if !digestPattern.MatchString(digest) {
		return nil, fmt.Errorf("digest is invalid: %s", digest)
	}

	return &Result{URL: repository, Dest: digest}, nil
}

func cloneResult(result *Result) *Result {
	if result == nil {
		return nil
	}
	copyResult := *result
	return &copyResult
}
