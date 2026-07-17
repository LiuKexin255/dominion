# Data Model: JavaScript Test Reliability Under Bazel

**Feature**: [019-js-test-reliability](.)

This feature has **no persistent data store**; the "model" is the taxonomy of artifacts, targets, and classifications the feature operates on. Relationships are reference-only (file paths), used to scope the implementation.

## Entities

### TestRunnerShim
The Node.js entry script a `js_test` target invokes; translates vitest results into a process exit code.
- **identity (canonical source)**: `tools/dev/js/run_vitest.mjs` — the single source of truth (one hand-maintained file). Exported via `exports_files(["run_vitest.mjs"])` so the `vitest_test` macro can consume it as a `genrule` input.
- **delivery (revised post-execution)**: a cross-package `entry_point` label is infeasible (aspect_rules_js `copy_to_bin` + Node module-resolution constraints — see [plan.md — Architecture Revision](plan.md#architecture-revision-execution-discovery)). Delivery is the **`vitest_test` macro** (`tools/dev/js/vitest_test.bzl`) — the stable external surface. The macro internally `genrule`-copies the canonical source into each consuming package (so the copy lands next to that package's `node_modules/vitest`) and wires the `js_test`; the `genrule` is a macro-internal detail callers never see. Generated copies are mechanical outputs, not hand-maintained sources — drift is impossible.
- **consumes**: argv (`["run", "<filter>", …]`), the `vitest/node` programmatic API.
- **emits**: process exit code — `0` (all pass) / `1` (≥1 failure or read error) — per [contracts/run-vitest-shim.md](contracts/run-vitest-shim.md).
- **state machine**: `parse args` → `startVitest("test", filters, {watch:false})` → `await close()` → `read failedCount` → `exit(code)`.
- **replaces**: six per-package `run_vitest.mjs` SOURCE copies (deleted).

### JsTestTarget
A Bazel `js_test` target (`aspect_rules_js`) running a TS suite through the shim.
- **identity**: label, e.g. `//projects/game/agent:lib_test`.
- **definition**: declared via the `vitest_test` macro — `vitest_test(name = "lib_test", data = [...])`. The caller passes only the code under test + the test-file glob; the macro injects the `entry_point` (the per-package `genrule`-generated `…_run_vitest.mjs`), `args` (`["run","src/"]` default), and `:node_modules/vitest` into `data`. Each package runs `npm_link_all_packages(name = "node_modules")` so `vitest` is resolvable from the generated copy's location.
- **inventory (6)**: `//projects/game/agent:lib_test`, `//common/js/logs:lib_test`, `//common/js/resolver:lib_test`, `//common/js/otel:lib_test`, `//common/js/grpc/otel:lib_test`, `//common/js/grpc/resolver:lib_test`.
- **invariant**: every target's Bazel pass/fail == actual vitest outcome (no false greens) — SC-001/SC-002.

### MockUsage (audit unit)
One classification record per `*.test.ts` file that uses vitest mock primitives.
- **file**: repo-relative path.
- **primitive**: `vi.mock` (module-level) | `vi.fn` | `vi.spyOn` | `vi.hoisted`.
- **classification**: `reliable` (DI/local — both modes agree) | `fragile` (module-level mock of a dep consumed by pre-compiled `:lib`).
- **disposition**: `no-action` | `refactor-to-DI` | `justified-skip` (with tracking ref).
- **validation rule (FR-010)**: a module mock MUST be asserted-called, or the test would fail if the real dependency ran.

### PreExistingFailure (triage unit)
One record per test failure surfaced by the corrected runner.
- **target**: `js_test` label.
- **test**: file + test name.
- **rootCauseCategory**: `mock-contract` | `proto-path` | `mock-interception` | `production-bug` | `out-of-scope`.
- **resolution**: `fixed` (mock/path/prod fix) | `deferred` (justified skip + tracking ref).
- **invariant (FR-014)**: deferred count == 0 unless each has a tracked justification.

## Relationships

```text
JsTestTarget ──entry_point──▶ TestRunnerShim (1, shared)
JsTestTarget ──data─────────▶ *.test.ts files ──▶ MockUsage (0..n per file)
PreExistingFailure ──target──▶ JsTestTarget
PreExistingFailure ──refactors▶ MockUsage (when rootCauseCategory = mock-interception)
```

## Validation rules (from spec FRs)

- FR-001/FR-002: exit code reflects real outcome → enforced by shim contract.
- FR-007: every mock file classified (audit completeness).
- FR-010: every module mock verifiably active.
- FR-014: zero unjustified deferrals.
