/**
 * @packageDocumentation
 * Barrel exports for the `@dominion/common-js-logs` package.
 *
 * Re-exports all public API surfaces and provides package-level convenience
 * functions that delegate to the {@link defaultLogger default logger}.
 *
 * @module
 */

import { defaultLogger, type LogAttributes } from "./logger.js";

// ---------------------------------------------------------------------------
// Re-exports from the context module
// ---------------------------------------------------------------------------
export { currentLogger, withAttributes, withLogger } from "./context.js";
// ---------------------------------------------------------------------------
// Re-exports from the logger module
// ---------------------------------------------------------------------------
// Type-only members are re-exported with `export type`: swc transpiles files
// in isolation, so value-form re-exports of types survive into the ESM output
// and fail module linking (the compiled target module no longer has those
// bindings).
export { defaultLogger, Logger } from "./logger.js";
export type { LogLevel, LogAttributeValue, LogAttributes } from "./logger.js";
// ---------------------------------------------------------------------------
// Re-exports from the reporter module
// ---------------------------------------------------------------------------
export {
	ConsoleReporter,
	installReporter,
	createOTelReporter,
	OTelReporter,
} from "./reporter.js";
export type { Reporter } from "./reporter.js";

// ---------------------------------------------------------------------------
// Package-level convenience functions
// ---------------------------------------------------------------------------

/**
 * Log an info-level message using the {@link defaultLogger default logger}.
 *
 * @param msg - The log message
 * @param attrs - Optional structured attributes
 */
export function info(
	msg: string,
	attrs?: LogAttributes,
): void {
	defaultLogger().info(msg, attrs);
}

/**
 * Log a warning-level message using the {@link defaultLogger default logger}.
 *
 * @param msg - The log message
 * @param attrs - Optional structured attributes
 */
export function warn(
	msg: string,
	attrs?: LogAttributes,
): void {
	defaultLogger().warn(msg, attrs);
}

/**
 * Log an error-level message using the {@link defaultLogger default logger}.
 *
 * @param msg - The log message
 * @param attrs - Optional structured attributes
 */
export function error(
	msg: string,
	attrs?: LogAttributes,
): void {
	defaultLogger().error(msg, attrs);
}

/**
 * Log a debug-level message using the {@link defaultLogger default logger}.
 *
 * @param msg - The log message
 * @param attrs - Optional structured attributes
 */
export function debug(
	msg: string,
	attrs?: LogAttributes,
): void {
	defaultLogger().debug(msg, attrs);
}
