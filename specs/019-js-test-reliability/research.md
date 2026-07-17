# Research: JavaScript Test Reliability Under Bazel

**Feature**: [019-js-test-reliability](.) | **Date**: 2026-07-17

Resolves every NEEDS CLARIFICATION and "is there a better approach" question raised by [spec.md](spec.md). Each decision cites its source.

## 1. Does the agent's fixed `run_vitest.mjs` use the correct vitest API? Is there a better approach? (FR-006, US1)

### Findings

The agent shim ([projects/game/agent/run_vitest.mjs](../../projects/game/agent/run_vitest.mjs)) calls:

```js
const vitest = await startVitest("test", filters, { watch: false });
const failedTests = typeof vitest?.state?.getCountOfFailedTests === "function"
  ? vitest.state.getCountOfFailedTests() : 0;
if (failedTests > 0) process.exit(1);
```

- **`startVitest` signature (vitest 3.x)**: `startVitest(mode, cliFilters, options, viteOverrides, vitestOptions)` — confirmed by the vitest advanced-guide example `startVitest('test', [], {}, {}, {})` ([vitest docs — guide/advanced/tests](https://vitest.dev/guide/advanced/tests)). The first arg is the test `mode` (`"test"`). *Note: vitest 4.x drops the `mode` parameter; the repo pins `^3.2.6` ([pnpm-workspace.yaml](../../pnpm-workspace.yaml) L38), so the 3.x `mode`-bearing form is correct.*
- **`getCountOfFailedTests()` exists** on `vitest.state` in vitest 3.x — defined in `packages/vitest/src/node/state.ts` ([vitest source](https://github.com/vitest-dev/vitest/blob/main/packages/vitest/src/node/state.ts)): returns `Array.from(this.idMap.values()).filter(t => t.result?.state === 'fail').length`. It is also surfaced over RPC (`packages/vitest/src/node/pools/rpc.ts`, `getCountOfFailedTests()`).
- **Why `startVitest` alone is insufficient**: in a programmatic (non-CLI) context `startVitest` does NOT set a non-zero `process.exitCode` on ordinary assertion failures — the original `if (!result)` guard was a no-op because `result` is always a truthy `Vitest` instance (this is exactly the bug documented in the agent shim's own comments and in [spec.md](spec.md) US1). The explicit `getCountOfFailedTests()` check is therefore necessary.

### Decision

Adopt the agent's proven approach, **hardened** to be fail-closed:

1. Call `await startVitest("test", filters, { watch: false })`.
2. **Fail-closed failure read**: if `vitest.state.getCountOfFailedTests` is not a callable function, treat it as a failure (exit non-zero) rather than defaulting to `0`. The current agent shim defaults to `0` on a missing method — a latent silent-pass risk. Per spec FR-001, the runner MUST fail on errors.
3. **Explicit `await vitest.close()`** before evaluating the result, to satisfy FR-004 (await asynchronous teardown). The vitest docs note `startVitest` auto-closes when `watch:false`, but an explicit close removes any teardown/exit race ([spec.md](spec.md) Edge Case: "asynchronous teardown that races with the process exit").
4. Exit `process.exit(failedTests > 0 ? 1 : 0)`.

### Alternatives considered

- **`createVitest('test', …)` + `await vitest.start(filters)`**: the docs state `start()` "will set process.exitCode to 1 if tests failed, and won't close the process automatically" ([vitest — guide/advanced/tests](https://vitest.dev/guide/advanced/tests)). This relies on vitest's own exit-code side-effect. **Rejected**: lower-level API requiring manual `close()`/error handling for `FilesNotFoundError`; the `getCountOfFailedTests()` approach is more explicit and already proven in-repo. (Documented here so the choice is auditable per FR-006.)
- **Shell out to the `vitest` CLI binary**: rejected — `vitest.mjs` is a `bin` file not exposed by `npm_link_all_packages`, which is why the programmatic shim exists (see the comment above every `js_test` block in each `BUILD.bazel`).

## 2. Why do module-level `vi.mock()` calls fail under Bazel `js_test` but pass under the vitest CLI? (US2)

### Findings (root cause)

The vitest mocking guide documents two hard limits of `vi.mock` ([vitest — API/vi.md#vi-mock](https://vitest.dev/api/vi.html#vi-mock); [vitest — guide/mocking/modules](https://vitest.dev/guide/mocking.html)):

1. **"`vi.mock` exclusively works for modules imported with the `import` keyword, not `require`."** Pre-compiled `:lib` artifacts (swc CommonJS/ESM output) may emit `require()` for interop, which `vi.mock` cannot intercept.
2. **vitest transforms static `import` into a dynamic `__handle_mock__` wrapper** so it can register the mock before module load — but this transform **only applies to files processed by vitest's Vite pipeline**. The `js_test` `data` includes the pre-compiled `:lib` (`ts_project` + `swc` output) as-is; its already-emitted `import "langchain"` / `require("langchain")` is resolved directly from runfiles `node_modules`, bypassing the mock registry.

This matches the in-repo diagnosis in [projects/game/agent/src/build-tools.test.ts](../../projects/game/agent/src/build-tools.test.ts) L4–9: *"`vi.mock("langchain")` — a mock that does not reliably intercept the pre-compiled `:lib`'s langchain import under Bazel"*.

### Audit of every module-level `vi.mock()` file

| File | Mocked module(s) | Verdict | Action |
|------|------------------|---------|--------|
| [common/js/logs/src/reporter.test.ts](../../common/js/logs/src/reporter.test.ts) L112 | `@opentelemetry/api-logs` | Fragile | Refactor to inject the logger dependency (constructor/param seam). |
| [projects/game/agent/src/llm-tools.test.ts](../../projects/game/agent/src/llm-tools.test.ts) L21 | `langchain` (spy on `createAgent`) | Fragile | Refactor to inject the `createAgent` factory (DI), per `build-tools.test.ts` precedent. |
| [projects/game/agent/src/prompt-client.test.ts](../../projects/game/agent/src/prompt-client.test.ts) L27,58,63,67 | `@grpc/grpc-js`, `node:fs`, `@grpc/proto-loader`, `@dominion/common-js-grpc-resolver` | Fragile | The `PromptClient` ctor already accepts an optional `client` (DI seam, L84–91). Restrict module mocks to the channel-construction/warmup tests or convert those to a factory seam; verify each mock is asserted-called (FR-010). |

All other ~27 test files use only `vi.fn()` / `vi.spyOn()` passed as constructor arguments or local factories (dependency injection) — **reliable under both modes** (e.g. [handler.test.ts](../../projects/game/agent/src/handler.test.ts), [grpc-js-resolver.test.ts](../../common/js/grpc/resolver/src/grpc-js-resolver.test.ts)). No action needed beyond codifying the pattern.

### Decision

- Reliable pattern = **dependency injection / test-double seam** (the production code accepts the collaborator as a parameter; the test supplies a `vi.fn()`). This works identically under the vitest CLI and Bazel because no module interception is involved.
- Fragile pattern = **module-level `vi.mock("external-dep")` for dependencies consumed transitively by a pre-compiled `:lib`**. Avoid; if unavoidable, the test MUST assert the mock was called (FR-010) and record a justification (FR-008b).
- Codify in a new "Testing" section of [style/javascript.md](../../style/javascript.md) (FR-009).

## 3. Can the six `run_vitest.mjs` copies become one shared shim? (FR-005)

### Findings

- `js_test`'s `entry_point` attribute is a **`Label`** ("This must be a target that provides a single file…") per the aspect_rules_js `js_binary`/`js_test` docs ([aspect_rules_js — docs/js_binary.md](https://github.com/aspect-build/rules_js/blob/main/docs/js_binary.md); BCR mirror [registry.bazel.build/docs/aspect_rules_js](https://registry.bazel.build/docs/aspect_rules_js)). The implementation `_create_launcher` reads `ctx.files.entry_point[0].short_path` and adds `entry_point` to the runfiles `data_files` ([js/private/js_binary.bzl](https://github.com/aspect-build/rules_js/blob/main/js/private/js_binary.bzl)).
- Therefore a cross-package label `entry_point = "//tools/dev/js:run_vitest.mjs"` is supported: the file is exported via `exports_files` and auto-included in each target's runfiles. Each `js_test` still supplies its own `:node_modules/vitest` so `import { startVitest } from "vitest/node"` resolves.
- `tools/dev/js/` already holds shared JS build helpers ([tools/dev/js/BUILD.bazel](../../tools/dev/js/BUILD.bazel): `vite.bzl`, `ts_proto_library.bzl`, `pnpm.sh`) — the natural home.

### Decision

Create ONE canonical `tools/dev/js/run_vitest.mjs` (the hardened shim from §1), `exports_files(["run_vitest.mjs"])`, and repoint all six `js_test` targets' `entry_point` to `//tools/dev/js:run_vitest.mjs`. Delete the six per-package copies. This satisfies FR-005 ("a single, shared, well-documented approach") and Constitution §II (refactor over patch).

### Alternatives considered

- **Six byte-identical copies + a "keep in sync" comment**: rejected — drift is exactly the current problem (5/6 are still broken); a single source of truth removes the failure mode.

## 4. How are `.proto` fixture paths resolved under the Bazel sandbox? (FR-013)

### Findings

[common/js/grpc/otel/src/index.test.ts](../../common/js/grpc/otel/src/index.test.ts) L36 resolves `path.join(__dirname, "test_service.proto")`. The proto is declared in the `js_test` `data` ([common/js/grpc/otel/BUILD.bazel](../../common/js/grpc/otel/BUILD.bazel) L63 `src/test_service.proto`). Under runfiles, `__dirname` of the compiled/transpiled test points into the runfiles tree where the proto is colocated. The same `__dirname`-relative pattern is used in production code ([projects/game/agent/src/prompt-client.ts](../../projects/game/agent/src/prompt-client.ts), `server.ts`) which already runs under Bazel.

### Decision

Treat proto-path failures (if any surface after the runner fix) as runfiles-resolution bugs: prefer `__dirname`-relative (works in both modes) over `process.cwd()`/absolute source paths (FR-013). Validate during US3 triage; no pre-emptive change unless a test actually fails.

## 5. What is the US3 failure-fix workflow, and what counts as a deferral?

### Decision (process)

Once the shared shim ships and reports honestly, run `bazel test` on every `js_test` target. For each failure:

1. Classify root cause: (a) mock/callback-contract mismatch → fix the mock to match the real API (FR-012), (b) proto/resource path → runfiles-aware resolution (FR-013), (c) module-mock interception gap → §2 refactor, (d) production-code bug → fix production, do NOT weaken assertions (spec Edge Case), (e) genuinely out-of-scope → `it.skip`/`describe.skip` with a recorded justification referencing the concurrent effort (FR-014).
2. Zero silent deferrals: every skipped test has a tracked justification (SC-004).

The exact failure inventory is discovered at execution time (the spec deliberately does not enumerate it); tasks.md (Phase 2) will group fixes by the categories above.
