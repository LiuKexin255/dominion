package main

import (
	"context"
	"fmt"

	game "dominion/projects/game"
	"dominion/projects/game/desktop/internal/api"
	"dominion/projects/game/desktop/internal/applog"
)

// App is the Wails application struct holding all state.
type App struct {
	logger *applog.Logger
	client *api.Client
	ws     *api.WSClient
	cfg    api.Config
	ctx    context.Context
}

// NewApp creates a new App with default configuration.
func NewApp(logger *applog.Logger) *App {
	return &App{
		logger: logger,
		cfg: api.Config{
			GatewayURL: "https://game.liukexin.com",
		},
	}
}

// SetContext is called by the Wails OnStartup hook to store the app context.
func (a *App) SetContext(ctx context.Context) {
	a.ctx = ctx
}

// GetConfig returns the current configuration.
func (a *App) GetConfig() api.Config {
	a.logger.Info("backend", "GetConfig called")
	return a.cfg
}

// SetConfig updates the configuration and recreates the HTTP client.
func (a *App) SetConfig(cfg api.Config) error {
	if cfg.GatewayURL == "" {
		return fmt.Errorf("set config: GatewayURL is required")
	}
	a.cfg = cfg
	a.client = api.NewClient(cfg)
	a.logger.Info("backend", "Config updated", map[string]any{"gateway_url": cfg.GatewayURL})
	return nil
}

// CreateSession creates a game session via the gateway.
func (a *App) CreateSession(sessionID string) (*api.Session, error) {
	if a.client == nil {
		a.client = api.NewClient(a.cfg)
	}
	a.logger.Info("backend", "Creating session", map[string]any{"session_id": sessionID})
	session, err := a.client.CreateSession(sessionID)
	if err != nil {
		a.logger.Error("backend", "Create session failed", map[string]any{"error": err.Error()})
		return nil, err
	}
	a.logger.Info("backend", "Session created", map[string]any{"session_id": session.SessionID})
	return session, nil
}

// GetSession retrieves a session by ID.
func (a *App) GetSession(sessionID string) (*api.Session, error) {
	if a.client == nil {
		a.client = api.NewClient(a.cfg)
	}
	a.logger.Info("backend", "Getting session", map[string]any{"session_id": sessionID})
	session, err := a.client.GetSession(sessionID)
	if err != nil {
		a.logger.Error("backend", "Get session failed", map[string]any{"error": err.Error()})
		return nil, err
	}
	return session, nil
}

// DeleteSession deletes a session by ID.
func (a *App) DeleteSession(sessionID string) error {
	if a.client == nil {
		a.client = api.NewClient(a.cfg)
	}
	a.logger.Info("backend", "Deleting session", map[string]any{"session_id": sessionID})
	if err := a.client.DeleteSession(sessionID); err != nil {
		a.logger.Error("backend", "Delete session failed", map[string]any{"error": err.Error()})
		return err
	}
	a.logger.Info("backend", "Session deleted")
	return nil
}

// CreateAgent creates an agent for a session.
func (a *App) CreateAgent(sessionID string) (*api.Agent, error) {
	if a.client == nil {
		a.client = api.NewClient(a.cfg)
	}
	a.logger.Info("backend", "Creating agent", map[string]any{"session_id": sessionID})
	agent, err := a.client.CreateAgent(sessionID)
	if err != nil {
		a.logger.Error("backend", "Create agent failed", map[string]any{"error": err.Error()})
		return nil, err
	}
	a.logger.Info("backend", "Agent created", map[string]any{"session_id": agent.SessionID})
	return agent, nil
}

// GetAgent retrieves the agent for a session.
func (a *App) GetAgent(sessionID string) (*api.Agent, error) {
	if a.client == nil {
		a.client = api.NewClient(a.cfg)
	}
	a.logger.Info("backend", "Getting agent", map[string]any{"session_id": sessionID})
	agent, err := a.client.GetAgent(sessionID)
	if err != nil {
		a.logger.Error("backend", "Get agent failed", map[string]any{"error": err.Error()})
		return nil, err
	}
	return agent, nil
}

// DeleteAgent deletes the agent for a session.
func (a *App) DeleteAgent(sessionID string) error {
	if a.client == nil {
		a.client = api.NewClient(a.cfg)
	}
	a.logger.Info("backend", "Deleting agent", map[string]any{"session_id": sessionID})
	if err := a.client.DeleteAgent(sessionID); err != nil {
		a.logger.Error("backend", "Delete agent failed", map[string]any{"error": err.Error()})
		return err
	}
	a.logger.Info("backend", "Agent deleted")
	return nil
}

// ConnectAgent establishes a WebSocket connection for the agent.
func (a *App) ConnectAgent(sessionID string) error {
	a.logger.Info("backend", "Connecting agent via WebSocket", map[string]any{"session_id": sessionID})

	// Close any existing WS connection first
	if a.ws != nil {
		a.ws.Close()
	}

	ws := &api.WSClient{}
	if err := ws.Connect(a.cfg.GatewayURL, sessionID, a.cfg.Env); err != nil {
		a.logger.Error("backend", "Connect agent failed", map[string]any{"error": err.Error()})
		return err
	}
	a.ws = ws
	a.logger.Info("backend", "Agent connected via WebSocket")
	return nil
}

// SendAgentFrame sends a frame over the WebSocket and returns the response.
func (a *App) SendAgentFrame(frame *game.AgentFrame) (*game.AgentFrame, error) {
	if a.ws == nil {
		return nil, fmt.Errorf("send frame: not connected")
	}
	a.logger.Info("backend", "Sending frame", map[string]any{"session_id": frame.GetSessionId()})
	if err := a.ws.SendFrame(frame); err != nil {
		a.logger.Error("backend", "Send frame failed", map[string]any{"error": err.Error()})
		return nil, err
	}
	resp, err := a.ws.RecvFrame()
	if err != nil {
		a.logger.Error("backend", "Receive frame failed", map[string]any{"error": err.Error()})
		return nil, err
	}
	a.logger.Info("backend", "Frame received", map[string]any{"session_id": resp.GetSessionId()})
	return resp, nil
}

// CloseAgent closes the WebSocket connection.
func (a *App) CloseAgent() error {
	if a.ws == nil {
		return nil
	}
	a.logger.Info("backend", "Closing agent WebSocket")
	if err := a.ws.Close(); err != nil {
		a.logger.Error("backend", "Close agent failed", map[string]any{"error": err.Error()})
		return err
	}
	a.ws = nil
	return nil
}

// RunConnectivityCheck executes the full connectivity check sequence.
// Sequence: CreateSession → CreateAgent → GetSession → GetAgent → ConnectAgent →
//
//	Send status (expect "initialized") → Send echo (expect echoed back) →
//	DeleteAgent → DeleteSession
func (a *App) RunConnectivityCheck(sessionID string) (*api.CheckResult, error) {
	result := &api.CheckResult{Success: false, Steps: []string{}}
	a.logger.Info("backend", "Starting connectivity check", map[string]any{"session_id": sessionID})

	// Step 1: CreateSession
	result.Steps = append(result.Steps, "CreateSession")
	if _, err := a.CreateSession(sessionID); err != nil {
		result.Error = fmt.Sprintf("CreateSession failed: %v", err)
		a.logger.Error("backend", "Connectivity check failed at CreateSession", map[string]any{"error": err.Error()})
		return result, err
	}

	// Step 2: CreateAgent
	result.Steps = append(result.Steps, "CreateAgent")
	if _, err := a.CreateAgent(sessionID); err != nil {
		result.Error = fmt.Sprintf("CreateAgent failed: %v", err)
		a.logger.Error("backend", "Connectivity check failed at CreateAgent", map[string]any{"error": err.Error()})
		return result, err
	}

	// Step 3: GetSession
	result.Steps = append(result.Steps, "GetSession")
	if _, err := a.GetSession(sessionID); err != nil {
		result.Error = fmt.Sprintf("GetSession failed: %v", err)
		a.logger.Error("backend", "Connectivity check failed at GetSession", map[string]any{"error": err.Error()})
		return result, err
	}

	// Step 4: GetAgent
	result.Steps = append(result.Steps, "GetAgent")
	if _, err := a.GetAgent(sessionID); err != nil {
		result.Error = fmt.Sprintf("GetAgent failed: %v", err)
		a.logger.Error("backend", "Connectivity check failed at GetAgent", map[string]any{"error": err.Error()})
		return result, err
	}

	// Step 5: ConnectAgent
	result.Steps = append(result.Steps, "ConnectAgent")
	if err := a.ConnectAgent(sessionID); err != nil {
		result.Error = fmt.Sprintf("ConnectAgent failed: %v", err)
		a.logger.Error("backend", "Connectivity check failed at ConnectAgent", map[string]any{"error": err.Error()})
		// Clean up before returning
		a.DeleteAgent(sessionID)
		a.DeleteSession(sessionID)
		return result, err
	}
	defer a.CloseAgent()

	// Step 6: Send status frame — verify response
	result.Steps = append(result.Steps, "SendStatus")
	statusFrame := &game.AgentFrame{
		SessionId: sessionID,
		Payload: &game.AgentFrame_Status{
			Status: &game.AgentStatusFrame{Status: "initialized"},
		},
	}
	respStatus, err := a.SendAgentFrame(statusFrame)
	if err != nil {
		result.Error = fmt.Sprintf("SendStatus failed: %v", err)
		a.logger.Error("backend", "Connectivity check failed at SendStatus", map[string]any{"error": err.Error()})
		a.DeleteAgent(sessionID)
		a.DeleteSession(sessionID)
		return result, err
	}
	// Check status response contains expected status
	statusPayload := respStatus.GetStatus()
	if statusPayload == nil || statusPayload.GetStatus() != "initialized" {
		err := fmt.Errorf("unexpected status response: %v", respStatus.GetPayload())
		result.Error = err.Error()
		a.logger.Error("backend", "Connectivity check failed at SendStatus verification", map[string]any{"response": respStatus.String()})
		a.DeleteAgent(sessionID)
		a.DeleteSession(sessionID)
		return result, err
	}
	a.logger.Info("backend", "Status response verified", map[string]any{"status": "initialized"})

	// Step 7: Send echo frame — verify echo back
	result.Steps = append(result.Steps, "SendEcho")
	echoFrame := &game.AgentFrame{
		SessionId: sessionID,
		Payload: &game.AgentFrame_Echo{
			Echo: &game.AgentEchoFrame{Data: []byte("hello")},
		},
	}
	respEcho, err := a.SendAgentFrame(echoFrame)
	if err != nil {
		result.Error = fmt.Sprintf("SendEcho failed: %v", err)
		a.logger.Error("backend", "Connectivity check failed at SendEcho", map[string]any{"error": err.Error()})
		a.DeleteAgent(sessionID)
		a.DeleteSession(sessionID)
		return result, err
	}
	echoPayload := respEcho.GetEcho()
	if echoPayload == nil || string(echoPayload.GetData()) != "hello" {
		err := fmt.Errorf("echo payload mismatch: expected hello, got %v", respEcho.GetPayload())
		result.Error = err.Error()
		a.logger.Error("backend", "Connectivity check failed at echo verification", map[string]any{"expected": "hello", "got": respEcho.String()})
		a.DeleteAgent(sessionID)
		a.DeleteSession(sessionID)
		return result, err
	}
	a.logger.Info("backend", "Echo response verified")

	// Step 8: DeleteAgent
	result.Steps = append(result.Steps, "DeleteAgent")
	if err := a.DeleteAgent(sessionID); err != nil {
		result.Error = fmt.Sprintf("DeleteAgent failed: %v", err)
		a.logger.Error("backend", "Connectivity check failed at DeleteAgent", map[string]any{"error": err.Error()})
		return result, err
	}

	// Step 9: DeleteSession
	result.Steps = append(result.Steps, "DeleteSession")
	if err := a.DeleteSession(sessionID); err != nil {
		result.Error = fmt.Sprintf("DeleteSession failed: %v", err)
		a.logger.Error("backend", "Connectivity check failed at DeleteSession", map[string]any{"error": err.Error()})
		return result, err
	}

	result.Success = true
	a.logger.Info("backend", "Connectivity check PASSED", map[string]any{"steps": result.Steps})
	return result, nil
}

// Logs returns all current log entries.
func (a *App) Logs() []applog.Entry {
	return a.logger.Entries()
}
