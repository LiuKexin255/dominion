/**
 * status-signal.test.ts — Tests for the status derivation pure function.
 *
 * Covers all three derivation branches per quickstart Scenario 1
 * (specs/021-agent-session-resync/quickstart.md) and data-model §1.
 */

import { describe, expect, it } from "vitest";

import {
  deriveStatusSignal,
  STATUS_SIGNAL_STATUS_ACTIVE,
  STATUS_SIGNAL_STATUS_IDLE,
  STATUS_SIGNAL_STATUS_UNSPECIFIED,
} from "./status-signal";

describe("deriveStatusSignal", () => {
  it("returns ACTIVE when a turn is in-flight, regardless of binding", () => {
    // Bound + in-flight: a turn is actively running on a bound adapter.
    expect(deriveStatusSignal(true, true)).toBe(STATUS_SIGNAL_STATUS_ACTIVE);
    // Unbound + in-flight: cannot normally occur (a turn implies a bound
    // adapter), but the in-flight signal takes precedence regardless.
    expect(deriveStatusSignal(true, false)).toBe(STATUS_SIGNAL_STATUS_ACTIVE);
  });

  it("returns IDLE when no turn is in-flight and an adapter is bound", () => {
    expect(deriveStatusSignal(false, true)).toBe(STATUS_SIGNAL_STATUS_IDLE);
  });

  it("returns UNSPECIFIED when no turn is in-flight and no adapter is bound", () => {
    expect(deriveStatusSignal(false, false)).toBe(
      STATUS_SIGNAL_STATUS_UNSPECIFIED,
    );
  });
});
