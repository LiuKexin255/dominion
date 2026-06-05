---

description: "Task list for JavaScript Common Library with OTel gRPC-JS Support & Structured Logging"
---

# Tasks: JavaScript Common Library with OTel gRPC-JS Support & Structured Logging

**Input**: Design documents from `/specs/004-js-common-otel-logs/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/

**Constitution**: Generated tasks satisfy `.specify/memory/constitution.md`.
Includes repository-specific verification tasks for formatting, Gazelle/dependency
synchronization, pnpm catalog updates for TypeScript/JavaScript dependency
versions, unit tests, testplan execution, and full-repository Bazel build/test
validation.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- **Common JS packages**: `common/js/logs/`, `common/js/logs/event/`, `common/js/grpc/otel/`, `common/js/otel/`
- **Acceptance service**: `experimental/ts/grpc_hello_world/`
- **Workspace config**: `pnpm-workspace.yaml`, `.bazelignore` at repository root

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Workspace configuration and npm dependency setup for the common JS library

- [ ] T001 [P] Add `common/js/*` to `packages` array and add OTel, gRPC test fixture, and vitest catalog entries to `pnpm-workspace.yaml`
- [ ] T002 [P] Add `common/js/*/node_modules` to `.bazelignore`
- [ ] T003 Run `bazel run @pnpm -- --dir /mnt/code/dominion install` and verify workspace resolution succeeds with new packages listed

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Establish the repository's vitest-under-Bazel pattern with the smallest package (event sub-package) before building larger packages

**CRITICAL**: No user story work can begin until this phase is complete. This proves the TS compilation + test toolchain works.

- [ ] T004 Create `common/js/logs/event/` package scaffold: `package.json` (name `@dominion/common-js-logs-event`, deps via `catalog:`), `tsconfig.json` (ES2020, strict, CommonJS), `.swcrc`, and `BUILD.bazel` with `ts_project` + SWC + vitest test target pattern
- [ ] T005 Implement `Event` interface and constructors (`string`, `int`, `bool`, `err`, `any`) with zero-value handling in `common/js/logs/event/src/index.ts`
- [ ] T006 Create unit tests for Event constructors and zero-value behavior in `common/js/logs/event/src/index.test.ts`
- [ ] T007 Verify `bazel build //common/js/logs/event:lib` and `bazel test //common/js/logs/event:lib_test` pass

**Checkpoint**: vitest-under-Bazel pattern proven — user story implementation can now begin in parallel

---

## Phase 3: User Story 1 — JavaScript Common Library Foundation (Priority: P1) 🎯 MVP

**Goal**: Create the OTel provider initialization package under `common/js/otel/` and validate that the common JS library structure follows all repository conventions (ts_project + SWC + CommonJS + ES2020 + strict + @dominion/ namespace + catalog deps)

**Independent Test**: `bazel build //common/js/otel:lib` and `bazel test //common/js/otel:lib_test` pass; all dependency versions use `catalog:` protocol

### Implementation for User Story 1

- [ ] T008 [US1] Create `common/js/otel/` package scaffold: `package.json` (name `@dominion/common-js-otel`, OTel SDK deps via `catalog:`), `tsconfig.json`, `.swcrc`, and `BUILD.bazel` with `ts_project` + SWC + vitest test target per `common/js/logs/event` pattern
- [ ] T009 [US1] Implement OTel provider initialization in `common/js/otel/src/index.ts`: `init(config?)` (Promise-based, creates TracerProvider + MeterProvider + LoggerProvider in deploy mode, local-only in non-deploy), `tracer()`, `meter()`, `traceId()`, `isLoggerProviderSet`, `shutdown()` (Promise-based); deploy detection via `SERVICE_APP`/`DOMINION_ENVIRONMENT`/`POD_NAMESPACE`; use OTLP HTTP exporters only (never gRPC exporters)
- [ ] T010 [US1] Create unit tests in `common/js/otel/src/index.test.ts`: deploy/non-deploy initialization paths, mocked HTTP exporters, `isLoggerProviderSet` state, Promise-based `shutdown` semantics, no-op when no provider configured
- [ ] T011 [US1] Verify `bazel build //common/js/otel:lib` and `bazel test //common/js/otel:lib_test` pass; confirm all `package.json` deps use `"catalog:"` protocol

**Checkpoint**: Common JS library foundation established. OTel provider package is functional and independently testable.

---

## Phase 4: User Story 3 — Structured Logging with OTel Reporter (Priority: P2)

**Goal**: Create `common/js/logs/` with idiomatic JS/TS structured logging, console default output, OTel reporter routing via `installReporter`, and uninstall semantics matching Go `common/gopkg/logs` behavior

**Independent Test**: Unit tests verify (a) console output when no reporter installed, (b) OTel bridge routing when reporter installed, (c) uninstall restores console, (d) object-style structured attributes, (e) LOG_LEVEL filtering, (f) `installReporter(null)` throws

### Implementation for User Story 3

- [ ] T012 [US3] Create `common/js/logs/` package scaffold: `package.json` (name `@dominion/common-js-logs`, deps on `@dominion/common-js-logs-event` workspace package + OTel logs deps via `catalog:`), `tsconfig.json`, `.swcrc`, and `BUILD.bazel`
- [ ] T013 [P] [US3] Implement `Logger` class and default logger singleton in `common/js/logs/src/logger.ts`: `info(msg, attrs?, ...events)`, `warn`, `error`, `debug` with `LogLevel` enum; lazy default instance; `LOG_LEVEL` environment variable filtering (debug vs info default)
- [ ] T014 [P] [US3] Implement `Reporter` abstraction in `common/js/logs/src/reporter.ts`: `Reporter` interface with `write(level, msg, attrs)`, `ConsoleReporter` (JSON to stdout), `OTelReporter` (bridges to `@opentelemetry/api-logs` LoggerProvider using `logs.getLogger(name)` + `logger.emit()` with `SeverityNumber` mapping), `installReporter(reporter)` (throws on null, returns uninstall function with replacement semantics), `newOTelReporter(name)`, `setDefault(logger)`
- [ ] T015 [P] [US3] Implement context helpers in `common/js/logs/src/context.ts`: `currentLogger()` (falls back to default), `withAttributes<T>(attrs, fn)` and `withLogger<T>(logger, fn)` using `AsyncLocalStorage` for scoped logger/attribute propagation
- [ ] T016 [US3] Create public API barrel export in `common/js/logs/src/index.ts`: re-export all package-level functions (`info`, `warn`, `error`, `debug`, `defaultLogger`, `currentLogger`, `withAttributes`, `withLogger`, `installReporter`, `newOTelReporter`, `setDefault`), types (`Logger`, `Reporter`, `LogAttributes`, `LogAttributeValue`, `Event`, `LogLevel`), and event constructors from `@dominion/common-js-logs-event`
- [ ] T017 [US3] Create unit tests in `common/js/logs/src/reporter.test.ts`: console fallback when no reporter installed, OTel routing when reporter installed via `installReporter`, uninstall restores console behavior, uninstall replacement semantics (only removes own reporter), `installReporter(null)` throws error
- [ ] T018 [US3] Create unit tests in `common/js/logs/src/logger.test.ts`: `info('message', { key: 'value' })` console output, LOG_LEVEL filtering (debug suppressed by default), object-style structured attributes, event helper attribute merging
- [ ] T019 [US3] Create unit tests in `common/js/logs/src/context.test.ts`: `withAttributes` enriches log calls within scope, `withLogger` switches logger within scope, nested `withAttributes` merges correctly, falls back to default logger outside scope
- [ ] T020 [US3] Verify `bazel build //common/js/logs:lib` and `bazel test //common/js/logs:lib_test` pass

**Checkpoint**: Structured logging package fully functional with console default and OTel reporter routing, independently testable

---

## Phase 5: User Story 2 — gRPC-JS OpenTelemetry Support (Priority: P2)

**Goal**: Create `common/js/grpc/otel/` that wraps `@opentelemetry/instrumentation-grpc` and validates span creation and trace context propagation for unary, server-streaming, client-streaming, and bidirectional-streaming gRPC-JS calls

**Independent Test**: Real in-process gRPC-JS client/server tests validate all four RPC types produce client/server spans with `rpc.system = "grpc"`, `rpc.service`, `rpc.method` attributes, correct status, and parent-child trace propagation

### Implementation for User Story 2

- [ ] T021 [US2] Create `common/js/grpc/otel/` package scaffold: `package.json` (name `@dominion/common-js-grpc-otel`, deps via `catalog:` including `@opentelemetry/instrumentation-grpc`; test deps: `@grpc/grpc-js`, `@grpc/proto-loader`), `tsconfig.json`, `.swcrc`, and `BUILD.bazel`
- [ ] T022 [US2] Implement gRPC instrumentation wrapper in `common/js/grpc/otel/src/index.ts`: re-export `GrpcInstrumentation` from `@opentelemetry/instrumentation-grpc`; export `createGrpcInstrumentation(config?)` with empty `ignoreGrpcMethods` and `metadataToSpanAttributes` defaults; document module-load-ordering requirement in JSDoc
- [ ] T023 [US2] Create test proto fixture for gRPC streaming tests: a simple `.proto` file with unary, server-streaming, client-streaming, and bidirectional-streaming service definitions under `common/js/grpc/otel/src/` or as a test resource; use `@grpc/proto-loader` for dynamic loading in tests
- [ ] T024 [US2] Create comprehensive gRPC instrumentation tests in `common/js/grpc/otel/src/index.test.ts`: register instrumentation before `@grpc/grpc-js` import; use in-memory `InMemorySpanExporter` with `SimpleSpanProcessor`; for each RPC type (unary, server-streaming, client-streaming, bidirectional-streaming): create real gRPC client/server, make RPC call, verify client span and server span exist with `rpc.system = "grpc"`, `rpc.service`, `rpc.method` attributes, verify parent-child trace linkage, verify gRPC status code on completion; verify graceful no-op when no TracerProvider configured
- [ ] T025 [US2] Verify `bazel build //common/js/grpc/otel:lib` and `bazel test //common/js/grpc/otel:lib_test` pass

**Checkpoint**: gRPC-JS OTel instrumentation validates all four RPC types with real spans and trace propagation

---

## Phase 6: User Story 5 — Testplan Acceptance with Existing gRPC-TS Service (Priority: P3)

**Goal**: Update `experimental/ts/grpc_hello_world/` to consume the new common JS packages and prove deployed service acceptance through the existing testplan workflow and queryable logs

**Independent Test**: `guitar validate` + `guitar run` for `experimental/ts/grpc_hello_world/testplan/interface_test.yaml` passes; SigNoz/log query finds structured SayHello log for `grpc-hello-world-ts/service` in the generated test environment

### Implementation for User Story 5

- [ ] T026 [US5] Update `experimental/ts/grpc_hello_world/package.json` to add workspace dependencies for `@dominion/common-js-logs`, `@dominion/common-js-grpc-otel`, and `@dominion/common-js-otel` via `workspace:*` protocol
- [ ] T027 [US5] Update `experimental/ts/grpc_hello_world/BUILD.bazel` to add `deps` entries for the common JS packages while preserving existing `:server`, `:server_pkg`, and `:cmd_image` targets
- [ ] T028 [US5] Update `experimental/ts/grpc_hello_world/src/server.ts`: import and call OTel `init()` with `createGrpcInstrumentation()` in the instrumentation array before any `@grpc/grpc-js` import (split into OTel bootstrap + server module if static imports prevent correct load order); use `info()` from logging package at startup and in `SayHello` handler with structured attributes (`rpc.service`, `rpc.method`, request name); register `shutdown()` on process termination signals
- [ ] T029 [US5] Verify `bazel build //experimental/ts/grpc_hello_world:server //experimental/ts/grpc_hello_world:server_pkg //experimental/ts/grpc_hello_world:cmd_image` succeeds
- [ ] T030 [US5] Validate the testplan: run `guitar validate experimental/ts/grpc_hello_world/testplan/interface_test.yaml` and confirm validation passes
- [ ] T031 [US5] Execute the testplan: run `guitar run experimental/ts/grpc_hello_world/testplan/interface_test.yaml` and capture the generated test environment from output
- [ ] T032 [US5] Query SigNoz logs for `service.name = 'grpc-hello-world-ts/service'` and `deployment.environment.name = '<generated env>'` within the test run window; verify at least one structured log from the SayHello request is present

**Checkpoint**: Deployed TypeScript gRPC service proves common JS packages work end-to-end with queryable observability evidence

---

## Phase 7: User Story 4 — Repository-Wide Build Consistency (Priority: P3)

**Goal**: Ensure the entire repository builds and tests successfully after all changes, with no regressions and all governance rules satisfied

**Independent Test**: `bazel build //...` and `bazel test //...` pass at repository root

### Verification for User Story 4

- [ ] T033 [US4] Run `bazel run @pnpm -- --dir /mnt/code/dominion install` to sync lockfile after all `package.json` changes
- [ ] T034 [US4] Run `bazel run //:gazelle` for Gazelle-managed BUILD files (Go, Python); TS BUILD files are manual per repository convention
- [ ] T035 [P] [US4] Verify all new `package.json` dependency versions use `"catalog:"` protocol; document any special direct-version exceptions if present
- [ ] T036 [US4] Run `bazel build //...` for full repository build verification
- [ ] T037 [US4] Run `bazel test //...` for full repository test verification

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: Final quality gates and code quality review

- [ ] T038 Run `quickstart.md` validation: verify all package imports, API examples, and verification commands from `specs/004-js-common-otel-logs/quickstart.md` work correctly
- [ ] T039 Confirm Code Quality Review covers test-code review (tests match development goal and test plan) and style review (changed code follows `style/` guides and Google TypeScript Style Guide)
- [ ] T040 Verify no JS bootstrap package was created (`common/js/bootstrap` MUST NOT exist per FR-012)
- [ ] T041 Verify no OTLP gRPC exporter is used anywhere in the common JS packages (only HTTP exporters per design constraint)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately
- **Foundational (Phase 2)**: Depends on Phase 1 completion — BLOCKS all user stories
- **US1 (Phase 3)**: Depends on Phase 2 (vitest pattern proven)
- **US3 (Phase 4)**: Depends on Phase 2 (vitest pattern proven); independent of US1
- **US2 (Phase 5)**: Depends on Phase 2 (vitest pattern proven); independent of US1 and US3
- **US5 (Phase 6)**: Depends on Phase 3 (US1: otel provider), Phase 4 (US3: logging), and Phase 5 (US2: grpc otel)
- **US4 (Phase 7)**: Depends on all implementation phases (3–6)
- **Polish (Phase 8)**: Depends on Phase 7

### User Story Dependencies

- **US1 (P1)**: Depends on Foundational — provides OTel provider package
- **US3 (P2)**: Depends on Foundational — can start after Phase 2, parallel with US1
- **US2 (P2)**: Depends on Foundational — can start after Phase 2, parallel with US1 and US3
- **US5 (P3)**: Depends on US1 + US2 + US3 — integrates all packages into the service
- **US4 (P3)**: Depends on all stories — validates the whole repository

### Within Each User Story

- Package scaffold before implementation
- Implementation files before barrel exports
- Barrel exports before tests
- Tests before build/test verification
- Story complete before moving to dependent stories

### Parallel Opportunities

- T001 and T002 (Phase 1): different files
- T013, T014, T015 (Phase 4, US3): different implementation files in same package
- Phase 3 (US1) and Phase 4 (US3) and Phase 5 (US2): all three user stories are independent after Foundational
- T035, T038, T039, T040, T041 (Phase 7/8): verification tasks on different concerns

---

## Parallel Example: User Stories 1, 2, and 3

```text
# After Phase 2 (Foundational) completes, launch all three stories in parallel:

# Track A — US1 (OTel Provider Package):
Task T008: "Create common/js/otel/ package scaffold"
Task T009: "Implement OTel provider init in common/js/otel/src/index.ts"
Task T010: "Create unit tests in common/js/otel/src/index.test.ts"
Task T011: "Verify bazel build/test for common/js/otel"

# Track B — US3 (Logging Package):
Task T012: "Create common/js/logs/ package scaffold"
Tasks T013, T014, T015 (parallel): "Implement logger.ts, reporter.ts, context.ts"
Task T016: "Create barrel export index.ts"
Tasks T017, T018, T019 (parallel): "Create test files for reporter, logger, context"
Task T020: "Verify bazel build/test for common/js/logs"

# Track C — US2 (gRPC OTel Package):
Task T021: "Create common/js/grpc/otel/ package scaffold"
Task T022: "Implement gRPC instrumentation wrapper"
Task T023: "Create test proto fixture"
Task T024: "Create gRPC streaming tests (all 4 RPC types)"
Task T025: "Verify bazel build/test for common/js/grpc/otel"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL — blocks all stories)
3. Complete Phase 3: User Story 1 (OTel provider package)
4. **STOP and VALIDATE**: Verify OTel provider package builds and tests pass
5. Service can already initialize OTel with this package

### Incremental Delivery

1. Complete Setup + Foundational → Foundation ready
2. Add US1 (OTel provider) → Test independently → Deployable for OTel init
3. Add US3 (Logging) → Test independently → Console logging + OTel reporter available
4. Add US2 (gRPC OTel) → Test independently → gRPC tracing available
5. Add US5 (Service integration) → Test via testplan → Full acceptance with queryable logs
6. Add US4 (Build consistency) → Full repo validation
7. Each story adds value without breaking previous stories

### Parallel Team Strategy

With multiple developers:

1. Team completes Setup + Foundational together
2. Once Foundational is done:
   - Developer A: User Story 1 (OTel provider)
   - Developer B: User Story 3 (Logging)
   - Developer C: User Story 2 (gRPC OTel)
3. After all three stories complete:
   - Developer A: User Story 5 (Service integration + testplan)
   - Developer B: User Story 4 (Build consistency) + Polish

---

## Notes

- [P] tasks = different files, no dependencies on incomplete work within the same phase
- [Story] label maps task to specific user story for traceability
- US2 and US3 are both P2 priority and independent — implement either first or in parallel
- OTel provider package (US1) is P1 but only blocks US5, not US2 or US3
- Module load order is critical: OTel instrumentation MUST be registered before `@grpc/grpc-js` is loaded
- OTLP gRPC exporters are prohibited — use HTTP exporters only
- No JS bootstrap package is created in this feature (FR-012)
- Tests use vitest under Bazel, established by the event sub-package in Phase 2
- gRPC streaming tests require real in-process client/server, not mocks
- Final testplan acceptance requires SigNoz/log query verification, not just HTTP response
- Final validation must prove delivered behavior through the artifact's real surface
- Do not mark testplan tasks skipped unless the blocker and residual risk are documented
