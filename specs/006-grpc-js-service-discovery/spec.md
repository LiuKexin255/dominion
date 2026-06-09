# Feature Specification: gRPC-JS Service Discovery

**Feature Branch**: `006-grpc-js-service-discovery`

**Created**: 2026-06-09

**Status**: Draft

**Input**: User description: "参考 common/gopkg/solver 和 common/gopkg/grpc/solver 为 grpc-js 提供服务发现能力。特别注意，不要过度迁移，grpc-js 服务发现包应遵循 ts/js 风格，不应引入 golang 风格和习惯。"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Resolve service endpoints from a dominion target string (Priority: P1)

A TypeScript service developer wants to connect to another service in the dominion platform. They provide a target string like `myapp/grpc:50051` and receive a list of ready endpoint addresses (e.g., `["10.0.0.1:50051", "10.0.0.2:50051"]`). The resolver fetches endpoints from the dominion deploy service using the current environment configuration. The developer does not need to know about Kubernetes, endpoint slices, or any infrastructure details.

**Why this priority**: Endpoint resolution is the core capability. Every other feature (gRPC integration, stateful services) depends on being able to resolve a target to addresses.

**Independent Test**: Can be fully tested by constructing a resolver, calling its resolve method with a target string, and verifying that the correct deploy service URL is called and endpoints are returned. All infrastructure interactions are mockable through dependency injection.

**Acceptance Scenarios**:

1. **Given** a target string `myapp/myservice:50051` and a configured deploy service environment, **When** the developer resolves the target, **Then** the resolver queries the deploy service for the matching endpoints and returns a list of `host:port` strings matching the numeric port.
2. **Given** a target string with a named port like `myapp/myservice:grpc`, **When** the deploy service response includes a port map `{"grpc": 50051}`, **Then** the resolver substitutes the named port with its numeric value and returns correctly filtered endpoints.
3. **Given** a target string with the `dominion:///` scheme prefix like `dominion:///myapp/myservice:50051`, **When** the developer resolves the target, **Then** the scheme is stripped and the target is parsed correctly.
4. **Given** an invalid target string (missing app, missing service, missing port, or invalid format), **When** the developer attempts to parse it, **Then** a clear descriptive error is thrown.

---

### User Story 2 - Automatic gRPC client load balancing via service discovery (Priority: P2)

A TypeScript gRPC-js client developer wants their client to automatically discover and balance across service endpoints. They register the dominion resolver with their gRPC client, and the resolver periodically refreshes endpoints in the background. When endpoints change (scale up, scale down, rolling update), the gRPC client picks up new addresses without manual intervention.

**Why this priority**: This integrates the resolver with grpc-js's built-in load balancing infrastructure, making service discovery transparent to client code. It is the primary value-add for gRPC service consumers.

**Independent Test**: Can be fully tested by registering the resolver, creating a gRPC channel with a dominion target URI, and verifying that the resolver correctly updates the channel's backend list as simulated endpoints change.

**Acceptance Scenarios**:

1. **Given** the dominion resolver is registered and a gRPC client is created with target `dominion:///myapp/myservice:50051`, **When** the resolver fetches endpoints `["10.0.0.1:50051", "10.0.0.2:50051"]`, **Then** the gRPC channel receives both addresses and can distribute requests across them.
2. **Given** a running resolver with cached endpoints, **When** the periodic refresh detects new endpoints, **Then** the resolver updates the gRPC channel state with the new address list without dropping in-flight requests.
3. **Given** a resolver that fails to fetch endpoints on refresh, **When** it has previously resolved endpoints successfully, **Then** the last known good endpoint list remains active and the error is reported.
4. **Given** a resolver that is no longer needed, **When** the developer closes it, **Then** the periodic refresh stops and all resources are released.

---

### User Story 3 - Resolve stateful service instances (Priority: P3)

A TypeScript service developer needs to connect to a specific ordinal instance of a stateful service (e.g., the 3rd replica of a statefulset). The resolver discovers all instances of the stateful service and returns the endpoints for the requested instance index.

**Why this priority**: Stateful service resolution is a more specialized use case. It builds on top of the core resolver but addresses a distinct access pattern needed by sharded or partitioned services.

**Independent Test**: Can be fully tested by providing mock stateful service endpoint data, requesting a specific instance index, and verifying the correct instance endpoints are returned.

**Acceptance Scenarios**:

1. **Given** a stateful service with 3 instances (indices 0, 1, 2), **When** the developer requests instance index 1, **Then** only the endpoints belonging to instance 1 are returned.
2. **Given** a stateful service where the requested instance index does not exist, **When** the developer requests instance index 99, **Then** a clear "instance not found" error is returned.
3. **Given** a stateful service instance that has no ready endpoints, **When** the developer requests that instance, **Then** a clear "no ready endpoints" error is returned.
4. **Given** a service that is not stateful, **When** the developer attempts a stateful resolution, **Then** a clear "service is not stateful" error is returned.

---

### Edge Cases

- What happens when the deploy service is temporarily unavailable? The resolver should retain its last known good state and report the error, not clear the endpoint list.
- What happens when the target string has extra whitespace? Whitespace around segments should be trimmed during parsing.
- What happens when multiple services match the same target selector? The resolver should surface the ambiguity clearly.
- What happens when the `DOMINION_ENVIRONMENT` environment variable is missing or malformed? The resolver should fail initialization with a clear descriptive error.
- What happens when the resolver is used outside a dominion-deployed environment (e.g., local development)? The resolver should fail with a clear error indicating missing environment configuration.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The package MUST parse dominion target strings in the format `app/service:port` or `dominion:///app/service:port`, supporting both numeric ports and named ports (DNS-label syntax).
- **FR-002**: The package MUST resolve targets to endpoint addresses by querying the dominion deploy service HTTP API, constructing the resource name from the current environment's scope, environment name, app, and service.
- **FR-003**: The package MUST read `DOMINION_ENVIRONMENT` (format `scope.envName`) from the environment to determine the deploy service resource name prefix.
- **FR-004**: The package MUST support both numeric and named port filtering: numeric ports filter endpoints by exact port match, named ports resolve through the deploy service port map.
- **FR-005**: The package MUST implement a grpc-js resolver plugin that integrates with grpc-js's `Resolver` interface, providing endpoint discovery through the standard grpc-js `resolver` registration mechanism.
- **FR-006**: The grpc-js resolver MUST periodically refresh endpoints at a configurable interval, defaulting to 30 seconds.
- **FR-007**: The grpc-js resolver MUST retain the last known good endpoint list when a refresh fails and report the error to the gRPC channel.
- **FR-008**: The grpc-js resolver MUST detect when the resolved endpoint list is unchanged and skip unnecessary state updates.
- **FR-009**: The package MUST support stateful service instance resolution, allowing callers to request endpoints for a specific ordinal instance index.
- **FR-010**: The package MUST report clear, descriptive errors for: invalid targets, missing environment configuration, service not found, service not stateful, instance not found, and instance with no ready endpoints.
- **FR-011**: The package MUST allow the deploy service base URL, HTTP client, and environment variable source to be injected for testing.
- **FR-012**: The package MUST use the `dominion` scheme for standard service resolution and `dominion-stateful` scheme for stateful service resolution.
- **FR-013**: The grpc-js resolver MUST cleanly stop its refresh loop and release resources (timers, channels) when closed.
- **FR-014**: The package API and code style MUST follow TypeScript/JavaScript idioms: async/await for async operations, plain objects for configuration, Promise-based interfaces, and descriptive error classes rather than Go-style error values or sentinel variables.
- **FR-015**: All new npm dependency versions MUST be declared in the root `pnpm-workspace.yaml` catalog, referenced via the `"catalog:"` protocol in the package's `package.json`.
- **FR-016**: The package MUST build and test successfully through Bazel as part of the full repository build.

### Key Entities

- **Target**: A parsed representation of a dominion service address, containing the app name, service name, and port selector (numeric or named).
- **Port Selector**: A discriminated choice between a numeric port number and a named port string, used to filter resolved endpoints.
- **Resolver**: A component that takes a target and returns a list of endpoint addresses. The deploy-backed resolver queries the deploy service HTTP API.
- **Service Endpoints**: The response from the deploy service containing endpoint addresses, port mappings, stateful instance data, and service metadata.
- **Stateful Instance**: A single instance of a stateful service, identified by its ordinal index, with its own set of endpoint addresses and hostname.
- **gRPC Resolver Plugin**: A grpc-js `Resolver` implementation that bridges the dominion resolver to grpc-js's load balancing infrastructure, including periodic refresh and state management.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A developer can resolve a dominion target to endpoint addresses with a single async function call, receiving a typed result without manually constructing HTTP requests or parsing responses.
- **SC-002**: A gRPC-js client configured with a dominion target URI automatically discovers and balances across backend endpoints without any service-discovery code in the client application.
- **SC-003**: The resolver correctly handles endpoint changes within one refresh interval, so a scaling event is reflected in client connections within 30 seconds by default.
- **SC-004**: 100% of resolver error paths produce descriptive, actionable error messages rather than generic failures.
- **SC-005**: The full repository continues to build and pass all tests after the package is added.

## Assumptions

- The package lives under `common/js/grpc/resolver/`, where a placeholder directory already exists.
- The deploy service HTTP API contract is stable: `GET /v1/deploy/scopes/{scope}/environments/{env}/apps/{app}/services/{svc}/endpoints` returns `ServiceEndpoints` in the same proto-JSON format used by the Go resolver.
- The package uses the existing `@grpc/grpc-js` resolver interface (`registerResolver`, `Resolver`, `ResolverResult`) for gRPC integration.
- Environment variable access follows Node.js conventions (`process.env`), with injection points for testing.
- The package does not need Kubernetes-specific logic (no in-cluster K8s client) because all resolution goes through the deploy service API.
- The package does not need to support browser environments; it targets Node.js runtime.
- CommonJS module format is used, consistent with the existing TypeScript service examples in this repository.
- The package follows the project convention of using the centralized `pnpm-workspace.yaml` catalog for dependency versions.
