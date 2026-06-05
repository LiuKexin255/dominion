# Quickstart: JavaScript Common Library with OTel gRPC-JS Support & Structured Logging

## Goal

Use the new `common/js` packages from a TypeScript service while preserving the repository's Bazel and pnpm conventions.

## Package Layout

- `common/js/logs` — structured logging with console fallback and OTel reporter routing
- `common/js/logs/event` — optional explicit event helpers
- `common/js/grpc/otel` — gRPC-JS OpenTelemetry instrumentation wrapper
- `common/js/otel` — explicit OTel provider initialization and Promise-based shutdown

No JS bootstrap package or bootstrap adapter is part of this feature.

## Logging Example

```typescript
import { info, installReporter, newOTelReporter } from 'dominion/common/js/logs';

info('service starting', { service: 'example', port: 50051 });

const uninstall = installReporter(newOTelReporter('dominion/example'));
info('remote log enabled', { service: 'example' });
uninstall();
```

## gRPC Instrumentation Example

Instrumentation must be registered before `@grpc/grpc-js` is loaded.

```typescript
import { createGrpcInstrumentation } from 'dominion/common/js/grpc/otel';
import { init } from 'dominion/common/js/otel';

const shutdown = await init({
  instrumentations: [createGrpcInstrumentation()],
});

// Import or require @grpc/grpc-js only after instrumentation setup.
const grpc = await import('@grpc/grpc-js');

// Start service/client here.

await shutdown();
```

Do not use OTLP gRPC exporters in JS OTel initialization; use OTLP HTTP exporters to avoid loading `@grpc/grpc-js` before instrumentation registration.

## Verification Commands

```bash
bazel run @pnpm -- --dir /mnt/code/dominion install
bazel build //common/js/...
bazel test //common/js/...
guitar validate experimental/ts/grpc_hello_world/testplan/interface_test.yaml
guitar run experimental/ts/grpc_hello_world/testplan/interface_test.yaml
bazel build //...
bazel test //...
```

The gRPC OTel tests must cover unary, server-streaming, client-streaming, and bidirectional-streaming RPCs.

## Testplan Acceptance

The deployed acceptance surface is the existing TypeScript gRPC service:

- Service: `experimental/ts/grpc_hello_world/`
- Testplan: `experimental/ts/grpc_hello_world/testplan/interface_test.yaml`
- Deployed service descriptor: `experimental/ts/grpc_hello_world/service.yaml`
- Gateway adapter: `experimental/ts/grpc_hello_world/testplan/gateway/`
- Public route prefix: `https://apitest.liukexin.com/experimental/ts/grpc-hello-world`

The testplan must pass by calling `GET /experimental/ts/grpc-hello-world/say-hello?name=World` through the gateway adapter and receiving `Hello World` from the TypeScript gRPC service.

## Log Query Acceptance

After `guitar run` passes, query logs for the generated test environment:

- SigNoz `service.name`: `grpc-hello-world-ts/service`
- SigNoz `deployment.environment.name`: the generated test environment from the guitar/test output (`TESTTOOL_ENV`)
- Time range: the test run window, or the last 30 minutes immediately after the run

Expected result: at least one structured log emitted by the TypeScript service for the SayHello request is present. If the testplan passes but the log is missing, the feature is not accepted unless an external observability outage is documented with remaining risk.
