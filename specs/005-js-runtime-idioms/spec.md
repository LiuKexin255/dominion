# Feature Specification: JavaScript Runtime Packaging and Idiomatic Common APIs

**Feature Branch**: `005-js-runtime-idioms`

**Created**: 2026-06-05

**Status**: Draft

**Input**: User description: "移除 common/js/logs/event 包，将仍需要的方法迁移进 common/js/logs 包；整理 JS runtime packaging 依赖传递问题，以及 common/js 中 Go 风格 API 不符合 JS/Node 习惯的问题，制定新需求。"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Deploy TypeScript service without missing runtime modules (Priority: P1)

As a service maintainer, I need a TypeScript service that imports shared JavaScript packages to start successfully after being packaged and deployed, without manually enumerating every indirect dependency in the service definition.

**Why this priority**: The current deployed service can compile and build but fail at startup with a missing module error. Runtime packaging correctness is the most immediate user-visible failure.

**Independent Test**: Can be tested by packaging and starting the TypeScript gRPC hello world service through its deployment artifact and observing that the process starts without module resolution errors.

**Acceptance Scenarios**:

1. **Given** a service that directly depends on shared JavaScript packages, **When** its deployable artifact is produced and started, **Then** all direct and indirect runtime modules required by those shared packages are available.
2. **Given** a shared JavaScript package adds a new runtime dependency, **When** a consuming service packages with only direct service dependencies declared, **Then** the new indirect dependency is included without requiring the service to declare it manually.
3. **Given** a runtime dependency is unavailable or cannot be packaged, **When** the artifact is built or validated, **Then** the failure is detected before deployment rather than surfacing only at service startup.

---

### User Story 2 - Use common JavaScript logging in a Node-idiomatic way (Priority: P2)

As a TypeScript developer, I want the common logging package to accept plain structured attribute objects and expose familiar JavaScript-style names, so that using shared logging does not require learning Go-style event constructors or zero-value sentinels.

**Why this priority**: The shared API should be natural for JavaScript developers and should not carry unnecessary Go-specific abstractions into the JavaScript package surface.

**Independent Test**: Can be tested by writing logging calls using object attributes only and verifying console and OpenTelemetry outputs preserve message and structured fields.

**Acceptance Scenarios**:

1. **Given** a developer logs a message with structured attributes, **When** the log is emitted, **Then** the attributes remain queryable structured fields rather than being embedded in a serialized body string.
2. **Given** existing logging convenience needs such as error fields remain useful, **When** the event sub-package is removed, **Then** the needed helpers are available from the main logging package with JavaScript-style naming.
3. **Given** a developer imports the logging package as a normal Node package, **When** module resolution occurs, **Then** the package exposes a valid runtime and type entrypoint.

---

### User Story 3 - Maintain observability behavior while simplifying package layout (Priority: P3)

As an observability maintainer, I need tracing, metrics, and logs to continue working with the existing deployment environment while the JavaScript package layout and APIs are cleaned up.

**Why this priority**: API cleanup must not regress service observability or the existing gRPC instrumentation load-order guarantees.

**Independent Test**: Can be tested by initializing the TypeScript gRPC service with shared OTel packages, invoking the service, and verifying startup, request logging, and trace/log correlation behavior.

**Acceptance Scenarios**:

1. **Given** gRPC instrumentation must patch modules before gRPC loads, **When** the service starts, **Then** instrumentation is registered before the gRPC runtime is loaded.
2. **Given** the service emits structured logs through OpenTelemetry, **When** logs are queried after an accepted request, **Then** message text and structured attributes are separately available.
3. **Given** shared JavaScript packages are packaged as Node packages, **When** they are consumed from a deployed artifact, **Then** each package resolves through its package metadata without relying on repository-local TypeScript path mappings.

### Edge Cases

- A shared logging helper receives an absent error value; it should omit the error field without producing an empty-key sentinel.
- A service has multiple shared JavaScript packages that depend on the same third-party runtime package; the packaged artifact should include a resolvable runtime layout without duplicate-conflict failures.
- A package contains both runtime code and type declarations; runtime consumers must resolve JavaScript while TypeScript consumers resolve declarations.
- Existing tests or services import the removed event sub-package; migration must update those imports or fail clearly during build.
- Deployment infrastructure may be unavailable during validation; the plan must record the blocker and still validate the package artifact through a local runtime surface when possible.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Shared JavaScript runtime packaging MUST allow service definitions to declare only direct runtime dependencies while automatically including required indirect runtime dependencies.
- **FR-002**: Runtime packaging MUST make missing module dependencies observable during build, package inspection, or local artifact startup validation before deployment.
- **FR-003**: Shared JavaScript packages MUST include standard package metadata so normal Node module resolution can locate runtime entrypoints and TypeScript declarations.
- **FR-004**: The standalone logging event sub-package MUST be removed from the public package set.
- **FR-005**: Any logging helper that remains useful after removing the event sub-package MUST be exposed from the main logging package using JavaScript-style naming.
- **FR-006**: The main logging API MUST support plain structured attribute objects as the primary way to attach fields to log records.
- **FR-007**: Logging MUST NOT require callers to pass Go-style key/value event structs or zero-value sentinel objects.
- **FR-008**: OpenTelemetry log emission MUST preserve structured attributes as attributes and message text as message/body content.
- **FR-009**: The TypeScript gRPC hello world service MUST continue to initialize observability before loading the gRPC runtime.
- **FR-010**: Existing behavior for console logging, log level filtering, reporter routing, reporter uninstall behavior, tracing, metrics initialization, and shutdown MUST remain covered by automated validation unless intentionally changed in the plan.
- **FR-011**: JavaScript/TypeScript dependency versions MUST remain centralized in the repository workspace catalog unless an explicit documented exception is required.
- **FR-012**: The feature MUST provide a migration path for imports and tests that currently reference the removed event package.

### Key Entities

- **Runtime Package**: A shared JavaScript package that exposes runtime JavaScript, type declarations, package metadata, and direct runtime dependencies.
- **Runtime Dependency Closure**: The complete set of direct and indirect modules required for a packaged service to resolve all runtime imports.
- **Structured Log Record**: A log emission containing severity, message text, and queryable structured attributes.
- **Service Package Artifact**: The deployable packaged form of a TypeScript service used to start the service process.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: The TypeScript gRPC hello world service starts from its packaged artifact without any module-not-found error in a local or deployed runtime validation.
- **SC-002**: A consuming service can add a shared JavaScript package with indirect runtime dependencies while declaring only direct service dependencies.
- **SC-003**: 100% of common JavaScript packages intended for runtime consumption expose valid package metadata for runtime and type entrypoints.
- **SC-004**: 0 imports of the removed logging event package remain in source code, tests, and package manifests after migration.
- **SC-005**: Structured logs emitted through the OpenTelemetry path preserve at least message, severity, and caller-supplied attributes as separately inspectable fields.
- **SC-006**: Repository validation passes with the relevant package tests, service package validation, full build, and full test suite, or any skipped deployment-only validation records an external blocker and residual risk.

## Assumptions

- The main runtime consumer for acceptance remains the existing TypeScript gRPC hello world service.
- The feature should preserve CommonJS service startup compatibility unless the implementation plan documents and validates a broader module-system migration.
- The existing requirement to avoid OTLP gRPC exporters remains in force to protect gRPC instrumentation load ordering.
- Package layout changes are allowed for `common/js` because these packages are newly introduced and have not yet become a stable external contract.
- Deployment-cluster validation may be blocked by external infrastructure; local artifact startup validation is still required to prove module resolution.
