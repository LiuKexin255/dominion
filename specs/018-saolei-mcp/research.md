# Research: Saolei MCP for Grid-Based Minesweeper Operation

**Feature**: 018-saolei-mcp
**Date**: 2026-07-20
**Status**: Phase 0 — resolves all spec Technical Context unknowns; grounds Phase 1 contracts.

This document records the decisions made for the saolei MCP feature, with rationale and alternatives. It is the authoritative reference for the data model and contracts. Citations follow Constitution §I (repo-relative paths or full URLs).

## Decision Index

1. [MCP runtime topology — loopback internal](#d1-mcp-runtime-topology)
2. [MCP client — `@langchain/mcp-adapters`](#d2-mcp-client)
3. [MCP server — `@modelcontextprotocol/sdk` Node streamable HTTP, path-routed](#d3-mcp-server)
4. [`saolei_init` — F2 keypress via `KeyboardPressPart`](#d4-saolei_init--f2-keypress)
5. [Cell operations — window-message mouse via `MouseMoveAndClickPart`](#d5-cell-operations--window-message-mouse)
6. [Fixed board geometry — 24/200/32 client-coordinate formula](#d6-fixed-board-geometry)
7. [Proto Part/enum extension design](#d7-proto-partenum-extension-design)
8. [Validation result surfacing — normal MCP results, not `isError`](#d8-validation-result-surfacing)
9. [Built-in skill injection — append to systemPrompt at bind time](#d9-built-in-skill-injection)
10. [Minesweeper rules — confirmation of validation constraints](#d10-minesweeper-rules-confirmation)

---

## D1. MCP runtime topology

**Decision**: Loopback internal — the agent process hosts the MCP server on a localhost port; the agent's own LangGraph/LangChain turn loop is the MCP client that connects to the per-session MCP endpoint. (Spec Clarifications Round 1, Q1.)

**Rationale**: The user mandated *"mcp 只是 agent 内部与 llm 交互的方式"* (MCP is the agent's internal LLM-interaction method) and *"agent 与 desktop 和其他服务的交互不受其影响"* (agent↔desktop interaction unaffected). Loopback keeps the entire MCP surface inside the agent boundary, reuses the existing supervised turn loop (`createAgent`, LangGraph checkpointing, streaming, per-session mutex, `OperationBridge`), and the per-session URL path gives a clean session→bridge+state binding.

**Alternatives considered**:
- *External client* (a client outside the agent process connects over localhost): rejected — contradicts "internal", would require a new LLM-driving path and abandon the established supervised loop + checkpointing.
- *In-process tool registration only* (no MCP, register saolei tools as `StructuredToolInterface` like mouse tools): rejected — the user explicitly wants MCP as the standardized tool surface and a per-session MCP server path.

---

## D2. MCP client

**Decision**: Use the official **`@langchain/mcp-adapters`** `MultiServerMCPClient` with the streamable-HTTP transport and a URL-only server entry. `getTools()` returns LangChain `DynamicStructuredTool[]` fed directly to the existing `createAgent({ model, tools })` call (`projects/game/agent/src/llm.ts:205-211`). No custom MCP client is built.

**Evidence** (LangChain JS docs, https://docs.langchain.com/oss/javascript/langchain/mcp):
```ts
const client = new MultiServerMCPClient({
  saolei: {
    transport: "http",
    url: "http://localhost:{mcp_port}/internal/mcp/{session_id}",
  },
});
const tools = await client.getTools();   // DynamicStructuredTool[]
const agent = createAgent({ model, tools });
```
- HTTP/streamable transport supports a URL-only config (the common "MCP server config is mostly just an HTTP path" pattern the user referenced).
- `getTools()` output composes directly with the agent's current `createAgent` wiring — no architectural change to the turn loop.
- This is the same pattern LangChain DeepAgent uses (`.mcp.json` with `{ "type": "http", "url": ... }`, auto-discovered, embedded client — https://docs.langchain.com/oss/javascript/deepagents/code/mcp-tools), confirming it is the community-standard embedded-MCP-client approach.

**Per-session construction**: `MultiServerMCPClient` is constructed inside the adapter factory (`AdapterFactory`, `projects/game/agent/src/llm.ts:165-171`) per session bind, with the URL carrying that session's id. The MCP client lifecycle follows the adapter lifecycle (re-created on profile switch / `RefreshAgent`).

**Adapter error behavior (important)**: when an MCP tool returns `isError: true`, the **TypeScript adapter raises a `ToolException`** and does NOT return the error to the model as a failed tool message (unlike the Python adapter) — see https://github.com/langchain-ai/langchainjs/blob/main/libs/langchain-mcp-adapters/README.md. This drives Decision D8.

**Alternatives considered**:
- *Hand-rolled MCP client over `@modelcontextprotocol/sdk` `Client`*: rejected — reinvents the adapter, more code, diverges from the standard pattern.
- *`loadMcpTools` with a self-managed `Client`*: rejected — `MultiServerMCPClient` already manages connection lifecycle, reconnection, and multi-server merging with less code.

---

## D3. MCP server

**Decision**: Implement the MCP server with **`@modelcontextprotocol/sdk`** (the `@modelcontextprotocol/node` Node runtime) using `NodeStreamableHTTPServerTransport` on an Express (or Node `http`) server. One HTTP listener on `{mcp_port}`; route `/internal/mcp/{session_id}` to a lazily-created, session-bound `McpServer` instance whose tool handlers close over that session's `OperationBridge` and game state.

**Evidence** (MCP TS SDK — https://github.com/modelcontextprotocol/typescript-sdk, docs `docs/serving/sessions-state-scaling.md`):
- `NodeStreamableHTTPServerTransport` supports stateful sessions with `sessionIdGenerator` and per-session transport maps.
- An Express route can dispatch POST/GET/DELETE to the correct transport by key (the SDK's own example keys by `Mcp-Session-Id` header; we key by the **path** `{session_id}` instead, which is the dominion-session identifier).
- `McpServer.registerTool(name, { description, inputSchema }, handler)` registers a tool whose handler runs server-side with closure access to the session's bridge + state.

**Per-session binding**: a `Map<sessionId, { mcpServer, transport }>` is maintained by the MCP host. On first request to `/internal/mcp/{session_id}`, the host looks up the `SessionAgent` via `SessionAgentStore.get(sessionId)` (`projects/game/agent/src/session-agent.ts:148-173`), obtains its `OperationBridge` (`session-agent.ts:88-90`), creates a fresh game-state model, constructs a `McpServer` whose tool handlers close over that bridge + state, and connects a new transport. The `Mcp-Session-Id` header still works for the MCP-level sub-session but is independent of the dominion session (the client is the agent's own loopback, one connection per dominion session).

**Unknown-session rejection** (FR-003): a request whose `{session_id}` is not in `SessionAgentStore` returns 404 (`{ jsonrpc: "2.0", error: { code: -32001, message: "Session not found" } }`), matching the SDK's own unknown-session response.

**Lifecycle**: the MCP host starts alongside the gRPC server in `projects/game/agent/src/server.ts` (a second listener). Port is configurable via env (e.g. `MCP_PORT`, default chosen at plan time). The MCP server introduces no new session-creation path; it only attaches to sessions already created by the gRPC `Connect` flow.

**Alternatives considered**:
- *Single `McpServer` with session context threaded via the MCP-Session-Id header*: rejected — the user explicitly wants path-based routing `/internal/mcp/{session_id}`; per-session `McpServer` instances give clean closure-based binding and isolation (FR-026).
- *stdio transport*: rejected — the client is an HTTP loopback client; stdio would require a subprocess per session.

---

## D4. `saolei_init` — F2 keypress

**Decision**: `saolei_init(width, height)` starts a new game by dispatching an **F2 keypress** to the bound window (F2 is the standard minesweeper new-game shortcut), via a new generic `KeyboardPressPart` (FR-004a, FR-006). It does **not** click a new-game face button. It also initializes the per-session game-state model: the model supplies the grid dimensions (`width`/`height` in cells, read from the screenshot); the board **pixel** geometry is fixed (D6). (Spec Clarifications Round 2, D2.)

**Rationale**: F2 is the canonical minesweeper "new game" key; using it avoids coupling the agent to the specific smiley-face button's pixel coordinates (which vary across minesweeper implementations and window sizes). A keyboard operation is the natural primitive for this. The desktop currently has only a stub `ExecuteKeyPress(keyCodes string)` (`projects/game/desktop/internal/operation/execute_windows.go:75-81`); this feature implements it for real (posting the key message to the bound window).

**Keyboard execution on the desktop**: the desktop posts the key to the bound HWND via window messages (`PostMessage`/`SendMessage` with `WM_KEYDOWN`/`WM_KEYUP`, or `SendInput` with `KEYBDINPUT`). Window-message posting is preferred (consistent with D5, no foreground-focus requirement, no cursor side effects). The desktop owns the concrete Win32 mechanism (spec FR-004: "具体的鼠标与键盘操作的实现方式有 desktop 负责").

**Alternatives considered**:
- *Click the new-game face button* (prior clarification Q2 default): superseded — F2 is implementation-agnostic and avoids extra geometry.

---

## D5. Cell operations — window-message mouse

**Decision**: `saolei_click`, `saolei_flag`, `saolei_chord_click` dispatch via a new generic **`MouseMoveAndClickPart`** carrying window-client coordinates, a `MouseClickAction`, and `MouseInputMethod = WINDOW_MESSAGE` (FR-004b/c, FR-007/008/009). The desktop posts the appropriate `WM_LBUTTON*` / `WM_RBUTTON*` messages to the bound HWND with the coordinate packed into `lParam`, **without moving the OS cursor**. (Spec Clarifications Round 2, D3.)

**Rationale**: The user stated *"因为鼠标 icon 会挡住格子"* (the mouse cursor icon blocks the cells). The model reads cell states from screenshots, so a real cursor sitting on a cell would visually corrupt that cell's reading. Window-message mouse (PostMessage `WM_*`) delivers the click to the window without any cursor movement, keeping the screenshot clean. A **combined move+click** Part is used because window-message clicks carry the target coordinate in the message itself (there is no separate "move cursor" step for window messages — unlike `SIMULATED`, which needs `SetCursorPos` then `SendInput`).

**Current desktop input mechanism** (to be extended, not replaced): the desktop uses raw Win32 via `syscall.NewLazyDLL("user32.dll")` — `SetCursorPos` for move and `SendInput` with `MOUSEINPUT` for clicks (`projects/game/desktop/internal/operation/execute_windows.go`, `execute_v2.go`). There is **no** existing `PostMessage`/`SendMessage`/`WM_*` code and **no** existing keyboard impl. This feature adds a `WINDOW_MESSAGE` code path alongside the existing `SIMULATED` path, selected by the new `MouseInputMethod` enum. Existing mouse tools default to `SIMULATED`, preserving current behavior (FR-004c).

**Action → message mapping** (desktop responsibility, FR-004d):
| `MouseClickAction` | `WM_*` message sequence |
|---|---|
| `LEFT_CLICK` | `WM_LBUTTONDOWN` → `WM_LBUTTONUP` |
| `LEFT_DOUBLE_CLICK` | `WM_LBUTTONDOWN`↔`UP` ×2 |
| `RIGHT_CLICK` | `WM_RBUTTONDOWN` → `WM_RBUTTONUP` |
| `RIGHT_DOUBLE_CLICK` | `WM_RBUTTONDOWN`↔`UP` ×2 |
| `LEFT_RIGHT_PRESS` | `WM_LBUTTONDOWN` → `WM_RBUTTONDOWN` (then ups) — chord |

**Alternatives considered**:
- *Reuse `MouseMovePart` + `MouseClickPart` with a `WINDOW_MESSAGE` flag*: rejected — for window messages there is no meaningful "move" step; a combined atomic Part matches the semantics and avoids dispatching a no-op move.
- *Keep simulated cursor but hide it*: rejected — hiding the OS cursor system-wide is invasive and out of scope; window messages are the clean solution.

---

## D6. Fixed board geometry

**Decision**: Grid→window-client-coordinate translation uses a **fixed** formula (no geometry supplied at init). Origin offset = **24 px** from the window's left edge and **200 px** from the top; each cell is **32×32 px**. Cell `(x, y)` center in window-client coordinates:
```
center_x = 24 + x*32 + 16
center_y = 200 + y*32 + 16
```
(Spec Clarifications Round 2, D4; FR-007.) These are **window-client coordinates** — used directly by `WINDOW_MESSAGE` mouse (packed into `lParam`), with **no** screen-offset addition (unlike `SIMULATED`, which adds window bounds via `operation.ScreenshotToScreenCoords` at `projects/game/desktop/internal/operation/convert.go:10-14`).

**Rationale**: The user supplied these constants directly; they correspond to the standard Microsoft Minesweeper window layout. Hardcoding them keeps the saolei MCP purpose-built and avoids per-session calibration. The constants live in one place (the saolei MCP integration) so future board layouts can be parameterized without touching the tool contracts.

**Board dimensions**: the grid dimensions (`width`/`height` in cells) are supplied by the model at `saolei_init` (read from the screenshot); they are not fixed. The board **pixel** geometry (origin offset, cell size) is fixed (D6). Out-of-bounds coordinates (outside `[0,width)×[0,height)`) are rejected per FR-016.

**Alternatives considered**:
- *Geometry supplied at `saolei_init` by the model*: superseded — the user provided fixed constants, removing the need for the model to measure the board.

---

## D7. Proto Part/enum extension design

**Decision**: Extend `projects/game/game.proto` as follows (detailed field numbers in `contracts/proto-operation-contract.md`):
1. Add `KeyboardPressPart keyboard_press` to the `Part.kind` oneof.
2. Add `MouseMoveAndClickPart mouse_move_and_click` to the `Part.kind` oneof.
3. Add a `MouseInputMethod` enum (`MOUSE_INPUT_METHOD_UNSPECIFIED = 0`, `_SIMULATED = 1`, `_WINDOW_MESSAGE = 2`).
4. Add a `MouseInputMethod method` field to `MouseMovePart`, `MouseClickPart`, and `MouseMoveAndClickPart`.

**Naming/numbering conventions** (verified from `projects/game/game.proto:201-234, 253-309`): proto3 native enums; `SCREAMING_SNAKE_CASE` with the enum-name prefix; first value `{ENUM}_UNSPECIFIED = 0`. Part oneof field numbers continue from the next free slot (`keyboard_press = 7`, `mouse_move_and_click = 8`).

**Backward compatibility**:
- New enum field defaults to `UNSPECIFIED`/0; the desktop treats `UNSPECIFIED` as `SIMULATED` (the existing behavior) so old mouse Parts keep working.
- The `Part.kind` oneof is purely additive; consumers that ignore unknown oneof members (the desktop's `GetMouseMove()`/`GetMouseClick()` accessors) are unaffected.
- TS types regenerate automatically via `ts_proto_library("game_types")` (`projects/game/agent/BUILD.bazel:11-15`); Go types regenerate via the existing `dominion/projects/game` proto import on the desktop.

**Tool-agnostic protocol** (FR-004): `AgentFrame` and `Part` declare **operations** (keyboard, mouse-move-and-click), never saolei semantics. The new Parts are generic input primitives reusable by any future tool, not saolei-specific. This is already the case structurally (`AgentFrame` carries `PartBlock` of `Part`s; `game.proto:346-365, 246-263`).

---

## D8. Validation result surfacing

**Decision**: saolei tools surface **validation rejections as normal MCP tool results** (a text content block describing the rejection, `isError: false`), NOT as MCP errors (`isError: true`). The model sees the rejection reason and can immediately retry (consistent with Clarification Q3 → A: rejected operations do not lock the alternation).

**Rationale**: the `@langchain/mcp-adapters` TypeScript adapter raises a `ToolException` on `isError: true` and does **not** return the error to the model as a tool message (D2). For saolei, the model must perceive rejection reasons to self-correct, so tools return structured text results. Accepted operations return the desktop tool result (status + optional screenshot); rejected operations return `{ accepted: false, reason: "..." }`-style text.

**Result shapes** (authoritative in `contracts/mcp-tool-contract.md`):
- Operation accepted + dispatched: text status + screenshot (image content block) from the desktop `ToolResultPart`.
- Operation rejected (validation): text describing the violated rule; no dispatch.
- `saolei_update` accepted: text confirmation.
- `saolei_update` rejected: text describing the violated rule; state unchanged.

---

## D9. Built-in skill injection

**Decision**: Built-in-skill injection works by **loading the `SKILL.md` body at session-bind time and appending it to the system prompt** for sessions whose profile `mcp_names` includes `saolei`. A small `mcp_name → built-in skill` registry maps `"saolei" → src/skill/saolei/SKILL.md`. No new prompt-transport mechanism is introduced; the existing `systemPrompt` plumbing (`session-agent.ts:119-127` → `AdapterFactory` → `AgentAdapterImpl`) carries the augmented prompt.

**Current state**: `projects/game/agent/src/skill/` has only a README (no skills authored, no loader, no injection). The agent's `PromptClient.getProfile()` (`projects/game/agent/src/prompt-client.ts:158-180`) currently extracts only `model`, `systemPrompt`, `toolNames` — `mcp_names`/`skill_names` are fetched but ignored. This feature:
1. Extends `ProfileData`/`ProfileResult` to carry `mcpNames`.
2. Adds a built-in-skill loader that reads `src/skill/{name}/SKILL.md` files (bundled into the agent image).
3. In the adapter factory, when `mcpNames` includes `saolei`, loads the saolei SKILL.md and appends its body to the systemPrompt before constructing `AgentAdapterImpl`.

**Scope guard** (FR-025): the registry is limited to the saolei mapping; the user-created `Skill` proto resource (PromptService CRUD, `game.proto:140-167, 438-454`) is untouched.

**SKILL.md format**: follows the repo contract `specs/020-agent-resources-layout/contracts/skill-md-format.md` (agentskills.io open standard, OpenCode-recognized subset). Example shape modeled on `.opencode/skills/signoz/SKILL.md`.

**Alternatives considered**:
- *Separate "skills" message channel to the model*: rejected — adds a new prompt-transport path; appending to systemPrompt reuses existing plumbing and is sufficient for guidance-style skills.
- *Inject skills via the MCP server (as MCP resources)*: rejected — over-engineered for guidance text; systemPrompt append is simpler and matches how skills function (steerage, not executable code).

---

## D10. Minesweeper rules — confirmation

**Decision**: The validation rules (FR-013..FR-019) are confirmed against standard minesweeper rules. Sources:
- Wikipedia: https://en.wikipedia.org/wiki/Minesweeper_(video_game) — left-click reveal; 0-cell cascade; right-click flag; chord = left+right on a satisfied number; first-click safety.
- minesweeper.now: https://minesweeper.now/help/gameplay — "Chord — Click a revealed number when the exact number of adjacent flags are placed. Auto-reveals all remaining neighbors."
- Rare Pike chord technique: https://rarepike.com/minesweeper/chord-technique/ — a chord only works when adjacent flag count == the number; a misplaced flag during a chord reveals a mine and ends the game.

**Rule → FR mapping**:
| Minesweeper rule | FR |
|---|---|
| Left-click reveals an unopened cell; clicking a mine ends the game | FR-007, FR-018 |
| Clicking a 0 (blank) cascades to reveal all connected safe cells | FR-013 (connectivity), FR-018 |
| Right-click toggles a flag on an unopened cell | FR-008, FR-014 |
| Chord = left+right on a number whose adjacent flags == the number | FR-009, FR-015 |
| Misplaced flag on a chord reveals a mine (game over) | FR-015 (mine-hit exception), FR-019 |

**Connectivity** (FR-013, FR-015): 8-connectivity (horizontal/vertical/diagonal), consistent with minesweeper adjacency. The "updated number cells must be connected through the target neighborhood" rule encodes the cascade: a 0-cell click reveals a connected region of cells, and the number cells revealed are exactly the boundary of that region (connected through the revealed 0-cells and each other).

**Mine-state semantics** (confirmed, Round 2 D6): HIT_MINE = mine directly triggered by the current operation; MINE = mines shown at game end that were NOT triggered by the current operation (e.g., on a loss all other mines are revealed).

**Out of scope for validation completeness** (FR-017): first-click safety guarantees, win-condition auto-flagging, mine-counter consistency. These MAY be added later without changing the tool contracts.

---

## Summary of dependencies to add

| Component | Dependency | Source |
|---|---|---|
| Agent (TS) — MCP client | `@langchain/mcp-adapters` | https://docs.langchain.com/oss/javascript/langchain/mcp |
| Agent (TS) — MCP server runtime | `@modelcontextprotocol/sdk` (Node) | https://github.com/modelcontextprotocol/typescript-sdk |
| Agent (TS) — HTTP server | Node `http` or `express` (choose at plan time; Express matches SDK examples) | — |

All TS/JS dependency versions MUST be pinned in the root `pnpm-workspace.yaml` catalog (per `AGENTS.md`). The agent's `package.json` (`projects/game/agent/package.json`) references them via `catalog:`.

## References

- LangChain MCP adapters: https://docs.langchain.com/oss/javascript/langchain/mcp
- langchain-mcp-adapters README: https://github.com/langchain-ai/langchainjs/blob/main/libs/langchain-mcp-adapters/README.md
- LangChain DeepAgent MCP: https://docs.langchain.com/oss/javascript/deepagents/code/mcp-tools
- MCP TS SDK: https://github.com/modelcontextprotocol/typescript-sdk
- MCP spec (transport): https://modelcontextprotocol.io/specification
- Minesweeper rules: https://en.wikipedia.org/wiki/Minesweeper_(video_game) ; https://minesweeper.now/help/gameplay ; https://rarepike.com/minesweeper/chord-technique/
- Repo: `projects/game/agent/src/{server.ts,session-agent.ts,operation-bridge.ts,llm.ts,prompt-client.ts}`, `projects/game/agent/BUILD.bazel`, `projects/game/game.proto`, `projects/game/desktop/{app.go,internal/operation/execute_windows.go,internal/operation/convert.go,internal/capture/window.go}`, `specs/020-agent-resources-layout/contracts/{directory-layout.md,skill-md-format.md}`, `specs/013-agent-game-tools/spec.md`.
