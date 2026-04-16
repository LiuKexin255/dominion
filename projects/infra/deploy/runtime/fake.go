package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"

	"dominion/projects/infra/deploy/domain"
)

// FakeRuntime is a test double for domain.EnvironmentRuntime.
type FakeRuntime struct {
	mu sync.RWMutex

	ApplyCalled  int
	DeleteCalled int
	AppliedEnvs  []*domain.Environment
	DeletedEnvs  []domain.EnvironmentName

	ApplyFunc  func(context.Context, *domain.Environment, func(string)) error
	DeleteFunc func(context.Context, domain.EnvironmentName) error

	applyErr  error
	deleteErr error
}

// NewFakeRuntime creates a fake runtime that succeeds by default.
func NewFakeRuntime() *FakeRuntime {
	return &FakeRuntime{}
}

// Apply records the call and delegates to the configured behavior.
func (f *FakeRuntime) Apply(ctx context.Context, env *domain.Environment, progress func(msg string)) error {
	f.mu.Lock()
	f.ApplyCalled++
	f.AppliedEnvs = append(f.AppliedEnvs, env)
	applyErr := f.applyErr
	applyFunc := f.ApplyFunc
	f.mu.Unlock()

	if applyErr != nil {
		return applyErr
	}
	if applyFunc != nil {
		return applyFunc(ctx, env, progress)
	}
	return nil
}

// Delete records the call and delegates to the configured behavior.
func (f *FakeRuntime) Delete(ctx context.Context, envName domain.EnvironmentName) error {
	f.mu.Lock()
	f.DeleteCalled++
	f.DeletedEnvs = append(f.DeletedEnvs, envName)
	deleteErr := f.deleteErr
	deleteFunc := f.DeleteFunc
	f.mu.Unlock()

	if deleteErr != nil {
		return deleteErr
	}
	if deleteFunc != nil {
		return deleteFunc(ctx, envName)
	}
	return nil
}

// SetApplyError switches Apply to return the provided error.
func (f *FakeRuntime) SetApplyError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applyErr = err
}

// SetDeleteError switches Delete to return the provided error.
func (f *FakeRuntime) SetDeleteError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteErr = err
}

// AdminHandler exposes runtime controls for tests.
func (f *FakeRuntime) AdminHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/admin/fake/apply-error", f.handleApplyError)
	mux.HandleFunc("/admin/fake/delete-error", f.handleDeleteError)
	mux.HandleFunc("/admin/fake/restore", f.handleRestore)
	mux.HandleFunc("/admin/fake/stats", f.handleStats)
	return mux
}

func (f *FakeRuntime) handleApplyError(w http.ResponseWriter, r *http.Request) {
	if !allowMethods(w, r, http.MethodGet, http.MethodPost) {
		return
	}
	if r.Method == http.MethodPost {
		err := readErrorValue(r.Body)
		f.SetApplyError(err)
	}
	writeJSON(w, http.StatusOK, errorPayload{Error: f.applyErrorString()})
}

func (f *FakeRuntime) handleDeleteError(w http.ResponseWriter, r *http.Request) {
	if !allowMethods(w, r, http.MethodGet, http.MethodPost) {
		return
	}
	if r.Method == http.MethodPost {
		err := readErrorValue(r.Body)
		f.SetDeleteError(err)
	}
	writeJSON(w, http.StatusOK, errorPayload{Error: f.deleteErrorString()})
}

func (f *FakeRuntime) handleRestore(w http.ResponseWriter, r *http.Request) {
	if !allowMethods(w, r, http.MethodPost) {
		return
	}
	f.mu.Lock()
	f.applyErr = nil
	f.deleteErr = nil
	f.ApplyFunc = nil
	f.DeleteFunc = nil
	f.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"status": "restored"})
}

func (f *FakeRuntime) handleStats(w http.ResponseWriter, r *http.Request) {
	if !allowMethods(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, f.stats())
}

type errorPayload struct {
	Error string `json:"error"`
}

type fakeStats struct {
	ApplyCalled  int      `json:"applyCalled"`
	DeleteCalled int      `json:"deleteCalled"`
	AppliedEnvs  []string `json:"appliedEnvs"`
	DeletedEnvs  []string `json:"deletedEnvs"`
	ApplyError   string   `json:"applyError"`
	DeleteError  string   `json:"deleteError"`
}

func (f *FakeRuntime) stats() fakeStats {
	f.mu.RLock()
	defer f.mu.RUnlock()

	stats := fakeStats{
		ApplyCalled:  f.ApplyCalled,
		DeleteCalled: f.DeleteCalled,
		AppliedEnvs:  make([]string, 0, len(f.AppliedEnvs)),
		DeletedEnvs:  make([]string, 0, len(f.DeletedEnvs)),
	}
	for _, env := range f.AppliedEnvs {
		if env == nil {
			continue
		}
		stats.AppliedEnvs = append(stats.AppliedEnvs, env.Name().String())
	}
	for _, envName := range f.DeletedEnvs {
		stats.DeletedEnvs = append(stats.DeletedEnvs, envName.String())
	}
	if f.applyErr != nil {
		stats.ApplyError = f.applyErr.Error()
	}
	if f.deleteErr != nil {
		stats.DeleteError = f.deleteErr.Error()
	}
	return stats
}

func (f *FakeRuntime) applyErrorString() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.applyErr == nil {
		return ""
	}
	return f.applyErr.Error()
}

func (f *FakeRuntime) deleteErrorString() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.deleteErr == nil {
		return ""
	}
	return f.deleteErr.Error()
}

func allowMethods(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	if slices.Contains(methods, r.Method) {
		return true
	}
	w.Header().Set("Allow", strings.Join(methods, ", "))
	http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	return false
}

func readErrorValue(body io.ReadCloser) error {
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		return errors.New("read request body")
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil
	}
	if strings.HasPrefix(trimmed, "{") {
		var payload errorPayload
		if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
			if payload.Error == "" {
				return nil
			}
			return errors.New(payload.Error)
		}
	}
	return errors.New(trimmed)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
