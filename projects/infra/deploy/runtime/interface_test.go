package runtime

import (
	"context"
	"testing"

	"dominion/projects/infra/deploy/domain"
)

type fakeRuntime struct{}

func (*fakeRuntime) Apply(context.Context, *domain.Environment) error {
	return nil
}

func (*fakeRuntime) Delete(context.Context, domain.EnvironmentName) error {
	return nil
}

var _ EnvironmentRuntime = (*fakeRuntime)(nil)

func TestEnvironmentRuntimeCompileTime(t *testing.T) {}
