package imagepush

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

type stubV3Runner struct {
	metadata  *Metadata
	metaErr   error
	pushErr   error
	digest    string
	digestErr error

	buildCalls int
	pushCalls  int
	readCalls  int
}

func (r *stubV3Runner) BuildAndReadMetadata(_ context.Context, _ string) (*Metadata, error) {
	r.buildCalls++
	return r.metadata, r.metaErr
}

func (r *stubV3Runner) RunPush(_ context.Context, _ string) error {
	r.pushCalls++
	return r.pushErr
}

func (r *stubV3Runner) ReadDigest(_ context.Context, _ string) (string, error) {
	r.readCalls++
	return r.digest, r.digestErr
}

func validV3Metadata() *Metadata {
	return &Metadata{
		SchemaVersion: "3",
		App:           "myapp",
		Service:       "myservice",
		Binary:        "mybinary",
		Entrypoint:    "/app/main",
		ImageTarget:   "//pkg:my_image",
		PushTarget:    "//pkg:my_image_push",
		Repository:    "registry.liukexin.com/myapp/myservice",
		Tag:           "latest",
	}
}

func TestMetadata_JSONParsing(t *testing.T) {
	raw := `{
		"schema_version": "3",
		"app": "myapp",
		"service": "myservice",
		"binary": "mybinary",
		"entrypoint": "/app/main",
		"image_target": "//pkg:my_image",
		"push_target": "//pkg:my_image_push",
		"repository": "registry.liukexin.com/myapp/myservice",
		"tag": "latest"
	}`

	var m Metadata
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("json.Unmarshal() failed: %v", err)
	}

	want := Metadata{
		SchemaVersion: "3",
		App:           "myapp",
		Service:       "myservice",
		Binary:        "mybinary",
		Entrypoint:    "/app/main",
		ImageTarget:   "//pkg:my_image",
		PushTarget:    "//pkg:my_image_push",
		Repository:    "registry.liukexin.com/myapp/myservice",
		Tag:           "latest",
	}

	if !reflect.DeepEqual(m, want) {
		t.Fatalf("Metadata = %+v, want %+v", m, want)
	}
}

func TestV3Resolver_ResolveSuccess(t *testing.T) {
	runner := &stubV3Runner{
		metadata: validV3Metadata(),
		digest:   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}

	resolver := NewV3Resolver(runner)
	result, err := resolver.Resolve(context.Background(), "//pkg:my_target", "myapp", "myservice")
	if err != nil {
		t.Fatalf("Resolve() failed: %v", err)
	}

	want := &Result{
		URL:  "registry.liukexin.com/myapp/myservice",
		Dest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	if !reflect.DeepEqual(result, want) {
		t.Fatalf("Resolve() = %+v, want %+v", result, want)
	}
}

func TestV3Resolver_AppMismatch(t *testing.T) {
	runner := &stubV3Runner{
		metadata: validV3Metadata(),
		digest:   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}

	resolver := NewV3Resolver(runner)
	_, err := resolver.Resolve(context.Background(), "//pkg:my_target", "wrongapp", "myservice")
	if err == nil {
		t.Fatal("Resolve() succeeded unexpectedly")
	}
	if !contains(err.Error(), "app") {
		t.Fatalf("Resolve() err = %v, want error containing 'app'", err)
	}
}

func TestV3Resolver_ServiceMismatch(t *testing.T) {
	runner := &stubV3Runner{
		metadata: validV3Metadata(),
		digest:   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}

	resolver := NewV3Resolver(runner)
	_, err := resolver.Resolve(context.Background(), "//pkg:my_target", "myapp", "wrongservice")
	if err == nil {
		t.Fatal("Resolve() succeeded unexpectedly")
	}
	if !contains(err.Error(), "service") {
		t.Fatalf("Resolve() err = %v, want error containing 'service'", err)
	}
}

func TestV3Resolver_RepositoryMismatch(t *testing.T) {
	md := validV3Metadata()
	md.Repository = "registry.liukexin.com/wrong/svc"
	runner := &stubV3Runner{
		metadata: md,
		digest:   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}

	resolver := NewV3Resolver(runner)
	_, err := resolver.Resolve(context.Background(), "//pkg:my_target", "myapp", "myservice")
	if err == nil {
		t.Fatal("Resolve() succeeded unexpectedly")
	}
	if !contains(err.Error(), "repository") {
		t.Fatalf("Resolve() err = %v, want error containing 'repository'", err)
	}
}

func TestV3Resolver_TagMismatch(t *testing.T) {
	md := validV3Metadata()
	md.Tag = "v1.0.0"
	runner := &stubV3Runner{
		metadata: md,
		digest:   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}

	resolver := NewV3Resolver(runner)
	_, err := resolver.Resolve(context.Background(), "//pkg:my_target", "myapp", "myservice")
	if err == nil {
		t.Fatal("Resolve() succeeded unexpectedly")
	}
	if !contains(err.Error(), "tag") {
		t.Fatalf("Resolve() err = %v, want error containing 'tag'", err)
	}
}

func TestV3Resolver_EmptyPushTarget(t *testing.T) {
	md := validV3Metadata()
	md.PushTarget = ""
	runner := &stubV3Runner{
		metadata: md,
		digest:   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}

	resolver := NewV3Resolver(runner)
	_, err := resolver.Resolve(context.Background(), "//pkg:my_target", "myapp", "myservice")
	if err == nil {
		t.Fatal("Resolve() succeeded unexpectedly")
	}
	if !contains(err.Error(), "push target") {
		t.Fatalf("Resolve() err = %v, want error containing 'push target'", err)
	}
}

func TestV3Resolver_NoCache(t *testing.T) {
	runner := &stubV3Runner{
		metadata: validV3Metadata(),
		digest:   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}

	resolver := NewV3Resolver(runner)

	first, err := resolver.Resolve(context.Background(), "//pkg:my_target", "myapp", "myservice")
	if err != nil {
		t.Fatalf("Resolve() first call failed: %v", err)
	}
	second, err := resolver.Resolve(context.Background(), "//pkg:my_target", "myapp", "myservice")
	if err != nil {
		t.Fatalf("Resolve() second call failed: %v", err)
	}

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("Resolve() result mismatch: first=%v second=%v", first, second)
	}
	if runner.buildCalls != 2 {
		t.Fatalf("BuildAndReadMetadata called %d times, want 2", runner.buildCalls)
	}
	if runner.pushCalls != 2 {
		t.Fatalf("RunPush called %d times, want 2", runner.pushCalls)
	}
	if runner.readCalls != 2 {
		t.Fatalf("ReadDigest called %d times, want 2", runner.readCalls)
	}
}

func TestV3Resolver_PushError(t *testing.T) {
	runner := &stubV3Runner{
		metadata: validV3Metadata(),
		pushErr:  errors.New("push failed"),
		digest:   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}

	resolver := NewV3Resolver(runner)
	_, err := resolver.Resolve(context.Background(), "//pkg:my_target", "myapp", "myservice")
	if err == nil {
		t.Fatal("Resolve() succeeded unexpectedly")
	}
	if !contains(err.Error(), "push failed") {
		t.Fatalf("Resolve() err = %v, want error containing 'push failed'", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
