/**
 * @packageDocumentation
 * Logger module provides structured logging with level-based filtering.
 *
 * The Logger class supports info, warn, error, and debug methods that accept
 * a message and optional structured attributes.
 * Records are routed through an installed Reporter when available, otherwise
 * fall back to console output.
 *
 * LOG_LEVEL environment variable: only `"debug"` (case-insensitive) enables
 * DEBUG-level output; all other values default to INFO.
 *
 * @module
 */

import {
  type LogLevel,
  getReporter,
} from "./reporter.js";

// Re-export shared types for consumer convenience. `export type` is required:
// swc transpiles files in isolation, so a value-form re-export of a type would
// survive into the ESM output and fail linking (the compiled reporter.js no
// longer has that binding).
export type { LogLevel } from "./reporter.js";

/**
 * Types accepted as log attribute values.
 */
export type LogAttributeValue =
  | string
  | number
  | boolean
  | Error
  | null
  | undefined
  | Record<string, unknown>;

/**
 * Structured log attributes: a map of string keys to log attribute values.
 */
export type LogAttributes = Record<string, LogAttributeValue>;

/**
 * Resolves the effective log level from the LOG_LEVEL environment variable.
 *
 * Only `"debug"` (case-insensitive) enables DEBUG output.
 * All other values (including unset) default to INFO.
 */
function resolveLogLevel(): LogLevel {
  const raw = (process.env.LOG_LEVEL || "").toLowerCase();
  if (raw === "debug") return "debug";
  return "info";
}

/**
 * Logger provides structured logging with level-based filtering.
 *
 * Each log method accepts a message string and optional structured attributes.
 *
 * Records are routed through an installed Reporter (via installReporter) when
 * available; otherwise they are written to console as JSON.
 */
export class Logger {
  private level: LogLevel;

  constructor(level?: LogLevel) {
    this.level = level ?? resolveLogLevel();
  }

  info(msg: string, attrs?: LogAttributes): void {
    this.log("info", msg, attrs ?? {});
  }

  warn(msg: string, attrs?: LogAttributes): void {
    this.log("warn", msg, attrs ?? {});
  }

  error(msg: string, attrs?: LogAttributes): void {
    this.log("error", msg, attrs ?? {});
  }

  debug(msg: string, attrs?: LogAttributes): void {
    if (this.level !== "debug") return;
    this.log("debug", msg, attrs ?? {});
  }

  private log(level: LogLevel, msg: string, attrs: LogAttributes): void {
    const reporter = getReporter();
    if (reporter) {
      reporter.write(level, msg, attrs);
    } else {
      console.log(JSON.stringify({ level, msg, ...attrs }));
    }
  }
}

let _defaultLogger: Logger | null = null;

/**
 * Returns the default Logger instance (lazy singleton).
 *
 * The first call creates a Logger with the log level resolved from
 * the LOG_LEVEL environment variable. Subsequent calls return the
 * same instance.
 */
export function defaultLogger(): Logger {
  if (!_defaultLogger) {
    _defaultLogger = new Logger();
  }
  return _defaultLogger;
}
