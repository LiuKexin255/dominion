# Contract: Common JS Logs API

## Primary Logging Shape

Consumers log with a message and optional plain structured attributes:

```typescript
info("message", { key: "value" });
warn("message", { key: "value" });
error("message", { error: "details" });
debug("message", { key: "value" });
```

## Helpers

Any retained helpers live in `@dominion/common-js-logs` and return plain attribute objects or values compatible with plain attributes. Helpers must not require a separate event package or zero-value sentinel objects.

## Reporter Contract

Console reporter output remains structured JSON.

OpenTelemetry reporter output preserves:

- severity number
- severity text
- message as body/message text
- caller attributes as structured log attributes

## Removed Contract

`@dominion/common-js-logs-event` is removed. Consumers must not import it.
