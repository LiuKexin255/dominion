/**
 * The hook registry (specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md
 * §2; specs/078-saolei-js-hooks/data-model.md §2.1): the team-materialization
 * process-memory store of planner-authored hook scripts.
 *
 * The registry is the single shared state between the planner's write/read
 * face (`saolei_hook`: set/remove/list) and the player-side event handler
 * (`entriesInOrder()`). It has no persistence and no explicit teardown: one
 * registry lives inside one hooks unit, so a team refresh (UpdateTeam rebuild)
 * or dispose drops it with the unit
 * (specs/078-saolei-js-hooks/spec.md FR-005).
 *
 * A registered script is compiled once here through the sandbox's
 * `compileHookScript` (length cap, banned patterns, syntax) and the compiled
 * `vm.Script` is reused across every execution context
 * (specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md §4).
 */

import type { Script } from "node:vm";

import { MAX_HOOKS } from "./limits.js";
import { compileHookScript } from "./sandbox.js";

/**
 * One registered hook: the name is the identity, the compiled script is reused
 * across executions, and `order` is the registration position this hook keeps
 * for the rest of its life
 * (specs/078-saolei-js-hooks/research.md D3).
 */
export interface HookEntry {
  readonly name: string;
  readonly script: Script;
  readonly source: string;
  readonly order: number;
}

/**
 * The list view of one registered hook
 * (specs/078-saolei-js-hooks/data-model.md §2.1): `length` is the script
 * source's character count.
 */
export interface HookSummary {
  readonly name: string;
  readonly order: number;
  readonly length: number;
}

/**
 * Outcome of a `set`/`remove` write: success, or the plain-text reason the
 * write is refused. Every refusal is a result the caller can hand back to the
 * model — never a thrown error
 * (specs/078-saolei-js-hooks/spec.md FR-003).
 */
export type HookWriteResult = { ok: true } | { ok: false; rejection: string };

/**
 * The hook registry. `set` on an existing name updates that hook in place —
 * the name keeps its position in the registration order — while `set` on a new
 * name appends to the end.
 */
export class HookRegistry {
  /** Keyed by name; a Map keeps insertion order, which a same-name drop-in
   * `set` preserves. */
  private readonly entries = new Map<string, HookEntry>();
  private nextOrder = 0;

  /**
   * Register or update one hook. The script goes through the full
   * registration-time validation chain — the count cap, then the sandbox's
   * length cap, banned patterns and compilation — and a refusal is returned
   * unchanged so the composing tool can surface it verbatim
   * (specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md §3.1 layers
   * 4–6).
   */
  set(name: string, source: string): HookWriteResult {
    if (name.trim().length === 0) {
      return { ok: false, rejection: "hook name must not be empty" };
    }
    const existing = this.entries.get(name);
    if (existing === undefined && this.entries.size >= MAX_HOOKS) {
      return {
        ok: false,
        rejection:
          `at most ${MAX_HOOKS} hooks can be registered` +
          ` (already ${this.entries.size}); remove one before adding another`,
      };
    }
    const compiled = compileHookScript(source);
    if (!compiled.ok) {
      return { ok: false, rejection: compiled.rejection };
    }
    this.entries.set(name, {
      name,
      script: compiled.script,
      source,
      order: existing?.order ?? this.nextOrder++,
    });
    return { ok: true };
  }

  /**
   * Remove one hook by name. An unknown name is refused with the list of the
   * names that ARE registered, so the caller can pick the right one
   * (specs/078-saolei-js-hooks/contracts/saolei-hooks-plugin.md §3.1 layer 7).
   */
  remove(name: string): HookWriteResult {
    if (!this.entries.delete(name)) {
      return {
        ok: false,
        rejection: `no hook named "${name}" is registered; registered hooks: ${this.names()}`,
      };
    }
    return { ok: true };
  }

  /** The registered hooks in registration order, as read-only summaries. */
  list(): readonly HookSummary[] {
    return this.ordered().map((entry) => ({
      name: entry.name,
      order: entry.order,
      length: entry.source.length,
    }));
  }

  /**
   * A fresh copy of the registered hooks in registration order — the event
   * handler's read face, so it never observes a later write and cannot disturb
   * the registry through the returned array
   * (specs/078-saolei-js-hooks/data-model.md §2.1).
   */
  entriesInOrder(): readonly HookEntry[] {
    return this.ordered();
  }

  /** Registration order: ascending `order`, which for this store also matches
   * Map insertion order (a same-name `set` keeps its slot). */
  private ordered(): HookEntry[] {
    return [...this.entries.values()].sort((left, right) => left.order - right.order);
  }

  /** The registered names for refusal texts; `(none)` when the registry is
   * empty. */
  private names(): string {
    const names = this.ordered().map((entry) => entry.name);
    return names.length === 0 ? "(none)" : names.join(", ");
  }
}
