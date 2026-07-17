# Tasks: JavaScript Test Reliability Under Bazel

**Input**: Design documents from `/specs/019-js-test-reliability/` — [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/run-vitest-shim.md](contracts/run-vitest-shim.md), [quickstart.md](quickstart.md)

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, contracts/

**Tests**: No new test-writing tasks. This feature repairs existing test infrastructure and tests; per Constitution §IV, `bazel build` + `bazel test` on the relevant `js_test` target is executed as part of each code task (not as separate tasks).

**Organization**: Tasks grouped by user story (US1 honest runner, US2 mock hardening, US3 failure fixes). US1 is the MVP and the blocking prerequisite for US2/US3.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on incomplete tasks)
- **[Story]**: Which user story this task belongs to (US1, US2, US3)
- Exact file paths included in every task

## Constitution compliance (read before coding — §V)

AGENTS.md and the spec/plan files are mandatory and not repeated below. **Each phase lists its additional required reading.** Per §IV, every code-change task runs `bazel build` + `bazel test` on the affected target(s) as part of the task.

---

## Phase 1: Setup (Shared Test-Runner Infrastructure)

**Purpose**: Create the single canonical shared shim that replaces the six drifting per-package copies.

**Required reading (§V)**:
- [contracts/run-vitest-shim.md](contracts/run-vitest-shim.md) (the exit-code contract + reference sketch)
- [research.md](research.md) §1 (API correctness) & §3 (shared-shim feasibility)
- [style/javascript.md](../../style/javascript.md)
- vitest `startVitest` API — https://vitest.dev/api/#startvitest
- aspect_rules_js `js_test` rule — https://docs.aspect.build/rules/aspect_rules_js/js_test/

- [ ] T001 Create the canonical hardened test-runner shim at `tools/dev/js/run_vitest.mjs` implementing the exit-code contract from `specs/019-js-test-reliability/contracts/run-vitest-shim.md`: parse `run`/`watch` tokens out of argv and pass the rest as `cliFilters` to `startVitest("test", filters, { watch: false })` (vitest 3.x signature); `await vitest.close()` before reading the result (FR-004); **fail-closed** — if `vitest.state.getCountOfFailedTests` is not callable, exit `1` (FR-001), never default to `0`; `process.exit(failed > 0 ? 1 : 0)`.
- [ ] T002 Register the shared shim by appending `exports_files(["run_vitest.mjs"])` to `tools/dev/js/BUILD.bazel` (depends on T001). Verify `bazel build //tools/dev/js:run_vitest.mjs`.

**Checkpoint**: One shared shim exists and is exportable as `//tools/dev/js:run_vitest.mjs`.

---

## Phase 2: User Story 1 — Honest Test Runner (Priority: P1) 🎯 MVP

**Goal**: Every `js_test` target faithfully reports pass/fail to Bazel (no false greens) and uses the single shared shim.

**Independent Test**: Run any `js_test` target with a deliberately-failing assertion through Bazel → it MUST report FAILED; remove the failure → MUST report PASSED ([quickstart.md](quickstart.md) Scenario 1).

**Required reading (§V)**:
- [contracts/run-vitest-shim.md](contracts/run-vitest-shim.md)
- [research.md](research.md) §1 & §3
- The six `BUILD.bazel` `js_test` blocks: `projects/game/agent/BUILD.bazel`, `common/js/logs/BUILD.bazel`, `common/js/resolver/BUILD.bazel`, `common/js/otel/BUILD.bazel`, `common/js/grpc/otel/BUILD.bazel`, `common/js/grpc/resolver/BUILD.bazel`

> **FR-005**: all six targets switch `entry_point` to `//tools/dev/js:run_vitest.mjs`; the per-package `run_vitest.mjs` files are deleted. `entry_point` is a `Label` whose file is auto-included in runfiles, so no `data` change is needed (each target keeps its own `:node_modules/vitest`).

- [ ] T003 [US1] Rewire `:lib_test` in `projects/game/agent/BUILD.bazel`: set `entry_point = "//tools/dev/js:run_vitest.mjs"` and delete the now-stale "Small bootstrap script…" comment block above the target; delete `projects/game/agent/run_vitest.mjs`. Verify `bazel test //projects/game/agent:lib_test`.
- [ ] T004 [P] [US1] Rewire `:lib_test` in `common/js/logs/BUILD.bazel` to `entry_point = "//tools/dev/js:run_vitest.mjs"`; delete the stale comment block and `common/js/logs/run_vitest.mjs`. Verify `bazel test //common/js/logs:lib_test`.
- [ ] T005 [P] [US1] Rewire `:lib_test` in `common/js/resolver/BUILD.bazel`; delete the stale comment block and `common/js/resolver/run_vitest.mjs`. Verify `bazel test //common/js/resolver:lib_test`.
- [ ] T006 [P] [US1] Rewire `:lib_test` in `common/js/otel/BUILD.bazel`; delete the stale comment block and `common/js/otel/run_vitest.mjs`. Verify `bazel test //common/js/otel:lib_test`.
- [ ] T007 [P] [US1] Rewire `:lib_test` in `common/js/grpc/otel/BUILD.bazel`; delete the stale comment block and `common/js/grpc/otel/run_vitest.mjs`. Verify `bazel test //common/js/grpc/otel:lib_test`.
- [ ] T008 [P] [US1] Rewire `:lib_test` in `common/js/grpc/resolver/BUILD.bazel`; delete the stale comment block and `common/js/grpc/resolver/run_vitest.mjs`. Verify `bazel test //common/js/grpc/resolver:lib_test`.
- [ ] T009 [US1] Validate runner honesty (SC-002, FR-001/FR-002): following [quickstart.md](quickstart.md) Scenario 1, temporarily inject a failing assertion, run `bazel test` on ≥3 targets across different packages, confirm each reports FAILED with a non-zero exit and a failing test in the log; revert the injection and confirm PASSED; additionally confirm an empty/mismatched filter is PASSED (vacuous), not a crash or hang.

**Checkpoint**: All six `js_test` targets run via the shared shim and Bazel's pass/fail matches the real outcome. US2/US3 can now begin.

---

## Phase 3: User Story 2 — Mock Hardening & Conventions (Priority: P2)

**Goal**: Every `vi.mock()` usage is reliable under both the vitest CLI and Bazel `js_test`; conventions are documented; the audit is recorded.

**Independent Test**: For each formerly-fragile file, direct-vitest and Bazel-`js_test` produce identical pass/fail and exercise the mocked path ([quickstart.md](quickstart.md) Scenario 3).

**Required reading (§V)**:
- [research.md](research.md) §2 (mock root-cause + audit table)
- [style/javascript.md](../../style/javascript.md)
- vitest mocking guide — https://vitest.dev/guide/mocking.html
- DI precedent: `projects/game/agent/src/build-tools.test.ts` (L4–9 comment + structure)
- The three fragile files: `common/js/logs/src/reporter.test.ts`, `projects/game/agent/src/llm-tools.test.ts`, `projects/game/agent/src/prompt-client.test.ts`

- [ ] T010 [US2] Append a `## 测试 (Testing)` section to `style/javascript.md` (FR-009) documenting: the `js_test` execution model (pre-compiled `:lib` vs vitest-CLI transpile-on-the-fly); the shared `run_vitest.mjs` entry-point contract (link [contracts/run-vitest-shim.md](contracts/run-vitest-shim.md)); the **reliable** pattern (dependency injection / `vi.fn()` test-double seam) vs the **fragile** pattern (module-level `vi.mock("external-dep")` consumed transitively by a pre-compiled `:lib`) with the [research.md](research.md) §2 root cause; and the "verify the mock is active" rule (FR-010). Also record the mock audit (FR-007) — every mock-using file classified reliable/fragile, reproducing the [research.md](research.md) §2 table.
- [ ] T011 [P] [US2] Refactor `common/js/logs/src/reporter.test.ts` to replace `vi.mock("@opentelemetry/api-logs", …)` (L112) with a dependency-injection seam that supplies the logger; remove the module mock. Verify `bazel test //common/js/logs:lib_test`.
- [ ] T012 [P] [US2] Refactor `projects/game/agent/src/llm-tools.test.ts` to replace the `vi.mock("langchain")` + `vi.hoisted` `createAgent` spy (L17–38) with a dependency-injection seam (per the `build-tools.test.ts` precedent). Verify `bazel test //projects/game/agent:lib_test`.
- [ ] T013 [US2] Refactor `projects/game/agent/src/prompt-client.test.ts`: keep the existing `PromptClient` ctor DI seam (L84–91); for the channel-construction/warmup tests still relying on module mocks (`@grpc/grpc-js`, `node:fs`, `@grpc/proto-loader`, `@dominion/common-js-grpc-resolver` at L27–72), convert to a factory seam where feasible, and ensure every remaining module mock is asserted-called (FR-010). Verify `bazel test //projects/game/agent:lib_test`.
- [ ] T014 [US2] Cross-mode equivalence (SC-003, FR-007/FR-010): for each file touched in T011–T013, run the file directly via the vitest CLI (transpile-on-the-fly) and via `bazel test` on its `js_test` target; confirm identical pass/fail and that the mocked code path is exercised (assert mock called). Follow [quickstart.md](quickstart.md) Scenario 3.

**Checkpoint**: All module-level `vi.mock()` usages are either refactored to DI or justified-and-asserted; conventions documented; both modes agree.

---

## Phase 4: User Story 3 — Pre-Existing Failure Fixes (Priority: P2)

**Goal**: Every pre-existing failure surfaced by the honest runner is fixed, or explicitly deferred with a tracked justification (zero silent reds).

**Independent Test**: `bazel test` on every `js_test` target reports PASSED with zero failing tests ([quickstart.md](quickstart.md) Scenario 4).

**Required reading (§V)**:
- [research.md](research.md) §4 (proto-path) & §5 (triage workflow + deferral rules)
- [spec.md](spec.md) Edge Cases and FR-011…FR-014
- [quickstart.md](quickstart.md) Scenario 4

> **Depends on US1** (honest runner) and **benefits from US2** (the `mock-interception` category reuses the Phase 3 DI approach). T016–T020 are driven by the failure list produced in T015; the exact set is discovered at execution time ([spec.md](spec.md) Assumptions).

- [ ] T015 [US3] Triage: run `bazel test` on all six `js_test` targets (`//projects/game/agent:lib_test`, `//common/js/{logs,resolver,otel}:lib_test`, `//common/js/grpc/{otel,resolver}:lib_test`) and record every failure as a list — target + file + test name + root-cause category (`mock-contract` | `proto-path` | `mock-interception` | `production-bug` | `out-of-scope`). This list drives T016–T020.
- [ ] T016 [P] [US3] Fix all `mock-contract` failures: correct each mock to faithfully match the real API's invocation contract (gRPC stub callback signatures, method shapes) — do NOT weaken the test's assertions (FR-012).
- [ ] T017 [P] [US3] Fix all `proto-path` failures using runfiles-aware `__dirname`-relative resolution (FR-013); in particular verify `common/js/grpc/otel/src/index.test.ts` `path.join(__dirname, "test_service.proto")` (L36) resolves under the Bazel sandbox.
- [ ] T018 [US3] Fix all `mock-interception` failures by applying the US2 dependency-injection refactor (coordinate with Phase 3 tasks T011–T013); ensure the resulting mock is asserted-called (FR-010).
- [ ] T019 [US3] Fix all `production-bug` failures in production code, scoped to making the test pass legitimately — do NOT weaken assertions (spec Edge Case).
- [ ] T020 [US3] For genuinely out-of-scope failures, mark `it.skip`/`describe.skip` with an inline justification referencing the concurrent effort; confirm zero silent/unjustified deferrals (FR-014, SC-004).

**Checkpoint**: Full green baseline; every skip is justified.

---

## Phase 5: Polish & Cross-Cutting Concerns

**Purpose**: Repository-wide verification and consistency after all stories land.

**Required reading (§V)**:
- [quickstart.md](quickstart.md)
- [plan.md](plan.md) Project Structure

- [ ] T021 Run `bazel test //projects/game/agent:lib_test //common/js/logs:lib_test //common/js/resolver:lib_test //common/js/otel:lib_test //common/js/grpc/otel:lib_test //common/js/grpc/resolver:lib_test`; confirm every target PASSED with zero failures (SC-001, [quickstart.md](quickstart.md) Scenario 4).
- [ ] T022 [P] Run the full [quickstart.md](quickstart.md) validation suite (Scenarios 1–5) end-to-end and record results.
- [ ] T023 [P] Run `bazel run //:gazelle` on the touched package dirs to keep BUILD files consistent after the `run_vitest.mjs` deletions; confirm no dangling references to any per-package `run_vitest.mjs` (search `projects/` and `common/`).
- [ ] T024 [P] Confirm the shared shim is the sole `run_vitest.mjs` in the repo (`tools/dev/js/run_vitest.mjs`) and all six `js_test` `entry_point` attributes equal `//tools/dev/js:run_vitest.mjs` ([quickstart.md](quickstart.md) Scenario 2).

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately. Produces `//tools/dev/js:run_vitest.mjs`.
- **US1 (Phase 2)**: Depends on Phase 1. **BLOCKS US2 and US3** — without an honest runner, mock fragility and failures cannot be detected through Bazel.
- **US2 (Phase 3)**: Depends on US1 (Phase 2). Provides the DI approach reused by US3's `mock-interception` fixes.
- **US3 (Phase 4)**: Depends on US1 (Phase 2); benefits from US2 (Phase 3) for the `mock-interception` category.
- **Polish (Phase 5)**: Depends on US1–US3 being complete.

### User Story Dependencies

- **US1 (P1)**: Foundation — can start after Phase 1; no dependencies on other stories.
- **US2 (P2)**: After US1. Independent of US3 (may run before or in parallel with US3's non-mock categories).
- **US3 (P2)**: After US1. The `mock-interception` category (T018) coordinates with US2.

### Within Each User Story

- Shared-infrastructure / wiring before validation.
- Per Constitution §IV, `bazel build` + `bazel test` on the affected target(s) accompanies each code task — not a separate task.
- Refactor before cross-mode equivalence check (US2).

### Parallel Opportunities

- Phase 1: T001 → T002 (sequential; T002 needs the file).
- Phase 2: T004–T008 are all `[P]` (distinct package BUILD files + shims) once T003 establishes the pattern; T009 runs after all wiring.
- Phase 3: T011 & T012 are `[P]` (distinct files); T013 then T014 (validation) last.
- Phase 4: T016 & T017 are `[P]` (distinct categories/files); T018–T020 are category-driven.
- Phase 5: T022, T023, T024 are `[P]` (read-only checks).

---

## Parallel Example: User Story 1 (Phase 2)

```bash
# After T003 (agent reference wiring) establishes the pattern, launch the five common/js rewire tasks together:
Task: "Rewire common/js/logs:lib_test entry_point + delete run_vitest.mjs"
Task: "Rewire common/js/resolver:lib_test entry_point + delete run_vitest.mjs"
Task: "Rewire common/js/otel:lib_test entry_point + delete run_vitest.mjs"
Task: "Rewire common/js/grpc/otel:lib_test entry_point + delete run_vitest.mjs"
Task: "Rewire common/js/grpc/resolver:lib_test entry_point + delete run_vitest.mjs"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1 (shared shim).
2. Complete Phase 2 (US1 — wire all targets + validate honesty).
3. **STOP and VALIDATE**: US1 independently — Scenario 1 (inject-fail) + Scenario 2 (shared shim).
4. At this point Bazel reports real results across all `js_test` targets — the core trust is restored.

### Incremental Delivery

1. Phase 1 → shared shim ready.
2. Phase 2 (US1) → honest runner; validate independently (MVP).
3. Phase 3 (US2) → reliable mocks + conventions; validate both-mode equivalence.
4. Phase 4 (US3) → green baseline; validate full-green + zero unjustified skips.
5. Phase 5 → polish; run full quickstart suite.

### Parallel Team Strategy

With multiple developers:
1. Team completes Phase 1 + US1 (Phase 2) together (US1 is the foundation).
2. Once US1 is done:
   - Developer A: US2 (Phase 3) — conventions + the three fragile-file refactors.
   - Developer B: US3 (Phase 4) triage + non-mock fixes (T016/T017/T019/T020); T018 (`mock-interception`) coordinates with Developer A.

---

## Notes

- `[P]` tasks = distinct files, no dependency on incomplete tasks.
- `[Story]` label maps each task to US1/US2/US3 for traceability.
- Per Constitution §IV, build + unit test (`bazel test` on the relevant `js_test`) is part of each code task — not listed separately.
- Per Constitution §V, each phase's "Required reading" block MUST be read before coding that phase.
- Per Constitution §VI, large tests are N/A (test infrastructure, not a service — see [plan.md](plan.md) Constitution Check).
- US3's exact failure inventory is discovered at execution time; tasks T016–T020 are category-driven and expand to cover whatever T015 surfaces.
- Commit after each task or logical group; stop at any checkpoint to validate the story independently.
