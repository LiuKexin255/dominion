package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dominion/projects/infra/deploy/domain"
)

func TestNewFakeRuntime_DefaultBehavior(t *testing.T) {
	fr := NewFakeRuntime()
	env := mustFakeEnvironment(t)
	name := env.Name()

	if err := fr.Apply(context.Background(), env, nil); err != nil {
		t.Fatalf("Apply() unexpected error: %v", err)
	}
	if err := fr.Delete(context.Background(), name); err != nil {
		t.Fatalf("Delete() unexpected error: %v", err)
	}

	if fr.ApplyCalled != 1 {
		t.Fatalf("ApplyCalled = %d, want 1", fr.ApplyCalled)
	}
	if fr.DeleteCalled != 1 {
		t.Fatalf("DeleteCalled = %d, want 1", fr.DeleteCalled)
	}
	if len(fr.AppliedEnvs) != 1 {
		t.Fatalf("len(AppliedEnvs) = %d, want 1", len(fr.AppliedEnvs))
	}
	if len(fr.DeletedEnvs) != 1 {
		t.Fatalf("len(DeletedEnvs) = %d, want 1", len(fr.DeletedEnvs))
	}
	if got := fr.AppliedEnvs[0].Name().String(); got != name.String() {
		t.Fatalf("AppliedEnvs[0].Name() = %q, want %q", got, name.String())
	}
	if got := fr.DeletedEnvs[0]; got != name {
		t.Fatalf("DeletedEnvs[0] = %#v, want %#v", got, name)
	}
}

func TestFakeRuntime_CustomFuncsAndDynamicErrors(t *testing.T) {
	fr := NewFakeRuntime()
	fr.ApplyFunc = func(context.Context, *domain.Environment, func(string)) error { return errors.New("apply func") }
	fr.DeleteFunc = func(context.Context, domain.EnvironmentName) error { return errors.New("delete func") }
	env := mustFakeEnvironment(t)
	name := env.Name()

	if err := fr.Apply(context.Background(), env, nil); err == nil || err.Error() != "apply func" {
		t.Fatalf("Apply() error = %v, want apply func", err)
	}
	if err := fr.Delete(context.Background(), name); err == nil || err.Error() != "delete func" {
		t.Fatalf("Delete() error = %v, want delete func", err)
	}

	fr.SetApplyError(errors.New("apply boom"))
	fr.SetDeleteError(errors.New("delete boom"))
	if err := fr.Apply(context.Background(), env, nil); err == nil || err.Error() != "apply boom" {
		t.Fatalf("Apply() error = %v, want apply boom", err)
	}
	if err := fr.Delete(context.Background(), name); err == nil || err.Error() != "delete boom" {
		t.Fatalf("Delete() error = %v, want delete boom", err)
	}

	fr.SetApplyError(nil)
	fr.SetDeleteError(nil)
	if err := fr.Apply(context.Background(), env, nil); err == nil || err.Error() != "apply func" {
		t.Fatalf("Apply() after restore error = %v, want apply func", err)
	}
	if err := fr.Delete(context.Background(), name); err == nil || err.Error() != "delete func" {
		t.Fatalf("Delete() after restore error = %v, want delete func", err)
	}
}

func TestFakeRuntime_AdminHandler(t *testing.T) {
	fr := NewFakeRuntime()
	handler := fr.AdminHandler()
	env := mustFakeEnvironment(t)
	name := env.Name()

	record := func(method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr
	}

	if rr := record(http.MethodPost, "/admin/fake/apply-error", "apply via admin"); rr.Code != http.StatusOK {
		t.Fatalf("POST apply-error status = %d, want %d", rr.Code, http.StatusOK)
	}
	if err := fr.Apply(context.Background(), env, nil); err == nil || err.Error() != "apply via admin" {
		t.Fatalf("Apply() error = %v, want apply via admin", err)
	}

	getApplyRR := record(http.MethodGet, "/admin/fake/apply-error", "")
	if getApplyRR.Code != http.StatusOK {
		t.Fatalf("GET apply-error status = %d, want %d", getApplyRR.Code, http.StatusOK)
	}
	if got := readAdminError(t, getApplyRR.Body.String()); got != "apply via admin" {
		t.Fatalf("apply-error = %q, want %q", got, "apply via admin")
	}

	if rr := record(http.MethodPost, "/admin/fake/delete-error", "delete via admin"); rr.Code != http.StatusOK {
		t.Fatalf("POST delete-error status = %d, want %d", rr.Code, http.StatusOK)
	}
	if err := fr.Delete(context.Background(), name); err == nil || err.Error() != "delete via admin" {
		t.Fatalf("Delete() error = %v, want delete via admin", err)
	}

	if rr := record(http.MethodPost, "/admin/fake/restore", ""); rr.Code != http.StatusOK {
		t.Fatalf("POST restore status = %d, want %d", rr.Code, http.StatusOK)
	}
	if err := fr.Apply(context.Background(), env, nil); err != nil {
		t.Fatalf("Apply() after restore unexpected error: %v", err)
	}
	if err := fr.Delete(context.Background(), name); err != nil {
		t.Fatalf("Delete() after restore unexpected error: %v", err)
	}

	statsRR := record(http.MethodGet, "/admin/fake/stats", "")
	if statsRR.Code != http.StatusOK {
		t.Fatalf("GET stats status = %d, want %d", statsRR.Code, http.StatusOK)
	}
	var stats fakeStats
	if err := json.Unmarshal(statsRR.Body.Bytes(), &stats); err != nil {
		t.Fatalf("unmarshal stats: %v", err)
	}
	if stats.ApplyCalled != 2 {
		t.Fatalf("ApplyCalled = %d, want 2", stats.ApplyCalled)
	}
	if stats.DeleteCalled != 2 {
		t.Fatalf("DeleteCalled = %d, want 2", stats.DeleteCalled)
	}
	if len(stats.AppliedEnvs) != 2 {
		t.Fatalf("len(AppliedEnvs) = %d, want 2", len(stats.AppliedEnvs))
	}
	if len(stats.DeletedEnvs) != 2 {
		t.Fatalf("len(DeletedEnvs) = %d, want 2", len(stats.DeletedEnvs))
	}
	if stats.ApplyError != "" || stats.DeleteError != "" {
		t.Fatalf("stats errors = (%q, %q), want empty", stats.ApplyError, stats.DeleteError)
	}
}

func mustFakeEnvironment(t *testing.T) *domain.Environment {
	t.Helper()

	name, err := domain.NewEnvironmentName("scope1", "dev")
	if err != nil {
		t.Fatalf("NewEnvironmentName() unexpected error: %v", err)
	}
	env, err := domain.NewEnvironment(name, "demo", &domain.DesiredState{})
	if err != nil {
		t.Fatalf("NewEnvironment() unexpected error: %v", err)
	}
	return env
}

func readAdminError(t *testing.T, body string) string {
	t.Helper()

	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("unmarshal admin error: %v", err)
	}
	return payload.Error
}
