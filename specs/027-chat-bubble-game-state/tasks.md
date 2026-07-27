# Tasks: Chat Bubble UX Polish & Saolei Game-State Awareness

**Input**: Design documents from `/specs/027-chat-bubble-game-state/` — [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/saolei-mcp-status-contract.md](./contracts/saolei-mcp-status-contract.md), [contracts/desktop-bubble-render-contract.md](./contracts/desktop-bubble-render-contract.md), [quickstart.md](./quickstart.md).

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/, quickstart.md.

**Tests**: Test tasks ARE included — the spec mandates library unit + agent unit (Constitution IV) and the agent large-test suite is the acceptance gate (Constitution VI). Compile (`bazel build`) + unit (`bazel test`) are PART OF each code task (Constitution IV — not separate tasks); the large test is a dedicated acceptance task because it must actually execute via the testplan skill (Constitution VI).

**Organization**: Tasks are grouped by user story. Phase order respects the dependency US3 → US4 → US5 (the agent file `saolei-mcp.ts` is shared, and US4's `game status:` line is carried by US5's rejection — so they are sequenced, not interleaved). US1/US2 (desktop, different files) are independent and parallelizable with everything else.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (e.g. US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

Multi-project under `projects/game/`:
- `pkg/saolei-board/` — TypeScript recognition library (US3)
- `agent/` — TypeScript agent service (US4/US5)
- `desktop/frontend/` — Svelte 5 desktop frontend (US1/US2)
- `testplan/` — Go large-test suites (acceptance gate)

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Confirm prerequisites are in place. No project initialization is needed — this feature edits existing projects, introduces no new project, no proto change, no new external dependency (plan.md Structure Decision). The testdata fixtures were placed during `/speckit.plan` and are committed assets.

- [ ] T001 Verify the testdata fixtures are present and correct: (a) **win fixtures**: `projects/game/pkg/saolei-board/testdata/saolei_10.png`, `projects/game/pkg/saolei-board/testdata/saolei_10.golden.txt`, and the large-test copy `projects/game/testplan/testdata/saolei_10.png` (cmp-identical to the library copy); confirm `saolei_10.golden.txt` matches `bazel run //projects/game/pkg/saolei-board:cli -- <abs path>/saolei_10.png` (2>/dev/null); (b) **loss fixture**: `projects/game/testplan/testdata/saolei_5.png` exists and is cmp-identical to the library copy at `projects/game/pkg/saolei-board/testdata/saolei_5.png` (used by the T015/T016 loss flow); (c) **in-progress fixtures**: `projects/game/testplan/testdata/saolei_1.png` and `saolei_2.png` exist as cmp-identical copies from the library testdata (existing, used by T016 playing flow). All testdata files follow the established pattern of byte-identical copies in both the library and testplan testdata directories (research.md D12). No code change — a read-only confirmation that the planning-phase fixtures are in place.

**Note on foundational work**: there is no blocking foundational phase for this feature — the changes are additive to existing surfaces (no shared module, no schema, no infra). User-story phases may begin immediately after Setup.

### 阅读清单（必读，编码前完整阅读）

- **代码规范文档**: 无（本 Phase 为只读验证，无编码任务）。
- **官方文档**: 无。
- **技术文章**: 无。
- **本特性设计文档**: `specs/027-chat-bubble-game-state/plan.md`（确认测试夹具位置与项目结构）；`specs/027-chat-bubble-game-state/research.md` D12（测试分层与夹具列表）。

---

## Phase 2: User Story 3 — saolei-board win predicate (Priority: P1) 🎯 enabler

**Goal**: Export a pure `isWin(state)` predicate from `@dominion/game-saolei-board` that classifies a recognized board as a win (FR-009..011). This is the enabling capability for US4 (the agent imports it to derive game status and to gate post-win operations).

**Independent Test**: `bazel test //projects/game/pkg/saolei-board:lib_test` — `win.test.ts` passes (synthetic grids: all-revealed/all-flagged → true; any INITIAL/HIT_MINE/MINE/UNKNOWN → false); the golden suite recognises `saolei_10.png` and asserts `isWin` returns `true` on it (quickstart.md Scenario 3).

### 阅读清单（必读，编码前完整阅读）

- **代码规范文档**: `style/javascript.md`；其引用的 [Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)。
- **官方文档**: 无（纯谓词，无第三方依赖）。
- **技术文章**: 无。
- **本特性设计文档**: `specs/027-chat-bubble-game-state/data-model.md` §1；`specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md` §1；`specs/027-chat-bubble-game-state/research.md` D6；`projects/game/pkg/saolei-board/README.md`（核心库用法与 golden 校准流程）。

### Implementation for User Story 3

- [ ] T002 [P] [US3] Create the `isWin(state)` pure predicate in `projects/game/pkg/saolei-board/src/core/win.ts` per data-model.md §1: a single short-circuiting pass over `state.grid` returning `true` iff no cell is `INITIAL`/`HIT_MINE`/`MINE`/`UNKNOWN` (every cell is `"0"`..`"8"` or `FLAG`). Pure function, no I/O, no mutation. Include the module docstring citing spec FR-009..011 + data-model.md §1.
- [ ] T003 [US3] Export `isWin` from the public barrel in `projects/game/pkg/saolei-board/src/core/index.ts` (`export { isWin } from "./win";`), matching the existing export convention (e.g. `renderBoardText`, `checkCompatible`). (depends T002)
- [ ] T004 [P] [US3] Add unit tests for `isWin` in `projects/game/pkg/saolei-board/src/core/win.test.ts`: an all-revealed-numbers board → true; an all-`FLAG` board → true; a mixed numbers+flags board → true; a board with one `INITIAL` → false; a board with `HIT_MINE` → false; a board with `MINE` → false; a board with `UNKNOWN` (that would otherwise be a win) → false. Import `isWin` from `"./win"` directly (style/javascript.md §测试 — pure unit, no DI, no `vi.mock`). (depends T002)
- [ ] T005 [P] [US3] Add `"saolei_10"` to the `CASES` array in `projects/game/pkg/saolei-board/src/core/golden.test.ts` (the `lib_test` data `glob(["testdata/*.png"])` already picks up the file — no BUILD edit), AND add a golden-coupled assertion that `isWin(recognizeBoard(readFileSync(testdata/saolei_10.png)).state) === true` — proving `isWin` returns true on the real win screenshot. Import `isWin` from the package barrel (`@dominion/game-saolei-board`) or from `"./win"`. (depends T003)

**Checkpoint**: `isWin` is exported and proven on synthetic + real win boards. US4 can import it. Run `bazel test //projects/game/pkg/saolei-board:lib_test` — all green.

---

## Phase 3: User Story 4 — saolei MCP game-status output + post-win terminal (Priority: P1)

**Goal**: Every saolei tool result carries a `game status: won|lost|playing` line (FR-012..015); a recognized win is a terminal state, so any cell operation after a win is rejected with a new `game_won` reason (FR-021..023). Depends on US3 (`isWin`).

**Independent Test**: `bazel test //projects/game/agent/src/mcp/saolei:saolei-mcp_test` — every result on an in-progress board contains `game status: playing`; a winning `GameState` → `game status: won` + a following cell op rejected `game_won`; a losing `GameState` → `game status: lost` + `game_over` (existing) carrying `game status: lost`; `no_active_game`/`unable to recognize` omit the status line (quickstart.md Scenario 4).

### 阅读清单（必读，编码前完整阅读）

- **代码规范文档**: `style/javascript.md`；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)。测试任务还需其引用的 [vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)（DI fake 模式，不使用 `vi.mock` 跨包 mock）。
- **官方文档**: [MCP Tools content blocks](https://modelcontextprotocol.io/docs/concepts/tools)（确认工具返回单个 text content block 的契约 — 025 FR-012 保留）。
- **技术文章**: 无。
- **本特性设计文档**: `specs/027-chat-bubble-game-state/data-model.md` §2/§3/§4/§5；`specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md` §2/§3/§4/§5/§6；`specs/027-chat-bubble-game-state/research.md` D7/D8。

### Implementation for User Story 4

- [ ] T006 [US4] In `projects/game/agent/src/mcp/saolei/saolei-mcp.ts`: (a) import `isWin` from `@dominion/game-saolei-board`; (b) add the `gameStatus(state): "won"|"lost"|"playing"` helper (data-model.md §3 — loss-first via the existing `isTerminalState`, then `isWin`, else `playing`); (c) extend the `MoveRejection` union with `"game_won"` (data-model.md §2); (d) in `validateMove`, add the post-win terminal check `if (isWin(state)) return { ok: false, reason: "game_won" }` immediately AFTER the existing `game_over` (loss) check (data-model.md §4 rule order; FR-021..023). (depends on US3 complete — T002/T003)
- [ ] T007 [US4] In `projects/game/agent/src/mcp/saolei/saolei-mcp.ts`, add the `game status: <status>` line to all four text-result builders — `initSuccessText`, `dispatchedText`, `rejectionText` (the has-state branch), per data-model.md §5 / contract §2: positioned immediately AFTER the outcome/rejection line and BEFORE the text board. The no-state branches (`no_active_game`, `unrecognizableText`) MUST omit the line (FR-015). The single-text-block contract (025 FR-012) is preserved — the line is part of the same text body. (depends T006)
- [ ] T008 [US4] Extend `projects/game/agent/src/mcp/saolei/saolei-mcp.test.ts`: (a) every successful/dispatched result on an in-progress canned board contains `game status: playing`; (b) a canned WINNING `GameState` (use the `board(["…"])` helper) → the dispatching result contains `game status: won`, and a following cell op returns `rejected: game_won` with NO dispatched FlowPart; (c) a canned LOSING board (`X`/`M` cell) → result contains `game status: lost`, following op `rejected: game_over` carrying `game status: lost`; (d) `no_active_game` and `unable to recognize` outcomes have NO `game status:` line. Follow the existing DI pattern (fake `OperationBridge` + fake `SaoleiBoardApi` — no `vi.mock`). (depends T006, T007)

**Checkpoint**: Game status is surfaced in every result; a win is terminal. Run `bazel test //projects/game/agent/src/mcp/saolei:saolei-mcp_test` — all green.

---

## Phase 4: User Story 5 — saolei_chord_click neighbor validation (Priority: P2)

**Goal**: Reject a chord whose non-flag neighbors contain no `INITIAL` (and no `UNKNOWN`) cell — a guaranteed no-op — with a new `chord_no_unrevealed_neighbor` reason (FR-016..020). Same file as US4 (sequenced after it to avoid churn); US5's rejection carries US4's `game status:` line via the shared `rejectionText` builder.

**Independent Test**: `bazel test //projects/game/agent/src/mcp/saolei:saolei-mcp_test` — a chord target whose non-flag neighbors are all revealed/flags → `chord_no_unrevealed_neighbor`; with an `INITIAL` or `UNKNOWN` neighbor → `{ ok: true }`; the rejection body carries `game status:` (quickstart.md Scenario 4).

### 阅读清单（必读，编码前完整阅读）

- **代码规范文档**: `style/javascript.md`；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)；测试任务还需 [vitest Mocking Modules — Pitfalls](https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls)。
- **官方文档**: 无（无新第三方组件）。
- **技术文章**: 无。
- **本特性设计文档**: `specs/027-chat-bubble-game-state/data-model.md` §2/§4（`neighbors`/`hasInitialOrUnknownNeighbor` helpers + rule order）；`specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md` §4/§5（含 §5.1 chord-neighbor 细节）；`specs/027-chat-bubble-game-state/research.md` D9。

### Implementation for User Story 5

- [ ] T009 [US5] In `projects/game/agent/src/mcp/saolei/saolei-mcp.ts`: (a) add the `neighbors(state, x, y)` helper (bounded Moore set — data-model.md §4) and `hasInitialOrUnknownNeighbor(state, x, y)` (true iff some in-bounds neighbor is `INITIAL` or `UNKNOWN` — FLAG/number/mine neighbors do not count); (b) extend the `MoveRejection` union with `"chord_no_unrevealed_neighbor"`; (c) in `validateMove`'s `saolei_chord_click` branch, AFTER the existing `chord_requires_number` check passes, add `if (!hasInitialOrUnknownNeighbor(state, x, y)) return { ok: false, reason: "chord_no_unrevealed_neighbor" }` (FR-016..020). The rejection flows through the existing `rejectionText` (now carrying the `game status:` line from T007). (depends T007 — same file)
- [ ] T010 [US5] Extend `projects/game/agent/src/mcp/saolei/saolei-mcp.test.ts` `validateMove` rule-table tests: a chord target (a revealed number) whose 8 neighbors are all revealed numbers → `chord_no_unrevealed_neighbor`; all `FLAG` neighbors → `chord_no_unrevealed_neighbor`; an edge/corner target with no in-bounds `INITIAL` neighbor → `chord_no_unrevealed_neighbor`; a target with one `INITIAL` neighbor → `{ ok: true }`; a target whose only non-revealed neighbor is `UNKNOWN` → `{ ok: true }` (lenient). Assert the existing `chord_requires_number` rule still fires first on a non-number target. (depends T009)
- [ ] T011 [US5] Update the built-in saolei skill `projects/game/agent/src/skill/saolei/SKILL.md` (FR-024): (a) document the `game status: won|lost|playing` line in the result-format section (and update the worked examples); (b) add a `game_won` row to the rejection-reason table ("The current game is already won. Call `saolei_init` to start a new game." — symmetric with the existing `game_over` row); (c) add a `chord_no_unrevealed_neighbor` row ("The chord target's neighbors are all revealed or flagged — there is no unrevealed cell to reveal; pick a different target or flag/unflag first."); (d) add a sentence to the validation narrative that a win is terminal exactly like a loss. (depends T007, T009)

**Checkpoint**: Meaningless chords are rejected pre-dispatch; the skill documents the new status line and both new reasons. Run `bazel test //projects/game/agent/src/mcp/saolei:saolei-mcp_test` — all green.

---

## Phase 5: User Story 1 — think bubble: hidden scrollbar + auto-scroll (Priority: P1)

**Goal**: The expanded think-bubble content area shows no visible scrollbar (scroll capability preserved), and follows the streaming reasoning — pausing when the operator scrolls up and resuming at the bottom (FR-001..004). Independent of US2/US3/US4/US5 (different file/project).

**Independent Test**: `bazel build //projects/game/desktop/...` (the frontend has no unit-test infra — 023/024 assumption); manual verification per quickstart.md Scenario 1 (no visible scrollbar on overflow; auto-scroll follows the stream; pause-on-scroll-up; open-to-bottom).

### 阅读清单（必读，编码前完整阅读）

- **代码规范文档**: `style/javascript.md`；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)。
- **官方文档**: [Svelte 5 `$effect`](https://svelte.dev/docs/svelte/$effect)（含 autoscroll 示例，`scrollTop + offsetHeight > scrollHeight − tolerance` 的 at-bottom 判定，与本研究 D2 设计一致）；[MDN `scrollbar-width`](https://developer.mozilla.org/en-US/docs/Web/CSS/scrollbar-width)（`none` 保留滚动能力，及 a11y 注意事项 — 须保留键盘/滚轮滚动）；[MDN `::-webkit-scrollbar`](https://developer.mozilla.org/en-US/docs/Web/CSS/::-webkit-scrollbar)（Wails WebView2/Chromium 生效的隐藏规则）。
- **技术文章**: 无。
- **本特性设计文档**: `specs/027-chat-bubble-game-state/data-model.md` §6（`contentEl` bind + 两个 `$effect` + TOLERANCE=8px）；`specs/027-chat-bubble-game-state/contracts/desktop-bubble-render-contract.md` §1/§2；`specs/027-chat-bubble-game-state/research.md` D1/D2。参考现有 chat-thread auto-scroll `$effect`（`projects/game/desktop/frontend/src/components/ChatView.svelte` L188-197）。

### Implementation for User Story 1

- [ ] T012 [P] [US1] In `projects/game/desktop/frontend/src/components/ChatMessage.svelte`: (a) bind the `.thinking-content` `<pre>` to a `$state` element ref (`let contentEl: HTMLPreElement | undefined = $state()`, `bind:this={contentEl}`); (b) add two `$effect`s per data-model.md §6 / contract §2 — one keyed on `expanded` (open→scroll to bottom on `requestAnimationFrame`), one keyed on `part.thinking.content` (if at-bottom per `scrollTop + clientHeight >= scrollHeight − 8`, scroll to bottom on `requestAnimationFrame`; else pause); (c) add the hidden-scrollbar CSS scoped to `.thinking-content` (`scrollbar-width: none` + `.thinking-content::-webkit-scrollbar { display: none }`) — keep `overflow-y: auto` and `max-height: 200px` unchanged (FR-001). Run `bazel build //projects/game/desktop/...`.

**Checkpoint**: Think bubble has no visible scrollbar and follows the stream. Manual verification per quickstart.md Scenario 1.

---

## Phase 6: User Story 2 — tool bubble: compact args + formatted result + collapsible body (Priority: P1)

**Goal**: Tool-call args render compact (single-line); the result message preserves its multi-line formatting; the result body is collapsed by default behind a toggle (status icon + label always visible) (FR-005..008). Independent of US1/US3/US4/US5 (different file).

**Independent Test**: `bazel build //projects/game/desktop/...`; manual verification per quickstart.md Scenario 2 (args one-line; result collapsed by default; expanded message shows the multi-line text board; screenshot sub-toggle independent).

### 阅读清单（必读，编码前完整阅读）

- **代码规范文档**: `style/javascript.md`；[Google TypeScript Style Guide](https://google.github.io/styleguide/tsguide.html)。
- **官方文档**: [MDN `<details>`/`<summary>`](https://developer.mozilla.org/en-US/docs/Web/HTML/Element/details)（默认折叠、原生键盘可访问、与现有截图子 toggle 一致）；[MDN `white-space`](https://developer.mozilla.org/en-US/docs/Web/CSS/white-space)（`pre-wrap` 保留换行并折行）。
- **技术文章**: 无。
- **本特性设计文档**: `specs/027-chat-bubble-game-state/data-model.md` §7（`compactArgs` + `.tool-args-inline` + `<details>` 结构 + `.op-result-message` pre-wrap）；`specs/027-chat-bubble-game-state/contracts/desktop-bubble-render-contract.md` §3/§4/§5；`specs/027-chat-bubble-game-state/research.md` D3/D4/D5。参考现有 `prettyArgs`（`ChatView.svelte` L179-186）与截图子 `<details>`（L276-285）。

### Implementation for User Story 2

- [ ] T013 [P] [US2] In `projects/game/desktop/frontend/src/components/ChatView.svelte`: replace the `prettyArgs(argsJson)` helper (which does `JSON.stringify(JSON.parse(argsJson), null, 2)`) with `compactArgs(argsJson)` (`JSON.stringify(JSON.parse(argsJson))` compact with a `try/catch` raw-string fallback — data-model.md §7), and render the args INLINE as `<code class="tool-args-inline">` next to `.tool-name` (remove the `.tool-args` `<pre>` block). Run `bazel build //projects/game/desktop/...`.
- [ ] T014 [US2] In `projects/game/desktop/frontend/src/components/ChatView.svelte`: (a) change the resolved result body to use a `<pre class="op-result-message">` with `white-space: pre-wrap; word-break: break-word;` (replacing the `<span class="op-result-message">` that collapsed newlines — FR-006); (b) wrap the resolved result body (status message + screenshot sub-toggle) in a `<details class="tool-result-details">` with NO `open` attribute (collapsed by default), `<summary>` holding the status icon + label, the `<pre>` message inside, and the existing screenshot `<details>` nested unchanged and independent (FR-007/008). The pending `running…` state stays outside the `<details>`. Run `bazel build //projects/game/desktop/...`. (depends T013 — same file)

**Checkpoint**: Tool bubble shows compact args, a formatted collapsible result. Manual verification per quickstart.md Scenario 2.

---

## Phase 7: Large-Test Acceptance Gate (Constitution §VI)

**Purpose**: Verify the `game status:` line and the `game_won`/`game_over` terminal rejections survive the full deployed-agent chain (MCP text result → `FlowResultPart` recognition → `ToolResultPart.message` → ListMessages) with the REAL recognition engine, across all three outcomes (win/loss/playing). This is the agent-service acceptance gate — build-only does NOT constitute acceptance (Constitution VI).

**Independent Test**: `guitar run projects/game/testplan/system_test.yaml` — all `agent-saolei` cases pass.

### 阅读清单（必读，编码前完整阅读）

- **代码规范文档**: `style/golang.md`；其引用的 [Google Go Style Guide](https://google.github.io/styleguide/go/guide)、[Style Decisions](https://google.github.io/styleguide/go/decisions)、[Best Practices](https://google.github.io/styleguide/go/best-practices)；`style/large_test.md`（大型测试特有约定 — given/when/then、helper 使用、MODULE 组织）。
- **官方文档**: 无（使用仓库内 `testtool` 框架）。
- **技术文章**: 无。
- **本特性设计文档**: `specs/027-chat-bubble-game-state/data-model.md` §1/§5（win fixture + 文本格式）；`specs/027-chat-bubble-game-state/contracts/saolei-mcp-status-contract.md` §2/§5；`specs/027-chat-bubble-game-state/research.md` D12（测试分层 — win/loss/playing 均可大型测试，chord-neighbor 仅单测）；`specs/027-chat-bubble-game-state/quickstart.md` Scenario 5。参考现有 `projects/game/testplan/agent_saolei_test.go`（`//go:embed` + "play desktop" 模式）与 `projects/game/testplan/BUILD.bazel`（`embedsrcs`）。

### Implementation for the Large-Test Gate

- [ ] T015 Add `"testdata/saolei_10.png"` AND `"testdata/saolei_5.png"` to the `embedsrcs` list in `projects/game/testplan/BUILD.bazel` (both files are already in `projects/game/testplan/testdata/` — `saolei_10.png` placed during planning, `saolei_5.png` copied from the library testdata as the loss-board fixture — both cmp-identical to their library originals, mirroring how `saolei_1`/`saolei_2` already exist in both dirs). Add two `//go:embed` vars in `projects/game/testplan/agent_saolei_test.go`: `saoleiBoardWinPNG` (`testdata/saolei_10.png` — 9×9 win) and `saoleiBoardLossPNG` (`testdata/saolei_5.png` — 16×16 loss, has `X`/`M`), mirroring the existing `saoleiBoardInitPNG`/`saoleiBoardRevealedPNG` declarations.
- [ ] T016 Add two large-test flows in `projects/game/testplan/agent_saolei_test.go` (style/large_test.md — MODULE-organised, given/when/then, reuse `helpers_test.go`): (a) **win flow** — `saolei_init` replied with `saoleiBoardWinPNG` (9×9 win) → assert the init result text contains `game status: won`; a following `saolei_click(x,y)` is rejected as `game_won` (assert NO operation FlowPart reaches the desktop; assert the rejection text contains `game status: won`); (b) **loss flow** — `saolei_init` replied with `saoleiBoardLossPNG` (16×16 loss) → assert the init result contains `game status: lost`; a following cell op rejected as `game_over` (existing terminal-loss) carrying `game status: lost`. Extend the existing `TestAgentSaoleiTextBoardFlow` to assert `game status: playing` on the in-progress results. Each flow is a single-game `saolei_init` (no cross-dimension update — research.md D12). (depends T015, US3, US4)
- [ ] T017 Execute the `agent-saolei` large-test suite via the testplan skill: `guitar run projects/game/testplan/system_test.yaml` (full deploy→test→cleanup loop). **All cases MUST pass** — any failed/flaky case means acceptance is NOT met; fix and re-run until fully green (Constitution VI). Build-only (`bazel build` of the test target) does NOT constitute acceptance. (depends T016)

**Checkpoint**: The agent service meets its large-test acceptance gate for the new status line + terminal rejections across all three outcomes.

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: Whole-feature validation and cleanup.

### 阅读清单（必读，编码前完整阅读）

- **代码规范文档**: 无（本 Phase 为验证与收尾，无编码任务）。
- **官方文档**: 无。
- **技术文章**: 无。
- **本特性设计文档**: `specs/027-chat-bubble-game-state/quickstart.md`（全部 5 个 Scenario 的验证矩阵）；`specs/027-chat-bubble-game-state/plan.md`（Structure Decision — 确认无待 gazelle 的新文件）。

- [ ] T018 Run the full quickstart.md validation matrix: Scenario 3 (`bazel test //projects/game/pkg/saolei-board:lib_test`), Scenario 4 (`bazel test //projects/game/agent/src/mcp/saolei:saolei-mcp_test`), Scenario 5 (covered by T017), and the manual Scenarios 1–2 (desktop build + visual check). Then run a whole-repo gate: `bazel build //projects/game/...` and `bazel test //projects/game/...` — all green. Confirm no `//gazelle` run is needed (no new source files outside existing globs — `win.ts`/`win.test.ts` are picked up by the `:core`/`:lib_test` globs; the testplan `embedsrcs` edit was manual per T015).

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — confirms fixtures. User-story work can begin immediately after (no foundational phase).
- **US3 (Phase 2)**: No deps. **Enables US4.**
- **US4 (Phase 3)**: Depends on US3 (`isWin`). Same file as US5 → sequenced before US5.
- **US5 (Phase 4)**: Depends on US4 (same file `saolei-mcp.ts`; its `rejectionText` carries US4's `game status:` line; the SKILL.md task T011 documents both US4 and US5 additions).
- **US1 (Phase 5)**: No deps on other stories — independent (different file/project).
- **US2 (Phase 6)**: No deps on other stories — independent (different file/project). T013 → T014 within the phase (same file).
- **Large-Test Gate (Phase 7)**: Depends on US3 + US4 (win predicate + status line + `game_won`). US5 is NOT a large-test dependency (the chord rule is unit-test-only — research.md D12).
- **Polish (Phase 8)**: Depends on all prior phases.

### User Story Dependencies

- **US3 (P1)**: Independent. MVP enabler.
- **US4 (P1)**: Depends on US3.
- **US5 (P2)**: Depends on US4 (same file).
- **US1 (P1)**: Independent — parallelizable with US2/US3/US4/US5.
- **US2 (P1)**: Independent — parallelizable with US1/US3/US4/US5.

### Within Each User Story

- Predicate/helpers before the validation rule that uses them (US3: win.ts before index export; US5: `neighbors`/`hasInitialOrUnknownNeighbor` before the chord rule).
- Code before its tests within a story where they share context, BUT test tasks here are written against the spec contract (T004/T005/T008/T010 may be written first and fail-red, then implementation makes them green — the spec mandates tests).
- Each code task includes its own `bazel build` + `bazel test` (Constitution IV) — not separately tasked.

### Parallel Opportunities

- **US1 (T012, ChatMessage.svelte) ‖ US2 (T013, ChatView.svelte)**: different files — full parallel.
- **US1/US2 ‖ US3/US4/US5**: different projects (desktop frontend vs library/agent) — full parallel.
- **Within US3**: T004 (win.test.ts) ‖ T005 (golden.test.ts) after T003 — different test files.
- **Within the Large-Test Gate**: T015 → T016 → T017 is strictly sequential (BUILD embed → test code → execution).

---

## Parallel Example: US1 ‖ US2 ‖ US3

```text
# Three developers, three independent surfaces (after Setup):
Task: "T012 [P] [US1] hidden-scrollbar CSS + auto-scroll $effect in ChatMessage.svelte"   # desktop A
Task: "T013 [P] [US2] compactArgs inline render in ChatView.svelte"                        # desktop B
Task: "T002 [P] [US3] isWin predicate in win.ts"                                           # library
# US4 (T006) starts once US3 (T002/T003) lands; US5 (T009) starts once US4 (T007) lands.
```

---

## Implementation Strategy

### MVP First (US3 + US4 — win detection + status surfacing)

1. Phase 1: confirm fixtures (T001).
2. Phase 2: US3 — `isWin` predicate + golden win case (T002–T005). **STOP + VALIDATE**: `bazel test //projects/game/pkg/saolei-board:lib_test` green; `isWin` proven on the real win screenshot.
3. Phase 3: US4 — game-status line + `game_won` terminal (T006–T008). **STOP + VALIDATE**: `bazel test //projects/game/agent/src/mcp/saolei:saolei-mcp_test` green; status line + post-win rejection confirmed (DI).
4. At this point win detection + status surfacing are functional (unit-level). Deploy/demo candidate after the large-test gate (Phase 7).

### Incremental Delivery

1. Setup → US3 (library) → US4 (agent status + terminal) — the win-awareness spine.
2. + US5 (chord-neighbor) — efficiency refinement.
3. + US1/US2 (desktop bubbles) — independent, any order, can interleave with the agent work.
4. + Large-Test Gate (Phase 7) — agent acceptance (Constitution VI; must `guitar run`, all green).
5. + Polish (Phase 8) — whole-repo gate + manual scenarios.

### Notes

- [P] tasks = different files, no dependencies on incomplete sibling tasks.
- [Story] label maps a task to its user story for traceability.
- Each code task MUST include `bazel build` + `bazel test` of its target as part of the task (Constitution IV).
- The large-test gate (T017) MUST actually execute via the testplan skill (`guitar run`) — build-only is NOT acceptance (Constitution VI).
- US1/US2 (frontend) are build + manual verified (no frontend unit-test infra — 023/024 assumption).
- Commit after each task or logical group; stop at any checkpoint to validate a story independently.
