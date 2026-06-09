# Data Model: gRPC-JS Service Discovery

## Target

Represents a parsed dominion service target.

**Fields**:
- `app: string` — application segment from `app/service:port`; trimmed and non-empty.
- `service: string` — service segment from `app/service:port`; trimmed and non-empty; must not contain `/`.
- `port: PortSelector` — numeric or named port selector.

**Validation**:
- Accepted input forms: `app/service:port`, `dominion:///app/service:port`, and `dominion-stateful:///app/service:port` when used by the stateful resolver.
- Reject missing app, service, or port with `InvalidTargetError`.
- Reject unknown URI schemes with `InvalidTargetError`.
- Trim whitespace around segments.

## PortSelector

Discriminated union describing how endpoints are filtered.

**Variants**:
- `{ kind: "number"; port: number }` — port is in `0..65535` and matches endpoint port exactly.
- `{ kind: "name"; name: string }` — name uses DNS-label syntax and resolves through `ServiceEndpoints.ports`.

**Validation**:
- Named ports must match DNS label syntax used by the Go resolver: lowercase letter followed by lowercase letters, digits, or `-`.
- Numeric ports outside the valid range are invalid.

## DominionEnvironment

Runtime environment prefix for deploy API resource names.

**Fields**:
- `scope: string`
- `environment: string`

**Validation**:
- Derived from `DOMINION_ENVIRONMENT` in `scope.envName` format.
- Missing or malformed values throw `MissingEnvironmentError` or `InvalidEnvironmentError`.

## ResolverConfig

Plain JS configuration object used by factory functions and registration helpers.

**Fields**:
- `deployBaseUrl?: string` — defaults to `http://infra.liukexin.com`.
- `fetch?: FetchLike` — injected HTTP function for tests; defaults to global `fetch`.
- `env?: Record<string, string | undefined>` — injected environment source; defaults to `process.env`.
- `refreshIntervalMs?: number` — defaults to `30_000`.
- `scheduler?: Scheduler` — optional timer injection for tests.

**Style rule**:
- Use this object shape instead of Go-style `WithResolver`, `WithRefreshInterval`, or factory option functions.

## ServiceEndpoints

Deploy API response model after JSON decoding.

**Fields**:
- `endpoints: string[]` — ready endpoint addresses, typically `host:port`.
- `ports: Record<string, number>` — named port map.
- `isStateful: boolean` — whether the service has ordinal instances.
- `statefulInstances: StatefulInstance[]` — per-instance endpoint data.

**Validation and mapping**:
- HTTP `404` maps to `ServiceNotFoundError`.
- Non-2xx responses throw a descriptive deploy API error including status and response text when available.
- Unknown extra fields from proto-JSON are ignored.

## StatefulInstance

Single ordinal instance of a stateful service.

**Fields**:
- `index: number` — zero-based ordinal index.
- `endpoints: string[]` — ready endpoint addresses for the instance.
- `hostname?: string` — optional pod/instance hostname from deploy data.

**Validation**:
- Stateful resolution against non-stateful data throws `ServiceNotStatefulError`.
- Missing requested ordinal throws `StatefulInstanceNotFoundError`.
- Requested ordinal with no filtered ready endpoints throws `StatefulInstanceNoReadyEndpointsError`.

## ResolvedEndpointSet

Internal grpc-js publication state.

**Fields**:
- `addresses: string[]` — sorted, unique `host:port` addresses.
- `endpoints: experimental.Endpoint[]` — grpc-js endpoint objects derived from `addresses`, one endpoint per resolved backend address.
- `lastUpdatedAt: Date` — diagnostic timestamp for the last successful update.

**State transitions**:
- `unresolved` → `ready` after first successful deploy resolution.
- `ready` → `ready` with no grpc-js update when refreshed addresses are identical.
- `ready` → `ready` with error report when refresh fails; last addresses remain active.
- any state → `closed` when grpc-js destroys the resolver; timers stop and no further updates publish.

**grpc-js mapping rule**:
- Convert `"10.0.0.1:50051"` to `{ addresses: [{ host: "10.0.0.1", port: 50051 }] }`.
- Publish through `experimental.ResolverListener` asynchronously, never inline with the constructor or `updateResolution()`.
