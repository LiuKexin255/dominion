/**
 * Memory tool operations — the hermes-style conversion core behind the
 * `memory` tool (spec 039 T015). The schema/result semantics:
 *
 * - ONE tool with `action` (add/replace/remove) / `content` / `old_text` in
 *   the single-operation form XOR an ordered `operations[]` batch form
 *   (mutually exclusive, refused with a text result);
 * - `old_text` locates an entry by case-sensitive substring containment
 *   (hermes `tools/memory_tool.py`): 0 hits → error text + the current
 *   entries; multiple DISTINCT hits → error text + hit previews; multiple
 *   identical hits → the first (dedupe);
 * - batch atomicity: the preflight validates EVERY op against one snapshot
 *   of the current entries (working copy) and a failing op aborts the whole
 *   batch with ZERO writes; only when all ops pass does the commit phase run
 *   the RPCs in order (v1 documented partial application on an infrastructure
 *   failure mid-commit — no service transaction API exists);
 * - errors are TEXT results, never thrown, so the model can re-pick a more
 *   specific `old_text` or retry after an outage; only infrastructure
 *   failures propagate to the caller, which maps them to `memory failed: …`.
 *
 * Contract: specs/064-memory-split-fold-remain/contracts/dsh-plugins.md §2,
 * specs/059-agent-v2-team-mode/spec.md FR-007.
 */

import { createHash } from "node:crypto";

import type { MemoryEntry, MemoryStore } from "./client.js";

/** The hermes memory tool's actions. */
export const MEMORY_ACTIONS = ["add", "replace", "remove"] as const;
export type MemoryAction = (typeof MEMORY_ACTIONS)[number];

/** One batch operation (`operations` array item). */
export interface MemoryOp {
  action: MemoryAction;
  content?: string;
  old_text?: string;
}

/** Arguments accepted by the `memory` tool. */
export interface MemoryToolArgs {
  action?: MemoryAction;
  content?: string;
  old_text?: string;
  operations?: MemoryOp[];
}

/** Truncated one-line preview (hermes `_previews`, width 80). */
function preview(content: string, width = 80): string {
  return content.length > width ? content.slice(0, width) + "..." : content;
}

/** Render the current entries for error feedback (helps the LLM re-pick). */
function renderEntries(entries: readonly MemoryEntry[]): string {
  if (entries.length === 0) {
    return "(no entries)";
  }
  return entries.map((e) => `- ${e.content}`).join("\n");
}

/**
 * Generate the service-internal memory_id for an `add`: a deterministic
 * sha256 hex digest of the content.
 *
 * - Satisfies the memory service's `[a-z0-9_-]+` memory_id charset.
 * - Deterministic: identical content ⇒ identical id, which keeps the add
 *   dedupe (equivalent content already present ⇒ success) consistent even
 *   across a CreateMemory ALREADY_EXISTS race — `applyAdd` maps that
 *   conflict (code 6) to the dedupe success text, since the digest guarantees
 *   the conflict is for the same content.
 */
export function generateMemoryId(content: string): string {
  return createHash("sha256").update(content).digest("hex").slice(0, 32);
}

/**
 * Locate the single entry whose content contains `oldText` as a substring
 * (hermes semantics — substring containment, case-sensitive).
 *
 * @param entries The current entries (from `listMemories`).
 * @param oldText The (possibly empty/whitespace) locator substring.
 * @returns The matched entry on success, or an error text on failure:
 *   - empty old_text → error + the current entries;
 *   - 0 hits → error + the current FULL entry list (re-pick a more specific
 *     substring);
 *   - multiple DISTINCT entries hit → error + hit previews ("be more
 *     specific");
 *   - multiple entries with IDENTICAL content hit → the first (dedupe).
 */
export function matchBySubstring(
  entries: readonly MemoryEntry[],
  oldText: string,
): { entry: MemoryEntry } | { error: string } {
  const old = oldText.trim();
  if (!old) {
    return {
      error:
        "memory: old_text cannot be empty. Check the current entries " +
        `and retry:\n${renderEntries(entries)}`,
    };
  }
  const hits = entries.filter((e) => e.content.includes(old));
  if (hits.length === 0) {
    return {
      error:
        `memory: no entry matched '${old}'. Check the current entries ` +
        "and retry with a more specific old_text:\n" +
        renderEntries(entries),
    };
  }
  if (hits.length > 1 && new Set(hits.map((e) => e.content)).size > 1) {
    return {
      error:
        `memory: multiple entries matched '${old}'. Be more specific ` +
        "(matched previews):\n" +
        hits.map((e) => `- ${preview(e.content)}`).join("\n"),
    };
  }
  // 0 distinct-content matches among hits (all identical) → first.
  return { entry: hits[0] };
}

/** Result texts (short bodies; hermes wording where close). */
const ADDED_TEXT = "memory added";
const REPLACED_TEXT = "memory replaced";
const REMOVED_TEXT = "memory removed";
const DEDUPE_TEXT = "memory already exists (no duplicate added)";
const INVALID_CALL_TEXT =
  "memory: invalid call. Use action=add/replace/remove with content/" +
  "old_text, or operations (batch).";
const AMBIGUOUS_ARGS_TEXT =
  "memory: provide EITHER action/content/old_text (single operation) " +
  "OR operations (batch), not both.";
const MISSING_ARGS_TEXT =
  "memory: provide EITHER action/content/old_text (single operation) " +
  "OR operations (batch).";
const EMPTY_CONTENT_TEXT = "memory: content cannot be empty";
const EMPTY_BATCH_TEXT = "memory: operations list is empty";

/**
 * Apply a single `add`: generate the internal memory_id and CreateMemory; an
 * equivalent existing content is a SUCCESS, not an error (dedupe — hermes
 * "no duplicate added").
 */
async function applyAdd(
  store: MemoryStore,
  template: string,
  session: string,
  content: string,
): Promise<string> {
  const text = content.trim();
  if (!text) {
    return EMPTY_CONTENT_TEXT;
  }
  const entries = await store.listMemories(template, session);
  if (entries.some((e) => e.content === text)) {
    return DEDUPE_TEXT;
  }
  try {
    await store.createMemory(template, session, generateMemoryId(text), text);
  } catch (err) {
    // ALREADY_EXISTS (code 6, AIP-193): a concurrent writer created an
    // equivalent entry between the dedupe check above and this RPC. Because
    // the id is a deterministic content digest, the conflict can only be for
    // the SAME content — treat it as the dedupe SUCCESS.
    if ((err as { code?: number })?.code === 6) {
      return DEDUPE_TEXT;
    }
    throw err;
  }
  return ADDED_TEXT;
}

/**
 * Apply a single `replace`: locate the entry by old_text substring, then
 * UpdateMemory with the new content.
 */
async function applyReplace(
  store: MemoryStore,
  template: string,
  session: string,
  oldText: string,
  content: string,
): Promise<string> {
  const text = content.trim();
  if (!text) {
    return EMPTY_CONTENT_TEXT + " (use action=remove to delete entries)";
  }
  const entries = await store.listMemories(template, session);
  const match = matchBySubstring(entries, oldText);
  if ("error" in match) {
    return match.error;
  }
  await store.updateMemory(template, session, match.entry.memory_id, text);
  return REPLACED_TEXT;
}

/** Apply a single `remove`: locate the entry by old_text substring, then DeleteMemory. */
async function applyRemove(
  store: MemoryStore,
  template: string,
  session: string,
  oldText: string,
): Promise<string> {
  const entries = await store.listMemories(template, session);
  const match = matchBySubstring(entries, oldText);
  if ("error" in match) {
    return match.error;
  }
  await store.deleteMemory(template, session, match.entry.memory_id);
  return REMOVED_TEXT;
}

/**
 * Apply a batch of operations: the preflight validates ALL ops against ONE
 * snapshot of the current entries (working copy, hermes `apply_batch`
 * semantics); any failing op aborts the whole batch with an error + the
 * current entries, with ZERO writes. Only when all ops preflight cleanly does
 * the commit phase execute the RPCs, in order. True all-or-nothing across
 * the commit phase is not achievable over plain gRPC (no service transaction
 * API): an infrastructure failure mid-commit may leave earlier writes
 * applied — such a failure propagates to the tool handler, which surfaces it
 * as `memory failed: …` text.
 */
async function applyBatch(
  store: MemoryStore,
  template: string,
  session: string,
  operations: readonly MemoryOp[],
): Promise<string> {
  if (operations.length === 0) {
    return EMPTY_BATCH_TEXT;
  }
  const entries = await store.listMemories(template, session);
  const working: MemoryEntry[] = entries.map((e) => ({ ...e }));

  const batchError = (pos: string, reason: string): string =>
    `memory: ${pos} failed: ${reason}. No operations were applied ` +
    "(batch is all-or-nothing). Current entries:\n" +
    renderEntries(entries);

  // Preflight against the working copy. Resolved writes are queued; the
  // working copy is mutated in lockstep so later ops see earlier ops'
  // effects (e.g. a remove then an add of a different entry).
  const writes: Array<
    | { kind: "create"; id: string; content: string }
    | { kind: "update"; id: string; content: string }
    | { kind: "delete"; id: string }
  > = [];
  for (let i = 0; i < operations.length; i += 1) {
    const op = operations[i];
    const pos = `operation ${i + 1}`;
    if (op.action === "add") {
      const content = op.content?.trim() ?? "";
      if (!content) {
        return batchError(pos, "content is required");
      }
      // Idempotent dedupe: an identical entry already in the batch's view is
      // skipped, not failed (hermes apply_batch).
      if (working.some((e) => e.content === content)) {
        continue;
      }
      const id = generateMemoryId(content);
      working.push({ memory_id: id, content });
      writes.push({ kind: "create", id, content });
    } else if (op.action === "replace") {
      const oldText = op.old_text ?? "";
      const content = op.content?.trim() ?? "";
      if (!oldText.trim()) {
        return batchError(pos, "old_text is required");
      }
      if (!content) {
        return batchError(
          pos,
          "content is required (use action=remove to delete)",
        );
      }
      const match = matchBySubstring(working, oldText);
      if ("error" in match) {
        return batchError(pos, match.error);
      }
      const index = working.findIndex(
        (e) => e.memory_id === match.entry.memory_id,
      );
      working[index] = { ...match.entry, content };
      writes.push({ kind: "update", id: match.entry.memory_id, content });
    } else if (op.action === "remove") {
      const oldText = op.old_text ?? "";
      if (!oldText.trim()) {
        return batchError(pos, "old_text is required");
      }
      const match = matchBySubstring(working, oldText);
      if ("error" in match) {
        return batchError(pos, match.error);
      }
      working.splice(
        working.findIndex((e) => e.memory_id === match.entry.memory_id),
        1,
      );
      writes.push({ kind: "delete", id: match.entry.memory_id });
    } else {
      return batchError(
        pos,
        `unknown action '${String((op as { action?: unknown }).action)}' ` +
          "(use add, replace, or remove)",
      );
    }
  }

  // Commit — all preflight checks passed, executed in order.
  for (const write of writes) {
    if (write.kind === "create") {
      await store.createMemory(template, session, write.id, write.content);
    } else if (write.kind === "update") {
      await store.updateMemory(template, session, write.id, write.content);
    } else {
      await store.deleteMemory(template, session, write.id);
    }
  }
  return `memory: applied ${writes.length} operation(s)`;
}

/**
 * Turn a hermes-style `memory` call into MemoryService RPCs and return the
 * text result. Never throws for domain-level outcomes — only infrastructure
 * failures (service unavailable) propagate to the caller, which maps them to
 * `memory failed: …` text.
 */
export async function applyMemoryCall(
  store: MemoryStore,
  template: string,
  session: string,
  args: MemoryToolArgs,
): Promise<string> {
  const hasSingle = args.action != null;
  const hasBatch = args.operations != null;
  if (hasSingle && hasBatch) {
    return AMBIGUOUS_ARGS_TEXT;
  }
  if (!hasSingle && !hasBatch) {
    return MISSING_ARGS_TEXT;
  }
  if (hasBatch) {
    return applyBatch(
      store,
      template,
      session,
      args.operations as MemoryOp[],
    );
  }
  if (args.action === "add") {
    return applyAdd(store, template, session, args.content ?? "");
  }
  if (args.action === "replace") {
    return applyReplace(
      store,
      template,
      session,
      args.old_text ?? "",
      args.content ?? "",
    );
  }
  if (args.action === "remove") {
    return applyRemove(store, template, session, args.old_text ?? "");
  }
  return INVALID_CALL_TEXT;
}
