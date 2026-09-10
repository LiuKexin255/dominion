/**
 * GameRuntime tests: the saolei game contract on the agent-scoped runtime
 * (specs/051-agent-v2-dsh-migration/data-model.md §2.5; contract
 * saolei-plugins.md §2.2/§7.1) — three-API semantics, dual-form operate,
 * per-rule rejections, batch SKIP/STOP triage, recognition-failure
 * invalidation, signal forwarding, counter-informed win, per-game statistics,
 * and the game-history (gameLog/gameEvent) buffer — plus the agent-scoped
 * lifecycle assertions (the `saoleiGame` service is reachable only on the
 * owning agent scope and unregisters with it; the root context never sees
 * it).
 *
 * Pattern (style/javascript.md Mock convention): pure DI — a fake dispatch
 * double records the dispatched FlowParts and resolves canned
 * OperationResults, and a fake `SaoleiBoardApi` returns canned `GameState`s
 * or throws to simulate recognition failure. No module interception.
 */

import { Context } from "@deepseek-ai/cordis";
import type { Agent } from "@deepseek-ai/dsh-agent";
import { createScope } from "@deepseek-ai/dsh-scope";
import type { CellStatus, GameState, MineCounter } from "@dominion/game-saolei-board";
import type { WireFlowPart } from "@dominion/dsh-desktop-bridge";
import { afterEach, describe, expect, it, vi } from "vitest";

import { createAgentGameRuntime } from "../index.js";
import { GameRuntimeService } from "./runtime.js";
import type { OperationResult } from "@dominion/dsh-desktop-bridge";
import type { SaoleiBoardApi } from "./board.js";
import { BOARD_ORIGIN_X_PX, BOARD_ORIGIN_Y_PX, CELL_SIZE_PX } from "./geometry.js";

/** Symbol → CellStatus map, mirroring saolei-board's renderer. */
const SYMBOL_TO_STATUS: Record<string, CellStatus> = {
  "*": "INITIAL",
  "0": "0",
  "1": "1",
  "2": "2",
  "3": "3",
  "4": "4",
  "5": "5",
  "6": "6",
  "7": "7",
  "8": "8",
  F: "FLAG",
  X: "HIT_MINE",
  M: "MINE",
  "?": "UNKNOWN",
};

/**
 * Build a `GameState` from space-separated symbol rows (the symbols the text
 * board renders), so tests read as the board the model sees. `mineCounter`
 * is optional — the counter-informed win returns false when it is undefined,
 * so a win-shape board MUST pass `{ decoded: true, value: 0 }`.
 */
function board(rows: string[], mineCounter?: MineCounter): GameState {
  const height = rows.length;
  const width = rows[0]?.split(/\s+/).length ?? 0;
  const grid = rows.map((r) => r.split(/\s+/).map((s) => SYMBOL_TO_STATUS[s] ?? "UNKNOWN"));
  return { width, height, grid, mineCounter };
}

/** A decoded `000` counter — the counter half of a win. */
const COUNTER_ZERO: MineCounter = { decoded: true, value: 0 };

/** A decoded `-01` counter — over-flagged (a grid-only would-be-win shape). */
const COUNTER_NEG_ONE: MineCounter = { decoded: true, value: -1 };

/** The default recognizable screenshot a SUCCEEDED receipt carries. */
const SCREENSHOT = { data: "AAAA", widthPx: 332, heightPx: 508 };

/** A SUCCEEDED receipt; `null` opts out of the screenshot (a recognition-
 * failure path — the desktop executed but returned nothing usable). */
function succeeded(screenshot: OperationResult["screenshot"] | null = SCREENSHOT): OperationResult {
  return screenshot === null
    ? { status: "TOOL_RESULT_STATUS_SUCCEEDED", message: "ok" }
    : { status: "TOOL_RESULT_STATUS_SUCCEEDED", message: "ok", screenshot };
}

/** A FAILED receipt (desktop disconnected/aborted/timeout). */
function failed(message: string): OperationResult {
  return { status: "TOOL_RESULT_STATUS_FAILED", message };
}

/**
 * Fake dispatch double: records every dispatched FlowPart and AbortSignal,
 * resolves the canned result (mutable through `set`).
 */
function makeFakeDispatch(initial: OperationResult = succeeded()): {
  dispatch: (part: WireFlowPart, signal?: AbortSignal) => Promise<OperationResult>;
  parts: WireFlowPart[];
  signals: AbortSignal[];
  set: (result: OperationResult) => void;
} {
  const parts: WireFlowPart[] = [];
  const signals: AbortSignal[] = [];
  let canned = initial;
  const dispatch = vi.fn(
    (part: WireFlowPart, signal?: AbortSignal): Promise<OperationResult> => {
      parts.push(part);
      if (signal !== undefined) {
        signals.push(signal);
      }
      return Promise.resolve(canned);
    },
  );
  return { dispatch, parts, signals, set: (result) => (canned = result) };
}

/** Controllable fake recognition engine (v1 baseline helper). */
function makeFakeBoardApi(initial: GameState): {
  api: SaoleiBoardApi;
  setInit: (s: GameState | "throw") => void;
  setUpdate: (s: GameState | "throw") => void;
} {
  let initResult: GameState | "throw" = initial;
  let updateResult: GameState | "throw" = initial;
  const api: SaoleiBoardApi = {
    init: () => {
      if (initResult === "throw") {
        throw new Error("fake recognition failure");
      }
      return initResult;
    },
    update: () => {
      if (updateResult === "throw") {
        throw new Error("fake recognition failure");
      }
      return updateResult;
    },
  };
  return { api, setInit: (s) => (initResult = s), setUpdate: (s) => (updateResult = s) };
}

/** Pixel centre of cell (x, y) per geometry.center. */
function centerX(x: number): number {
  return BOARD_ORIGIN_X_PX + x * CELL_SIZE_PX + CELL_SIZE_PX / 2;
}
function centerY(y: number): number {
  return BOARD_ORIGIN_Y_PX + y * CELL_SIZE_PX + CELL_SIZE_PX / 2;
}

/** One fresh runtime over injected doubles. */
function makeRuntime(
  dispatch = makeFakeDispatch().dispatch,
  boardApi = makeFakeBoardApi(board(["* *", "* *"])).api,
): GameRuntimeService {
  return new GameRuntimeService(new Context(), "saoleiGame", {
    sessionName: "templates/saolei/sessions/t1",
    dispatch,
    boardApi,
  });
}

describe("GameRuntime: init", () => {
  it("dispatches F2 and returns the initial text board with the status line", async () => {
    const fake = makeFakeDispatch();
    const runtime = makeRuntime(fake.dispatch);

    const outcome = await runtime.init();

    expect(outcome).toEqual({
      isError: false,
      text: expect.stringContaining("new game started\ngame status: playing"),
    });
    expect((outcome as { text: string }).text).toContain("board size 2*2");
    expect(fake.parts).toHaveLength(1);
    expect(fake.parts[0]).toEqual({ keyboardPress: { key: "KEYBOARD_KEY_F2" } });
  });

  it("seeds the board so subsequent ops validate and dispatch against it", async () => {
    const fake = makeFakeDispatch();
    const fakeBoard = makeFakeBoardApi(board(["* * 1", "* * 2", "1 2 3"]));
    const runtime = makeRuntime(fake.dispatch, fakeBoard.api);
    await runtime.init();

    const outcome = await runtime.operate({ type: "click", x: 2, y: 2 });

    // (2,2) is a revealed number on the seeded board: a harmless no-op,
    // skipped before dispatch (v1 validateMove triage).
    expect((outcome as { text: string }).text).toContain(
      "saolei_operate → executed 0 ops, skipped 1 no-op ops",
    );
    expect(fake.parts).toHaveLength(1); // only the init F2 dispatched
  });

  it("maps a FAILED receipt to an error outcome carrying the bridge message", async () => {
    const fake = makeFakeDispatch(failed("desktop disconnected"));
    const runtime = makeRuntime(fake.dispatch);

    const outcome = await runtime.init();

    expect(outcome).toEqual({ isError: true, error: { message: "desktop disconnected" } });
  });

  it("maps a screenshot-less receipt to the recognition-failure guidance", async () => {
    const fake = makeFakeDispatch(succeeded(null));
    const fakeBoard = makeFakeBoardApi(board(["* *", "* *"]));
    const runtime = makeRuntime(fake.dispatch, fakeBoard.api);

    const outcome = await runtime.init();

    expect(outcome).toEqual({
      isError: false,
      text: "unable to recognize board\n\ncall saolei_init to start a new game.",
    });
    // The state is invalidated: cell ops reject until a re-init.
    const operate = await runtime.operate({ type: "click", x: 0, y: 0 });
    expect((operate as { text: string }).text).toBe(
      "rejected: no_active_game\n\ncall saolei_init first to start a game.",
    );
  });

  it("maps a throwing recognition pass to the same guidance (state invalidated)", async () => {
    const fake = makeFakeDispatch();
    const fakeBoard = makeFakeBoardApi(board(["* *", "* *"]));
    fakeBoard.setInit("throw");
    const runtime = makeRuntime(fake.dispatch, fakeBoard.api);

    const outcome = await runtime.init();

    expect(outcome).toEqual({
      isError: false,
      text: "unable to recognize board\n\ncall saolei_init to start a new game.",
    });
    const operate = await runtime.operate({ type: "click", x: 0, y: 0 });
    expect((operate as { text: string }).text).toContain("rejected: no_active_game");
  });
});

describe("GameRuntime: operate", () => {
  it("dispatches a legal click in client space with WINDOW_MESSAGE", async () => {
    const fake = makeFakeDispatch();
    const fakeBoard = makeFakeBoardApi(board(["* * * * * * * * *", "* * * * * * * * *", "* * * * * * * * *"]));
    const runtime = makeRuntime(fake.dispatch, fakeBoard.api);
    await runtime.init();

    const outcome = await runtime.operate({ type: "click", x: 4, y: 2 });

    expect(outcome).toEqual({
      isError: false,
      text: expect.stringContaining("saolei_operate → executed 1 ops\ngame status: playing"),
    });
    expect(fake.parts).toHaveLength(2);
    expect(fake.parts[1]).toEqual({
      mouseMoveAndClick: {
        xPx: centerX(4),
        yPx: centerY(2),
        click: "MOUSE_CLICK_ACTION_LEFT_CLICK",
        method: "MOUSE_INPUT_METHOD_WINDOW_MESSAGE",
      },
    });
    // Worked example from the coordinate contract: center(4,4) = (168, 248).
    expect(centerX(4)).toBe(168);
    expect(centerY(4)).toBe(248);
  });

  it("dispatches flag as right click and chord as the atomic left+right press", async () => {
    const fake = makeFakeDispatch();
    const fakeBoard = makeFakeBoardApi(board(["* 2 *", "* 1 *", "* * *"]));
    const runtime = makeRuntime(fake.dispatch, fakeBoard.api);
    await runtime.init();
    // Recognized board evolves: after flagging (0,0), click around (1,1).
    fakeBoard.setUpdate(board(["F 2 *", "F 1 *", "* * *"]));

    await runtime.operate({ type: "flag", x: 0, y: 0 });
    await runtime.operate({ type: "chord", x: 1, y: 1 });

    expect(fake.parts[1]?.mouseMoveAndClick?.click).toBe("MOUSE_CLICK_ACTION_RIGHT_CLICK");
    expect(fake.parts[2]?.mouseMoveAndClick?.click).toBe("MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS");
  });

  it("executes a batch in order and records ONE game-log entry with the full op list", async () => {
    const fake = makeFakeDispatch();
    const runtime = makeRuntime(fake.dispatch);
    await runtime.init();

    const outcome = await runtime.operate({
      operations: [
        { type: "click", x: 0, y: 0 },
        { type: "click", x: 1, y: 1 },
      ],
    });

    expect((outcome as { text: string }).text).toContain("saolei_operate → executed 2 ops");
    expect(fake.parts).toHaveLength(3); // init + two ops, in order
    const log = runtime.peekGameLog();
    expect(log).toHaveLength(2); // init entry + ONE operate entry
    expect(log[1]).toMatchObject({
      tool: "saolei_operate",
      operations: [
        { type: "click", x: 0, y: 0 },
        { type: "click", x: 1, y: 1 },
      ],
      status: "playing",
    });
  });

  it("accepts the single and batch forms equivalently", async () => {
    const make = () => {
      const fake = makeFakeDispatch();
      const runtime = makeRuntime(fake.dispatch);
      return { fake, runtime };
    };
    const single = make();
    const batch = make();
    await single.runtime.init();
    await batch.runtime.init();

    const a = await single.runtime.operate({ type: "click", x: 1, y: 0 });
    const b = await batch.runtime.operate({ operations: [{ type: "click", x: 1, y: 0 }] });

    expect((a as { text: string }).text).toBe((b as { text: string }).text);
    expect(single.fake.parts[1]).toEqual(batch.fake.parts[1]);
  });

  it("treats an empty operations list as a no-op result", async () => {
    const fake = makeFakeDispatch();
    const runtime = makeRuntime(fake.dispatch);
    await runtime.init();

    const outcome = await runtime.operate({ operations: [] });

    expect((outcome as { text: string }).text).toContain(
      "saolei_operate → executed 0 ops\ngame status: playing",
    );
    expect(fake.parts).toHaveLength(1); // init only — no dispatch
    // v1 semantics: an empty call returns before the sink fires — no entry.
    expect(runtime.peekGameLog()).toHaveLength(1); // init only
  });

  it("rejects cell ops with no_active_game before any init", async () => {
    const fake = makeFakeDispatch();
    const runtime = makeRuntime(fake.dispatch);

    const outcome = await runtime.operate({ type: "click", x: 0, y: 0 });

    expect((outcome as { text: string }).text).toBe(
      "rejected: no_active_game\n\ncall saolei_init first to start a game.",
    );
    expect(fake.parts).toHaveLength(0);
    expect(runtime.peekGameLog()).toHaveLength(0);
  });

  it("skips harmless no-ops and continues the batch", async () => {
    const fake = makeFakeDispatch();
    const fakeBoard = makeFakeBoardApi(board(["0 1 *", "1 2 *", "* * *"]));
    const runtime = makeRuntime(fake.dispatch, fakeBoard.api);
    await runtime.init();

    const outcome = await runtime.operate({
      operations: [
        { type: "click", x: 0, y: 0 }, // revealed number → skip
        { type: "click", x: 2, y: 2 }, // legal
      ],
    });

    expect((outcome as { text: string }).text).toContain(
      "saolei_operate → executed 1 ops, skipped 1 no-op ops",
    );
    expect(fake.parts).toHaveLength(2); // init + only the legal op
  });

  it("stops the batch at a structural rejection; earlier ops take effect", async () => {
    const fake = makeFakeDispatch();
    const runtime = makeRuntime(fake.dispatch);
    await runtime.init();

    const outcome = await runtime.operate({
      operations: [
        { type: "click", x: 0, y: 0 },
        { type: "click", x: 9, y: 9 }, // out of bounds
        { type: "click", x: 1, y: 1 },
      ],
    });

    expect((outcome as { text: string }).text).toContain(
      "saolei_operate → stopped at click(9,9) (out_of_bounds)",
    );
    // A stop body carries the status line and board, no valid-range line.
    expect((outcome as { text: string }).text).not.toContain("valid range:");
    expect(fake.parts).toHaveLength(2); // init + the first op only
  });

  it("stops the batch when an op ends the game as a loss and records the terminal event", async () => {
    const fake = makeFakeDispatch();
    const fakeBoard = makeFakeBoardApi(board(["* * *", "* * *", "* * *"], COUNTER_ZERO));
    const runtime = makeRuntime(fake.dispatch, fakeBoard.api);
    await runtime.init();
    fakeBoard.setUpdate(board(["* * *", "* X *", "* * *"], { decoded: true, value: 9 }));

    const outcome = await runtime.operate({ type: "click", x: 1, y: 1 });

    const text = (outcome as { text: string }).text;
    expect(text).toContain("saolei_operate → stopped at click(1,1) (lost)");
    expect(text).toContain("game status: lost");
    // No valid-range line on a stop body (board + status only).
    expect(text).not.toContain("valid range:");
    const event = runtime.peekGameEvent();
    expect(event).not.toBeNull();
    expect(event).toMatchObject({ status: "lost", stats: { operationCount: 1 } });
    const log = runtime.peekGameLog();
    expect(log.at(-1)).toMatchObject({ tool: "(game-end)", status: "lost" });
  });

  it("rejects cell ops after a loss with game_over and after a win with game_won", async () => {
    const fake = makeFakeDispatch();
    const fakeBoard = makeFakeBoardApi(board(["* * *", "* * *", "* * *"], COUNTER_ZERO));
    const runtime = makeRuntime(fake.dispatch, fakeBoard.api);
    await runtime.init();

    // A lost board re-seeded via a fresh init (state invalidation otherwise
    // happens only on recognition failure, so re-seed directly).
    fakeBoard.setInit(board(["* X *", "* * *", "* * *"]));
    await runtime.init();

    const afterLoss = await runtime.operate({ type: "click", x: 0, y: 0 });
    expect((afterLoss as { text: string }).text).toContain(
      "saolei_operate → stopped at click(0,0) (game_over)",
    );

    // A won board re-seeded likewise.
    fakeBoard.setInit(board(["0 F 0", "0 0 0", "0 0 0"], COUNTER_ZERO));
    await runtime.init();

    const afterWin = await runtime.operate({ type: "click", x: 0, y: 0 });
    expect((afterWin as { text: string }).text).toContain(
      "saolei_operate → stopped at click(0,0) (game_won)",
    );
  });

  it("decides won only with the counter at 000 (counter-informed win)", async () => {
    const fake = makeFakeDispatch();
    // Fully revealed/flagged grid but an over-flagged counter: playing.
    const fakeBoard = makeFakeBoardApi(board(["0 0 0", "0 0 0", "0 0 0"], COUNTER_NEG_ONE));
    const runtime = makeRuntime(fake.dispatch, fakeBoard.api);
    await runtime.init();
    fakeBoard.setUpdate(board(["0 0 F", "0 0 0", "0 0 0"], COUNTER_NEG_ONE));

    const outcome = await runtime.operate({ type: "flag", x: 2, y: 0 });

    expect((outcome as { text: string }).text).toContain("game status: playing");
    expect(runtime.peekGameEvent()).toBeNull();
  });

  it("records a win when the counter reads 000 and computes the per-game stats", async () => {
    const fake = makeFakeDispatch();
    const fakeBoard = makeFakeBoardApi(board(["* * 1", "* * 1", "1 1 1"], COUNTER_ZERO));
    const runtime = makeRuntime(fake.dispatch, fakeBoard.api);
    await runtime.init();
    await runtime.operate({ type: "click", x: 0, y: 0 });
    // The winning flag: grid fully revealed/flagged, counter 000.
    fakeBoard.setUpdate(board(["0 F 1", "0 0 1", "1 1 1"], COUNTER_ZERO));

    const outcome = await runtime.operate({ type: "flag", x: 1, y: 0 });

    const text = (outcome as { text: string }).text;
    expect(text).toContain("saolei_operate → stopped at flag(1,0) (won)");
    expect(text).toContain("game status: won");
    // init mineCounter = 0 mines ⇒ correctFlags = 0 ⇒ avgOpsPerMine "N/A".
    expect(runtime.peekGameEvent()).toMatchObject({
      status: "won",
      stats: { operationCount: 2, correctFlags: 0, avgOpsPerMine: "N/A" },
    });
  });

  it("degrades correctFlags to null when the init counter was undecodable", async () => {
    const fake = makeFakeDispatch();
    const fakeBoard = makeFakeBoardApi(board(["* * *", "* * *", "* * *"]));
    const runtime = makeRuntime(fake.dispatch, fakeBoard.api);
    await runtime.init();
    fakeBoard.setUpdate(board(["* X *", "* * *", "* * *"]));

    await runtime.operate({ type: "click", x: 1, y: 0 });

    expect(runtime.peekGameEvent()).toMatchObject({
      status: "lost",
      stats: { operationCount: 1, correctFlags: null, avgOpsPerMine: "N/A" },
    });
  });

  it("computes correctFlags from revealed end-game mines against the init counter", async () => {
    const fake = makeFakeDispatch();
    // Init counter reads 3 mines (flags=0 at start ⇒ counter = mine total).
    const fakeBoard = makeFakeBoardApi(board(["* * *", "* * *", "* * *"], { decoded: true, value: 3 }));
    const runtime = makeRuntime(fake.dispatch, fakeBoard.api);
    await runtime.init();
    // Loss reveals 1 triggered (X) + 1 end-game mine (M): correctFlags = 3-2 = 1.
    fakeBoard.setUpdate(board(["X M *", "* * *", "* * *"], { decoded: true, value: 2 }));

    await runtime.operate({ type: "click", x: 0, y: 0 });

    expect(runtime.peekGameEvent()).toMatchObject({
      status: "lost",
      stats: { operationCount: 1, correctFlags: 1, avgOpsPerMine: 1 },
    });
  });

  it("maps a mid-batch FAILED dispatch to an error outcome and keeps the state", async () => {
    const fake = makeFakeDispatch();
    const fakeBoard = makeFakeBoardApi(board(["* * *", "* * *", "* * *"]));
    const runtime = makeRuntime(fake.dispatch, fakeBoard.api);
    await runtime.init();
    fake.set(failed("operation timed out"));

    const outcome = await runtime.operate({ type: "click", x: 0, y: 0 });

    expect(outcome).toEqual({ isError: true, error: { message: "operation timed out" } });
    // The recognized board is untouched (no recognition was attempted).
    expect(runtime.peekGameEvent()).toBeNull();
  });

  it("invalidates the state on a mid-operate recognition failure", async () => {
    const fake = makeFakeDispatch();
    const fakeBoard = makeFakeBoardApi(board(["* * *", "* * *", "* * *"]));
    const runtime = makeRuntime(fake.dispatch, fakeBoard.api);
    await runtime.init();
    fakeBoard.setUpdate("throw");

    const outcome = await runtime.operate({ type: "click", x: 0, y: 0 });

    expect((outcome as { text: string }).text).toBe(
      "unable to recognize board\n\ncall saolei_init to start a new game.",
    );
    const next = await runtime.operate({ type: "click", x: 1, y: 1 });
    expect((next as { text: string }).text).toContain("rejected: no_active_game");
  });

  it("forwards the caller signal to every dispatch", async () => {
    const fake = makeFakeDispatch();
    const runtime = makeRuntime(fake.dispatch);
    const controller = new AbortController();

    await runtime.init(controller.signal);
    await runtime.operate({ type: "click", x: 0, y: 0 }, controller.signal);

    expect(fake.signals).toHaveLength(2);
    expect(fake.signals[0]).toBe(controller.signal);
    expect(fake.signals[1]).toBe(controller.signal);
  });

  it("restarts on re-init: resets tracking but keeps the latest terminal event", async () => {
    const fake = makeFakeDispatch();
    const fakeBoard = makeFakeBoardApi(board(["* * *", "* * *", "* * *"], COUNTER_ZERO));
    const runtime = makeRuntime(fake.dispatch, fakeBoard.api);
    await runtime.init();
    fakeBoard.setUpdate(board(["* X *", "* * *", "* * *"], { decoded: true, value: 9 }));
    await runtime.operate({ type: "click", x: 1, y: 0 });
    expect(runtime.peekGameEvent()).toMatchObject({ status: "lost" });

    fakeBoard.setUpdate(board(["* * *", "* * *", "* * *"], COUNTER_ZERO));
    await runtime.init(); // restart

    const log = runtime.peekGameLog();
    expect(log).toHaveLength(1); // reset to the fresh saolei_init entry
    expect(log[0]).toMatchObject({ tool: "saolei_init", status: "playing" });
    expect(runtime.peekGameEvent()).toMatchObject({ status: "lost" }); // kept
  });
});

describe("GameRuntime: remain", () => {
  it("rejects with no_active_game when no board is recognized", () => {
    const runtime = makeRuntime();

    const outcome = runtime.remain();

    expect(outcome).toEqual({
      isError: false,
      text: "rejected: no_active_game\n\ncall saolei_init first to start a game.",
    });
  });

  it("computes the remain grid (number − adjacent flags, negatives kept) without dispatching", async () => {
    const fake = makeFakeDispatch();
    const fakeBoard = makeFakeBoardApi(board(["* 2 *", "F 1 *", "* * *"], COUNTER_ZERO));
    const runtime = makeRuntime(fake.dispatch, fakeBoard.api);
    await runtime.init();

    const outcome = runtime.remain();

    const text = (outcome as { text: string }).text;
    expect(text).toContain("saolei_remain → computed\ngame status: playing");
    expect(text).toContain("board size 3*3");
    // (1,1) is `1` with one adjacent flag (0,1)=F → remain 0;
    // (1,0) is `2` with one adjacent flag → remain 1.
    expect(text).toContain("0");
    expect(fake.parts).toHaveLength(1); // init only — remain dispatches nothing
    expect(runtime.peekGameLog()).toHaveLength(1); // and logs nothing
  });

  it("is not blocked by a terminal board (pure query)", async () => {
    const fake = makeFakeDispatch();
    const fakeBoard = makeFakeBoardApi(board(["* * *", "* * *", "* * *"], COUNTER_ZERO));
    const runtime = makeRuntime(fake.dispatch, fakeBoard.api);
    await runtime.init();
    fakeBoard.setUpdate(board(["* X *", "* * *", "* * *"]));

    await runtime.operate({ type: "click", x: 1, y: 0 });
    const outcome = runtime.remain();

    expect((outcome as { text: string }).text).toContain("game status: lost");
  });
});

// ── agent-scoped lifecycle (contract saolei-plugins.md §7.1) ────────────────

/** The session resource name shared by the lifecycle fixtures. */
const LIFECYCLE_SESSION = "templates/saolei/sessions/t1";

/** Drain the microtask queue so a Service registration's availability settles. */
async function flushProvide(): Promise<void> {
  for (let i = 0; i < 10; i++) {
    await Promise.resolve();
  }
}

/**
 * Agent-scope harness: a real cordis plugin fiber stands in for the agent
 * scope that the host's materialization setup owns
 * (projects/game/agent_v2/src/session.ts calls createAgentGameRuntime inside
 * `ctx.agents.create({setup})`); the runtime registers through the SAME
 * GameRuntimeService construction the production builder performs, with fake
 * dispatch/board doubles — one recognition engine PER AGENT (keyed by session
 * id), so a crosstalk assertion can never be masked by a shared mutable
 * double.
 */
function makeScopeHarness() {
  const ctx = new Context();
  const dispatched: { sessionName: string; part: WireFlowPart }[] = [];
  const desktopBridge = {
    dispatch: vi.fn(
      async (sessionName: string, part: WireFlowPart, _signal?: AbortSignal) => {
        dispatched.push({ sessionName, part });
        return succeeded();
      },
    ),
  };
  const boards = new Map<string, ReturnType<typeof makeFakeBoardApi>>();
  /** Start one agent scope: the runtime registers through the REAL scope
   * primitive (createScope — the production agent-scope boundary) with the
   * per-agent service label isolation the agent scope performs, and
   * unregisters with the scope dispose (the Service contract under test). */
  const startAgentScope = async (sessionName: string) => {
    const fakeBoard = makeFakeBoardApi(board(["* *", "* *"]));
    boards.set(sessionName, fakeBoard);
    const scope = createScope(ctx, { sessionName });
    const agentCtx = scope.ctx.isolate("saoleiGame");
    new GameRuntimeService(agentCtx, "saoleiGame", {
      sessionName,
      dispatch: (part, signal) => desktopBridge.dispatch(sessionName, part, signal),
      boardApi: fakeBoard.api,
    });
    await flushProvide();
    return {
      scope,
      runtime: () => agentCtx.get("saoleiGame") as GameRuntimeService | undefined,
    };
  };
  return { ctx, dispatched, boards, startAgentScope };
}

describe("agent-scoped saoleiGame lifecycle", () => {
  it("registers the runtime on the agent scope, unreachable from the root context", async () => {
    const harness = makeScopeHarness();
    const { scope, runtime } = await harness.startAgentScope(LIFECYCLE_SESSION);

    // Reachable on the agent scope; invisible on the host/root context
    // (agent-scoped services never leak upward).
    expect(runtime()).toBeDefined();
    expect(harness.ctx.get("saoleiGame")).toBeUndefined();

    await scope.dispose();
  });

  it("unregisters the service when the agent scope disposes", async () => {
    const harness = makeScopeHarness();
    const { scope, runtime } = await harness.startAgentScope(LIFECYCLE_SESSION);
    expect(runtime()).toBeDefined();

    await scope.dispose();

    expect(runtime()).toBeUndefined();
  });

  it("routes runtime dispatches through the bridge under the agent's session name", async () => {
    const harness = makeScopeHarness();
    const { scope, runtime } = await harness.startAgentScope(LIFECYCLE_SESSION);
    const game = runtime();
    expect(game).toBeDefined();

    await game!.init();

    expect(harness.dispatched).toHaveLength(1);
    expect(harness.dispatched[0]).toMatchObject({
      sessionName: LIFECYCLE_SESSION,
      part: { keyboardPress: { key: "KEYBOARD_KEY_F2" } },
    });

    await scope.dispose();
  });

  it("isolates two agents: each scope resolves its own runtime and states never cross", async () => {
    const harness = makeScopeHarness();
    const sessionA = "templates/saolei/sessions/a";
    const sessionB = "templates/saolei/sessions/b";
    const agentA = await harness.startAgentScope(sessionA);
    const agentB = await harness.startAgentScope(sessionB);

    // Each scope resolves ITS OWN runtime instance, never the other's.
    const runtimeA = agentA.runtime();
    const runtimeB = agentB.runtime();
    expect(runtimeA).toBeDefined();
    expect(runtimeB).toBeDefined();
    expect(runtimeB).not.toBe(runtimeA);

    // Both games start; each runtime tracks only its own history.
    await runtimeA!.init();
    await runtimeB!.init();
    expect(runtimeA!.peekGameLog()).toHaveLength(1);

    // B loses its game: the terminal event lands on B only — A's state is
    // untouched by B's operations (data-model.md §4 不变量 6 会话隔离).
    harness.boards.get(sessionB)!.setUpdate(board(["* X", "* *"]));
    const outcome = await runtimeB!.operate({ type: "click", x: 0, y: 0 });
    expect((outcome as { text: string }).text).toContain("(lost)");
    expect(runtimeB!.peekGameEvent()).toMatchObject({ status: "lost" });
    expect(runtimeB!.peekGameLog().at(-1)).toMatchObject({ tool: "(game-end)" });
    expect(runtimeA!.peekGameEvent()).toBeNull();
    expect(runtimeA!.peekGameLog()).toHaveLength(1);

    // Dispatch routing stays per agent session.
    expect(new Set(harness.dispatched.map((d) => d.sessionName))).toEqual(
      new Set([sessionA, sessionB]),
    );

    // Disposing B does not touch A: A's runtime stays reachable and playable.
    await agentB.scope.dispose();
    expect(agentB.runtime()).toBeUndefined();
    const after = await runtimeA!.operate({ type: "click", x: 0, y: 0 });
    expect((after as { text: string }).text).toContain(
      "saolei_operate → executed 1 ops",
    );

    await agentA.scope.dispose();
  });
});

describe("createAgentGameRuntime wiring", () => {
  it("binds the runtime to the explicit game session name and registers it on the agent ctx", async () => {
    const ctx = new Context();
    const dispatched: { sessionName: string; part: WireFlowPart }[] = [];
    const desktopBridge = {
      dispatch: vi.fn(
        async (sessionName: string, part: WireFlowPart, _signal?: AbortSignal) => {
          dispatched.push({ sessionName, part });
          return succeeded();
        },
      ),
    };
    // The host's materialization setup call shape: the member's dsh session
    // id is namespaced (`{game-session}/player`), while the desktop-bridge
    // connection is keyed by the GAME session resource name the orchestrator
    // passes explicitly.
    const scope = createScope(ctx, { sessionName: LIFECYCLE_SESSION });
    const agentCtx = scope.ctx.isolate("saoleiGame");
    createAgentGameRuntime(
      { id: `${LIFECYCLE_SESSION}/player`, ctx: agentCtx } as unknown as Agent,
      desktopBridge as never,
      LIFECYCLE_SESSION,
    );
    await flushProvide();

    // Resolution reads the context PROPERTY (the proxy's fiber-walking
    // lookup) — the exact face the saolei tools resolve through.
    const runtime = agentCtx.saoleiGame;
    expect(runtime).toBeDefined();
    await runtime!.init();
    expect(dispatched[0]?.sessionName).toBe(LIFECYCLE_SESSION);
    expect(desktopBridge.dispatch).toHaveBeenCalledOnce();

    await scope.dispose();
    // Post-dispose the property walk fails loud (inject semantics) instead
    // of answering a stale service.
    expect(() => (agentCtx as { saoleiGame: unknown }).saoleiGame).toThrow(
      /without inject/,
    );
  });
});

afterEach(() => {
  vi.restoreAllMocks();
});
