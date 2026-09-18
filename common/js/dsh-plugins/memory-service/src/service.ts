/**
 * PlannerMemoryService — the host service face `ctx.plannerMemory` of the
 * memory plugin. It owns the per-agent binding between the agent and the
 * memory scope `(template, session)`, the frozen snapshot cache, and the
 * storage access shared by the memory tool's write path.
 *
 * - `load(agentCtx, scope)` reads the scope's most recently updated entries
 *   (ONE server-ordered ListMemories page — `SNAPSHOT_ORDER_BY` with
 *   `SNAPSHOT_ENTRY_LIMIT`) through the injected storage client and writes the
 *   rendered snapshot into the cache, keyed by the agent (the
 *   `AssembleContext.scope` the prompt assembly passes to the snapshot
 *   section — `assembleContextFor` sets `scope: agent`,
 *   https://unpkg.com/@deepseek-ai/dsh-agent@0.1.1-rc.2/lib/index.js). The
 *   binding is registered as an effect on `agentCtx`, so it unwinds with the
 *   agent scope. A rejection propagates (fail-loud): the materialization
 *   setup that awaits it rolls the whole member creation back
 *   (specs/064-memory-split-fold-remain/contracts/dsh-plugins.md §2 item 3;
 *   survey/deepseek-harness-memory-plugin.md decision ⑧).
 * - `applyCall(agent, args)` runs a hermes-style call against the same client
 *   with the agent's bound scope, so writes persist immediately and are never
 *   scoped to a stale session. Domain outcomes are text results; only
 *   infrastructure failures reject, and the tool surface maps them to text.
 * - `snapshot(scope)` is the read face the snapshot section evaluates on each
 *   assembly; an unbound or empty snapshot reads as `undefined`/`""`, which
 *   the registry drops from the prompt.
 *
 * Contract: specs/064-memory-split-fold-remain/contracts/dsh-plugins.md §2; scope
 * key per spec/plan decision ⑦ (survey/deepseek-harness-memory-plugin.md §9)
 * and specs/059-agent-v2-team-mode/data-model.md §3 (memory 快照缓存).
 */

import type { Context } from "@deepseek-ai/cordis";
import type { Agent } from "@deepseek-ai/dsh-agent";
import type { ScopeKey } from "@deepseek-ai/dsh-scope";

import type { MemoryStore } from "./client.js";
import { applyMemoryCall } from "./operations.js";
import type { MemoryToolArgs } from "./operations.js";
import {
  renderMemorySnapshot,
  SNAPSHOT_ENTRY_LIMIT,
  SNAPSHOT_ORDER_BY,
} from "./snapshot.js";

/**
 * The memory scope key: the business session whose memories this planner
 * works with (the memory service resource pattern
 * `templates/{template}/sessions/{session}/memories/{memory}`). Structurally
 * identical to the orchestration layer's `PlannerMemoryScope`
 * (common/js/dsh-plugins/saolei-loop/src/orchestrator.ts), so the
 * materialization setup passes the same object without a shared dependency.
 */
export interface PlannerMemoryScope {
  readonly template: string;
  readonly session: string;
}

/** The `ctx.plannerMemory` face. */
export interface PlannerMemoryService {
  /**
   * Prefetch the scope's snapshot and bind it to the agent behind `agentCtx`.
   * Rejects on a storage failure (fail-loud materialization rollback).
   */
  load(agentCtx: Context, scope: PlannerMemoryScope): Promise<void>;
  /**
   * Execute one memory call for `agent` against its bound scope. Domain
   * outcomes return text; infrastructure failures reject.
   */
  applyCall(agent: Agent, args: MemoryToolArgs): Promise<string>;
  /** The snapshot text bound to a scope key (agent), or undefined. */
  snapshot(scope: ScopeKey | undefined): string | undefined;
}

declare module "@deepseek-ai/cordis" {
  interface Context {
    plannerMemory: PlannerMemoryService;
  }
}

/** The collaborators of {@link createPlannerMemory} (DI seam for tests). */
export interface PlannerMemoryDeps {
  /** Storage access (production: a {@link import("./client.js").MemoryClient}). */
  readonly client: MemoryStore;
}

/**
 * Assemble the planner-memory service over an injected storage client. The
 * snapshot cache and scope bindings are per-service-instance state, so two
 * agents never share a snapshot (per-agent isolation is the plugin's
 * responsibility under the preset standing mount —
 * survey/deepseek-harness-memory-plugin.md §2.4/risk 3).
 */
export function createPlannerMemory(
  deps: PlannerMemoryDeps,
): PlannerMemoryService {
  const { client } = deps;
  const snapshots = new Map<ScopeKey, string>();
  const scopes = new Map<ScopeKey, PlannerMemoryScope>();

  const scopeKeyOf = (agentCtx: Context): ScopeKey => {
    const agent = agentCtx.agent;
    if (agent === undefined) {
      throw new Error(
        "planner memory: load requires an agent-scoped context (agentCtx.agent is unset)",
      );
    }
    return agent;
  };

  return {
    async load(agentCtx: Context, scope: PlannerMemoryScope): Promise<void> {
      const key = scopeKeyOf(agentCtx);
      // The read is the fail-loud point: a rejection here must leave no
      // partial binding behind, so the maps are written only after it
      // resolves (memory-plugin decision ⑧). One server-ordered page holds
      // the most recently updated entries — sorting/truncation happen in the
      // memory service, not here
      // (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md
      // §2).
      const entries = await client.listMemories(scope.template, scope.session, {
        orderBy: SNAPSHOT_ORDER_BY,
        pageSize: SNAPSHOT_ENTRY_LIMIT,
      });
      scopes.set(key, scope);
      snapshots.set(key, renderMemorySnapshot(entries));
      agentCtx.effect(() => () => {
        scopes.delete(key);
        snapshots.delete(key);
      }, "planner-memory.scope()");
    },

    async applyCall(agent: Agent, args: MemoryToolArgs): Promise<string> {
      const scope = scopes.get(agent);
      if (scope === undefined) {
        throw new Error(
          `planner memory: agent "${agent.id}" has no loaded memory scope (the materialization setup must await load() first)`,
        );
      }
      return applyMemoryCall(client, scope.template, scope.session, args);
    },

    snapshot(scope: ScopeKey | undefined): string | undefined {
      return scope === undefined ? undefined : snapshots.get(scope);
    },
  };
}
