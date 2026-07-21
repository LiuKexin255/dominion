/**
 * validation.ts — Pure saolei update / pre-dispatch validators
 * (FR-013..FR-016).
 *
 * Data-model authority: `specs/018-saolei-mcp/data-model.md` §8.
 * Minesweeper-rule basis: `research.md` D10 (8-connectivity, cascade
 * reveal — https://en.wikipedia.org/wiki/Minesweeper_(video_game) ;
 * https://minesweeper.now/help/gameplay ).
 *
 * Each validator is a pure function — given the current `GameState`, the
 * recorded `lastOp`, and the model's update batch, it returns either
 * `{ ok: true }` or `{ ok: false, reason: "<violated rule detail>" }`.
 * The pure design follows `style/javascript.md` §测试 (DI / table-driven
 * tests): no module state, no side effects, every case is enumerable.
 *
 * FR-017 (extensibility): rule sets are composable per `lastOp.kind`;
 * Phase 6 adds the `flag` / `chord_click` validators, Phase 7 hardens the
 * mine-state semantics — neither changes the click rules here nor the five
 * tool contracts.
 */

import { CellStatus, NUMBER_STATUSES } from "./game-state";
import type { GameState, LastOp } from "./game-state";

/**
 * One cell update reported by `saolei_update`. Coordinates use the
 * top-left origin `(0, 0)`; `status` is the post-operation wire value
 * (contracts/mcp-tool-contract.md `saolei_update` schema).
 */
export interface CellUpdate {
  x: number;
  y: number;
  status: CellStatus;
}

/**
 * Validator outcome. Rejects carry a short human-readable reason surfaced
 * to the model as a normal MCP text result (research.md D8 — never
 * `isError:true`, so `@langchain/mcp-adapters` returns the message instead
 * of raising `ToolException`).
 */
export type ValidationResult =
  | { ok: true }
  | { ok: false; reason: string };

/**
 * FR-016 range check — every coordinate in the batch MUST lie inside
 * `[0,width)×[0,height)`. Used as the shared first-pass check before any
 * `lastOp.kind`-specific validator runs.
 */
export function validateRange(
  state: GameState,
  cells: readonly CellUpdate[],
): ValidationResult {
  for (const cell of cells) {
    if (
      cell.x < 0 ||
      cell.x >= state.width ||
      cell.y < 0 ||
      cell.y >= state.height
    ) {
      return {
        ok: false,
        reason:
          `coordinate (${cell.x},${cell.y}) is out of bounds ` +
          `(grid ${state.width}x${state.height})`,
      };
    }
  }
  return { ok: true };
}

/**
 * FR-013 click pre-dispatch — `saolei_click(x,y)` requires
 * `grid[y][x] == INITIAL`. Reject any target that has already been
 * revealed, flagged, or mine-marked without dispatching and without
 * entering the pending state (Clarification Q3 → A: a rejected operation
 * does NOT lock the alternation; the model may retry immediately).
 */
export function validateClickPreDispatch(
  state: GameState,
  target: { x: number; y: number },
): ValidationResult {
  // Defensive: pre-dispatch target must be on the grid too.
  if (
    target.x < 0 ||
    target.x >= state.width ||
    target.y < 0 ||
    target.y >= state.height
  ) {
    return {
      ok: false,
      reason:
        `click target (${target.x},${target.y}) is out of bounds ` +
        `(grid ${state.width}x${state.height})`,
    };
  }
  const current = state.grid[target.y][target.x];
  if (current !== CellStatus.INITIAL) {
    return {
      ok: false,
      reason:
        `click target (${target.x},${target.y}) is not INITIAL ` +
        `(current=${current})`,
    };
  }
  return { ok: true };
}

/**
 * FR-013 click post-update — the update MUST transition the target cell
 * to a number (0..8) or `HIT_MINE`; the set `N` of cells updated to a
 * number MUST form a single 8-connected region that contains the target
 * (encoding the 0-cell cascade reveal of standard minesweeper —
 * research.md D10). When the target becomes `HIT_MINE` the game ends and
 * the connectivity requirement is relaxed (FR-018 / data-model.md §8).
 */
export function validateClickUpdate(
  state: GameState,
  lastOp: LastOp,
  cells: readonly CellUpdate[],
): ValidationResult {
  // `state` is part of the pure signature so future FR-016 hardening
  // (Phase 7: HIT_MINE vs MINE semantics, target pre-status checks) can
  // consult the grid without changing the call sites; currently unused
  // beyond range checking done by `validateUpdate` upstream.
  void state;

  const target = lastOp.target;

  // FR-013 (target must change): the batch MUST include an entry for the
  // target cell whose new status is a number or HIT_MINE.
  const targetUpdate = cells.find(
    (c) => c.x === target.x && c.y === target.y,
  );
  if (!targetUpdate) {
    return {
      ok: false,
      reason:
        `click update must include the target cell ` +
        `(${target.x},${target.y})`,
    };
  }
  const isNumber = NUMBER_STATUSES.has(targetUpdate.status);
  const isHitMine = targetUpdate.status === CellStatus.HIT_MINE;
  if (!isNumber && !isHitMine) {
    return {
      ok: false,
      reason:
        `click update must change target (${target.x},${target.y}) to ` +
        `a number 0..8 or HIT_MINE (got=${targetUpdate.status})`,
    };
  }

  // FR-018: target hit a mine → game over; the cascade rule is relaxed.
  if (isHitMine) {
    return { ok: true };
  }

  // FR-013 connectivity: N = updated number cells must form a single
  // 8-connected region containing the target. Target is in N (verified
  // above), so a single-region check suffices.
  const numberCells = cells.filter((c) => NUMBER_STATUSES.has(c.status));
  if (!isConnected8(numberCells)) {
    return {
      ok: false,
      reason:
        `click update number cells must form a single 8-connected ` +
        `region containing the target (${target.x},${target.y})`,
    };
  }
  return { ok: true };
}

/**
 * Update dispatcher — runs the shared FR-016 range check, then routes to
 * the validator matching `lastOp.kind`. The `flag` / `chord_click` paths
 * are stubbed until Phase 6 (`saolei_flag` / `saolei_chord_click`
 * handlers); the stubs reject cleanly so a stale `lastOp` from a future
 * session cannot mutate state through an unvalidated path.
 */
export function validateUpdate(
  state: GameState,
  lastOp: LastOp,
  cells: readonly CellUpdate[],
): ValidationResult {
  const range = validateRange(state, cells);
  if (!range.ok) return range;

  switch (lastOp.kind) {
    case "click":
      return validateClickUpdate(state, lastOp, cells);
    case "flag":
      return {
        ok: false,
        reason: "flag update validation not yet implemented (Phase 6)",
      };
    case "chord_click":
      return {
        ok: false,
        reason: "chord_click update validation not yet implemented (Phase 6)",
      };
  }
}

/**
 * 8-connectivity helper (research.md D10) — returns `true` iff the given
 * cells form one connected region via Chebyshev-distance-1 adjacency
 * (horizontal / vertical / diagonal), consistent with minesweeper
 * adjacency. An empty input is treated as vacuously connected.
 *
 * Implementation: BFS over the cell set keyed by `"x,y"`; coincident
 * coordinates are deduplicated first so duplicates don't distort the
 * visited count.
 */
export function isConnected8(
  cells: readonly { x: number; y: number }[],
): boolean {
  if (cells.length === 0) return true;

  const seen = new Map<string, { x: number; y: number }>();
  for (const c of cells) {
    const key = `${c.x},${c.y}`;
    if (!seen.has(key)) seen.set(key, { x: c.x, y: c.y });
  }
  const unique = [...seen.values()];
  if (unique.length === 1) return true;

  const visited = new Set<string>();
  const queue: { x: number; y: number }[] = [unique[0]];
  visited.add(`${unique[0].x},${unique[0].y}`);
  while (queue.length > 0) {
    const cur = queue.shift() as { x: number; y: number };
    for (const next of unique) {
      const key = `${next.x},${next.y}`;
      if (visited.has(key)) continue;
      const dx = Math.abs(next.x - cur.x);
      const dy = Math.abs(next.y - cur.y);
      // Chebyshev distance == 1 and not the same cell.
      if (dx <= 1 && dy <= 1 && dx + dy > 0) {
        visited.add(key);
        queue.push(next);
      }
    }
  }
  return visited.size === unique.length;
}
