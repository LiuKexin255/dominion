/**
 * @packageDocumentation
 * Event package provides type-safe log event field constructors for structured logging.
 *
 * Each constructor creates an {@link Event} with a typed value, following the same
 * patterns as the Go implementation at `common/gopkg/logs/event/`.
 *
 * @module
 */

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
 * Event represents a structured log field with a key and value.
 *
 * A zero-value Event (key = "", value = undefined) is silently skipped
 * during emission by downstream log consumers.
 */
export interface Event {
  key: string;
  value: LogAttributeValue;
}

/**
 * Creates an Event with a string value.
 *
 * @param key - Attribute name
 * @param value - String value
 * @returns An Event with the specified key and string value
 */
export function eventString(key: string, value: string): Event {
  return { key, value };
}

/**
 * Creates an Event with a numeric value.
 *
 * @param key - Attribute name
 * @param value - Numeric value
 * @returns An Event with the specified key and number value
 */
export function eventInt(key: string, value: number): Event {
  return { key, value };
}

/**
 * Creates an Event with a boolean value.
 *
 * @param key - Attribute name
 * @param value - Boolean value
 * @returns An Event with the specified key and boolean value
 */
export function eventBool(key: string, value: boolean): Event {
  return { key, value };
}

/**
 * Creates an Event for an error value.
 *
 * If `err` is `null` or `undefined`, returns a zero-value Event
 * (`{ key: "", value: undefined }`) that is silently skipped during
 * emission. This allows callers to safely pass error results without
 * conditionals.
 *
 * @param err - Error, null, or undefined
 * @returns An Event with key "error" and the error as value, or a zero-value Event if err is null/undefined
 */
export function eventErr(err: Error | null | undefined): Event {
  if (!err) return { key: "", value: undefined };
  return { key: "error", value: err };
}

/**
 * Creates an Event with an arbitrary log attribute value.
 *
 * @param key - Attribute name
 * @param value - Any log attribute value (string, number, boolean, Error, null, or undefined)
 * @returns An Event with the specified key and value
 */
export function eventAny(key: string, value: LogAttributeValue): Event {
  return { key, value };
}
