# Feature Specification: JavaScript Test Reliability Under Bazel

**Feature Branch**: `019-js-test-reliability`

**Created**: 2026-07-16

**Status**: Draft

**Input**: User description: "收集 developer 反馈的 js bazel 单测文件，创建新的需求规范，包含以下目标：1. 查验修复生效，以及是否存在更有更好的解决方案；2. 针对 js test mock 脆弱的问题，进行加固，并 review 仓库内所有使用 mock 的 js 测试；3. 修复所有已经存在的 js test 问题。"

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Bazel Test Runner Reports Real Results (Priority: P1)

A developer runs the TypeScript unit test suite through Bazel (the repository's mandated build/test entry point) and expects the pass/fail outcome reported by Bazel to faithfully reflect the actual test execution. Today this trust is broken: every `js_test` target in the repository uses a shared `run_vitest.mjs` shim that swallows assertion failures — the Bazel test step exits zero (green) regardless of whether individual tests passed or failed. The developer only learns a test was broken when the same test is run outside Bazel (e.g., invoking vitest directly), and CI pipelines that gate on Bazel test results provide no real signal.

**Why this priority**: without a trustworthy green/red signal from Bazel, every other test-quality effort in this spec is unverifiable. This is the foundation — the shim must exit non-zero on test failures before any test-fixing work can be validated through the standard build pipeline.

**Independent Test**: run any `js_test` target that contains at least one deliberately-failing assertion through Bazel; the Bazel step MUST report FAILED. Then remove the deliberate failure; the same target MUST report PASSED. This can be fully validated without touching any test file content.

**Acceptance Scenarios**:

1. **Given** a `js_test` target whose test suite contains a failing assertion, **When** the developer runs `bazel test` on that target, **Then** Bazel reports the target as FAILED with a non-zero exit code and the test log shows the failing assertion.
2. **Given** a `js_test` target whose test suite passes fully, **When** the developer runs `bazel test` on that target, **Then** Bazel reports the target as PASSED.
3. **Given** the repository contains multiple `js_test` targets across different packages, **When** the developer runs `bazel test` on each, **Then** every target's pass/fail status matches the actual test outcome (no false greens).
4. **Given** a test suite that passes locally via the vitest CLI, **When** the same suite runs through the Bazel `js_test` target, **Then** both invocations agree on pass/fail.

---

### User Story 2 — Mock-Based Tests Are Robust and Maintainable (Priority: P2)

A developer writes or maintains a TypeScript unit test that uses module-level mocks (e.g., `vi.mock()` to intercept external dependency imports) and expects the mock to take effect consistently — whether the test runs through the vitest CLI directly or through the Bazel `js_test` target that executes against the pre-compiled library artifact. Today this expectation is unreliable: certain `vi.mock()` calls do not intercept the pre-compiled artifact's imports under Bazel, causing tests to silently bypass the mock, call the real dependency, and either fail opaquely or produce false results. The developer has no clear guidance on which mocking patterns are safe under both execution modes.

**Why this priority**: once the test runner faithfully reports results (US1), the next layer of trust is that the tests themselves exercise the intended code paths. A mock that silently fails to intercept is worse than no mock — it produces a green test that validates nothing. This story covers hardening the mocking patterns across the repository and establishing conventions that prevent regression.

**Independent Test**: take any test file that currently uses `vi.mock()` and whose result differs between direct-vitest and Bazel-vitest; rewrite it so both modes agree. Validate by running both invocations and comparing results.

**Acceptance Scenarios**:

1. **Given** a test that mocks an external module, **When** the test runs via the vitest CLI AND via the Bazel `js_test` target, **Then** both invocations produce the same pass/fail result and exercise the mocked code path (not the real dependency).
2. **Given** the repository's full set of mock-based test files, **When** a reviewer audits each file, **Then** every mock usage follows a documented, reliable pattern — and any file that cannot follow the pattern has a recorded justification and an alternative testing strategy.
3. **Given** a developer adding a new mock-based test, **When** they consult the repository's testing conventions, **Then** they find clear guidance on which mocking strategies work reliably under both vitest-CLI and Bazel-`js_test` execution, avoiding the fragile patterns that caused prior failures.
4. **Given** a test that previously relied on a fragile module mock, **When** it is refactored to a reliable pattern (e.g., dependency injection, direct function invocation, or a test-double seam), **Then** the refactored test passes identically under both execution modes and its assertions verify the intended behavior.

---

### User Story 3 — All Pre-Existing Test Failures Are Fixed (Priority: P2)

A developer runs the full TypeScript test suite through Bazel and expects zero failures from pre-existing tests — every test that was written before this feature should pass under the corrected test runner. Today, once the test-runner shim is fixed (US1), a substantial number of pre-existing test failures surface across the agent service package. These failures were previously invisible because the broken shim reported them as passing. The failures span multiple root causes: gRPC client mock callback mismatches, proto-file path resolution under the Bazel sandbox, module-mock interception gaps (US2), spy/assertion mismatches, and fake-model test-harness state issues.

**Why this priority**: a test suite with known-broken tests erodes developer trust in CI and makes it impossible to detect new regressions. Once the runner is honest (US1) and mocking is reliable (US2), the remaining failures are genuine test bugs that must be fixed so the suite returns to a trustworthy green baseline.

**Independent Test**: run `bazel test` on every `js_test` target in the repository; every target MUST report PASSED with zero failures. Any test that cannot be fixed within this feature's scope MUST be documented with a recorded justification and a tracking item, not silently left red.

**Acceptance Scenarios**:

1. **Given** the agent service's test suite currently has multiple pre-existing failures, **When** the developer runs `bazel test` on the agent `js_test` target after all fixes, **Then** the target reports PASSED with zero failures.
2. **Given** a test that fails due to a mock callback signature mismatch (e.g., a gRPC stub mock invoked with a callback-style API that no longer matches the real client's invocation), **When** the test is fixed, **Then** the mock correctly simulates the real client's behavior and the test passes.
3. **Given** a test that fails due to a proto-file path that does not resolve under the Bazel sandbox runfiles tree, **When** the test is fixed, **Then** the proto file is located correctly in both direct-vitest and Bazel-vitest execution.
4. **Given** the full set of `js_test` targets across all packages, **When** the developer runs `bazel test` on each, **Then** every target reports PASSED.
5. **Given** a test whose failure is genuinely out of scope (e.g., it tests a feature actively being rewritten by a concurrent effort), **When** the developer encounters it, **Then** the test is documented as skipped-with-justification (not silently left failing), and the justification references the concurrent effort.

---

### Edge Cases

- What happens when a `js_test` target's test suite is empty (zero test files)? The runner MUST report PASSED (vacuously), not crash or hang.
- What happens when vitest encounters an unhandled exception outside any test case (e.g., a top-level `import` error)? The runner MUST exit non-zero so Bazel reports FAILED.
- What happens when a mock-based test exercises asynchronous teardown that races with the process exit? The runner MUST await vitest's completion before checking the failure count.
- What happens when two `js_test` targets in different packages share a common mock dependency but one packages its mock differently? Each target's mocks MUST resolve independently without cross-target interference.
- What happens when a pre-existing test failure is caused by a real production-code bug (not a test bug)? The spec treats this as a test-failure-to-triage: the test is correct, the production code is broken. The fix may touch production code, and the spec does not forbid that — but the fix MUST be scoped to making the test pass legitimately, not to weakening the test's assertions.

## Requirements *(mandatory)*

### Functional Requirements

**Test Runner Honesty (US1)**

- **FR-001**: Every `js_test` target in the repository MUST exit with a non-zero status code when any test case in its suite fails an assertion.
- **FR-002**: Every `js_test` target MUST exit zero when all test cases pass.
- **FR-003**: The test-runner shim MUST correctly parse the vitest CLI invocation (command mode and file filters) so that the intended subset of tests runs — not the entire suite by default.
- **FR-004**: The test-runner shim MUST await vitest's full completion (including asynchronous teardown) before evaluating the pass/fail outcome.
- **FR-005**: The test-runner shim MUST be consistent across all packages — a single, shared, well-documented approach (whether a shared script or an identically-structured per-package script) MUST be used so that no package's test runner silently drifts.
- **FR-006**: The fixed test-runner approach MUST be validated against the vitest API documentation to confirm it uses the correct function signature and return-value semantics (the prior bug was a signature misuse).

**Mock Hardening (US2)**

- **FR-007**: The repository MUST contain a documented audit of every TypeScript test file that uses `vi.mock()`, `vi.fn()`, `vi.spyOn()`, or equivalent mock primitives, classifying each usage as "reliable under both vitest-CLI and Bazel-`js_test`" or "fragile."
- **FR-008**: Every "fragile" mock usage identified in the audit MUST be either (a) refactored to a reliable pattern, or (b) documented with a recorded justification and an alternative testing strategy that provides equivalent coverage.
- **FR-009**: The repository's testing conventions document MUST include guidance on which mocking patterns are reliable under Bazel `js_test` execution and which to avoid, so future tests do not regress.
- **FR-010**: A test that mocks a module-level import MUST be verifiable: there MUST be a way to confirm the mock is active (e.g., the test asserts the mock was called, or the test would fail if the real dependency ran).

**Pre-Existing Failure Fixes (US3)**

- **FR-011**: Every pre-existing test failure that surfaces once the runner is fixed (FR-001) MUST be either fixed or explicitly documented as deferred with a tracked justification.
- **FR-012**: A test failure caused by a mock that does not match the real API's invocation contract MUST be fixed by correcting the mock to faithfully simulate the real API — not by weakening the test's assertions.
- **FR-013**: A test failure caused by a resource-path that does not resolve under the Bazel sandbox MUST be fixed by using a path-resolution strategy that works in both direct and Bazel execution (e.g., runfiles-aware resolution).
- **FR-014**: The total count of deferred (not-fixed) test failures MUST be zero unless each deferral has a documented, tracked justification referencing a concurrent effort or an out-of-scope dependency.

### Key Entities

- **Test-runner shim**: the script (e.g., `run_vitest.mjs`) that a `js_test` Bazel target invokes as its entry point; it calls the vitest programmatic API and is responsible for translating vitest's results into a process exit code.
- **`js_test` target**: a Bazel test target (defined via `aspect_rules_js`) that runs a TypeScript test suite through the test-runner shim inside a sandboxed environment.
- **Pre-compiled library artifact**: the output of a `ts_project` Bazel target; test files included as raw `.ts` `data` may import from this artifact, causing module-mock interception to behave differently than when vitest transpiles source on the fly.
- **Mock audit**: a classification of every mock-based test file in the repository, recording the mocking strategy used and whether it is reliable under both execution modes.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Running `bazel test` on every `js_test` target in the repository reports PASSED for every target, with zero failing tests across the entire suite.
- **SC-002**: A deliberately-injected failing assertion in any `js_test` target causes that target to report FAILED through Bazel — validated on at least three targets across different packages.
- **SC-003**: For every test file that uses module-level mocks, running the file through both `vitest run` (direct CLI) and `bazel test` (Bazel target) produces identical pass/fail results.
- **SC-004**: The count of pre-existing test failures that are deferred (not fixed) is zero, or each deferral has a recorded justification with a tracking reference.
- **SC-005**: A developer adding a new mock-based test can find, in the repository's testing conventions, clear guidance on reliable mocking patterns — and following that guidance produces a test that passes identically under both execution modes.

## Assumptions

- The vitest version currently pinned in the repository's dependency catalog is the target version; this feature does not upgrade vitest unless the upgrade is itself necessary to fix a failure.
- The Bazel `js_test` infrastructure (via `aspect_rules_js`) is the correct and retained test-execution mechanism; this feature does not propose replacing it with a different test runner.
- Pre-existing test failures are bugs in the tests or in production code that the tests exercise — not symptoms of an fundamentally wrong testing approach. The fixes are surgical corrections, not a rewrite of the test strategy.
- The scope is limited to TypeScript/JavaScript test files executed through Bazel `js_test` targets. Go tests, end-to-end/large tests, and frontend-component tests (if they use a different runner) are out of scope unless they share the same `run_vitest.mjs` shim.
- Packages outside `projects/game/agent/` and `common/js/` that do not use `js_test` targets are unaffected and out of scope.

## References *(mandatory per Constitution §I — Citation Provenance)*

### Official Documentation

- [vitest — `startVitest` API](https://vitest.dev/api/#startvitest) — the programmatic Node.js API; the signature is `(mode, cliFilters, options)`, returning a `Vitest` instance whose `state` exposes `getCountOfFailedTests()`. The prior shim bug was a misuse of this signature.
- [vitest — Mocking guide](https://vitest.dev/guide/mocking.html) — official guidance on `vi.mock()`, `vi.doMock()`, and the module-interception lifecycle; documents the hoisting behavior that affects when mocks take effect relative to pre-compiled imports.
- [Aspect Build — `js_test` rule](https://docs.aspect.build/rules/aspect_rules_js/js_test/) — the Bazel rule that runs a JavaScript test target; documents the `entry_point`, `args`, and `data` attributes and the sandboxed execution model.
- [Bazel — Runfiles](https://bazel.build/extending/rules#runfiles) — the runfiles tree layout that test binaries execute against; relevant to proto-file path resolution failures under the sandbox.

### Repositories

- No external repository references. All affected code is in this repository (`projects/game/agent/`, `common/js/*/`).

### Articles & RFCs

- No article or RFC references.
