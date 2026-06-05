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
  LogLevel,
  getReporter,
} from "./reporter";

// Re-export shared types for consumer convenience.
export { LogLevel } from "./reporter";

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
  if (raw === "debug") return LogLevel.DEBUG;
  return LogLevel.INFO;
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
    this.log(LogLevel.INFO, msg, attrs ?? {});
  }

  warn(msg: string, attrs?: LogAttributes): void {
    this.log(LogLevel.WARN, msg, attrs ?? {});
  }

  error(msg: string, attrs?: LogAttributes): void {
    this.log(LogLevel.ERROR, msg, attrs ?? {});
  }

  debug(msg: string, attrs?: LogAttributes): void {
    if (this.level > LogLevel.DEBUG) return;
    this.log(LogLevel.DEBUG, msg, attrs ?? {});
  }

  private log(level: LogLevel, msg: string, attrs: LogAttributes): void {
    const reporter = getReporter();
    if (reporter) {
      reporter.write(level, msg, attrs);
    } else {
      const levelName = LogLevel[level];
      console.log(JSON.stringify({ level: levelName, msg, ...attrs }));
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
