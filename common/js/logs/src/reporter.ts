/**
 * @packageDocumentation
 * Reporter abstraction for the logs package.
 *
 * Provides the {@link Reporter} interface and two implementations:
 * - {@link ConsoleReporter}: writes JSON-formatted lines to stdout
 * - {@link OTelReporter}: bridges to @opentelemetry/api-logs LoggerProvider
 *
 * Also provides state management for installing/uninstalling reporters
 * with identity-based uninstall semantics.
 *
 * @module
 */

import {
  logs,
  type Logger as OTelLogger,
  SeverityNumber,
} from "@opentelemetry/api-logs";
import type { LogAttributeValue } from "./logger.js";

// ---------------------------------------------------------------------------
// Shared types (defined here to avoid circular imports with logger.ts)
// ---------------------------------------------------------------------------

/**
 * Log level type controlling verbosity of log output.
 */
export type LogLevel = "debug" | "info" | "warn" | "error";

/**
 * Structured log attributes: a map of string keys to log attribute values.
 */
export type LogAttributes = Record<string, LogAttributeValue>;

// ---------------------------------------------------------------------------
// Reporter interface
// ---------------------------------------------------------------------------

/**
 * Reporter receives structured log records from Logger instances.
 *
 * Implementations decide where to route records (console, OTel SDK, etc.).
 */
export interface Reporter {
  write(level: LogLevel, msg: string, attrs: LogAttributes): void;
}

/**
 * ConsoleReporter writes JSON-formatted log lines to stdout.
 *
 * Each entry includes level, message, all attributes, and an ISO timestamp.
 */
export class ConsoleReporter implements Reporter {
  write(level: LogLevel, msg: string, attrs: LogAttributes): void {
    const entry = {
      level,
      msg,
      ...attrs,
      time: new Date().toISOString(),
    };
    process.stdout.write(JSON.stringify(entry) + "\n");
  }
}

/**
 * OTelReporter bridges log records to an OpenTelemetry LoggerProvider.
 *
 * If no global LoggerProvider has been configured, emit is a no-op.
 */
export class OTelReporter implements Reporter {
  private logger: OTelLogger | null = null;
  private name: string;

  /**
   * @param name   Logger name used to obtain an OTel Logger when no logger is injected.
   * @param logger Optional injected Logger (dependency-injection seam). When omitted,
   *               the global OTel LoggerProvider is consulted via `logs.getLogger`.
   *               Tests inject a `vi.fn()`-backed logger so emit assertions do not
   *               depend on module-level `vi.mock` interception (which is bypassed by
   *               the pre-compiled `:lib` under Bazel `js_test` — see
   *               `style/javascript.md` §测试).
   */
  constructor(name: string, logger?: OTelLogger) {
    this.name = name;
    if (logger !== undefined) {
      this.logger = logger;
    } else {
      try {
        this.logger = logs.getLogger(name);
      } catch {
        this.logger = null;
      }
    }
  }

  write(level: LogLevel, msg: string, attrs: LogAttributes): void {
    if (!this.logger) return;
    const severity = severityFromLevel(level);
    const attributes = toOTelAttributes(attrs);
    this.logger.emit({
      severityNumber: severity,
      severityText: level,
      body: msg,
      attributes,
    });
  }
}

/**
 * Converts caller-supplied LogAttributes into OTel-compatible attribute values.
 *
 * Rules:
 * - Primitives (string, number, boolean) and null pass through as-is.
 * - Error instances are converted to their `.message` string representation.
 * - `undefined` values are omitted (no sentinel attribute emitted).
 * - Nested records pass through as AnyValueMap.
 *
 * @returns OTel-compatible attribute map (assignable to LogRecord.attributes)
 */
function toOTelAttributes(
  attrs: LogAttributes,
): Record<string, string | number | boolean | null | Record<string, any>> {
  const result: Record<string, any> = {};
  for (const [key, value] of Object.entries(attrs)) {
    if (value === undefined) continue;
    if (value instanceof Error) {
      result[key] = value.message;
    } else {
      result[key] = value;
    }
  }
  return result;
}

/**
 * Maps LogLevel values to OpenTelemetry SeverityNumber constants.
 */
function severityFromLevel(level: LogLevel): SeverityNumber {
  switch (level) {
    case "debug":
      return SeverityNumber.DEBUG;
    case "info":
      return SeverityNumber.INFO;
    case "warn":
      return SeverityNumber.WARN;
    case "error":
      return SeverityNumber.ERROR;
    default:
      return SeverityNumber.UNSPECIFIED;
  }
}

// ---------------------------------------------------------------------------
// Reporter state management
// ---------------------------------------------------------------------------

let _reporter: Reporter | null = null;

// Forward reference — Logger type is defined in logger.ts. Only used as a
// type annotation here, so this is a type-only import that compiles away
// and does NOT create a runtime circular dependency.
import type { Logger } from "./logger.js";

let _defaultLogger: Logger | null = null;

/**
 * Returns the currently installed Reporter, or null if none is active.
 *
 * Used by Logger to decide whether to route records through a reporter
 * or fall back to console output.
 */
export function getReporter(): Reporter | null {
  return _reporter;
}

/**
 * Installs the given Reporter as the active reporter.
 *
 * Returns an uninstall function that restores the previous reporter.
 * The uninstall function uses reference identity — it only removes
 * this specific reporter instance. If a different reporter was installed
 * later, calling uninstall is a no-op.
 *
 * @param reporter - Reporter to install (must not be null)
 * @returns Uninstall function
 * @throws {Error} if reporter is null
 */
export function installReporter(reporter: Reporter | null): () => void {
  if (reporter === null) {
    throw new Error("logs: installReporter called with null reporter");
  }
  const previous = _reporter;
  _reporter = reporter;
  return () => {
    if (_reporter === reporter) {
      _reporter = previous;
    }
  };
}

/**
 * Creates a new OTelReporter backed by the global LoggerProvider.
 *
 * @param name - Logger name used to obtain an OTel Logger
 * @returns A Reporter that emits to the OTel Logs SDK
 */
export function createOTelReporter(name: string): OTelReporter {
  return new OTelReporter(name);
}

/**
 * Sets the default Logger instance.
 *
 * Bypasses the console handler — the injected logger is used directly.
 * Primarily intended for testing.
 *
 * @param logger - Logger to use as default
 */
export function setDefault(logger: Logger): void {
  _defaultLogger = logger;
}

/**
 * Returns the current default Logger, or null if none has been set.
 * Used internally by the package-level log functions.
 */
export function getDefault(): Logger | null {
  return _defaultLogger;
}

/**
 * Force-clears the installed Reporter singleton (test-only isolation helper).
 *
 * `installReporter`'s uninstall restores the *previous* reporter, so once any
 * test leaves a reporter installed, the `installReporter(temp); temp()`
 * reset idiom propagates the leak instead of clearing it. Tests reset to a
 * known-null baseline via this helper rather than depending on prior test
 * cleanup. Not intended for production use.
 */
export function resetReporterForTesting(): void {
  _reporter = null;
}
