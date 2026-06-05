# Data Model: JavaScript Common Library

**Branch**: `004-js-common-otel-logs` | **Date**: 2026-06-04

## Package Structure

The common JS library aligns with the role of `common/gopkg/` with three packages under an idiomatic JS path:

```
common/js/
├── logs/          # Structured logging (mirrors common/gopkg/logs)
│   ├── src/
│   │   ├── index.ts      # Public API: info, warn, error, debug, installReporter, etc.
│   │   ├── logger.ts     # Logger class and default logger singleton
│   │   ├── reporter.ts   # Reporter abstraction (console + OTel bridge)
│   │   └── context.ts    # Context-based logger storage (AsyncLocalStorage)
│   └── event/
│       └── src/
│           └── index.ts  # Optional event constructors: string, int, err, etc.
├── grpc/
│   └── otel/      # gRPC-JS OTel instrumentation (mirrors common/gopkg/grpc/gateway/otel.go)
│       └── src/
│           └── index.ts  # Re-export + convenience wrapper for GrpcInstrumentation
└── otel/          # OTel provider initialization (mirrors common/gopkg/otel)
    └── src/
        └── index.ts      # Init, Tracer, Meter, isLoggerProviderSet
```

## Entities

### Event (logs/event)

Optional structured key-value helper attached to log records. The primary JS/TS logging path accepts object-style attributes; event helpers exist for callers that prefer explicit field constructors.

| Field   | Type                | Description                          |
|---------|---------------------|--------------------------------------|
| key     | `string`            | Attribute name                       |
| value   | `string \| number \| boolean \| Error \| null \| undefined` | Attribute value; null/undefined = zero-value (skip) |

**Constructors**: `string(k, v)`, `int(k, v)`, `bool(k, v)`, `err(error)`, `any(k, v)`

**Validation**:
- `err(null)` / `err(undefined)` returns zero-value Event (skipped during emission)
- Key must be non-empty for non-zero events

### Logger (logs)

The primary logging interface. Provides leveled log methods and context integration.

| Method              | Signature                                                      | Description                                      |
|---------------------|----------------------------------------------------------------|--------------------------------------------------|
| `info`              | `(msg: string, attrs?: LogAttributes, ...events: Event[]) => void`    | Log at INFO level                                |
| `warn`              | `(msg: string, attrs?: LogAttributes, ...events: Event[]) => void`    | Log at WARN level                                |
| `error`             | `(msg: string, attrs?: LogAttributes, ...events: Event[]) => void`    | Log at ERROR level                               |
| `debug`             | `(msg: string, attrs?: LogAttributes, ...events: Event[]) => void`    | Log at DEBUG level                               |

**Lifecycle**:
- Default instance is lazily created (console output, INFO level by default)
- When `installReporter()` is called, package-level functions route through the reporter
- When uninstall function is called, routing reverts to the default logger

### Reporter (logs)

Abstraction for log output destination.

| Method   | Signature                                                               | Description                                  |
|----------|-------------------------------------------------------------------------|----------------------------------------------|
| `write`  | `(level: LogLevel, msg: string, attrs: LogAttributes) => void` | Emit a log record to the destination |

**Variants**:
- `ConsoleReporter`: Writes JSON-formatted lines to stdout (default)
- `OTelReporter`: Bridges to `@opentelemetry/api-logs` LoggerProvider

**State transitions**:
```
[no reporter] ←→ [console reporter] ←→ [OTel reporter installed via installReporter()]
                       ↑                          ↓
                       └──── uninstall() restores ──┘
```

### OTelReporter (logs)

Creates an OTel-backed reporter using the global LoggerProvider.

| Parameter | Type     | Description                                     |
|-----------|----------|-------------------------------------------------|
| name      | `string` | Instrumentation scope name (e.g., `"dominion/common/js/logs"`) |

**Behavior**:
- Calls `logs.getLogger(name)` from `@opentelemetry/api-logs` to obtain an OTel Logger
- Each `write()` call maps to `logger.emit()` with the appropriate `SeverityNumber`
- Trace context is propagated via the active OTel JS context rather than a Go-style context argument

### GrpcInstrumentation Wrapper (grpc/otel)

Convenience wrapper around `@opentelemetry/instrumentation-grpc`.

| Export               | Type                      | Description                                              |
|----------------------|---------------------------|----------------------------------------------------------|
| `createGrpcInstrumentation` | `(config?) => GrpcInstrumentation` | Creates a pre-configured GrpcInstrumentation instance    |
| `GrpcInstrumentation`       | Re-export                 | Direct re-export from `@opentelemetry/instrumentation-grpc` |

**Default configuration**:
- `ignoreGrpcMethods`: Empty (trace all methods)
- `metadataToSpanAttributes`: Empty (no metadata → span attribute mapping by default)

**Required RPC coverage**: unary, server-streaming, client-streaming, and bidirectional-streaming gRPC-JS calls.

### Testplan Acceptance Run

Repository-level acceptance execution that deploys the existing TypeScript gRPC service and verifies queryable logs.

| Field | Type | Description |
|-------|------|-------------|
| testplan | file path | `experimental/ts/grpc_hello_world/testplan/interface_test.yaml` |
| service | service name | `grpc-hello-world-ts/service` |
| gateway | service name | `grpc-hello-world-ts/gateway` |
| endpoint | URL | `https://apitest.liukexin.com/experimental/ts/grpc-hello-world` |
| environment | string | Generated test environment from guitar/testtool |
| log_query | filter | `service.name = 'grpc-hello-world-ts/service'` and `deployment.environment.name = environment` |

**Behavior**:
- `guitar validate` must accept the testplan YAML.
- `guitar run` must deploy, execute the HTTP acceptance case, and clean up.
- After the SayHello request, a structured log from the TypeScript service must be queryable for the generated environment.

## Relationships

```
Event ←── used by ──→ Logger ←── routes through ──→ Reporter
                        │                                ├── ConsoleReporter (default)
                        │                                └── OTelReporter (installed)
                        │
                        └── scoped by ──→ AsyncLocalStorage / active OTel context

GrpcInstrumentation ←── wraps ──→ @opentelemetry/instrumentation-grpc
                                      └── patches ──→ @grpc/grpc-js (Server + Client)

OTelReporter ←── uses ──→ @opentelemetry/api-logs (Logger.emit)
                  └── backed by ──→ LoggerProvider (from @opentelemetry/sdk-logs)

TestplanAcceptanceRun ── deploys ──→ experimental/ts/grpc_hello_world service + gateway
                      └── verifies ─→ HTTP SayHello response + queryable service log
```

## Configuration

### Environment Variables

| Variable    | Default   | Description                                                    |
|-------------|-----------|----------------------------------------------------------------|
| `LOG_LEVEL` | `"info"`  | Minimum log level. `"debug"` enables debug output; all other values default to `"info"`. |

## Validation Rules

- `installReporter(null)` MUST throw/panic (matching Go behavior)
- `installReporter()` returns an uninstall function
- Calling uninstall twice is safe (idempotent)
- Calling uninstall for a previously-replaced reporter is safe (only uninstalls if still active)
- Zero-value Events (key="" and value=undefined) are silently skipped during emission
- No JS bootstrap integration is modeled in this feature; initialization is explicit through the OTel package lifecycle API.
- The service-level acceptance run must leave queryable log evidence for `grpc-hello-world-ts/service` in the generated test environment.
