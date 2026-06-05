# Implementation Plan: JavaScript Common Library with OTel gRPC-JS Support & Structured Logging

**Branch**: `004-js-common-otel-logs` | **Date**: 2026-06-04 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/004-js-common-otel-logs/spec.md`

## Summary

Create a JavaScript/TypeScript common library under `common/js/` that aligns with the behavior of the existing Go common packages while exposing idiomatic JS/TS APIs. The library provides three packages: (1) `common/js/logs`, a structured logging package that defaults to console output and routes through OTel when `installReporter` installs an OTel reporter; (2) `common/js/grpc/otel`, a wrapper around `@opentelemetry/instrumentation-grpc` that supports unary, server-streaming, client-streaming, and bidirectional-streaming gRPC-JS tracing; and (3) `common/js/otel`, an explicit OTel provider initialization package with Promise-based lifecycle APIs. JS bootstrap integration is out of scope. Service-level acceptance uses the existing `experimental/ts/grpc_hello_world/` service and its repository testplan; after the testplan passes, SigNoz/log tooling must be able to query a structured log emitted by `grpc-hello-world-ts/service` for the generated test environment.

## Technical Context

**Language/Version**: TypeScript 6.x (from root package.json), ES2020 target, CommonJS modules

**Primary Dependencies**:
- `@opentelemetry/api` ^1.9.0 — OTel tracing/metrics API
- `@opentelemetry/api-logs` ^0.218.0 — OTel logs bridge API
- `@opentelemetry/sdk-logs` ^0.218.0 — OTel logs SDK (LoggerProvider, processors)
- `@opentelemetry/sdk-trace-node` ^2.7.0 — Node.js TracerProvider
- `@opentelemetry/sdk-trace-base` ^2.7.0 — in-memory span export for unit tests
- `@opentelemetry/sdk-metrics` ^2.7.0 — MeterProvider
- `@opentelemetry/instrumentation-grpc` ^0.218.0 — gRPC-JS auto-instrumentation
- `@opentelemetry/semantic-conventions` ^1.29.0 — Standard attribute names
- `@opentelemetry/resources` ^2.7.0 — Resource detection
- `@opentelemetry/exporter-trace-otlp-http` ^0.57.0 — OTLP trace exporter (HTTP, NOT gRPC)
- `@opentelemetry/exporter-metrics-otlp-http` ^0.57.0 — OTLP metrics exporter (HTTP)
- `@opentelemetry/exporter-logs-otlp-http` ^0.218.0 — OTLP logs exporter (HTTP)
- `@grpc/grpc-js` and `@grpc/proto-loader` — test-only gRPC client/server fixtures
- `@aspect/rules_ts` 3.8.8, `@aspect/rules_js` 3.0.3, `@aspect/rules_swc` 2.7.1 — existing Bazel TS toolchain
- `vitest` ^3.2.4 — test framework (already in catalog)

**Storage**: N/A (library packages only)

**Testing**: Unit tests with vitest for all packages. `common/js/grpc/otel` tests must create a real in-process gRPC-JS client/server and validate unary, server-streaming, client-streaming, and bidirectional-streaming spans using an in-memory span exporter. Final acceptance uses `guitar validate` and `guitar run` against `experimental/ts/grpc_hello_world/testplan/interface_test.yaml`, then SigNoz/log query verification for `service.name = 'grpc-hello-world-ts/service'` and the generated test environment.

**Target Platform**: Node.js 24.14.0 (Linux, configured via MODULE.bazel)

**Project Type**: Shared library (common infrastructure)

**Performance Goals**: Logging overhead < 1ms per call when console reporter is active; near-zero application blocking when OTel reporter is active because export is asynchronous/batched

**Constraints**: No generated proto files committed; all npm deps via catalog; Gazelle does not generate TS BUILD files, so TS BUILD targets are manual; CommonJS output required for Node.js interop; OTLP gRPC exporters must NOT be used because they load `@grpc/grpc-js` before instrumentation can patch it; `@opentelemetry/instrumentation-grpc` must be registered before `@grpc/grpc-js` is loaded; JS bootstrap integration is out of scope

**Scale/Scope**: 3 packages under `common/js/`, ~15 source files plus vitest tests and gRPC streaming fixtures; update `experimental/ts/grpc_hello_world/` to use the packages for deployed acceptance

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Authority & Style**: PASS. Implementers must read `.specify/memory/constitution.md`, root `README.md`, `style/api.md`, and the Google TypeScript Style Guide referenced from `style/README.md`. APIs align with Go behavior but use JS/TS naming, object attributes, and Promise lifecycle conventions.
- **Bazel Integrity**: PASS. All compilation and tests run through Bazel. PNPM uses `bazel run @pnpm -- --dir /mnt/code/dominion`. TS BUILD files are manual because Gazelle is not configured for TS. Dependencies are added to the root `pnpm-workspace.yaml` catalog and referenced with `"catalog:"`.
- **Generated Files & Dependencies**: PASS. No generated proto files are committed. gRPC test protos are source fixtures only; generated TS output remains Bazel output.
- **Testing Strategy**: PASS. Tests are defined before implementation. Streaming RPC coverage is required for gRPC instrumentation. Because the feature now updates a gRPC service for acceptance, repository testplan execution is required using `experimental/ts/grpc_hello_world/testplan/interface_test.yaml`.
- **Behavioral Acceptance**: PASS. Validation covers zero-config console logging, OTel reporter routing, uninstall semantics, object-style structured attributes, unary and streaming gRPC span propagation, package builds, `experimental/ts/grpc_hello_world` testplan success, queryable service logs after the testplan, whole-repository build, and whole-repository tests.
- **Repository Verification**: PASS. Final plan includes `bazel build //...` and `bazel test //...`.
- **Testplan Execution**: PASS. Service-level acceptance uses the existing TypeScript gRPC demo testplan. The testplan deploys `experimental/ts/grpc_hello_world/service.yaml` and `experimental/ts/grpc_hello_world/testplan/gateway/service.yaml`, calls the HTTP gateway endpoint, and cleanup is handled by `guitar run`. Post-run observability verification queries logs for the generated test environment.

## Project Structure

### Documentation (this feature)

```text
specs/004-js-common-otel-logs/
├── plan.md
├── spec.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── logs-api.md
│   └── grpc-otel-api.md
└── checklists/
    └── requirements.md
```

### Source Code (repository root)

```text
common/js/
├── logs/
│   ├── BUILD.bazel
│   ├── package.json                  # @dominion/common-js-logs
│   ├── tsconfig.json
│   ├── .swcrc
│   ├── src/
│   │   ├── index.ts
│   │   ├── logger.ts                 # info('message', attrs), object attributes, default logger
│   │   ├── reporter.ts               # Reporter, ConsoleReporter, OTelReporter, installReporter
│   │   └── context.ts                # AsyncLocalStorage / active OTel context helpers
│   └── event/
│       ├── BUILD.bazel
│       ├── package.json              # @dominion/common-js-logs-event
│       ├── tsconfig.json
│       ├── .swcrc
│       └── src/index.ts              # optional event helpers
├── grpc/otel/
│   ├── BUILD.bazel
│   ├── package.json                  # @dominion/common-js-grpc-otel
│   ├── tsconfig.json
│   ├── .swcrc
│   └── src/index.ts                  # GrpcInstrumentation re-export + createGrpcInstrumentation
└── otel/
    ├── BUILD.bazel
    ├── package.json                  # @dominion/common-js-otel
    ├── tsconfig.json
    ├── .swcrc
    └── src/index.ts                  # init, tracer, meter, traceId, isLoggerProviderSet, shutdown

experimental/ts/grpc_hello_world/
├── BUILD.bazel                       # add common/js package deps and preserve artifact targets
├── package.json                      # add common-js package deps via catalog/workspace packages
├── src/server.ts                     # initialize OTel before @grpc/grpc-js load; emit SayHello log
├── service.yaml                      # existing grpc-js service deployment material
└── testplan/
    ├── interface_test.yaml           # existing guitar testplan acceptance entrypoint
    ├── deploy.yaml                   # deploys TS service + gateway adapter
    ├── interface_test.go             # HTTP acceptance case using testtool
    └── gateway/                      # existing Go grpc-gateway adapter

Tests live beside source files as `*.test.ts`; `common/js/grpc/otel/src/index.test.ts`
must include real unary, server-streaming, client-streaming, and bidirectional-streaming
gRPC-JS calls. Service acceptance is not in-process: it uses the existing
`experimental/ts/grpc_hello_world/testplan/interface_test.yaml` guitar workflow.
```

**Updated existing files**:
- `pnpm-workspace.yaml` — add `common/js/*` package glob and OTel catalog entries
- `.bazelignore` — add `common/js/*/node_modules`

## Complexity Tracking

> No constitution violations detected. All gates pass. The main implementation complexity is establishing the repository's first vitest-under-Bazel pattern, ensuring gRPC instrumentation tests isolate module load order correctly, and updating the existing TypeScript gRPC service so its deployed testplan proves logs are exported and queryable.

---

## Implementation Phases

### Phase A: Dependency Setup and Workspace Configuration

**Goal**: Add required npm dependencies and package discovery.

**Files to modify**:
- `pnpm-workspace.yaml` — add `common/js/*` and catalog entries for OTel, gRPC test fixtures, and vitest support
- `.bazelignore` — add `common/js/*/node_modules`

**Verification**: `bazel run @pnpm -- --dir /mnt/code/dominion install` succeeds; `pnpm-workspace.yaml` contains `common/js/*`.

### Phase B: Establish TS Test Pattern + Event Sub-Package

**Goal**: Create `common/js/logs/event` and prove the repository's vitest-under-Bazel pattern with the smallest package first.

**Files to create**:
- `common/js/logs/event/package.json` — `@dominion/common-js-logs-event`
- `common/js/logs/event/tsconfig.json`
- `common/js/logs/event/.swcrc`
- `common/js/logs/event/BUILD.bazel` — `ts_project` plus vitest test target
- `common/js/logs/event/src/index.ts` — optional `Event` type and constructors
- `common/js/logs/event/src/index.test.ts` — constructors and zero-value behavior

**Verification**: `bazel build //common/js/logs/event:lib` and `bazel test //common/js/logs/event:lib_test` pass.

### Phase C: Logging Package Core

**Goal**: Create `common/js/logs` with idiomatic JS/TS structured logging and Go-aligned reporter semantics.

**Files to create**:
- `common/js/logs/package.json` — `@dominion/common-js-logs`
- `common/js/logs/tsconfig.json`, `.swcrc`, `BUILD.bazel`
- `common/js/logs/src/index.ts`
- `common/js/logs/src/event.ts`
- `common/js/logs/src/logger.ts` — `info('message', attrs)`, `warn`, `error`, `debug`, `defaultLogger`
- `common/js/logs/src/reporter.ts` — `Reporter`, `ConsoleReporter`, `OTelReporter`, `installReporter`, `newOTelReporter`
- `common/js/logs/src/context.ts` — `currentLogger`, `withAttributes`, `withLogger`
- `common/js/logs/src/*.test.ts` — console fallback, OTel reporter routing, uninstall-only-own, LOG_LEVEL, object attributes, optional event helpers, async scoped attributes

**Key implementation details**:
- Public API is camelCase and object-attribute first: `info('message', { userId })`.
- Event helpers are optional convenience helpers, not the primary logging shape.
- OTel trace context comes from active OTel JS context, not a required Go-style `Context` first parameter.
- `installReporter` preserves Go semantics: latest reporter wins, uninstall only removes its own reporter.

**Verification**: `bazel build //common/js/logs:lib` succeeds; all logging unit tests pass.

### Phase D: gRPC-JS OTel Instrumentation Package

**Goal**: Create `common/js/grpc/otel` that wraps `@opentelemetry/instrumentation-grpc` and validates all RPC types.

**Files to create**:
- `common/js/grpc/otel/package.json` — `@dominion/common-js-grpc-otel`
- `common/js/grpc/otel/tsconfig.json`, `.swcrc`, `BUILD.bazel`
- `common/js/grpc/otel/src/index.ts` — `GrpcInstrumentation` re-export and `createGrpcInstrumentation(config?)`
- `common/js/grpc/otel/src/index.test.ts` — real gRPC-JS client/server tests for unary, server-streaming, client-streaming, bidirectional-streaming

**Key implementation details**:
- Tests must register instrumentation before importing/loading `@grpc/grpc-js`.
- Tests should use in-memory span export to assert client/server spans, attributes, status, and parent-child trace propagation.
- OTLP gRPC exporters remain prohibited; use HTTP exporters in provider code.

**Verification**: `bazel build //common/js/grpc/otel:lib` succeeds; `bazel test //common/js/grpc/otel:lib_test` proves all four RPC types.

### Phase E: OTel Provider Initialization Package

**Goal**: Create `common/js/otel` for explicit OTel initialization and shutdown.

**Files to create**:
- `common/js/otel/package.json` — `@dominion/common-js-otel`
- `common/js/otel/tsconfig.json`, `.swcrc`, `BUILD.bazel`
- `common/js/otel/src/index.ts` — `init`, `tracer`, `meter`, `traceId`, `isLoggerProviderSet`, Promise-based `shutdown`
- `common/js/otel/src/index.test.ts` — deploy/non-deploy initialization, mocked HTTP exporters, logs reporter install/uninstall, Promise shutdown semantics

**Key implementation details**:
- `isDeploy()` checks `SERVICE_APP`, `DOMINION_ENVIRONMENT`, and `POD_NAMESPACE`.
- Deploy mode uses OTLP HTTP exporters and default collector HTTP endpoint, not OTLP gRPC.
- Non-deploy mode creates local tracing only and keeps logs on console.
- No JS bootstrap package, component, or adapter is created.

**Verification**: `bazel build //common/js/otel:lib` succeeds; unit tests pass.

### Phase F: Integrate Existing TypeScript gRPC Service for Acceptance

**Goal**: Update `experimental/ts/grpc_hello_world/` to consume the new common JS packages and emit observable logs during the existing testplan.

**Files to modify**:
- `experimental/ts/grpc_hello_world/package.json` — add workspace/catalog dependencies for `@dominion/common-js-logs`, `@dominion/common-js-grpc-otel`, and `@dominion/common-js-otel`
- `experimental/ts/grpc_hello_world/BUILD.bazel` — add TS deps for the common JS packages while preserving `:server`, `:server_pkg`, and `:cmd_image`
- `experimental/ts/grpc_hello_world/src/server.ts` — initialize `common/js/otel` with `createGrpcInstrumentation()` before `@grpc/grpc-js` is loaded, use `common/js/logs.info()` in startup and `SayHello`, and shutdown cleanly on process termination
- `experimental/ts/grpc_hello_world/testplan/interface_test.go` — if needed, keep HTTP assertion behavior and ensure the request path triggers a service log with structured request fields

**Key implementation details**:
- Module load order is critical: OTel setup must run before `@grpc/grpc-js` is imported. If the current `server.ts` static import prevents this, split startup into an OTel bootstrap module plus a server module loaded after initialization.
- The service should emit a deterministic structured log for `SayHello`, including fields such as `rpc.service`, `rpc.method`, and request name, without logging secrets.
- Keep the existing testplan surface: `experimental/ts/grpc_hello_world/testplan/interface_test.yaml`, `deploy.yaml`, gateway adapter, and `go_largetest` HTTP case.

**Verification**: `bazel build //experimental/ts/grpc_hello_world:server //experimental/ts/grpc_hello_world:cmd_image //experimental/ts/grpc_hello_world/testplan:testplan_test` succeeds.

### Phase G: Testplan and Observability Acceptance

**Goal**: Prove the delivered feature through the deployed TypeScript gRPC service and queryable logs.

**Steps**:
1. Install/ensure deploy and guitar tools: `bazel run //:deploy_install` and `bazel run //:guitar_install` when needed.
2. Validate the testplan: `guitar validate experimental/ts/grpc_hello_world/testplan/interface_test.yaml`.
3. Run the testplan: `guitar run experimental/ts/grpc_hello_world/testplan/interface_test.yaml`.
4. Capture the generated test environment from the guitar/test output (`TESTTOOL_ENV` / generated suite environment).
5. Query SigNoz/logs for `service.name = 'grpc-hello-world-ts/service'` and `deployment.environment.name = '<generated env>'` within the test run window; verify the structured SayHello log is present.
6. Run `bazel run @pnpm -- --dir /mnt/code/dominion install`, `bazel run //:gazelle` for Gazelle-managed files only, `bazel build //...`, and `bazel test //...`.

**Verification**: Testplan validation passes, testplan execution completes deploy/test/cleanup, logs are queryable for the generated environment, and repository build/test commands succeed or any pre-existing blocker is documented with remaining validation risk.

---

## Step-by-Step Implementation Guide

1. Add `common/js/*` and catalog dependencies to `pnpm-workspace.yaml`; add `common/js/*/node_modules` to `.bazelignore`.
2. Establish vitest-under-Bazel with `common/js/logs/event` before adding larger packages.
3. Implement `common/js/logs/event`.
4. Implement `common/js/logs` with object-style logging and reporter semantics.
5. Implement `common/js/grpc/otel` and its unary plus streaming gRPC-JS instrumentation tests.
6. Implement `common/js/otel` with explicit Promise lifecycle APIs and no bootstrap integration.
7. Update `experimental/ts/grpc_hello_world/` to initialize OTel/logging and emit a structured SayHello log.
8. Run `guitar validate` and `guitar run` for `experimental/ts/grpc_hello_world/testplan/interface_test.yaml`.
9. Query logs for `grpc-hello-world-ts/service` in the generated test environment.
10. Run full repository verification.

---

## Acceptance Verification

| ID | Verification | How |
|----|-------------|-----|
| FR-001 | JS common library directory under common/ | `common/js/` exists with logs/, grpc/otel/, otel/ packages |
| FR-002 | Follows TS conventions | ts_project + SWC + CommonJS + ES2020 + strict + @dominion/ namespace + catalog deps |
| FR-003 | gRPC-JS OTel instrumentation package | `common/js/grpc/otel/` exports `createGrpcInstrumentation` + `GrpcInstrumentation` |
| FR-004 | Spans with standard attributes | gRPC tests verify unary and streaming spans include `rpc.system`, `rpc.service`, `rpc.method`, status |
| FR-005 | Trace context propagation | gRPC tests verify client/server parent-child linkage for unary and all streaming RPC types |
| FR-006 | Structured logging with levels | `common/js/logs/` exports `info/warn/error/debug` with object attributes and optional event helpers |
| FR-007 | Console output by default | Unit test verifies `info('message')` console output when no reporter installed |
| FR-008 | installReporter pattern | Unit test verifies OTel routing when reporter installed |
| FR-009 | Uninstall callback | Unit test verifies uninstall restores console and only uninstalls its own reporter |
| FR-010 | Full repo build + test | `bazel build //...` and `bazel test //...` |
| FR-011 | Catalog-based deps | All package.json dependency versions use `"catalog:"` |
| FR-012 | No JS bootstrap | No `common/js/bootstrap` package and no bootstrap adapter in `common/js/otel` |
| FR-013 | Idiomatic JS/TS API | Contract and tests use camelCase, object attributes, and Promise lifecycle operations |
| FR-014 | Existing TS gRPC service consumes common JS packages | `experimental/ts/grpc_hello_world/src/server.ts` initializes OTel/logs and BUILD/package deps include common JS packages |
| FR-015 | Testplan-based acceptance | `guitar validate` and `guitar run experimental/ts/grpc_hello_world/testplan/interface_test.yaml` pass |
| FR-016 | Logs queryable after testplan | SigNoz/log query finds the structured SayHello service log for `grpc-hello-world-ts/service` and generated test environment |
| SC-001 | Zero-config logging | Tests verify `info('message')` works immediately with console output |
| SC-002 | OTel spans from unary and streaming gRPC calls | In-memory span tests cover unary, server-streaming, client-streaming, bidirectional-streaming |
| SC-003 | Zero regressions | `bazel build //...` and `bazel test //...` |
| SC-004 | Go-aligned but JS-idiomatic structure | `common/js/` layout aligns with `common/gopkg/` intent while using JS/TS package names and APIs |
| SC-005 | gRPC-TS testplan acceptance | Existing testplan verifies `Hello World` through the deployed gateway-backed HTTP endpoint |
| SC-006 | Queryable service logs | Post-testplan SigNoz/log query returns the structured SayHello log within 10 minutes of the call |
