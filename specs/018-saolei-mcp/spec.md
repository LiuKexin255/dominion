# Feature Specification: Saolei (Minesweeper) MCP, Agent Capability Reorganization & Profile MCP/Skill Selection

**Feature Branch**: `018-saolei-mcp`

**Created**: 2026-07-14

**Status**: Draft

**Input**: User description: "为 projects/game/agent/ 新增 saolei mcp 用于 agent 通过游戏指令操作扫雷游戏，以代替直接通过 mouse 工具操作带来的不准确性。mcp 包括工具 saolei_init / saolei_click / saolei_flag / saolei_double_click / saolei_update；维护棋盘内部状态并在操作/更新之间切换；agent 每次操作后必须更新棋盘状态才能进行下一次操作；saolei_update 支持批量更新与格子状态枚举；mcp 对操作和状态更新进行校验；鼠标操作通过向窗口发送消息（而非物理光标）以避免遮挡，坐标由 top_offset/left_offset/block_length 常数计算；为 mcp 编写配套 skill；desktop ui 对 agent profile 编辑增加 mcp 和 skill 选择；并将 agent 服务重构为 tools/mcp/skill 独立目录并搭配 readme。"

> **Terminology note**: in this feature, "MCP" refers to the [Model Context Protocol](https://modelcontextprotocol.io/) standard. In the standard MCP topology, a dedicated MCP server process exposes tools to an MCP client ([MCP architecture](https://modelcontextprotocol.io/docs/concepts/architecture)). Here the **agent service itself fulfils the MCP server role**: the saolei tools are exposed by the agent service acting as the MCP server, and the agent (LLM) acts as the MCP client that calls them. The client-server architecture is preserved unchanged — only the server's deployment form differs (embedded within the agent service rather than a separate process). This is why no additional/extra server instance is required: the agent service already serves as the MCP server.

## Clarifications

### Session 2026-07-14

- Q: How should the MCP handle an indefinite "awaiting update" block when the agent never sends `saolei_update` after an operation? → A: No automatic timeout — the MCP stays in "awaiting update" until the next `saolei_init` starts a fresh game. This avoids any stale-state assumption; a lighter "clear awaiting without a new game" tool may be added in a later iteration if real play shows the agent frequently getting stuck.
- Q: How are operation rejections conveyed to the agent through the MCP tool interface? → A: The tool returns a success result carrying a structured rejection (a status indicating rejection plus a machine-readable reason), so the agent can read the reason and adapt. Thrown errors are reserved for genuine infrastructure failures only, not game-rule rejections.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Play Minesweeper Accurately Through a Game-Specific MCP (Priority: P1)

An agent plays a desktop Minesweeper (扫雷) game through a purpose-built saolei MCP instead of the generic mouse tool. The MCP exposes five tools — `saolei_init` (start a new game and define the board), `saolei_click` (reveal a cell), `saolei_flag` (mark or clear a mine flag), `saolei_double_click` (chord a numbered cell to reveal its non-mine neighbours), and `saolei_update` (report the observed board state). Because the MCP understands the game, it computes the exact cell-centre pixel coordinates from the grid position, sends input as window messages so the OS cursor never slides over the target, and lets the agent reason in game terms ("reveal cell (3,4)") rather than raw pixels. This replaces the error-prone raw-mouse-tool path where the agent mis-estimated coordinates and could not tell a revealed cell from a covered one.

**Why this priority**: Accurate, semantically meaningful cell operations are the entire reason the feature exists. Every other part of this spec (state, validation, skill, UI, reorganization) exists to support or surround this capability. Without it, the agent continues to operate Minesweeper inaccurately through the mouse tool.

**Independent Test**: Can be fully tested by binding the Minesweeper window, calling `saolei_init` with a board size, issuing a `saolei_click` on a covered cell, and verifying the MCP computes the correct cell-centre coordinate and delivers the input as a window message (no physical cursor movement over the cell).

**Acceptance Scenarios**:

1. **Given** the Minesweeper window is bound and no board is initialized, **When** the agent calls `saolei_init` with width `x` and height `y`, **Then** an F2 "new game" keystroke is delivered to the window, the MCP creates an internal board of `x` by `y` cells all in the `block` state, and the MCP reports the board is ready.
2. **Given** an initialized board, **When** the agent calls `saolei_click` at grid coordinate `(x, y)` on a `block` cell, **Then** the MCP computes the cell-centre pixel coordinate from `left_offset`, `top_offset`, and `block_length`, delivers a left-button press and release to the window as window messages (the OS cursor is not moved over the cell), and the MCP enters its "awaiting update" state.
3. **Given** an initialized board and a `block` cell, **When** the agent calls `saolei_flag` at `(x, y)`, **Then** the MCP delivers a right-button window message that toggles the flag on that cell, and the MCP enters "awaiting update".
4. **Given** a numbered cell whose mine neighbours are all flagged, **When** the agent calls `saolei_double_click` at that cell, **Then** the MCP delivers a simultaneous left-and-right-button window message (the chord action), and the MCP enters "awaiting update".
5. **Given** the agent has just operated, **When** it observes the resulting board and calls `saolei_update` with the changed cells, **Then** the MCP applies the new states and returns to "ready", allowing the next operation.

---

### User Story 2 - Maintained Board State with a Mandatory Update Protocol and Legality Validation (Priority: P1)

The saolei MCP does more than dispatch input — it owns the full board state as a two-dimensional grid and enforces a strict operate-then-update protocol. After `saolei_init` the board is fully known (all `block`), so the MCP self-updates and no agent update is required. After every other operation (`click`, `flag`, `double_click`) the MCP enters an "awaiting update" state in which further operations are rejected until the agent calls `saolei_update` with the cells it observed. The MCP also validates every operation and every state change against the rules of the game: you cannot operate on an already-revealed number or a flagged cell, only right-button actions produce flags, a double-click (chord) targets a numbered cell, coordinates must be in bounds, and once a `boom` is reported the board is terminal until a fresh `saolei_init`.

**Why this priority**: The state machine and validation are what make the MCP trustworthy. Without enforced update-after-operation, the agent could issue a second click based on a stale mental model of the board. Without legality checks, the agent could attempt meaningless actions that the game would ignore or that corrupt the MCP's model. This is co-equal with US1 because accurate operation is worthless without a coherent, validated state.

**Independent Test**: Can be fully tested by driving the MCP through a scripted sequence and asserting that operations on revealed/flagged cells, out-of-bounds coordinates, pre-init operations, and a second operation before `saolei_update` are all rejected with a clear reason, while legal sequences advance the board correctly.

**Acceptance Scenarios**:

1. **Given** an initialized board, **When** `saolei_init` completes, **Then** the MCP's board is entirely `block` and the MCP is in "ready" without requiring any `saolei_update`.
2. **Given** the agent has called `saolei_click` but not yet `saolei_update`, **When** it calls any further `saolei_click`, `saolei_flag`, or `saolei_double_click`, **Then** the MCP rejects it as "awaiting update" and performs no window input.
3. **Given** a cell whose state is a number (`zero`..`eight`) or `flag`, **When** the agent calls `saolei_click` on it, **Then** the MCP rejects the operation as illegal and performs no window input.
4. **Given** a cell that is not a number, **When** the agent calls `saolei_double_click` on it, **Then** the MCP rejects the operation.
5. **Given** a `saolei_update` that would change a number cell back to `block`, **When** the MCP validates the batch, **Then** the update is rejected as an illegal state transition.
6. **Given** an update reports a `boom` on some cell, **When** the agent attempts any subsequent operation, **Then** the MCP rejects it as terminal until a new `saolei_init` resets the board.
7. **Given** a coordinate outside the initialized board dimensions, **When** the agent calls any positional tool, **Then** the MCP rejects it as out of bounds.
8. **Given** two sessions each running a saolei MCP, **When** session A initializes its board and performs several operations and updates, **Then** session B's MCP board and lifecycle state remain entirely unaffected (no cross-session leakage).
9. **Given** a session whose saolei MCP instance is replaced (e.g. the session agent is rebuilt on a profile refresh), **When** the new MCP instance starts, **Then** no board state, pending update obligation, or terminal flag from the previous instance survives, and the new instance requires a fresh `saolei_init` before accepting any operation.

---

### User Story 3 - Select MCPs and Skills When Editing an Agent Profile (Priority: P2)

An operator editing or creating an agent profile in the desktop UI can choose which MCPs and which skills the profile uses, in exactly the same way they already choose tools today. The selections persist on the profile (the `mcp_names` and `skill_names` fields that already exist in the data model), travel through create and update, and reach the agent so a profile that declares the saolei MCP actually exposes the saolei tools to its agent.

**Why this priority**: Without UI selection, the saolei MCP and its companion skill cannot be attached to a profile through the operator-facing surface, so US1 and the skill cannot be exercised end-to-end by an operator. It is secondary to the MCP itself because the capability must exist before there is anything to select.

**Independent Test**: Can be fully tested by creating a profile with the saolei MCP and skill selected, listing it back, editing the selections off and on, and verifying every change round-trips through create, update, and list with the expected `mcp_names` and `skill_names`.

**Acceptance Scenarios**:

1. **Given** the profile create form, **When** the operator toggles the saolei MCP and the saolei skill chips on, fills the remaining fields, and submits, **Then** the created profile carries `mcp_names` and `skill_names` containing the selected values.
2. **Given** an existing profile, **When** the operator opens the edit form, changes the MCP and skill selections, and saves, **Then** the update request includes `mcp_names` and `skill_names` in its field mask and the persisted profile reflects the new selections.
3. **Given** a profile list, **When** the operator reopens a previously saved profile, **Then** the MCP and skill chips show the previously selected values as active.
4. **Given** a profile whose `mcp_names` references the saolei MCP, **When** the agent adapter is built for that profile, **Then** the saolei MCP's tools are exposed to the agent for that profile.

---

### User Story 4 - Companion Saolei Skill Documents the MCP Usage Protocol (Priority: P2)

The saolei MCP ships with a companion skill that teaches the agent how to use it: the tool set, the strict `init → operate → update → repeat` protocol, the legality rules, the cell-state enumeration, and a worked example of a play flow. The skill is injected when the agent is created (not dynamically mid-session) and gives the model the procedural knowledge it needs to drive the MCP correctly instead of guessing.

**Why this priority**: The MCP enforces correctness at runtime, but the agent still needs to know the protocol in advance to avoid a long sequence of rejections. The skill is the teaching layer; it is secondary to the MCP and the state machine it describes.

**Independent Test**: Can be fully tested by creating an agent profile that carries the saolei skill, building the agent, and verifying the skill content is present in the agent's injected context; and by reading the skill document and confirming it documents all five tools, the update protocol, and the legality rules.

**Acceptance Scenarios**:

1. **Given** a profile whose `skill_names` includes the saolei skill, **When** the agent is created, **Then** the skill content is injected into the agent's context at creation time.
2. **Given** the saolei skill document, **When** a reader inspects it, **Then** it documents all five tools (`saolei_init`, `saolei_click`, `saolei_flag`, `saolei_double_click`, `saolei_update`), the mandatory update-after-operation protocol, the cell-state enumeration, and at least one example play flow.

---

### User Story 5 - Agent Service Reorganized into tools/, mcp/, skill/ Directories (Priority: P3)

The agent service is reorganized so that tools, MCPs, and skills each live under their own top-level directory, with a README in each directory describing its purpose and how to add to it. The existing mouse tool moves to `tools/mouse/`. This makes the new saolei MCP and skill land in predictable homes (`mcp/saolei/`, `skill/saolei/`) instead of a flat `src/`, and makes the capability surface of the service self-describing.

**Why this priority**: This is an enabling refactor that gives the new MCP and skill clean, conventional locations and makes future capabilities easy to add. It is the lowest priority because it is internal hygiene; it does not by itself change agent behaviour.

**Independent Test**: Can be fully tested by inspecting the agent service directory tree and confirming three top-level capability directories exist, each contains a README, the mouse tool has been relocated under `tools/mouse/`, and all existing tests still pass after the move.

**Acceptance Scenarios**:

1. **Given** the reorganized service, **When** a reader lists the agent source tree, **Then** there are `tools/`, `mcp/`, and `skill/` directories, each containing a README describing the directory's purpose and conventions.
2. **Given** the mouse tool before the change lives in the flat source, **When** the reorganization completes, **Then** the mouse tool source and its tests live under `tools/mouse/` and continue to build and pass.
3. **Given** the new saolei capability, **When** a developer adds it, **Then** its tool and state implementation lives under `mcp/saolei/` and its skill document lives under `skill/saolei/`, matching the documented convention.

### Edge Cases

- What happens when the agent calls a positional tool with a coordinate at or beyond the board edge (e.g. `(x, 0)` or a negative value)? The MCP rejects any coordinate outside `[0, width)` × `[0, height)` as out of bounds and performs no window input.
- What happens when `saolei_flag` is called on a cell that is already flagged? The flag is a toggle, so the right-button window message clears the flag; the MCP still enters "awaiting update" and expects the agent to confirm the cleared state via `saolei_update`.
- What happens when the agent's `saolei_update` batch mixes legal and illegal transitions? The MCP rejects the entire batch atomically (no partial application) and reports which transitions were illegal, leaving the board in its pre-update state.
- What happens when `saolei_init` is called while a game is already in progress? It sends F2 to start a fresh game and resets the entire board to `block`, discarding the prior state.
- What happens when the bound window is not the Minesweeper game (e.g. wrong window bound)? The MCP cannot detect the game type; F2 and cell-coordinate input are delivered to whatever window is bound. Correct window binding remains the operator's responsibility, consistent with the existing mouse tool.
- What happens when a window message is delivered but the game does not respond (e.g. game already over, modal open)? The MCP still enters "awaiting update"; the agent observes via screenshot that nothing changed and reports an unchanged (or terminal) state through `saolei_update`.
- What happens if the agent never calls `saolei_update` after an operation? The MCP stays in "awaiting update" indefinitely and accepts no further operations until a `saolei_init` starts a fresh game; there is no automatic timeout and no silent recovery, so no stale board state is assumed.
- What happens when a profile names an MCP or skill that the system does not recognize? Unknown names are ignored for that profile and surfaced as a validation warning, consistent with how unknown tool names are already handled.
- What happens to the in-flight adapter when an operator edits a profile's `mcp_names` or `skill_names` mid-session? The existing refresh mechanism rebuilds the adapter for the next turn; an in-flight turn is rejected until the refresh completes, consistent with the current `RefreshAgent` contract. Rebuilding the adapter creates a fresh saolei MCP instance for that session, which discards the previous board state per FR-025c.

## Requirements *(mandatory)*

### Functional Requirements

#### MCP tool surface

- **FR-001**: The system MUST provide a `saolei_init` tool that accepts board width `x` and height `y` in cell counts, delivers an F2 keystroke to the bound window to start a new game, and initializes the MCP's internal board to an `x` by `y` grid of `block` cells.
- **FR-002**: The system MUST provide a `saolei_click` tool that accepts a grid coordinate `(x, y)` and performs a left-button reveal at that cell's centre.
- **FR-003**: The system MUST provide a `saolei_flag` tool that accepts a grid coordinate `(x, y)` and toggles a mine flag on that cell via a right-button action.
- **FR-004**: The system MUST provide a `saolei_double_click` tool that accepts a grid coordinate `(x, y)` and performs a chord (simultaneous left-and-right-button) action on a numbered cell, revealing its non-mine neighbours.
- **FR-005**: The system MUST provide a `saolei_update` tool that accepts a batch of `(x, y, state)` tuples and applies them to the MCP's board state.

#### Cell-state model

- **FR-006**: The cell-state enumeration MUST consist of exactly `block` (initial unrevealed), `zero` through `eight` (revealed number cells), `flag` (mine marker), and `boom` (detonated mine).
- **FR-007**: Grid coordinates MUST be zero-indexed from the top-left corner of the board, i.e. `(0, 0)` is the top-left cell.

#### Board-state maintenance and the operate-then-update protocol

- **FR-008**: The MCP MUST maintain the full board as a two-dimensional grid of cell states that persists across tool calls within a session.
- **FR-009**: After `saolei_init`, the MCP MUST set the board to all `block` and self-transition to its "ready" state without requiring a `saolei_update`.
- **FR-010**: After `saolei_click`, `saolei_flag`, or `saolei_double_click`, the MCP MUST enter an "awaiting update" state in which all further positional operations are rejected until a `saolei_update` is received.
- **FR-011**: A successful `saolei_update` MUST transition the MCP back to its "ready" state, enabling the next operation.
- **FR-011a**: The MCP MUST NOT auto-time-out of the "awaiting update" state. It remains awaiting until either a `saolei_update` is received or a new `saolei_init` resets the board; there is no silent recovery to "ready", so no stale board state is ever assumed after an operation.
- **FR-012**: The agent MUST NOT be required to update after `saolei_init`; only `click`, `flag`, and `double_click` trigger the "awaiting update" obligation.

#### Coordinate computation and occlusion-free input

- **FR-013**: For a grid coordinate `(x, y)`, the MCP MUST compute the target cell-centre pixel as `left_offset + x * block_length + block_length / 2` horizontally and `top_offset + y * block_length + block_length / 2` vertically, using the configuration constants `top_offset`, `left_offset`, and `block_length`.
- **FR-014**: Mouse operations (click, flag, double-click) MUST be delivered to the game window as window messages rather than as physical cursor events, so that the OS cursor does not move over and occlude the targeted cell.
- **FR-015**: The constants `top_offset`, `left_offset`, and `block_length` MUST be configurable for the target Minesweeper game; their concrete values are determined at design time and are not agent-supplied parameters.

#### Validation

- **FR-016**: The MCP MUST reject any positional operation attempted before `saolei_init` has established a board.
- **FR-017**: The MCP MUST reject any positional operation attempted while it is in the "awaiting update" state.
- **FR-018**: The MCP MUST reject `saolei_click` on any cell whose current state is not `block` (i.e. number, `flag`, or `boom` cells cannot be clicked).
- **FR-019**: The MCP MUST reject `saolei_double_click` on any cell whose current state is not a number (`zero`..`eight`).
- **FR-020**: The MCP MUST restrict `flag` production so that only the right-button `saolei_flag` action transitions a cell to or from the `flag` state; no other tool may produce a `flag` state.
- **FR-021**: The MCP MUST reject any positional operation whose coordinate is outside the initialized board bounds.
- **FR-022**: The MCP MUST reject a `saolei_update` batch that contains an illegal state transition (e.g. reverting a number to `block`, or flagging a revealed number), and MUST apply the batch atomically so that no partial state is committed.
- **FR-023**: Once a `boom` state is present on the board, the MCP MUST reject all further positional operations until a new `saolei_init` resets the board.
- **FR-024**: The validation rule set is foundational and intended to grow iteratively; an exhaustive enumeration of every game rule is explicitly out of scope for this version.
- **FR-024a**: When the MCP rejects an operation or a `saolei_update` batch (per FR-016 through FR-023), the corresponding tool MUST return a success result carrying a structured rejection — a status indicating rejection and a machine-readable reason — rather than raising an error. Thrown errors are reserved for genuine infrastructure failures (e.g. the desktop operation channel is unreachable) and MUST NOT be used for game-rule rejections.

#### In-process deployment and per-session lifecycle

- **FR-025**: The saolei MCP MUST follow the Model Context Protocol client-server architecture, with the agent service itself acting as the MCP server that exposes the saolei tools, and the agent acting as the MCP client that invokes them. Because the agent service fulfils the server role, the MCP MUST NOT require an additional or separate server instance beyond the agent service.
- **FR-025a**: The saolei MCP instance MUST be per-session — each session agent owns exactly one MCP instance, created when the agent for that session is established, mirroring the per-session ownership of the existing operation bridge.
- **FR-025b**: Board state MUST be isolated per session; the board state, lifecycle marker, and any pending "awaiting update" obligation of one session's MCP instance MUST NOT be visible to or affect any other session's MCP instance.
- **FR-025c**: When a new saolei MCP instance is created for a session that previously held one (e.g. adapter rebuild on profile refresh, or any other restart of the session agent), the MCP MUST discard all board state from the prior instance before accepting any operation, so that no stale board survives into the new instance.

#### Companion skill

- **FR-026**: The feature MUST ship a companion saolei skill document that describes the five tools, the mandatory `init → operate → update → repeat` protocol, the cell-state enumeration, the legality rules, and at least one example play flow.
- **FR-027**: Skills MUST be injectable at agent creation; dynamic mid-session skill injection is out of scope for this version.

#### Agent service reorganization

- **FR-028**: The agent service MUST be reorganized so that tools, MCPs, and skills each live under their own directory (`tools/`, `mcp/`, `skill/`), and each directory MUST contain a README describing its purpose, its conventions, and how to add a new entry.
- **FR-029**: The existing mouse tool MUST be relocated under `tools/mouse/` (together with its tests) and MUST continue to build and pass after the move.
- **FR-030**: The saolei MCP implementation MUST live under `mcp/saolei/` and the saolei skill document MUST live under `skill/saolei/`.

#### Desktop profile MCP and skill selection

- **FR-031**: The agent-profile create form in the desktop UI MUST allow the operator to select zero or more MCPs and zero or more skills, mirroring the existing tool-selection interaction.
- **FR-032**: The agent-profile edit form MUST allow the operator to change the selected MCPs and skills, and the update request MUST include `mcp_names` and `skill_names` in its update mask.
- **FR-033**: The profile create path MUST carry `skill_names` and `mcp_names` from the UI all the way to the persisted profile, closing the existing gap where the create view omits these fields.
- **FR-034**: The profile data delivered to the agent adapter MUST include `skill_names` and `mcp_names` alongside the existing `tool_names`, so a profile's declared MCPs and skills actually take effect for its agent.
- **FR-035**: MCP and skill names declared on a profile but not recognized by the system MUST be ignored for that profile and surfaced as a validation warning, consistent with existing handling of unknown tool names.

### Key Entities *(include if feature involves data)*

- **Saolei MCP**: The MCP server exposed by the agent service for Minesweeper, bundling the five saolei tools and the board state they share. Lives under `mcp/saolei/`.
- **Board State**: A two-dimensional grid of cell states owned by the MCP, together with a lifecycle marker that is one of `ready`, `awaiting-update`, or `terminal` (after a `boom`). Persists across tool calls within a session. The board state is scoped to a single session's MCP instance — it is never shared across sessions, and it is discarded wholesale whenever a new MCP instance is created for that session.
- **Cell State**: One of `block`, `zero`, `one`, `two`, `three`, `four`, `five`, `six`, `seven`, `eight`, `flag`, `boom`.
- **Coordinate Constants**: The three board-geometry constants `top_offset`, `left_offset`, and `block_length` (measured relative to the bound window) used to convert a grid coordinate into a cell-centre pixel.
- **Saolei Skill**: The companion skill document that teaches the agent the MCP usage protocol. Lives under `skill/saolei/`.
- **Agent Profile (extended in use)**: The existing profile resource already carries `tool_names`, `skill_names`, and `mcp_names`; this feature wires the MCP and skill fields through the UI, the create/update paths, and the adapter, so they are selectable and effective rather than dormant.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: In 100% of test cases, an agent can complete a full saolei turn (`saolei_init` → `saolei_click` → `saolei_update`) using only the saolei MCP tools, with no fallback to the raw mouse tool.
- **SC-002**: In 100% of test cases, the MCP rejects illegal operations — operating before init, operating while awaiting update, clicking a non-`block` cell, chording a non-number cell, and out-of-bounds coordinates — by returning a structured rejection (status + machine-readable reason, not a thrown error) and performing no window input.
- **SC-003**: In 100% of positional operations, the OS cursor is not moved over the targeted cell (input delivered as window messages), verifiable by confirming no cursor marker appears over the target cell in a post-operation screenshot.
- **SC-004**: In at least 95% of test cases across the board (including edge and corner cells), the computed cell-centre coordinate lands inside the intended cell.
- **SC-005**: An operator can add or remove an MCP and a skill on an agent profile through the desktop UI and see the change persist through create, edit, and list in under 30 seconds, in 100% of test cases.
- **SC-006**: After reorganization, the agent service has exactly the three capability directories `tools/`, `mcp/`, and `skill/`, each containing a README, with the mouse tool relocated under `tools/mouse/` and all pre-existing tests still passing.
- **SC-007**: In 100% of test cases, an `saolei_update` batch containing any illegal transition is rejected atomically with no partial state committed to the board.
- **SC-008**: In 100% of test cases, board state is confined to the session that produced it — operations and updates in one session never alter another session's board, and a session whose MCP instance is replaced starts from an empty board with no carry-over state.

## Assumptions

- The Minesweeper game runs as a desktop window bound to the desktop application exactly like other games; the F2 keystroke starts a new game, matching the classic Microsoft Minesweeper keyboard convention ([Minesweeper keyboard shortcuts](https://en.wikipedia.org/wiki/Minesweeper_(video_game))). Correct window binding remains the operator's responsibility.
- The concrete values of `top_offset`, `left_offset`, and `block_length` are measured and fixed for the target Minesweeper game at design/implementation time; the spec does not prescribe their numeric values.
- The saolei MCP reuses the existing agent-to-desktop operation channel to reach the bound window (the same path the mouse tool uses today), with the desktop performing the window-message delivery; introducing a new transport is a plan-stage decision and is out of scope for this spec.
- The saolei MCP instance is owned per-session by the session agent, analogous to the existing per-session `OperationBridge`; session agents and their lifecycle (including adapter rebuild on refresh) are the boundary at which MCP instances and their board state are created and discarded.
- The existing `AgentProfile` proto fields `skill_names` and `mcp_names`, and the existing `Skill` message, are reused as-is; no proto schema additions are required for the data model.
- After every operation except `saolei_init`, the new cell states are observed and reported by the agent (via its screenshot/vision capability) through `saolei_update`; the MCP does not auto-detect post-operation state, it only enforces the protocol and validates the reported states.
- The validation rule set specified here is foundational and intentionally non-exhaustive; additional rules are expected to be added iteratively in follow-up work.
- "MCP" in this feature denotes the Model Context Protocol. The agent service implements the MCP server role in-process (it is the MCP server), and the agent is the MCP client that invokes the exposed tools. No additional/extra MCP server instance is required because the agent service already serves that role; the client-server architecture is unchanged and only the server's deployment form (embedded versus separate process) differs.
- Skills are injected at agent creation time, not dynamically during a live session, per the user's explicit note.
- A batch `saolei_update` is applied atomically (all-or-nothing); the agent retries the whole batch if any transition is illegal.
- The term "扫雷 / saolei" and the saolei skill/MCP naming are used interchangeably for the Minesweeper capability throughout the codebase.

## References *(mandatory per Constitution §I — Citation Provenance)*

### Official Documentation

- [Model Context Protocol — introduction](https://modelcontextprotocol.io/) — the MCP standard this feature implements, with the agent service acting as the MCP server.
- [Model Context Protocol — architecture](https://modelcontextprotocol.io/docs/concepts/architecture) — documents the MCP host/client/server topology this feature adapts by embedding the server role inside the agent service.
- [Minesweeper (video game) — keyboard shortcuts](https://en.wikipedia.org/wiki/Minesweeper_(video_game)) — documents the F2 "new game" keyboard convention relied on by `saolei_init`.

### Repositories

- No external repository references. The capability reuses this repository's existing agent-to-desktop operation channel and profile data model.

### Articles & RFCs

- No article or RFC references.

### Repository-Internal References

- `projects/game/game.proto` — defines `AgentProfile` (with existing `skill_names`, `mcp_names`, `tool_names` fields) and the `Skill` message reused by this feature.
- `projects/game/agent/src/session-agent.ts` — defines `ProfileData` (currently `model`, `systemPrompt`, `toolNames`), which this feature extends to carry `skill_names` and `mcp_names` to the adapter.
- `projects/game/agent/src/mouse-tool.ts` — the existing mouse tool relocated under `tools/mouse/` by this feature; also the pattern the saolei MCP's window-input delivery follows.
- `projects/game/agent/src/operation-bridge.ts` — the agent-to-desktop operation channel the saolei MCP reuses to reach the bound window.
- `projects/game/desktop/frontend/src/components/ProfileManagement.svelte` — the profile create/edit form extended with MCP and skill selection.
- `projects/game/desktop/view_model.go` — the Wails view models; `CreateAgentProfileView` currently omits `skill_names`/`mcp_names` and is extended by this feature.
- `specs/013-agent-game-tools/spec.md` — prior feature establishing agent tools, `tool_names` profile scoping, and the operation result frame baseline.
- `specs/014-mouse-move-screenshot/spec.md` — prior feature establishing mouse `MOVE` action and post-operation screenshot feedback, the accuracy context this MCP improves on.
