# Implementation Plan: Agent Game Tools and Image Turns

**Branch**: `013-agent-game-tools` | **Date**: 2026-06-21 | **Spec**: `specs/013-agent-game-tools/spec.md`

**Input**: Feature specification from `specs/013-agent-game-tools/spec.md`

## Summary

Extend the existing game session chat loop so a desktop operator can bind a game window, attach one PNG screenshot to a user turn, and let a profile-scoped LangChain agent request one supervised mouse operation. The implementation keeps the current session/profile WebSocket architecture, extends `projects/game/game.proto` and generated frontend types for multimodal user turns, `tool_names`, simultaneous left-right mouse operation, and a dedicated operation result frame, then updates the agent, desktop, prompt service, fake LLM service, and large-test flows around those shared contracts.

## Technical Context

**Language/Version**: Go 1.26.2 for desktop, proto-backed services, and large tests; TypeScript 6.0.3 on Node >=20 for the agent and Svelte desktop frontend. LangChain package metadata also declares Node >=20 for `langchain` 1.5.0 ([langchain v1.5.0 package.json](https://github.com/langchain-ai/langchainjs/blob/6d212ef91aff/libs/langchain/package.json)).

**Primary Dependencies**: Existing `langchain` 1.5.x, `@langchain/core` 1.2.x, `@langchain/openai` 1.5.x, and `@langchain/langgraph` 1.4.x remain the agent foundation; `createAgent` accepts `tools` and middleware in the public source API ([createAgent source](https://github.com/langchain-ai/langchainjs/blob/6d212ef91aff/libs/langchain/src/agents/index.ts)). Existing Wails v2.12.0 remains the desktop shell and has a public v2.12.0 module tag ([Wails v2.12.0 go.mod](https://github.com/wailsapp/wails/blob/v2.12.0/v2/go.mod)). Svelte 5 remains the frontend UI framework and is MIT-licensed with browser exports ([Svelte package.json](https://github.com/sveltejs/svelte/blob/main/packages/svelte/package.json)). Add `marked` ^18.0.5 for markdown parsing because it exposes browser ESM exports and is MIT-licensed ([marked v18.0.5 package.json](https://github.com/markedjs/marked/blob/v18.0.5/package.json)); add `dompurify` ^3.4.11 because Marked explicitly does not sanitize output and recommends sanitizing generated HTML, while DOMPurify provides strict allow-list sanitization ([Marked safe output guidance](https://github.com/markedjs/marked/blob/v18.0.5/docs/INDEX.md), [DOMPurify allow-list guidance](https://github.com/cure53/DOMPurify/wiki/Security-Goals-&-Threat-Model), [DOMPurify v3.4.11 package.json](https://github.com/cure53/DOMPurify/blob/3.4.11/package.json)). Add `zod` to the pnpm workspace catalog (`zod: "^3.x"`) and to `projects/game/agent/package.json` as a catalog dependency, because the LangChain `tool()` factory requires Zod schemas for structured input ([LangChain tool() function definition (V3)](https://github.com/langchain-ai/langchainjs/blob/6d212ef91aff/langchain-core/src/tools/index.ts)).

**Storage**: Prompt service MongoDB documents for agent profiles gain `tool_names`; session conversation/checkpoint storage remains process-lifetime in-memory for this milestone; screenshot attachment bytes are carried in live frames and conversation view state, not durable storage.

**Testing**: Bazel unit tests for Go and TypeScript packages, Svelte frontend build through the Bazel pnpm wrapper, and game large tests through the repository `testplan` skill. Deterministic fake LLM coverage must extend beyond text to multimodal request capture and tool-call response simulation.

**Target Platform**: Desktop interaction targets the existing Wails `windows/amd64` desktop artifact; services and large tests continue to run in the existing game SUT deployment.

**Project Type**: Multi-component game application: Go/proto control services, TypeScript LangChain agent service, Wails/Svelte desktop app, fake LLM test service, and Bazel-driven large tests.

**Performance Goals**: Preserve SC-001: bind window, attach screenshot, send text-plus-image, and display text plus collapsed image within 10 seconds for at least 95% of test attempts. Reject oversized images before send in 100% of tests.

**Constraints**: Maximum image payload is 5 MiB per user turn; no automatic follow-up screenshot after an operation result; missing or empty `tool_names` exposes no tools; raw HTML in agent markdown must be stripped; gRPC max message size is 8 MiB uniformly across all 3 hops (proxy ↔ agent, agent ↔ desktop WS) ([gRPC 8 MiB default](https://grpc.io/docs/guides/performance/#keepalive-ping)).

**Scale/Scope**: One user-published screenshot attachment per user turn; initial tool set is the `mouse` tool only; keyboard tools, MCP tools, autonomous multi-step loops, and durable image history are out of scope.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Citation Provenance (§I)**: PASS. External dependency/API claims in this plan cite official documentation or source/package metadata inline, with grouped references below.
- **Code Style Precedence (§II)**: PASS. Implementation tasks must explicitly read relevant `style/` documents before editing Go, TypeScript, Svelte, proto, or large-test files.
- **External Dependency Research (§III)**: PASS. LangChain, Wails, Svelte, marked, and DOMPurify were checked against official docs/source/package metadata before being recorded here.

## Project Structure

### Documentation (this feature)

```text
specs/013-agent-game-tools/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   └── agent-game-tools.md
└── tasks.md             # Created by /speckit.tasks, not /speckit.plan
```

### Source Code (repository root)

```text
projects/game/
├── game.proto                         # shared frame/profile/message contract extensions (oneof payload: operation_result=22, user_turn=23)
├── agent/                             # LangChain tool exposure and multimodal turn handling
│   ├── src/llm.ts
│   ├── src/handler.ts
│   ├── src/prompt-client.ts
│   ├── src/operation-bridge.ts        # session-scoped OperationBridge (NEW)
│   ├── src/mouse-tool.ts              # LangChain mouse tool definition (NEW)
│   └── src/bootstrap-test.ts
├── desktop/                           # Wails backend, screenshot binding, operation execution/result frames
│   ├── app.go
│   ├── view_model.go
│   ├── internal/capture/
│   ├── internal/operation/
│   │   └── execute_v2.go              # new 5-action mouse executor (NEW)
│   └── frontend/src/
├── prompt/                            # profile tool_names persistence and API mapping
│   ├── domain/
│   ├── handler/
│   └── runtime/mongo/
├── fake-llm/                          # deterministic multimodal/tool-call fake responses (extended with tools config section)
└── testplan/                          # large-test acceptance flows
```

**Structure Decision**: Use the existing shared `projects/game/game.proto` transport contract as the sole API surface. The `oneof payload` field numbers are updated: `operation_result = 22` (was incorrectly 20), `user_turn = 23`. Service-specific code maps to and from that contract in its existing package (`agent`, `desktop`, `prompt`, `fake-llm`, `testplan`) to avoid duplicate frame or profile types.

## Complexity Tracking

No constitution gate violations.

## Phase 0: Research Summary

See `specs/013-agent-game-tools/research.md`.

## Phase 1: Design Summary

See `specs/013-agent-game-tools/data-model.md`, `specs/013-agent-game-tools/contracts/agent-game-tools.md`, and `specs/013-agent-game-tools/quickstart.md`.

## Post-Design Constitution Check

- **Citation Provenance (§I)**: PASS. Design artifacts cite the parent spec or researched external sources where they depend on external behavior. All `main`/`master` branch URLs replaced with version-tagged or commit-SHA URLs.
- **Code Style Precedence (§II)**: PASS. No source code was edited in this planning phase; implementation tasks must include style-read acceptance criteria.
- **External Dependency Research (§III)**: PASS. New dependencies (`marked ^18.0.5`, `dompurify ^3.4.11`, `zod ^3.x`) researched against tagged source repositories and official documentation. LangChain and LangGraph dependencies verified at specific commits. No `main`/`master` references remain.

## References *(mandatory per Constitution §I — Citation Provenance)*

### Official Documentation

- [LangChain JavaScript agents documentation](https://docs.langchain.com/oss/javascript/langchain/agents) — agent tool/function calling baseline.
- [LangChain JavaScript v1 migration notes: multimodal messages](https://docs.langchain.com/oss/javascript/migrate/langchain-v1) — text and image content block shape.
- [LangChain JavaScript message content blocks](https://docs.langchain.com/oss/javascript/langchain/messages) — text/image content block fields.
- [Marked documentation: safe output guidance](https://github.com/markedjs/marked/blob/v18.0.5/docs/INDEX.md) — marked output is not sanitized and requires output filtering.
- [DOMPurify security goals and allow-list guidance](https://github.com/cure53/DOMPurify/wiki/Security-Goals-&-Threat-Model) — strict sanitizer configuration for markdown/comment output.
- [gRPC performance guide: keepalive and max message size](https://grpc.io/docs/guides/performance/#keepalive-ping) — 8 MiB default max message size reference.

### Repositories

- [langchain v1.5.0 package.json](https://github.com/langchain-ai/langchainjs/blob/6d212ef91aff/libs/langchain/package.json) — version, Node engine, license, repository metadata.
- [LangChain createAgent source](https://github.com/langchain-ai/langchainjs/blob/6d212ef91aff/libs/langchain/src/agents/index.ts) — tools and middleware API surface.
- [LangChain tool() function definition (V3)](https://github.com/langchain-ai/langchainjs/blob/6d212ef91aff/langchain-core/src/tools/index.ts) — `tool()` factory requires Zod schema for structured input.
- [LangChain multimodal content block types (V2)](https://github.com/langchain-ai/langchainjs/blob/6d212ef91aff/langchain-core/src/messages/content/multimodal.ts) — HumanMessage multimodal constructor shape.
- [LangChain ChatOpenAI completions converter (V4)](https://github.com/langchain-ai/langchainjs/blob/d43194b62/langchain-openai/src/converters/completions.ts) — ChatOpenAI serializes image content as `image_url` format.
- [LangGraph checkpoint base.ts (V1)](https://github.com/langchain-ai/langgraphjs/blob/981853c01979/libs/checkpoint/src/base.ts) — MemorySaver preserves content blocks through JSON serialization.
- [LangGraph checkpoint JSON plus serializer (V1)](https://github.com/langchain-ai/langgraphjs/blob/981853c01979/libs/checkpoint/src/serde/jsonplus.ts) — Uint8Array handling in checkpoint serialization.
- [Wails v2.12.0 go.mod](https://github.com/wailsapp/wails/blob/v2.12.0/v2/go.mod) — researched existing Wails module version.
- [Svelte package.json](https://github.com/sveltejs/svelte/blob/main/packages/svelte/package.json) — researched Svelte 5 browser exports and license.
- [marked v18.0.5 package.json](https://github.com/markedjs/marked/blob/v18.0.5/package.json) — researched current marked version, browser exports, Node engine, and MIT license.
- [DOMPurify v3.4.11 package.json](https://github.com/cure53/DOMPurify/blob/3.4.11/package.json) — researched current DOMPurify version, browser exports, and license.

### Articles & RFCs

- No article or RFC references.
