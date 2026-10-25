/**
 * Resource caps for the saolei hook engine (specs/078-saolei-js-hooks/
 * data-model.md §2.3). The values are engineering bounds, not part of any
 * interface contract — they are adjustable without a contract change
 * (specs/078-saolei-js-hooks/research.md D4).
 *
 * The synchronous execution budget upper bound is `MAX_HOOKS ×
 * EXECUTION_TIMEOUT_MS` (specs/078-saolei-js-hooks/spec.md FR-011).
 */

/**
 * Registered-hook count cap; a `set` beyond it is refused as a plain text
 * result (specs/078-saolei-js-hooks/spec.md FR-003). Eight covers a strategy
 * mix of deterministic safe cells + position statistics + heuristic warnings
 * without inflating the per-operate broadcast count
 * (specs/078-saolei-js-hooks/research.md D4).
 */
export const MAX_HOOKS = 8;

/**
 * Hook script source length cap in characters; a longer `set` is refused as a
 * plain text result (specs/078-saolei-js-hooks/research.md D4: far above a
 * function-body strategy script's practical size, enough for hundred-line
 * analysis code).
 */
export const MAX_SCRIPT_CHARS = 16384;

/**
 * Announcement output cap in characters; a longer result is truncated with a
 * marker (specs/078-saolei-js-hooks/research.md D4: an announcement is a
 * broadcast entering every member's context, so it stays on the same order as a
 * saolei tool result).
 */
export const MAX_OUTPUT_CHARS = 4096;

/**
 * Per-hook execution budget in milliseconds; exceeding it interrupts the run
 * and is reported as a timeout announcement (specs/078-saolei-js-hooks/
 * research.md D4: ~3 orders of magnitude above a deterministic 30×30 scan, and
 * `MAX_HOOKS × EXECUTION_TIMEOUT_MS` still stays far below a desktop dispatch
 * round-trip).
 */
export const EXECUTION_TIMEOUT_MS = 500;
