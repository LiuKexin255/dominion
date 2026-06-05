# Contract: gRPC-JS OTel Instrumentation Package

**Package**: `@dominion/common-js-grpc-otel` (`common/js/grpc/otel`)
**Import path**: `dominion/common/js/grpc/otel`

## Exports

```typescript
/**
 * Re-export of the official GrpcInstrumentation class from
 * @opentelemetry/instrumentation-grpc for direct access.
 */
export { GrpcInstrumentation } from '@opentelemetry/instrumentation-grpc';

/**
 * Creates a pre-configured GrpcInstrumentation instance with
 * sensible defaults for the dominion environment.
 *
 * @param config - Optional overrides for GrpcInstrumentationConfig
 * @returns Configured GrpcInstrumentation instance ready to register with NodeSDK
 *
 * @example
 * // In your tracing setup (must run before any @grpc/grpc-js import):
 * import { createGrpcInstrumentation } from 'dominion/common/js/grpc/otel';
 * import { NodeSDK } from '@opentelemetry/sdk-node';
 *
 * const sdk = new NodeSDK({
 *   instrumentations: [createGrpcInstrumentation()],
 * });
 * sdk.start();
 */
export function createGrpcInstrumentation(
  config?: GrpcInstrumentationConfig
): GrpcInstrumentation;
```

## Configuration Defaults

When `createGrpcInstrumentation()` is called without arguments:

| Config field               | Default  | Description                         |
|----------------------------|----------|-------------------------------------|
| `ignoreGrpcMethods`        | `[]`     | No methods are excluded from tracing |
| `metadataToSpanAttributes` | `{}`     | No metadata→attribute mapping       |

## Span Attributes

The underlying `GrpcInstrumentation` creates spans with these attributes:

| Attribute              | Value source                             |
|------------------------|------------------------------------------|
| `rpc.system`           | `"grpc"` (constant)                      |
| `rpc.service`          | Parsed from gRPC method path             |
| `rpc.method`           | Parsed from gRPC method path             |
| `rpc.grpc.status_code` | gRPC status code on completion           |
| `net.peer.name`        | Client target hostname (client spans)    |
| `net.peer.port`        | Client target port (client spans)        |

## Required RPC Coverage

The package contract covers all gRPC-JS RPC types supported by the underlying
instrumentation:

| RPC type | Required behavior |
|----------|-------------------|
| Unary | Client and server spans are emitted with propagated trace context |
| Server streaming | Client and server spans cover the completed stream and final status |
| Client streaming | Client and server spans cover the completed stream and final status |
| Bidirectional streaming | Client and server spans cover the completed stream and final status |

## Critical Usage Note

The instrumentation MUST be registered **before** `@grpc/grpc-js` is loaded. If `@grpc/grpc-js` is loaded first, the instrumentation cannot patch it and will emit a warning.

**Consumers MUST NOT** use `@opentelemetry/exporter-trace-otlp-grpc` as the trace exporter, because it loads `@grpc/grpc-js` internally, creating a circular patching conflict. Use `@opentelemetry/exporter-trace-otlp-http` or `@opentelemetry/exporter-trace-otlp-proto` instead.

## Service Acceptance Contract

The repository-level acceptance consumer is `experimental/ts/grpc_hello_world/`:

1. The service initializes OTel and registers `createGrpcInstrumentation()` before loading `@grpc/grpc-js`.
2. The service emits a structured log during the `SayHello` request path.
3. `guitar run experimental/ts/grpc_hello_world/testplan/interface_test.yaml` passes through the existing gateway-backed HTTP acceptance path.
4. Logs for `service.name = 'grpc-hello-world-ts/service'` and the generated test environment are queryable after the testplan passes.
