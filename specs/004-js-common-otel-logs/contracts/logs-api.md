# Contract: Logging Package Public API

**Package**: `@dominion/common-js-logs` (`common/js/logs`)
**Import path**: `dominion/common/js/logs`

## Exports

### Package-Level Functions

```typescript
/**
 * Log at INFO level. Routes through active reporter if installed,
 * otherwise writes to console.
 */
export function info(msg: string, attrs?: LogAttributes, ...events: Event[]): void;

/**
 * Log at WARN level.
 */
export function warn(msg: string, attrs?: LogAttributes, ...events: Event[]): void;

/**
 * Log at ERROR level.
 */
export function error(msg: string, attrs?: LogAttributes, ...events: Event[]): void;

/**
 * Log at DEBUG level. Suppressed unless LOG_LEVEL=debug.
 */
export function debug(msg: string, attrs?: LogAttributes, ...events: Event[]): void;
```

### Logger Instance Functions

```typescript
/**
 * Returns the default Logger instance (console-backed).
 */
export function defaultLogger(): Logger;

/**
 * Retrieves the Logger from the current async logging scope. Falls back to
 * defaultLogger() if no logger is associated.
 */
export function currentLogger(): Logger;

/**
 * Runs fn in an async logging scope enriched with the supplied structured
 * attributes and optional event helpers.
 */
export function withAttributes<T>(attrs: LogAttributes, fn: () => T): T;

/**
 * Runs fn in an async logging scope using the supplied Logger.
 * If logger is null/undefined, fn runs with the current logger.
 */
export function withLogger<T>(logger: Logger | null | undefined, fn: () => T): T;
```

### Reporter Functions

```typescript
/**
 * Installs the given Reporter as the active reporter. Package-level
 * info/warn/error/debug calls route through this reporter.
 *
 * Returns an uninstall function that restores the previous behavior.
 * The uninstall function only removes this specific reporter; if a
 * different reporter was installed later, calling uninstall is a no-op.
 *
 * @throws {Error} if reporter is null
 */
export function installReporter(reporter: Reporter): () => void;

/**
 * Creates an OTel-backed Reporter using the global LoggerProvider.
 * When emit is called, the record is routed through the OTel Logs SDK.
 *
 * If no global LoggerProvider has been set, emit is a no-op.
 */
export function newOTelReporter(name: string): Reporter;

/**
 * Sets the default Logger. Bypasses console handler — the injected
 * logger is used directly. Intended for testing.
 */
export function setDefault(logger: Logger): void;
```

### Logger Interface

```typescript
export interface Logger {
  info(msg: string, attrs?: LogAttributes, ...events: Event[]): void;
  warn(msg: string, attrs?: LogAttributes, ...events: Event[]): void;
  error(msg: string, attrs?: LogAttributes, ...events: Event[]): void;
  debug(msg: string, attrs?: LogAttributes, ...events: Event[]): void;
}
```

### Reporter Interface

```typescript
export interface Reporter {
  write(level: LogLevel, msg: string, attrs: LogAttributes): void;
}
```

### Structured Attributes

```typescript
export type LogAttributeValue = string | number | boolean | Error | null | undefined;
export type LogAttributes = Record<string, LogAttributeValue>;
```

### Event Interface and Constructors

```typescript
export interface Event {
  key: string;
  value: LogAttributeValue;
}

export function eventString(key: string, value: string): Event;
export function eventInt(key: string, value: number): Event;
export function eventBool(key: string, value: boolean): Event;
export function eventErr(err: Error | null | undefined): Event;
export function eventAny(key: string, value: unknown): Event;
```

### LogLevel Enum

```typescript
export enum LogLevel {
  DEBUG = 0,
  INFO = 1,
  WARN = 2,
  ERROR = 3,
}
```

## Behavior Guarantees

1. **Zero-config**: Importing and calling `info("message")` works immediately with console output.
2. **Lazy initialization**: Default logger is created on first use.
3. **Async safety**: `installReporter` / `uninstall` preserve reporter replacement semantics across asynchronous call paths.
4. **Nil-safe**: `eventErr(null)` returns zero-value Event that is silently skipped.
5. **Ordering**: When reporter is installed, all subsequent calls route through it. When uninstalled, calls revert to console.

## Error Handling

- `installReporter(null)` throws `Error("logs: installReporter called with null reporter")`
- All other functions are infallible — they never throw.
