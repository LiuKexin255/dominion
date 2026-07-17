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

> **Revised post-execution (2026-07-17).** The original §3 (below the line) asserted a
> cross-package `entry_point` label works. Execution of Phase 2 **empirically disproved** both
> supporting claims. This section now records the verified mechanism and decision; the
> superseded text is retained at the end for provenance. Full evidence is in
> [plan.md — Architecture Revision](plan.md#architecture-revision-execution-discovery).

### Verified findings (each reproduced with `--discard_analysis_cache`)

1. **Cross-package file-label `entry_point` FAILS at analysis.** `entry_point = "//tools/dev/js:run_vitest.mjs"` on `//projects/game/agent:lib_test` fails with `Expected to find source file run_vitest.mjs in '//projects/game/agent', but instead it is in '//tools/dev/js'` — the `aspect_rules_js` `copy_to_bin` constraint (source: `copy_js_file_to_bin_action` in [js/private/js_helpers.bzl](https://github.com/aspect-build/rules_js/blob/main/js/private/js_helpers.bzl)). `entry_point` does accept a `Label` ([js/private/js_binary.bzl](https://github.com/aspect-build/rules_js/blob/main/js/private/js_binary.bzl)), but a *source file* must be in the same package or wrapped.

2. **A `js_library` wrapper passes analysis but FAILS at runtime.** Wrapping the shim and pointing `entry_point` at the wrapper lets analysis succeed, but the test then fails: `Error [ERR_MODULE_NOT_FOUND]: Cannot find package 'vitest' imported from …/run_vitest.mjs`. Node resolves `import { startVitest } from "vitest/node"` by walking up from the **entry_point file's location** (`tools/dev/js/`), not the consuming target's package. `vitest` is only resolvable where an `npm_link_all_packages(name = "node_modules")` link root exists ([aspect_rules_js — docs/pnpm.md](https://github.com/aspect-build/rules_js/blob/main/docs/pnpm.md): "dependencies from the current directory's BUILD file and above").

3. **`tools/dev/js` CANNOT host an npm-link root.** `npm_link_all_packages()` there fails: it "may only be called in bazel packages that correspond to the pnpm root package or pnpm workspace projects" — `tools/dev/js` is a Starlark tooling directory, not a pnpm workspace package.

4. **Runfiles layout confirms the model.** In a working per-package target, `_main/node_modules` holds only `.aspect_rules_js` (the store); `vitest` is reachable only via the per-package symlink `…/<pkg>/node_modules/vitest → …/node_modules/.aspect_rules_js/vitest@3.2.6_…/node_modules/vitest`. No top-level `vitest` is resolvable from `tools/dev/js/`.

### Decision (revised)

Keep ONE canonical hardened shim at `tools/dev/js/run_vitest.mjs` (the §1 logic), exported via `exports_files(["run_vitest.mjs"])` so it is consumable as a `genrule` input. Deliver it to each `js_test` package **through a single macro `vitest_test`** (`tools/dev/js/vitest_test.bzl`), the stable external surface every package uses. The macro internally `genrule`-copies the canonical source into the consuming package and wires the `js_test` (`entry_point` → the local generated copy; `:node_modules/vitest` auto-injected into `data`). The `genrule` is an **internal implementation detail of the macro** — callers pass only `name`, `data` (the lib under test + test-file glob), and optional `args`; they never see the copy, the `entry_point`, or the `vitest` dependency. Because the copy lands in the consuming package's runfiles tree, Node resolves `vitest` from that package's `node_modules`. The six per-package SOURCE shims are deleted; the macro-generated copies are mechanical outputs of the single canonical source, so **drift is impossible**. This satisfies FR-005 ("a single, shared, well-documented approach … no package's test runner silently drifts") with a true single source of truth for BOTH shim content and wiring (Constitution §II).

Why a macro (not a custom rule, not bare genrules): it expands to the proven `genrule + js_test` native rules with zero new provider/runfiles code, while giving callers the same experience as a custom rule; bare `genrule`s repeated in six BUILD files would re-expose the details this design exists to hide and cannot be statically enforced. Prerequisite (documented on the macro): the calling package must already run `npm_link_all_packages(name = "node_modules")` so `:node_modules/vitest` resolves — all six target packages already do.

**Verification**: the macro's underlying mechanism (a `genrule` copy of the canonical shim → local `entry_point` + `:node_modules/vitest`) was run end-to-end — `bazel test //projects/game/agent:lib_test` executed 276 tests in 6.24s and honestly reported `26 failed | 250 passed` (Exit 1); the 26 are the US3 backlog, not a regression.

### Alternatives considered (revised)

- **Per-package hand-maintained identical source files** (FR-005's literal "identically-structured per-package script" wording): works, but relies on *convention* to keep six copies identical — the exact failure mode that left 5/6 buggy. Rejected in favour of mechanical generation inside the macro, which achieves FR-005 without convention.
- **Bare `genrule` repeated per BUILD file** (no macro): same mechanism, but re-exposes `entry_point`/`outs`/`vitest`-injection at every call site, cannot be enforced, needs per-site comments. Rejected — the macro fixes these details in one place.
- **A custom Bazel rule** (not a macro): re-implements provider/runfiles plumbing for "copy a file + forward `js_test`". Over-engineering; the macro expands to proven native rules with identical caller experience. Rejected.
- **`DirectoryPathInfo` entry_point** (the canonical cross-package pattern in [`npm/private/npm_import.bzl`](https://github.com/aspect-build/rules_js/blob/main/npm/private/npm_import.bzl)): that pattern targets npm *bin* files whose store directory carries `node_modules`; it does not solve `vitest` resolution for a hand-written shim in a non-workspace directory. Rejected.
- **Make `tools/dev/js` a pnpm workspace package**: rejected as over-engineering for a 30-line shim.
- **`no_copy_to_bin` exception**: addresses the analysis constraint only, not the runtime resolution blocker. Insufficient alone.

---

### Superseded original §3 (retained for provenance — DO NOT rely on these claims)

> ~~`js_test`'s `entry_point` attribute is a **`Label`** … Therefore a cross-package label `entry_point = "//tools/dev/js:run_vitest.mjs"` is supported … Each `js_test` still supplies its own `:node_modules/vitest` so `import { startVitest } from "vitest/node"` resolves.~~
>
> ~~Decision: Create ONE canonical `tools/dev/js/run_vitest.mjs` … repoint all six `js_test` targets' `entry_point` to `//tools/dev/js:run_vitest.mjs`.~~

These two claims are **false** (verified findings 1 & 2 above). The single-source-of-truth *intent* is preserved by the `vitest_test`-macro decision; only the *delivery mechanism* changed.

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
