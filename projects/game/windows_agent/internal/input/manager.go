package input

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

const (
	// DefaultTimeout is the IPC timeout for click, double-click, and hover actions.
	DefaultTimeout = 1 * time.Second
	// DragTimeout is the IPC timeout for drag actions.
	DragTimeout = 30 * time.Second
	// MaxHoldDuration is the maximum IPC timeout for hold actions.
	MaxHoldDuration = 30 * time.Second
)

// timeoutForAction returns the appropriate IPC timeout for a given action.
func timeoutForAction(action Action) time.Duration {
	switch action {
	case ActionMouseDrag:
		return DragTimeout
	case ActionMouseHold:
		return MaxHoldDuration
	default:
		return DefaultTimeout
	}
}

// Manager manages the input-helper.exe subprocess lifecycle and IPC
// communication.
type Manager struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.Reader
	mu     sync.Mutex
	cancel context.CancelFunc
}

// NewManager creates a new Manager.
func NewManager() *Manager {
	return new(Manager)
}

// Start launches the input-helper.exe subprocess with stdin/stdout pipes for
// JSON-line IPC communication.
func (m *Manager) Start(helperPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cmd != nil && m.cmd.Process != nil {
		return fmt.Errorf("helper already running")
	}

	ctx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(ctx, helperPath)
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("create stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start helper: %w", err)
	}

	m.cmd = cmd
	m.stdin = stdinPipe
	m.stdout = stdoutPipe
	m.cancel = cancel
	return nil
}

// ExecuteCommand sends a JSON command to the helper via stdin and reads the
// JSON response from stdout. It uses the appropriate timeout based on the
// action type.
func (m *Manager) ExecuteCommand(ctx context.Context, cmd Command) (Response, error) {
	m.mu.Lock()
	if m.stdin == nil || m.stdout == nil {
		m.mu.Unlock()
		return Response{}, fmt.Errorf("helper not running")
	}
	stdin := m.stdin
	stdout := m.stdout
	m.mu.Unlock()

	timeout := timeoutForAction(cmd.Action)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	data, err := json.Marshal(cmd)
	if err != nil {
		return Response{}, fmt.Errorf("marshal command: %w", err)
	}

	if _, err := fmt.Fprintf(stdin, "%s\n", data); err != nil {
		return Response{}, fmt.Errorf("write command: %w", err)
	}

	reader := bufio.NewReader(stdout)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return Response{}, fmt.Errorf("read response: %w", err)
	}

	resp, err := ParseResponse(line)
	if err != nil {
		return Response{}, fmt.Errorf("parse response: %w", err)
	}

	if resp.Status == StatusError {
		return resp, fmt.Errorf("helper error: %s", resp.Message)
	}

	return resp, nil
}

// Stop releases all held buttons, kills the helper process, and cleans up
// resources.
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cancel != nil {
		m.cancel()
	}

	var err error
	if m.cmd != nil && m.cmd.Process != nil {
		killErr := m.cmd.Process.Kill()
		if killErr != nil {
			err = fmt.Errorf("kill helper: %w", killErr)
		}
		// Wait to reap the process.
		_ = m.cmd.Wait()
	}

	m.cmd = nil
	m.stdin = nil
	m.stdout = nil
	m.cancel = nil
	return err
}

// Running reports whether the helper subprocess is currently active.
func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cmd != nil && m.cmd.Process != nil
}
