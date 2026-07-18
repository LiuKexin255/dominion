# Implementation Plan: JavaScript Test Reliability Under Bazel

**Branch**: `019-js-test-reliability` | **Date**: 2026-07-17 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from [specs/019-js-test-reliability/spec.md](spec.md)

> **Revision 2026-07-17 (post-execution correction).** The original plan specified a
> single shared shim referenced cross-package via `entry_point = "//tools/dev/js:run_vitest.mjs"`.
> Execution of Phase 2 (US1) **empirically disproved** that approach: it fails at Bazel
> analysis (`copy_to_bin` requires source files in the consuming package) AND at runtime
> (Node resolves `vitest` from the entry_point's location, but `tools/dev/js` is not a pnpm
> workspace package so it cannot host `node_modules/vitest`). See the
> [Architecture Revision](#architecture-revision-execution-discovery) section for the verified
> evidence and the corrected design. Phase 1 (the canonical shim + `exports_files`) is
> **preserved unchanged**; only Phase 2's *consumption mechanism* changes. Affected sibling
> docs — [research.md](research.md) §3, [quickstart.md](quickstart.md) Scenario 2,
> [data-model.md](data-model.md), and [tasks.md](tasks.md) Phase 2 — carry the same stale
> assumption and **MUST be updated to match** before execution resumes (tracked below).

## Summary

Restore trustworthy pass/fail signaling from Bazel `js_test` targets across the repository. Today six near-identical `run_vitest.mjs` shims exist; five of them (all `common/js/*`) still carry the original bug — `const result = await startVitest(args); if (!result) process.exit(1)` — which never exits non-zero on assertion failures because `startVitest` always returns a truthy `Vitest` instance. Only `projects/game/agent/run_vitest.mjs` has been fixed so far.

The canonical hardened shim logic lives in ONE file — `tools/dev/js/run_vitest.mjs` (created in Phase 1, implementing the exit-code contract in [contracts/run-vitest-shim.md](contracts/run-vitest-shim.md)). Because `aspect_rules_js` cannot reference that file cross-package as a `js_test` `entry_point` (verified — see [Architecture Revision](#architecture-revision-execution-discovery)), each `js_test` package instead gets a local copy of the canonical shim **through a single macro, `vitest_test`** (the stable external interface), which internally generates that copy so it lands next to the package's own `node_modules/vitest` for correct Node module resolution at runtime. The `genrule` that performs the copy is an **internal implementation detail of the macro** — callers never see it. The six per-package SOURCE shims are deleted; the macro-generated copies are mechanical outputs of the single canonical source, so **drift is impossible**. This satisfies FR-005 ("a single, shared, well-documented approach … no package's test runner silently drifts") with a true single source of truth for BOTH the shim content and the wiring.

The plan then hardens the three test files that use fragile module-level `vi.mock()` — whose root cause is that `vi.mock` only intercepts modules flowing through vitest's transform pipeline, not the pre-compiled `:lib` artifact (see [research.md](research.md) §2) — by refactoring them to dependency-injection seams, and fixes the pre-existing test failures that the corrected runner surfaces. Reliable mocking conventions are codified in a new "Testing" section of [style/javascript.md](../../style/javascript.md).

Research decisions and alternatives are recorded in [research.md](research.md); the shim's exit-code contract is specified in [contracts/run-vitest-shim.md](contracts/run-vitest-shim.md); the entity taxonomy in [data-model.md](data-model.md); end-to-end validation in [quickstart.md](quickstart.md).

## Technical Context

**Language/Version**: TypeScript, transpiled to JavaScript via `@aspect_rules_swc` (`swc`) inside `ts_project` targets; runtime is Node.js selected by the Bazel node toolchain.

**Primary Dependencies**: vitest `^3.2.6` (catalog: [pnpm-workspace.yaml](../../pnpm-workspace.yaml) L38), `@aspect_rules_js` (`js_test` / `js_library` / `ts_project` / `npm_link_all_packages`), `@grpc/grpc-js` + `@grpc/proto-loader`, langchain, OpenTelemetry SDKs.

**Storage**: N/A (test infrastructure; no persistent state).

**Testing**: vitest executed through Bazel `js_test` targets — this feature IS the test infrastructure under repair. Per Constitution §IV, build + unit (`bazel test` on each `js_test`) are the per-change gate.

**Target Platform**: Linux; Bazel sandbox + runfiles execution model.

**Project Type**: build/test tooling — not a service application. Consequently Constitution §VI (service large-test acceptance) does not apply (see Constitution Check).

**Performance Goals**: N/A — correctness-focused (honest pass/fail signal).

**Constraints**: must NOT upgrade vitest unless an upgrade is itself required to fix a failure (spec Assumptions); must retain the `js_test` / `aspect_rules_js` execution mechanism; results MUST agree between the `vitest` CLI and Bazel `js_test` for every mock-based test (SC-003); the shared shim's canonical source MUST remain a single file (Constitution §II — single source of truth) while satisfying aspect_rules_js's package-local execution model.

**Scale/Scope**: 6 `js_test` targets (`//projects/game/agent:lib_test`, `//common/js/{logs,resolver,otel}:lib_test`, `//common/js/grpc/{otel,resolver}:lib_test`); ~30 `*.test.ts` files; 3 files use fragile module-level `vi.mock()`.

## Constitution Check

*GATE: evaluated pre-research, re-checked post-design, and re-checked post-execution-revision below.*

| Principle | Status | Evidence / Plan |
|-----------|--------|-----------------|
| **I. Citation & Provenance** | ✅ PASS | Spec cites vitest `startVitest` API, vitest mocking guide, aspect `js_test` rule, Bazel runfiles ([spec.md](spec.md) §References). This plan and [research.md](research.md) cite exact source files (vitest `state.ts`, `core.ts`; aspect `js_binary.bzl`, `npm_import.bzl`) with URLs. The [Architecture Revision](#architecture-revision-execution-discovery) cites the `copy_to_bin` error, the pnpm-workspace constraint, and the end-to-end verification run of the `vitest_test` macro's underlying mechanism as provenance. |
| **II. Refactoring Over Patching** | ✅ PASS | The runner logic has ONE canonical source (`tools/dev/js/run_vitest.mjs`); each package consumes a **generated** copy, so the six hand-maintained drifting copies are replaced by a mechanical single-source-of-truth pipeline (refactor, not patch). The drift failure mode (5/6 buggy) is removed mechanically, not by convention. Fragile `vi.mock` tests → dependency-injection seams (the `build-tools.test.ts` precedent). |
| **III. Interface-First Design** | ✅ PASS | The primary interface — the shim's process-exit-code contract (vitest result → exit code) — is specified BEFORE implementation in [contracts/run-vitest-shim.md](contracts/run-vitest-shim.md). The conventions doc is the guidance interface for developers. (The contract is unchanged by the execution revision; only the shim's *delivery mechanism* changed.) |
| **IV. Test Granularity & Cadence** | ✅ PASS | `bazel build` + `bazel test` on each affected `js_test` target is the per-change gate, embedded in dev tasks (not separate tasks). The `vitest_test` macro's underlying mechanism was itself validated by `bazel test` (276 tests collected, 26 pre-existing failures surfaced honestly — see [Architecture Revision](#architecture-revision-execution-discovery)). No large test task needed (see §VI). |
| **V. Read Before Code** | ✅ PASS (pending tasks.md) | tasks.md MUST list per-phase docs: vitest API/mocking docs, aspect `js_test` rule, `style/javascript.md` (incl. the new "Testing" section), and — for Phase 2 — the `vitest_test` macro interface documented in this plan's [Project Structure](#project-structure). |
| **VI. Service Large-Test Acceptance** | ⬜ N/A | This feature is test/build **infrastructure**, not a service application. No deployed service, no cross-service communication to validate. Large tests (`testplan`) are out of scope (spec Assumptions). Justified, no violation. |

**GATE RESULT: PASS** — no unresolved violations. §VI is N/A (justified by project type), recorded here.

## Project Structure

### Documentation (this feature)

```text
specs/019-js-test-reliability/
├── plan.md                       # This file (revised post-execution)
├── research.md                   # Phase 0: API verification, mock root-cause, alternatives (§3 to be corrected — see Stale-Doc Cascade)
├── data-model.md                 # Phase 1: entity taxonomy (entry_point value to be corrected)
├── quickstart.md                 # Phase 1: end-to-end validation scenarios (Scenario 2 to be corrected)
├── contracts/
│   └── run-vitest-shim.md        # Phase 1: process exit-code contract (UNCHANGED)
└── tasks.md                      # Phase 2 output (Phase 2 tasks to be corrected)
```

### Source Code (repository root)

This feature does NOT add a new source tree; it modifies existing test-infrastructure files. Touched locations:

```text
tools/dev/js/
├── run_vitest.mjs                # NEW (Phase 1, DONE): single canonical hardened shim (the one source of truth)
├── vitest_test.bzl               # NEW (Phase 2): the stable external macro `vitest_test` — encapsulates per-package shim copy + js_test wiring (genrule is internal to this macro)
└── BUILD.bazel                   # exports_files(["run_vitest.mjs"]) (Phase 1, DONE) — canonical source consumed by the macro's internal genrule

projects/game/agent/
├── BUILD.bazel                   # lib_test = vitest_test(name="lib_test", data=[...]); caller passes only the lib under test + test-file glob
├── run_vitest.mjs                # DELETED (per-package SOURCE replaced by macro-generated output)
└── src/*.test.ts                 # fragile-mock refactors (llm-tools, prompt-client) + failure fixes

common/js/logs/        { BUILD.bazel (vitest_test call), run_vitest.mjs DELETED, src/reporter.test.ts }
common/js/resolver/    { BUILD.bazel (vitest_test call), run_vitest.mjs DELETED }
common/js/otel/        { BUILD.bazel (vitest_test call), run_vitest.mjs DELETED }
common/js/grpc/otel/   { BUILD.bazel (vitest_test call), run_vitest.mjs DELETED, src/index.test.ts (proto path) }
common/js/grpc/resolver/ { BUILD.bazel (vitest_test call), run_vitest.mjs DELETED }

style/
└── javascript.md                 # NEW "Testing" section (FR-009): reliable vs fragile mocking + js_test conventions + the vitest_test macro usage
```

**Structure Decision (revised)**: The canonical shim logic lives in one file, `tools/dev/js/run_vitest.mjs`, in the existing `tools/dev/js/` directory (already home to shared JS build helpers `vite.bzl`, `ts_proto_library.bzl` — see [tools/dev/js/BUILD.bazel](../../tools/dev/js/BUILD.bazel)). Because `aspect_rules_js` cannot reference that file cross-package as a `js_test` `entry_point` (verified empirically — see [Architecture Revision](#architecture-revision-execution-discovery)), each consuming package must obtain a local copy that lands in its own runfiles tree next to its `node_modules/vitest` (each `js_test` package already runs `npm_link_all_packages(name = "node_modules")`, so `vitest` is resolvable from there). The copy is delivered by a **single macro `vitest_test`** rather than as exposed `genrule` boilerplate, so the wiring invariant is defined in one place and callers cannot get it wrong.

**External interface — `tools/dev/js/vitest_test.bzl` macro (`vitest_test`)**: this macro is the one stable, documented surface every `js_test` package uses. It encapsulates the per-package copy and the `js_test` wiring so that **callers pass only the code under test and the test-file glob** — they never see the `genrule`, the `entry_point`, or the `vitest` data dependency. Sketch:

```starlark
load("@aspect_rules_js//js:defs.bzl", "js_test")

# Stable external interface. Internally: per-package shim copy + js_test wiring.
def vitest_test(name, data, args = ["run", "src/"], **kwargs):
    native.genrule(                       # INTERNAL — callers do not see this
        name = "%s_shim" % name,
        srcs = ["//tools/dev/js:run_vitest.mjs"],
        outs = ["%s_run_vitest.mjs" % name],
        cmd = "cp $< $@",
    )
    js_test(
        name = name,
        entry_point = ":%s_run_vitest.mjs" % name,
        data = data + [":node_modules/vitest"],   # vitest auto-injected
        args = args,
        **kwargs
    )
```

Caller side (the entire contribution of a consuming BUILD file):

```starlark
load("//tools/dev/js:vitest_test.bzl", "vitest_test")

vitest_test(
    name = "lib_test",
    data = [":lib"] + glob(["src/**/*.test.ts"], allow_empty = True),
)
```

This makes BOTH the shim content AND the wiring pattern a single source of truth (Constitution §II), and the invariants (canonical source as `genrule` input, local `entry_point`, mandatory `:node_modules/vitest`) are enforced by the macro rather than left to convention. **Why a macro (not a custom rule, not bare genrules)**: a macro expands to the proven `genrule + js_test` native rules (already verified end-to-end) with zero new rule/provider code, while giving callers an identical experience to a custom rule; bare `genrule`s repeated in six BUILD files would re-expose the very details this design exists to hide and cannot be statically enforced. **Prerequisite** (documented on the macro): the calling package must already run `npm_link_all_packages(name = "node_modules")` so `:node_modules/vitest` resolves — all six target packages already do.

**Why `exports_files(["run_vitest.mjs"])` is retained (Phase 1, unchanged)**: the macro's internal `genrule` consumes `//tools/dev/js:run_vitest.mjs` as a `src`. The `exports_files` declaration makes that label referenceable cross-package for use as a `genrule` input (a `genrule` input is NOT subject to the `js_test` `copy_to_bin` constraint that blocks cross-package `entry_point`). Phase 1's work is therefore fully reused.

**Testing-conventions section**: rather than a standalone `style/js-testing.md`, FR-009 is satisfied by appending a new `## 测试 (Testing)` section to the existing [style/javascript.md](../../style/javascript.md). The section covers: the `js_test` execution model (pre-compiled `:lib` vs vitest-CLI transpile-on-the-fly); the shared `run_vitest.mjs` entry-point contract (see [contracts/run-vitest-shim.md](contracts/run-vitest-shim.md)) AND the `vitest_test` macro as the supported usage surface (why the shim is delivered per-package via the macro, not referenced cross-package); the reliable pattern (dependency injection / `vi.fn()` test-double seam) vs the fragile pattern (module-level `vi.mock("external-dep")` consumed transitively by a pre-compiled `:lib`) with the root cause from [research.md](research.md) §2; and a "verify the mock is active" rule (FR-010). Co-locating with the language style guide keeps all JS/TS conventions in one discoverable file.

## Architecture Revision (Execution Discovery)

This section records a correction forced by empirical findings during Phase 2 execution. It is the provenance/justification for every change above (Constitution §I, Governance "修订程序").

### What the original plan claimed

[research.md](research.md) §3 asserted that a cross-package `entry_point = "//tools/dev/js:run_vitest.mjs"` works because "`entry_point` is a `Label` whose file is auto-included in runfiles" and "each `js_test` still supplies its own `:node_modules/vitest` so `import { startVitest } from "vitest/node"` resolves". The original plan adopted this. **Both claims are false**, verified below.

### Verified findings (each reproduced with a fresh analysis cache, `--discard_analysis_cache`)

1. **Cross-package file-label `entry_point` FAILS at Bazel analysis.** Setting `entry_point = "//tools/dev/js:run_vitest.mjs"` on `//projects/game/agent:lib_test` fails with:

   > `Expected to find source file run_vitest.mjs in '//projects/game/agent', but instead it is in '//tools/dev/js'. All source and data files that are not in the Bazel output tree must be in in the same package as the target so that they can be copied to the output tree in an action. … Either move run_vitest.mjs to '//projects/game/agent', or add a 'js_library' target in run_vitest.mjs's package …`

   This is the `aspect_rules_js` `copy_to_bin` constraint (source: `copy_js_file_to_bin_action` in [js/private/js_helpers.bzl](https://github.com/aspect-build/rules_js/blob/main/js/private/js_helpers.bzl)). The `entry_point` attribute does accept a `Label` per [`js/private/js_binary.bzl`](https://github.com/aspect-build/rules_js/blob/main/js/private/js_binary.bzl) (whose `copy_data_to_bin` attr doc states "`data` files and the `entry_point` file are copied to the Bazel output tree"), but a *source file* must be in the same package or wrapped.

2. **A `js_library` wrapper passes analysis but FAILS at runtime.** Wrapping the shim in `js_library(name = "run_vitest_mjs", srcs = ["run_vitest.mjs"])` (Bazel's own recommended fix from the error above) and pointing `entry_point` at it lets analysis succeed — but the test then fails at runtime:

   > `Error [ERR_MODULE_NOT_FOUND]: Cannot find package 'vitest' imported from …/run_vitest.mjs`

   Node resolves `import { startVitest } from "vitest/node"` by walking up from the **entry_point file's location** (`tools/dev/js/` in runfiles), not from the consuming target's package. `vitest` is only resolvable where an `npm_link_all_packages(name = "node_modules")` link root exists — i.e. inside each pnpm-workspace package.

3. **`tools/dev/js` CANNOT become an npm-link root.** Attempting `npm_link_all_packages(name = "node_modules")` in `tools/dev/js/BUILD.bazel` fails:

   > `The npm_link_all_packages() macro … may only be called in bazel packages that correspond to the pnpm root package or pnpm workspace projects. … pnpm workspace projects: 'common/js/...', 'projects/game/agent', …`

   `tools/dev/js` is a Starlark tooling directory, not a pnpm workspace package (no `package.json`, not listed in `pnpm-workspace.yaml`). Making it a workspace package just to host a 30-line shim would be over-engineering and out of scope.

4. **Runfiles layout confirms the resolution model.** Inspecting `//projects/game/agent:lib_test` runfiles (working per-package configuration): `_main/node_modules` contains only `.aspect_rules_js` (the central store); `vitest` is reachable only via the per-package symlink `_main/projects/game/agent/node_modules/vitest → ../../../../node_modules/.aspect_rules_js/vitest@3.2.6_…/node_modules/vitest`. There is no top-level `vitest` resolvable from `tools/dev/js/`. (See [aspect_rules_js pnpm docs](https://github.com/aspect-build/rules_js/blob/main/docs/pnpm.md): "dependencies from the current directory's BUILD file and above".)

### The corrected design (verified working)

Single canonical source `tools/dev/js/run_vitest.mjs` + the `vitest_test` macro, which internally `genrule`-copies that source into each consuming package so the copy lands in the package's runfiles tree (e.g. `_main/projects/game/agent/lib_test_run_vitest.mjs`), where Node resolves `vitest` from that package's `node_modules/vitest`. The `genrule` is an internal detail of the macro; callers use only `vitest_test(name, data, …)`.

**Verification run** (a `genrule` copy of the canonical shim → local `entry_point` + the package's own `:node_modules/vitest` — the exact mechanism the macro encapsulates — fresh analysis cache):

```text
$ bazel test //projects/game/agent:lib_test --discard_analysis_cache
…
 Test Files  7 failed | 11 passed (18)
      Tests  26 failed | 250 passed (276)
   Duration  6.24s
//projects/game/agent:lib_test  FAILED in 6.8s   (Exit 1)
```

This is the **honest runner working**: vitest executed (276 tests, 6.24s), the 26 pre-existing failures surfaced, and the target correctly reported `FAILED` (Exit 1). The 26 failures are the US3 (Phase 4) backlog, not a wiring regression.

### Alternatives considered (and why rejected)

- **Per-package hand-maintained identical source files** (FR-005's explicit "identically-structured per-package script" wording): works, but relies on *convention* to keep six copies identical — the exact failure mode that caused 5/6 to drift buggy. The macro-generated approach achieves FR-005 *mechanically* (single generated source), which better satisfies Constitution §II. Rejected in favour of generation.
- **Bare `genrule` repeated in each BUILD file** (no macro): same underlying mechanism, but re-exposes the `entry_point`/`outs`/`vitest`-injection details at every call site, cannot be statically enforced, and needs per-site explanatory comments. Rejected — the `vitest_test` macro exists precisely to fix these details in one place.
- **A custom Bazel rule** (not a macro): would re-implement provider/runfiles plumbing for what is fundamentally "copy a file + forward `js_test`". Over-engineering; the macro expands to the already-proven `genrule + js_test` native rules with identical caller experience. Rejected in favour of the macro.
- **`DirectoryPathInfo` entry_point** (the canonical cross-package pattern in `aspect_rules_js` [`npm/private/npm_import.bzl`](https://github.com/aspect-build/rules_js/blob/main/npm/private/npm_import.bzl)): that pattern is for npm *bin* files whose store directory carries `node_modules`; it does not solve the `vitest`-resolution problem for a hand-written shim in a non-workspace directory. Rejected.
- **Make `tools/dev/js` a pnpm workspace package**: rejected as over-engineering (adds `package.json` + `pnpm-workspace.yaml` + lock churn) for a 30-line shim.
- **`no_copy_to_bin` exception**: addresses the *analysis* constraint only, not the *runtime* resolution blocker. Insufficient alone.

## Module-Identity Revision (Execution Discovery)

This section records a second correction, forced by US3 (Phase 4) execution. Whereas the [Architecture Revision](#architecture-revision-execution-discovery) corrected the shim *delivery mechanism*, this revision corrects the **what the test executes** — the pre-compiled `:lib` artifact vs. the package source. Full evidence and official vitest basis are in [research.md §6](research.md#6-why-do-instanceof-checks-and-module-singletons-fail-under-bazel-js_test-but-pass-under-the-vitest-cli-discovered-during-us3-execution).

### What execution surfaced

US3 triage (T015) found ~50+ of the pre-existing failures share a root cause outside §5's five categories: **dual-module-instance `instanceof`/singleton divergence**. They pass under the vitest CLI and fail under Bazel `js_test`, with the thrown objects having correct `name`+`message` but failing `instanceof` (resolver ~41, logs 6, agent ~2–9, possibly grpc/otel OTel-tracer split 4). This blocks SC-001 (full green).

### Verified findings

1. **Runfiles mix two module systems.** Each `js_test` target's runfiles `src/` contains the **pre-compiled CJS `:lib`** (`tsconfig` `"module": "commonjs"` → `ts_project`+`swc` emit `module.exports`) **alongside** the raw `.test.ts`. The test is vitest-Vite-transformed; the pre-compiled CJS's internal `require()` is **not intercepted** by vitest (Vite SSR does not rewrite CJS `require`), so the production code and the test resolve the *same* source file to **two different module instances** → `instanceof`/singleton state diverge.
2. **Empirical proof (CLI == single pipeline == pass).** `target.test.ts`: Bazel `js_test` fails every `instanceof InvalidTargetError`; the vitest CLI (transforms all package `.ts` through one Vite pipeline) reports **25/25 passed**. Reproduced `(cd common/js/resolver && vitest run src/target.test.ts)`.
3. **vitest maintainers confirm this is expected CJS behavior**, not a bug — see the issue citations in [research.md §6](research.md) (#7591, #5601, #6494, #9147, migration guide). The supported fixes are configuration (`server.deps.inline`), aliasing to `.ts`/ESM source, or ESM conversion.

### The corrected design (Fix B — test against source)

Each `vitest_test` caller's `data` changes from `[":lib"] + glob(["src/**/*.test.ts"])` to **`glob(["src/**/*.ts"])`** (all package source, production + tests; the compiled `:lib` is dropped from the test's data). vitest then transforms the entire package through a single Vite pipeline → one module instance → `instanceof`/singletons behave identically to the (passing) CLI mode. SC-003 (CLI == Bazel) holds trivially because both modes now transform the same source the same way.

Caller side becomes:

```starlark
load("//tools/dev/js:vitest_test.bzl", "vitest_test")

vitest_test(
    name = "lib_test",
    data = glob(["src/**/*.ts"], allow_empty = True),   # source, NOT :lib
    size = "small",
)
```

**Why drop `:lib` rather than keep both**: if both the compiled `errors.js` and the source `errors.ts` are present in runfiles, module resolution becomes ambiguous (Node/vitest may prefer `.js`), re-creating the dual instance. Test data must contain **only** the `.ts` source for the package's own code, so vitest resolves every relative import to `.ts` and transforms it once.

**Trade-off (acknowledged):** the test no longer exercises the swc-compiled artifact at runtime. Compile/type correctness is retained — `:lib` (`ts_project`) is still built as a dependency of `server_pkg` (and the existing `lib_typecheck_test`), so `bazel build //projects/game/agent:server_pkg` still type-checks and compiles. SC-001/SC-003 are better served by single-pipeline source transform. The `vitest_test` macro itself is unchanged (it still forwards `data` and auto-injects `:node_modules/vitest`); only the **caller's `data`** and the **documented contract** change.

### Alternatives considered (and why rejected)

- **Fix A — `server.deps.inline` (process `:lib` through Vite):** preserves "test the compiled artifact" but the inline pattern must match each package's own pre-compiled `src/*.js` in runfiles — runfiles-path-dependent and fragile; vitest upgrades may shift the mechanism. Rejected for fragility vs. Fix B's simplicity.
- **Fix C — convert `:lib` to ESM (`module`→ESM, `type:module`):** vitest fully supports ESM (single instance), but the blast radius is the entire production build (server packaging, all consumers, pnpm `exports`, swc config). Out of scope and too risky for a test-infrastructure feature. Rejected.
- **Inverse — `server.deps.external` (force native load for all):** would re-introduce the §2 `vi.mock` interception gap that US2 (Phase 3) just removed. Wrong direction. Rejected.

### Placement

Delivered as a **new Phase 4 (Module-Identity Infrastructure)** preceding US3 (which becomes Phase 5); Polish becomes Phase 6. It is a prerequisite for US3's full-green goal: after Fix B lands, the ~50+ dual-instance failures are expected to resolve, and US3 re-triages whatever genuine failures remain.

## Stale-Doc Cascade (must update before execution resumes)

The execution revision changes Phase 2's mechanism. The following docs still encode the disproven cross-package-`entry_point` assumption and **MUST be corrected for coherence** (Constitution §I — citations must be accurate):

- [research.md](research.md) §3 — record the verified findings above (copy_to_bin, runtime resolution, npm_link-workspace constraint) and the `vitest_test`-macro decision; preserve §3's *intent* (single source of truth) while correcting the *mechanism*.
- [quickstart.md](quickstart.md) Scenario 2 — update to the `vitest_test` macro usage (one canonical SOURCE; per-package SOURCE shims replaced by macro-generated outputs).
- [data-model.md](data-model.md) `JsTestTarget` entity — update the `entry_point` attribute (macro-generated local copy) and add the `vitest_test` macro relationship.
- [tasks.md](tasks.md) Phase 2 (T002, T003–T009) — replace "switch `entry_point` to `//tools/dev/js:run_vitest.mjs`" with "add `tools/dev/js/vitest_test.bzl` macro; each package calls `vitest_test(name="lib_test", data=[...])`"; T002's `exports_files` stays (consumed by the macro's internal genrule); the per-package shim deletions stay.

### Module-Identity cascade (Fix B)

The Module-Identity Revision changes the test's execution subject (source, not `:lib`). The following docs encode the "pre-compiled `:lib`" assumption and **MUST be corrected for coherence** (Constitution §I):

- [research.md](research.md) §6 — added (root cause + evidence + Fix B decision).
- [style/javascript.md](../../style/javascript.md) §测试 — the execution-model table must change: in BOTH modes vitest now transforms the package `.ts` source through a single pipeline; the "pre-compiled `:lib` vs vitest-CLI transpile-on-the-fly" contrast and the dual-instance root cause are reframed (the `vi.mock` interception gap of §2 is removed at the infrastructure level once tests no longer consume `:lib`).
- [data-model.md](data-model.md) `JsTestTarget.definition` — `data` is source glob, not `:lib`; note the trade-off (test executes source, not compiled artifact; compile correctness retained via `:lib` build).
- [quickstart.md](quickstart.md) Scenario 2/3 — update the data shape (`glob(["src/**/*.ts"])`, no `:lib`) and note both modes now agree by construction.
- [tasks.md](tasks.md) — insert new Phase 4 (Module-Identity Infrastructure); US3 becomes Phase 5; Polish becomes Phase 6.

## Complexity Tracking

No Constitution violations require justification. §VI (large-test acceptance) is N/A by project type (test infrastructure, not a service) — documented in the Constitution Check table above, not a complexity exception. The [Architecture Revision](#architecture-revision-execution-discovery) records the one design correction (forced by verified aspect_rules_js constraints) and its provenance; the `vitest_test` macro preserves the §II single-source-of-truth intent (for both shim content and wiring) while conforming to the package-local execution model. The [Module-Identity Revision](#module-identity-revision-execution-discovery) records a second correction (forced by vitest's expected CJS-handling limit, verified empirically + by vitest maintainers) — Fix B (test against source) preserves SC-001/SC-003 while removing the dual-instance root cause at the infrastructure level; §II (single source of truth for the test execution model) is upheld since all six targets adopt the same source-based `data` via the unchanged `vitest_test` macro.
