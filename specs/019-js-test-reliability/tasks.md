# Tasks: JavaScript Test Reliability Under Bazel

**Input**: Design documents from `/specs/019-js-test-reliability/` — [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/run-vitest-shim.md](contracts/run-vitest-shim.md), [quickstart.md](quickstart.md)

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, contracts/

**Tests**: No new test-writing tasks. This feature repairs existing test infrastructure and tests; per Constitution §IV, `bazel build` + `bazel test` on the relevant `js_test` target is executed as part of each code task (not as separate tasks).

**Organization**: Tasks grouped by user story (US1 honest runner, US2 mock hardening, US3 failure fixes). US1 is the MVP and the blocking prerequisite for US2/US3.

> **Architecture note (revised post-execution — read before Phase 2).** The shared shim is
> delivered via a single macro `vitest_test` (`tools/dev/js/vitest_test.bzl`), NOT a cross-package
> `entry_point` label (which fails — see [plan.md — Architecture Revision](plan.md#architecture-revision-execution-discovery)).
> The macro internally `genrule`-copies the canonical source `//tools/dev/js:run_vitest.mjs` into
> each consuming package and wires the `js_test`; callers pass only the code under test + the
> test-file glob. Phase 2 implements the macro and rewires all six targets through it.
>
> **Module-identity note (read before Phase 4 — Fix B).** Phase 2 wired callers with `data = [":lib"]`
> (the pre-compiled CJS artifact). US3 execution proved this causes a dual-module-instance root cause
> (`instanceof`/singleton divergence — vitest does not intercept `require()` inside the compiled `:lib`;
> see [plan.md — Module-Identity Revision](plan.md#module-identity-revision-execution-discovery) and
> [research.md](research.md) §6). Phase 4 (Fix B) changes each caller's `data` to the package's raw
> `.ts` source (`glob(["src/**/*.ts"])`), dropping `:lib`, so vitest transforms the whole package
> through one pipeline → single module instance (== CLI). After Phase 4, callers pass source, not `:lib`.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on incomplete tasks)
- **[Story]**: Which user story this task belongs to (US1, US2, US3)
- Exact file paths included in every task

## Constitution compliance (read before coding — §V)

AGENTS.md and the spec/plan files are mandatory and not repeated below. **Each phase lists its additional required reading.** Per §IV, every code-change task runs `bazel build` + `bazel test` on the affected target(s) as part of the task.

---

## Phase 1: Setup (Shared Test-Runner Infrastructure) — ✅ DONE (committed `cbf9ce7`)

**Purpose**: Create the single canonical hardened shim and export it so the `vitest_test` macro (Phase 2) can consume it.

**Required reading (§V)**:
- [contracts/run-vitest-shim.md](contracts/run-vitest-shim.md) (the exit-code contract + reference sketch)
- [research.md](research.md) §1 (API correctness)
- [style/javascript.md](../../style/javascript.md) (repo JS/TS conventions entry — currently a baseline pointer only, no local rules) + [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html) (the actual normative content; §V no-citation-transitivity — the repo file only references this)
- vitest `startVitest` programmatic API — https://vitest.dev/guide/advanced/tests (the actual location of the `startVitest('test', cliFilters, options, viteOverrides?, vitestOptions?)` signature + `createVitest`/`start()` alternative; NOTE: the `/api/` page is the Test API Reference for `test()`/`it()`, NOT this function)

- [X] T001 Create the canonical hardened test-runner shim at `tools/dev/js/run_vitest.mjs` implementing the exit-code contract from `specs/019-js-test-reliability/contracts/run-vitest-shim.md`: parse `run`/`watch` tokens out of argv and pass the rest as `cliFilters` to `startVitest("test", filters, { watch: false })` (vitest 3.x signature `(mode, cliFilters, options, viteOverrides?, vitestOptions?)`); `await vitest.close()` before reading the result (FR-004); **fail-closed** — if `vitest.state.getCountOfFailedTests` is not callable, exit `1` (FR-001), never default to `0`; `process.exit(failed > 0 ? 1 : 0)`.
- [X] T002 Register the shared shim by appending `exports_files(["run_vitest.mjs"])` to `tools/dev/js/BUILD.bazel` (depends on T001). Verify `bazel build //tools/dev/js:run_vitest.mjs`.

**Checkpoint**: One shared shim exists and is exportable as `//tools/dev/js:run_vitest.mjs`. *(Committed; preserved unchanged by the architecture revision — the macro consumes this label as a `genrule` input.)*

---

## Phase 2: User Story 1 — Honest Test Runner (Priority: P1) 🎯 MVP

**Goal**: Every `js_test` target faithfully reports pass/fail to Bazel (no false greens) and obtains the shared shim through the single `vitest_test` macro.

**Independent Test**: Run any `js_test` target with a deliberately-failing assertion through Bazel → it MUST report FAILED; remove the failure → MUST report PASSED ([quickstart.md](quickstart.md) Scenario 1).

**Required reading (§V)**:
- [plan.md](plan.md) — **Structure Decision** (the `vitest_test` macro sketch + caller sketch + "Why a macro" rationale) and **Architecture Revision** (why cross-package `entry_point` fails, why the macro/genrule mechanism works)
- [research.md](research.md) §3 (the verified macro/genrule decision; §1 for the shim API the macro invokes)
- [tools/dev/js/vite.bzl](../../tools/dev/js/vite.bzl) — repo Starlark rule/macro style reference (docstring, `def`, `rule`/macro, attr naming). **Phase 2 writes Starlark (`.bzl` macro + `BUILD.bazel`), NOT JS/TS** — the JS style guide does not apply here; use this file as the local Starlark idiom reference.
- aspect_rules_js `js_test`/`js_binary` rule source — https://github.com/aspect-build/rules_js/blob/main/js/private/js_binary.bzl (authoritative `entry_point`/`data`/`copy_data_to_bin` attribute docs that the macro wraps; the `docs.aspect.build` site is JS-rendered and returns empty to fetchers, so read this source instead)

> **FR-005 (delivery via macro)**: all six targets are declared with the `vitest_test` macro; the per-package `run_vitest.mjs` SOURCE files are deleted. The macro auto-injects the `:node_modules/vitest` data dep and the per-package generated `entry_point`; callers pass only the lib under test + test-file data (+ any test-specific `node_modules/*` deps). `size` and other `js_test` attrs forward via `**kwargs`.

- [ ] T003 [US1] Create the stable external macro `vitest_test` at `tools/dev/js/vitest_test.bzl` per the sketch in [plan.md](plan.md) Structure Decision: `def vitest_test(name, data, args=["run","src/"], **kwargs)` that emits (a) `native.genrule(name="%s_shim"%name, srcs=["//tools/dev/js:run_vitest.mjs"], outs=["%s_run_vitest.mjs"%name], cmd="cp $< $@")` and (b) `js_test(name=name, entry_point=":%s_run_vitest.mjs"%name, data=data+[":node_modules/vitest"], args=args, **kwargs)`. Add a docstring stating the prerequisite (caller package must run `npm_link_all_packages(name="node_modules")`). Then rewire `:lib_test` in `projects/game/agent/BUILD.bazel` as the reference consumer: `load("//tools/dev/js:vitest_test.bzl", "vitest_test")`, replace the `js_test(...)` block with a `vitest_test(name="lib_test", data=[...], size="small")` call — pass the existing `data` entries **except** `:node_modules/vitest` (auto-injected) and drop the `entry_point`/`args` (macro defaults); delete the now-stale "Small bootstrap script…" comment block above the target; delete `projects/game/agent/run_vitest.mjs`. Verify `bazel test //projects/game/agent:lib_test`. *(The target MAY report FAILED — that is the honest runner surfacing pre-existing failures, not a wiring regression; US3 fixes them. Confirm the failure is real test failures, not a `Cannot find package 'vitest'`/analysis error.)*
- [ ] T004 [P] [US1] Rewire `:lib_test` in `common/js/logs/BUILD.bazel` via the macro: `load("//tools/dev/js:vitest_test.bzl","vitest_test")` → `vitest_test(name="lib_test", data=[":lib"]+glob(["src/**/*.test.ts"], allow_empty=True), size="small")` (drop `:node_modules/vitest` and `entry_point`); delete the stale "Small bootstrap script…" comment block; delete `common/js/logs/run_vitest.mjs`. Verify `bazel test //common/js/logs:lib_test`.
- [ ] T005 [P] [US1] Rewire `:lib_test` in `common/js/resolver/BUILD.bazel` via the macro: `vitest_test(name="lib_test", data=[":lib", ":node_modules/@types/node"]+glob(["src/**/*.test.ts"], allow_empty=True), size="small")`; delete `common/js/resolver/run_vitest.mjs`. (No stale comment block in this file.) Verify `bazel test //common/js/resolver:lib_test`.
- [ ] T006 [P] [US1] Rewire `:lib_test` in `common/js/otel/BUILD.bazel` via the macro: `vitest_test(name="lib_test", data=["src/index.test.ts", ":lib"], size="small")` (otel uses an explicit test file, not a glob — preserve that); delete the stale "Small bootstrap script…" comment block; delete `common/js/otel/run_vitest.mjs`. Verify `bazel test //common/js/otel:lib_test`.
- [ ] T007 [P] [US1] Rewire `:lib_test` in `common/js/grpc/otel/BUILD.bazel` via the macro: `vitest_test(name="lib_test", data=["src/test_service.proto", ":lib", ":node_modules/@grpc/grpc-js", ":node_modules/@grpc/proto-loader", ":node_modules/@opentelemetry/instrumentation", ":node_modules/@opentelemetry/sdk-trace-base", ":node_modules/@opentelemetry/sdk-trace-node"]+glob(["src/**/*.test.ts"], allow_empty=True), size="small")`; delete the stale "Small bootstrap script…" comment block; delete `common/js/grpc/otel/run_vitest.mjs`. Verify `bazel test //common/js/grpc/otel:lib_test`.
- [ ] T008 [P] [US1] Rewire `:lib_test` in `common/js/grpc/resolver/BUILD.bazel` via the macro: `vitest_test(name="lib_test", data=[":lib", ":node_modules/@dominion/common-js-resolver", ":node_modules/@grpc/grpc-js", ":node_modules/@types/node"]+glob(["src/**/*.test.ts"], allow_empty=True), size="small")`; delete `common/js/grpc/resolver/run_vitest.mjs`. (No stale comment block in this file.) Verify `bazel test //common/js/grpc/resolver:lib_test`.
- [ ] T009 [US1] Validate runner honesty (SC-002, FR-001/FR-002): following [quickstart.md](quickstart.md) Scenario 1, temporarily inject a failing assertion, run `bazel test` on ≥3 targets across different packages, confirm each reports FAILED with a non-zero exit and a failing test in the log; revert the injection and confirm the prior result (PASSED, or FAILED-only-for-pre-existing-reasons); additionally confirm an empty/mismatched filter is PASSED (vacuous), not a crash or hang. **The injected assertion MUST be fully reverted** before completing this task.

**Checkpoint**: All six `js_test` targets run via the `vitest_test` macro and Bazel's pass/fail matches the real outcome. US2/US3 can now begin.

---

## Phase 3: User Story 2 — Mock Hardening & Conventions (Priority: P2)

**Goal**: Every `vi.mock()` usage is reliable under both the vitest CLI and Bazel `js_test`; conventions are documented; the audit is recorded.

**Independent Test**: For each formerly-fragile file, direct-vitest and Bazel-`js_test` produce identical pass/fail and exercise the mocked path ([quickstart.md](quickstart.md) Scenario 3).

**Required reading (§V)**:
- [research.md](research.md) §2 (mock root-cause + audit table)
- [style/javascript.md](../../style/javascript.md) (repo JS/TS conventions entry) + [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html) (the actual normative content; §V no-citation-transitivity)
- vitest mocking guide — https://vitest.dev/guide/mocking.html
- DI precedent: `projects/game/agent/src/build-tools.test.ts` (L4–9 comment + structure)
- The three fragile files: `common/js/logs/src/reporter.test.ts`, `projects/game/agent/src/llm-tools.test.ts`, `projects/game/agent/src/prompt-client.test.ts`

- [ ] T010 [US2] Append a `## 测试 (Testing)` section to `style/javascript.md` (FR-009) documenting: the `js_test` execution model (pre-compiled `:lib` vs vitest-CLI transpile-on-the-fly); the shared `run_vitest.mjs` entry-point contract (link [contracts/run-vitest-shim.md](contracts/run-vitest-shim.md)) **and the `vitest_test` macro as the supported usage surface** (callers pass only lib + test glob; the per-package shim copy is a macro-internal detail — do NOT write raw `genrule`/`entry_point`); the **reliable** pattern (dependency injection / `vi.fn()` test-double seam) vs the **fragile** pattern (module-level `vi.mock("external-dep")` consumed transitively by a pre-compiled `:lib`) with the [research.md](research.md) §2 root cause; and the "verify the mock is active" rule (FR-010). Also record the mock audit (FR-007) — every mock-using file classified reliable/fragile, reproducing the [research.md](research.md) §2 table.
- [ ] T011 [P] [US2] Refactor `common/js/logs/src/reporter.test.ts` to replace `vi.mock("@opentelemetry/api-logs", …)` (L112) with a dependency-injection seam that supplies the logger; remove the module mock. Verify `bazel test //common/js/logs:lib_test`.
- [ ] T012 [P] [US2] Refactor `projects/game/agent/src/llm-tools.test.ts` to replace the `vi.mock("langchain")` + `vi.hoisted` `createAgent` spy (L17–38) with a dependency-injection seam (per the `build-tools.test.ts` precedent). Verify `bazel test //projects/game/agent:lib_test`.
- [ ] T013 [US2] Refactor `projects/game/agent/src/prompt-client.test.ts`: keep the existing `PromptClient` ctor DI seam (L84–91); for the channel-construction/warmup tests still relying on module mocks (`@grpc/grpc-js`, `node:fs`, `@grpc/proto-loader`, `@dominion/common-js-grpc-resolver` at L27–72), convert to a factory seam where feasible, and ensure every remaining module mock is asserted-called (FR-010). Verify `bazel test //projects/game/agent:lib_test`.
- [ ] T014 [US2] Cross-mode equivalence (SC-003, FR-007/FR-010): for each file touched in T011–T013, run the file directly via the vitest CLI (transpile-on-the-fly) and via `bazel test` on its `js_test` target; confirm identical pass/fail and that the mocked code path is exercised (assert mock called). Follow [quickstart.md](quickstart.md) Scenario 3.

**Checkpoint**: All module-level `vi.mock()` usages are either refactored to DI or justified-and-asserted; conventions documented; both modes agree.

---

## Phase 4: Module-Identity Infrastructure (Fix B — test against source) — 🆕 inserted post-execution

**Purpose**: Eliminate the dual-module-instance root cause (discovered during US3 execution — see [research.md](research.md) §6 and [plan.md — Module-Identity Revision](plan.md#module-identity-revision-execution-discovery)) so that `instanceof` checks and module singletons behave identically under Bazel `js_test` and the vitest CLI. **Prerequisite for US3's full-green goal (SC-001)**: ~50+ failures share this root cause and cannot be fixed per-test.

**Goal**: Each `js_test` target runs against the **raw package `.ts` source** (production + tests) instead of the pre-compiled CJS `:lib`, so vitest transforms the whole package through a single Vite pipeline → one module instance (== CLI mode, SC-003 holds by construction).

**Independent Test**: `bazel test //common/js/resolver:lib_test` → the ~41 `instanceof InvalidTargetError` failures (and the logs/agent singleton failures) are gone; resolver/logs/agent/grpc-otel targets report no module-identity failures.

**Required reading (§V)**:
- [research.md](research.md) §6 (root cause + empirical CLI-vs-Bazel evidence + Fix B decision + vitest maintainer citations #7591/#5601/#6494/#9147)
- [plan.md](plan.md) — **Module-Identity Revision (Execution Discovery)** (the corrected design + caller sketch + trade-off + alternatives rejected)
- vitest official basis: [vitest#7591 instanceof not working in monorepo](https://github.com/vitest-dev/vitest/issues/7591) and [vitest#5601 instanceof fails](https://github.com/vitest-dev/vitest/issues/5601) (the CJS-`require`-not-intercepted behavior + `server.deps.inline` lever)
- [style/javascript.md](../../style/javascript.md) §测试 (the conventions this phase updates)
- `tools/dev/js/vitest_test.bzl` (the macro whose caller contract changes)

> **Fix B mechanic**: change each `vitest_test` caller's `data` from `[":lib"] + glob(["src/**/*.test.ts"])` to `glob(["src/**/*.ts"])` (drop `:lib`; keep explicit `:node_modules/*` deps + proto data). `:lib` must NOT also be present (ambiguity between `errors.js` and `errors.ts` re-creates the dual instance). The `vitest_test` macro itself is unchanged (forwards `data`, auto-injects `:node_modules/vitest`).

- [ ] T025 [infra] Update the `vitest_test` macro docstring in `tools/dev/js/vitest_test.bzl` to document the new caller contract: `data` MUST be the package's raw `.ts` source (production + tests), NOT `:lib`; explain why (dual-module-instance — vitest does not intercept `require()` inside the pre-compiled CJS `:lib`, so production code and the test resolve shared modules to two instances → `instanceof`/singleton divergence; see research.md §6). The macro body is unchanged.
- [ ] T026 [US1.5] Rewire all six `vitest_test` callers to source-based `data` (drop `:lib`; keep each target's existing `:node_modules/*` deps and any proto `data`):
  - `projects/game/agent/BUILD.bazel`: `data = glob(["src/**/*.ts"], allow_empty = True) + [":node_modules/@dominion/common-js-grpc-resolver", ":node_modules/@dominion/common-js-logs", ":node_modules/@dominion/common-js-resolver", ":node_modules/@grpc/grpc-js", ":node_modules/@grpc/proto-loader", ":node_modules/@types/node", ":node_modules/zod"]`. Verify `bazel test //projects/game/agent:lib_test`.
  - `common/js/logs/BUILD.bazel`: `data = glob(["src/**/*.ts"], allow_empty = True)`. Verify `bazel test //common/js/logs:lib_test`.
  - `common/js/resolver/BUILD.bazel`: `data = glob(["src/**/*.ts"], allow_empty = True) + [":node_modules/@types/node"]`. Verify `bazel test //common/js/resolver:lib_test`.
  - `common/js/otel/BUILD.bazel`: `data = glob(["src/**/*.ts"], allow_empty = True)` (subsumes the prior explicit `src/index.test.ts`; `:lib` dropped). Verify `bazel test //common/js/otel:lib_test`.
  - `common/js/grpc/otel/BUILD.bazel`: `data = glob(["src/**/*.ts"], allow_empty = True) + ["src/test_service.proto", ":node_modules/@grpc/grpc-js", ":node_modules/@grpc/proto-loader", ":node_modules/@opentelemetry/instrumentation", ":node_modules/@opentelemetry/sdk-trace-base", ":node_modules/@opentelemetry/sdk-trace-node"]`. Verify `bazel test //common/js/grpc/otel:lib_test`.
  - `common/js/grpc/resolver/BUILD.bazel`: `data = glob(["src/**/*.ts"], allow_empty = True) + [":node_modules/@dominion/common-js-resolver", ":node_modules/@grpc/grpc-js", ":node_modules/@types/node"]`. Verify `bazel test //common/js/grpc/resolver:lib_test`.
- [ ] T027 Verify the dual-instance root cause is resolved: run `bazel test` on all six targets; confirm the `instanceof InvalidTargetError`/singleton failures (resolver ~41, logs 6, agent handler/OperationBridge/ChatOpenAI, grpc/otel OTel-tracer) are GONE (not merely masked). Record the new per-target pass/fail counts — this is the input to Phase 5 (US3) re-triage. Confirm a deliberate failing assertion still surfaces FAILED (runner honesty preserved).
- [ ] T028 Update `style/javascript.md` §测试: the execution-model table changes — in BOTH modes vitest now transforms the package `.ts` source through a single Vite pipeline (the "pre-compiled `:lib` vs vitest-CLI transpile-on-the-fly" contrast is retired); the dual-instance root cause is recorded as removed at the infrastructure level (tests no longer consume `:lib`); the §2 `vi.mock` interception gap note is updated to reflect that the gap no longer arises for the package's own code once `:lib` is not in test `data`. State the trade-off: tests execute source, not the swc-compiled artifact; compile/type correctness is retained via `:lib`'s build.

**Checkpoint**: All six `js_test` targets run against source via the `vitest_test` macro; module-identity failures are gone; both modes agree by construction. US3 (Phase 5) can re-triage the (much smaller) genuine failure set.

---

## Phase 5: User Story 3 — Pre-Existing Failure Fixes (Priority: P2)

**Goal**: Every pre-existing failure surfaced by the honest runner is fixed, or explicitly deferred with a tracked justification (zero silent reds).

**Independent Test**: `bazel test` on every `js_test` target reports PASSED with zero failing tests ([quickstart.md](quickstart.md) Scenario 4).

**Required reading (§V)**:
- [research.md](research.md) §4 (proto-path) & §5 (triage workflow + deferral rules)
- [spec.md](spec.md) Edge Cases and FR-011…FR-014
- [quickstart.md](quickstart.md) Scenario 4

> **Depends on Phase 4 (Fix B)** — the module-identity infrastructure MUST land first: it removes ~50+ dual-instance failures so the genuine failure set is small enough to triage. **Benefits from US2** (Phase 3 — the `mock-interception` category reuses the DI approach). T016–T020 are driven by the failure list produced in T015; the exact set is discovered at execution time ([spec.md](spec.md) Assumptions).
>
> **First-pass triage already executed (commit `76549cf`)**: T015 produced an 84-failure inventory; T016 (mock-contract) fixed grpc/otel `getFinishedSpanItems`→`getFinishedSpans` + prompt-client getProfile 4-arg contract; T019 (production-bug) fixed the prompt-client keepalive stale assertion (300_000/0). These are committed. The remaining ~77 failures were dominated by the dual-instance root cause (now Phase 4) plus a few genuine items (named-port resolution, grpc/resolver fake-timer loop, handler empty frames, enum serialization) — **re-triage after Phase 4** (T015-redo).

- [X] T015 [US3] Triage (FIRST PASS — done, commit `76549cf`): ran `bazel test` on all six `js_test` targets; recorded an 84-failure inventory (otel 0; agent 22; logs 6; resolver 45; grpc/otel 4; grpc/resolver 7) classified by category, with ~50+ sharing the dual-instance root cause. **After Phase 4 (Fix B) lands, RE-RUN this triage** (`T015-redo`): the dual-instance failures should be gone; re-classify the (much smaller) remaining set into `mock-contract` | `proto-path` | `mock-interception` | `production-bug` | `out-of-scope`. This re-triage list drives the remainder of T016–T020.
- [ ] T016 [P] [US3] Fix all `mock-contract` failures: correct each mock to faithfully match the real API's invocation contract (gRPC stub callback signatures, method shapes) — do NOT weaken the test's assertions (FR-012).
- [ ] T017 [P] [US3] Fix all `proto-path` failures using runfiles-aware `__dirname`-relative resolution (FR-013); in particular verify `common/js/grpc/otel/src/index.test.ts` `path.join(__dirname, "test_service.proto")` (L36) resolves under the Bazel sandbox.
- [ ] T018 [US3] Fix all `mock-interception` failures by applying the US2 dependency-injection refactor (coordinate with Phase 3 tasks T011–T013); ensure the resulting mock is asserted-called (FR-010).
- [ ] T019 [US3] Fix all `production-bug` failures in production code, scoped to making the test pass legitimately — do NOT weaken assertions (spec Edge Case).
- [ ] T020 [US3] For genuinely out-of-scope failures, mark `it.skip`/`describe.skip` with an inline justification referencing the concurrent effort; confirm zero silent/unjustified deferrals (FR-014, SC-004).

**Checkpoint**: Full green baseline; every skip is justified.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Repository-wide verification and consistency after all stories land.

**Required reading (§V)**:
- [quickstart.md](quickstart.md)
- [plan.md](plan.md) Project Structure & Structure Decision (the `vitest_test` macro)

- [ ] T021 Run `bazel test //projects/game/agent:lib_test //common/js/logs:lib_test //common/js/resolver:lib_test //common/js/otel:lib_test //common/js/grpc/otel:lib_test //common/js/grpc/resolver:lib_test`; confirm every target PASSED with zero failures (SC-001, [quickstart.md](quickstart.md) Scenario 4).
- [ ] T022 [P] Run the full [quickstart.md](quickstart.md) validation suite (Scenarios 1–5) end-to-end and record results.
- [ ] T023 [P] Run `bazel run //:gazelle` on the touched package dirs to keep BUILD files consistent after the `run_vitest.mjs` deletions; confirm no dangling references to any per-package `run_vitest.mjs` (search `projects/` and `common/`) and no raw `js_test`/`genrule`/`entry_point` test-runner boilerplate outside the `vitest_test` macro.
- [ ] T024 [P] Confirm the shared shim is the sole `run_vitest.mjs` SOURCE in the repo (`tools/dev/js/run_vitest.mjs`), all six `js_test` packages declare their target via the `vitest_test` macro (no raw `entry_point`/`genrule` test-runner code), and `tools/dev/js:run_vitest.mjs` is the only `genrule` input for the shim copy ([quickstart.md](quickstart.md) Scenario 2).

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: ✅ DONE (committed `cbf9ce7`). Produces `//tools/dev/js:run_vitest.mjs` (the canonical source the macro consumes).
- **US1 (Phase 2)**: ✅ DONE. Depends on Phase 1. **BLOCKS US2, US3, and the module-identity phase** — without an honest runner delivered via the macro, nothing downstream is detectable through Bazel.
- **US2 (Phase 3)**: ✅ DONE. Depends on US1 (Phase 2). Provides the DI approach reused by US3's `mock-interception` fixes.
- **Module-Identity Infrastructure (Phase 4)**: 🆕 Depends on Phase 2. Removes the dual-instance root cause (~50+ failures) by switching test `data` from `:lib` to source. **BLOCKS US3's full-green goal** — US3 cannot reach SC-001 until this lands.
- **US3 (Phase 5)**: Depends on Phase 4 (re-triage after Fix B) and benefits from Phase 3 (`mock-interception` category). First-pass triage + 2 in-scope fixes already committed (`76549cf`).
- **Polish (Phase 6)**: Depends on Phase 4 + US3 (Phase 5) being complete.

### User Story Dependencies

- **US1 (P1)**: Foundation — can start immediately (Phase 1 is done); no dependencies on other stories.
- **US2 (P2)**: After US1. Independent of US3 (may run before or in parallel with US3's non-mock categories).
- **US3 (P2)**: After US1. The `mock-interception` category (T018) coordinates with US2.

### Within Each User Story

- Shared-infrastructure / wiring before validation.
- Per Constitution §IV, `bazel build` + `bazel test` on the affected target(s) accompanies each code task — not a separate task.
- Refactor before cross-mode equivalence check (US2).

### Parallel Opportunities

- Phase 1: DONE.
- Phase 2: T003 (macro + agent reference consumer) is sequential and establishes the pattern; T004–T008 are all `[P]` (distinct package BUILD files + shims) once T003 lands; T009 runs after all wiring.
- Phase 3: T011 & T012 are `[P]` (distinct files); T013 then T014 (validation) last.
- Phase 4 (module-identity): T025 (docstring) + T026 (six caller rewires) then T027 (verify) then T028 (conventions). The six caller rewires in T026 are `[P]` (distinct BUILD files) but grouped under one task for coherence.
- Phase 5 (US3): T015-redo drives T016 & T017 (`[P]`, distinct categories/files); T018–T020 category-driven.
- Phase 6: T022, T023, T024 are `[P]` (read-only checks).

---

## Parallel Example: User Story 1 (Phase 2)

```bash
# After T003 (vitest_test macro + agent reference consumer) establishes the pattern, launch the five common/js rewire tasks together:
Task: "Rewire common/js/logs:lib_test via vitest_test macro + delete run_vitest.mjs"
Task: "Rewire common/js/resolver:lib_test via vitest_test macro + delete run_vitest.mjs"
Task: "Rewire common/js/otel:lib_test via vitest_test macro + delete run_vitest.mjs"
Task: "Rewire common/js/grpc/otel:lib_test via vitest_test macro + delete run_vitest.mjs"
Task: "Rewire common/js/grpc/resolver:lib_test via vitest_test macro + delete run_vitest.mjs"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. ✅ Complete Phase 1 (canonical shim) — DONE.
2. Complete Phase 2 (US1 — `vitest_test` macro + wire all targets + validate honesty).
3. **STOP and VALIDATE**: US1 independently — Scenario 1 (inject-fail) + Scenario 2 (single canonical source + macro usage).
4. At this point Bazel reports real results across all `js_test` targets — the core trust is restored.

### Incremental Delivery

1. ✅ Phase 1 → canonical shim ready.
2. ✅ Phase 2 (US1) → honest runner via macro; validated independently (MVP).
3. ✅ Phase 3 (US2) → reliable mocks + conventions; validated both-mode equivalence.
4. Phase 4 (module-identity) → switch test `data` to source (Fix B); validate dual-instance failures gone.
5. Phase 5 (US3) → green baseline; validate full-green + zero unjustified skips.
6. Phase 6 → polish; run full quickstart suite.

### Parallel Team Strategy

With multiple developers (Phases 1–3 are complete):
1. Team completed Phase 1 + US1 (Phase 2) + US2 (Phase 3).
2. Phase 4 (module-identity, Fix B) is a single coherent infra change — done by one developer (T025→T028).
3. Once Phase 4 lands, US3 (Phase 5) re-triages (T015-redo) and fixes the remaining genuine failures; the `mock-interception` category (T018) reuses the Phase 3 DI approach.

---

## Notes

- `[P]` tasks = distinct files, no dependency on incomplete tasks.
- `[Story]` label maps each task to US1/US2/US3 for traceability.
- Per Constitution §IV, build + unit test (`bazel test` on the relevant `js_test`) is part of each code task — not listed separately.
- Per Constitution §V, each phase's "Required reading" block MUST be read before coding that phase. **Style-guide handling (§V no-citation-transitivity)**: `style/javascript.md` is currently only a baseline pointer (no local rules); the actual normative content is [Google TypeScript Style](https://google.github.io/styleguide/tsguide.html), which is therefore listed alongside it for JS/TS phases (Phase 1, Phase 3). **Phase 2 writes Starlark** (`.bzl` macro + `BUILD.bazel`), so it references `tools/dev/js/vite.bzl` (local Starlark idiom) instead of the JS guide.
- Per Constitution §VI, large tests are N/A (test infrastructure, not a service — see [plan.md](plan.md) Constitution Check).
- US3's exact failure inventory is discovered at execution time; tasks T016–T020 are category-driven and expand to cover whatever T015 surfaces. (Phase 2's verification run already established a 26-failure starting inventory in `//projects/game/agent:lib_test`.)
- The `vitest_test` macro is the single supported surface for `js_test` targets in this repo after Phase 2; raw `genrule`/`entry_point` test-runner boilerplate must not reappear in package BUILD files (enforced by T024).
- Commit after each task or logical group; stop at any checkpoint to validate the story independently.
