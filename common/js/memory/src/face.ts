/**
 * The capability primitives: bind one storage domain's scope into a
 * per-member face and render the frozen memory snapshot. No I/O and no context
 * side effects happen at construction — the orchestration drives the one I/O
 * (the fail-loud memory prefetch).
 *
 * There is one face per functional unit: the memory face binds
 * `(template, preset)` and the wiki face binds `template` alone — a wiki
 * collection is shared by every preset of a template
 * (specs/069-wiki-template-web/contracts/wiki-service.md §1), so a face that
 * carried the other half of the scope would claim a binding the wiki store
 * does not have. Each face serves exactly its own unit's tools, which is what
 * lets either unit be mounted on its own
 * (specs/076-agent-v2-optimize/contracts/memory-library-layering.md §1 义务 1).
 *
 * The memory scope travels with the face and needs no agent-keyed lookup. A
 * rejection from {@link MemoryFace.prefetch} propagates (fail-loud): the
 * orchestration that awaits it rolls the whole member creation back
 * (specs/074-dsh-grpc-runtime/contracts/memory-library.md §1;
 * specs/064-memory-split-fold-remain/contracts/dsh-plugins.md §2 item 3;
 * survey/deepseek-harness-memory-plugin.md decision ⑧).
 *
 * `applyCall` runs a hermes-style call against the bound scope's memory
 * collection. Domain outcomes are text results; only infrastructure failures
 * reject, and the tool surface maps them to text.
 */

import { applyMemoryCall } from "./operations.js";
import type { MemoryToolArgs } from "./operations.js";
import {
  renderMemorySnapshot,
  SNAPSHOT_ENTRY_LIMIT,
  SNAPSHOT_ORDER_BY,
} from "./snapshot.js";
import type { MemoryStoreRpcs, WikiStore } from "./store.js";
import { wikiGet, wikiSearch, wikiWrite } from "./wiki-operations.js";
import type { WikiWriteArgs } from "./wiki-operations.js";

/**
 * The memory scope: the business template and the materialization source
 * preset whose memories this member works with (the memory service resource
 * pattern `templates/{template}/presets/{preset}/memories/{memory}`).
 * Structurally compatible with the orchestration layer's scope type
 * (common/js/dsh-plugins/saolei-loop/src/orchestrator.ts), so the
 * materialization setup passes the same object without a shared dependency
 * (specs/071-memory-preset-scope/contracts/dsh-plugins.md §1).
 */
export interface MemoryScope {
  readonly template: string;
  readonly preset: string;
}

/**
 * The per-member memory face. The scope is bound at creation; the frozen
 * snapshot is filled by {@link MemoryFace.prefetch} before the first prompt
 * assembly and never refreshed by writes
 * (specs/071-memory-preset-scope/spec.md FR-005;
 * specs/064-memory-split-fold-remain/contracts/dsh-plugins.md §2).
 */
export interface MemoryFace {
  /**
   * Prefetch the scope's snapshot and freeze the rendered text. Rejects on a
   * storage failure (fail-loud materialization rollback).
   */
  prefetch(): Promise<void>;
  /**
   * Execute one memory call against the bound scope. Domain outcomes return
   * text; infrastructure failures reject.
   */
  applyCall(args: MemoryToolArgs): Promise<string>;
  /** The frozen snapshot text (the empty string before/without a prefetch). */
  snapshotText(): string;
}

/**
 * The per-member wiki face. The template is bound at creation (the wiki
 * collection's single scope half); domain outcomes return text and only
 * infrastructure failures reject, exactly like the memory face's calls.
 */
export interface WikiFace {
  /**
   * Search the template's wiki titles for `query` (all matching titles, no
   * content). Domain outcomes return text; infrastructure failures reject.
   */
  wikiSearch(query: string): Promise<string>;
  /**
   * Retrieve the template's wiki entries by exact title (content included).
   * Domain outcomes return text; infrastructure failures reject.
   */
  wikiGet(titles: readonly string[]): Promise<string>;
  /**
   * Upsert the template's wiki entries by title (single XOR batch form; the
   * batch is validated in a preflight and committed entry by entry). Domain
   * outcomes return text; infrastructure failures reject.
   */
  wikiWrite(input: WikiWriteArgs): Promise<string>;
}

/**
 * Bind a memory scope into a per-member face over the memory store. Each call
 * carries the bound scope, so no agent-keyed resolution is needed.
 */
export function createMemoryFace(
  store: MemoryStoreRpcs,
  scope: MemoryScope,
): MemoryFace {
  let snapshot = "";
  return {
    async prefetch(): Promise<void> {
      // The read is the fail-loud point: a rejection must leave no partial
      // snapshot behind, so the cache is written only after it resolves
      // (memory-plugin decision ⑧). One server-ordered page holds the most
      // recently updated entries — sorting/truncation happen in the memory
      // service, not here
      // (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md
      // §2).
      const entries = await store.listMemories(scope.template, scope.preset, {
        orderBy: SNAPSHOT_ORDER_BY,
        pageSize: SNAPSHOT_ENTRY_LIMIT,
      });
      snapshot = renderMemorySnapshot(entries);
    },

    applyCall(args: MemoryToolArgs): Promise<string> {
      return applyMemoryCall(store, scope.template, scope.preset, args);
    },

    snapshotText(): string {
      return snapshot;
    },
  };
}

/**
 * Bind a wiki template into a per-member face over the wiki store. Every
 * operation addresses that template's collection alone.
 */
export function createWikiFace(store: WikiStore, template: string): WikiFace {
  return {
    wikiSearch(query: string): Promise<string> {
      return wikiSearch(store, template, query);
    },

    wikiGet(titles: readonly string[]): Promise<string> {
      // The operation core accepts a readonly titles array, so the caller's
      // array is passed through without a defensive copy.
      return wikiGet(store, template, titles);
    },

    wikiWrite(input: WikiWriteArgs): Promise<string> {
      return wikiWrite(store, template, input);
    },
  };
}
