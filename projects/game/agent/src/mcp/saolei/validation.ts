/**
 * validation.ts — Pure saolei update / pre-dispatch validators
 * (FR-013..FR-016).
 *
 * Data-model authority: `specs/018-saolei-mcp/data-model.md` §8.
 * Minesweeper-rule basis: `research.md` D10 (8-connectivity, cascade
 * reveal — https://en.wikipedia.org/wiki/Minesweeper_(video_game) ;
 * https://minesweeper.now/help/gameplay ) and the chording technique
 * ( https://rarepike.com/minesweeper/chord-technique/ — satisfied-number
 * rule, misplaced-flag mine-hit).
 *
 * Each validator is a pure function — given the current `GameState`, the
 * recorded `lastOp`, and the model's update batch, it returns either
 * `{ ok: true }` or `{ ok: false, reason: "<violated rule detail>" }`.
 * The pure design follows `style/javascript.md` §测试 (DI / table-driven
 * tests): no module state, no side effects, every case is enumerable.
 *
 * FR-017 (extensibility): rule sets are composable per `lastOp.kind`;
 * Phase 5 ships the `click` validator, Phase 6 adds `flag` (FR-014) and
 * `chord_click` (FR-015), Phase 7 hardens the mine-state semantics —
 * none of these change the five tool contracts.
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
 *
 * Phase 7 mine-state hardening (FR-016 / D6): when the target is a number
 * (the click did NOT end the game), the batch MUST NOT contain any
 * `HIT_MINE` or `MINE` cell. `HIT_MINE` is the mine directly triggered by
 * the current operation (research.md D6) — a click only triggers it on
 * the clicked cell, so `HIT_MINE` on any other cell is impossible.
 * `MINE` denotes mines shown at game end (research.md D6) — the game is
 * still in progress, so `MINE` is inconsistent. Either case violates
 * FR-016 ("statuses inconsistent with the operation performed").
 */
export function validateClickUpdate(
  state: GameState,
  lastOp: LastOp,
  cells: readonly CellUpdate[],
): ValidationResult {
  // `state` remains in the pure signature for future FR-016 hardening
  // (e.g. target pre-status checks consulting the grid). Phase 7's
  // mine-state consistency check derives from the batch + lastOp alone.
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

  // FR-016 / D6 mine-state consistency: target is a number → the click
  // did NOT end the game. A click only triggers HIT_MINE on the clicked
  // cell (research.md D6), and MINE denotes mines shown at game end
  // (research.md D6). Neither status may appear in a non-game-ending
  // click batch; encountering either is inconsistent with the performed
  // operation and rejected (FR-016).
  for (const c of cells) {
    if (
      c.status === CellStatus.HIT_MINE ||
      c.status === CellStatus.MINE
    ) {
      return {
        ok: false,
        reason:
          `click update contains a mine-state cell ` +
          `(${c.x},${c.y})=${c.status} but the target is a number ` +
          `(game not over; MINE/HIT_MINE only appear at game end — ` +
          `research.md D6)`,
      };
    }
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
 * FR-014 flag pre-dispatch — `saolei_flag(x,y)` requires
 * `grid[y][x] == INITIAL`. A flag may only be placed on an unopened,
 * unflagged cell; flagging an already-revealed number, an existing flag,
 * or a mine-state cell is rejected without dispatching and without
 * entering the pending state (Clarification Q3 → A).
 *
 * FR-008 makes flag a toggle "only between the initial and flagged
 * states"; since pre-dispatch requires INITIAL, the only legal post-update
 * transition is INITIAL → FLAG (the reverse direction is unreachable from
 * a legal pre-state).
 */
export function validateFlagPreDispatch(
  state: GameState,
  target: { x: number; y: number },
): ValidationResult {
  // Defensive: pre-dispatch target must be on the grid.
  if (
    target.x < 0 ||
    target.x >= state.width ||
    target.y < 0 ||
    target.y >= state.height
  ) {
    return {
      ok: false,
      reason:
        `flag target (${target.x},${target.y}) is out of bounds ` +
        `(grid ${state.width}x${state.height})`,
    };
  }
  const current = state.grid[target.y][target.x];
  if (current !== CellStatus.INITIAL) {
    return {
      ok: false,
      reason:
        `flag target (${target.x},${target.y}) is not INITIAL ` +
        `(current=${current})`,
    };
  }
  return { ok: true };
}

/**
 * FR-014 flag post-update — the update MUST change only the target cell,
 * and only between `INITIAL` ↔ `FLAG`. No other cell may change; no other
 * transition is permitted.
 *
 * Because `validateFlagPreDispatch` required the target to be INITIAL and
 * the grid is not mutated between dispatch and update (only `pendingUpdate`
 * and `lastOp` are set), the only legal transition observable in the batch
 * is INITIAL → FLAG. A no-op (INITIAL → INITIAL) is not a transition and
 * is rejected; any other status (number / HIT_MINE / MINE) is rejected.
 * Cells in the batch that are not the target are rejected (no other cell
 * may change).
 */
export function validateFlagUpdate(
  state: GameState,
  lastOp: LastOp,
  cells: readonly CellUpdate[],
): ValidationResult {
  // `state` is part of the pure signature for consistency with
  // `validateClickUpdate` and future FR-016 hardening (Phase 7); the
  // flag rule currently derives everything from `lastOp` + `cells`.
  void state;

  const target = lastOp.target;

  // FR-014 (no other cell may change): every entry in the batch MUST be
  // the target. Duplicates of the target are tolerated (the apply step
  // resolves them by last-wins); any non-target cell violates the rule.
  for (const c of cells) {
    if (c.x !== target.x || c.y !== target.y) {
      return {
        ok: false,
        reason:
          `flag update must change only the target cell ` +
          `(${target.x},${target.y}); got extraneous cell ` +
          `(${c.x},${c.y})`,
      };
    }
  }

  // FR-014 (target MUST transition between INITIAL ↔ FLAG).
  const targetUpdate = cells.find(
    (c) => c.x === target.x && c.y === target.y,
  );
  if (!targetUpdate) {
    return {
      ok: false,
      reason:
        `flag update must include the target cell ` +
        `(${target.x},${target.y})`,
    };
  }
  if (targetUpdate.status !== CellStatus.FLAG) {
    return {
      ok: false,
      reason:
        `flag update target (${target.x},${target.y}) must transition ` +
        `INITIAL↔FLAG (got=${targetUpdate.status})`,
    };
  }
  return { ok: true };
}

/**
 * FR-015 chord pre-dispatch — `saolei_chord_click(x,y)` requires the
 * target to be a non-0 number (`1..8`) AND the count of adjacent `FLAG`
 * cells to equal the target's number (the "satisfied number" rule,
 * https://rarepike.com/minesweeper/chord-technique/ ). Reject otherwise
 * without dispatching and without entering the pending state
 * (Clarification Q3 → A).
 *
 * Adjacency is 8-connectivity (research.md D10 — consistent with
 * minesweeper adjacency and the click connectivity rule).
 */
export function validateChordPreDispatch(
  state: GameState,
  target: { x: number; y: number },
): ValidationResult {
  // Defensive: pre-dispatch target must be on the grid.
  if (
    target.x < 0 ||
    target.x >= state.width ||
    target.y < 0 ||
    target.y >= state.height
  ) {
    return {
      ok: false,
      reason:
        `chord target (${target.x},${target.y}) is out of bounds ` +
        `(grid ${state.width}x${state.height})`,
    };
  }
  const current = state.grid[target.y][target.x];

  // FR-015 (target must be a non-0 number). NUMBER_0 is excluded — a 0 has
  // no adjacent mines and thus no flags to satisfy; chording it is a no-op
  // in standard minesweeper and the rule rejects it.
  if (current === CellStatus.NUMBER_0 || !NUMBER_STATUSES.has(current)) {
    return {
      ok: false,
      reason:
        `chord target (${target.x},${target.y}) must be a non-0 number ` +
        `1..8 (current=${current})`,
    };
  }

  // FR-015 (satisfied number): adjacent FLAG count == the target's number.
  // Number statuses are the string digits "1".."8"; parse with `Number`.
  const targetNumber = Number(current);
  const flagCount = countAdjacentFlags(state, target);
  if (flagCount !== targetNumber) {
    return {
      ok: false,
      reason:
        `chord target (${target.x},${target.y}) number=${targetNumber} ` +
        `but adjacent flags=${flagCount} (must be equal — satisfied-number rule)`,
    };
  }
  return { ok: true };
}

/**
 * FR-015 chord post-update — enforce the post-chord update shape:
 *
 *   1. No target-adjacent FLAG cell may change (flags are the chord's
 *      precondition; chording never toggles them).
 *   2. Every other target-adjacent non-number cell MUST be updated to a
 *      number or `HIT_MINE`/`MINE` — chording reveals all unflagged
 *      neighbors. **Exception** (FR-019): if the operation hit a mine
 *      (a flag was misplaced), only the hit mine need be updated.
 *   3. Let N = updated number cells. Each 8-connected component of N MUST
 *      contain at least one cell adjacent to the chord target (the chord
 *      only reveals target's neighbors; cascades run through them).
 *
 * The mine-hit exception (rule 2 exception) applies when the batch
 * contains any `HIT_MINE` cell. The detonated mine MUST be target-adjacent
 * (chord reveals only target's neighbors); other rules are then relaxed
 * (research.md D10 / spec Edge Case "chord hits a mine").
 */
export function validateChordUpdate(
  state: GameState,
  lastOp: LastOp,
  cells: readonly CellUpdate[],
): ValidationResult {
  const target = lastOp.target;

  // Index the batch by coordinate for O(1) status lookups.
  const updates = new Map<string, CellStatus>();
  for (const c of cells) {
    updates.set(`${c.x},${c.y}`, c.status);
  }

  // FR-019 mine-hit exception: a chord can detonate at most one mine — it
  // must be a target-adjacent cell (chord only reveals target's
  // neighbors). When present, the "every neighbor must be updated" rule
  // is relaxed (only the hit mine is required).
  const hitMines = cells.filter((c) => c.status === CellStatus.HIT_MINE);
  if (hitMines.length > 0) {
    for (const hm of hitMines) {
      if (!isAdjacentTo(hm, target)) {
        return {
          ok: false,
          reason:
            `chord hit-mine cell (${hm.x},${hm.y}) must be adjacent to ` +
            `the chord target (${target.x},${target.y})`,
        };
      }
    }
    return { ok: true };
  }

  // FR-016 / D6 mine-state consistency: no HIT_MINE in the batch → the
  // chord did NOT detonate a mine, so the game continues. `MINE` denotes
  // mines shown at game end (research.md D6); a `MINE` cell that is NOT
  // adjacent to the chord target is inconsistent — the chord only reveals
  // the target's neighbours, so a far-away MINE cannot be a chord product
  // and the game has not ended. Target-adjacent `MINE` is tolerated per
  // data-model.md §8 (rule 2's defensive allowance includes MINE in the
  // status set for revealed neighbours).
  for (const c of cells) {
    if (c.status === CellStatus.MINE && !isAdjacentTo(c, target)) {
      return {
        ok: false,
        reason:
          `chord update contains a MINE cell (${c.x},${c.y}) not ` +
          `adjacent to the chord target (${target.x},${target.y}) ` +
          `(MINE only appears at game end — research.md D6)`,
      };
    }
  }

  // FR-015 rule 1: target-adjacent FLAG cells MUST NOT change.
  // FR-015 rule 2: every other target-adjacent non-number cell MUST be
  // updated to a number, HIT_MINE, or MINE. "Other" = non-FLAG, non-number
  // — in practice, INITIAL (HIT_MINE/MINE pre-states mean the game is
  // already over and a chord would not occur).
  for (const n of adjacentCells(state, target)) {
    const current = state.grid[n.y][n.x];
    const newStatus = updates.get(`${n.x},${n.y}`);
    if (current === CellStatus.FLAG) {
      if (newStatus !== undefined && newStatus !== CellStatus.FLAG) {
        return {
          ok: false,
          reason:
            `chord update must not change target-adjacent FLAG cell ` +
            `(${n.x},${n.y}) (got=${newStatus})`,
        };
      }
    } else if (!NUMBER_STATUSES.has(current)) {
      // INITIAL / HIT_MINE / MINE — treat as "must be updated" (in
      // practice only INITIAL is reachable; HIT_MINE/MINE pre-states
      // imply prior game-over and the chord validator wouldn't be
      // reachable through a legal `validateChordPreDispatch` path).
      if (newStatus === undefined) {
        return {
          ok: false,
          reason:
            `chord update must update target-adjacent non-number cell ` +
            `(${n.x},${n.y}) (current=${current})`,
        };
      }
      const isNumber = NUMBER_STATUSES.has(newStatus);
      const isMineReveal =
        newStatus === CellStatus.HIT_MINE || newStatus === CellStatus.MINE;
      if (!isNumber && !isMineReveal) {
        return {
          ok: false,
          reason:
            `chord update target-adjacent cell (${n.x},${n.y}) must ` +
            `become a number or MINE/HIT_MINE (got=${newStatus})`,
        };
      }
    }
  }

  // FR-015 rule 3: each connected component of updated number cells MUST
  // contain at least one cell adjacent to the chord target.
  const numberCells = cells.filter((c) => NUMBER_STATUSES.has(c.status));
  for (const component of connectedComponents(numberCells)) {
    const touchesTarget = component.some(
      (c) => isAdjacentTo(c, target) || (c.x === target.x && c.y === target.y),
    );
    if (!touchesTarget) {
      return {
        ok: false,
        reason:
          `chord update has a connected component of number cells ` +
          `not adjacent to the chord target (${target.x},${target.y})`,
      };
    }
  }
  return { ok: true };
}

/**
 * Update dispatcher — runs the shared FR-016 range check, then routes to
 * the validator matching `lastOp.kind`. Phase 5 wires the click validator
 * (FR-013); Phase 6 adds the flag (FR-014) and chord (FR-015) validators.
 * The dispatcher is the single seamed entry point used by the
 * `saolei_update` handler so adding a new operation kind (FR-017
 * extensibility) only needs a new case + validator function.
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
      return validateFlagUpdate(state, lastOp, cells);
    case "chord_click":
      return validateChordUpdate(state, lastOp, cells);
  }
}

/**
 * Chebyshev-distance-1 adjacency (8-connectivity, research.md D10) — two
 * distinct cells are adjacent iff they differ by at most 1 in each axis
 * and are not the same cell. Exported for table-tested coverage and for
 * the chord validators' "adjacent to target" checks.
 */
export function isAdjacentTo(
  a: { x: number; y: number },
  b: { x: number; y: number },
): boolean {
  const dx = Math.abs(a.x - b.x);
  const dy = Math.abs(a.y - b.y);
  return dx <= 1 && dy <= 1 && dx + dy > 0;
}

/**
 * Return all in-bounds 8-connectivity neighbours of `target` on `state`'s
 * grid. Used by the chord validators to iterate the chord's affected
 * neighbourhood. Order is row-major (deterministic for table tests).
 */
function adjacentCells(
  state: GameState,
  target: { x: number; y: number },
): { x: number; y: number }[] {
  const out: { x: number; y: number }[] = [];
  for (let dy = -1; dy <= 1; dy++) {
    for (let dx = -1; dx <= 1; dx++) {
      if (dx === 0 && dy === 0) continue;
      const x = target.x + dx;
      const y = target.y + dy;
      if (x >= 0 && x < state.width && y >= 0 && y < state.height) {
        out.push({ x, y });
      }
    }
  }
  return out;
}

/** Count the FLAG cells in `target`'s 8-neighbourhood (FR-015 precondition). */
function countAdjacentFlags(
  state: GameState,
  target: { x: number; y: number },
): number {
  let count = 0;
  for (const n of adjacentCells(state, target)) {
    if (state.grid[n.y][n.x] === CellStatus.FLAG) count++;
  }
  return count;
}

/**
 * Compute the 8-connected components of a set of cells (research.md D10).
 * Each component is the maximal set of cells reachable via Chebyshev-1
 * steps within the input. Exported so the chord post-update connectivity
 * rule (FR-015 rule 3) is auditable via table tests.
 */
export function connectedComponents(
  cells: readonly { x: number; y: number }[],
): { x: number; y: number }[][] {
  // Deduplicate by coordinate so coincident inputs don't skew BFS.
  const seen = new Map<string, { x: number; y: number }>();
  for (const c of cells) {
    const key = `${c.x},${c.y}`;
    if (!seen.has(key)) seen.set(key, { x: c.x, y: c.y });
  }
  const unique = [...seen.values()];

  const visited = new Set<string>();
  const components: { x: number; y: number }[][] = [];

  for (const start of unique) {
    const startKey = `${start.x},${start.y}`;
    if (visited.has(startKey)) continue;
    const component: { x: number; y: number }[] = [];
    const queue: { x: number; y: number }[] = [start];
    visited.add(startKey);
    while (queue.length > 0) {
      const cur = queue.shift() as { x: number; y: number };
      component.push(cur);
      for (const next of unique) {
        const nextKey = `${next.x},${next.y}`;
        if (visited.has(nextKey)) continue;
        if (isAdjacentTo(cur, next)) {
          visited.add(nextKey);
          queue.push(next);
        }
      }
    }
    components.push(component);
  }
  return components;
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
