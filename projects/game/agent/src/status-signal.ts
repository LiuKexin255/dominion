/**
 * status-signal.ts — Pure derivation of the agent working-state signal.
 *
 * The status ping-pong reports a value derived from per-session state
 * (specs/021-agent-session-resync/data-model.md §1,
 * contracts/agent-desktop-channel-contract.md §1): a turn in-flight ⇒ ACTIVE;
 * else adapter bound ⇒ IDLE; else UNSPECIFIED. Extracted as a pure function so
 * the derivation rule is unit-testable in isolation and reusable by the
 * Connect handler.
 *
 * The StatusSignalStatus literals are defined locally (mirroring handler.ts)
 * rather than imported as a value from the generated game_types, which is not
 * resolvable from the test runfiles tree; all game_types references elsewhere
 * are type-only.
 */

/** Proto string-literal values for StatusSignalStatus (game.proto). */
export const STATUS_SIGNAL_STATUS_UNSPECIFIED =
  "STATUS_SIGNAL_STATUS_UNSPECIFIED";
export const STATUS_SIGNAL_STATUS_ACTIVE = "STATUS_SIGNAL_STATUS_ACTIVE";
export const STATUS_SIGNAL_STATUS_IDLE = "STATUS_SIGNAL_STATUS_IDLE";

export type StatusSignalStatus =
  | typeof STATUS_SIGNAL_STATUS_UNSPECIFIED
  | typeof STATUS_SIGNAL_STATUS_ACTIVE
  | typeof STATUS_SIGNAL_STATUS_IDLE;

/**
 * Derive the working-state signal from per-session turn/adapter state.
 *
 * - isInFlight (the shared per-session turn mutex is held) ⇒ ACTIVE
 * - else isBound (an adapter is bound) ⇒ IDLE
 * - else ⇒ UNSPECIFIED
 *
 * The "in-flight" source is the shared per-session turn mutex, not a per-stream
 * flag, so the result is correct regardless of which stream issues the probe.
 */
export function deriveStatusSignal(
  isInFlight: boolean,
  isBound: boolean,
): StatusSignalStatus {
  if (isInFlight) {
    return STATUS_SIGNAL_STATUS_ACTIVE;
  }
  if (isBound) {
    return STATUS_SIGNAL_STATUS_IDLE;
  }
  return STATUS_SIGNAL_STATUS_UNSPECIFIED;
}
