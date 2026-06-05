/**
 * @packageDocumentation
 * Logger module provides structured logging with level-based filtering.
 *
 * The Logger class supports info, warn, error, and debug methods that accept
 * a message, optional structured attributes, and variadic Event fields.
 * Records are routed through an installed Reporter when available, otherwise
 * fall back to console output.
 *
 * LOG_LEVEL environment variable: only `"debug"` (case-insensitive) enables
 * DEBUG-level output; all other values default to INFO.
 *
 * @module
 */

import { type Event } from "@dominion/common-js-logs-event";
import {
  LogLevel,
  type LogAttributes,
  getReporter,
} from "./reporter";

// Re-export shared types for consumer convenience.
export { LogLevel } from "./reporter";
export type { LogAttributes } from "./reporter";

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
 * Merges base attributes with variadic Event fields into a single
 * LogAttributes object.
 *
 * Zero-value events (key = "" and value = undefined) are silently skipped.
 */
function mergeEvents(
  attrs: LogAttributes | undefined,
  events: Event[],
): LogAttributes {
  const merged: LogAttributes = { ...attrs };
  for (const e of events) {
    if (e.key === "" && e.value === undefined) continue;
    merged[e.key] = e.value;
  }
  return merged;
}

/**
 * Logger provides structured logging with level-based filtering.
 *
 * Each log method accepts a message string, optional structured attributes,
 * and variadic Event fields. Events are merged into the attributes before
 * emission.
 *
 * Records are routed through an installed Reporter (via installReporter) when
 * available; otherwise they are written to console as JSON.
 */
export class Logger {
  private level: LogLevel;

  constructor(level?: LogLevel) {
    this.level = level ?? resolveLogLevel();
  }

  info(msg: string, attrs?: LogAttributes, ...events: Event[]): void {
    this.log(LogLevel.INFO, msg, mergeEvents(attrs, events));
  }

  warn(msg: string, attrs?: LogAttributes, ...events: Event[]): void {
    this.log(LogLevel.WARN, msg, mergeEvents(attrs, events));
  }

  error(msg: string, attrs?: LogAttributes, ...events: Event[]): void {
    this.log(LogLevel.ERROR, msg, mergeEvents(attrs, events));
  }

  debug(msg: string, attrs?: LogAttributes, ...events: Event[]): void {
    if (this.level > LogLevel.DEBUG) return;
    this.log(LogLevel.DEBUG, msg, mergeEvents(attrs, events));
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
