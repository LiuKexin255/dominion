/**
 * Board-change handler tests (specs/078-saolei-js-hooks/contracts/
 * hook-announcement.md §5 group 1; specs/078-saolei-js-hooks/spec.md
 * FR-008–FR-011/FR-013, US1, SC-003/SC-004): registration order, the
 * announcement text of every result variant, an empty result's silence, a
 * failing hook's isolation, the announce-boundary swallow and the payload
 * passthrough.
 *
 * The announcement seam is a `vi.fn()` injected through the handler's deps —
 * the DI test-double convention (style/javascript.md §Mock 约定) — and every
 * call to it is asserted, so a seam that is never exercised fails the test. The
 * registry has no collaborator and is driven through its production path.
 */

import { describe, expect, it, vi } from "vitest";

import type { GameLogEntry } from "@dominion/dsh-saolei";

import { createOnBoardChange } from "./handler.js";
import { EXECUTION_TIMEOUT_MS, MAX_OUTPUT_CHARS } from "./limits.js";
import { HookRegistry } from "./registry.js";
import type { GameState } from "./sandbox.js";

/** A 2×2 recognized board; the handler only forwards it as the entry state. */
function fixtureBoard(): GameState {
  return {
    width: 2,
    height: 2,
    grid: [
      ["0", "1"],
      ["FLAG", "INITIAL"],
    ],
    mineCounter: { decoded: true, value: 1 },
  };
}

/** A board-change entry as the game runtime reports it. */
function fixtureEntry(board: GameState = fixtureBoard()): GameLogEntry {
  return { tool: "saolei_operate", operations: [], state: board, status: "playing" };
}

/** Register a hook through the production path, failing loudly on a rejection. */
function setOk(registry: HookRegistry, name: string, source: string): void {
  const result = registry.set(name, source);
  if (!result.ok) {
    throw new Error(`unexpected set rejection: ${result.rejection}`);
  }
}

describe("saolei hook board-change handler", () => {
  it("runs the registered hooks in registration order", () => {
    const registry = new HookRegistry();
    const announce = vi.fn();
    // Built before the planner registers, like the composition: the handler
    // must read the registry per event, not at construction.
    const handler = createOnBoardChange({ registry, announce });
    setOk(registry, "alpha", 'return "first";');
    setOk(registry, "beta", 'return "second";');

    handler(fixtureEntry());

    expect(announce).toHaveBeenCalledTimes(2);
    expect(announce).toHaveBeenNthCalledWith(1, "[hook alpha] first");
    expect(announce).toHaveBeenNthCalledWith(2, "[hook beta] second");
  });

  it("renders the announcement text of each result variant", () => {
    const registry = new HookRegistry();
    const announce = vi.fn();
    const handler = createOnBoardChange({ registry, announce });
    setOk(registry, "out", 'return "safe cells: (0,0)";');
    setOk(registry, "cut", `return "a".repeat(${MAX_OUTPUT_CHARS + 1});`);
    setOk(registry, "err", 'throw new Error("boom");');
    setOk(registry, "slow", "while (true) {}");

    handler(fixtureEntry());

    expect(announce).toHaveBeenCalledTimes(4);
    expect(announce).toHaveBeenNthCalledWith(1, "[hook out] safe cells: (0,0)");
    expect(announce).toHaveBeenNthCalledWith(
      2,
      `[hook cut] ${"a".repeat(MAX_OUTPUT_CHARS)} ...[truncated]`,
    );
    expect(announce).toHaveBeenNthCalledWith(3, "[hook err] error: boom");
    expect(announce).toHaveBeenNthCalledWith(
      4,
      `[hook slow] timeout after ${EXECUTION_TIMEOUT_MS}ms`,
    );
  });

  it("announces a body with no return as not serializable", () => {
    const registry = new HookRegistry();
    const announce = vi.fn();
    const handler = createOnBoardChange({ registry, announce });
    setOk(registry, "blank", "const unused = 1;");

    handler(fixtureEntry());

    expect(announce).toHaveBeenCalledOnce();
    expect(announce).toHaveBeenCalledWith(
      "[hook blank] error: output is not serializable",
    );
  });

  it("stays silent for an empty result and still runs the following hook", () => {
    const registry = new HookRegistry();
    const announce = vi.fn();
    const handler = createOnBoardChange({ registry, announce });
    setOk(registry, "quiet", 'return "";');
    setOk(registry, "loud", 'return "second";');

    handler(fixtureEntry());

    expect(announce).toHaveBeenCalledOnce();
    expect(announce).toHaveBeenCalledWith("[hook loud] second");
  });

  it("keeps running the following hook after a runtime failure", () => {
    const registry = new HookRegistry();
    const announce = vi.fn();
    const handler = createOnBoardChange({ registry, announce });
    setOk(registry, "err", 'throw new Error("boom");');
    setOk(registry, "after", 'return "still here";');

    handler(fixtureEntry());

    expect(announce).toHaveBeenCalledTimes(2);
    expect(announce).toHaveBeenNthCalledWith(1, "[hook err] error: boom");
    expect(announce).toHaveBeenNthCalledWith(2, "[hook after] still here");
  });

  it("keeps running after a hook times out", () => {
    const registry = new HookRegistry();
    const announce = vi.fn();
    const handler = createOnBoardChange({ registry, announce });
    setOk(registry, "slow", "while (true) {}");
    setOk(registry, "after", 'return "still here";');

    handler(fixtureEntry());

    expect(announce).toHaveBeenCalledTimes(2);
    expect(announce).toHaveBeenNthCalledWith(
      1,
      `[hook slow] timeout after ${EXECUTION_TIMEOUT_MS}ms`,
    );
    expect(announce).toHaveBeenNthCalledWith(2, "[hook after] still here");
  });

  it("swallows an announce failure, reports it and aborts the event", () => {
    const registry = new HookRegistry();
    const announce = vi.fn(() => {
      throw new Error("announce down");
    });
    const warn = vi.fn();
    const handler = createOnBoardChange({ registry, announce, logger: { warn } });
    setOk(registry, "first", 'return "one";');
    setOk(registry, "second", 'return "two";');

    expect(() => handler(fixtureEntry())).not.toThrow();

    // The whole loop is one try/catch, so the throwing announcement aborts this
    // event: the second hook never announces.
    expect(announce).toHaveBeenCalledOnce();
    expect(announce).toHaveBeenCalledWith("[hook first] one");
    expect(warn).toHaveBeenCalledOnce();
    expect(warn).toHaveBeenCalledWith(expect.stringContaining("announce down"));
  });

  it("does nothing when no hook is registered", () => {
    const registry = new HookRegistry();
    const announce = vi.fn();
    const handler = createOnBoardChange({ registry, announce });

    handler(fixtureEntry());

    expect(announce).not.toHaveBeenCalled();
  });

  it("hands each script the entry's board as input", () => {
    const registry = new HookRegistry();
    const announce = vi.fn();
    const handler = createOnBoardChange({ registry, announce });
    setOk(registry, "probe", "return JSON.stringify(board.width);");

    handler(fixtureEntry({ ...fixtureBoard(), width: 7 }));

    expect(announce).toHaveBeenCalledOnce();
    expect(announce).toHaveBeenCalledWith("[hook probe] 7");
  });
});
