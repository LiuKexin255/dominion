# Feature Specification: Saolei MCP for Grid-Based Minesweeper Operation

**Feature Branch**: `018-saolei-mcp`

**Created**: 2026-07-20

**Status**: Draft

**Input**: User description: "新增 saolei mcp，来代替直接使用 mouse 作为操作方式。1) agent 新增一个 localhost service 作为 mcp server，为每个 session agent 单独配置 mcp server path（localhost:mcp_port/internal/mcp/{session_id}）。2) 将 mcp server 逻辑绑定到 session 的实现，复用 OperationBridge。3) mcp 只是 agent 内部与 llm 交互的方式，agent 与 desktop 和其他服务的交互不受其影响。4) saolei mcp 工具：saolei_init / saolei_click / saolei_flag / saolei_double_click / saolei_update。5) saolei mcp 维护游戏状态；saolei_update 在每次操作后手动更新状态，操作与更新交替进行（init 除外）。6) 对游戏状态做合法性校验，校验不通过则拒绝。7) 检索扫雷规则补充约束。8) desktop 编辑 agentprofile 增加 mcp 选项；为 saolei mcp 增加内部 skill，选择 saolei mcp 时自动注入。"

## Clarifications

### Session 2026-07-20

- Q: Who consumes the per-session MCP endpoint (MCP runtime topology)? → A: Loopback internal — the agent hosts the MCP server, and the agent's own existing LangGraph/LangChain turn loop connects to the per-session MCP endpoint as a loopback MCP client (via an MCP-client adapter), surfacing the saolei tools to the model. The supervised turn loop, checkpointing, streaming, and OperationBridge are reused; the per-session MCP path provides session → bridge + game-state binding.
- Q: Does `saolei_init` reset the OS-level game on the desktop, or only initialize agent-side state? → A: Both — `saolei_init` dispatches a "new game" (restart) click on the desktop via OperationBridge AND resets the agent-side game-state model. The new-game button geometry is supplied at init. `saolei_init` remains exempt from the operate-then-update alternation (no `saolei_update` required after it).
- Q: Does a validation-rejected operation (pre-dispatch) require a `saolei_update` before the next attempt? → A: No — a rejected operation does NOT set the "operation pending update" flag; the model may immediately retry with a valid operation. Only a successfully dispatched operation triggers the "must update first" requirement.

### Session 2026-07-20 (Round 2: Plan-Input Decisions)

- D1 (MCP client): The agent's loopback MCP client MUST be the official `@langchain/mcp-adapters` `MultiServerMCPClient` (HTTP/streamable transport, URL-only config `{ transport: "http", url: "http://localhost:{mcp_port}/internal/mcp/{session_id}" }`), whose `getTools()` returns LangChain tools fed to the existing `createAgent({ model, tools })` loop. No custom MCP client is built. This follows the common embedded-MCP-client pattern (confirmed by LangChain DeepAgent's `.mcp.json` HTTP-URL config).
- D2 (`saolei_init` mechanism): `saolei_init(width, height)` triggers a new game by sending an **F2 keypress** to the bound window (F2 is the minesweeper new-game shortcut) via a new generic `KeyboardPressPart` — it does NOT click a new-game face button — and sizes the game-state grid to the given `width`×`height` (cell counts supplied by the model from the screenshot). Board **pixel** geometry remains **fixed** (not supplied at init): origin offset 24 px from the window's left edge and 200 px from the top, with 32×32 px cells.
- D3 (cell-operation mechanism): `saolei_click`, `saolei_flag`, and `saolei_chord_click` MUST dispatch via a new generic `MouseMoveAndClickPart` with implementation method = window message (PostMessage-style `WM_*` to the bound HWND), NOT simulated cursor input, because the real cursor icon would visually block cells in the screenshot the model reads.
- D4 (grid→pixel formula): for cell `(x, y)` the operation target in **window-client coordinates** is `(24 + x*32 + 16, 200 + y*32 + 16)` (cell center). Window-client coordinates are used directly by the desktop for window-message mouse (no screen-offset addition).
- D5 (proto extensions): Add `KeyboardPressPart` and `MouseMoveAndClickPart` to the `Part.kind` oneof; add a `MouseInputMethod` enum (`SIMULATED` | `WINDOW_MESSAGE`) to `MouseMovePart`, `MouseClickPart`, and `MouseMoveAndClickPart`. `AgentFrame` and `Part` remain **tool-agnostic** — they declare operations only, never coupling to saolei tool semantics. Existing mouse tools default to `SIMULATED` (current behavior preserved).
- D6 (mine-state semantics confirmed): HIT_MINE = the mine directly triggered by the current operation; MINE ("未命中地雷") = mines shown at game end that were NOT triggered by the current operation. (Matches the spec's FR-010/Assumptions exactly.)
- D7 (chord tool renamed + disambiguated): The chord tool is renamed `saolei_double_click` → **`saolei_chord_click`**. Its operation is a **single simultaneous left+right button press** (one atomic chord) — NOT two separate clicks and NOT a left double-click. It maps to the existing `MouseClickAction = LEFT_RIGHT_PRESS` and is dispatched as one `MouseMoveAndClickPart{ click: LEFT_RIGHT_PRESS, method: WINDOW_MESSAGE }`. (The original input's `saolei_double_click` wording is superseded everywhere except the verbatim Input quote above.)

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Configure a Saolei MCP Profile and Expose Saolei Tools to the LLM (Priority: P1)

A desktop operator edits an agent profile and enables the "saolei" MCP option. When a session runs under that profile, the agent hosts a localhost MCP server that exposes the saolei tools (`saolei_init`, `saolei_click`, `saolei_flag`, `saolei_chord_click`, `saolei_update`) on an endpoint path unique to that session. The model interacts with the minesweeper game through these tools rather than through raw mouse tools.

**Why this priority**: Profile configuration plus per-session MCP tool exposure is the foundation; none of the gameplay, validation, or skill-injection stories can be exercised without it.

**Independent Test**: Configure a profile with the saolei MCP selected, start a session, connect an MCP client to that session's MCP path, and verify the five saolei tools are discoverable and callable while raw mouse tools are not exposed to the model.

**Acceptance Scenarios**:

1. **Given** a profile whose MCP option includes "saolei", **When** a session starts under that profile, **Then** the agent exposes an MCP endpoint at a localhost path unique to that session (`/internal/mcp/{session_id}`).
2. **Given** an MCP client connected to the session's MCP path, **When** it lists available tools, **Then** exactly the five saolei tools are available.
3. **Given** a saolei-enabled profile, **When** the model selects tools during a turn, **Then** the raw mouse tools (`mouse_move`, `mouse_click`) are not exposed to the model — saolei tools replace them as the LLM-facing operation channel.

---

### User Story 2 - Left-Click Reveal With Manual State Update (Priority: P1)

The model calls `saolei_init` to set up the board, then calls `saolei_click` on an unopened, unflagged cell. The agent dispatches the actual left-click to the desktop through the existing per-session desktop channel and returns the result. The model must then call `saolei_update` with the cell states it observes before any further operation is permitted; the update is validated against the game rules.

**Why this priority**: This is the core supervised gameplay loop. It exercises board initialization, real desktop dispatch, server-side state maintenance, and the operate-then-update alternation.

**Independent Test**: `init` → `click` → (tool result returned) → `update` with the revealed connected number cells → confirm the next operation becomes allowed.

**Acceptance Scenarios**:

1. **Given** an initialized board, **When** `saolei_click` targets an unopened, unflagged cell, **Then** a left mouse click is dispatched to the desktop at that cell's pixel position and a tool result is returned to the model.
2. **Given** a completed click, **When** the model calls `saolei_update` with a rule-consistent batch, **Then** the state model is updated and the next operation becomes allowed.
3. **Given** a click was just performed, **When** the model attempts another operation before calling `saolei_update`, **Then** the attempt is rejected — operations and updates must alternate.
4. **Given** `saolei_init` was called, **When** the model performs the first operation, **Then** no prior `saolei_update` is required (initialization needs no manual update).

---

### User Story 3 - Flag Toggle and Chord (Double-Click) With Validation (Priority: P2)

The model flags and unflags cells via `saolei_flag` (toggling only between the initial and flagged states), and performs a chord via `saolei_chord_click` — a single simultaneous left+right press on a satisfied number cell (a non-0 number whose adjacent flag count equals its number). Validation enforces the flag-toggle constraint, the chord preconditions, and the post-chord update shape (target-adjacent flags unchanged; other neighbors updated except on a mine hit; updated number cells connected through the target neighborhood).

**Why this priority**: These are the advanced operations and carry the most intricate validation rules; they build on the P1 loop.

**Independent Test**: Place flags around a number until satisfied, chord it, then `saolei_update`; separately attempt illegal flags/chords and verify rejection.

**Acceptance Scenarios**:

1. **Given** an unopened cell, **When** `saolei_flag` toggles it, **Then** the subsequent `saolei_update` is accepted only if the cell transitions solely between the initial and flagged states.
2. **Given** a non-0 number cell whose adjacent flag count equals its number, **When** `saolei_chord_click` targets it, **Then** a single simultaneous left+right press (a chord) is dispatched to the desktop.
3. **Given** a chord was performed, **When** the model calls `saolei_update`, **Then** target-adjacent flagged cells are unchanged, other target-adjacent non-number cells are updated (mine-hit excepted), and every connected component of updated number cells includes at least one cell adjacent to the chord target.

---

### User Story 4 - Validation Rejects Illegal Operations and Updates (Priority: P2)

The saolei MCP validates every operation and every update against the game-state rules and rejects illegal ones with a clear error, without dispatching to the desktop and without mutating the board state.

**Why this priority**: Validation is what makes the tool dependable and prevents the model from making nonsensical or rule-violating moves.

**Independent Test**: Attempt illegal operations (click a flagged cell, flag an opened cell, chord an unsatisfied number, update disconnected cells, update out-of-bounds coordinates) and confirm each is rejected with state unchanged.

**Acceptance Scenarios**:

1. **Given** a flagged cell, **When** `saolei_click` targets it, **Then** the operation is rejected before any desktop dispatch.
2. **Given** a number cell whose adjacent flag count does not equal its number, **When** `saolei_chord_click` targets it, **Then** the operation is rejected.
3. **Given** a `saolei_click` update batch, **When** the updated number cells are not connected, **Then** the update is rejected and the state is left unchanged.
4. **Given** any `saolei_update`, **When** it contains coordinates outside the board or statuses inconsistent with the operation performed, **Then** it is rejected and the state is left unchanged.

---

### User Story 5 - Built-in Saolei Skill Auto-Injected With the Profile (Priority: P3)

When the "saolei" MCP is selected on a profile, a built-in skill (authored under the agent's internal skill directory) is automatically attached to sessions using that profile, so the model receives minesweeper-specific guidance: the rules, the grid coordinate convention (top-left origin), and the operation/update alternation requirement.

**Why this priority**: This materially improves model effectiveness, but it depends on a built-in-skill injection mechanism that does not yet exist in the runtime, so it is sequenced after the gameplay core.

**Independent Test**: Select saolei on a profile, start a session, and verify the built-in saolei skill content reaches the agent's prompt context; verify it is absent for a non-saolei profile.

**Acceptance Scenarios**:

1. **Given** a profile whose MCP option includes "saolei", **When** the agent binds the session, **Then** the built-in saolei skill is loaded and injected into the agent context.
2. **Given** a profile without saolei selected, **When** the agent binds the session, **Then** the saolei skill is not injected.

---

### Edge Cases

- What happens when an MCP client connects to a session path that does not correspond to a live session? The connection is rejected (session not found); no game state is created for unknown sessions.
- What happens when `saolei_init` is called again on an already-initialized session? The F2 keypress is re-dispatched to the desktop and the board state is reset to all-initial (the new initialization discards prior state).
- What happens when a click is dispatched but the desktop is disconnected? The existing desktop-channel behavior surfaces a failed result; the operation is reported as failed and the state is not advanced until a valid `saolei_update`.
- What happens when `saolei_update` contains coordinates outside the board? The update is rejected; the state is unchanged.
- What happens when the model skips `saolei_update` and attempts another operation? The attempt is rejected; the operate-then-update alternation is enforced.
- What happens when an operation is rejected by validation before dispatch? The "operation pending update" flag is not set and the board state is unchanged, so the model may immediately retry with a valid operation (no intervening `saolei_update` required).
- What happens when a chord hits a mine (a flag was misplaced)? Only the hit-mine cell need be reflected in the update; the game-ending semantics apply and the other neighbors are not required to be updated in that batch.
- What happens when the validation rules are not exhaustive against every minesweeper edge case? The rules are explicitly extensible; this feature ships the listed rules and allows future additions without changing the tool contracts.

## Requirements *(mandatory)*

### Functional Requirements

**MCP server hosting and session binding**

- **FR-001**: The agent process MUST host a localhost MCP server (HTTP-based MCP transport) on a port separate from the existing gRPC server, exposing the saolei tools under a per-session path of the form `/internal/mcp/{session_id}`.
- **FR-002**: The MCP server MUST bind each session path to that session's existing OperationBridge (the same session-scoped desktop-channel component already used by mouse tools); the feature MUST NOT introduce a parallel desktop-binding mechanism.
- **FR-002a**: The agent's own model turn loop MUST consume the saolei tools from the per-session MCP endpoint as a loopback MCP client (i.e., the saolei tools are surfaced to the model through the MCP server bound to that session, not via direct in-process tool registration). The existing supervised turn loop, checkpointing, and streaming MUST be reused for saolei profiles.
- **FR-002b**: The loopback MCP client MUST be the official `@langchain/mcp-adapters` `MultiServerMCPClient` configured with the streamable-HTTP transport and a URL-only server entry (`http://localhost:{mcp_port}/internal/mcp/{session_id}`); its `getTools()` output MUST feed the existing `createAgent({ model, tools })` loop. No custom MCP client MAY be built.
- **FR-003**: An MCP connection to a path whose `{session_id}` does not correspond to a live session MUST be rejected; no game state is created for unknown sessions.
- **FR-004**: The agent-to-desktop operation protocol MUST remain **tool-agnostic**: `AgentFrame` and `Part` declare generic input operations only and MUST NOT couple to saolei tool semantics. The desktop gains two generic input primitives — a keyboard operation (for `saolei_init`'s F2) and a window-message mouse operation (for the saolei cell operations) — but the desktop MUST NOT become saolei-aware; it executes generic keyboard/mouse Parts and selects the implementation method declared on each Part.

**Operation protocol extensions (proto)**

- **FR-004a**: The `Part.kind` oneof in `projects/game/game.proto` MUST add a `KeyboardPressPart keyboard_press` variant carrying a key identifier (e.g., the F2 key) used by `saolei_init`. The desktop MUST execute keyboard Parts by posting the corresponding key message to the bound window.
- **FR-004b**: The `Part.kind` oneof MUST add a `MouseMoveAndClickPart mouse_move_and_click` variant carrying window-client pixel coordinates, a `MouseClickAction`, and a `MouseInputMethod`, used atomically (move + click) by the saolei cell operations.
- **FR-004c**: A new `MouseInputMethod` enum (`UNSPECIFIED` | `SIMULATED` | `WINDOW_MESSAGE`) MUST be added to `MouseMovePart`, `MouseClickPart`, and `MouseMoveAndClickPart`. `SIMULATED` denotes the existing real-cursor behavior (SetCursorPos + SendInput); `WINDOW_MESSAGE` denotes posting `WM_*` messages to the bound window's HWND without moving the cursor. Existing mouse tools (`mouse_move`, `mouse_click`) MUST default to `SIMULATED` so current behavior is preserved.
- **FR-004d**: For `WINDOW_MESSAGE` mouse operations, the desktop MUST interpret the Part's coordinates as **window-client coordinates** and post the appropriate `WM_LBUTTONDOWN`/`WM_LBUTTONUP`/`WM_RBUTTONDOWN`/`WM_RBUTTONUP`/`WM_MBUTTONDOWN`/`WM_MBUTTONUP` messages (per the action) with the coordinate packed into `lParam`, without adding the window's screen offset and without moving the OS cursor.

**Tool set and operation semantics**

- **FR-005**: The saolei MCP MUST expose exactly five tools: `saolei_init`, `saolei_click`, `saolei_flag`, `saolei_chord_click`, `saolei_update`.
- **FR-006**: `saolei_init(width, height)` MUST (a) dispatch an **F2 keypress** to the desktop (the minesweeper new-game shortcut) via a generic `KeyboardPressPart` through OperationBridge, and (b) initialize the per-session game-state model to a `width`×`height` grid (cell counts, NOT pixels) of cells all in the initial state, using the **fixed** board pixel geometry for later grid→pixel translation (origin offset 24 px from the window's left edge and 200 px from the top; 32×32 px cells — see FR-007). `width` is the column count (x ranges `0..width-1`); `height` is the row count (y ranges `0..height-1`). `saolei_init` MUST remain exempt from the operate-then-update alternation — no `saolei_update` is required after initialization. The F2 keypress is a direct desktop dispatch and is not subject to the cell-operation validation rules (FR-013..FR-015).
- **FR-007**: `saolei_click(x, y)` MUST dispatch a left click to the desktop at the target cell's center, computed in **window-client coordinates** by the fixed formula `(24 + x*32 + 16, 200 + y*32 + 16)`, using a generic `MouseMoveAndClickPart` with implementation method = WINDOW_MESSAGE (not simulated cursor input). It MUST accept only a target that is in the initial state and not flagged.
- **FR-008**: `saolei_flag(x, y)` MUST toggle the flag marker on a cell that is in the initial state (flag/unflag toggles only between the initial and flagged states), dispatching the corresponding right click to the desktop via `MouseMoveAndClickPart` (WINDOW_MESSAGE) at the cell's window-client center per the FR-007 formula.
- **FR-009**: `saolei_chord_click(x, y)` MUST dispatch a **single simultaneous left+right button press** (a chord — one atomic operation, NOT two separate clicks and NOT a left double-click) on a non-0 number cell whose adjacent flagged-cell count equals the cell's number, via `MouseMoveAndClickPart{ click: LEFT_RIGHT_PRESS, method: WINDOW_MESSAGE }` at the cell's window-client center per the FR-007 formula, consistent with standard minesweeper chording rules.
- **FR-010**: `saolei_update([(x, y, status), ...])` MUST batch-update cell states in the game-state model. Coordinates use a top-left origin `(0, 0)`. The status enum comprises: INITIAL; the revealed numbers 0 through 8 (0 denotes a revealed blank cell); FLAG (a mine marker placed via right click on an unopened cell); HIT_MINE (the mine detonated by a click or chord); and MINE (other mine locations revealed at game end).
- **FR-011**: After `saolei_click`, `saolei_flag`, or `saolei_chord_click`, the model MUST call `saolei_update` before any subsequent operation; the system MUST reject a second operation that arrives before a valid `saolei_update`. `saolei_init` is exempt — no update is required after initialization. An operation that is **rejected by validation before dispatch** does NOT count as a successfully dispatched operation: it MUST NOT set the "operation pending update" flag, and the model MAY immediately retry with a valid operation without an intervening `saolei_update`.
- **FR-012**: For a saolei-enabled profile, the raw mouse tools (`mouse_move`, `mouse_click`) MUST NOT be exposed to the model; the saolei tools replace them as the LLM-facing operation channel.

**Validation rules**

- **FR-013** (click validation): `saolei_click` MUST reject any target that is not in the INITIAL state or that is flagged, without dispatching. The subsequent `saolei_update` MUST update the target cell's state, and all number cells updated in that same `saolei_update` MUST be mutually connected (connectivity may run through other number cells updated in the same batch), consistent with the cascade/flood-fill reveal of blank cells in minesweeper.
- **FR-014** (flag validation): `saolei_flag` MUST reject any target that is not in the INITIAL state. The subsequent `saolei_update` MUST change the target cell only between INITIAL and FLAG; no other transition is permitted.
- **FR-015** (chord validation): `saolei_chord_click` MUST reject any target that is not a non-0 number, or whose adjacent FLAG count is not equal to the cell's number. The subsequent `saolei_update` MUST NOT change any target-adjacent FLAG cell; MUST update the other target-adjacent non-number cells (except when a mine is hit, in which case only the hit mine need be updated); and each connected component of updated number cells MUST contain at least one cell adjacent to the chord target.
- **FR-016**: Any `saolei_update` containing coordinates outside the board, or statuses inconsistent with the operation performed, MUST be rejected with the state left unchanged.
- **FR-017**: Validation MUST be designed for extensibility; additional minesweeper-rule checks MAY be added later without changing the five tool contracts.

**Minesweeper-rule-derived behavior**

- **FR-018**: Left-clicking a mine ends the game; the corresponding `saolei_update` reflects HIT_MINE on the clicked cell. Left-clicking a blank (0) cell cascades to reveal all connected safe cells — the basis of the FR-013 connectivity rule.
- **FR-019**: Chording a satisfied number reveals all unflagged neighbors; if any flag is misplaced (a real mine remains among the unflagged neighbors), a mine is revealed and the game ends (HIT_MINE on that mine), which is the mine-hit exception in FR-015.

**Desktop profile editing**

- **FR-020**: The desktop agent-profile editor MUST add an "MCP" option (a selectable control consistent with the existing Tool Names chip UI) that lets the operator include "saolei" in the profile's `mcp_names`.
- **FR-021**: Creating or updating a profile with the saolei MCP selected MUST persist `mcp_names` (including "saolei") through the existing AgentProfile create/update API; on edit, `mcp_names` MUST be included in the update mask.
- **FR-022**: The profile list and profile card MUST visibly indicate when a profile is saolei-enabled (for example, a badge), so operators can see the selection at a glance.

**Built-in skill auto-injection**

- **FR-023**: A built-in skill MUST be authored under the agent's internal skill directory at `projects/game/agent/src/skill/saolei/SKILL.md`, conforming to the repository's SKILL.md file-format contract (`specs/020-agent-resources-layout/contracts/skill-md-format.md`). It MUST cover the minesweeper rules, the `(x, y)` top-left-origin coordinate convention, the operation/update alternation requirement, and the validation expectations.
- **FR-024**: When a profile's `mcp_names` includes "saolei", the agent MUST automatically load and inject the built-in saolei skill into the agent context for sessions using that profile. When saolei is not selected, the skill MUST NOT be injected.
- **FR-025**: The skill-injection mechanism MUST be limited to built-in skills mapped from `mcp_names`; it MUST NOT alter the existing user-created Skill resource surface (the PromptService CRUD on the `Skill` proto resource).

**Session and state lifecycle**

- **FR-026**: Game state MUST be session-scoped: each session has its own isolated board state, co-located with that session's OperationBridge; one session's state or operations MUST NOT affect another's.
- **FR-027**: Calling `saolei_init` again on an initialized session MUST re-dispatch the F2 keypress on the desktop (per FR-006) and reset the board state to all-initial, discarding the prior state.

### Key Entities *(include if feature involves data)*

- **Saolei MCP integration**: the per-session MCP integration that registers the five saolei tools on the session's MCP path, bound to that session's OperationBridge and game state.
- **Game State (per session)**: an in-memory grid model of cell statuses (INITIAL / 0-8 / FLAG / HIT_MINE / MINE), whose `width`×`height` dimensions (cell counts) are established by `saolei_init`, together with the **fixed board-layout constants** (origin offset 24 px from the window's left edge and 200 px from the top; 32×32 px cells) used for grid→window-client-coordinate translation, and an "operation pending update" flag that enforces the operate-then-update alternation.
- **Cell Status (enum)**: INITIAL; 0, 1, …, 8; FLAG; HIT_MINE; MINE (see FR-010).
- **MCP server path**: `/internal/mcp/{session_id}` — the per-session MCP endpoint on the agent's localhost MCP port.
- **Built-in saolei skill**: the `src/skill/saolei/SKILL.md` built-in skill, auto-injected when the saolei MCP is selected on a profile.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In 100% of sessions running under a saolei-enabled profile, an MCP client connecting to the session's MCP path discovers exactly the five saolei tools, and the raw mouse tools are not exposed to the model.
- **SC-002**: In 100% of operation tests, `saolei_init`, `saolei_click`, `saolei_flag`, and `saolei_chord_click` dispatch the correct generic operation Part to the desktop through the existing OperationBridge (`saolei_init` dispatches an F2 `KeyboardPressPart`; the others dispatch a `MouseMoveAndClickPart` with `WINDOW_MESSAGE` at the cell's window-client center — `saolei_chord_click` uses `click: LEFT_RIGHT_PRESS`), with no new desktop-facing contract beyond the generic proto extensions (FR-004a..FR-004d).
- **SC-003**: In 100% of tests, the operation-to-update alternation is enforced: a second operation before a valid `saolei_update` is rejected, and no `saolei_update` is required after `saolei_init`.
- **SC-004**: In 100% of validation tests, each rule in FR-013 through FR-016 accepts legal operations/updates and rejects illegal ones, with the board state left unchanged on rejection.
- **SC-005**: In 100% of profile-edit flows, selecting the saolei MCP persists `mcp_names` and visibly badges the profile; de-selecting removes it from both the persisted profile and the badge.
- **SC-006**: In 100% of saolei-profile sessions, the built-in saolei skill content is injected into the agent context; non-saolei sessions do not receive it.
- **SC-007**: A model can complete a simple supervised reveal sequence — `init` → `click` → `update` → `flag` → `update` → `chord_click` → `update` — using only the saolei tools, with each step validated.

## Assumptions

- The agent process is the MCP server host. The MCP server runs on a localhost port distinct from the gRPC port (50051), configurable via environment. Runtime topology is **loopback internal**: the agent's own LangGraph/LangChain turn loop connects to the per-session MCP endpoint as a loopback MCP client via the official `@langchain/mcp-adapters` `MultiServerMCPClient` (HTTP/streamable transport, URL-only config), so the saolei tools are surfaced to the model from the MCP server bound to that session. The existing supervised turn loop, checkpointing, streaming, and OperationBridge are reused; the per-session MCP path provides the session → bridge + game-state binding.
- Grid-to-pixel translation is performed agent-side using a **fixed** board pixel layout: the grid origin is 24 px from the window's left edge and 200 px from the top, with 32×32 px cells; cell `(x, y)`'s window-client center is `(24 + x*32 + 16, 200 + y*32 + 16)`. No board **pixel** geometry is supplied at `saolei_init` (the model supplies only the grid dimensions `width`/`height` in cells; pixel geometry is fixed). The desktop remains a generic input executor (keyboard + window-message mouse) and gains no saolei awareness.
- The operator is responsible for having a minesweeper game window open and bound, consistent with the existing supervised, operator-driven loop established in `specs/013-agent-game-tools`. Starting/resetting the actual game is done by `saolei_init` (which dispatches the F2 new-game keypress); the operator need not click the new-game face manually.
- Cell-status semantics: HIT_MINE is the specific mine detonated by a click or chord (game-ending); MINE (the user's "未命中地雷") denotes other mine locations revealed at game end (for example, on a loss all mines are shown). The exact label is confirmable at plan time; the two-state distinction is fixed here.
- Connectivity in FR-013 and FR-015 is 8-connectivity (horizontal, vertical, or diagonal adjacency), consistent with minesweeper adjacency, unless plan-phase analysis selects 4-connectivity; this does not change the intent of the rules.
- The validation rule set shipped here (FR-013 through FR-016, plus FR-018 and FR-019) is explicitly extensible and is not required to be exhaustive against every minesweeper edge case for this feature.
- Built-in skill injection is scoped to the saolei-to-skill mapping required by this feature; a general built-in-skill registry or CLI is out of scope.
- The MCP server lifecycle follows the agent process lifecycle; sessions map onto the existing SessionAgent lifecycle — the MCP server introduces no new session-creation path.

## References *(mandatory per Constitution §I — Citation Provenance)*

### Official Documentation

- LangChain MCP adapters (`@langchain/mcp-adapters`, `MultiServerMCPClient`, `getTools()`): https://docs.langchain.com/oss/javascript/langchain/mcp
- langchain-mcp-adapters README (transports, multi-server, error handling): https://github.com/langchain-ai/langchainjs/blob/main/libs/langchain-mcp-adapters/README.md
- LangChain DeepAgent MCP integration (`.mcp.json` HTTP-URL config pattern): https://docs.langchain.com/oss/javascript/deepagents/code/mcp-tools
- Model Context Protocol TypeScript SDK — Streamable HTTP transport, stateful sessions, per-session routing: https://github.com/modelcontextprotocol/typescript-sdk
- Model Context Protocol specification (transport): https://modelcontextprotocol.io/specification
- agentskills.io SKILL.md open standard: https://agentskills.io/specification
- OpenCode-recognized SKILL.md subset: https://opencode.ai/docs/skills/

### Articles & RFCs

- Minesweeper rules (chording, flagging, cascade reveal, first-click safety): https://en.wikipedia.org/wiki/Minesweeper_(video_game)
- Minesweeper gameplay (left-click reveal, right-click flag, chord click): https://minesweeper.now/help/gameplay
- Minesweeper chording technique (satisfied-number chord, mine-hit on misplaced flag): https://rarepike.com/minesweeper/chord-technique/

### Repository-Internal References

- `projects/game/agent/src/operation-bridge.ts` — OperationBridge (session-scoped desktop channel) to be reused.
- `projects/game/agent/src/session-agent.ts` — `SessionAgent` / `SessionAgentStore` (session lifecycle; owns the bridge).
- `projects/game/agent/src/server.ts` — agent gRPC server entry; the localhost MCP server is to be added alongside.
- `projects/game/agent/src/llm.ts` — `buildTools` / `AgentAdapterImpl` (current LLM-facing tool wiring).
- `projects/game/agent/src/mcp/README.md`, `projects/game/agent/src/skill/README.md` — file-format contracts for the new MCP integration and built-in skill.
- `specs/020-agent-resources-layout/contracts/directory-layout.md` — directory layout contract for `src/mcp/`, `src/skill/`, `src/tools/`.
- `specs/020-agent-resources-layout/contracts/skill-md-format.md` — SKILL.md file-format contract.
- `specs/013-agent-game-tools/spec.md` — mouse-tool, OperationBridge, and supervised-loop baseline.
- `specs/014-mouse-move-screenshot/spec.md` — `mouse_move` / `mouse_click` tool baseline.
- `projects/game/desktop/frontend/src/components/ProfileManagement.svelte` — profile editor UI to extend with the MCP option.
- `projects/game/game.proto` — `AgentProfile` (including the existing `mcp_names` field) and the mouse `Part` / `MouseClickPart` / `MouseMovePart` definitions.
