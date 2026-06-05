# Research: JavaScript Common Library with OTel gRPC-JS Support & Structured Logging

**Branch**: `004-js-common-otel-logs` | **Date**: 2026-06-04

## Decision 1: gRPC-JS OTel Instrumentation Approach

**Decision**: Use the official `@opentelemetry/instrumentation-grpc` package instead of writing custom gRPC interceptors.

**Rationale**:
- The official package (`@opentelemetry/instrumentation-grpc`, v0.218.0) provides production-quality instrumentation for `@grpc/grpc-js` via module patching.
- It automatically creates spans with `rpc.system = "grpc"`, `rpc.service`, `rpc.method`, and `rpc.grpc.status_code` attributes.
- It handles trace context propagation via gRPC metadata.
- It covers unary, client-streaming, server-streaming, and bidirectional-streaming RPCs; this feature will validate all four types with real in-process gRPC-JS tests.
- Consumers register the instrumentation through the OTel SDK's `instrumentations` array, avoiding per-service handler wiring.

**Alternatives considered**:
- **Custom gRPC interceptors** (`ServerInterceptingCall` / `InterceptorProvider`): Rejected because they require explicit wiring in every consumer, duplicate span/context/status logic, and increase risk for streaming RPC correctness.
- **Custom `Server.prototype.register` patching**: Rejected because it would duplicate upstream instrumentation behavior.

**Critical constraint**: `@grpc/grpc-js` must NOT be loaded before instrumentation is registered. This means:
- Use OTLP HTTP/protobuf exporters for traces, not OTLP gRPC exporters, because the gRPC exporter loads `@grpc/grpc-js` internally.
- Tests must isolate module load order so instrumentation is active before `@grpc/grpc-js` is imported.

**Our library's role**: The `common/js/grpc/otel` package will:
1. Re-export `GrpcInstrumentation` from `@opentelemetry/instrumentation-grpc`.
2. Provide a pre-configured `createGrpcInstrumentation(config?)` convenience wrapper.
3. Document the module-load-ordering requirement.
4. Validate unary, server-streaming, client-streaming, and bidirectional-streaming behavior.

## Decision 2: OTel Logs Bridge for JavaScript

**Decision**: Use `@opentelemetry/api-logs` and `@opentelemetry/sdk-logs` directly to build the logging bridge, matching the behavior of Go's `otelslog` path without copying Go API shapes.

**Rationale**:
- The OTel Logs SDK for JavaScript (`@opentelemetry/sdk-logs`, v0.218.0) is still Development/Experimental, but it is the direct OTel logs bridge for JavaScript.
- The API surface maps to the Go behavior: a global LoggerProvider supplies loggers that emit structured records.
- A thin bridge avoids adding a heavy framework dependency such as Winston or Pino solely for OTel routing.
- The logging package can keep Go-aligned semantics (console default, reporter install/uninstall, log level filtering) while exposing `info('message', { key: value })` as the primary JS/TS call shape.

**Alternatives considered**:
- **Winston/Pino with OTel transport**: Rejected because it introduces a logging framework dependency when the feature only requires a common facade and OTel bridge.
- **Console-only logging**: Rejected because deployed services need remote log routing when a LoggerProvider is registered.

**Severity mapping**:

| Level | `SeverityNumber` | Value |
|-------|------------------|-------|
| `debug` | `DEBUG` | 5 |
| `info` | `INFO` | 9 |
| `warn` | `WARN` | 13 |
| `error` | `ERROR` | 17 |

## Decision 3: Common JS Library Structure and Build System

**Decision**: Create `common/js/` as the JS common library root with per-package directories, using the existing `ts_project` + SWC + CommonJS + ES2020 + strict pattern established by `experimental/ts/` projects.

**Rationale**:
- The user explicitly selected `common/js/` over the earlier proposed JS package path.
- The path is concise and clearly identifies JavaScript/TypeScript shared code under `common/`.
- Per-package directories align with the Go common library's ownership model and keep Bazel/package dependencies explicit.
- `pnpm-workspace.yaml` needs `common/js/*` added to the packages list.
- Each package needs `BUILD.bazel`, `package.json`, `tsconfig.json`, and `.swcrc`.

**Alternatives considered**:
- **Earlier longer JS package path proposal**: Rejected because the user requested `common/js/`.
- **Monolithic `common/js/package.json`**: Rejected because separate packages better match existing common-library boundaries (`logs`, `grpc/otel`, `otel`) and make Bazel deps clearer.
- **Using `experimental/ts/`**: Rejected because this is shared infrastructure, not demo code.

**Package naming**: Use `@dominion/common-js-logs`, `@dominion/common-js-logs-event`, `@dominion/common-js-grpc-otel`, and `@dominion/common-js-otel`.

## Decision 4: Logging Package API Design

**Decision**: Align with Go `common/gopkg/logs` behavior while keeping the TypeScript API idiomatic.

**Rationale**:
- Developers should recognize the behavior: console fallback, OTel reporter install, uninstall restoring previous behavior, structured fields, LOG_LEVEL filtering.
- JS/TS users should not be forced into Go-shaped APIs such as a required `Context` first parameter or PascalCase function names.
- Object-style attributes are the natural JS/TS default; event helpers can remain as optional explicit constructors.

**Mapping from Go behavior to TypeScript API**:

| Go (`common/gopkg/logs`) | TypeScript (`common/js/logs`) |
|--------------------------|-------------------------------|
| `logs.Info(ctx, msg, ...event)` | `logs.info(msg, attrs?, ...events)` |
| `logs.Error(ctx, msg, ...event)` | `logs.error(msg, attrs?, ...events)` |
| `logs.Warn(ctx, msg, ...event)` | `logs.warn(msg, attrs?, ...events)` |
| `logs.Debug(ctx, msg, ...event)` | `logs.debug(msg, attrs?, ...events)` |
| `logs.Default()` | `logs.defaultLogger()` |
| `logs.FromContext(ctx)` | `logs.currentLogger()` |
| `logs.With(ctx, ...event)` | `logs.withAttributes(attrs, fn)` |
| `logs.WithLogger(ctx, logger)` | `logs.withLogger(logger, fn)` |
| `logs.InstallReporter(logger)` | `logs.installReporter(reporter)` |
| `logs.NewOTelReporter(name)` | `logs.newOTelReporter(name)` |

**Key differences from Go**:
- No required Go-style `context.Context` argument.
- Trace context should use the active OTel JS context and AsyncLocalStorage when needed.
- Lifecycle APIs exposed by `common/js/otel` should be Promise-based.
- Event helpers use camelCase and are optional because object attributes are primary.

## Decision 5: Test Framework and Streaming Coverage

**Decision**: Use `vitest` for package-level unit tests, add in-process gRPC-JS tests for all RPC types in the gRPC OTel package, and use the existing `experimental/ts/grpc_hello_world` testplan for deployed service acceptance.

**Rationale**:
- `vitest` is already in the root catalog.
- The repository does not yet have a TS test target pattern, so `common/js/logs/event` should establish the smallest vitest-under-Bazel pattern first.
- Constructor-only tests for `createGrpcInstrumentation()` are insufficient for the updated feature. Tests must observe real spans from real gRPC-JS calls.
- The user requires service acceptance through the repository testplan and post-run log query, so package tests are necessary but not sufficient.

**Required gRPC test cases**:
1. Unary RPC creates client/server spans and propagates trace context.
2. Server-streaming RPC creates client/server spans and records final status.
3. Client-streaming RPC creates client/server spans and records final status.
4. Bidirectional-streaming RPC creates client/server spans and records final status.

**Alternatives considered**:
- **Only test wrapper construction**: Rejected because it would not prove instrumentation behavior or streaming support.
- **Only in-process unit/integration tests**: Rejected because they do not prove deployed service behavior or remote log export.

## Decision 6: JS Bootstrap Integration Scope

**Decision**: Do not add JS bootstrap integration in this feature.

**Rationale**:
- The user explicitly deferred bootstrap for JS.
- There is no established JS bootstrap package in the repository to integrate with.
- Adding bootstrap now would create speculative API surface beyond the common logging/OTel/gRPC instrumentation scope.

**Alternatives considered**:
- **Mirror Go `common/gopkg/otel/bootstrap.go` now**: Rejected because it is out of scope and would prematurely define JS service lifecycle conventions.

## Decision 7: Service-Level Acceptance Through Existing gRPC-TS Testplan

**Decision**: Use the existing `experimental/ts/grpc_hello_world/` service and `experimental/ts/grpc_hello_world/testplan/interface_test.yaml` as the service-level acceptance surface.

**Rationale**:
- The user explicitly requested this service and testplan as the acceptance path.
- Repository style requires gRPC service acceptance through `testplan`/guitar rather than only unit tests.
- `experimental/ts/grpc_hello_world/testplan/` already deploys the TypeScript gRPC service and a suite-owned Go grpc-gateway adapter, exposes HTTP on `apitest.liukexin.com`, and verifies `SayHello` through `common/gopkg/testtool`.
- This path proves the common JS packages work when consumed by a deployed service artifact, not only in in-process tests.

**Acceptance workflow**:
1. Update `experimental/ts/grpc_hello_world/src/server.ts` so OTel initialization and `createGrpcInstrumentation()` happen before `@grpc/grpc-js` loads.
2. Emit a deterministic structured log from the TypeScript service during `SayHello`.
3. Run `guitar validate experimental/ts/grpc_hello_world/testplan/interface_test.yaml`.
4. Run `guitar run experimental/ts/grpc_hello_world/testplan/interface_test.yaml`.
5. Query logs for `service.name = 'grpc-hello-world-ts/service'` and the generated test environment (`deployment.environment.name`) in the test run window.

**Alternatives considered**:
- **Create a new acceptance service**: Rejected because `experimental/ts/grpc_hello_world/` already provides the correct gRPC-JS service and testplan material.
- **Skip log query after testplan**: Rejected because the user explicitly requires logs to be queryable after the testplan passes.
