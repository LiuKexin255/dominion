/**
 * cordis plugin entry for the saolei tools plugin: registers the
 * saolei_init/saolei_operate/saolei_remain tools and the `saolei:guidance`
 * prompt section. The tools are stateless model-facing entry points — all
 * game state and execution live in the saolei-loop plugin's GameRuntime
 * (FR-013), resolved per call through the calling agent's scope.
 *
 * `saoleiGame` is an agent-scoped service (registered on `agent.ctx` by the
 * host's agent-creation setup hook when an agent materializes — the
 * saolei-loop plugin's createAgentGameRuntime; at plugin-load time no agent
 * exists), so it CANNOT be statically injected — the tool exec bodies
 * resolve it lazily via `exec.agent.ctx` (dsh-tools `ToolExecution.agent`
 * carries the calling agent; research.md D7). `exec.agent` absent, or no
 * `saoleiGame` in the caller's scope (non-loop-driven call, or the service
 * already unregistered with the agent scope), fails loud — never a silent
 * success. Contract: specs/051-agent-v2-dsh-migration/contracts/
 * saolei-plugins.md §3.
 */

import { defineTool } from "@deepseek-ai/dsh-tools";
import type { ToolRunContext } from "@deepseek-ai/dsh-tools";
import type { Context } from "@deepseek-ai/cordis";

import type { OperateInput, SaoleiGame, ToolOutcome } from "@dominion/dsh-saolei-loop";

export const name = "saolei";

export const inject = ["tools", "systemPrompt"];

// ── v1 argument-combination rejection literals (verbatim, data-model.md
// §2.5 双形式 operate 入参 — refuse ambiguity over silent precedence) ────────

/** Both the single and the batch form supplied. */
export const AMBIGUOUS_ARGS_TEXT =
  "saolei_operate → rejected: provide EITHER type/x/y (single operation) " +
  "OR operations (batch), not both.";

/** Neither form supplied. */
export const MISSING_ARGS_TEXT =
  "saolei_operate → rejected: provide EITHER type/x/y (single operation) " +
  "OR an operations array (batch).";

/** A partial single form (e.g. type without x/y). */
export const INCOMPLETE_ARGS_TEXT =
  "saolei_operate → rejected: the single-operation form requires ALL of " +
  "type, x and y together.";

/** Model arguments of the dual-form `saolei_operate` (all optional — the
 * presence combination is validated by {@link normalizeOperateArgs}). */
interface OperateToolArgs {
  type?: "click" | "flag" | "chord";
  x?: number;
  y?: number;
  operations?: { type: "click" | "flag" | "chord"; x: number; y: number }[];
}

/**
 * Validate the dual-form combination and normalize to an `OperateInput`.
 * Returns the literal rejection text on an illegal combination (the call is
 * refused, NOTHING is dispatched — a normal result the model can act on).
 */
function normalizeOperateArgs(args: OperateToolArgs): OperateInput | { rejection: string } {
  const { type, x, y, operations } = args;
  if (type != null || x != null || y != null) {
    if (operations != null) {
      return { rejection: AMBIGUOUS_ARGS_TEXT };
    }
    if (type == null || x == null || y == null) {
      return { rejection: INCOMPLETE_ARGS_TEXT };
    }
    return { type, x, y };
  }
  if (operations != null) {
    return { operations };
  }
  return { rejection: MISSING_ARGS_TEXT };
}

/**
 * Resolve the calling agent's game runtime. Failure is fail-loud: a missing
 * `exec.agent` (non-loop-driven dispatch) or an out-of-scope `saoleiGame`
 * (the owning agent scope unloaded) throws — the pipeline turns the throw
 * into a model-visible error result, never a fabricated success.
 *
 * Resolution reads the `saoleiGame` context PROPERTY (the proxy's
 * fiber-walking lookup), not `ctx.get` — the builder registers the runtime
 * under a per-agent isolation label (`agent.ctx.isolate("saoleiGame")`,
 * createAgentGameRuntime), which the property walk resolves from the shared
 * agent fiber while `get`'s label-keyed store lookup would not.
 */
function resolveRuntime(exec: ToolRunContext): SaoleiGame {
  if (exec.agent === undefined) {
    throw new Error(
      "saolei tools require a loop-driven agent caller: no agent on the tool execution",
    );
  }
  const runtime = exec.agent.ctx.saoleiGame;
  if (runtime === undefined) {
    throw new Error(
      `saolei tools require the agent-scoped "saoleiGame" service; agent "${exec.agent.id}" has none (the materialization setup registers it on agent.ctx via createAgentGameRuntime)`,
    );
  }
  return runtime;
}

/** Run one runtime call and map its outcome to the canonical output. Error
 * outcomes throw (model-visible failure); text outcomes become the rendered
 * `{result}` value. A successful outcome carrying the runtime's terminal-board
 * marker (`concludesTurn: true`) first concludes the calling turn through the
 * dsh-tools seam — the result still commits normally afterwards
 * (specs/062-team-game-end-handoff/contracts/saolei-turn-conclude.md §2). */
async function executeOutcome(
  exec: ToolRunContext,
  run: (runtime: SaoleiGame) => ToolOutcome | Promise<ToolOutcome>,
): Promise<{ result: string }> {
  const outcome = await run(resolveRuntime(exec));
  if (outcome.isError) {
    throw new Error(outcome.error.message);
  }
  if (outcome.concludesTurn === true) {
    exec.concludeTurn();
  }
  return { result: outcome.text };
}

/** The canonical `{result: string}` output schema shared by the three tools
 * (as const keeps the literal types the schema DSL compiles from). */
const RESULT_OUTPUT_SCHEMA = {
  type: "object",
  properties: {
    result: {
      type: "string",
      required: true,
      description: "The saolei result text: outcome line, game status line, text board",
    },
  },
  additionalProperties: false,
} as const;

/** Build the output declaration shared by the three tools; render emits the
 * board text as the model-facing content. */
function resultOutput() {
  return {
    schema: RESULT_OUTPUT_SCHEMA,
    render: (_args: unknown, value: { result: string }) => [{ type: "text" as const, text: value.result }],
  };
}

/**
 * Apply the plugin: three global tool registrations (one registry, every
 * agent sees them; per-agent routing happens at resolution time) plus the
 * tool-guidance prompt section (order 100 — the official tool-guidance
 * band 100–199).
 */
export function apply(ctx: Context): void {
  ctx.tools.register(
    defineTool({
      name: "saolei_init",
      description:
        "Start a new minesweeper game. Dispatches an F2 keypress (the " +
        "new-game shortcut) to the bound desktop window, recognizes the " +
        "post-init board, and returns it as a TEXT board (no image). Takes " +
        "no arguments — the board bounds are inferred from the returned " +
        "screenshot. Re-calling re-dispatches F2 (restarts the game) and " +
        "re-seeds the board.",
      parameters: {},
      output: resultOutput(),
      execute: (_args, exec) => executeOutcome(exec, (runtime) => runtime.init(exec.signal)),
    }),
  );

  ctx.tools.register(
    defineTool({
      name: "saolei_operate",
      description:
        "Execute one or more minesweeper cell operations IN ORDER and " +
        "return ONE result with the final TEXT board. Operation types " +
        "(click/flag/chord): click = left-click to reveal a cell; flag = " +
        "right-click to place/toggle a flag; chord = simultaneous left+" +
        "right press on a revealed number 1–8 to expand it. Two mutually " +
        "exclusive argument forms: a single operation via type/x/y, OR a " +
        "batch via the operations array [{type, x, y}, ...] (order " +
        "preserved). Top-left origin (0, 0); x = column, y = row. Each op " +
        "is validated strictly against the recognized board before " +
        "dispatch: a no-op rejection is SKIPPED and execution continues; a " +
        "structural rejection (out-of-bounds, no active game) or a game " +
        "end STOPS the batch — earlier successful operations take effect.",
      // The operation coordinates are deliberately schema-unbounded: the
      // dsh-tools 0.1.1-rc.2 enforced schema subset has no numeric-bound
      // keyword (CONSTRAINT_KEYWORDS = type/oneOf/properties/required/
      // additionalProperties/items/enum/const — a `minimum` node is REJECTED
      // at registration, not ignored), and the board dimensions are only
      // known after recognition anyway. The negative-coordinate rejection
      // therefore happens at the runtime rule table: validateMove answers
      // out_of_bounds before any dispatch.
      parameters: {
        type: {
          type: "string",
          enum: ["click", "flag", "chord"],
          description: "Single form: the operation type (mutually exclusive with operations)",
        },
        x: {
          type: "integer",
          description: "Single form: column index (0-based)",
        },
        y: {
          type: "integer",
          description: "Single form: row index (0-based)",
        },
        operations: {
          type: "array",
          description: "Batch form: ordered cell operations (mutually exclusive with type/x/y)",
          items: {
            type: "object",
            additionalProperties: false,
            properties: {
              type: { type: "string", enum: ["click", "flag", "chord"], required: true },
              x: { type: "integer", required: true },
              y: { type: "integer", required: true },
            },
          },
        },
      },
      output: resultOutput(),
      execute: (args, exec) => {
        const normalized = normalizeOperateArgs(args);
        if ("rejection" in normalized) {
          return Promise.resolve({ result: normalized.rejection });
        }
        return executeOutcome(exec, (runtime) => runtime.operate(normalized, exec.signal));
      },
    }),
  );

  // Remain semantics wording (primary meaning: mines still unmarked per
  // number cell; explicit flag-count exclusion) follows the terminal text
  // contract in specs/064-memory-split-fold-remain/contracts/saolei-plugins.md
  // §2.
  ctx.tools.register(
    defineTool({
      name: "saolei_remain",
      description:
        "Read-only deduction view. Takes NO arguments and dispatches " +
        "NOTHING to the desktop. For every revealed number cell (1–8) it " +
        "returns the count of mines still unmarked around it (= cell " +
        "number − adjacent flags; may be 0 or NEGATIVE when over-flagged). " +
        "It is NOT the count of flags. Every other cell (0, *, F, X, M, ?) " +
        "shows `-`. Columns are x and rows are y, the same ruler as the " +
        "board grid. Rejects with `no_active_game` only when no board is " +
        "recognized; a terminal board is not blocked (pure query).",
      parameters: {},
      output: resultOutput(),
      execute: (_args, exec) => executeOutcome(exec, (runtime) => runtime.remain()),
    }),
  );

  ctx.systemPrompt.section({
    name: "saolei:guidance",
    order: 100,
    text: SAOLEI_GUIDANCE,
  });
}

/**
 * The saolei tool-guidance section (FR-014: the plugin owns its tools'
 * cross-call guidance as a prompt section — no separate skill-file form)
 * adapted to the plugin tool context (no MCP wording, no raw-mouse-tool
 * reference — the composition mounts no generic mouse tools). Tool usage
 * ONLY: symbol/coordinate reading, result body shape, call forms, and
 * validation semantics; the game rules live in the saolei-loop plugin's
 * `saolei:game` section
 * (specs/060-agent-v2-team-optimize/contracts/prompt-sections.md §2). The
 * `saolei_remain` wording in the tool description and this guidance entry
 * follows specs/064-memory-split-fold-remain/contracts/saolei-plugins.md
 * §2/§4.
 */
export const SAOLEI_GUIDANCE = `## saolei (Minesweeper tools)

Play the desktop Minesweeper game ONLY through the three saolei tools. The agent recognizes the board deterministically from the desktop screenshot and returns it as TEXT — every tool result is a text board; there is NO screenshot to read and you must not try to read pixels.

### Symbol legend

| Symbol | Meaning |
|---|---|
| \`*\` | Unrevealed (initial) cell |
| \`0\`–\`8\` | Revealed number |
| \`F\` | Flag |
| \`X\` | Triggered mine |
| \`M\` | Mine shown on the end-game board |
| \`?\` | Recognition uncertain (treat it as possibly unrevealed) |

### Coordinate ruler

Every text board carries a tagged ruler: a header row \`col0 col1 …\` above the grid and a \`row0\`, \`row1\`, … prefix per row. Indices are 0-based, top-left origin — identical to the \`(x, y)\` arguments of \`saolei_operate\`. \`col3\`/\`row1\` are labels, never game-state digits. Read the header and prefix, and pass those N values as \`(x, y)\`.

Example (9-wide board):

\`\`\`
board size 9*9

     col0 col1 col2 col3 col4 col5 col6 col7 col8
row0    *    *    *    1    0    0    1    M    *
row1    *    *    2    1    0    0    1    2    *
row2    *    *    1    0    0    0    0    1    *
\`\`\`

### Tool-result body shape

Every result body has three layers, in this fixed order:

1. **Outcome line** — \`new game started\` (init); \`saolei_operate → executed N ops\` (with \`, skipped S no-op ops\` when any were skipped); \`saolei_operate → stopped at type(x,y) (reason)\` (mid-batch stop); \`rejected: <reason>\`; \`unable to recognize board\`; \`saolei_remain → computed\`.
2. **Game-status line** — \`game status: won|lost|playing\`, the recognized board's state. Read it BEFORE parsing the board. It is omitted only when there is no recognized board (\`no_active_game\`, \`unable to recognize board\`) or on an illegal-argument rejection.
3. **The text board** — the \`board size <w>*<h>\` header and the symbol grid. The \`valid range: x 0..<w-1>, y 0..<h-1>\` line appears on \`rejected: <reason>\` bodies only.

A won/lost board is TERMINAL for cell operations: any further cell operation stops before dispatch with \`game_won\`/\`game_over\`. Call \`saolei_init\` to start a new game.

### Tools

- \`saolei_init()\` — no arguments. Dispatches the F2 new-game keypress, recognizes the initial board, returns it as TEXT. Call it FIRST, and again whenever the game should restart (re-calling re-dispatches F2 and re-seeds the board).
- \`saolei_operate(type, x, y)\` / \`saolei_operate(operations: [{type, x, y}, ...])\` — execute one or more cell operations in order and return ONE result with the final board. The two forms are mutually exclusive and semantically equivalent (single = length-1 batch); all three of type/x/y must be present together. Operation types:
  - \`click\` — a left-click on one cell.
  - \`flag\` — a right-click on one cell (places/removes the flag).
  - \`chord\` — ONE atomic simultaneous left+right press on a revealed number 1–8. NEVER emulate a chord with two separate click ops.
- \`saolei_remain()\` — read-only. No dispatch, no board change. For every revealed number cell it returns the count of mines still unmarked around it (\`cell number − adjacent flags\`; may be 0 or NEGATIVE). It is NOT the count of flags. Other cells show \`-\`. Not blocked by a terminal board.

### Validation triage (illegal moves are handled before dispatch)

Every op is validated against the recognized board; the desktop never receives an illegal operation:

- **Harmless no-op → SKIPPED, batch continues**: \`cell_already_revealed\` (click on 0–8), \`cell_is_flagged\` (click on F), \`cannot_flag_revealed\` (flag on 0–8), \`chord_requires_number\` (chord on non-number), \`chord_no_unrevealed_neighbor\` (chord with nothing left to reveal).
- **Structural / terminal → batch STOPS** (\`stopped at type(x,y) (reason)\`; earlier successful ops take effect): \`out_of_bounds\`, \`no_active_game\`, \`game_over\`, \`game_won\`.
- A rejection is a NORMAL result, not an error: read the reason and the board, then pick a legal cell. A chord that reveals nothing is still LEGAL (not a rejection). A \`?\` cell is never rejected for being uncertain.
- Illegal argument combinations are refused verbatim: both forms — \`provide EITHER type/x/y (single operation) OR operations (batch), not both.\`; neither — \`provide EITHER type/x/y (single operation) OR an operations array (batch).\`; partial single form — \`the single-operation form requires ALL of type, x and y together.\`
- An empty \`operations\` list is a no-op returning the current board.
- If recognition fails (\`unable to recognize board\`), the state is invalidated; subsequent cell ops answer \`no_active_game\` until you call \`saolei_init\` again.

### Example flow

\`\`\`
1. saolei_init()
   → new game started / game status: playing / board size 9*9 (all *)
2. saolei_operate(type="click", x=4, y=4)
   → saolei_operate → executed 1 ops / game status: playing / the revealed board
3. saolei_operate(operations=[{type:"click",x:5,y:4},{type:"flag",x:3,y:3},{type:"chord",x:4,y:4}])
   → executed 3 ops in order, ONE result with the final board
4. (terminal) the op whose board first shows won/lost reports it:
   saolei_operate → stopped at flag(3,3) (won) / game status: won
\`\`\`

### Do not

- Do NOT read pixels or expect a screenshot — tool results are text boards only.
- Do NOT emulate a chord with two clicks — use one \`chord\` op.
- There is no status-reporting tool: the agent recognizes the board itself after every operation.`;
