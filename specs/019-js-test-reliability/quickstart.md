# Quickstart: JavaScript Test Reliability Under Bazel

**Feature**: [019-js-test-reliability](.)

Runnable validation scenarios proving the feature works end-to-end. These double as the manual acceptance steps for SC-001…SC-005. Contract details: [contracts/run-vitest-shim.md](contracts/run-vitest-shim.md); entity scope: [data-model.md](data-model.md).

## Prerequisites

- A `bazel`-configured checkout of this repo (see [AGENTS.md](../../AGENTS.md)).
- No extra services — this feature is pure test infrastructure.

## Scenario 1 — Runner is honest (inject a failing assertion) [SC-002, FR-001]

Proves the shim exits non-zero on failure, on multiple packages.

1. Temporarily add a failing assertion to a test in a `js_test` target, e.g. in [projects/game/agent/src/context-middleware.test.ts](../../projects/game/agent/src/context-middleware.test.ts) add `it("forced fail", () => { expect(true).toBe(false); });`.
2. `bazel test //projects/game/agent:lib_test`
3. **Expected**: Bazel reports **FAILED** (non-zero exit), test log shows `forced fail`.
4. Repeat for ≥2 other targets (e.g. `//common/js/logs:lib_test`, `//common/js/resolver:lib_test`).
5. Revert the injected assertion; `bazel test //projects/game/agent:lib_test` → **PASSED**.
6. Empty-suite check: temporarily point a target's filter at a non-matching path → **PASSED** (vacuous), not a crash.

## Scenario 2 — Shared shim is the single source of truth [FR-005]

1. Confirm exactly one canonical SOURCE shim exists: `ls tools/dev/js/run_vitest.mjs`. The six per-package `run_vitest.mjs` SOURCE files are gone — each package obtains the shim through the `vitest_test` macro, which internally `genrule`-copies the canonical source into the package (see [plan.md — Architecture Revision](plan.md#architecture-revision-execution-discovery) for why a cross-package `entry_point` is infeasible).
2. Each of the six `BUILD.bazel` `js_test` packages calls the macro: `load("//tools/dev/js:vitest_test.bzl", "vitest_test")` then `vitest_test(name = "lib_test", data = [...])`. No package writes a raw `genrule`/`entry_point`/`:node_modules/vitest` itself — those are macro internals.
3. `bazel test //common/js/...` → all `lib_test` targets run via a macro-generated copy of the shared canonical shim.

## Scenario 3 — Mock-based tests agree in both modes [SC-003, FR-007/FR-010]

For each formerly-fragile file (`reporter.test.ts`, `llm-tools.test.ts`, `prompt-client.test.ts`) and the audit set:

1. `bazel run //projects/game/agent:vitest -- run src/llm-tools.test.ts` (direct vitest CLI) — record pass/fail.
2. `bazel test //projects/game/agent:lib_test` (Bazel, pre-compiled `:lib`) — record pass/fail.
3. **Expected**: identical results; the mocked code path is exercised (assert mock was called — FR-010).

## Scenario 4 — Full green baseline [SC-001, FR-014]

1. `bazel test //projects/game/agent:lib_test //common/js/logs:lib_test //common/js/resolver:lib_test //common/js/otel:lib_test //common/js/grpc/otel:lib_test //common/js/grpc/resolver:lib_test`
2. **Expected**: every target **PASSED**, zero failing tests.
3. `grep -rn 'it.skip\|describe.skip\|it.only' projects/game/agent/src common/js --include='*.test.ts'` — any skip MUST have an inline justification referencing a concurrent effort (SC-004).

## Scenario 5 — Conventions are discoverable [SC-005, FR-009]

1. Open the "Testing" section of [style/javascript.md](../../style/javascript.md) — it documents reliable (DI) vs fragile (module-level `vi.mock` on pre-compiled deps) patterns.
2. A developer following the guidance writes a mock-based test that passes identically under both modes (Scenario 3).
