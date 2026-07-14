# Implementation Plan: Saolei (Minesweeper) MCP, Agent Capability Reorganization & Profile MCP/Skill Selection

**Branch**: `018-saolei-mcp` | **Date**: 2026-07-14 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/018-saolei-mcp/spec.md`. User planning input: (1) research LangChain.js (`https://docs.langchain.com/oss/javascript/langchain/overview`) for first-time MCP + skill introduction — if LangChain lacks native MCP/skill support, **reference** (not adopt) deep agents' skill + MCP patterns; (2) board geometry pinned: `top_offset=200px`, `left_offset=24px`, `block_length=32px` (all coordinates in pixels).

## Summary

Add an in-process **Model Context Protocol** server for Minesweeper (扫雷) inside the `@dominion/game-agent` service, exposing five tools (`saolei_init`/`saolei_click`/`saolei_flag`/`saolei_double_click`/`saolei_update`) that operate the game by **cell coordinates** with **window-message input** (no cursor occlusion), while **maintaining full board state** and enforcing a strict **operate → update** protocol with legality validation. The agent service itself is the MCP server (embedded, no extra process); the LangChain agent is the MCP client. A companion **skill** document is injected at agent creation. Concurrently: reorganize the agent service into `tools/`, `mcp/`, `skill/` directories (each with a README); extend the desktop profile editor with MCP + skill selection; and wire `skill_names`/`mcp_names` end-to-end (they exist in the proto but are dormant today).

The technical approach for how the in-process MCP server exposes tools to the LangChain agent, and how skills are injected, is the single research-dependent decision — resolved in [research.md](./research.md) (Phase 0, per Constitution §III). The working hypothesis, grounded in the existing `buildTools` + `createAgent` adapter, is stated under [Design Decisions](#design-decisions-working-hypotheses--phase-0-confirms).

## Technical Context

**Language/Version**:
- Agent service: TypeScript on Node.js (built with Bazel).
- Desktop: Go (Wails v2) on Windows.
- Frontend: Svelte 5 (runes) + TypeScript.

**Primary Dependencies** (current, from `pnpm-workspace.yaml` catalog):
- `@langchain/core` `^1.2.0`, `langchain` `^1.5.0`, `@langchain/langgraph` `^1.4.4`, `@langchain/anthropic` `^1.5.0`, `@langchain/openai` `^1.5.1` ([LangChain.js v1](https://docs.langchain.com/oss/javascript/langchain/overview)) — **LangChain v1**, which exposes `createAgent` (used in `llm.ts`).
- `zod` (catalog) — tool input schemas.
- `@grpc/grpc-js`, `@grpc/proto-loader` — agent↔desktop/gateway transport.
- Wails v2 (Go) — desktop shell with Win32 window access.

**Potential NEW dependency (Phase 0 decides)**: `@modelcontextprotocol/sdk` (TypeScript MCP SDK) and/or `@langchain/mcp-adapters` — **only if** research.md concludes the MCP-adapter approach is worth it for an in-process server. The working hypothesis is that NO new runtime dependency is needed (see Decision D-1).

**Storage**:
- Board state: **in-memory**, per-session (owned by the session's MCP instance, mirroring `OperationBridge`). Not persisted.
- Profiles + skills: existing `PromptService` storage (reused; `Skill` message already defined).

**Testing**: `vitest` (agent service, TS); `go test` (desktop, Go). Existing fakes: fake-llm service, `OperationBridge` test harness.

**Target Platform**: Windows desktop for game operation (Win32 `PostMessage`/`SendMessage` to the bound Minesweeper window); Linux for unit/dev builds.

**Project Type**: desktop-app (Wails) + in-process agent service + Svelte frontend.

**Performance Goals** (from spec): computed cell-centre lands inside the intended cell in ≥95% of cases (SC-004); post-operation screenshot delivered within <500ms (inherited from spec 014 SC-004); operation dispatch round-trip within the existing 5s `OperationBridge` timeout.

**Constraints**: occlusion-free input (window messages, no physical cursor over the cell — FR-014); per-session state isolation (FR-025b); no auto-timeout of "awaiting update" (FR-011a); rejections return structured success, not thrown errors (FR-024a).

**Scale/Scope**: 1 MCP (saolei) with 5 tools + 1 companion skill; agent service reorganization (3 capability dirs + READMEs); desktop profile editor extension; proto Part additions for window-message + keyboard input.

**Coordinate constants (resolved by user, per FR-015)**:

| Constant | Value | Meaning |
|---|---|---|
| `top_offset` | `200` px | board top edge offset from window top |
| `left_offset` | `24` px | board left edge offset from window left |
| `block_length` | `32` px | cell width = cell height (square cells) |

Cell-centre pixel for grid `(x, y)`: `X = 24 + x*32 + 16`, `Y = 200 + y*32 + 16`. All coordinates in pixels (window-client-relative).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Citation Provenance (§I)**: every external fact, dependency choice, and design decision in this plan carries an inline `[description](URL)` link and a matching `## References` entry. The LangChain v1 version pins cite the official docs; `@langchain/mcp-adapters` v1.1.3, the langchainjs/deepagents source commits, and the Agent Skills spec are cited in [research.md](./research.md). Statements without a citation are assumptions and live in the spec's `## Assumptions`. ✔.
- **Version pins accompany citations whose state matters**: LangChain catalog pins recorded above; `@langchain/mcp-adapters` v1.1.3 and deepagents commit `46e10640c` pinned in [research.md](./research.md). ✔
- **All cited links resolve to publicly accessible resources**: ✔
- **Code Style Precedence (§II)**: every implementation task exported to `tasks.md` MUST reference the applicable `style/` documents (TS/Go/Svelte) and confirm review before code changes begin. The `style/` docs will be read in Phase 2 (`/speckit.tasks`). ✔ (forward obligation).
- **External Dependency Research (§III)**: the MCP + skill introduction is a first-time external-dependency event. LangChain.js MCP support, the MCP TypeScript SDK in-process transport, and deep-agents skill/MCP patterns were researched against official docs + source repos BEFORE this plan was finalized — see [research.md](./research.md) (§R-1, §R-2). Decision: **no new runtime dependency** (D-1 plain LangChain tools; D-2 system-prompt skill assembly). ✔ (Phase 0 complete; gate passed).
- **Refactoring-Oriented Changes (§IV)**: every change is classified 新增 / 修改 / 删除 below in [Change Classification](#change-classification). 修改 changes (adapter, `buildTools`, `ProfileData`, proto Part, desktop view-model, profile editor) are refactored, not stacked; each carries an existing-design review + verdict. The agent service reorganization is a 修改 of the source layout (with 删除 of the flat `mouse-tool.ts` location). ✔

**Gate verdict**: PASSED. Phase 0 ([research.md](./research.md)) is complete; §III research obligation satisfied; decisions D-1/D-2 confirmed with no new dependency. No violations to justify; `Complexity Tracking` left empty.

## Change Classification

Per Constitution §IV. Each unit of change is labeled and (for 修改/删除) carries an existing-design verdict.

### 新增 (Add) — modules/files/types that did not previously exist
- **`mcp/saolei/`** — the saolei MCP: board state machine, cell-state enum, coordinate computation, the five LangChain tool factories, validation, per-session instance lifecycle.
- **`skill/saolei/`** — the companion skill markdown document + its loader.
- **`tools/README.md`, `mcp/README.md`, `skill/README.md`** — capability-directory conventions.
- Proto: **new generic `KeyPart` + `KeyAction` enum** declaring a key-press operation (tool-agnostic; starts with `F2`), added as a new member of the existing `Part.kind` oneof. The desktop implements it (PostMessage `WM_KEYDOWN/UP`). No tool-specific parts.
- Proto: **new `InputDelivery` enum** (`SIMULATE` | `WINDOW_MESSAGE`) — see the 修改 entry on `MouseMovePart`/`MouseClickPart`.
- Desktop (Go): **extend the input executors** to honor `InputDelivery` (`SIMULATE` = current physical cursor path; `WINDOW_MESSAGE` = Win32 `PostMessage` to the bound window's HWND without moving the OS cursor) **and** to handle `KeyPart` (PostMessage `WM_KEYDOWN/UP`). The desktop also processes a whole `PartBlock` (move+click combo) as one operation.

### 修改 (Modify) — refactor of an existing unit (not logic stacked on top)
- **`llm.ts` `buildTools` / `AdapterFactory` / `AgentAdapterImpl`**: extend to (a) resolve `mcp_names` → the MCP's tool set and (b) assemble `skill` contents into the agent's system prompt at compile time.
  - *Existing-design verdict*: `buildTools` currently maps `toolNames` strings → mouse tool factories via an `if/else if` chain. That design is a name→factory registry and **still serves the goal**, but the chain will grow unwieldy with MCPs. The refactor generalizes it to a registry (name → factory) covering tools AND mcp-bundled tool sets, so adding the saolei MCP does not add a seventh branch — it adds one registry entry. Skill assembly is added to the adapter constructor where `systemPrompt` is already composed, keeping prompt composition in one place.
- **`session-agent.ts` `ProfileData`**: add `skillNames`, `mcpNames` (and fetched skill contents) alongside `toolNames`.
  - *Existing-design verdict*: `ProfileData` is a flat data carrier passed to `AdapterFactory`; adding fields is a natural extension of a still-coherent struct. No layering change.
- **`prompt-client.ts`**: fetch skill contents for the profile's `skill_names` (the `Skill` resource + `GetSkill` RPC already exist).
  - *Existing-design verdict*: `prompt-client.ts` already fetches the profile; extending it to also fetch referenced skills is the same call pattern. Still serves the goal.
- **`game.proto` `MouseMovePart` / `MouseClickPart` (修改)**: add an `InputDelivery delivery` field to each (`SIMULATE` | `WINDOW_MESSAGE`, default `SIMULATE`). Parts declare the *operation*; the desktop implements per the declared delivery. A `WINDOW_MESSAGE` click reads its target coordinate from a companion `MouseMovePart` in the same `PartBlock`.
  - *Existing-design verdict*: a part's purpose is "declare the mouse operation"; the existing `MouseMovePart{x,y}`/`MouseClickPart{action}` shape and the physical-cursor semantics **still serve the goal** and remain the default. Adding `delivery` generalizes the operation to also declare its preferred delivery — a natural extension, not stacked logic. The existing mouse tool is untouched (it leaves `delivery` unset → `SIMULATE`). This replaces an earlier rejected idea of tool-specific `WindowMousePart`/`WindowKeyPart` variants, which would have bound the wire format to a tool.
- **`game.proto` `Part.kind` oneof (修改)**: add the new generic `key_press` (`KeyPart`) member. Additive — backward compatible.
- **`operation-bridge.ts` `dispatch` (修改)**: generalize to dispatch a **`PartBlock`** (one or more parts) as a single operation and await one `ToolResultPart`. The single-part path becomes the one-element block.
  - *Existing-design verdict*: `dispatch` currently wraps one Part in a single-element `PartBlock` content frame (`envelope.content = { parts: [part] }`) and correlates by `tool_id` + 5s timeout. That design is **already PartBlock-shaped** and still serves the goal; the refactor accepts a multi-part block (e.g. `[MouseMovePart, MouseClickPart]`) so a move+click combo is one atomic dispatch with one result. The correlation/timeout/sink logic is unchanged and transport-agnostic.
- **Desktop `view_model.go` `CreateAgentProfileView`**: add `SkillNames`/`McpNames` (currently explicitly omitted — the dormant gap FR-033 closes).
- **Desktop frontend `ProfileManagement.svelte`**: add MCP + skill selection chips to create/edit forms (mirroring the existing `toolNames` chip pattern), and include `mcp_names`/`skill_names` in the create payload + update mask.
- **Desktop frontend `api.ts`**: already carries `skillNames`/`mcpNames` on `AgentProfile` — wire the create/edit calls to actually send them.

### 删除 (Delete) — locations removed
- **`src/mouse-tool.ts`** (flat location): the file is **relocated** to `tools/mouse/`. The old flat path ceases to exist; imports update. (Functionally a move: 删除 of the old location + 新增 of the new.)

## Project Structure

### Documentation (this feature)

```text
specs/018-saolei-mcp/
├── plan.md              # This file
├── research.md          # Phase 0: LangChain MCP/skill + deep-agents reference
├── data-model.md        # Phase 1: board state, cell state, MCP lifecycle, Part additions, profile extensions
├── quickstart.md        # Phase 1: end-to-end validation guide
├── contracts/           # Phase 1: MCP tool schemas, window-message Part contract, profile API contract
└── tasks.md             # Phase 2 (/speckit.tasks — NOT created by /speckit.plan)
```

### Source Code (repository root) — agent service reorganization

```text
projects/game/agent/src/
├── llm.ts                       # 修改: buildTools registry + skill assembly + AdapterFactory
├── session-agent.ts             # 修改: ProfileData += skillNames, mcpNames
├── prompt-client.ts             # 修改: fetch skill contents
├── operation-bridge.ts          # 修改: accept window-message Parts
├── handler.ts, server.ts, ...   # unchanged
├── tools/
│   ├── README.md                # 新增: tool directory conventions
│   └── mouse/                   # mouse tool relocated here (from flat src/mouse-tool.ts)
│       ├── mouse-tool.ts
│       └── mouse-tool.test.ts
├── mcp/
│   ├── README.md                # 新增: MCP directory conventions
│   └── saolei/                  # 新增: the saolei MCP
│       ├── board.ts             # board state + cell-state enum + lifecycle
│       ├── geometry.ts          # coordinate computation (200/24/32 constants)
│       ├── validation.ts        # legality rules
│       ├── saolei-tools.ts      # the five LangChain tool factories
│       ├── saolei-mcp.ts        # per-session MCP instance (server facade)
│       └── *.test.ts
└── skill/
    ├── README.md                # 新增: skill directory conventions
    └── saolei/                  # 新增: companion skill
        └── saolei.skill.md      # the skill document (markdown)

projects/game/
├── game.proto                   # 修改: Part.kind += window-message variants
└── desktop/
    ├── view_model.go            # 修改: CreateAgentProfileView += skill/mcp names
    └── frontend/src/
        ├── api.ts               # 修改: wire skill/mcp names through create/edit
        └── components/ProfileManagement.svelte  # 修改: MCP + skill selection chips
```

**Structure Decision**: the agent service adopts a three-bucket capability layout (`tools/`, `mcp/`, `skill/`), each self-describing via README. The mouse tool moves from flat `src/` into `tools/mouse/` (FR-029). The saolei MCP lives under `mcp/saolei/` and its skill under `skill/saolei/` (FR-030). Proto and desktop changes are localized to their existing files.

## Design Decisions (confirmed by Phase 0 — see [research.md](./research.md))

> D-1 and D-2 were researched against official docs and source per Constitution §III and are now **confirmed** (no new runtime dependency). D-3..D-6 are stable design directions not dependent on external research.

- **D-1 (MCP integration) — CONFIRMED: in-process MCP = ordinary LangChain tools, grouped logically as an "MCP"**: `@langchain/mcp-adapters` v1.1.3 supports only stdio/http/sse — not in-process — and its `loadMcpTools()` produces `DynamicStructuredTool`, the same type our `tool()`+zod factories already yield. Running the MCP wire protocol in-process would add serialization for zero benefit. The saolei tools are therefore exposed as ordinary LangChain tools (identical to the mouse tool), selected by the profile's `mcp_names`; "MCP" is the conceptual grouping (one entry → a bundle of tools + shared state). **No new dependency.** See [research.md §R-1](./research.md).
- **D-2 (Skill injection) — CONFIRMED: assemble skill markdown into the system prompt at agent creation**: deep agents' `SkillsMiddleware.modify_request()` appends a block to the system message — i.e. system-prompt assembly; LangChain.js's own "skills" pattern is likewise middleware + system-prompt assembly. The saolei skill is a `SKILL.md` document (Agent Skills open standard) fetched via the existing `GetSkill` RPC and concatenated into the `systemPrompt` passed to `createAgent`. No new LangChain concept. See [research.md §R-2](./research.md).
- **D-3 (Input delivery) — delivery mode on existing mouse parts + generic KeyPart + PartBlock multi-part dispatch**: parts declare the *operation* (mouse move, mouse click, key press) and are **tool-agnostic**; the desktop owns the *implementation*. `MouseMovePart`/`MouseClickPart` gain an `InputDelivery` enum (`SIMULATE` | `WINDOW_MESSAGE`, default `SIMULATE` → backward-compatible with the existing physical mouse tool). A `WINDOW_MESSAGE` click targets the coordinate supplied by a companion `MouseMovePart` in the same `PartBlock`, so a move+click combo is **one atomic dispatch** (one block, not two LLM tool calls). A new generic `KeyPart` (`KeyAction` enum, starting with `F2`) declares key-press operations; the desktop implements it (PostMessage `WM_KEYDOWN/UP`). **No tool-specific parts** — the saolei MCP reuses the generic mouse/key parts. Occlusion-free (FR-014): `WINDOW_MESSAGE` delivery PostMessages the bound window without moving the OS cursor. See [data-model.md §5](./data-model.md) and [contracts/input-delivery.md](./contracts/input-delivery.md).
- **D-4 (Board state machine) — per-session, in-memory, ready/awaiting-update/terminal**: the MCP instance owns a 2D cell grid + lifecycle marker; `saolei_init` self-resets to all-`block`; click/flag/double_click enter awaiting-update; `saolei_update` returns to ready; `boom` is terminal until re-init; no auto-timeout (FR-011a).
- **D-5 (Rejection semantics) — structured success, not thrown errors**: rejected operations/update-batches return a success result with `{ status: "rejected", reason }`; thrown errors are reserved for infrastructure failure (FR-024a).
- **D-6 (Coordinate constants pinned)**: `top_offset=200`, `left_offset=24`, `block_length=32` px (user-provided). Module constants in `geometry.ts`, not agent-supplied params (FR-015).

## Complexity Tracking

> Fill ONLY if Constitution Check has violations that must be justified.

No violations. Empty.

## References *(mandatory per Constitution §I — Citation Provenance)*

### Official Documentation

- [LangChain.js — overview](https://docs.langchain.com/oss/javascript/langchain/overview) — LangChain v1 documentation; `createAgent` is the agent entry point used by `llm.ts`. Catalog pins: `@langchain/core ^1.2.0`, `langchain ^1.5.0`, `@langchain/langgraph ^1.4.4`.
- [Model Context Protocol — architecture](https://modelcontextprotocol.io/docs/concepts/architecture) — MCP host/client/server topology; this feature embeds the server role in the agent service.
- [Model Context Protocol — introduction](https://modelcontextprotocol.io/) — the MCP standard this feature implements.
- [Minesweeper (video game) — keyboard shortcuts](https://en.wikipedia.org/wiki/Minesweeper_(video_game)) — F2 "new game" convention used by `saolei_init`.

### Repositories

- [`@langchain/mcp-adapters` on npm](https://www.npmjs.com/package/@langchain/mcp-adapters) — version **1.1.3** (2026-03-12); peer deps `@langchain/core@^1.0.0`, `@langchain/langgraph@^1.0.0` (compatible with our catalog). Evaluated and **rejected** for in-process use in [research.md §R-1](./research.md) — supports only stdio/http/sse, no in-process transport.
- [`langchain-ai/langchainjs` — `libs/langchain-mcp-adapters`](https://github.com/langchain-ai/langchainjs/tree/main/libs/langchain-mcp-adapters) — adapter source; `connection.ts` hard-codes `["http","sse","stdio"]`; `tools.ts` shows MCP→`DynamicStructuredTool` conversion.
- [`langchain-ai/deepagents`](https://github.com/langchain-ai/deepagents) — deep agents reference (MIT, Python); commit `46e10640c`. `SkillsMiddleware` ([`middleware/skills.py`](https://github.com/langchain-ai/deepagents/blob/46e10640caf78a84f9715cb8807882ea1b825d6a/libs/deepagents/deepagents/middleware/skills.py)) appends skill metadata to the system message — referenced (not adopted) per [research.md §R-2](./research.md).

### Articles & RFCs

- No article or RFC references (pending Phase 0).

### Repository-Internal References

- `projects/game/agent/src/llm.ts` — `buildTools`, `AdapterFactory`, `AgentAdapterImpl` (the MCP/skill integration surface).
- `projects/game/agent/src/session-agent.ts` — `ProfileData` (extended with skill/mcp names); per-session `SessionAgent` owns the MCP instance (FR-025a).
- `projects/game/agent/src/operation-bridge.ts` — `OperationBridge.dispatch` (extended to accept window-message Parts); the agent→desktop operation channel.
- `projects/game/agent/src/mouse-tool.ts` — existing tool factory pattern (relocated to `tools/mouse/`); the template the saolei tools follow.
- `projects/game/game.proto` — `Part.kind` oneof (extended with window-message variants); `AgentProfile` (`skill_names`/`mcp_names`/`tool_names`); `Skill` message; `PromptService` RPCs.
- `projects/game/desktop/view_model.go` — `CreateAgentProfileView` (dormant skill/mcp names re-enabled).
- `projects/game/desktop/frontend/src/components/ProfileManagement.svelte` — profile editor (MCP/skill chips added).
- `specs/013-agent-game-tools/spec.md`, `specs/014-mouse-move-screenshot/spec.md` — prior features establishing the tool/bridge/screenshot baseline this MCP builds on.
