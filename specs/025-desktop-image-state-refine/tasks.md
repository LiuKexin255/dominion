# Tasks: Desktop Window-Select Flow, Image-Transfer Hardening & Saolei Text-State Recognition

**Input**: Design documents from `/specs/025-desktop-image-state-refine/` (spec.md, plan.md, research.md, data-model.md, contracts/, quickstart.md)

**Prerequisites**: plan.md (required), spec.md (required), research.md, data-model.md, contracts/, quickstart.md — all required reading (Constitution §V; "spec 相关文件" are not re-listed per phase below).

**Tests**: Unit tests are part of each code task per Constitution principle IV (compile + `bazel test` are not separate tasks). The **large test** is a single acceptance task (principle VI) in the Polish phase.

**Organization**: Phase 1 is the shared foundation (the `FlowResultPart` directive, FR-023..026, no user story). Phases 2–4 are the three user stories (US1/US2/US3, all P1), mutually independent after Phase 1. Phase 5 is polish + large-test acceptance.

## Format: `[ID] [P?] [Story?] Description`

- **[P]**: parallelizable (different files, no dependency on an incomplete task in the same phase)
- **[Story]**: US1/US2/US3 for story-phase tasks; Phase 1 and Phase 5 have no story label
- Exact file paths are included in every task description

---

## Phase 1: Foundation — `FlowResultPart` channel separation (FR-023..FR-026)

**Purpose**: The shared proto contract + the desktop↔agent migration that separates the operation-execution result (control channel) from the display `tool_result`. This is the user's plan-phase directive and the interface foundation (Constitution §III) that US3 builds on and US1 builds atop (to avoid double-editing `executeAgentOperation`).

**⚠️ CRITICAL**: US1 and US3 depend on this phase; it MUST land first.

### 文档清单（编码前必读）

- **代码规范文档**: `style/api.md` + [AIP-140 Field names](https://google.aip.dev/140) + [AIP-126 Enumerations](https://google.aip.dev/126)（proto 字段/枚举约定）; `style/golang.md` + [Google Go Style Guide](https://google.github.io/styleguide/go/guide)（Go 生成类型与绑定）; `style/javascript.md` + [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)（TS 类型）
- **官方文档**: [Protocol Buffers Language Guide (proto3)](https://protobuf.dev/programming-guides/proto3/)（`oneof`、`message`、`bytes` 字段语义）
- **技术文章**: 无

### Tasks

- [ ] T001 Add `FlowResultPart` message and `FlowPart` oneof kind `flow_result = 8` to `projects/game/game.proto` per `specs/025-desktop-image-state-refine/data-model.md` §1 (mirror `ToolResultPart` fields `tool_id`/`status`/`message`/`screenshot`; reuse `ToolResultStatus`; include the doc comments)
- [ ] T002 [P] Regenerate proto-derived types from the change in T001 and confirm a clean build: run `bazel run //:gazelle` (Go bindings + BUILD) and regenerate the TS `game_types`; `bazel build //projects/game/...` green
- [ ] T003 [P] Make the desktop report operation outcomes as `FlowResultPart` on the control channel in `projects/game/desktop/app.go`: `executeAgentOperation` returns `*game.FlowResultPart` (same fields as today's `*game.ToolResultPart`); `handleInboundOperation` builds a `flow_parts` frame whose single part is `FlowPart{flow_result}` (replacing the `message_parts`/`tool_result` frame at `app.go:892-898`). Keep the window source (`boundWin`) unchanged for now. Add/extend unit tests in `projects/game/desktop/app_test.go`; `bazel build //projects/game/desktop:go_default_test && bazel test //projects/game/desktop:go_default_test` green
- [ ] T004 [P] Make the agent consume `FlowResultPart` on the control channel in `projects/game/agent/src/operation-bridge.ts` and `projects/game/agent/src/handler.ts`: change `OperationBridge.handleResult(result: FlowResultPart)` (internal `OperationResult` shape unchanged); route inbound `flow_parts` frames whose kind is `flow_result` to `bridge.handleResult` (today the router pulls `tool_result` out of `message_parts`). Add/extend the bridge unit tests; `bazel test //projects/game/agent:...` green

**Order**: T001 → T002 → {T003, T004}. T003 and T004 are different files (desktop vs agent) and can be developed in parallel, but they MUST deploy together (desktop sends `flow_result`, agent expects `flow_result`) — integrate and verify the end-to-end operation-result path before exiting Phase 1.

**Checkpoint**: An operation dispatched from agent → desktop returns a `FlowResultPart` that resolves the bridge dispatch; no display `tool_result` `MessagePart` is emitted by the desktop for an operation outcome.

---

## Phase 2: User Story 1 — Selected window is the single source of truth (Priority: P1) 🎯 MVP

**Goal**: Picking a window in the dropdown is sufficient for every screenshot and operation; no separate "bind"/"capture to activate" step. Fixes the reported "no window bound" defect (spec FR-001..FR-006, `contracts/window-select-contract.md`).

**Independent Test**: Select a window **without** pressing Capture; send a message that triggers an operation; the operation executes against the selected window and a post-action screenshot is returned (no "no window bound" error).

### 文档清单（编码前必读）

- **代码规范文档**: `style/golang.md` + [Google Go Style Guide](https://google.github.io/styleguide/go/guide)（Go 后端）; `style/javascript.md` + [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)（前端 TS/Svelte）
- **官方文档**: 无
- **技术文章**: 无

### Tasks

- [ ] T005 [US1] Replace `App.boundWin` with a selected-window source of truth in `projects/game/desktop/app.go`: remove the `boundWin` field and the `BindWindow` method (`app.go:200,1249-1277`); add a selected-window state set on dropdown selection plus a resolver that looks up the `capture.WindowRef` via `capture.ListWindows` (the same lookup `BindWindow` performs today); expose the setter to the frontend (Wails binding)
- [ ] T006 [US1] Wire the resolved selected window into operations and screenshots in `projects/game/desktop/app.go` and `projects/game/desktop/app_operation.go`: `executeAgentOperation` resolves the selected `WindowRef` (or returns a graceful "no window selected" failure — replacing the `app.go:1074` guard), passing the handle into `runMouseMoveAndClick`/`runMouseMove`/`runMouseClick` (`app_operation.go:28,94,156`) instead of `a.boundWin.Handle`; `SendUserTurn` (`app.go:693-758`) and `CaptureScreenshot` (`app.go:1279-1309`) read `ScaleFactor`/`WindowTitle` from the resolved `WindowRef`. Add/extend unit tests in `projects/game/desktop/app_test.go`; `bazel build //projects/game/desktop:go_default_test && bazel test //projects/game/desktop:go_default_test` green
- [ ] T007 [US1] Update the frontend to push the selected handle on dropdown change and drop the binding step in `projects/game/desktop/frontend/src/App.svelte`: set the selected handle to the backend on selection (`selectedWindowHandle`, `App.svelte:125`); remove the `bindWindow(selectedWindowHandle)` call from `handleCaptureScreenshot` (`App.svelte:770-788`) so capture uses the selected window directly; the manual Capture affordance remains as "attach a screenshot to my next message" only

**Order**: T005 → T006 → T007 (same files, sequential). Depends on Phase 1 (T003 already changed `executeAgentOperation`'s return type; US1 changes only the window source within it).

**Checkpoint**: Select-then-chat works with no Capture press; re-selecting a window retargets subsequent ops; no selection ⇒ graceful failure.

---

## Phase 3: User Story 2 — Robust, efficient image transfer (Priority: P1)

**Goal**: Image data on the desktop↔gateway WebSocket leg is delivered reliably and never fails on frame size; the desktop read-limit bug is fixed (spec FR-007..FR-011, `contracts/image-transport-contract.md`, `research.md` D1).

**Independent Test**: A turn against a large/high-DPI window (screenshot near/above the prior 5 MiB ceiling, and well above the old 32 KiB WS default) completes with no `ErrMessageTooBig` / WS teardown.

### 文档清单（编码前必读）

- **代码规范文档**: `style/golang.md` + [Google Go Style Guide](https://google.github.io/styleguide/go/guide)
- **官方文档**: [coder/websocket — SetReadLimit / ErrMessageTooBig / message types](https://coder.com/docs/websocket)（默认 32 KiB 限制、超限关闭连接、binary 消息）; [google.golang.org/protobuf/proto](https://pkg.go.dev/google.golang.org/protobuf/proto)（`Marshal`/`Unmarshal`）; [gRPC Go — Server MaxRecvMsgSize](https://pkg.go.dev/google.golang.org/grpc#MaxRecvMsgSize)（T010 校验服务端消息大小上限）
- **技术文章**: 无

### Tasks

- [ ] T008 [P] [US2] Switch the desktop `WSClient` to binary protobuf and fix the read limit in `projects/game/desktop/internal/api/websocket.go`: `SendFrame` uses `proto.Marshal` + `websocket.MessageBinary` (was `protojson.Marshal` + `MessageText`); `RecvFrame` uses `proto.Unmarshal` (was `protojson.Unmarshal`); call `conn.SetReadLimit(10 << 20)` immediately after `websocket.Dial` succeeds in `Connect` (`websocket.go:28-59`). Add unit tests: (a) a > 32 KiB frame round-trips successfully (would fail under the old default); (b) a frame exceeding the 10 MiB limit surfaces as a clear, attributable error (not a hang — validates FR-010). `bazel test` green
- [ ] T009 [P] [US2] Switch the gateway `wsStream` to binary protobuf in `projects/game/gateway/cmd/main.go`: `wsStream.Recv` uses `proto.Unmarshal` and `wsStream.Send` uses `proto.Marshal` + `websocket.MessageBinary` (was protojson text at `main.go:151-173`); retain the sessionID injection (`main.go:161`). Update `projects/game/gateway/cmd/main_test.go` (e.g. `TestReadLimitSet` at `main_test.go:773`, and the protojson round-trip tests) from protojson text to binary; `bazel test //projects/game/gateway/...` green
- [ ] T010 [US2] Verify the gRPC server-side max message size for the proxy/agent hops (`research.md` D1 verification item): the gateway client side is already 8 MiB (`gateway/cmd/main.go:48-51`); confirm the proxy/agent server `MaxRecvMsgSize` tolerates the largest expected screenshot (default gRPC server max is 4 MiB) and raise it if a screenshot can exceed that

**Order**: T008 and T009 are different files (desktop vs gateway) and parallelizable, but MUST deploy together (both WS ends binary). T010 is a verification (no-op if already configured).

**Checkpoint**: A large screenshot round-trips desktop↔gateway with no frame-size failure; both WS ends agree on binary proto + 10 MiB read limit.

---

## Phase 4: User Story 3 — Saolei text-board return & strict validation (Priority: P1)

**Goal**: Saolei tools return a recognized **text** board (via `@dominion/game-saolei-board`) and validate each move **strictly** before dispatch, rejecting illegal moves (spec FR-012..FR-022, `contracts/saolei-mcp-contract.md`, `research.md` D4/D5/D6).

**Independent Test**: `saolei_init` returns a text board (no image); a legal `saolei_click` dispatches and returns the updated board; an illegal move (e.g. click on a revealed cell) is rejected before dispatch with a reason.

**Depends on**: Phase 1 (the screenshot arrives as `FlowResultPart.screenshot` on the control channel).

### 文档清单（编码前必读）

- **代码规范文档**: `style/javascript.md` + [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html); [vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（单测采用依赖注入，禁止跨包 `vi.mock`）
- **官方文档**: `projects/game/pkg/saolei-board/README.md`（识别 API：`recognizeBoard` / `SaoleiBoard.init` / `updateFromScreenshot` / `renderBoardText`、符号图例、单调校验语义、坐标空间注意）; [Model Context Protocol — Tools](https://modelcontextprotocol.io/docs/concepts/tools)（MCP 工具返回 content blocks）
- **技术文章**: 无

### Tasks

- [ ] T011 [P] [US3] Add the `@dominion/game-saolei-board` workspace dependency to the agent: add `"@dominion/game-saolei-board": "workspace:*"` to `projects/game/agent/package.json`; run `bazel run @pnpm -- --dir /mnt/code/dominion/projects/game/agent up`; add `:node_modules/@dominion/game-saolei-board` to the agent BUILD deps and run `bazel run //:gazelle projects/game/agent`; `bazel build //projects/game/agent` green
- [ ] T012 [US3] Rewrite the saolei MCP tools in `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` (extract a validation helper if helpful) per `contracts/saolei-mcp-contract.md`: hold a per-session `SaoleiBoard` (the MCP server is created per-session by `projects/game/agent/src/mcp-host.ts`); `saolei_init` dispatches F2, decodes the `FlowResultPart` screenshot (base64 → `Buffer`), and calls `SaoleiBoard.init`; legal cell ops dispatch then `updateFromScreenshot`; implement **strict** pre-dispatch validation (the rule table in `contracts/saolei-mcp-contract.md` §4) rejecting illegal moves with reason codes before dispatch; return `renderBoardText(state)` (no image block) for every tool, with rejected moves also returning the current board + valid range; handle recognition failure (`BoardStateIncompatibleError`/`BoardDimensionMismatchError` → "unable to recognize", state invalid, subsequent ops rejected). Mind the coordinate-space split (recognition = screenshot space originY 200; clicks = client space via `projects/game/agent/src/mcp/saolei/geometry.ts` originY 104). Add/extend unit tests in `projects/game/agent/src/mcp/saolei/saolei-mcp.test.ts` covering: init returns text (no image); legal click dispatches + updated text; each illegal rule rejected pre-dispatch (`cell_already_revealed`, `cannot_flag_revealed`, `chord_requires_number`, `out_of_bounds`, `no_active_game`, `game_over`); chord-on-number-with-wrong-flag-count is **not** rejected; recognition failure → "unable to recognize"; `bazel test` green
- [ ] T013 [P] [US3] Update the built-in saolei skill in `projects/game/agent/src/skill/saolei/SKILL.md` per FR-021: describe the four tools; state that results return a **text** board with the symbol legend (`*` `0`-`8` `F` `X` `M` `?`); state that illegal moves are **rejected with a reason** (list the rule categories); remove any guidance saying the model should read a returned screenshot

**Order**: T011 → T012 (dependency then implementation+tests). T013 is a markdown-only change (different file), parallelizable with T011/T012.

**Checkpoint**: Saolei results are text boards (never a model-facing image); every illegal move is rejected before dispatch; every legal move dispatches and returns the updated board; the saolei skill correctly describes the text-board return format and rejection behavior.

---

## Phase 5: Polish & Cross-Cutting Concerns

**Purpose**: Large-test acceptance (Constitution principle VI).

### 文档清单（编码前必读）

- **代码规范文档**: `style/large_test.md`（测试按模块组织、加入既有 testplan、禁止按 spec 编号建文件/计划）; `style/golang.md` + [Google Go Style Guide](https://google.github.io/styleguide/go/guide)（Go 测试代码）
- **官方文档**: 无
- **技术文章**: 无

### Tasks

- [ ] T014 Large-test acceptance (Constitution principle VI): extend the **existing** `projects/game/testplan` — add cases organized **by module** (not by spec number, per `style/large_test.md`): (a) `projects/game/testplan/agent_operation_test.go` — agent receives a `FlowResultPart` and emits the correct display `tool_result` end-to-end; (b) `projects/game/testplan/agent_saolei_test.go` — saolei returns text boards and rejects illegal moves; (c) `projects/game/testplan/agent_multimodal_test.go` — a large-image round-trip completes without frame-size failure; add the corresponding `suite`/`case` to `projects/game/testplan/system_test.yaml` (do **not** create a new plan YAML); reuse shared helpers in `projects/game/testplan/helpers_test.go`. Execute via the testplan skill (`guitar run projects/game/testplan/system_test.yaml`, full deploy→test→cleanup); **all cases must pass**. Desktop-client concerns (WSClient binary frames, window-select) are verified by build + unit + manual per `style/large_test.md`

**Checkpoint**: Large test green (all cases pass via actual testplan execution, not build-only).

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Foundation)**: no dependencies; **blocks US1** (same `executeAgentOperation`, sequenced to avoid double-editing) and **US3** (consumes `FlowResultPart.screenshot`).
- **Phase 3 (US2)**: independent of Phase 1 (different files: WS transport only); may run in parallel with Phase 1/2 if capacity allows.
- **Phase 2 (US1)**: after Phase 1.
- **Phase 4 (US3 + FR-021)**: after Phase 1 (needs `FlowResultPart`); T013 (skill update) may run in parallel with T011/T012.
- **Phase 5 (Polish)**: after US1+US2+US3 (T014 large test covers all stories).

### User Story Dependencies

- **US1 (window-select)**: after Phase 1; no dependency on US2/US3.
- **US2 (image transport)**: independent of Phase 1/US1/US3 (WS transport only).
- **US3 (saolei)**: after Phase 1 (control-channel screenshot); no dependency on US1/US2.

### Parallel Opportunities

- Phase 1: T003 (desktop) ∥ T004 (agent) — different repos; integrate together.
- After Phase 1: US1 ∥ US2 ∥ US3 — different files (`app.go`/`app_operation.go`/`App.svelte` vs `websocket.go`/`gateway` vs `agent/.../saolei-mcp.ts`).
- Phase 3: T008 (desktop WS) ∥ T009 (gateway) — deploy together.
- Phase 4: T013 (skill markdown) ∥ T011/T012 — different files.

---

## Implementation Strategy

### MVP First (Phase 1 + US1)

1. Complete Phase 1 (Foundation) — proto + `FlowResultPart` channel.
2. Complete Phase 2 (US1) — selected-window single source of truth.
3. **STOP and VALIDATE**: the primary reported defect ("选中窗口后操作，截图提示失败") is fixed; operations run against the selected window with no Capture press.
4. Deploy/demo if ready.

### Incremental Delivery

1. Phase 1 → Foundation ready (`FlowResultPart` on the control channel).
2. + US1 → window-select fix (MVP).
3. + US2 → robust image transfer (large screenshots no longer break the turn).
4. + US3 → saolei text board + strict validation + skill update (deterministic recognition, illegal moves rejected, skill describes text format).
5. Phase 5 → large-test acceptance (green).

---

## Notes

- Each code task includes its unit tests and MUST pass `bazel build` + `bazel test` for the relevant target (Constitution principle IV — not separately allocated).
- The large test (T014) is the only separate test task (Constitution principle VI) and MUST be executed via the testplan skill (deploy→test→cleanup), all cases passing — build-only does not constitute acceptance.
- `FlowResultPart` (Phase 1) is the user's plan-phase directive (FR-023..FR-026); it is foundational, not a user story.
- Commit after each task or logical group; stop at any checkpoint to validate a story independently.
