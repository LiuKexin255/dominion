# Implementation Plan: JavaScript Test Reliability Under Bazel

**Branch**: `019-js-test-reliability` | **Date**: 2026-07-17 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from [specs/019-js-test-reliability/spec.md](spec.md)

## Summary

Restore trustworthy pass/fail signaling from Bazel `js_test` targets across the repository. Today six near-identical `run_vitest.mjs` shims exist; five of them (all `common/js/*`) still carry the original bug — `const result = await startVitest(args); if (!result) process.exit(1)` — which never exits non-zero on assertion failures because `startVitest` always returns a truthy `Vitest` instance. Only `projects/game/agent/run_vitest.mjs` has been fixed so far. The plan consolidates the six drifting copies into a single hardened shared shim (`tools/dev/js/run_vitest.mjs`), referenced by every `js_test` target via a cross-package `entry_point` label (FR-005). It then hardens the three test files that use fragile module-level `vi.mock()` — whose root cause is that `vi.mock` only intercepts modules flowing through vitest's transform pipeline, not the pre-compiled `:lib` artifact (see [research.md](research.md) §2) — by refactoring them to dependency-injection seams, and fixes the pre-existing test failures that the corrected runner surfaces. Reliable mocking conventions are codified in a new "Testing" section of [style/javascript.md](../../style/javascript.md).

Research decisions and alternatives are recorded in [research.md](research.md); the shim's exit-code contract is specified in [contracts/run-vitest-shim.md](contracts/run-vitest-shim.md); the entity taxonomy in [data-model.md](data-model.md); end-to-end validation in [quickstart.md](quickstart.md).

## Technical Context

**Language/Version**: TypeScript, transpiled to JavaScript via `@aspect_rules_swc` (`swc`) inside `ts_project` targets; runtime is Node.js selected by the Bazel node toolchain.

**Primary Dependencies**: vitest `^3.2.6` (catalog: [pnpm-workspace.yaml](../../pnpm-workspace.yaml) L38), `@aspect_rules_js` (`js_test` / `js_library` / `ts_project`), `@grpc/grpc-js` + `@grpc/proto-loader`, langchain, OpenTelemetry SDKs.

**Storage**: N/A (test infrastructure; no persistent state).

**Testing**: vitest executed through Bazel `js_test` targets — this feature IS the test infrastructure under repair. Per Constitution §IV, build + unit (`bazel test` on each `js_test`) are the per-change gate.

**Target Platform**: Linux; Bazel sandbox + runfiles execution model.

**Project Type**: build/test tooling — not a service application. Consequently Constitution §VI (service large-test acceptance) does not apply (see Constitution Check).

**Performance Goals**: N/A — correctness-focused (honest pass/fail signal).

**Constraints**: must NOT upgrade vitest unless an upgrade is itself required to fix a failure (spec Assumptions); must retain the `js_test` / `aspect_rules_js` execution mechanism; results MUST agree between the `vitest` CLI and Bazel `js_test` for every mock-based test (SC-003).

**Scale/Scope**: 6 `js_test` targets (`//projects/game/agent:lib_test`, `//common/js/{logs,resolver,otel}:lib_test`, `//common/js/grpc/{otel,resolver}:lib_test`); ~30 `*.test.ts` files; 3 files use fragile module-level `vi.mock()`.

## Constitution Check

*GATE: evaluated pre-research and re-checked post-design below.*

| Principle | Status | Evidence / Plan |
|-----------|--------|-----------------|
| **I. Citation & Provenance** | ✅ PASS | Spec cites vitest `startVitest` API, vitest mocking guide, aspect `js_test` rule, Bazel runfiles ([spec.md](spec.md) §References). This plan and [research.md](research.md) cite exact source files (vitest `state.ts`, `core.ts`; aspect `js_binary.bzl`) with URLs. |
| **II. Refactoring Over Patching** | ✅ PASS | Consolidate 6 drifting shim copies → 1 shared canonical shim (refactor, not patch). Refactor fragile `vi.mock` tests → dependency-injection seams (the `build-tools.test.ts` precedent). The architecture (single source of truth for the runner) changes WITH the fix. |
| **III. Interface-First Design** | ✅ PASS | The primary interface — the shim's process-exit-code contract (vitest result → exit code) — is specified BEFORE implementation in [contracts/run-vitest-shim.md](contracts/run-vitest-shim.md). The conventions doc is the guidance interface for developers. |
| **IV. Test Granularity & Cadence** | ✅ PASS | `bazel build` + `bazel test` on each affected `js_test` target is the per-change gate, embedded in dev tasks (not separate tasks). No large test task needed (see §VI). |
| **V. Read Before Code** | ✅ PASS (pending tasks.md) | tasks.md (Phase 2) MUST list per-phase docs: vitest API/mocking docs, aspect `js_test` rule, `style/javascript.md` (incl. the new "Testing" section). |
| **VI. Service Large-Test Acceptance** | ⬜ N/A | This feature is test/build **infrastructure**, not a service application. No deployed service, no cross-service communication to validate. Large tests (`testplan`) are out of scope (spec Assumptions). Justified, no violation. |

**GATE RESULT: PASS** — no unresolved violations. §VI is N/A (justified by project type), recorded here.

## Project Structure

### Documentation (this feature)

```text
specs/019-js-test-reliability/
├── plan.md                       # This file
├── research.md                   # Phase 0: API verification, mock root-cause, alternatives
├── data-model.md                 # Phase 1: entity taxonomy (shim, target, audit, failure)
├── quickstart.md                 # Phase 1: end-to-end validation scenarios
├── contracts/
│   └── run-vitest-shim.md        # Phase 1: process exit-code contract
└── tasks.md                      # Phase 2 output (/speckit.tasks — not this command)
```

### Source Code (repository root)

This feature does NOT add a new source tree; it modifies existing test-infrastructure files. Touched locations:

```text
tools/dev/js/
├── run_vitest.mjs                # NEW: single canonical shared shim (replaces 6 copies)
└── BUILD.bazel                   # exports_files(["run_vitest.mjs"])

projects/game/agent/
├── BUILD.bazel                   # js_test entry_point → shared shim label; drop local shim dep
├── run_vitest.mjs                # DELETED (replaced by shared)
└── src/*.test.ts                 # fragile-mock refactors (llm-tools, prompt-client) + failure fixes

common/js/logs/        { BUILD.bazel, run_vitest.mjs DELETED, src/reporter.test.ts }
common/js/resolver/    { BUILD.bazel, run_vitest.mjs DELETED }
common/js/otel/        { BUILD.bazel, run_vitest.mjs DELETED }
common/js/grpc/otel/   { BUILD.bazel, run_vitest.mjs DELETED, src/index.test.ts (proto path) }
common/js/grpc/resolver/ { BUILD.bazel, run_vitest.mjs DELETED }

style/
└── javascript.md                 # NEW "Testing" section (FR-009): reliable vs fragile mocking + js_test conventions
```

**Structure Decision**: No new package layout. The shared shim lives in the existing `tools/dev/js/` directory (already home to shared JS build helpers `vite.bzl`, `ts_proto_library.bzl` — see [tools/dev/js/BUILD.bazel](../../tools/dev/js/BUILD.bazel)). Each `js_test` target switches its `entry_point` to the cross-package label `//tools/dev/js:run_vitest.mjs`, which aspect_rules_js supports because `entry_point` is a `Label` whose file is auto-included in runfiles ([research.md](research.md) §3).

**Testing-conventions section**: rather than a standalone `style/js-testing.md`, FR-009 is satisfied by appending a new `## 测试 (Testing)` section to the existing [style/javascript.md](../../style/javascript.md). The section covers: the `js_test` execution model (pre-compiled `:lib` vs vitest-CLI transpile-on-the-fly), the shared `run_vitest.mjs` entry-point contract (see [contracts/run-vitest-shim.md](contracts/run-vitest-shim.md)), the reliable pattern (dependency injection / `vi.fn()` test-double seam) vs the fragile pattern (module-level `vi.mock("external-dep")` consumed transitively by a pre-compiled `:lib`) with the root cause from [research.md](research.md) §2, and a "verify the mock is active" rule (FR-010). Co-locating with the language style guide keeps all JS/TS conventions in one discoverable file.

## Complexity Tracking

No Constitution violations require justification. §VI (large-test acceptance) is N/A by project type (test infrastructure, not a service) — documented in the Constitution Check table above, not a complexity exception.
