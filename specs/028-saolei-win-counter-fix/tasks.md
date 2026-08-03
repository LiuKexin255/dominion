# Tasks: Saolei Win-Detection Counter Cross-Check (False-Positive Fix)

**Input**: Design documents from `/specs/028-saolei-win-counter-fix/`

**Prerequisites**: plan.md (required), spec.md (required), research.md, data-model.md, contracts/, quickstart.md — all read during planning.

**Tests**: This feature modifies an existing vitest suite — the existing `*.test.ts` peers MUST be updated as part of each implementation task (Constitution §IV: compile + unit per change, not a separate task). New test files are created inline with their feature. No separate "tests-first" phase.

**Organization**: Tasks are grouped by user story. US1 (counter decode) is the enabling capability and the MVP; US2 (counter-informed `isWin`) depends on US1.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on an incomplete task).
- **[Story]**: Which user story (US1/US2). Setup/Foundational/Polish tasks have NO story label.
- All paths are repo-relative.

## Per-phase required reading (Constitution §V — read BEFORE coding each phase)

> AGENTS.md, `specs/028-saolei-win-counter-fix/spec.md`, and the feature design docs (`plan.md`, `research.md`, `data-model.md`, `contracts/`, `quickstart.md`) are mandatory baseline reading for every phase and are NOT repeated below. Each phase lists ONLY its additional code-spec / official / tech-article references. The exact counter decode coordinates (digit-cell origins, segment-core sub-rects, red-pixel test, threshold) are authoritative in `specs/028-saolei-win-counter-fix/research.md` D2/D3/D5 — implement from those measured values, not from memory.

---

## Phase 1: Setup

**Purpose**: make the two badcase fixtures available to the test suites.

**Phase 1 文档清单**:
- 代码规范文档: 无
- 官方文档: 无
- 技术文章: 无

- [X] T001 `git add` the two badcase fixtures `projects/game/pkg/saolei-board/testdata/saolei_9.png` and `projects/game/pkg/saolei-board/testdata/saolei_11.png` (currently untracked) so the `:lib_test` `data` glob (`testdata/*.png`) and the large-test embed pick them up. (No BUILD edit — `BUILD.bazel` already globs `testdata/*.png` and `src/core/**/*.ts`.)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the shared types both US1 and US2 depend on. **⚠️ MUST complete before any user-story work.**

**Phase 2 文档清单**:
- 代码规范文档: `style/javascript.md`; Google TypeScript Style — https://google.github.io/styleguide/tsguide.html
- 官方文档: 无
- 技术文章: 无

- [X] T002 Add the shared types to `projects/game/pkg/saolei-board/src/core/types.ts`: the `MineCounter` tagged union (`{ decoded: true; value: number } | { decoded: false }`), the `SegmentId` union (`"a".."g"`), and the `CounterProfile` interface (per `specs/028-saolei-win-counter-fix/data-model.md` entity 1 & 3); add the optional field `mineCounter?: MineCounter` to `GameState`. Do not change existing fields.
- [X] T003 Export the new types (`MineCounter`, `SegmentId`, `CounterProfile`) from the public barrel `projects/game/pkg/saolei-board/src/core/index.ts` (type-only re-exports, alongside the existing `GameState`/`ColorProfile` exports). (Depends on T002.)

**Checkpoint**: `bazel build //projects/game/pkg/saolei-board:core` succeeds; types are exported.

---

## Phase 3: User Story 1 — The Library Reads the Top-Left Mine Counter (Priority: P1) 🎯 MVP

**Goal**: `recognizeBoard` decodes the top-left 3-digit 7-segment LED and populates `state.mineCounter` with the correct value; the CLI `--debug` surfaces it.

**Independent Test**: `bazel test //projects/game/pkg/saolei-board:lib_test` is green and the new assertions hold — `saolei_9.png` ⇒ `value -1` (glyphs `-`,`0`,`1`), `saolei_10.png` ⇒ `value 0`, `saolei_11.png` ⇒ `value 0`; a synthetic undecodable region ⇒ `{ decoded: false }`.

**Phase 3 文档清单**:
- 代码规范文档: `style/javascript.md` (§测试 — DI / pure-function testing); Google TypeScript Style — https://google.github.io/styleguide/tsguide.html
- 官方文档: pngjs — https://github.com/pngjs/pngjs (the decoder consumes `DecodedImage`/`getRGB` from `src/core/decode.ts`, which decodes via pngjs); vitest — https://vitest.dev (for `counter.test.ts`)
- 技术文章: classic minesweeper LED/number colour reference — https://online.games.narkive.com/FUc9B1QB/colors-in-minesweeper (the counter's red-on-black LED uses the same classic red already cited in `classify.ts`/`types.ts`); the fixed-geometry colour-analysis approach — https://www.besthub.dev/articles/how-to-build-an-automated-minesweeper-bot-with-python-and-win32-api-d1d7ef54e731

**Implementation for User Story 1**

- [X] T004 [US1] Create `projects/game/pkg/saolei-board/src/core/counter.ts` (a peer of `classify.ts`) and its unit test `projects/game/pkg/saolei-board/src/core/counter.test.ts`: the pure `decodeMineCounter(img: DecodedImage, profile?: CounterProfile): MineCounter` function, the `DEFAULT_COUNTER_PROFILE` value (classic Win32 — the measured constants from `research.md` D2/D3/D5: region X 32 Y 120 82×50; digit-cell origins (38,126)/(64,126)/(90,126), 22×42; the 7 segment-core sub-rects; red test R≥150 G≤80 B≤80; segmentOnRatio 0.5), and the segment→glyph table + the `{g}`⇒`-` minus rule + value-with-sign semantics (`research.md` D4). Returns `{ decoded: false }` when a cell's ON-set matches no table entry. Pure — no I/O, consumes the already-decoded image only. The unit test covers each glyph's segment pattern (`0`-`9`), the lone-`{g}` minus, the value-with-sign computation (e.g. `-`+`0`+`1` ⇒ −1; `0`+`4`+`0` ⇒ 40), and an undecodable pattern ⇒ `{ decoded: false }`. Build synthetic `DecodedImage`s with red/black pixels at the segment cores (DI-free — pure function under test).
- [X] T005 [US1] Wire the counter decode into `recognizeBoard` and `SaoleiBoard` in `projects/game/pkg/saolei-board/src/core/recognize.ts`: after building the grid (same pass, same decoded image), call `decodeMineCounter(img, counterProfile)` and set `state.mineCounter` on the returned `state`; in `SaoleiBoard.init`, fix the `CounterProfile` alongside `ColorProfile`/`BoardGeometry` (default `DEFAULT_COUNTER_PROFILE`), and re-decode the counter on every `updateFromScreenshot` (the counter is non-monotonic — it is NOT part of `checkCompatible`). Verify `bazel test` still passes for existing `validate.ts` tests — `checkCompatible` must work correctly with the new `mineCounter` field on `GameState`. Export `decodeMineCounter` and `DEFAULT_COUNTER_PROFILE` from `projects/game/pkg/saolei-board/src/core/index.ts`. (Depends on T004.)
- [X] T006 [US1] Add counter-decode fixture assertions to `projects/game/pkg/saolei-board/src/core/golden.test.ts`: `recognizeBoard(saolei_9.png).state.mineCounter` ⇒ `{ decoded: true, value: -1 }`; `saolei_10.png` ⇒ `value 0`; `saolei_11.png` ⇒ `value 0` (FR-004 / SC-001). (Depends on T005.)
- [X] T007 [US1] Surface the decoded counter in the CLI `--debug` path in `projects/game/pkg/saolei-board/src/cli/cli.ts`: after the per-cell diagnostics block, print the decoded mine counter (value + the three glyphs, e.g. `mine counter: -01 (decoded, value -1)` or `mine counter: undecodable`). Read it from `result.state.mineCounter`. (Depends on T005.)

**Checkpoint**: US1 fully functional — `bazel test //projects/game/pkg/saolei-board:lib_test` green; `BAZEL_BINDIR=. ./bazel-bin/projects/game/pkg/saolei-board/cli_/cli <saolei_9.png> --debug` shows counter `-01`.

---

## Phase 4: User Story 2 — A Win Requires BOTH a Fully-Revealed Grid AND the Counter Reading 000 (Priority: P1)

**Goal**: `isWin` returns `true` only when the grid is fully revealed/flagged AND `mineCounter === { decoded: true, value: 0 }`; the three fixtures classify correctly; the saolei MCP's `won` decision becomes counter-informed (text contract unchanged).

**Independent Test**: `bazel test //projects/game/pkg/saolei-board:lib_test //projects/game/agent/src/mcp/saolei/...` green — `saolei_9` ⇒ `isWin false`, `saolei_11` ⇒ `isWin false`, `saolei_10` ⇒ `isWin true`; the MCP over-flag case ⇒ `game status: playing` + cell op allowed.

**Phase 4 文档清单**:
- 代码规范文档: `style/javascript.md` (§测试 — DI seam for the MCP test); Google TypeScript Style — https://google.github.io/styleguide/tsguide.html
- 官方文档: vitest — https://vitest.dev ; vitest Mocking Modules — Pitfalls — https://vitest.dev/guide/mocking/modules#mocking-modules-pitfalls (the MCP test uses the existing `SaoleiBoardApi` DI seam, not `vi.mock` of cross-package deps)
- 技术文章: 无

**Implementation for User Story 2**

- [X] T008 [US2] Strengthen `isWin` in `projects/game/pkg/saolei-board/src/core/win.ts` and update its test in `projects/game/pkg/saolei-board/src/core/win.test.ts`: return `true` ONLY when (a) no cell is `INITIAL`/`HIT_MINE`/`MINE`/`UNKNOWN` AND (b) `state.mineCounter === { decoded: true, value: 0 }` (FR-005..010). Keep it a single-argument pure function (`isWin(state: GameState)`) — read `state.mineCounter` internally. `mineCounter === undefined` or `{ decoded: false }` ⇒ `false` (lenient, FR-008). Update the file's doc comment + spec citation to reference this feature's spec (FR-005..010) alongside [027 FR-009..011]. The existing positive test cases (all-revealed/all-flag/mixed ⇒ `true`) MUST now set `mineCounter = { decoded: true, value: 0 }` on the `GameState` (via the `board()` helper) to remain `true`; add cases for counter non-zero with an all-revealed grid ⇒ `false` (FR-006), counter `000` with an `INITIAL` cell ⇒ `false` (FR-007), `mineCounter undefined` ⇒ `false` (FR-008), and a `HIT_MINE`/`MINE` loss ⇒ `false` regardless of counter (FR-010). (Depends on T002; logic only — no recognition dependency.)
- [X] T009 [US2] Add win-classification assertions to `projects/game/pkg/saolei-board/src/core/golden.test.ts`: `isWin(recognizeBoard(saolei_9.png).state) === false` (FR-006/SC-002), `isWin(... saolei_11.png ...) === false` (FR-007/SC-003), and keep `isWin(... saolei_10.png ...) === true` (FR-005/SC-003) (FR-011). (Depends on T005, T008.)
- [X] T010 [US2] Update the MCP consumption in `projects/game/agent/src/mcp/saolei/saolei-mcp.ts` and its test `projects/game/agent/src/mcp/saolei/saolei-mcp.test.ts`: `gameStatus`, `validateMove`, and `isTerminalState` now derive `won` via the counter-informed `isWin(state)` (they already take `state: GameState`, which now carries `mineCounter` — NO signature changes). The default `createDefaultBoardApi` already returns the real `SaoleiBoard.state` (which now carries the decoded counter), so no seam change. Update the module doc comment's win-rule description. In the test, the existing win test case(s) must supply a `GameState` whose `mineCounter` is `{ decoded: true, value: 0 }` (extend the `board()` / `makeFakeBoardApi` helper to set it) so the genuine-win path still ⇒ `game status: won` + `game_won`; add a new case — an all-revealed grid with `mineCounter = { decoded: true, value: -1 }` (the `saolei_9` over-flag shape) ⇒ result body contains `game status: playing` and a following cell op is ALLOWED (dispatched, not `game_won`) (FR-012 / SC-004). (Depends on T005, T008.)

**Checkpoint**: US2 fully functional — `bazel test //projects/game/pkg/saolei-board:lib_test //projects/game/agent/src/mcp/saolei/...` green.

---

## Phase 5: Polish & Cross-Cutting Concerns (Docs + Large-Test Acceptance)

**Purpose**: documentation (FR-013) and the Constitution §VI large-test acceptance gate. T011/T012 (docs) require no additional reading beyond the feature spec/plan baseline.

- [X] T011 [P] Update `projects/game/pkg/saolei-board/README.md`: document the mine-counter cross-check as the win condition (a board is a win iff the grid is fully revealed/flagged AND the counter reads `000`), the lenient undecodable-counter behaviour, and the counter output in the `--debug` calibration flow (FR-013).
- [X] T012 [P] Update `projects/game/agent/src/skill/saolei/SKILL.md`: update the "Game-status line" win rule (the `won = every non-mine cell revealed and every mine auto-flagged` sentence) to state the counter-`000` cross-check, so the model reads the corrected rule (FR-013; the skill is the model's authority on the result format per [027 FR-024]).

**Phase 5 文档清单** (for T013/T014):
- 代码规范文档: `style/golang.md` (§单元测试 — 命名风格、表驱动、`given/when/then`、禁止在用例中塞断言); `style/large_test.md` (§测试组织 — add to the EXISTING module file `agent_saolei_test.go`, reuse `helpers_test.go`, do NOT create a new test-plan YAML or a spec-id-named file)
- 官方文档: 无
- 技术文章: 无

- [X] T013 Extend the large test `projects/game/testplan/agent_saolei_test.go` (Go, per `style/golang.md` + `style/large_test.md`): copy `projects/game/pkg/saolei-board/testdata/saolei_9.png` into `projects/game/testplan/testdata/saolei_9.png` and add a `//go:embed testdata/saolei_9.png` (the large test embeds its OWN testdata copies — see the existing `saolei_1/2/5/10` embeds); add a test function (follow the pattern of the existing `TestSaoleiWinDetection` or the `saolei_10` genuine-win case in the same file, reusing the helpers in `helpers_test.go`) that seeds the over-flag board via `saolei_init`, asserts the init result carries `game status: playing` (NOT `won`), and asserts a following cell operation is DISPATCHED (the desktop receives the operation — NOT rejected as `game_won`). Keep the existing genuine-win case (`saolei_10` ⇒ `won` + `game_won`) green. Do NOT create a new test-plan YAML — add the case to the existing saolei large-test plan/binary. (Depends on T010.)
- [X] T014 Run the saolei large test via the **testplan** skill (`guitar run <plan.yaml>`) — full deploy → test → cleanup. This is the Constitution §VI acceptance gate: ALL cases (including the new over-flag case and the preserved genuine-win/loss cases) MUST pass; any failed/flaky case ⇒ fix and re-run until fully green. Build-only (`bazel build` of the test target) does NOT constitute acceptance. (Depends on T013 and all prior phases.)

**Checkpoint**: docs updated; large test executed via testplan skill with all cases green → feature accepted.

---

## Dependencies & Execution Order

### Phase Dependencies
- **Phase 1 (Setup)**: none — start immediately (T001).
- **Phase 2 (Foundational)**: depends on T001 — BLOCKS US1/US2 (the `MineCounter`/`GameState.mineCounter` types are required by both).
- **Phase 3 (US1)**: depends on Phase 2.
- **Phase 4 (US2)**: depends on Phase 3 (US2 needs recognition to populate `state.mineCounter`, which US1 delivers in T005). The pure `isWin` logic (T008) only needs the Phase-2 types, but its fixture/MCP verification (T009/T010) needs US1's recognition wiring.
- **Phase 5 (Polish)**: T011/T012 are independent (can start after Phase 2); T013 depends on US2; T014 depends on T013 + everything.

### Within US1
- T004 (decoder + test) → T005 (recognition wiring) ; T006 (golden counter assertions) and T007 (CLI debug) depend on T005 and can proceed in parallel after it.

### Within US2
- T008 (`isWin` logic + test) → T009 (golden win) and T010 (MCP wiring + test) can proceed in parallel after T008.

### Parallel Opportunities
- Phase 2: none (T003 depends on T002).
- US1: T006 ∥ T007 (after T005).
- US2: T009 ∥ T010 (after T008).
- Polish: T011 ∥ T012 (different doc files, no deps).

---

## Implementation Strategy

### MVP First (US1 only)
1. Phase 1 (T001) → Phase 2 (T002, T003) → Phase 3 (T004–T007).
2. **STOP and VALIDATE**: `bazel test //projects/game/pkg/saolei-board:lib_test` green; the counter decoder is correct on the three fixtures and exposed via recognition + `--debug`. US1 is a viable standalone increment (it adds the recognition capability without yet changing `isWin`).

### Incremental Delivery
3. Phase 4 (T008–T010): the counter-informed `isWin` fix + MCP wiring. **VALIDATE**: `bazel test //projects/game/pkg/saolei-board:lib_test //projects/game/agent/src/mcp/saolei/...` green — the false-positive (`saolei_9`) is now `playing`.
4. Phase 5 (T011–T014): docs + the large-test acceptance gate (§VI) executed via the testplan skill, all green.

---

## Notes
- No BUILD.bazel / gazelle edits are expected: `:lib_test` already globs `src/core/**/*.ts` + `testdata/*.png` + `testdata/*.golden.txt`, and `:core` globs `src/core/**/*.ts` (excluding tests). New `counter.ts`/`counter.test.ts` and the badcase PNGs (once `git add`-ed) are auto-included. Verify with `bazel build //...` + `bazel test //...` after each phase (Constitution §IV).
- Code format + deps: after TS edits run `bazel run //:go -- fmt <files>` is N/A (that's Go); for TS use the repo's prettier/eslint via the normal edit flow (see `style/javascript.md`). No new npm dependency is introduced (pngjs is already a dep).
- `isWin` stays a single-argument pure function (FR-009) and the MCP text-result contract is unchanged in wording (FR-012) — only the `won` decision becomes counter-informed.
