# Contract: Shared vitest Test-Runner Shim

**Feature**: [019-js-test-reliability](..) | Constitution §III (Interface-First Design)

This is the primary interface of the feature: the contract between **vitest** (test execution) and **Bazel** (pass/fail gating) implemented by the single shared shim `tools/dev/js/run_vitest.mjs`, referenced by every `js_test` target via `entry_point`.

## Interface

**Input** (argv, after `process.argv.slice(2)`): a vitest-style token list whose first meaningful token is a command (`run` | `watch`) followed by optional file/path filters (e.g. `["run", "src/"]`, `["run", "src/handler.test.ts"]`).

**Output**: process exit code.

| Condition | Exit code | Bazel result | Spec ref |
|-----------|-----------|--------------|----------|
| All test cases pass | `0` | PASSED | FR-002 |
| ≥1 test case fails an assertion | `1` | FAILED | FR-001 |
| Top-level/unhandled exception (e.g. import error) | `1` | FAILED | Edge Case |
| vitest `state.getCountOfFailedTests` is missing/non-callable (read error) | `1` | FAILED | FR-001 (fail-closed) |
| Empty suite (zero matching test files) | `0` | PASSED (vacuous) | Edge Case |

## Behavioral requirements (binding)

1. **Command/filter parsing (FR-003)**: strip `run`/`watch` tokens; pass the remainder as `cliFilters` to `startVitest`. Must NOT run the whole suite when a filter is supplied.
2. **Single run mode**: `watch:false` always (Bazel sandboxes are non-TTY).
3. **Await completion (FR-004)**: `await startVitest(...)` then `await vitest.close()` before reading the result — no teardown/exit race.
4. **Fail-closed failure read**: inability to obtain the failed-test count is a failure (exit `1`), never a silent pass.
5. **Shared & identical (FR-005)**: one file at `tools/dev/js/run_vitest.mjs`; all six `js_test` targets reference it. No per-package drift.
6. **API correctness (FR-006)**: uses vitest 3.x `startVitest(mode, cliFilters, options)` and `state.getCountOfFailedTests()` ([research.md](../research.md) §1).

## Reference implementation sketch (contract, not final code)

```js
import { startVitest } from "vitest/node";

async function main() {
  const filters = process.argv.slice(2).filter((a) => a !== "run" && a !== "watch");
  const vitest = await startVitest("test", filters, { watch: false });
  await vitest.close();
  const read = vitest?.state?.getCountOfFailedTests;
  const failed = typeof read === "function" ? read.call(vitest.state) : Number.POSITIVE_INFINITY;
  process.exit(failed > 0 ? 1 : 0);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
```

(Final code lives in tasks.md Phase 2; this sketch pins the contract.)

## Consumers

All six `js_test` targets: `//projects/game/agent:lib_test`, `//common/js/{logs,resolver,otel}:lib_test`, `//common/js/grpc/{otel,resolver}:lib_test`. Each sets `entry_point = "//tools/dev/js:run_vitest.mjs"`.
