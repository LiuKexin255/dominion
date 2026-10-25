/**
 * The board-change event handler (specs/078-saolei-js-hooks/data-model.md §3.3;
 * specs/078-saolei-js-hooks/contracts/hook-announcement.md §2;
 * specs/078-saolei-js-hooks/spec.md FR-008–FR-011, US1): every board-changing
 * `operate` the game runtime reports through its event seam
 * (specs/078-saolei-js-hooks/contracts/board-change-event.md) runs each
 * registered hook in registration order against the entry's board snapshot, and
 * every non-empty result is broadcast through the injected announce seam.
 *
 * The handler owns the whole synchronous execution: it reads the shared
 * registry (the same instance the planner's `saolei_hook` tool writes,
 * specs/078-saolei-js-hooks/data-model.md §3.1), runs each compiled script
 * through the sandbox executor and renders the announcement text itself. It
 * knows nothing about the announcer, the team or the orchestration — `announce`
 * is a plain function the composition injects
 * (specs/078-saolei-js-hooks/spec.md FR-016; the orchestration binds it to its
 * announcer, specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md §5).
 *
 * Failure isolation is this subscriber's own boundary
 * (specs/078-saolei-js-hooks/contracts/board-change-event.md §3): the game
 * foundation runs the callback without any try/catch, so the callback swallows
 * whatever it throws and reports it to the logger instead — a hook-side failure
 * is never allowed to escape into the domain result of `operate`
 * (specs/078-saolei-js-hooks/spec.md FR-013).
 */

import type { GameLogEntry } from "@dominion/dsh-saolei";

import { EXECUTION_TIMEOUT_MS } from "./limits.js";
import type { HookRegistry } from "./registry.js";
import { runHookScript } from "./sandbox.js";
import type { HookRunResult } from "./sandbox.js";

/**
 * The warning face the handler reports a swallowed callback failure to. It
 * follows the cordis plugins' `ctx.logger?.warn?.()` convention
 * (common/js/dsh-plugins/team/src/team.ts;
 * common/js/dsh-plugins/saolei/loop/src/agent-member.ts): the host injects its
 * own logger, and the handler stays silent without one.
 */
export interface OnBoardChangeLogger {
  warn(message: string): void;
}

/** Collaborators of the board-change handler (all injected — the DI seam of
 * style/javascript.md §Mock 约定). */
export interface OnBoardChangeDeps {
  /** The shared hook registry — written by the planner tool, read here per
   * event (specs/078-saolei-js-hooks/data-model.md §3.1). */
  readonly registry: HookRegistry;
  /** The announcement seam: the composition's announcer broadcast face,
   * invoked once per non-empty hook result with the rendered text. */
  readonly announce: (text: string) => void;
  /** Optional failure reporter; absent keeps the swallowed failure silent, the
   * shape the context-backed plugins' `ctx.logger?.warn?.()` takes
   * (common/js/dsh-plugins/team/src/team.ts;
   * common/js/dsh-plugins/saolei/loop/src/agent-member.ts for the same
   * in-flight-delivery swallow,
   * specs/077-announcer-member-unification/contracts/inflight-broadcast-fastpath.md
   * §2.1). */
  readonly logger?: OnBoardChangeLogger;
}

/**
 * Build the event handler over one registry and one announcement seam. The
 * returned callback is the game runtime's `onBoardChange`
 * (specs/078-saolei-js-hooks/data-model.md §1.1). The registry is read per
 * event, so a hook the planner registers after this construction is picked up
 * by the next board change — the composition builds the handler first and
 * registers hooks through the tool later.
 */
export function createOnBoardChange(
  deps: OnBoardChangeDeps,
): (entry: GameLogEntry) => void {
  return (entry) => {
    // One try/catch around the whole read-and-run loop: the executor classifies
    // its own script failures, so the only expected throw here is the
    // announcement seam itself. A throwing announcement aborts this event — the
    // remaining hooks do not run, and the failure is swallowed rather than
    // propagated into `operate`
    // (specs/078-saolei-js-hooks/contracts/board-change-event.md §3). The
    // registry read sits inside the block too, so the whole handler is
    // throw-free as documented.
    try {
      const hooks = deps.registry.entriesInOrder();
      for (const hook of hooks) {
        const text = announcementFor(
          hook.name,
          runHookScript(hook.script, entry.state),
        );
        if (text !== undefined) {
          deps.announce(text);
        }
      }
    } catch (error) {
      deps.logger?.warn(`saolei hook handler failed: ${errorMessage(error)}`);
    }
  };
}

/**
 * The announcement body for one hook result, or undefined for a result that
 * announces nothing. The text table is owned by
 * specs/078-saolei-js-hooks/contracts/hook-announcement.md §2 — this function is
 * its only implementation. An empty result (the empty string alone) is silent
 * (specs/078-saolei-js-hooks/spec.md FR-008); a truncated result already
 * carries the text cut to `MAX_OUTPUT_CHARS`, so the marker is all that is
 * appended.
 */
function announcementFor(name: string, result: HookRunResult): string | undefined {
  switch (result.kind) {
    case "output":
      return `[hook ${name}] ${result.text}`;
    case "truncated":
      return `[hook ${name}] ${result.text} ...[truncated]`;
    case "error":
      return `[hook ${name}] error: ${result.message}`;
    case "timeout":
      return `[hook ${name}] timeout after ${EXECUTION_TIMEOUT_MS}ms`;
    case "empty":
      return undefined;
  }
}

/** The message of a caught value, assuming (as ECMAScript requires of thrown
 * values it controls) an `Error`; anything else is stringified. */
function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
