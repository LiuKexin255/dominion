# Tasks: Mouse Move Action & Post-Operation Screenshot Feedback

**Input**: Design documents from `specs/014-mouse-move-screenshot/`

**Prerequisites**: [plan.md](plan.md) (required), [spec.md](spec.md) (required), [research.md](research.md), [data-model.md](data-model.md), [contracts/mouse-operation.md](contracts/mouse-operation.md)

**Organization**: Tasks grouped by user story. US1 (screenshot feedback, P1) is the MVP. US2 (MOVE action, P2) builds on US1's screenshot infrastructure.

## Format: `[ID] [P?] [Story] [新增|修改|删除] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1, US2)
- **[新增|修改|删除]**: Change classification per Constitution §V
- Include exact file paths in descriptions

## Constitution Check

*GATE: Must pass before implementation begins.*

- **Citation Provenance (§I)**: tasks referencing the LangChain tool image-return pattern cite [research.md R-001](research.md#r-001). No new external dependencies are introduced.
- **Code Style Precedence (§II)**: before starting ANY Go task, read `style/golang.md`. Before starting ANY TypeScript task, follow Google TypeScript Style (`style/README.md`). Confirm review in the task's commit message.
- **External Dependency Research (§III)**: no new dependencies. All Go image processing is stdlib. LangChain image return researched in [research.md R-001](research.md#r-001).
- **Implementation Checkpointing (§IV)**: this file has 2 user-story phases — CHECK tasks inserted at each phase boundary.
- **Refactoring-Oriented Changes (§V)**: every task carries 新增/修改/删除 classification from [plan.md](plan.md). 修改 tasks implemented as refactors.

## Phase 1: Foundational (Proto Model + Codegen)

**Purpose**: Shared proto changes that both user stories depend on. US1 needs the `screenshot` field on `AgentOperationResultFrame`; US2 needs the `MOVE` value in `AgentMouseAction`.

- [ ] T001 [修改] Add `AGENT_MOUSE_ACTION_MOVE = 6;` to `AgentMouseAction` enum in `projects/game/game.proto` (line ~342)
- [ ] T002 [修改] Add `AgentImageFrame screenshot = 4;` field to `AgentOperationResultFrame` message in `projects/game/game.proto` (line ~399)
- [ ] T003 Run `bazel run //:gazelle projects/game` to regenerate `BUILD.bazel`, then `bazel build //projects/game/...` to regenerate Go (`.pb.go`) and TypeScript (`game_types/`) proto types for the two changes above
- [ ] CHECK Foundational phase: confirm proto changes match [data-model.md](data-model.md), generated code compiles, and all work is committed. Fix deviations before starting US1.

---

## Phase 2: User Story 1 — Post-Operation Screenshot Feedback (Priority: P1) 🎯 MVP

**Goal**: After executing any mouse operation, the desktop captures a screenshot of the bound window, applies a red-ring overlay marker at the executed coordinate, and returns it in the operation result. The agent receives the screenshot as image content blocks in the tool result.

**Independent Test**: The agent issues a LEFT_CLICK, the desktop returns a screenshot with a visible marker at the click position in the tool result, and the agent can describe the post-action screen state.

### Desktop (Go) — Screenshot Capture + Overlay Marker

- [ ] T004 [P] [US1] [新增] Create `ApplyMarker(pngData []byte, x, y int) ([]byte, error)` in `projects/game/desktop/internal/operation/marker.go` — decode PNG via stdlib `image/png`, draw red ring (radius 12px, `color.RGBA{255,0,0,255}`) at `(x,y)` using midpoint-circle algorithm on the decoded pixel buffer, re-encode to PNG. Clamp coordinates to image bounds. Ref: [research.md R-002](research.md#r-002).
- [ ] T005 [P] [US1] [新增] Create marker tests in `projects/game/desktop/internal/operation/marker_test.go` — verify `ApplyMarker` draws red pixels at the expected ring positions for a known input image, returns error for invalid PNG input, and clamps out-of-bounds coordinates.
- [ ] T006 [US1] [修改] Restructure `executeAgentOperation` in `projects/game/desktop/app.go` (line ~582) to capture screenshot after mouse action (success or failure per FR-007), apply `ApplyMarker` at `mouse.GetXPx()/GetYPx()`, enforce 5 MiB size limit (FR-010), set `Screenshot` field on result frame. **Refactor**: replace the `failed()` early-return pattern with an error-accumulation flow that always proceeds to the screenshot phase. Design verdict: the existing two-phase design (validate→execute) is extended with a third phase (capture screenshot); the function's single-exit structure is preserved. Ref: [research.md R-005](research.md#r-005).

### Agent (TypeScript) — Bridge + Handler + Tool Return Format

- [ ] T007 [P] [US1] [修改] Extend `OperationResult` interface with optional `screenshot?: OperationScreenshot` (new interface: `{data: string; widthPx: number; heightPx: number}`) and update `handleResult` to extract screenshot from result frame in `projects/game/agent/src/operation-bridge.ts`. Design verdict: one optional field added; existing consumers reading only status/message are unaffected.
- [ ] T008 [P] [US1] [修改] Extend the `operationResult` inline type in the `Connect` handler (line ~155) to include `screenshot?: {data?: Uint8Array|string; encoding?: string; widthPx?: number; heightPx?: number}` and pass it through to `sa.getBridge().handleResult()` in `projects/game/agent/src/handler.ts`.
- [ ] T009 [US1] [修改] Refactor mouse tool callback in `projects/game/agent/src/mouse-tool.ts` (line ~61) to return a content-block array `[{type:"text",text:status}, {type:"image_url",image_url:{url}}?, {type:"text",text:sizeAnnotation}?]` instead of a plain string. Append size-annotation text block `[图片像素尺寸：${w}×${h}（宽×高，单位：像素）。鼠标工具坐标基于此像素空间。]` after image when screenshot is present (FR-014). Ref: [research.md R-001](research.md#r-001), [research.md R-006](research.md#r-006).
- [ ] CHECK US1 checkpoint: confirm T004–T009 match [plan.md](plan.md) change classification and [contracts/mouse-operation.md](contracts/mouse-operation.md), screenshot flows end-to-end, and all work is committed. Fix deviations before starting US2.

---

## Phase 3: User Story 2 — Mouse Move Action (Priority: P2)

**Goal**: The agent can issue a MOVE operation that repositions the cursor without clicking. MOVE requires a bound window (same as click actions per FR-004) and returns a post-action screenshot with marker (inherited from US1 infrastructure).

**Independent Test**: The agent issues a MOVE to (x, y), the cursor moves without any button events, and the result includes a screenshot with marker at (x, y).

### Desktop (Go) — MOVE Action Validation + Event Sequence

- [ ] T010 [US2] [修改] Add `game.AgentMouseAction_AGENT_MOUSE_ACTION_MOVE` to the valid-actions switch in `validateMouseAction` in `projects/game/desktop/internal/operation/execute_v2_logic.go` (line ~42). Design verdict: function already switches on all enum values; one case added.
- [ ] T011 [US2] [修改] Add `MOVE` case to `actionEventSequence` switch returning `[]uint32{}` (empty slice) in `projects/game/desktop/internal/operation/execute_v2_logic.go` (line ~61). Design verdict: same switch-based design; empty sequence means the SendInput loop runs zero iterations — SetCursorPos alone moves the cursor. Ref: [research.md R-004](research.md#r-004).

> T010 and T011 are in the same file (`execute_v2_logic.go`) — do them together in one edit session.

### Agent (TypeScript) — MOVE in Tool Schema

- [ ] T012 [US2] [修改] Add `"MOVE"` to the zod `action` enum, add `MOVE: "AGENT_MOUSE_ACTION_MOVE"` to `MOUSE_ACTION_TO_PROTO` map, and update the tool description string to mention MOVE and screenshot feedback in `projects/game/agent/src/mouse-tool.ts`. Ref: [data-model.md mouse tool schema change](data-model.md#mouse-tool-schema-change-修改). (Depends on T009 — same file.)
- [ ] CHECK US2 checkpoint: confirm T010–T012 match [plan.md](plan.md), MOVE produces no button events, and all work is committed. Fix deviations before the Polish phase.

---

## Phase 4: Polish & Cross-Cutting Concerns

- [ ] T013 [修改] Update existing `validateMouseAction` and `actionEventSequence` tests in `projects/game/desktop/internal/operation/execute_v2_test.go` to cover the new `MOVE` action (validate accepts MOVE; sequence returns empty).
- [ ] T014 [修改] Add test for mouse tool screenshot return format in `projects/game/agent/src/mouse-tool.test.ts` — when bridge returns a result with screenshot, the tool callback returns `[{type:"text"}, {type:"image_url"}, {type:"text"}]` with correct annotation text.
- [ ] T015 [修改] Add or update user-turn image annotation coverage in `projects/game/agent/src/llm.test.ts` — verify user-turn images still append the `[图片像素尺寸：W×H...]` text block after the image so FR-014 covers both user-turn and tool-result image paths.
- [ ] T016 [修改] Update `AgentMouseAction` enum and `AgentOperationResultFrame` interface in `projects/game/desktop/frontend/src/api.ts` to include `MOVE = 6` and optional `screenshot` field (keeps frontend types in sync with proto).
- [ ] T017 Run `bazel test //projects/game/desktop:desktop_test && bazel test //projects/game/agent:lib_test` and verify all tests pass.
- [ ] T018 Run `bazel build --platforms=@rules_go//go/toolchain:windows_amd64 //projects/game/desktop:desktop_lib` to verify Windows cross-compilation.
- [ ] T019 [修改] Execute `specs/014-mouse-move-screenshot/quickstart.md` Scenario 5 on Windows and record whether screenshot capture + marker + tool-result delivery completes within the SC-004 500 ms target.

---

## Dependencies & Execution Order

### Phase Dependencies

```
Phase 1 (Foundational: proto + codegen)
  └──→ Phase 2 (US1: screenshot feedback) 🎯 MVP
        └──→ Phase 3 (US2: MOVE action)
              └──→ Phase 4 (Polish: tests + frontend sync)
```

### Task-Level Dependencies

| Task | Depends On | Reason |
|------|-----------|--------|
| T002 | T001 | Same file (`game.proto`) |
| T003 | T001, T002 | Codegen needs proto changes |
| T006 | T003, T004 | Needs proto types + marker function |
| T009 | T007, T008 | Tool callback uses bridge result shape |
| T011 | T010 | Same file (`execute_v2_logic.go`) |
| T012 | T009 | Same file (`mouse-tool.ts`) |
| T013 | T010, T011 | Tests the MOVE changes |
| T014 | T009 | Tests the tool return format |
| T015 | T009 | Tests the existing user-turn image annotation path for FR-014 |
| T016 | T003 | Frontend types mirror proto |
| T019 | T006, T009, T012 | Manual end-to-end latency validation requires screenshot feedback, tool-result image return, and MOVE schema |

### Parallel Opportunities

- **Phase 2**: T004+T005 (marker.go + test) run in parallel with T007+T008 (bridge + handler). Then T06 joins them, then T009.
- **Phase 3**: T010+T011 (Go) can run in parallel with T012 (TS) after T009 completes.

## Implementation Strategy

### MVP First (US1 Only)

1. Complete Phase 1: proto changes + codegen
2. Complete Phase 2: US1 (screenshot feedback)
3. **STOP and VALIDATE**: issue a click, verify screenshot + marker returned to agent
4. Deploy/demo if ready — the agent can now self-correct coordinates

### Incremental Delivery

1. Foundational → proto types ready
2. US1 → agent gets visual feedback after every mouse operation
3. US2 → agent gains non-destructive MOVE for positioning
4. Polish → tests + frontend sync

## Notes

- [P] tasks = different files, no dependencies on incomplete tasks
- Each user story is independently testable (US2 test uses US1's screenshot infrastructure but MOVE itself works without it)
- Commit after each task or logical group
- CHECK tasks are §IV checkpoints — verify work matches plan and is committed before proceeding
- §II style review: read `style/golang.md` before Go tasks, `style/README.md` before TS tasks

## References *(mandatory per Constitution §I — Citation Provenance)*

### Official Documentation

- [Protocol Buffers Language Guide — Updating Message Types](https://protobuf.dev/programming-guides/proto3/#updating) — proto3 backward-compatible field addition (inherited from [research.md R-003](research.md))

### Repositories

- [langchain-ai/langchainjs — _formatToolOutput](https://github.com/langchain-ai/langchainjs/blob/3bebc82d6a56e9afa99b61a68b5a3b7d3382a46b/libs/langchain-core/src/tools/index.ts#L785-L811) — tool content-block array passthrough (inherited from [research.md R-001](research.md))
- [langchain-ai/langchainjs — Anthropic _formatContentBlocks](https://github.com/langchain-ai/langchainjs/blob/3bebc82d6a56e9afa99b61a68b5a3b7d3382a46b/libs/providers/langchain-anthropic/src/utils/message_inputs.ts#L148-L162) — image_url serialization in tool results (inherited from [research.md R-001](research.md))

### Articles & RFCs

- No external articles or RFCs referenced.
