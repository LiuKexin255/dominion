# Research: Desktop Window-Select Flow, Image-Transfer Hardening & Saolei Text-State Recognition

**Feature**: [spec.md](./spec.md) | **Date**: 2026-07-26

This document resolves the open architecture decision (image-transport mechanism) and records the design rationale for the `FlowResultPart`, the window-select simplification, the `saolei-board` integration, and the strict validation rule set. All decisions are grounded in the current code (cited) and, for the transport, the `coder/websocket` documentation.

---

## D1 — Image-transport mechanism (Problem 2)

**Decision**: Switch the desktop↔gateway WebSocket leg from **protojson text frames** to **binary protobuf frames**, and set the desktop `WSClient` read limit to match the gateway (10 MiB).

**Rationale (grounded in code + docs)**:

1. The gateway is a *translating* proxy, not a transparent one. `wsStream.Recv`/`Send` (`projects/game/gateway/cmd/main.go:151-173`) do `protojson.Marshal`/`Unmarshal` on both WS ends, converting WS frames ↔ the gRPC `ConnectAgent` stream. So the WS wire format is a local contract between desktop and gateway — it can be changed without touching the gRPC `AgentFrame` contract.
2. protojson encodes the proto `bytes` field `ImagePart.data` as **base64**, inflating every screenshot by ~33% (`projects/game/desktop/internal/api/websocket.go:70`, `SendFrame`). Binary protobuf carries the raw bytes with no inflation — directly satisfying FR-008 ("MUST NOT carry a fixed large inflation ... as the only path").
3. **The default `coder/websocket` `ReadLimit` is 32768 bytes (32 KiB)** ([coder/websocket `read.go`](https://github.com/coder/websocket/blob/master/websocket/read.go); confirmed by `projects/game/gateway/cmd/main_test.go:774-775`). Exceeding it returns `ErrMessageTooBig` and **closes the connection** with `StatusMessageTooBig` ([coder/websocket docs — SetReadLimit](https://coder.com/docs/websocket)). The gateway raises it to 10 MiB (`main.go:216`), but the **desktop `WSClient` never calls `SetReadLimit`** (`projects/game/desktop/internal/api/websocket.go:28-59`) — so any agent→desktop image-bearing frame > 32 KiB tears down the WS session. This is a concrete, confirmed mechanism behind the reported "frame 过大" failures and MUST be fixed regardless of encoding.
4. Switching to binary proto makes the WS leg carry the same compact representation as the gRPC leg; the gateway stops paying the JSON-parse + base64 cost on every frame. `proto.Unmarshal` handles unknown fields gracefully per the proto spec, preserving the forward-compatibility that `protojson`'s `DiscardUnknown: true` provided (`main.go:157`).

**Concrete changes**: desktop `WSClient.SendFrame`/`RecvFrame` use `proto.Marshal`/`proto.Unmarshal` + `websocket.MessageBinary`, and call `conn.SetReadLimit(10 << 20)` after `Dial`; gateway `wsStream.Recv`/`Send` switch to `proto.Marshal`/`proto.Unmarshal` + `websocket.MessageBinary` (the sessionID-injection logic at `main.go:161` is retained). See [contracts/image-transport-contract.md](./contracts/image-transport-contract.md).

**Alternatives considered**:

- *Just raise the desktop read limit (keep protojson).* Fixes the connection teardown but keeps the ~33% base64 inflation and the per-frame JSON parse cost — does not satisfy FR-008's "no fixed large inflation as the only path". Rejected as insufficient.
- *WS-level chunking (like the SSE path, `projects/game/desktop/internal/chatstream/chunk.go:19`).* Adds reassembly complexity on both ends; unnecessary once binary proto + correct limits are in place and the gRPC hop already tolerates 8 MiB (`main.go:48-51`). Rejected for the WS leg (chunking remains the SSE-delivery mechanism, which is unaffected).
- *Compression (gzip/zstd).* PNG is already compressed, so gzip yields little on the image bytes and adds CPU on every frame. Possible future enhancement, not core. Rejected for now.
- *Crop/resize the image to the board region before sending (saolei-specific).* A source-side optimization orthogonal to the transport; reduces bytes at the origin. Noted as an optional saolei follow-on, not required by FR-007..FR-011 (the board-region geometry lives in screenshot space per `projects/game/pkg/saolei-board/README.md` → "坐标空间注意").

**Verification note (gRPC hop)**: the gateway sets `MaxCallRecvMsgSize`/`MaxCallSendMsgSize` to 8 MiB on its *client* side (`main.go:48-51`). The proxy/agent *server* side gRPC max message size must be verified during implementation (default gRPC server max is 4 MiB); if a screenshot can exceed 4 MiB after the binary switch, the server-side limit must be raised too. This is an implementation verification item, not a separate design decision.

---

## D2 — FlowResultPart: operation-result channel separation

**Decision**: Introduce a new `FlowResultPart` proto message (fields mirror `ToolResultPart`: `tool_id`, `status`, `message`, `screenshot`) and add it as a new `FlowPart` oneof kind `flow_result = 8`. The desktop reports each operation's outcome as a `FlowResultPart` inside a `flow_parts` frame; it no longer wraps the result as a display `tool_result` `MessagePart`.

**Rationale**:

1. Today the desktop wraps its execution result as a **display** `MessagePart{tool_result}` in a `message_parts` frame (`projects/game/desktop/app.go:892-898`), yet that frame is consumed **only** by `OperationBridge.handleResult` (`projects/game/agent/src/operation-bridge.ts:261`) as a *control* message — it is never rendered. That is a semantic abuse: a display-channel message used to carry a control-channel response.
2. A dedicated `FlowResultPart` placed in the `FlowPart` oneof puts the operation result on the **control channel** (`FlowParts`), where it belongs. This completes 023's conversation/control decoupling (C13): the operation channel (request `FlowPart` ↔ response `FlowResultPart`, correlated by the bridge-minted `tool_id`) is fully separate from the conversation channel (`tool_call` ↔ `tool_result` `MessagePart`, correlated by the LangChain `tool_call.id`).
3. It gives Problem 3 a clean foundation: the post-action screenshot travels control-channel (`FlowResultPart.screenshot`) for the agent's recognition, while the model-facing display `tool_result` is emitted by the agent and is **text-only** for saolei (FR-012/FR-022). The screenshot never leaks into the saolei display path.
4. `status` reuses the existing `ToolResultStatus` enum (UNSPECIFIED/SUCCEEDED/FAILED) — the operation outcome and the derived tool-result status share semantics, so a new enum would be redundant churn. (Naming `ToolResultStatus` on a `FlowResultPart` is slightly imprecise; accepted as the lower-churn option. A future rename to a neutral `OperationStatus` is possible but out of scope.)

**Alternatives considered**:

- *Add a new `AgentFrame` payload case `FlowResultParts` (separate from `FlowParts`).* Breaks the "exactly one payload category, rendered vs control" clarity and complicates the frame router. Rejected — a `FlowResultPart` as a `FlowPart` kind keeps the control channel unified.
- *Keep `ToolResultPart` for the operation result and add a flag.* Conflates display and control in one message; directly contradicts the user directive and FR-024. Rejected.

See [contracts/flow-result-contract.md](./contracts/flow-result-contract.md) for the proto, the migration, and the per-tool translation.

---

## D3 — Window-select single source of truth (Problem 1)

**Decision**: Remove the `App.boundWin` field and its `BindWindow`/`CaptureScreenshot` two-step. The window selected in the dropdown (`selectedWindowHandle`, `projects/game/desktop/frontend/src/App.svelte:125`) is passed to each operation and each screenshot capture directly; the backend resolves the `WindowRef` from the selected handle at use time.

**Rationale**:

1. There are two notions of "the active window" today — the frontend `selectedWindowHandle` (set by the dropdown) and the backend `App.boundWin` (set **only** by `BindWindow`, called **only** inside `handleCaptureScreenshot`, `App.svelte:770-788`). Selecting a window without clicking Capture leaves `boundWin` zero-valued, so every operation fails at `projects/game/desktop/app.go:1074` (`"no window bound"`) and the post-action screenshot is skipped (`app.go:1129`). This is the reported defect.
2. The fix is not to auto-bind on selection (that preserves a redundant layer); it is to **collapse** the layer (Constitution §II): the selected window is the single source of truth. `executeAgentOperation` and the mouse executors (`projects/game/desktop/app_operation.go:28,94`) take the resolved `WindowRef` (or its handle) as an input, not from a stored field.
3. `ScaleFactor`/`WindowTitle` for `ImagePart`/`FlowResultPart` are read from the resolved selected-window `WindowRef` at capture time (replacing the reads at `app.go:719-720,1146-1147`).

**Edge handling**: when no window is selected, operations/screenshots fail gracefully with a clear message (FR-005); when the selected window has disappeared (closed/minimized/hidden), capture fails gracefully (the existing `capture.CaptureWindow` validation at `projects/game/desktop/internal/capture/capture.go` already rejects such windows).

See [contracts/window-select-contract.md](./contracts/window-select-contract.md).

---

## D4 — saolei-board integration (Problem 3)

**Decision**: Add `@dominion/game-saolei-board` as an agent workspace dependency and hold one `SaoleiBoard` instance per session inside the saolei MCP server closure (created per-session by `mcp-host.ts`). `saolei_init` calls `SaoleiBoard.init(screenshot)`; each subsequent cell op calls `updateFromScreenshot(screenshot)`.

**Rationale**:

1. `saolei-board` is a TypeScript library (`projects/game/pkg/saolei-board`); the agent is TypeScript, so it is the natural host. Recognition is deterministic fixed-geometry color analysis — no OCR, no CV — which is exactly the "把视觉识别从 LLM 卸载到确定性的颜色分析代码" its README prescribes.
2. The library already provides the needed API: `recognizeBoard(pngBytes)`, `SaoleiBoard.init(firstScreenshot)`, `updateFromScreenshot(nextScreenshot)` (monotonic cross-screenshot validation), and `renderBoardText(state)` (`projects/game/pkg/saolei-board/README.md` → "核心库用法"). It also throws `BoardStateIncompatibleError` / `BoardDimensionMismatchError` when a screenshot does not belong to the current game — mapped to FR-017's "unable to recognize" outcome.
3. The screenshot arrives from `FlowResultPart.screenshot` as a base64 PNG string (`OperationScreenshot.data`, `operation-bridge.ts:75-79`); `saolei-board` consumes raw PNG bytes, so the MCP decodes base64 → `Buffer` before calling `recognizeBoard`/`init`/`updateFromScreenshot`.
4. **Coordinate-space consistency**: `saolei-board` geometry is *screenshot space* (originY = 200, includes non-client chrome) for reading pixels; the agent's `projects/game/agent/src/mcp/saolei/geometry.ts` is *client space* (originY = 104) for `WM_*` clicks. The captured screenshot is a full-window capture (includes chrome), so screenshot-space recognition is correct; cell ops continue to use client-space geometry for clicks. The two must not be mixed (per the README's "坐标空间注意").

**State lifecycle**: in-memory in the per-session MCP server, co-located with the LangChain checkpoint; both lost together on agent restart (spec Clarification Q1). No persistence, no reconnect recovery.

See [contracts/saolei-mcp-contract.md](./contracts/saolei-mcp-contract.md).

---

## D5 — Validation rule set (strict)

**Decision**: Pre-dispatch validation is **strict** (spec Clarification Q2 / FR-015). Reject any move whose target-cell state makes it a no-op or is impermissible per the rules; allow only rule-permitted moves to dispatch. Validation judges **target-cell compatibility**, not predicted outcome.

**The rules (recognized cell symbols from `saolei-board`: `*` INITIAL, `0`–`8` revealed number, `F` FLAG, `X` HIT_MINE, `M` MINE, `?` UNKNOWN)**:

| Op | Target state | Verdict | Reason |
|---|---|---|---|
| `saolei_click` (left) | `*` INITIAL | **dispatch** | legal reveal |
| `saolei_click` | `0`–`8` revealed | **reject** | no-op on revealed cell |
| `saolei_click` | `F` FLAG | **reject** | flagged cell is protected (no-op) |
| `saolei_flag` (right) | `*` INITIAL | **dispatch** | place flag |
| `saolei_flag` | `F` FLAG | **dispatch** | toggle/unflag (legal) |
| `saolei_flag` | `0`–`8` revealed | **reject** | cannot flag a revealed cell |
| `saolei_chord_click` | `1`–`8` revealed number | **dispatch** | legal chord (even if flag-count mismatches — may reveal nothing) |
| `saolei_chord_click` | `0` / `*` / `F` | **reject** | chord requires a revealed number |
| any cell op | `?` UNKNOWN target | **dispatch** (lenient) | FR-018 — do not reject on uncertainty |
| any cell op | no active game (pre-init) | **reject** | FR-015(a) |
| any cell op | coordinate outside board | **reject** | FR-015(b) |
| any cell op | terminal state (won/lost) | **reject** | FR-015(f) |

**Rationale**: A rejected move is cheap (no dispatch, no screenshot round-trip); a no-op dispatch is expensive (full operation + capture + recognition that changes nothing) and gives the model weaker feedback. Deterministic recognition + `UNKNOWN` leniency keeps false-positive risk low. The chord nuance (target-is-number ⇒ always legal, even if it reveals nothing) reflects that validation cannot predict chord success without the mine layout, so it judges only target-cell compatibility.

See [contracts/saolei-mcp-contract.md](./contracts/saolei-mcp-contract.md) §Validation.

---

## D6 — Saolei tool return contract (text board)

**Decision**: Every saolei tool returns the recognized board as **text** via `renderBoardText(state)` (`projects/game/pkg/saolei-board/README.md` → symbol legend), plus a short outcome line. No model-facing image block (FR-012). A rejected move returns the rejection reason + the current text board + the valid coordinate range (FR-016). `saolei_init` returns the initial board text; the cell tools return the updated board text after a legal dispatch.

**Rationale**: the model gets an accurate, token-light board representation instead of being asked to read pixels, improving move accuracy (SC-006). The screenshot is consumed for recognition only and stays in the control channel (`FlowResultPart`), so the display bubble is text-only (FR-022).

See [contracts/saolei-mcp-contract.md](./contracts/saolei-mcp-contract.md) §Return shape.

---

## Summary of decisions

| ID | Topic | Decision |
|---|---|---|
| D1 | Image transport | Binary protobuf WS frames + desktop `SetReadLimit(10 MiB)` |
| D2 | Operation result | New `FlowResultPart` as a `FlowPart` kind (`flow_result = 8`) |
| D3 | Window-select | Remove `boundWin`; selected window is the single source of truth |
| D4 | Recognition | `@dominion/game-saolei-board` per-session `SaoleiBoard` in the agent |
| D5 | Validation | Strict — reject target-cell no-ops/impermissible moves (table above) |
| D6 | Saolei return | Text board via `renderBoardText`; no model-facing image |
