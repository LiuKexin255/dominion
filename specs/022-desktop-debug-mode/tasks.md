# Tasks: Desktop Conversation Debug Mode

**Input**: Design documents from `/specs/022-desktop-debug-mode/`

**Prerequisites**: [plan.md](./plan.md) (required), [spec.md](./spec.md) (required, user stories), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/debug-control-plane.md](./contracts/debug-control-plane.md), [quickstart.md](./quickstart.md)

**Tests**: Per Constitution §IV, compile + unit tests run on every code change as part of development (not separate tasks). Go unit tests are written alongside the Go logic they cover (the desktop has Go tests; the frontend has no JS test runner — only `svelte-check` typecheck + a `vite build` consumed by Bazel (`bazel build //projects/game/desktop/frontend:dist`; never run `pnpm build`/`vite build` directly — see `projects/game/desktop/README.md`)). Large-test acceptance (agent) is a dedicated task in the Polish phase per Constitution §VI.

**Organization**: Tasks grouped by user story (spec US1 = P1, US2 = P2). US2 depends on US1 (debug mode must exist before results can be held "when debug mode is ON" — spec US2 "depends on debug mode existing").

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: Which user story this task belongs to (US1, US2)
- Exact file paths in descriptions; interface details cite `contracts/debug-control-plane.md` sections

## Path Conventions

This feature extends an existing single Wails desktop project — no new top-level directories. Paths are repository-relative:
- Go desktop backend: `projects/game/desktop/`
- Desktop frontend (Svelte 5 + TS): `projects/game/desktop/frontend/src/`
- Agent service (TS): `projects/game/agent/src/`

## 必读设计文档（ Constitution §V — 无需逐 phase 重复列出）

The feature design docs are required reading for every code phase (Constitution §V exempts spec-related files from per-phase re-listing). Implement against:
- **Interface contract**: [contracts/debug-control-plane.md](./contracts/debug-control-plane.md) — bound methods (`SetDebugMode` §1.1, `ConfirmToolResult` §1.2), events (`game:debug:result-held`/`result-released` §2), rendering rule §3.
- **Decisions**: [research.md](./research.md) (D1–D9).
- **Entities / state transitions**: [data-model.md](./data-model.md).
- **Validation**: [quickstart.md](./quickstart.md).

---

## Phase 1: Setup (Baseline)

**Purpose**: Confirm a green starting point before any change.

**文档清单**:
- 代码规范文档：无
- 官方文档：无
- 技术文章：无

- [X] T001 Verify baseline build + typecheck + tests are green: `bazel build //projects/game/desktop/... //projects/game/agent/...`, `bazel test //projects/game/desktop/... //projects/game/agent/...`, and frontend typecheck via `bazel run @pnpm -- --dir "$PWD/projects/game/desktop/frontend" run typecheck` + frontend dist build via `bazel build //projects/game/desktop/frontend:dist` (per `projects/game/desktop/README.md`: frontend `dist/` is built by the Bazel `vite_build` rule — never run `pnpm build`/`vite build` directly). Fix nothing here — only confirm green; if red, stop and surface the pre-existing failure.

**Checkpoint**: Green baseline established.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Realize the debug control-plane binding surface (interface-first, Constitution §III) that both user stories call into.

**⚠️ CRITICAL**: US1 and US2 frontend tasks both depend on this surface existing.

**文档清单**:
- 代码规范文档：`style/javascript.md`（含其引用的 [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)）
- 官方文档：[Wails v2 — Application Development (Bindings)](https://wails.io/docs/guides/application-development)（`Bind` 暴露 `*App` 公有方法为 `window.go.main.App.<method>`）
- 技术文章：无

- [X] T002 Declare the debug control-plane binding surface in `projects/game/desktop/frontend/src/api.ts`: add `SetDebugMode(enabled: boolean): Promise<void>` and `ConfirmToolResult(toolID: string): Promise<void>` to the `WailsApp` interface, and add `export async function setDebugMode(enabled: boolean)` / `export async function confirmToolResult(toolID: string)` wrappers following the existing `app()` / `window.go.main.App` pattern (per `contracts/debug-control-plane.md` §1). TS-only; the Go methods are implemented in their story phases, so runtime calls are not made until then.

**Checkpoint**: Binding surface declared; frontend typechecks.

---

## Phase 3: User Story 1 — Debug toggle surfaces verbose logs (Priority: P1) 🎯 MVP

**Goal**: A Debug switch in the conversation toolbar that, when ON, surfaces DEBUG-level entries from both the frontend logger and the Go backend logger in the existing log panel; OFF suppresses them and is the default (spec US1, FR-001–FR-005).

**Independent Test** (spec US1): open a session, toggle Debug on, observe DEBUG entries appear that were absent before; toggle off, no new DEBUG entries; leave/re-enter → resets to OFF. See [quickstart.md](./quickstart.md) Scenario 1.

**文档清单**:
- 代码规范文档：`style/golang.md`（含其 `## 引用` 列出的 [Google Go Style Guide](https://google.github.io/styleguide/go/) 三份文档：[Guide](https://google.github.io/styleguide/go/guide)、[Decisions](https://google.github.io/styleguide/go/decisions)、[Best Practices](https://google.github.io/styleguide/go/best-practices)）；`style/javascript.md`（含 [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)）
- 官方文档：[Wails v2 — How does it work](https://wails.io/docs/howdoesitwork)（方法绑定 + 运行时事件）；[Svelte 5 — `$state`](https://svelte.dev/docs/svelte/$state)（响应式开关）
- 技术文章：无

- [X] T003 [P] [US1] Add a gated DEBUG level to the Go backend logger in `projects/game/desktop/internal/applog/logger.go`: add a `debugEnabled atomic.Bool` field, a `SetDebug(bool)` setter, and a `Debug(source, msg string, fields ...map[string]any)` method that is a no-op (no append, no event-sink push) when `debugEnabled` is false (zero overhead off). DEBUG entries flow the existing `Entry.Level`/event-sink path unchanged (per `research.md` D5). Include a Go unit test (per `style/golang.md` 单元测试): off → `Debug` produces no entry; on → one entry with `level:"debug"`.
- [X] T004 [P] [US1] Add a gated DEBUG level to the frontend logger in `projects/game/desktop/frontend/src/logger.ts`: add a module-level `debugEnabled` flag (default false), `setDebugEnabled(bool)`, and a `logDebug(source, message, fields?)` helper that short-circuits (no sink push, no console) when off (per `research.md` D5).
- [X] T005 [US1] Implement the `*App.SetDebugMode(enabled bool) error` bound method in `projects/game/desktop/app.go`: store an atomic debug flag on `*App`, call `a.logger.SetDebug(enabled)`, and log the transition via `a.logger.Info`. (Depends on T003; per `contracts/debug-control-plane.md` §1.1 — release-all-holds behavior is added later in T010.)
- [X] T006 [US1] Add the Debug switch + frontend DEBUG emission in `projects/game/desktop/frontend/src/App.svelte`: add `debugMode = $state(false)` and a labeled switch in the chat-top-bar (beside the session label / window selector, ~lines 787–803) whose toggle calls `setDebugMode(...)` and `setDebugEnabled(...)`; reset `debugMode` to false on page/session exit (extend `resetPlayPageState()` or the session-exit path, FR-002); add `logDebug(...)` calls at inbound chat-frame handling (FR-004 frontend). (Depends on T002, T004.)
- [X] T007 [US1] Add Go DEBUG emission at key tool-execution / result points in `projects/game/desktop/app.go` (`recvLoop`, `executeAgentOperation`) via `a.logger.Debug(...)` (FR-004 Go backend). (Depends on T005; same file as T005, so sequence after it.)
- [X] T008 [P] [US1] Add a `log-debug` visual style for DEBUG entries in `projects/game/desktop/frontend/src/components/LogPanel.svelte` (the panel already styles by `log-{level}` at line 35; add a distinct color for `debug`).

**Checkpoint**: US1 fully functional — Debug toggle surfaces DEBUG logs from both layers; OFF is default and suppresses them; resets on exit. Validate via [quickstart.md](./quickstart.md) Scenario 1.

---

## Phase 4: User Story 2 — Tool result held for confirmation before returning to the agent (Priority: P2)

**Goal**: When debug mode is ON, the desktop holds each computed tool result (after execution, before sending to the agent) until the user clicks "Confirm" on the result bubble (or the 15-minute auto-continue fires); the agent only waits; the pause is transparent (spec US2, FR-006–FR-014).

**Independent Test** (spec US2): with Debug ON, trigger a tool op — the result bubble shows "Confirm" and the agent does not advance; click Confirm → button disappears, agent resumes; same turn run with Debug OFF is identical. See [quickstart.md](./quickstart.md) Scenarios 2–4.

**文档清单**:
- 代码规范文档：`style/golang.md`（含 [Google Go Style Guide](https://google.github.io/styleguide/go/) Guide/Decisions/Best Practices）；`style/javascript.md`（含 [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)）
- 官方文档：[Wails v2 — Application Development (Events)](https://wails.io/docs/guides/application-development)（`runtime.EventsEmit` Go→frontend）；[Svelte 5 — `$props`](https://svelte.dev/docs/svelte/$props) 与 [Svelte 5 — v5 migration (event handlers / `onclick`)](https://svelte.dev/docs/svelte/v5-migration-guide)
- 技术文章：无

- [X] T009 [P] [US2] Raise the agent dispatch-result timeout in `projects/game/agent/src/operation-bridge.ts`: change `DISPATCH_TIMEOUT_MS` (line 35) from `5_000` to `1_200_000` (20 min) as the safety-net backstop (FR-014; 20 min > desktop's 15-min auto-continue). One-constant change; no logic change.
- [X] T010 [US2] Add the Go holds infrastructure in `projects/game/desktop/app.go`: add `holds map[string]*hold` + `holdsMu sync.Mutex` on `*App` (the `hold` carries a `confirmCh chan struct{}` keyed by `tool_id`, per `data-model.md`); implement `*App.ConfirmToolResult(toolID string) error` (close the hold's `confirmCh`, delete the entry; logged no-op if absent — `contracts` §1.2); extend `SetDebugMode(false)` (from T005) to release every held result (reason `"debug-off"`). Include Go unit tests (per `style/golang.md`): confirm releases a hold; confirm on unknown toolID is a no-op; `SetDebugMode(false)` drains all holds. (Depends on T005.)
- [X] T011 [US2] Implement the hold logic in `*App.handleInboundOperation` in `projects/game/desktop/app.go` (lines 611–651): when debug mode is ON, reorder to compute → `chatStreams.Append` (show result) → register hold + emit `game:debug:result-held {toolId}` → `select` on `<-confirmCh` / `<-time.After(15*time.Minute)` (FR-013 auto-continue) / `<-a.ctx.Done()` (shutdown) → emit `game:debug:result-released {toolId, reason}` + delete hold → `ws.SendFrame` (per `data-model.md` state transitions, `research.md` D4). When OFF, keep today's compute→send→append order unchanged (FR-011). Make the 15-min duration a package-level constant so the auto-continue branch is unit-testable (test signals `confirmCh`/cancels `ctx`; optionally override the duration). Include Go unit tests for the held/confirmed/timeout/shutdown branches. (Depends on T010; same file, sequence after it.)
- [X] T012 [US2] Wire held-state into the frontend in `projects/game/desktop/frontend/src/App.svelte`: add `heldToolIds = $state<Set<string>>(new Set())`; register `game:debug:result-held` / `game:debug:result-released` listeners (in `onMount`, same pattern as the existing `game:log` listener) that add/remove toolIDs by reassigning a new `Set`; pass `heldToolIds` and an `onConfirm(toolID)` callback (→ `confirmToolResult(toolID)`) down to `ChatView`. (Depends on T002.)
- [X] T013 [US2] Render the "Confirm" control in `projects/game/desktop/frontend/src/components/ChatView.svelte` (tool-result branch, lines 197–221): accept `heldToolIds` + `onConfirm` props; render a "Confirm" button iff `part.toolResult?.toolId != null && heldToolIds.has(part.toolResult.toolId)`; its `onclick` calls `onConfirm(part.toolResult.toolId)`. Display-only / history results never appear in `heldToolIds`, so they never show the control (FR-012). (Depends on T012.)

**Checkpoint**: US1 AND US2 both functional — tool results are held with a Confirm control; the agent waits; confirm/auto-continue returns the result transparently. Validate via [quickstart.md](./quickstart.md) Scenarios 2–4.

---

## Phase 5: Polish & Cross-Cutting Concerns

**Purpose**: Build hygiene, full validation, and large-test acceptance.

**文档清单**:
- 代码规范文档：`style/large_test.md`（大型测试编排，T016 执行时使用 `testplan` SKILL）
- 官方文档：无
- 技术文章：无

- [ ] T014 [P] Regenerate build files + format: run `bazel run //:gazelle projects/game/desktop projects/game/agent` (update `BUILD.bazel`), `bazel run //:go -- fmt` on changed Go files, then frontend typecheck via `bazel run @pnpm -- --dir "$PWD/projects/game/desktop/frontend" run typecheck` + frontend dist build via `bazel build //projects/game/desktop/frontend:dist` (per `projects/game/desktop/README.md`; do NOT run `pnpm build`/`vite build` directly). Confirm `bazel build //...` and `bazel test //projects/game/desktop/... //projects/game/agent/...` are green.
- [ ] T015 Run the [quickstart.md](./quickstart.md) validation scenarios end-to-end (manual): Scenario 1 (logs), Scenario 2 (hold/confirm), Scenario 3 (15-min auto-continue — use a shortened internal duration or the Go unit test to avoid waiting), Scenario 4 (transparency), Scenario 5 (scope boundary / no regressions).
- [ ] T016 [P] Run the existing agent large tests via the `testplan` skill — `guitar run projects/game/testplan/system_test.yaml` — to confirm the `DISPATCH_TIMEOUT_MS` change (T009) keeps agent dispatch green (Constitution §VI; per `style/large_test.md`, no new large-test plan is created — the desktop is a client, out of large-test scope; the agent is covered by its existing plan). Update any agent test asserting the prior 5 s timeout if it now fails.

**Checkpoint**: Feature complete and validated; agent large tests green.

---

## Dependencies & Execution Order

### Phase Dependencies
- **Phase 1 (Setup)**: no dependencies — start immediately.
- **Phase 2 (Foundational)**: after Phase 1 — **BLOCKS** all user-story frontend tasks (T002 surface).
- **Phase 3 (US1)**: after Phase 2.
- **Phase 4 (US2)**: after **US1** (US2 reads the debug flag and extends `SetDebugMode` established in US1 — spec: "US2 depends on debug mode existing").
- **Phase 5 (Polish)**: after all desired stories.

### Task-level Dependencies
- **T005** (SetDebugMode) → depends on **T003** (applog Debug).
- **T006** (App.svelte switch + frontend logs) → depends on **T002**, **T004**.
- **T007** (Go DEBUG emission) → depends on **T005** (same file `app.go`).
- **T010** (holds infra + ConfirmToolResult + release-all) → depends on **T005** (same file `app.go`).
- **T011** (hold in `handleInboundOperation`) → depends on **T010** (same file `app.go`).
- **T012** (App.svelte held-state) → depends on **T002**.
- **T013** (ChatView Confirm) → depends on **T012**.
- **T016** (agent large tests) → depends on **T009**.

### Parallel Opportunities
- **Phase 2**: T002 is a lone task (no parallelism within the phase).
- **US1**: T003 (`logger.go`), T004 (`logger.ts`), T008 (`LogPanel.svelte`) are different files with no inter-dependency — **all [P], run together**.
- **US2**: T009 (`operation-bridge.ts`) is **[P]** and independent — run alongside T010/T011/T012/T013.
- **Polish**: T014 and T016 are **[P]** (different concerns/files); T015 is manual.

### Within Each User Story
- Infrastructure/capability (logger levels) before wiring.
- Go bound methods before the frontend calls that exercise them.
- Same-file edits are sequenced (no [P]); different-file edits are [P].

---

## Parallel Example: User Story 1

```bash
# Run these independent (different-file) US1 tasks together:
Task: "T003 applog DEBUG level — projects/game/desktop/internal/applog/logger.go"
Task: "T004 frontend logger DEBUG level — projects/game/desktop/frontend/src/logger.ts"
Task: "T008 LogPanel log-debug style — projects/game/desktop/frontend/src/components/LogPanel.svelte"
# Then the dependent chain (same file app.go): T005 → T007 ; and App.svelte: T006 (after T002+T004).
```

## Parallel Example: User Story 2

```bash
# Independent (different-file) US2 tasks together:
Task: "T009 agent timeout — projects/game/agent/src/operation-bridge.ts"
# (T010/T011 are sequenced on app.go; T012/T013 sequenced on App.svelte/ChatView.svelte)
```

---

## Implementation Strategy

### MVP First (User Story 1 only)
1. Phase 1 (baseline) → Phase 2 (binding surface) → Phase 3 (US1).
2. **STOP and VALIDATE** US1 independently via [quickstart.md](./quickstart.md) Scenario 1 (toggle surfaces DEBUG logs; off is default; resets on exit).
3. US1 alone is a usable diagnostic increment (verbose logging) and can ship/demo.

### Incremental Delivery
1. Setup + Foundational → binding surface ready.
2. US1 → debug logging works end-to-end (MVP).
3. US2 → tool-result hold/confirm works; agent backstop in place.
4. Polish → build green, quickstart validated, agent large tests green.

---

## Notes
- [P] tasks = different files, no dependency on an incomplete task.
- Same-file tasks are sequenced (e.g., the `app.go` chain T005→T007 in US1 and T010→T011 in US2).
- Per Constitution §IV, every Go code change includes its unit test as part of the task (not a separate task); the frontend has no JS test runner (typecheck + build only).
- Per Constitution §VI / `style/large_test.md`, the desktop client is out of large-test scope; only the agent (gRPC service) is covered, by its existing large-test plan (T016).
- Commit after each task or logical group; stop at any checkpoint to validate a story independently.
