// Package fakeagent provides a fake game agent for testing the game gateway.
//
// The agent connects to a game gateway via WebSocket and simulates the
// windows agent side of the protocol. It supports configurable scenarios
// for testing different agent behaviors (normal, timeout, disconnect, etc.).
//
// Tests import this package and start an Agent as a goroutine within the
// test process, eliminating the need for a separately deployed fake-agent
// service while still exercising the real network path through the gateway.
package fakeagent

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	gw "dominion/projects/game/gateway"
	"dominion/projects/game/gateway/domain"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Scenario selects the agent's behavior pattern.
type Scenario string

const (
	Normal       Scenario = "normal"
	Delayed      Scenario = "delayed"
	Timeout      Scenario = "timeout"
	Disconnect   Scenario = "disconnect"
	PauseMedia   Scenario = "pause_media"
	SnapshotFail Scenario = "snapshot_fail"
)

const (
	mediaSegmentInterval = 100 * time.Millisecond
	pauseMediaAfter      = 5 * time.Second

	controlAckDelay    = 50 * time.Millisecond
	controlResultDelay = 100 * time.Millisecond

	mimeTypeMP4 = "video/mp4; codecs=\"avc1.64001f\""
)

var (
	protojsonMarshaler   = &protojson.MarshalOptions{}
	protojsonUnmarshaler = &protojson.UnmarshalOptions{DiscardUnknown: true}
)

// Config configures a fake agent instance.
type Config struct {
	// ConnectURL is the full WebSocket URL including token query param.
	ConnectURL string
	// SessionID is sent in the hello message envelope.
	SessionID string
	// Scenario selects the agent behavior.
	Scenario Scenario
	// EnvHeader is the value for the "env" HTTP header required by the
	// ingress HTTPRoute to route the request to the backend.
	EnvHeader string
	// VideoURL is an optional S3 URL (s3://bucket/key) for real video data.
	VideoURL string
	// VideoFile is an optional local file path for real video data.
	VideoFile string
}

// Agent connects to a game gateway and runs the agent side of the WebSocket
// protocol according to the configured scenario.
type Agent struct {
	cfg    Config
	conn   *websocket.Conn
	mu     sync.Mutex
	cancel context.CancelFunc
	ready  chan struct{}
	once   sync.Once
}

// New creates a new Agent with the given configuration.
func New(cfg Config) *Agent {
	return &Agent{
		cfg:   cfg,
		ready: make(chan struct{}),
	}
}

// Run connects to the gateway and runs the configured scenario.
// It blocks until the scenario completes or the context is cancelled.
//
// After hello is sent successfully, the Ready channel is signaled.
func (a *Agent) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	defer cancel()

	log.Printf("fakeagent: connecting to %s", maskURL(a.cfg.ConnectURL))
	opts := &websocket.DialOptions{}
	if a.cfg.EnvHeader != "" {
		opts.HTTPHeader = http.Header{"env": {a.cfg.EnvHeader}}
	}
	conn, _, err := websocket.Dial(ctx, a.cfg.ConnectURL, opts)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	conn.SetReadLimit(int64(domain.MaxSegmentSize)*2 + 4096)
	a.conn = conn
	defer func() {
		conn.Close(websocket.StatusNormalClosure, "fake-agent done")
	}()

	log.Printf("fakeagent: connected, sending hello")
	if err := a.sendHello(ctx); err != nil {
		return fmt.Errorf("send hello: %w", err)
	}

	a.once.Do(func() { close(a.ready) })

	md := prepareMediaData(a.cfg.VideoFile, a.cfg.VideoURL)

	switch a.cfg.Scenario {
	case Normal:
		return a.runScenario(ctx, md, scenarioConfig{})
	case Delayed:
		return a.runScenario(ctx, md, scenarioConfig{
			ackDelay:    controlAckDelay,
			resultDelay: controlResultDelay,
		})
	case Timeout:
		return a.runScenario(ctx, md, scenarioConfig{ignoreControl: true})
	case Disconnect:
		return a.runDisconnect(ctx)
	case PauseMedia:
		return a.runScenario(ctx, md, scenarioConfig{pauseAfter: pauseMediaAfter})
	case SnapshotFail:
		failMD := &mediaData{
			initSegment:  md.initSegment,
			mediaSegs:    md.mediaSegs,
			keyFrameMask: make([]bool, len(md.mediaSegs)),
		}
		return a.runScenario(ctx, failMD, scenarioConfig{})
	default:
		return fmt.Errorf("unknown scenario: %s", a.cfg.Scenario)
	}
}

// Ready returns a channel that is closed after the agent has connected
// and sent hello. Tests can use this to synchronize with the agent.
func (a *Agent) Ready() <-chan struct{} {
	return a.ready
}

// Close cancels the agent's context, causing Run to return.
func (a *Agent) Close() {
	if a.cancel != nil {
		a.cancel()
	}
}

func (a *Agent) sendHello(ctx context.Context) error {
	return a.writeEnvelope(ctx, &gw.GameWebSocketEnvelope{
		SessionId: a.cfg.SessionID,
		MessageId: messageID("hello"),
		Payload: &gw.GameWebSocketEnvelope_Hello{
			Hello: &gw.GameHello{
				Role: gw.GameClientRole_GAME_CLIENT_ROLE_WINDOWS_AGENT,
			},
		},
	})
}

// runDisconnect sends media_init, then waits for a control_request.
// When a control_request is received, the agent closes the connection.
func (a *Agent) runDisconnect(ctx context.Context) error {
	a.mu.Lock()
	a.sendMediaInit(ctx, generateFakeInitSegment())
	a.mu.Unlock()

	log.Printf("fakeagent: disconnect scenario, waiting for control_request")
	for {
		_, data, err := a.conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read: %w", err)
		}

		envelope := new(gw.GameWebSocketEnvelope)
		if err := protojsonUnmarshaler.Unmarshal(data, proto.Message(envelope)); err != nil {
			continue
		}

		if req := envelope.GetControlRequest(); req != nil {
			log.Printf("fakeagent: received control_request, disconnecting: %s", req.OperationId)
			a.conn.Close(websocket.StatusNormalClosure, "fake-agent disconnect")
			return nil
		}
		if ping := envelope.GetPing(); ping != nil {
			a.mu.Lock()
			a.sendPong(ctx, envelope.GetSessionId(), ping.Nonce)
			a.mu.Unlock()
		}
	}
}

type scenarioConfig struct {
	ackDelay      time.Duration
	resultDelay   time.Duration
	ignoreControl bool
	pauseAfter    time.Duration
}

func (a *Agent) runScenario(ctx context.Context, md *mediaData, sc scenarioConfig) error {
	controlCh := make(chan *gw.GameControlRequest, 16)

	go func() {
		defer close(controlCh)
		a.readControlLoop(ctx, controlCh)
	}()

	func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.sendMediaInit(ctx, md.initSegment)
	}()

	segIdx := 0
	segCount := len(md.mediaSegs)

	if segCount > 0 {
		isKeyFrame := segIdx < len(md.keyFrameMask) && md.keyFrameMask[segIdx]
		segData := md.mediaSegs[segIdx%segCount]
		segIdx++
		a.mu.Lock()
		a.sendMediaSegment(ctx, segData, isKeyFrame)
		a.mu.Unlock()
	}

	ticker := time.NewTicker(mediaSegmentInterval)
	defer ticker.Stop()

	startTime := time.Now()
	paused := false

	for {
		select {
		case <-ctx.Done():
			return nil

		case req := <-controlCh:
			if req == nil {
				return nil
			}
			a.handleControl(ctx, req, sc)

		case <-ticker.C:
			if sc.pauseAfter > 0 && !paused && time.Since(startTime) > sc.pauseAfter {
				paused = true
				log.Printf("fakeagent: pausing media after %v", sc.pauseAfter)
				continue
			}
			if paused {
				continue
			}

			isKeyFrame := segIdx < len(md.keyFrameMask) && md.keyFrameMask[segIdx]
			segData := md.mediaSegs[segIdx%segCount]
			segIdx++

			a.mu.Lock()
			a.sendMediaSegment(ctx, segData, isKeyFrame)
			a.mu.Unlock()
		}
	}
}

func (a *Agent) readControlLoop(ctx context.Context, ch chan<- *gw.GameControlRequest) {
	for {
		_, data, err := a.conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("fakeagent: read error: %v", err)
			return
		}

		envelope := new(gw.GameWebSocketEnvelope)
		if err := protojsonUnmarshaler.Unmarshal(data, proto.Message(envelope)); err != nil {
			log.Printf("fakeagent: unmarshal: %v", err)
			continue
		}

		if req := envelope.GetControlRequest(); req != nil {
			log.Printf("fakeagent: received control_request: %s kind=%v", req.OperationId, req.Kind)
			ch <- req
		} else if ping := envelope.GetPing(); ping != nil {
			a.mu.Lock()
			a.sendPong(ctx, envelope.GetSessionId(), ping.Nonce)
			a.mu.Unlock()
		}
	}
}

func (a *Agent) handleControl(ctx context.Context, req *gw.GameControlRequest, sc scenarioConfig) {
	if sc.ignoreControl {
		log.Printf("fakeagent: ignoring control_request (timeout scenario): %s", req.OperationId)
		return
	}

	if sc.ackDelay > 0 {
		time.Sleep(sc.ackDelay)
	}

	func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.sendControlAck(ctx, req.OperationId)
	}()
	log.Printf("fakeagent: sent control_ack: %s", req.OperationId)

	if sc.resultDelay > 0 {
		time.Sleep(sc.resultDelay)
	}

	func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.sendControlResult(ctx, req.OperationId, gw.GameControlResultStatus_GAME_CONTROL_RESULT_STATUS_SUCCEEDED)
	}()
	log.Printf("fakeagent: sent control_result: %s SUCCEEDED", req.OperationId)
}

func (a *Agent) sendMediaInit(ctx context.Context, segment []byte) {
	if len(segment) > domain.MaxSegmentSize {
		log.Printf("fakeagent: skip media_init: %d bytes exceeds %d limit", len(segment), domain.MaxSegmentSize)
		return
	}
	if err := a.writeEnvelope(ctx, &gw.GameWebSocketEnvelope{
		SessionId: a.cfg.SessionID,
		MessageId: messageID("media-init"),
		Payload: &gw.GameWebSocketEnvelope_MediaInit{
			MediaInit: &gw.GameMediaInit{
				MimeType: mimeTypeMP4,
				Segment:  segment,
			},
		},
	}); err != nil {
		log.Printf("fakeagent: send media_init: %v", err)
		return
	}
	log.Printf("fakeagent: sent media_init (%d bytes)", len(segment))
}

func (a *Agent) sendMediaSegment(ctx context.Context, segment []byte, keyFrame bool) {
	if len(segment) > domain.MaxSegmentSize {
		log.Printf("fakeagent: skip media_segment: %d bytes exceeds %d limit", len(segment), domain.MaxSegmentSize)
		return
	}
	if err := a.writeEnvelope(ctx, &gw.GameWebSocketEnvelope{
		SessionId: a.cfg.SessionID,
		MessageId: messageID("media-seg"),
		Payload: &gw.GameWebSocketEnvelope_MediaSegment{
			MediaSegment: &gw.GameMediaSegment{
				SegmentId: messageID("seg"),
				Segment:   segment,
				KeyFrame:  keyFrame,
			},
		},
	}); err != nil {
		log.Printf("fakeagent: send media_segment: %v", err)
	}
}

func (a *Agent) sendControlAck(ctx context.Context, operationID string) {
	if err := a.writeEnvelope(ctx, &gw.GameWebSocketEnvelope{
		SessionId: a.cfg.SessionID,
		MessageId: messageID("ack"),
		Payload: &gw.GameWebSocketEnvelope_ControlAck{
			ControlAck: &gw.GameControlAck{
				OperationId: operationID,
			},
		},
	}); err != nil {
		log.Printf("fakeagent: send control_ack: %v", err)
	}
}

func (a *Agent) sendControlResult(ctx context.Context, operationID string, status gw.GameControlResultStatus) {
	if err := a.writeEnvelope(ctx, &gw.GameWebSocketEnvelope{
		SessionId: a.cfg.SessionID,
		MessageId: messageID("result"),
		Payload: &gw.GameWebSocketEnvelope_ControlResult{
			ControlResult: &gw.GameControlResult{
				OperationId: operationID,
				Status:      status,
			},
		},
	}); err != nil {
		log.Printf("fakeagent: send control_result: %v", err)
	}
}

func (a *Agent) sendPong(ctx context.Context, sessionID, nonce string) {
	if err := a.writeEnvelope(ctx, &gw.GameWebSocketEnvelope{
		SessionId: sessionID,
		MessageId: messageID("pong"),
		Payload: &gw.GameWebSocketEnvelope_Pong{
			Pong: &gw.GamePong{Nonce: nonce},
		},
	}); err != nil {
		log.Printf("fakeagent: send pong: %v", err)
	}
}

func (a *Agent) writeEnvelope(ctx context.Context, envelope *gw.GameWebSocketEnvelope) error {
	data, err := protojsonMarshaler.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return a.conn.Write(ctx, websocket.MessageText, data)
}

func messageID(prefix string) string {
	return fmt.Sprintf("fake-agent-%s-%d", prefix, time.Now().UnixNano())
}

func maskURL(rawURL string) string {
	idx := strings.Index(rawURL, "?")
	if idx >= 0 {
		return rawURL[:idx] + "?..."
	}
	return rawURL
}
