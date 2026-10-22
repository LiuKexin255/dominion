/**
 * Hook registry tests (specs/078-saolei-js-hooks/contracts/
 * saolei-hooks-plugin.md §6 group 4; specs/078-saolei-js-hooks/spec.md
 * FR-003/FR-004/FR-005, US2): registration order with same-name
 * update-in-place, removal, the count and length caps, and the registration
 * validation chain.
 *
 * The registry is a plain in-process store with no collaborators, so these
 * tests drive it directly — the DI/`vi.fn()` seam convention
 * (style/javascript.md §Mock 约定) applies to tests whose subject HAS
 * collaborators.
 */

import { Script } from "node:vm";

import { describe, expect, it } from "vitest";

import { MAX_HOOKS, MAX_SCRIPT_CHARS } from "./limits.js";
import { HookRegistry } from "./registry.js";
import type { HookEntry } from "./registry.js";

/** A minimal body that compiles; returns the recognized board width. */
const SCRIPT_WIDE = "return String(board.width);";

/** A second compilable body, so an update is observable in the source. */
const SCRIPT_TAG = 'return "tag";';

/** Set through the production path, failing loudly on a rejection. */
function setOk(registry: HookRegistry, name: string, source: string): void {
  const result = registry.set(name, source);
  if (!result.ok) {
    throw new Error(`unexpected set rejection: ${result.rejection}`);
  }
}

/** Fill the registry to the count cap with compilable hooks. */
function fillToCap(registry: HookRegistry, source = SCRIPT_WIDE): void {
  for (let index = 0; index < MAX_HOOKS; index += 1) {
    setOk(registry, `hook-${index}`, source);
  }
}

describe("hook registry writes", () => {
  it("registers a hook and exposes it through list and entriesInOrder", () => {
    const registry = new HookRegistry();

    expect(registry.set("alpha", SCRIPT_WIDE)).toEqual({ ok: true });

    expect(registry.list()).toEqual([
      { name: "alpha", order: 0, length: SCRIPT_WIDE.length },
    ]);
    const entries = registry.entriesInOrder();
    expect(entries.map((entry) => entry.name)).toEqual(["alpha"]);
    expect(entries[0].order).toBe(0);
    expect(entries[0].source).toBe(SCRIPT_WIDE);
    expect(entries[0].script).toBeInstanceOf(Script);
  });

  it("keeps the registration order when a same-name set updates the script", () => {
    const registry = new HookRegistry();
    setOk(registry, "alpha", SCRIPT_WIDE);
    setOk(registry, "beta", SCRIPT_TAG);

    expect(registry.set("alpha", SCRIPT_TAG)).toEqual({ ok: true });

    const entries = registry.entriesInOrder();
    expect(entries.map((entry) => entry.name)).toEqual(["alpha", "beta"]);
    expect(entries[0].order).toBe(0);
    expect(entries[1].order).toBe(1);
    expect(entries[0].source).toBe(SCRIPT_TAG);
    expect(registry.list()[0]).toEqual({
      name: "alpha",
      order: 0,
      length: SCRIPT_TAG.length,
    });
  });

  it("removes a hook and drops it from the list", () => {
    const registry = new HookRegistry();
    setOk(registry, "alpha", SCRIPT_WIDE);
    setOk(registry, "beta", SCRIPT_TAG);

    expect(registry.remove("alpha")).toEqual({ ok: true });

    expect(registry.list().map((entry) => entry.name)).toEqual(["beta"]);
    expect(registry.entriesInOrder().map((entry) => entry.name)).toEqual(["beta"]);
  });

  it("refuses an unknown name and lists the registered names", () => {
    const registry = new HookRegistry();
    setOk(registry, "alpha", SCRIPT_WIDE);
    setOk(registry, "beta", SCRIPT_TAG);

    const result = registry.remove("gamma");

    expect(result.ok).toBe(false);
    if (result.ok) {
      throw new Error("expected the unknown name to refuse the removal");
    }
    expect(result.rejection).toContain("gamma");
    expect(result.rejection).toContain("alpha");
    expect(result.rejection).toContain("beta");
    expect(registry.list()).toHaveLength(2);
  });

  it("reports no registered names when the registry is empty", () => {
    const result = new HookRegistry().remove("alpha");

    expect(result.ok).toBe(false);
    if (result.ok) {
      throw new Error("expected the empty registry to refuse the removal");
    }
    expect(result.rejection).toContain("(none)");
  });

  it("hands entriesInOrder a fresh copy the caller cannot disturb", () => {
    const registry = new HookRegistry();
    setOk(registry, "alpha", SCRIPT_WIDE);

    const view = registry.entriesInOrder() as HookEntry[];
    view.length = 0;

    expect(registry.list()).toHaveLength(1);
  });
});

describe("hook registry caps", () => {
  it("refuses the hook beyond the count cap and names the cap", () => {
    const registry = new HookRegistry();
    fillToCap(registry);

    const result = registry.set("overflow", SCRIPT_WIDE);

    expect(result.ok).toBe(false);
    if (result.ok) {
      throw new Error("expected the count cap to refuse the registration");
    }
    expect(result.rejection).toContain(String(MAX_HOOKS));
    expect(registry.list()).toHaveLength(MAX_HOOKS);
  });

  it("updates an existing hook while the registry is at the count cap", () => {
    const registry = new HookRegistry();
    fillToCap(registry);

    expect(registry.set("hook-0", SCRIPT_TAG)).toEqual({ ok: true });

    expect(registry.list()).toHaveLength(MAX_HOOKS);
    expect(registry.list()[0]).toEqual({
      name: "hook-0",
      order: 0,
      length: SCRIPT_TAG.length,
    });
  });

  it("refuses a script beyond the length cap and names the cap", () => {
    const registry = new HookRegistry();

    const result = registry.set("alpha", "a".repeat(MAX_SCRIPT_CHARS + 1));

    expect(result.ok).toBe(false);
    if (result.ok) {
      throw new Error("expected the length cap to refuse the registration");
    }
    expect(result.rejection).toContain(String(MAX_SCRIPT_CHARS));
    expect(registry.list()).toEqual([]);
  });

  it("refuses an empty name without registering anything", () => {
    const registry = new HookRegistry();

    const result = registry.set("   ", SCRIPT_WIDE);

    expect(result.ok).toBe(false);
    if (result.ok) {
      throw new Error("expected the empty name to refuse the registration");
    }
    expect(result.rejection).toContain("name");
    expect(registry.list()).toEqual([]);
  });
});

describe("hook registry registration validation", () => {
  it("passes the sandbox rejection for a banned pattern through unchanged", () => {
    const registry = new HookRegistry();

    const result = registry.set("alpha", 'return require("fs");');

    expect(result.ok).toBe(false);
    if (result.ok) {
      throw new Error("expected the banned pattern to refuse the registration");
    }
    expect(result.rejection).toContain("require(");
    expect(registry.list()).toEqual([]);
  });

  it("passes the compile rejection for a syntax error through unchanged", () => {
    const registry = new HookRegistry();

    const result = registry.set("alpha", "const half = (\nreturn half;");

    expect(result.ok).toBe(false);
    if (result.ok) {
      throw new Error("expected the syntax error to refuse the registration");
    }
    expect(result.rejection).toContain("SyntaxError");
    expect(registry.list()).toEqual([]);
  });
});

describe("hook registry lifetime", () => {
  it("starts every instance empty — one registry per hooks unit", () => {
    const first = new HookRegistry();
    setOk(first, "alpha", SCRIPT_WIDE);

    const second = new HookRegistry();

    expect(second.list()).toEqual([]);
    expect(second.entriesInOrder()).toEqual([]);
    expect(first.list()).toHaveLength(1);
  });
});
