import { describe, expect, it } from "vitest";

import { checkCompatible } from "./validate.js";
import type { GameState } from "./types.js";

function state(grid: GameState["grid"]): GameState {
  return { width: grid[0]?.length ?? 0, height: grid.length, grid };
}

describe("checkCompatible", () => {
  it("accepts identical states", () => {
    const s = state([["INITIAL", "1"]]);
    expect(checkCompatible(s, s)).toEqual({ ok: true });
  });

  it("accepts INITIAL -> FLAG and FLAG -> revealed", () => {
    const prev = state([["INITIAL", "FLAG"]]);
    const next = state([["FLAG", "3"]]);
    expect(checkCompatible(prev, next)).toEqual({ ok: true });
  });

  it("accepts FLAG -> INITIAL (unflag)", () => {
    const prev = state([["FLAG"]]);
    expect(checkCompatible(prev, state([["INITIAL"]]))).toEqual({ ok: true });
  });

  it("rejects a revealed number reverting to INITIAL", () => {
    const prev = state([["3"]]);
    const next = state([["INITIAL"]]);
    const r = checkCompatible(prev, next);
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.kind).toBe("state");
  });

  it("rejects a revealed number changing to a different number", () => {
    const prev = state([["3"]]);
    const next = state([["4"]]);
    expect(checkCompatible(prev, next).ok).toBe(false);
  });

  it("rejects a dimension change", () => {
    const prev = state([["INITIAL", "INITIAL"]]);
    const next = state([["INITIAL"]]);
    const r = checkCompatible(prev, next);
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.kind).toBe("dimension");
  });

  it("treats UNKNOWN permissively in both directions", () => {
    expect(checkCompatible(state([["3"]]), state([["UNKNOWN"]])).ok).toBe(true);
    expect(checkCompatible(state([["UNKNOWN"]]), state([["3"]])).ok).toBe(true);
  });

  it("keeps a revealed number stable (3 -> 3)", () => {
    expect(checkCompatible(state([["3"]]), state([["3"]])).ok).toBe(true);
  });

  it("allows cross-step updates (skipping intermediate clicks)", () => {
    // A whole region flipped from INITIAL to revealed numbers in one update —
    // legal because INITIAL may become any revealed state, regardless of how
    // many game steps elapsed between screenshots.
    const prev = state([["INITIAL", "INITIAL", "INITIAL"]]);
    const next = state([["1", "0", "2"]]);
    expect(checkCompatible(prev, next)).toEqual({ ok: true });
  });
});
