# Implementation Plan: Mouse Move Action & Post-Operation Screenshot Feedback

**Branch**: `014-mouse-move-screenshot` | **Date**: 2026-06-25 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/014-mouse-move-screenshot/spec.md`

## Summary

Extend the mouse tool with (1) a new MOVE action for cursor repositioning without clicking, and (2) post-operation screenshot feedback — after every mouse operation, the desktop captures a screenshot of the bound window, applies a red-ring overlay marker at the executed coordinate, and returns it in the operation result. The agent receives the screenshot as image content blocks in the tool result, enabling self-correction of coordinate estimates. All images — user-turn and tool-result — carry a pixel-dimension annotation text block so the agent calibrates against the correct pixel space.

## Technical Context

**Language/Version**: Go 1.23 (desktop), TypeScript 5.x (agent)

**Primary Dependencies**: Go stdlib `image`/`image/png`/`image/draw` (overlay marker), LangChain `langchain@1.5.0` / `@langchain/core@1.2.0` (tool image return), Protocol Buffers (proto model)

**Storage**: N/A

**Testing**: `bazel test` (Go unit tests via `go_test`, TypeScript unit tests via vitest)

**Target Platform**: Windows (desktop), Linux (agent service)

**Project Type**: desktop-app + agent gRPC service

**Performance Goals**: Screenshot capture + overlay + delivery < 500 ms (SC-004)

**Constraints**: Operation-bridge dispatch timeout is 5 s — must accommodate screenshot capture round-trip; screenshot ≤ 5 MiB (FR-010)

**Scale/Scope**: Single-session, single-window desktop automation

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Citation Provenance (§I)**: External facts in this plan — LangChain `_formatToolOutput` passthrough behavior, proto3 backward-compatible field addition — carry inline citations in [research.md](research.md) with pinned versions. No new external dependencies are introduced; all Go packages used are stdlib. ✅
- **Code Style Precedence (§II)**: Implementation tasks MUST read `style/golang.md` (Go) and `style/README.md` (TS: Google TypeScript Style) before code changes. ✅ — inherited by tasks.md.
- **External Dependency Research (§III)**: No new external dependencies. LangChain image-return format researched against source code (R-001). Go stdlib image processing (R-002). Proto compatibility (R-003). All documented in research.md with citations. ✅
- **Implementation Checkpointing (§IV)**: This plan spans two user-story phases (screenshot feedback + MOVE action). Check tasks will be inserted at the phase boundary in tasks.md. ✅
- **Refactoring-Oriented Changes (§V)**: Every change classified below as 新增 / 修改 / 删除. 修改 changes implemented as refactors of existing units. Design verdicts recorded. ✅

*Post-Phase-1 re-check*: All research findings (R-001 through R-006) are consolidated. No new dependencies discovered. No gates violated. ✅

## Project Structure

### Documentation (this feature)

```text
specs/014-mouse-move-screenshot/
├── plan.md              # This file
├── research.md          # Phase 0 — R-001 through R-006
├── data-model.md        # Phase 1 — proto + TS type changes
├── quickstart.md        # Phase 1 — validation scenarios
├── contracts/
│   └── mouse-operation.md # Phase 1 — wire protocol + tool contract
└── tasks.md             # Phase 2 output (/speckit.tasks)
```

### Source Code (repository root)

```text
projects/game/
├── game.proto                          # [修改] AgentMouseAction + AgentOperationResultFrame
├── *.pb.go                             # [auto-regen] Go generated proto code
└── game_types/                         # [auto-regen] TS generated types
    └── projects/game/
        ├── AgentMouseAction.ts
        └── AgentOperationResultFrame.ts

projects/game/desktop/
├── app.go                              # [修改] executeAgentOperation — screenshot capture flow
└── internal/operation/
    ├── execute_v2_logic.go             # [修改] validateMouseAction + actionEventSequence — add MOVE
    └── marker.go                       # [新增] overlay marker drawing (stdlib image)
    └── marker_test.go                  # [新增] overlay marker tests

projects/game/agent/src/
├── mouse-tool.ts                       # [修改] MOVE in schema + image return + size annotation
├── mouse-tool.test.ts                  # [修改] screenshot return tests
├── operation-bridge.ts                 # [修改] OperationResult + handleResult — screenshot data
├── operation-bridge.test.ts            # [修改] screenshot passthrough test
└── handler.ts                          # [修改] operationResult inline type — screenshot field
```

**Structure Decision**: Existing monorepo layout — no new directories or projects. The only new file is `marker.go` (and its test) in the already-existing `internal/operation/` package.

## Change Classification (§V)

### Proto Layer

| # | File | Classification | Change | Design Verdict |
|---|------|---------------|--------|----------------|
| 1 | `game.proto` — `AgentMouseAction` | 修改 | Add `AGENT_MOUSE_ACTION_MOVE = 6` | Enum extension; existing values unchanged. Design still serves: one action per operation. |
| 2 | `game.proto` — `AgentOperationResultFrame` | 修改 | Add `AgentImageFrame screenshot = 4` (optional) | Message extension; existing fields unchanged. Result frame gains an optional visual payload — natural evolution of the "execute and report" contract into "execute, show, and report". |

### Desktop Go Layer

| # | File | Classification | Change | Design Verdict |
|---|------|---------------|--------|----------------|
| 3 | `execute_v2_logic.go` — `validateMouseAction` | 修改 | Add `AGENT_MOUSE_ACTION_MOVE` to valid switch | Function already switches on all enum values; adding one case is the intended extension pattern. |
| 4 | `execute_v2_logic.go` — `actionEventSequence` | 修改 | Add `MOVE` case returning `[]uint32{}` | Same switch-based design. Empty sequence means the SendInput loop runs zero iterations — SetCursorPos alone moves the cursor. Design still serves: one function maps action → event sequence. |
| 5 | `marker.go` | 新增 | New file: `ApplyMarker(img []byte, x, y int) ([]byte, error)` — decode PNG, draw red ring at (x,y), re-encode | New module in existing package. Pure stdlib, no dependencies. |
| 6 | `app.go` — `executeAgentOperation` | 修改 | Restructure control flow: attempt screenshot after action regardless of action success; apply marker; enforce size limit; set screenshot on result | **Refactor**: the current `failed()` early-return pattern is replaced with an error-accumulation flow that proceeds to the screenshot phase. The function returns a single result frame carrying status, message, and optional screenshot. The existing two-phase design (validate → execute) is preserved; a third phase (capture screenshot) is added as a natural extension. |

### Agent TS Layer

| # | File | Classification | Change | Design Verdict |
|---|------|---------------|--------|----------------|
| 7 | `operation-bridge.ts` — `OperationResult` | 修改 | Add optional `screenshot?: OperationScreenshot` field | Interface gains one optional field. Existing consumers that read only status/message are unaffected. |
| 8 | `operation-bridge.ts` — `handleResult` | 修改 | Extract screenshot from result frame, pass to `pending.resolve` | Same resolve path; one additional field extraction. |
| 9 | `handler.ts` — `operationResult` inline type | 修改 | Add `screenshot?` field to inline type in Connect handler | Inline type mirrors proto shape; one field added. |
| 10 | `mouse-tool.ts` — schema + `MOUSE_ACTION_TO_PROTO` | 修改 | Add `"MOVE"` to zod enum and proto map | Same map+enum pattern; one entry added. |
| 11 | `mouse-tool.ts` — tool callback | 修改 | Return content-block array `[text, image_url?, text?]` instead of string | **Refactor**: callback return type changes from `string` to `ContentBlock[]`. The status text is wrapped in `{type:"text"}`, screenshot (when present) is `{type:"image_url"}`, and the size-annotation text follows the image. This is the canonical LangChain pattern for multimodal tool results (R-001). |
| 12 | `mouse-tool.ts` — tool description | 修改 | Update description string to mention MOVE and screenshot feedback | String update; no structural change. |

## Complexity Tracking

No Constitution Check violations. No complexity justifications needed.

## References *(mandatory per Constitution §I — Citation Provenance)*

### Official Documentation

- [Protocol Buffers Language Guide — Updating Message Types](https://protobuf.dev/programming-guides/proto3/#updating) — proto3 backward-compatible field addition

### Repositories

- [langchain-ai/langchainjs — _formatToolOutput](https://github.com/langchain-ai/langchainjs/blob/3bebc82d6a56e9afa99b61a68b5a3b7d3382a46b/libs/langchain-core/src/tools/index.ts#L785-L811) — tool content-block array passthrough (v1.5.0 / @langchain/core v1.2.0)
- [langchain-ai/langchainjs — Anthropic _formatContentBlocks](https://github.com/langchain-ai/langchainjs/blob/3bebc82d6a56e9afa99b61a68b5a3b7d3382a46b/libs/providers/langchain-anthropic/src/utils/message_inputs.ts#L148-L162) — image_url serialization in tool results

### Articles & RFCs

- No external articles or RFCs referenced.
