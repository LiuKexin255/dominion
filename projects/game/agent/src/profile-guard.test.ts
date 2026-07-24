/**
 * profile-guard.test.ts — Tests for the profile-mismatch guard pure function.
 *
 * Covers all guard branches per quickstart Scenario 3
 * (specs/021-agent-session-resync/quickstart.md) and data-model §5 /
 * contract §3: reject only when bound + named + mismatched; accept when
 * unbound, unnamed, or matching.
 */

import { describe, expect, it } from "vitest";

import { shouldRejectProfile } from "./profile-guard";

describe("shouldRejectProfile", () => {
  it("rejects when bound to a named profile that differs from the turn", () => {
    expect(shouldRejectProfile("alice", true, "bob")).toBe(true);
  });

  it("accepts when bound and the turn targets the bound profile", () => {
    expect(shouldRejectProfile("alice", true, "alice")).toBe(false);
  });

  it("accepts when unbound (first turn / post-Refresh) regardless of name", () => {
    // Any profile name is accepted so the adapter can be built for it.
    expect(shouldRejectProfile(null, false, "bob")).toBe(false);
    expect(shouldRejectProfile("alice", false, "bob")).toBe(false);
  });

  it("accepts when bound but the active profile name is empty/null", () => {
    // A bound adapter always carries a non-empty activeProfileName, but the
    // guard is defensive: an empty/null name never mismatches.
    expect(shouldRejectProfile(null, true, "bob")).toBe(false);
    expect(shouldRejectProfile("", true, "bob")).toBe(false);
  });
});
