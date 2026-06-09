# Research: gRPC-JS Service Discovery

## Decision: Treat Go code as behavioral reference only

**Rationale**: `common/gopkg/solver` already defines the dominion semantics for target parsing, deploy API resource names, endpoint filtering, stateful instance filtering, and last-known-good refresh behavior. The user explicitly requested not to over-migrate Go style into JS. The JS package should therefore preserve observable behavior while using TypeScript-native APIs: discriminated unions, plain configuration objects, Promise-returning methods, injected dependencies, and error classes.

**Alternatives considered**:
- Directly copy Go packages into class-per-struct and option-function APIs: rejected because it would expose Go idioms such as `WithX` option functions, context-first method signatures, sentinel errors, and ticker abstractions that are not idiomatic JS.
- Reimplement behavior without referencing Go tests: rejected because the Go code is the source of existing dominion resolver semantics.

## Decision: Implement a dedicated `@dominion/common-js-grpc-resolver` package

**Rationale**: Existing shared JS grpc capability lives in `common/js/grpc/otel/` with package-local `package.json`, `tsconfig.json`, `BUILD.bazel`, and `run_vitest.mjs`. A sibling package keeps resolver concerns isolated from observability and lets service code import only the resolver dependency it needs.

**Alternatives considered**:
- Add resolver exports to `common/js/grpc/otel`: rejected because service discovery is not observability.
- Put code in an experimental TS service: rejected because the feature is a reusable common package.

## Decision: Use `@grpc/grpc-js` experimental resolver registration directly

**Rationale**: Upstream grpc-js exposes custom resolver APIs from the `experimental` namespace. Resolver classes implement `experimental.Resolver` with `updateResolution()` and `destroy()`, are registered by scheme via `experimental.registerResolver(scheme, ResolverConstructor)`, and publish results through a `ResolverListener`. Built-in resolvers register `dns`, `unix`, `ipv4`, and `ipv6` schemes with this same constructor pattern. Dominion should expose a `registerDominionResolver(config?)` function that registers `dominion` and `dominion-stateful` schemes before any clients are constructed and uses grpc-js resolver listener updates rather than wrapping client constructors.

**Alternatives considered**:
- Mirror Go's `grpc.WithResolvers` and `Builder` shape: rejected because grpc-js uses global scheme registration and client channel options, not Go dial options.
- Require callers to manually resolve endpoints and pass `ipv4:///` targets: rejected because it fails the transparent grpc-js integration requirement.

## Decision: Publish grpc-js `Endpoint[]` asynchronously

**Rationale**: grpc-js resolvers publish `Endpoint[]`, where each endpoint contains one or more subchannel addresses. This differs from Go's flat `resolver.Address` list. Dominion should map each resolved `host:port` string into one grpc-js endpoint with one `{ host, port }` address. grpc-js also documents that resolver listener callbacks must not run synchronously from the constructor or `updateResolution()`, so even immediate/cached results must be delivered through the event loop.

**Alternatives considered**:
- Publish a flat address array mirroring Go: rejected because grpc-js load balancers consume `Endpoint[]`.
- Call the listener synchronously after a cache hit: rejected because it violates grpc-js resolver lifecycle expectations and risks re-entrant channel state changes.

## Decision: Use service config/load balancing instead of custom balancing logic

**Rationale**: grpc-js supports built-in `pick_first` and `round_robin` policies through service config (`grpc.service_config` channel option or resolver-provided service config). The resolver should provide all resolved backend endpoints and let grpc-js balancing policies select subchannels. This keeps the package focused on name resolution and matches grpc-js extension boundaries.

**Alternatives considered**:
- Implement request distribution in the resolver: rejected because grpc-js load balancers own picking.
- Force round-robin by default: rejected for the resolver package because policy belongs to client channel options; quickstart can show how to opt in with service config.

## Decision: Use deploy HTTP API with injected `fetch` and environment access

**Rationale**: The feature spec assumes `GET /v1/deploy/scopes/{scope}/environments/{env}/apps/{app}/services/{svc}/endpoints` returns proto-JSON compatible service endpoint data. Node services can use built-in `fetch`; tests can inject a fake fetch function and fake environment object. This avoids adding HTTP client dependencies while preserving testability.

**Alternatives considered**:
- Add Axios or another HTTP client: rejected because a new dependency is unnecessary for one GET request and would require catalog churn.
- Read `process.env` directly everywhere: rejected because FR-011 requires injection for testing.

## Decision: Represent errors with descriptive classes

**Rationale**: FR-014 explicitly requires descriptive error classes rather than Go-style error values or sentinel variables. Classes such as `InvalidTargetError`, `MissingEnvironmentError`, `ServiceNotFoundError`, `ServiceNotStatefulError`, `StatefulInstanceNotFoundError`, and `StatefulInstanceNoReadyEndpointsError` give callers `instanceof` checks and readable messages.

**Alternatives considered**:
- Export constants like `ERR_SERVICE_NOT_FOUND`: rejected as too close to Go sentinel error style.
- Return `{ value, error }` tuples: rejected because JS async APIs conventionally reject promises on failure.

## Decision: Use timer handles and closed-state guards for refresh

**Rationale**: grpc-js resolvers need `updateResolution`/`resolveNow` and `destroy`/`close` behavior. In JS, this should be a `setInterval`/`clearInterval` loop (or injectable scheduler in tests) with a closed flag and no Go-like goroutine/channel/ticker abstraction. Failed refreshes keep the last successful endpoint list and report errors to grpc-js without clearing addresses.

**Alternatives considered**:
- Port Go's ticker interface and channel select loop: rejected as Go-style over-migration.
- Refresh only on `resolveNow`: rejected because FR-006 requires periodic refresh.

## Decision: Test through library behavior and grpc-js integration seams

**Rationale**: This is a shared library. Unit/contract tests can prove parsing, HTTP requests, endpoint mapping, unchanged-state suppression, last-known-good retention, and timer cleanup using Vitest and fake dependencies. Full repository Bazel build/test remains the final verification.

**Alternatives considered**:
- Add a large-test deployment: rejected for this package because no service is deployed by the feature itself.
- Only test pure parser/filter helpers: rejected because grpc-js registration and refresh behavior are central requirements.
