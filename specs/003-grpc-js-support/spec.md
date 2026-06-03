# Feature Specification: gRPC-JS Build Support & Example Service

**Feature Branch**: `003-grpc-js-support`

**Created**: 2026-06-03

**Status**: Draft

**Input**: User description: "为仓库增加 grpc-js 编译支持，并在 @experimental/ts/ 增加一个 grpc-js 服务作为样例（服务代码要求使用 ts 而不是 js）"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Proto-to-TypeScript gRPC Type Generation (Priority: P1)

A developer defines a service contract in a `.proto` file. The build system automatically generates TypeScript type definitions for the dynamically loaded gRPC service from that proto definition. The generated type files appear in the Bazel output tree (never committed to source control) and are consumable by downstream TypeScript targets without manual intervention. Static JavaScript or TypeScript protobuf/gRPC stub code is not required.

**Why this priority**: Code generation is the foundational capability — without it, no gRPC-JS service can be built. Everything else depends on this pipeline working correctly.

**Independent Test**: Can be fully tested by adding a `.proto` file, running `bazel build` on the proto type target, and verifying that generated TypeScript type definitions appear in the build output.

**Acceptance Scenarios**:

1. **Given** a `.proto` file defining a service with one or more RPC methods, **When** the developer builds the proto type target through Bazel, **Then** TypeScript type files for messages, service handlers, clients, and the loaded package shape are produced in the build output.
2. **Given** a proto definition that depends on Google API annotations (e.g., `google/api/annotations.proto`), **When** the developer builds the proto type target, **Then** type generation succeeds without errors and the generated types correctly reference the imported definitions.
3. **Given** a proto file with no changes since the last build, **When** the developer rebuilds, **Then** Bazel correctly caches the output and produces no new artifacts.

---

### User Story 2 - TypeScript gRPC Service Implementation (Priority: P2)

A developer creates a gRPC service implementation entirely in TypeScript under `experimental/ts/`. The service code uses generated TypeScript types for compile-time safety, dynamically loads the `.proto` definition at runtime, implements the RPC handlers, and starts a gRPC server. The code follows the same TypeScript project conventions used by the existing `experimental/ts/hello_world` example.

**Why this priority**: This proves the generated types are usable in real TypeScript code and demonstrates the end-to-end developer workflow, from proto definition to running service.

**Independent Test**: Can be fully tested by a testplan suite that deploys the TypeScript grpc-js service plus a small Go HTTP wrapper that belongs to the testplan suite that lives under the testplan suite material. The wrapper exposes an HTTP endpoint in the test environment, forwards the request to the internal grpc-js service over gRPC, and the repository-standard Go large-test client verifies the wrapper response. Unit tests MUST NOT start the service process in-process for this acceptance path.

**Acceptance Scenarios**:

1. **Given** the example service project under `experimental/ts/`, **When** the developer runs `bazel build` on the service target, **Then** the TypeScript source compiles without type errors, successfully referencing the generated gRPC types.
2. **Given** the compiled service binary, **When** the developer runs it via `bazel run`, **Then** the gRPC server starts and listens on the configured port.
3. **Given** a running example gRPC server and Go HTTP wrapper managed by the testplan, **When** the Go large-test HTTP client calls the wrapper endpoint for the proto-defined RPC method, **Then** the wrapper forwards to the grpc-js service and returns a correctly structured response within 5 seconds.

---

### User Story 3 - Repository-Wide Build Consistency (Priority: P3)

After adding gRPC-JS support, the entire repository continues to build and test successfully. Existing Go-based proto and gRPC targets are unaffected. The new TypeScript project follows all repository conventions documented in the constitution and style guides.

**Why this priority**: Regression safety is essential — the new capability must not break anything that already works, and it must conform to established project governance.

**Independent Test**: Can be fully tested by running `bazel build //...` and `bazel test //...` at the repository root and verifying all targets pass.

**Acceptance Scenarios**:

1. **Given** the completed feature, **When** the developer runs `bazel build //...` at the repository root, **Then** all targets (Go, TypeScript, and others) build successfully with zero failures.
2. **Given** the completed feature, **When** the developer runs `bazel test //...` at the repository root, **Then** all existing tests continue to pass at their prior status.
3. **Given** the new TypeScript package, **When** the developer checks the dependency declarations, **Then** all runtime dependencies use the centralized catalog protocol (`"catalog:"`) and no generated proto/gRPC files exist in source control.

---

### Edge Cases

- What happens when a proto definition references types from another proto file in a different Bazel package? The build system should resolve cross-package proto dependencies correctly.
- What happens when the proto file contains syntax errors? The build should fail with a clear error message pointing to the proto file and line number.
- What happens when the TypeScript service code uses a generated type incorrectly? The TypeScript compiler should report a type error at build time, preventing runtime failures.
- What happens when the service binary runs under a testplan environment rather than a unit-test process? The service should be launched as an external system under test and verified through the deployed HTTP wrapper that forwards to its internal gRPC surface.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The build system MUST generate TypeScript type files from `.proto` files for use with dynamically loaded gRPC services. Static JavaScript or TypeScript protobuf/gRPC stub generation is NOT required.
- **FR-002**: Generated code MUST NOT be committed to the repository; it MUST reside exclusively in the Bazel output tree.
- **FR-003**: The code generation pipeline MUST support proto files that import Google API annotations (`google/api/annotations.proto`, etc.) already used by existing proto definitions in the repository.
- **FR-004**: An example gRPC service project MUST be created under `experimental/ts/` with all service implementation code written in TypeScript (`.ts` files).
- **FR-005**: The example service MUST define a `.proto` file, generate TypeScript types from it, dynamically load the `.proto` at runtime, implement at least one unary RPC method in TypeScript, and start a gRPC server.
- **FR-006**: The example project MUST follow the same conventions as `experimental/ts/hello_world`: `ts_project` compilation, SWC transpilation, CommonJS output, ES2020 target, strict mode, `@dominion/` package namespace, and catalog-based dependency management.
- **FR-007**: All new npm dependency versions MUST be declared in the root `pnpm-workspace.yaml` catalog, referenced via the `"catalog:"` protocol in the example project's `package.json`.
- **FR-008**: The full repository MUST build (`bazel build //...`) and pass all tests (`bazel test //...`) after the changes are applied.
- **FR-009**: The TypeScript gRPC example MUST include a testplan-based acceptance test that verifies the running grpc-js service through an HTTP wrapper deployed in the same suite; the wrapper MAY be Go and MUST translate HTTP requests to the internal gRPC `SayHello` call, while the service implementation MUST remain TypeScript and unit tests MUST NOT start the service process in-process for this acceptance path.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A developer can generate TypeScript gRPC type definitions from a `.proto` file using a single `bazel build` command, with zero manual steps.
- **SC-002**: The example TypeScript gRPC service starts under the testplan and responds to at least one RPC call within 5 seconds of launch.
- **SC-003**: Zero build failures or test regressions are introduced across the entire repository.
- **SC-004**: Zero generated proto or gRPC files exist in the source tree — all generated code resides exclusively in Bazel output directories.

## Assumptions

- The example service uses a simple proto definition (e.g., a Greeter service with a unary SayHello RPC), similar in scope to the existing Go `grpc_hello_world` example.
- The example service does not require TLS, authentication, or production-grade configuration — it serves as a demonstrative template.
- Runtime npm packages needed by the example service (such as the gRPC runtime and protobuf runtime) will be added to the centralized catalog in `pnpm-workspace.yaml`.
- The code generation tooling integrates with the existing `rules_proto` infrastructure already declared in `MODULE.bazel`.
- Gazelle auto-generation for TypeScript proto targets is out of scope; BUILD files for proto compilation will be written manually following the pattern established by the Go targets.
- The example service uses dynamic `.proto` loading at runtime; generated files provide compile-time TypeScript typing only.
- The TypeScript demo acceptance path uses the repository testplan workflow rather than a unit test that starts the service process inside the test runtime; because deploy only exposes HTTP endpoints, the suite deploys a Go HTTP wrapper that calls the internal grpc-js service, and the test case uses existing Go large-test HTTP client infrastructure.
