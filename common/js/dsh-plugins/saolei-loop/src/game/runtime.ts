/**
 * The per-agent game runtime: init/operate/remain with recognized text-board
 * state and strict pre-dispatch validation (specs/051-agent-v2-dsh-migration/
 * research.md D6; contracts/saolei-plugins.md §2.2; data-model.md §2.5).
 *
 * Registration form: a cordis Service class constructed with
 * `(agent.ctx, "saoleiGame", deps)` by the agent-creation setup hook (the
 * host's materialization path — projects/game/agent_v2/src/session.ts —
 * calls {@link createAgentGameRuntime} inside `ctx.agents.create({setup})`),
 * so the instance registers as the agent-scoped `saoleiGame` service and
 * cordis unregisters it automatically when the agent scope unloads — there
 * is no host-level registry and no manual cleanup path. Tests inject a
 * builder wired to fake dispatch/board doubles (style/javascript.md Mock
 * convention).
 *
 * Error-result discipline (data-model.md §2.5): a game-rule rejection is a
 * NORMAL result text (`rejected: <reason>`), while a desktop-side failure
 * (bridge dispatch FAILED: disconnected/aborted/timeout) is an ERROR outcome
 * (`isError: true`) the saolei tool surfaces as a model-visible failure. A
 * receipt that arrives without a usable screenshot (any non-FAILED status)
 * is the recognition-failure path — the state is invalidated and the
 * `unable to recognize board` guidance is returned as a normal result.
 */

import { Service } from "@deepseek-ai/cordis";
import type { Context } from "@deepseek-ai/cordis";
import type { Agent } from "@deepseek-ai/dsh-agent";
import type { GameState } from "@dominion/game-saolei-board";
import type {
  DesktopBridgeService,
  OperationResult,
  WireFlowPart,
} from "@dominion/dsh-desktop-bridge";

import {
  createDefaultBoardApi,
  gameStatus,
  validateMove,
  SKIP_REASONS,
  computeGameStats,
} from "./board.js";
import type {
  GameStats,
  MoveRejection,
  OperationType,
  SaoleiBoardApi,
} from "./board.js";

export type { GameStats, OperationType } from "./board.js";
import { WINDOW_MESSAGE, center } from "./geometry.js";
import {
  initSuccessText,
  operateResultText,
  rejectionText,
  remainText,
  unrecognizableText,
} from "./text.js";
import type { StoppedOp } from "./text.js";

/**
 * Wire value of `KeyboardKey.KEYBOARD_KEY_F2` (proto enum string,
 * projects/game/game.proto `enum KeyboardKey`) — the new-game shortcut
 * dispatched by `init`.
 */
const KEY_F2 = "KEYBOARD_KEY_F2";

/** Wire values of `MouseClickAction` (proto enum strings) dispatched per
 * operation type. Chord = one atomic simultaneous left+right press. */
const OPERATION_ACTIONS: Record<OperationType, string> = {
  click: "MOUSE_CLICK_ACTION_LEFT_CLICK",
  flag: "MOUSE_CLICK_ACTION_RIGHT_CLICK",
  chord: "MOUSE_CLICK_ACTION_LEFT_RIGHT_PRESS",
};

/** A single cell operation: top-left origin (0,0), x = column, y = row. */
export interface CellOperation {
  type: OperationType;
  x: number;
  y: number;
}

/** Dual-form `saolei_operate` input: one operation XOR a batch. */
export type OperateInput = CellOperation | { operations: CellOperation[] };

/**
 * Tool-facing outcome contract shared by the three saolei tools. `isError`
 * outcomes are surfaced as model-visible tool failures; text outcomes are
 * normal results (including rejections).
 *
 * A successful outcome whose recognized board is terminal (`gameStatus(state)
 * ∈ {won, lost}`) carries `concludesTurn: true` — the dsh turn-conclusion
 * marker the saolei tool forwards through `ToolRunContext.concludeTurn()`
 * (specs/062-team-game-end-handoff/contracts/saolei-turn-conclude.md §1). The
 * marker is pure content-driven: it reads only the recognized board and never
 * the terminal-event/orchestration state, and the failure variant cannot carry
 * it (type-level parity with dsh `ToolExecutionFailure.concludesTurn?: never`,
 * node_modules/.pnpm/@deepseek-ai+dsh-tools@0.1.1-rc.2_119bc70f73f8eddebfaa6b47561adeb3/
 * node_modules/@deepseek-ai/dsh-tools/lib/types/index.d.ts:400-409).
 */
export type ToolOutcome =
  | { isError: false; text: string; concludesTurn?: true }
  | { isError: true; error: { message: string } };

/**
 * The `concludesTurn` marker for one recognized result board: terminal
 * (won/lost) boards conclude the turn, everything else carries no key. The
 * single helper exists so the three marking sites (init success, empty-batch
 * operate, normal operate) share one predicate — hand-written per-path
 * variants would drift from the judgment matrix
 * (specs/062-team-game-end-handoff/data-model.md §1.2). Pure function of
 * `state`.
 */
function concludeMarker(state: GameState): { concludesTurn?: true } {
  const status = gameStatus(state);
  return status === "won" || status === "lost" ? { concludesTurn: true } : {};
}

/**
 * One game-log entry: one step of the current game. One `operate` call —
 * single or batch — is ONE entry carrying its full operations list; init
 * resets the log with an `saolei_init` entry; a terminal game appends a
 * `(game-end)` entry. The log is ephemeral: it covers only the current game
 * (reset by init) and is never persisted across games.
 */
export interface GameLogEntry {
  /** The step trigger: "saolei_init", "saolei_operate", or "(game-end)". */
  tool: string;
  /** The full operation list of one `operate` call (absent for init/end). */
  operations?: CellOperation[];
  /** Board state after the step. */
  state: GameState;
  /** Game status after the step (loss-first, counter-informed). */
  status: "won" | "lost" | "playing";
}

/** Terminal game record carried by `peekGameEvent` (data-model.md §2.5). */
export interface GameEventRecord {
  status: "won" | "lost";
  /** Per-game statistics at end (operationCount/operationsByType/correctFlags/avgOpsPerMine). */
  stats: GameStats;
  endedAt: number;
}

/**
 * The public runtime face: the saolei tools call these three API methods and
 * read the terminal-event view; the game state itself stays private (model
 * visibility flows only through the returned text).
 */
export interface GameRuntime {
  init(signal?: AbortSignal): Promise<ToolOutcome>;
  operate(input: OperateInput, signal?: AbortSignal): Promise<ToolOutcome>;
  remain(): ToolOutcome;
  /** Terminal-event read-only view (statistics/replay; no planner consumer
   * in this feature). */
  peekGameEvent(): GameEventRecord | null;
}

/** The agent-scoped service face (`saoleiGame` on `agent.ctx`). */
export interface SaoleiGame extends GameRuntime {}

/** Collaborators of one runtime instance (all injected — DI seam). */
export interface GameRuntimeDeps {
  /** The session resource name the runtime dispatches for (`agent.id`). */
  sessionName: string;
  /** The desktop-bridge dispatch face, pre-bound to `sessionName`. */
  dispatch: (
    part: WireFlowPart,
    signal?: AbortSignal,
  ) => Promise<OperationResult>;
  /** The recognition engine. */
  boardApi: SaoleiBoardApi;
}

/**
 * Per-op execution verdict of the batch loop (v1
 * `executeOperation` triage), kept discriminated so the loop can triage
 * without re-reading the state.
 */
type OpExecution =
  | { kind: "ok"; state: GameState; status: ReturnType<typeof gameStatus> }
  | { kind: "skip"; reason: MoveRejection }
  | { kind: "stop"; reason: MoveRejection }
  | { kind: "dispatch-failed"; message: string }
  | { kind: "unrecognizable" };

/**
 * The GameRuntime cordis Service. Constructing it with
 * `new GameRuntimeService(agent.ctx, "saoleiGame", deps)` registers the
 * instance as the agent scope's `saoleiGame` service; the registration is an
 * effect on the agent scope's backing fiber and unwinds with it.
 */
export class GameRuntimeService extends Service implements GameRuntime {
  /** Latest recognized board; null = no active game / invalidated. */
  private recognized: GameState | null = null;
  /** This game's initial board (mineCounter decodes correctFlags). */
  private initState: GameState | null = null;
  /** Successful dispatch count this game. */
  private operationCount = 0;
  /** Successful dispatch count per operation type this game (reset with the
   * game, incremented at the same point as {@link operationCount};
   * specs/065-agent-v2-team-refine/contracts/game-stats-broadcast.md §1). */
  private operationsByType: Record<OperationType, number> = {
    click: 0,
    flag: 0,
    chord: 0,
  };
  /** This game's operation sequence (reset on init). */
  private readonly gameLog: GameLogEntry[] = [];
  /** Latest terminal record (persists across a restart-init, v1 buffer
   * semantics: the buffer records the LATEST end event). */
  private gameEvent: GameEventRecord | null = null;

  constructor(
    ctx: Context,
    name: string,
    private readonly deps: GameRuntimeDeps,
  ) {
    super(ctx, name);
  }

  /** Start a new game: dispatch the F2 new-game keypress, recognize the
   * initial board, and reset the per-game tracking. Re-calling restarts. */
  async init(signal?: AbortSignal): Promise<ToolOutcome> {
    const part: WireFlowPart = { keyboardPress: { key: KEY_F2 } };
    const result = await this.deps.dispatch(part, signal);
    if (result.status === "TOOL_RESULT_STATUS_FAILED") {
      return { isError: true, error: { message: result.message } };
    }
    const state = this.recognize(result, (png) => this.deps.boardApi.init(png));
    if (!state) {
      return { isError: false, text: unrecognizableText() };
    }
    this.initState = state;
    this.operationCount = 0;
    this.operationsByType = { click: 0, flag: 0, chord: 0 };
    this.gameLog.length = 0;
    this.gameLog.push({ tool: "saolei_init", state, status: "playing" });
    return { isError: false, text: initSuccessText(state), ...concludeMarker(state) };
  }

  /**
   * Execute one or more cell operations IN ORDER and return ONE result with
   * the final text board. Harmless no-ops are skipped and the batch
   * continues; structural rejections and game end stop the batch; earlier
   * successful ops take effect. A desktop-side dispatch failure aborts the
   * batch with an error outcome.
   */
  async operate(input: OperateInput, signal?: AbortSignal): Promise<ToolOutcome> {
    const operations: CellOperation[] =
      "operations" in input ? input.operations : [input];

    // Empty operations list: no-op by definition — no board side effect.
    if (operations.length === 0) {
      if (!this.recognized) {
        return { isError: false, text: rejectionText("no_active_game", null) };
      }
      return {
        isError: false,
        text: operateResultText(0, 0, this.recognized, null, null),
        ...concludeMarker(this.recognized),
      };
    }

    let executed = 0;
    let skipped = 0;
    let stoppedOp: StoppedOp | null = null;
    let stoppedReason: MoveRejection | "won" | "lost" | null = null;
    let endedStatus: "won" | "lost" | null = null;

    for (let i = 0; i < operations.length; i += 1) {
      const op = operations[i];
      const result = await this.executeOperation(op, signal);
      if (result.kind === "ok") {
        executed += 1;
        // Game end: the ending op takes effect, then the batch stops.
        if (result.status !== "playing") {
          endedStatus = result.status;
          stoppedOp = op;
          stoppedReason = result.status;
          break;
        }
      } else if (result.kind === "skip") {
        skipped += 1;
      } else if (result.kind === "stop") {
        stoppedOp = op;
        stoppedReason = result.reason;
        break;
      } else if (result.kind === "dispatch-failed") {
        return { isError: true, error: { message: result.message } };
      } else {
        // Recognition failure: the state is invalidated — the batch cannot
        // continue meaningfully.
        return { isError: false, text: unrecognizableText() };
      }
    }

    const finalState = this.recognized;
    if (!finalState) {
      return { isError: false, text: rejectionText("no_active_game", null) };
    }

    // One game-log entry per call with the full op list (v1 sink semantics:
    // fired whenever an active game exists, however the batch went), then
    // the terminal record + `(game-end)` entry when the call ended the game.
    this.gameLog.push({
      tool: "saolei_operate",
      operations,
      state: finalState,
      status: gameStatus(finalState),
    });
    if (endedStatus !== null) {
      const stats = computeGameStats(
        this.initState,
        finalState,
        this.operationCount,
        this.operationsByType,
      );
      this.gameEvent = { status: endedStatus, stats, endedAt: Date.now() };
      this.gameLog.push({ tool: "(game-end)", state: finalState, status: endedStatus });
    }

    return {
      isError: false,
      text: operateResultText(executed, skipped, finalState, stoppedOp, stoppedReason),
      ...concludeMarker(finalState),
    };
  }

  /** Read-only remain-grid query: dispatches nothing, mutates nothing. */
  remain(): ToolOutcome {
    if (!this.recognized) {
      return { isError: false, text: rejectionText("no_active_game", null) };
    }
    return { isError: false, text: remainText(this.recognized) };
  }

  peekGameEvent(): GameEventRecord | null {
    return this.gameEvent;
  }

  /**
   * Read-only view of this game's operation sequence (FR-011 history). Not
   * part of the `GameRuntime`/`SaoleiGame` contract face — this feature has
   * no in-composition consumer (no planner); tests and future consumers use
   * the concrete class.
   */
  peekGameLog(): readonly GameLogEntry[] {
    return this.gameLog;
  }

  /** Validate + dispatch + recognize ONE cell operation. */
  private async executeOperation(
    op: CellOperation,
    signal?: AbortSignal,
  ): Promise<OpExecution> {
    // No active game (pre-init, or invalidated by a recognition failure).
    if (!this.recognized) {
      return { kind: "stop", reason: "no_active_game" };
    }
    const verdict = validateMove(this.recognized, op.type, op.x, op.y);
    if (!verdict.ok) {
      if (SKIP_REASONS.has(verdict.reason)) {
        return { kind: "skip", reason: verdict.reason };
      }
      return { kind: "stop", reason: verdict.reason };
    }

    const { xPx, yPx } = center(op.x, op.y);
    const part: WireFlowPart = {
      mouseMoveAndClick: {
        xPx,
        yPx,
        click: OPERATION_ACTIONS[op.type],
        method: WINDOW_MESSAGE,
      },
    };
    const result = await this.deps.dispatch(part, signal);
    if (result.status === "TOOL_RESULT_STATUS_FAILED") {
      return { kind: "dispatch-failed", message: result.message };
    }
    const state = this.recognize(result, (png) => this.deps.boardApi.update(png));
    if (!state) {
      return { kind: "unrecognizable" };
    }
    // Only successful dispatches count as operations (the per-type counter
    // moves at the same point, keeping the parts summing to the total).
    this.operationCount += 1;
    this.operationsByType[op.type] += 1;
    return { kind: "ok", state, status: gameStatus(state) };
  }

  /**
   * Recognize a freshly-dispatched screenshot and update the state. Any
   * recognition failure (recognizer throws, or no screenshot attached)
   * invalidates the state (null).
   */
  private recognize(
    result: OperationResult,
    recognizer: (png: Buffer) => GameState,
  ): GameState | null {
    const data = result.screenshot?.data;
    if (!data) {
      this.recognized = null;
      return null;
    }
    try {
      this.recognized = recognizer(Buffer.from(data, "base64"));
      return this.recognized;
    } catch {
      this.recognized = null;
      return null;
    }
  }
}

/**
 * Production builder: the materialization setup's registration call. Wires
 * the runtime to the game session's desktop-bridge connection and the real
 * recognition engine. `sessionName` is the GAME session resource name
 * (templates/{template}/sessions/{session}) the bridge connection is
 * registered under — the member's dsh session id is namespaced
 * (`{session}/player`) and is NOT the dispatch key, so the caller passes the
 * game session explicitly; the default keeps the single-agent tests working.
 * The instance registers on an `isolate("saoleiGame")` child of the agent
 * context — a per-agent isolation label keeps the underlying registration
 * slot unique per agent (re-materializations and concurrent members never
 * collide), and the isolated child shares the agent scope's fiber, so the
 * service stays resolvable from `agent.ctx` (and from `exec.agent.ctx` in the
 * saolei tools) and unregisters with the agent scope.
 */
export function createAgentGameRuntime(
  agent: Agent,
  desktopBridge: DesktopBridgeService,
  sessionName: string = agent.id,
): SaoleiGame {
  return new GameRuntimeService(agent.ctx.isolate("saoleiGame"), "saoleiGame", {
    sessionName,
    dispatch: (part, signal) => desktopBridge.dispatch(sessionName, part, signal),
    boardApi: createDefaultBoardApi(),
  });
}
