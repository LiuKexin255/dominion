# Feature Specification: JavaScript Common Library with OTel gRPC-JS Support & Structured Logging

**Feature Branch**: `004-js-common-otel-logs`

**Created**: 2026-06-04

**Status**: Draft

**Input**: User description: "1. 在 common 下创建一个 js 的公共库。2. 为 grpc-js 提供 otel 支持，覆盖 unary 与 streaming RPC。3. 在 js 公共下提供一个日志包，逻辑与 golang 的 logs 包类似，如果注册了 otel provider 则将日志打印到远端，否则打印到控制台。4. 公共库目录为 common/js。5. JS 先不增加 bootstrap 相关内容。6. 行为参考 Go，但 API 应保持 JS/TS 风格和习惯。7. 使用 experimental/ts/grpc_hello_world/ 作为测试所用 grpc-ts 服务，验收通过 testplan 进行，testplan 通过后应能查询到日志。"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - JavaScript Common Library Foundation (Priority: P1)

A developer needs a shared JavaScript/TypeScript common library under `common/js/` that provides reusable infrastructure packages while aligning behavior with the existing Go common library (`common/gopkg/`). The library must be buildable through the existing Bazel + pnpm + TypeScript tooling, follow the same project conventions (catalog dependencies, `@dominion/` namespace, `ts_project` + SWC, CommonJS, ES2020, strict), expose idiomatic JS/TS APIs, and serve as the foundation for all subsequent JS/TS packages in the common library.

**Why this priority**: The common library structure is the foundational prerequisite — the OTel gRPC-JS support and logging packages both depend on it existing first.

**Independent Test**: Can be fully tested by creating the package scaffold, verifying it builds through Bazel (`bazel build //common/js/...`), and confirming it follows the same conventions as `experimental/ts/hello_world` and `experimental/ts/grpc_hello_world`.

**Acceptance Scenarios**:

1. **Given** a developer wants to add a new shared JS/TS package, **When** they create a package under `common/js/`, **Then** it builds through the existing Bazel TS tooling without manual configuration changes beyond a `BUILD.bazel` file and `package.json`.
2. **Given** the common JS library structure, **When** a developer inspects the package manifests, **Then** all dependency versions are centralized in the root `pnpm-workspace.yaml` catalog using the `"catalog:"` protocol.

---

### User Story 2 - gRPC-JS OpenTelemetry Support (Priority: P2)

A developer building a gRPC-JS service wants automatic distributed tracing. They register the OTel gRPC-JS instrumentation from the common library before loading `@grpc/grpc-js`. Once registered, every inbound and outbound unary or streaming gRPC call automatically produces OpenTelemetry spans with correct parent-child relationships, trace context propagation, and standard gRPC semantic attributes (rpc.system, rpc.service, rpc.method). This aligns with the repository's Go observability behavior while using the JS OpenTelemetry instrumentation model.

**Why this priority**: gRPC-JS tracing is the primary observability gap for TypeScript services. Without it, TypeScript gRPC services are invisible to the distributed tracing pipeline, making debugging and performance analysis significantly harder.

**Independent Test**: Can be fully tested by writing tests that create a real gRPC client-server pair with instrumentation registered, make unary, server-streaming, client-streaming, and bidirectional-streaming RPC calls, and verify that spans are created with the correct attributes, trace propagation, and parent-child linkage.

**Acceptance Scenarios**:

1. **Given** a gRPC-JS server with instrumentation registered, **When** a client makes a unary RPC call, **Then** client and server spans are created with `rpc.system = "grpc"`, `rpc.service`, and `rpc.method` attributes.
2. **Given** a gRPC-JS service with instrumentation registered, **When** a server-streaming RPC completes, **Then** client and server spans preserve trace context and record the completed RPC status.
3. **Given** a gRPC-JS service with instrumentation registered, **When** a client-streaming RPC completes, **Then** client and server spans preserve trace context and record the completed RPC status.
4. **Given** a gRPC-JS service with instrumentation registered, **When** a bidirectional-streaming RPC completes, **Then** client and server spans preserve trace context and record the completed RPC status.
5. **Given** the gRPC-JS OTel package, **When** no global TracerProvider is configured, **Then** the instrumentation degrades gracefully without throwing errors (spans go to a no-op provider).

---

### User Story 3 - Structured Logging with OTel Reporter (Priority: P2)

A developer wants structured logging that automatically routes to the correct destination. When the application has registered an OTel LoggerProvider (deployed in-cluster), log records are sent to the remote OTel collector via the OTel Logs Bridge. When no OTel provider is registered (local development), log records are printed to the console in a human-readable format. This follows the Go `common/gopkg/logs` behavior while presenting an idiomatic JS/TS API: package-level `info/warn/error/debug` helpers accept a message and structured object attributes, with optional event helpers for callers that prefer explicit field constructors.

**Why this priority**: Consistent logging behavior across Go and TypeScript services is essential for operational observability. The JS logging package must follow the same reporter-installation behavior as the Go equivalent while keeping JS service initialization explicit and JS/TS-idiomatic.

**Independent Test**: Can be fully tested by unit tests that verify: (a) console output when no reporter is installed, (b) OTel bridge routing when a reporter is installed via `installReporter`, (c) reporter uninstall restores console behavior, (d) object-style structured attributes and optional event helpers both emit structured fields.

**Acceptance Scenarios**:

1. **Given** the logging package with no reporter installed, **When** a developer calls `info("message", { key: "value" })`, **Then** the log record is printed to the console with the message and structured attributes.
2. **Given** an OTel LoggerProvider has been registered via `installReporter`, **When** a developer calls `info("message", { key: "value" })`, **Then** the log record is routed to the OTel LoggerProvider instead of the console.
3. **Given** an installed reporter, **When** the uninstall function is called, **Then** subsequent log calls revert to console output.
4. **Given** a log call with structured event fields, **When** the record is emitted, **Then** all provided key-value pairs appear as structured attributes on the log record regardless of the output destination.

---

### User Story 4 - Repository-Wide Build Consistency (Priority: P3)

After adding the JS common library, OTel gRPC-JS support, and logging packages, the entire repository continues to build and test successfully. Existing Go and TypeScript targets are unaffected.

**Why this priority**: Regression safety — the new packages must not break anything that already works and must conform to all repository governance rules.

**Independent Test**: Can be fully tested by running `bazel build //...` and `bazel test //...` at the repository root and verifying all targets pass.

**Acceptance Scenarios**:

1. **Given** the completed feature, **When** the developer runs `bazel build //...` at the repository root, **Then** all targets (Go, TypeScript, and others) build successfully with zero failures.
2. **Given** the completed feature, **When** the developer runs `bazel test //...` at the repository root, **Then** all existing tests continue to pass at their prior status.
3. **Given** the new JS common packages, **When** the developer checks the dependency declarations, **Then** all runtime dependencies use the centralized catalog protocol (`"catalog:"`).

---

### User Story 5 - Testplan Acceptance with Existing gRPC-TS Service (Priority: P3)

A developer wants proof that the JS common OTel and logging packages work when used by a deployed TypeScript gRPC service. The existing `experimental/ts/grpc_hello_world/` service is updated to initialize the JS OTel provider, register gRPC instrumentation before `@grpc/grpc-js` loads, and emit structured logs from the service path. The existing repository testplan deploys the TypeScript gRPC service plus its Go grpc-gateway adapter, verifies the SayHello call through HTTP, and then operators can query the emitted logs for that test environment.

**Why this priority**: Unit tests prove package behavior, but the acceptance surface for a gRPC service is the repository testplan workflow. The deployed service must demonstrate that logging is exported in the same environment where the gRPC call succeeds.

**Independent Test**: Can be fully tested by running `guitar validate experimental/ts/grpc_hello_world/testplan/interface_test.yaml`, then `guitar run experimental/ts/grpc_hello_world/testplan/interface_test.yaml`, and finally querying logs for the deployed `grpc-hello-world-ts/service` in the generated test environment.

**Acceptance Scenarios**:

1. **Given** the updated `experimental/ts/grpc_hello_world/` service, **When** the testplan runs, **Then** it deploys the TypeScript gRPC service and suite-owned gateway adapter, calls `GET /experimental/ts/grpc-hello-world/say-hello?name=World`, and receives `Hello World`.
2. **Given** the testplan has passed, **When** an operator queries logs for `grpc-hello-world-ts/service` and the generated test environment, **Then** at least one structured log emitted by the TypeScript service for the SayHello request is available.
3. **Given** the TypeScript service initializes OTel for deployment, **When** the service starts under the testplan, **Then** gRPC instrumentation is registered before `@grpc/grpc-js` is loaded and no OTLP gRPC exporter is used.

---

### Edge Cases

- What happens when the OTel gRPC-JS instrumentation encounters server-streaming, client-streaming, or bidirectional-streaming RPCs? The instrumentation must create spans for streaming calls and propagate trace context correctly.
- What happens when the logging package is used before any initialization? It must default to console output without throwing errors.
- What happens when `installReporter` is called multiple times? The most recently installed reporter should be active; the previous reporter's uninstall function should correctly uninstall only its own reporter (matching the Go behavior).
- What happens when the OTel collector is unreachable? The instrumentation and logging bridge should not block the application — tracing and logging should degrade gracefully without crashing.
- What happens when the testplan passes but logs are not queryable for the generated environment? The feature is not accepted until either the missing log export is fixed or a documented external observability outage is recorded with remaining risk.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: A JavaScript/TypeScript common library directory MUST be created at `common/js/` that aligns with the role of `common/gopkg/` and provides shared packages for TypeScript services in the repository.
- **FR-002**: The common JS library MUST follow existing repository TypeScript conventions: `ts_project` compilation, SWC transpilation, CommonJS output, ES2020 target, strict mode, `@dominion/` package namespace, and catalog-based dependency management via the root `pnpm-workspace.yaml`.
- **FR-003**: A gRPC-JS OTel instrumentation package MUST be provided in the common JS library for automatic distributed tracing of unary and streaming gRPC calls.
- **FR-004**: The gRPC-JS OTel instrumentation MUST create spans with standard OpenTelemetry semantic attributes: `rpc.system = "grpc"`, `rpc.service`, `rpc.method`, and appropriate status codes.
- **FR-005**: The gRPC-JS OTel instrumentation MUST propagate trace context via gRPC metadata so that parent-child span relationships are preserved across service boundaries for unary, server-streaming, client-streaming, and bidirectional-streaming RPCs.
- **FR-006**: A structured logging package MUST be provided in the common JS library that supports leveled logging (debug, info, warn, error) with idiomatic JS/TS structured attributes.
- **FR-007**: The logging package MUST default to console output when no OTel reporter is installed, producing human-readable structured log lines.
- **FR-008**: The logging package MUST support an `installReporter` pattern that, when called with an OTel-backed reporter, routes all subsequent log records through the OTel LoggerProvider instead of the console — behaviorally matching the Go `common/gopkg/logs` reporter installation pattern while using JS/TS naming conventions.
- **FR-009**: The logging package MUST support an uninstall callback from `installReporter` that restores the previous (console) output behavior — matching the Go package's uninstall semantics.
- **FR-010**: The full repository MUST build (`bazel build //...`) and pass all tests (`bazel test //...`) after the changes are applied.
- **FR-011**: All new npm dependency versions MUST be declared in the root `pnpm-workspace.yaml` catalog and referenced via the `"catalog:"` protocol in package manifests.
- **FR-012**: The JS common library MUST NOT add bootstrap integration in this feature; no `common/js/bootstrap` package or JS equivalent of Go `common/gopkg/otel/bootstrap.go` is in scope.
- **FR-013**: The public API MUST preserve Go-aligned behavior but use idiomatic JS/TS shapes, including camelCase naming, object-style structured logging attributes, and Promise-based lifecycle operations where asynchronous initialization or shutdown is exposed.
- **FR-014**: The existing `experimental/ts/grpc_hello_world/` service MUST be updated to consume `common/js/otel`, `common/js/grpc/otel`, and `common/js/logs` for service-level acceptance.
- **FR-015**: Acceptance MUST use the existing `experimental/ts/grpc_hello_world/testplan/interface_test.yaml` testplan flow rather than only in-process unit tests.
- **FR-016**: After the testplan passes, logs for the TypeScript service MUST be queryable by service and generated test environment through the repository observability tooling.

### Key Entities

- **Logger**: The primary logging interface that provides leveled log methods (`info`, `warn`, `error`, `debug`) with structured attribute support. Can be used directly through package-level helpers or logger instances.
- **Reporter**: An abstraction for the log output destination. Console is the default reporter; an OTel-backed reporter can be installed and uninstalled at runtime.
- **Event**: A structured key-value pair used to attach attributes to log records (mirroring Go `event.Event` with `String`, `Int`, `Err` constructors).
- **gRPC Instrumentation**: OpenTelemetry instrumentation that observes gRPC-JS client and server calls with span creation and context propagation.
- **Testplan Acceptance Run**: A repository `guitar` run that deploys `experimental/ts/grpc_hello_world/`, verifies the gateway-backed HTTP call, cleans up, and leaves queryable observability evidence.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Developers can import and use the JS common logging package in a TypeScript service with zero manual configuration — calling `info("message")` works immediately with console output.
- **SC-002**: Unary and streaming gRPC-JS calls made after OTel instrumentation is registered produce valid distributed trace spans visible in the observability backend within 10 seconds of the call completing.
- **SC-003**: Zero build failures or test regressions are introduced across the entire repository.
- **SC-004**: The JS common library structure aligns with the same organizational intent as `common/gopkg/`, making it intuitive for developers already familiar with the Go common library while remaining idiomatic for JS/TS developers.
- **SC-005**: The `experimental/ts/grpc_hello_world` testplan completes successfully and verifies the deployed TypeScript gRPC service through the gateway-backed HTTP endpoint.
- **SC-006**: Within 10 minutes after the testplan call completes, at least one structured service log from `grpc-hello-world-ts/service` for the SayHello request can be queried for the generated test environment.

## Assumptions

- The JS common library directory will be `common/js/`.
- The gRPC-JS OTel instrumentation will use the OpenTelemetry SDK for JavaScript (`@opentelemetry/api`, `@opentelemetry/sdk-trace-base` or equivalent) already consistent with the OTel ecosystem.
- The logging package's OTel bridge will use the OpenTelemetry Logs API for JavaScript when routing to the remote collector.
- The packages target Node.js 24.14.0 (the runtime already configured in MODULE.bazel).
- Unit tests are required for the common library packages, and service-level acceptance is required through the existing `experimental/ts/grpc_hello_world/` testplan.
- The `common/js/` packages are compiled through the same Bazel TS tooling used by `experimental/ts/` projects.
- The OTel gRPC-JS instrumentation covers unary, server-streaming, client-streaming, and bidirectional-streaming RPCs in this feature.
- JS bootstrap integration is intentionally out of scope for this feature.
- The `experimental/ts/grpc_hello_world/testplan/` material remains the acceptance surface; deploy exposes HTTP through the suite-owned gateway adapter rather than raw gRPC.
