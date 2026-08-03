# Contract: Image transport (desktop ↔ gateway WebSocket leg)

**Feature**: [spec.md](../spec.md) (FR-007..FR-011) | **Research**: [research.md](../research.md) D1

This contract specifies the wire format and read limits for the desktop↔gateway WebSocket leg that carries `AgentFrame`s (including image-bearing frames), replacing the current protojson-text-frame encoding.

## 1. Scope

The image path is: desktop ↔ gateway (WebSocket) ↔ proxy (gRPC) ↔ agent (gRPC). This contract governs the **WebSocket leg** (desktop ↔ gateway) — the leg the user identified as "frame 过大". The gRPC hops are governed by their existing message-size config (see §5).

Two directions carry images on this leg:
- desktop → gateway: user-turn screenshot (`SendUserTurn`), operation-result screenshot (now in `FlowResultPart`, [flow-result-contract.md](./flow-result-contract.md)).
- gateway → desktop: agent-emitted display frames that may include a screenshot (e.g. a mouse tool's `tool_result`).

## 2. Wire format — binary protobuf (was: protojson text)

Both ends switch from protojson (`websocket.MessageText`) to **binary protobuf** (`websocket.MessageBinary`):

| End | File | Before | After |
|---|---|---|---|
| Desktop send | `projects/game/desktop/internal/api/websocket.go:62-79` (`SendFrame`) | `protojson.Marshal` → `Write(MessageText, …)` | `proto.Marshal` → `Write(MessageBinary, …)` |
| Desktop recv | `projects/game/desktop/internal/api/websocket.go:87-106` (`RecvFrame`) | `conn.Read` → `protojson.Unmarshal(DiscardUnknown)` | `conn.Read` → `proto.Unmarshal` |
| Gateway recv | `projects/game/gateway/cmd/main.go:151-163` (`wsStream.Recv`) | `protojson.Unmarshal(DiscardUnknown)` | `proto.Unmarshal` |
| Gateway send | `projects/game/gateway/cmd/main.go:167-173` (`wsStream.Send`) | `protojson.Marshal` → `Write(MessageText, …)` | `proto.Marshal` → `Write(MessageBinary, …)` |

The gateway's sessionID injection (`main.go:161`, `frame.SessionId = s.sessionID`) is retained — it still unmarshals the `AgentFrame`, sets `SessionId`, and re-marshals.

**Why**: binary protobuf carries `ImagePart.data` (proto `bytes`) as raw bytes — no base64, no ~33% inflation (FR-008). It also removes the per-frame JSON parse cost. `proto.Unmarshal` preserves unknown fields per the proto spec, preserving the forward-compatibility that `protojson`'s `DiscardUnknown` provided.

## 3. Read limits

`coder/websocket`'s default `ReadLimit` is **32 KiB** ([coder/websocket `read.go`](https://github.com/coder/websocket/blob/master/websocket/read.go)); exceeding it returns `ErrMessageTooBig` and **closes the connection** with `StatusMessageTooBig` ([coder/websocket docs — SetReadLimit](https://coder.com/docs/websocket)). The gateway already raises it to 10 MiB (`projects/game/gateway/cmd/main.go:216`); the **desktop `WSClient` currently does not set it** (`websocket.go:28-59`) — so any image-bearing agent→desktop frame > 32 KiB tears down the WS session.

Required fix: the desktop `WSClient` calls `conn.SetReadLimit(10 << 20)` immediately after `websocket.Dial` succeeds (matching the gateway's 10 MiB). Both ends now agree on 10 MiB.

## 4. Reliability & error handling (FR-010)

- An oversized frame (only possible if > 10 MiB) MUST surface as a clear, attributable error. At 10 MiB this is far above any expected window screenshot, so it is a guardrail, not an operating point.
- A delivery failure MUST NOT destabilize the session or other turns: the desktop's turn fails with a clear message; the WS connection is re-established per the existing connect/close lifecycle (`projects/game/desktop/app.go` `ConnectAgent`/`CloseAgent`).
- No silent truncation: `coder/websocket` either delivers a complete message or returns `ErrMessageTooBig` (and closes); there is no partial-read path to handle.

## 5. gRPC hops (verification, not a design change)

The gateway sets `MaxCallRecvMsgSize`/`MaxCallSendMsgSize` to 8 MiB on its *client* side (`projects/game/gateway/cmd/main.go:48-51`). The **server-side** gRPC max message size for the proxy/agent must be verified during implementation (default gRPC server max is 4 MiB); if a screenshot can exceed 4 MiB, the server-side limit must be raised to match. This is an implementation verification item recorded in [research.md](../research.md) D1.

## 6. Out of scope

- The SSE delivery path (desktop → frontend chat display) is unchanged; it already chunks at 48 KiB (`projects/game/desktop/internal/chatstream/chunk.go:19`). Switching the WS leg to binary does not affect SSE (the desktop re-serializes for SSE independently).
- Compression, and saolei-specific image cropping/resizing, are noted as optional future enhancements ([research.md](../research.md) D1); not required by FR-007..FR-011.

## 7. Test anchors

- Desktop unit: `WSClient.SendFrame`/`RecvFrame` use binary proto and `SetReadLimit(10 MiB)`; a round-trip of a large (> 32 KiB) frame succeeds (would fail under the old default limit).
- Gateway unit: `wsStream` reads/writes binary proto; the existing `TestReadLimitSet` (`projects/game/gateway/cmd/main_test.go:773`) is updated from protojson to binary and continues to assert the 10 MiB limit.
- Large test: a turn carrying a large screenshot completes without a frame-size failure ([quickstart.md](../quickstart.md)).
