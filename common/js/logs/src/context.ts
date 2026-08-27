/**
 * @packageDocumentation
 * Context helpers for scoped logger and attribute propagation using AsyncLocalStorage.
 *
 * Provides async-safe context for structured logging — attributes and logger instances
 * are automatically inherited by nested async operations within a scope.
 *
 * @module
 */

import { AsyncLocalStorage } from "node:async_hooks";

import { defaultLogger, type Logger, type LogAttributes } from "./logger.js";

interface ContextState {
  logger?: Logger;
  attributes?: LogAttributes;
}

const storage = new AsyncLocalStorage<ContextState>();

/**
 * Retrieves the Logger from the current async logging scope.
 * Falls back to {@link defaultLogger} if no logger is associated.
 *
 * @returns The scoped logger or the default logger
 */
export function currentLogger(): Logger {
  const state = storage.getStore();
  if (state?.logger) return state.logger;
  return defaultLogger();
}

/**
 * Retrieves the current merged attributes from the async logging scope.
 *
 * @returns The scoped attributes, or an empty object if no scope is active
 */
export function currentAttributes(): LogAttributes {
  const state = storage.getStore();
  return state?.attributes ?? {};
}

/**
 * Runs `fn` in an async logging scope enriched with the supplied structured
 * attributes. Inner attributes override outer attributes on key collision.
 *
 * Nested calls correctly merge attributes from all enclosing scopes.
 *
 * @param attrs - Attributes to merge into the current scope
 * @param fn - Function to run with enriched attributes
 * @returns The return value of `fn`
 */
export function withAttributes<T>(attrs: LogAttributes, fn: () => T): T {
  const parent = storage.getStore();
  const merged: LogAttributes = { ...(parent?.attributes ?? {}), ...attrs };
  return storage.run({ ...parent, attributes: merged }, fn);
}

/**
 * Runs `fn` in an async logging scope using the supplied Logger.
 * If `logger` is `null` or `undefined`, `fn` runs with the current logger
 * (falls back to parent scope logger or the default logger).
 *
 * @param logger - Logger instance to use, or null/undefined to keep current
 * @param fn - Function to run with the specified logger
 * @returns The return value of `fn`
 */
export function withLogger<T>(
  logger: Logger | null | undefined,
  fn: () => T,
): T {
  const parent = storage.getStore();
  const effectiveLogger = logger ?? parent?.logger ?? defaultLogger();
  return storage.run({ ...parent, logger: effectiveLogger }, fn);
}
