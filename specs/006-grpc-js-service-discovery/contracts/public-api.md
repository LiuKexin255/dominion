# Public API Contract: `@dominion/common-js-grpc-resolver`

The package exposes a TypeScript/CommonJS library API. It does not expose an HTTP server or CLI.

## Package metadata

- Package path: `common/js/grpc/resolver/`
- Package name: `@dominion/common-js-grpc-resolver`
- Module format: CommonJS with declaration files
- Runtime dependency: `@grpc/grpc-js` via `catalog:`

## Target parsing

```typescript
export type PortSelector =
  | { kind: "number"; port: number }
  | { kind: "name"; name: string };

export interface Target {
  app: string;
  service: string;
  port: PortSelector;
}

export function parseTarget(raw: string): Target;
```

**Behavior**:
- Accepts `app/service:port` and `dominion:///app/service:port`.
- Stateful resolver accepts `dominion-stateful:///app/service:port` at the grpc-js scheme boundary.
- Throws `InvalidTargetError` for invalid format, missing segments, unsupported schemes, out-of-range numeric ports, or invalid named ports.

## Direct endpoint resolution

```typescript
export interface ResolverConfig {
  deployBaseUrl?: string;
  fetch?: FetchLike;
  env?: Record<string, string | undefined>;
  refreshIntervalMs?: number;
}

export interface EndpointResolver {
  resolve(target: string | Target): Promise<string[]>;
}

export function createResolver(config?: ResolverConfig): EndpointResolver;
```

**Behavior**:
- Reads `DOMINION_ENVIRONMENT=scope.envName` from `config.env ?? process.env`.
- Calls `GET {deployBaseUrl}/v1/deploy/scopes/{scope}/environments/{env}/apps/{app}/services/{service}/endpoints`.
- Numeric ports keep only endpoint addresses with the requested port.
- Named ports resolve through the deploy response `ports` map and rewrite each endpoint host to the resolved numeric port.
- Returns sorted, unique `host:port` strings.

## Stateful endpoint resolution

```typescript
export interface StatefulEndpointResolver {
  resolveInstance(target: string | Target, instance: number): Promise<string[]>;
}

export function createStatefulResolver(config?: ResolverConfig): StatefulEndpointResolver;
```

**Behavior**:
- Uses the same deploy API response as standard resolution.
- Requires `isStateful === true`.
- Selects the requested ordinal instance and applies the target port selector to that instance's endpoints.
- Throws descriptive error classes for non-stateful service, missing instance, or instance with no ready endpoints.

## grpc-js registration

```typescript
export function registerDominionResolver(config?: ResolverConfig): void;
```

**Behavior**:
- Registers scheme `dominion` for standard service discovery.
- Registers scheme `dominion-stateful` for stateful service discovery.
- Uses `@grpc/grpc-js` `experimental.registerResolver` with classes implementing `experimental.Resolver`.
- Registration must be safe to call multiple times in one process.
- For `dominion-stateful`, the URI must include `?instance=N`.
- The resolver maps resolved `host:port` strings to grpc-js `experimental.Endpoint[]` and publishes them asynchronously through the resolver listener.
- The resolver reports refresh errors without clearing the last known good endpoint list.
- The resolver refresh interval is `config.refreshIntervalMs ?? 30_000`.
- Closing/destroying the grpc-js resolver clears the interval.
- Callers who want round-robin balancing should provide grpc-js service config, e.g. `{ "loadBalancingConfig": [{ "round_robin": {} }], "methodConfig": [] }`, unless the implementation later chooses to provide an equivalent resolver service config.

## Error contract

All exported errors extend `Error` and include descriptive messages. Callers may use `instanceof` checks.

```typescript
export class InvalidTargetError extends Error {}
export class MissingEnvironmentError extends Error {}
export class InvalidEnvironmentError extends Error {}
export class DeployServiceError extends Error {}
export class ServiceNotFoundError extends DeployServiceError {}
export class ServiceNotStatefulError extends Error {}
export class StatefulInstanceNotFoundError extends Error {}
export class StatefulInstanceNoReadyEndpointsError extends Error {}
```

Do not expose Go-style `(value, error)` tuples, sentinel error constants, `context` parameters, or `WithX` option functions.
