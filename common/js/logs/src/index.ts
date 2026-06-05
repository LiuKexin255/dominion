/**
 * @packageDocumentation
 * Barrel exports for the `@dominion/common-js-logs` package.
 *
 * Re-exports all public API surfaces and provides package-level convenience
 * functions that delegate to the {@link defaultLogger default logger}.
 *
 * @module
 */

import type { Event } from "@dominion/common-js-logs-event";
import { defaultLogger, type LogAttributes } from "./logger";

// ---------------------------------------------------------------------------
// Re-exports from the event package
// ---------------------------------------------------------------------------
export {
	Event,
	eventAny,
	eventBool,
	eventErr,
	eventInt,
	eventString,
	LogAttributeValue,
} from "@dominion/common-js-logs-event";
// ---------------------------------------------------------------------------
// Re-exports from the context module
// ---------------------------------------------------------------------------
export { currentLogger, withAttributes, withLogger } from "./context";
export type { LogAttributes } from "./logger";
// ---------------------------------------------------------------------------
// Re-exports from the logger module
// ---------------------------------------------------------------------------
export { defaultLogger, Logger, LogLevel } from "./logger";
// ---------------------------------------------------------------------------
// Re-exports from the reporter module
// ---------------------------------------------------------------------------
export {
	ConsoleReporter,
	installReporter,
	newOTelReporter,
	OTelReporter,
	Reporter,
} from "./reporter";

// ---------------------------------------------------------------------------
// Package-level convenience functions
// ---------------------------------------------------------------------------

/**
 * Log an info-level message using the {@link defaultLogger default logger}.
 *
 * @param msg - The log message
 * @param attrs - Optional structured attributes
 * @param events - Optional variadic Event fields to merge into attributes
 */
export function info(
	msg: string,
	attrs?: LogAttributes,
	...events: Event[]
): void {
	defaultLogger().info(msg, attrs, ...events);
}

/**
 * Log a warning-level message using the {@link defaultLogger default logger}.
 *
 * @param msg - The log message
 * @param attrs - Optional structured attributes
 * @param events - Optional variadic Event fields to merge into attributes
 */
export function warn(
	msg: string,
	attrs?: LogAttributes,
	...events: Event[]
): void {
	defaultLogger().warn(msg, attrs, ...events);
}

/**
 * Log an error-level message using the {@link defaultLogger default logger}.
 *
 * @param msg - The log message
 * @param attrs - Optional structured attributes
 * @param events - Optional variadic Event fields to merge into attributes
 */
export function error(
	msg: string,
	attrs?: LogAttributes,
	...events: Event[]
): void {
	defaultLogger().error(msg, attrs, ...events);
}

/**
 * Log a debug-level message using the {@link defaultLogger default logger}.
 *
 * @param msg - The log message
 * @param attrs - Optional structured attributes
 * @param events - Optional variadic Event fields to merge into attributes
 */
export function debug(
	msg: string,
	attrs?: LogAttributes,
	...events: Event[]
): void {
	defaultLogger().debug(msg, attrs, ...events);
}
