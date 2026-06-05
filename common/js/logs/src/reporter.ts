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
import { type LogAttributeValue } from "@dominion/common-js-logs-event";

// ---------------------------------------------------------------------------
// Shared types (defined here to avoid circular imports with logger.ts)
// ---------------------------------------------------------------------------

/**
 * Log level enum controlling verbosity of log output.
 */
export enum LogLevel {
  DEBUG = 0,
  INFO = 1,
  WARN = 2,
  ERROR = 3,
}

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
      level: LogLevel[level],
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

  constructor(name: string) {
    this.name = name;
    try {
      this.logger = logs.getLogger(name);
    } catch {
      this.logger = null;
    }
  }

  write(level: LogLevel, msg: string, attrs: LogAttributes): void {
    if (!this.logger) return;
    const severity = severityFromLevel(level);
    const body = JSON.stringify({ msg, ...attrs });
    this.logger.emit({ severityNumber: severity, body });
  }
}

/**
 * Maps LogLevel values to OpenTelemetry SeverityNumber constants.
 */
function severityFromLevel(level: LogLevel): SeverityNumber {
  switch (level) {
    case LogLevel.DEBUG:
      return SeverityNumber.DEBUG;
    case LogLevel.INFO:
      return SeverityNumber.INFO;
    case LogLevel.WARN:
      return SeverityNumber.WARN;
    case LogLevel.ERROR:
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
import type { Logger } from "./logger";

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
export function newOTelReporter(name: string): OTelReporter {
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
