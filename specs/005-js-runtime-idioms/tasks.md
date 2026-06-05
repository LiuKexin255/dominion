# Tasks: JavaScript Runtime Packaging and Idiomatic Common APIs

**Input**: Design documents from `/specs/005-js-runtime-idioms/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/

**Constitution**: Generated tasks satisfy `.specify/memory/constitution.md`.
Includes repository-specific verification tasks for formatting, Gazelle/dependency
synchronization, pnpm catalog updates, unit tests, large tests for service code,
testplan execution, and full-repository Bazel build/test validation.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Ensure development environment and build tooling are ready; read required style and constitution files.

- [ ] T001 Read `.specify/memory/constitution.md`, `style/README.md`, `style/api.md`, and Google TypeScript Style Guide referenced by `style/README.md` before any code changes
- [ ] T002 Read existing `common/js/logs/package.json`, `common/js/otel/package.json`, `common/js/grpc/otel/package.json`, and `experimental/ts/grpc_hello_world/package.json` to understand current package metadata state
- [ ] T003 Read `tools/release/defs.bzl` and `experimental/ts/grpc_hello_world/BUILD.bazel` to understand current JS artifact packaging model
- [ ] T004 Read existing `common/js/logs/src/index.ts`, `common/js/logs/src/logger.ts`, `common/js/logs/src/reporter.ts`, and `common/js/logs/src/context.ts` to understand current logging API surface
- [ ] T005 [P] Read `common/js/logs/event/src/index.ts` and `common/js/logs/event/BUILD.bazel` to catalog all exports that must be migrated or removed

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Add runtime package metadata to all retained `common/js` packages so they can be resolved as normal Node packages. This MUST be complete before any user story work begins.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [ ] T006 Add `main`, `types`, and `exports` fields to `common/js/logs/package.json` matching the `ts_project` output layout (e.g., `"main": "src/index.ts"` for compile-time, with emitted path alignment for runtime)
- [ ] T007 [P] Add `main`, `types`, and `exports` fields to `common/js/otel/package.json` matching the `ts_project` output layout
- [ ] T008 [P] Add `main`, `types`, and `exports` fields to `common/js/grpc/otel/package.json` matching the `ts_project` output layout
- [ ] T009 Remove `@dominion/common-js-logs-event` dependency from `common/js/logs/package.json` if present as a workspace dependency
- [ ] T010 Verify each retained package compiles and type-checks with new metadata: `bazel build //common/js/logs //common/js/otel //common/js/grpc/otel`
- [ ] T011 Write a minimal Node `require()` smoke test or Bazel analysis test confirming each retained package's entrypoint resolves from the packaged layout (not only through TypeScript path mappings)

**Checkpoint**: Foundation ready — all retained common JS packages expose valid Node-resolvable metadata

---

## Phase 3: User Story 1 — Deploy TypeScript service without missing runtime modules (Priority: P1) 🎯 MVP

**Goal**: Service BUILD files declare only direct runtime dependencies; `artifact_pkg_js` automatically packages the transitive runtime closure.

**Independent Test**: Package and start the TypeScript gRPC hello world service through its deployment artifact; observe no module resolution errors.

### Implementation for User Story 1

- [ ] T012 [US1] Extend `artifact_pkg_js` in `tools/release/defs.bzl` with a runtime dependency input model that accepts direct workspace runtime packages and npm link targets, packaging the transitive runtime closure automatically
- [ ] T013 [US1] Introduce a Starlark provider (e.g., `JsRuntimePackageInfo`) in `tools/release/defs.bzl` exposing: package name, package metadata, compiled runtime files, declaration outputs, direct runtime deps, and transitive runtime files for workspace JS runtime packages
- [ ] T014 [US1] Implement transitive closure collection logic in `artifact_pkg_js` that walks the `JsRuntimePackageInfo` provider chain to collect all indirect runtime files, preserving node_modules-style layout for third-party packages without flattening that breaks pnpm/peer-dependency semantics
- [ ] T015 [US1] Add build-time validation in `artifact_pkg_js` that detects missing package metadata or unresolved runtime dependencies and fails with a clear error message before deployment
- [ ] T016 [US1] Update `experimental/ts/grpc_hello_world/BUILD.bazel` to declare only direct runtime dependencies using the new `artifact_pkg_js` input model instead of manually enumerating indirect dependencies
- [ ] T017 [US1] Remove obsolete workspace dependency references from `experimental/ts/grpc_hello_world/package.json` if present
- [ ] T018 [US1] Implement a local Node startup/import smoke test target that runs against the packaged artifact contents and fails before deployment if any required module cannot be resolved
- [ ] T019 [US1] Build the service image artifact: `bazel build //experimental/ts/grpc_hello_world:cmd_image` and verify packaging succeeds
- [ ] T020 [US1] Run the local package smoke test to confirm all direct and transitive runtime modules resolve without `MODULE_NOT_FOUND` errors

**Checkpoint**: The TypeScript gRPC hello world service packages and starts from its artifact with zero missing modules while declaring only direct runtime dependencies.

---

## Phase 4: User Story 2 — Use common JavaScript logging in a Node-idiomatic way (Priority: P2)

**Goal**: Replace Go-style event structs with plain object attributes; remove the `logs/event` package; expose JS-idiomatic logging helpers from `common/js/logs`.

**Independent Test**: Write logging calls using plain attribute objects; verify console and OTel outputs preserve message and structured fields; confirm zero imports of the removed event package.

### Implementation for User Story 2

- [ ] T021 [P] [US2] Delete `common/js/logs/event/` package directory entirely (including `BUILD.bazel`, `src/index.ts`, `src/index.test.ts`, `package.json`, `tsconfig.json`, `.swcrc`, `run_vitest.mjs`)
- [ ] T022 [P] [US2] Remove `Event` type and variadic event arguments from logger methods in `common/js/logs/src/logger.ts`; accept `Record<string, unknown>` attribute objects as primary parameter
- [ ] T023 [US2] Migrate any retained useful helpers from `common/js/logs/event` into `common/js/logs/src/index.ts` using JavaScript-style naming (e.g., optional error attribute helper returning plain attributes or `undefined`)
- [ ] T024 [US2] Rename `newOTelReporter` to `createOTelReporter` in `common/js/logs/src/reporter.ts`; add a deprecated alias only if existing tests show migration risk that warrants it
- [ ] T025 [US2] Change `LogLevel` to a string union type or const-object shape in `common/js/logs/src/logger.ts` (e.g., `type LogLevel = "debug" | "info" | "warn" | "error"`)
- [ ] T026 [US2] Update `common/js/logs/BUILD.bazel` to remove the `event` sub-package target and any references to `@dominion/common-js-logs-event`
- [ ] T027 [US2] Update `common/js/logs/src/index.ts` to export the new primary logging API shape: `info("message", { key: "value" })`, `warn(...)`, `error(...)`, `debug(...)` as specified in `contracts/logs-api.md`
- [ ] T028 [US2] Update all existing tests in `common/js/logs/` to use plain attribute objects instead of event constructors; ensure tests compile and pass: `bazel test //common/js/logs/...`
- [ ] T029 [US2] Search entire repository for remaining imports of `@dominion/common-js-logs-event`, `eventString`, `eventInt`, `eventBool`, `eventAny`, or `newOTelReporter` and update or remove them
- [ ] T030 [US2] Update `experimental/ts/grpc_hello_world/src/server.ts` and `experimental/ts/grpc_hello_world/src/bootstrap.ts` to import logging helpers only from `@dominion/common-js-logs` using the new API shape

**Checkpoint**: The `logs/event` package is gone; all logging uses plain attribute objects; no Go-style event references remain.

---

## Phase 5: User Story 3 — Maintain observability behavior while simplifying package layout (Priority: P3)

**Goal**: OTel log records preserve structured attributes; gRPC instrumentation load-order is maintained; service observability is not regressed.

**Independent Test**: Initialize the TypeScript gRPC service with shared OTel packages; verify startup, structured log output, and trace/log correlation.

### Implementation for User Story 3

- [ ] T031 [US3] Update `OTelReporter` in `common/js/logs/src/reporter.ts` to emit message text as log body and caller fields as structured log attributes (not serialized into body string)
- [ ] T032 [US3] Ensure severity number and severity text remain consistent between console and OTel reporter paths
- [ ] T033 [US3] Map error values to supported OTel log attribute values or documented string fields in `common/js/logs/src/reporter.ts`; ensure absent error values produce no empty-key sentinel attributes
- [ ] T034 [US3] Write unit test in `common/js/logs/` asserting OTel emit receives `body`, `severityNumber`, `severityText`, and `attributes` as separate fields
- [ ] T035 [US3] Verify console output remains structured JSON through existing or updated console reporter tests in `common/js/logs/`
- [ ] T036 [US3] Preserve dynamic import/load-order pattern for gRPC instrumentation in `experimental/ts/grpc_hello_world/src/bootstrap.ts`; confirm OTel SDK and instrumentations are registered before `@grpc/grpc-js` is loaded
- [ ] T037 [US3] Confirm each shared JS package resolves through its package metadata when consumed from the packaged service artifact (not through repository-local TypeScript path mappings)

**Checkpoint**: Observability is preserved; OTel logs have queryable structured attributes; gRPC instrumentation load-order guarantee holds.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Final verification, code quality review, and repository-wide validation.

- [ ] T038 Run `rg "@dominion/common-js-logs-event|eventString|eventInt|eventBool|eventAny|newOTelReporter"` across `common/js` and `experimental/ts/grpc_hello_world`; confirm zero active source usages except documented compatibility aliases if intentionally retained
- [ ] T039 [P] Run quickstart.md validation: execute steps 1–4 from `specs/005-js-runtime-idioms/quickstart.md`
- [ ] T040 [P] Run TypeScript formatting through Bazel wrapper for all changed files: `bazel run //:go -- fmt` (if applicable) or manual `prettier`/formatting check per repository style
- [ ] T041 Run `bazel run //:gazelle` to update BUILD.bazel files for any changed dependency inputs
- [ ] T042 [P] Confirm TypeScript/JavaScript dependency versions remain centralized in root `pnpm-workspace.yaml` catalog; verify no sub-package `pnpm-lock.yaml` was introduced; document any special direct-version exception if needed
- [ ] T043 Run `bazel build //common/js/...` to verify all common JS packages build cleanly
- [ ] T044 Run `bazel test //common/js/...` to verify all common JS tests pass
- [ ] T045 Run `bazel build //experimental/ts/grpc_hello_world:cmd_image` to verify the service image builds
- [ ] T046 Run `guitar validate experimental/ts/grpc_hello_world/testplan/interface_test.yaml` to verify the testplan definition is valid
- [ ] T047 Execute `guitar run experimental/ts/grpc_hello_world/testplan/interface_test.yaml` if deployment infrastructure is reachable; otherwise record external blocker and residual risk in the task completion notes
- [ ] T048 Run `bazel build //...` for full-repository build verification
- [ ] T049 Run `bazel test //...` for full-repository test verification
- [ ] T050 Code quality review covering: JS naming conventions, package metadata completeness, OTel attribute correctness, artifact dependency closure correctness, test-code review, and style review per `style/api.md` and Google TypeScript Style Guide

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion — BLOCKS all user stories
- **User Story 1 (Phase 3, P1)**: Depends on Phase 2 completion
- **User Story 2 (Phase 4, P2)**: Depends on Phase 2 completion; independent of US1
- **User Story 3 (Phase 5, P3)**: Depends on Phase 2 completion; benefits from US2 completion (uses new reporter shape) but can proceed in parallel with integration adjustments
- **Polish (Phase 6)**: Depends on US1 + US2 + US3 completion

### User Story Dependencies

- **US1 (P1)**: Can start after Phase 2. No dependencies on US2 or US3.
- **US2 (P2)**: Can start after Phase 2. No dependencies on US1. Produces new logging API that US3 will validate.
- **US3 (P3)**: Can start after Phase 2. Logically depends on US2's reporter changes being complete for final OTel attribute validation, but core load-order and package resolution tasks can proceed independently.

### Within Each User Story

- Models/types before services
- Service logic before endpoints/integration
- Package removal before migration
- Core implementation before smoke tests and validation

### Parallel Opportunities

- Phase 1: T002, T003, T004, T005 can run in parallel (read-only exploration)
- Phase 2: T007, T008 can run in parallel with T006 (different package.json files)
- Phase 4: T021, T022 can run in parallel (different files — deletion vs. logger update)
- Phase 5: T034, T035 can run in parallel (different test files)
- Phase 6: T039, T040, T042 can run in parallel (independent checks)
- After Phase 2: US1 and US2 can be worked on in parallel by different developers

---

## Parallel Example: User Story 2

```
# After Phase 2 completes, these can launch in parallel:
Task: "Delete common/js/logs/event/ package directory"
Task: "Remove Event type from logger.ts"

# Then sequentially:
Task: "Migrate retained helpers into common/js/logs/src/index.ts"
Task: "Update index.ts exports to new API shape"
Task: "Update tests to use plain attribute objects"

# Then parallel verification:
Task: "Search repo for remaining event-package imports"
Task: "Update grpc_hello_world imports"
```

## Parallel Example: User Story 1

```
# After Phase 2 completes, these can launch in parallel:
Task: "Introduce JsRuntimePackageInfo provider in defs.bzl"
Task: "Read current grpc_hello_world BUILD.bazel to plan direct-dep model"

# Then sequentially:
Task: "Implement transitive closure collection in artifact_pkg_js"
Task: "Update grpc_hello_world BUILD.bazel with new declarations"

# Then parallel verification:
Task: "Build service image artifact"
Task: "Run local package smoke test"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup — read all required context files
2. Complete Phase 2: Foundational — add package metadata to all retained packages
3. Complete Phase 3: User Story 1 — runtime dependency packaging
4. **STOP and VALIDATE**: Build service image, run smoke test, confirm no missing modules
5. Deploy/demo if ready — this alone fixes the most immediate user-visible failure

### Incremental Delivery

1. Setup + Foundational → Foundation ready
2. Add US1 → Test independently → Service packages and starts correctly (MVP!)
3. Add US2 → Test independently → Logging is JS-idiomatic, event package removed
4. Add US3 → Test independently → Observability preserved with structured OTel attributes
5. Polish → Full repository validation, code quality review
6. Each story adds value without breaking previous stories

### Parallel Team Strategy

With multiple developers:

1. Team completes Setup + Foundational together
2. Once Foundational is done:
   - Developer A: User Story 1 (packaging — `tools/release/defs.bzl`, service BUILD)
   - Developer B: User Story 2 (logging API — `common/js/logs/`, event removal)
3. US3 proceeds after US2's reporter changes are in place
4. Final Polish done together

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story should be independently completable and testable
- Final validation must prove delivered behavior through the artifact's real surface
- Do not mark T047 (testplan execution) as skipped unless the blocker and residual risk are documented
- The `logs/event` package removal (T021) should happen early in US2 to prevent accidental re-imports
- Package metadata fields must match the actual `ts_project` output layout, not just the source layout
- Avoid: vague tasks, same file conflicts, cross-story dependencies that break independence
