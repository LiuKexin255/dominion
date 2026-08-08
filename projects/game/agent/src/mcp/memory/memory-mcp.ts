/**
 * memory-mcp.ts — Session-bound memory MCP server for the planner (spec 039
 * T015).
 *
 * Exposes ONE hermes-style `memory` tool (`action`/`content`/`old_text`/
 * `operations` — no `memory_id`, no `target`, FR-008) to the planner. The
 * tool is converted agent-side into the MemoryService's memory_id-based
 * RPCs via `memory-client.ts`; the mcp server NEVER connects to the memory
 * service directly (FR-007 — the agent forwards).
 *
 * Contract: `specs/039-planner-memory-calibration/contracts/memory-mcp-contract.md`
 * §1-2. Old_text substring matching semantics follow hermes
 * `tools/memory_tool.py` (https://github.com/NousResearch/hermes-agent/blob/main/tools/memory_tool.py):
 * substring containment, case-sensitive; 0 hits → error text + the current
 * entries; multiple DISTINCT hits → error text + hit previews; all-identical
 * hits → act on the first (dedupe).
 *
 * Errors are returned as TEXT results (never thrown — 031 C15 neutral
 * status), so the LLM can react (pick a more specific old_text, retry after
 * a service outage). Mutating storage does NOT refresh the frozen snapshot
 * (FR-010 — the snapshot refreshes at the compression boundary only).
 *
 * DI seam (`style/javascript.md` §测试): `memoryClient` is injected; tests
 * pass a fake with NO `vi.mock`.
 */

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { createHash } from "node:crypto";
import { z } from "zod";

import type { MemoryClient, MemoryEntry } from "../../memory-client";

/** The hermes memory tool's actions (contract §1). */
export const MEMORY_ACTIONS = ["add", "replace", "remove"] as const;
export type MemoryAction = (typeof MEMORY_ACTIONS)[number];

/** One batch operation (contract §1 — `operations` array item). */
export interface MemoryOp {
	action: MemoryAction;
	content?: string;
	old_text?: string;
}

/** Single MCP text content block (same shape as saolei-mcp's textResult). */
type MemoryTextBlock = { type: "text"; text: string };

/** Build a single-block MCP result from a body string. */
function textResult(text: string): { content: MemoryTextBlock[] } {
	return { content: [{ type: "text", text }] };
}

function describeErr(err: unknown): string {
	return err instanceof Error ? err.message : String(err);
}

/** Truncated one-line preview (hermes `_previews`, width 80). */
function preview(content: string, width = 80): string {
	return content.length > width ? content.slice(0, width) + "..." : content;
}

/** Render the current entries for error feedback (helps the LLM re-pick). */
function renderEntries(entries: MemoryEntry[]): string {
	if (entries.length === 0) {
		return "(no entries)";
	}
	return entries.map((e) => `- ${e.content}`).join("\n");
}

/**
 * Generate the service-internal memory_id for an `add` (FR-008): a
 * deterministic sha256 hex digest of the content.
 *
 * - Satisfies the memory service's `[a-z0-9_-]+` memory_id charset
 *   (Phase 3 T011 — `projects/game/memory/handler/handler.go`).
 * - Deterministic: identical content ⇒ identical id, which keeps the add
 *   dedupe (equivalent content already present ⇒ success) consistent even
 *   across a CreateMemory ALREADY_EXISTS race — `applyAdd` maps that
 *   conflict (code 6) to the dedupe success text (hermes "no duplicate
 *   added"), since the digest guarantees the conflict is for the same
 *   content.
 */
export function generateMemoryId(content: string): string {
	return createHash("sha256").update(content).digest("hex").slice(0, 32);
}

/**
 * Locate the single entry whose content contains `oldText` as a substring
 * (hermes semantics, contract §1.1 — substring containment, case-sensitive).
 *
 * @param entries The current entries (from `listMemories`).
 * @param oldText The (possibly empty/whitespace) locator substring.
 * @returns The matched entry on success, or an error text on failure:
 *   - empty old_text → error + the current entries (hermes
 *     `_missing_old_text_error` equivalent);
 *   - 0 hits → error + the current FULL entry list (LLM re-picks a more
 *     specific substring);
 *   - multiple DISTINCT entries hit → error + hit previews ("be more
 *     specific");
 *   - multiple entries with IDENTICAL content hit → the first (dedupe).
 */
export function matchBySubstring(
	entries: MemoryEntry[],
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

/** Result texts (contract §2-style short bodies; hermes wording where close). */
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
 * Apply a single `add` (contract §1): generate the internal memory_id and
 * CreateMemory; an equivalent existing content is a SUCCESS, not an error
 * (dedupe — hermes "no duplicate added").
 */
async function applyAdd(
	memoryClient: MemoryClient,
	template: string,
	session: string,
	content: string,
): Promise<string> {
	const text = content.trim();
	if (!text) {
		return EMPTY_CONTENT_TEXT;
	}
	const entries = await memoryClient.listMemories(template, session);
	if (entries.some((e) => e.content === text)) {
		return DEDUPE_TEXT;
	}
	try {
		await memoryClient.createMemory(
			template,
			session,
			generateMemoryId(text),
			text,
		);
	} catch (err) {
		// ALREADY_EXISTS (code 6, AIP-193): a concurrent writer created an
		// equivalent entry between the dedupe check above and this RPC.
		// Because the id is a deterministic content digest, the conflict can
		// only be for the SAME content — treat it as the dedupe SUCCESS
		// (hermes "no duplicate added"), matching the pre-check semantics.
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
	memoryClient: MemoryClient,
	template: string,
	session: string,
	oldText: string,
	content: string,
): Promise<string> {
	const text = content.trim();
	if (!text) {
		return EMPTY_CONTENT_TEXT + " (use action=remove to delete entries)";
	}
	const entries = await memoryClient.listMemories(template, session);
	const match = matchBySubstring(entries, oldText);
	if ("error" in match) {
		return match.error;
	}
	await memoryClient.updateMemory(
		template,
		session,
		match.entry.memory_id,
		text,
	);
	return REPLACED_TEXT;
}

/** Apply a single `remove`: locate the entry by old_text substring, then DeleteMemory. */
async function applyRemove(
	memoryClient: MemoryClient,
	template: string,
	session: string,
	oldText: string,
): Promise<string> {
	const entries = await memoryClient.listMemories(template, session);
	const match = matchBySubstring(entries, oldText);
	if ("error" in match) {
		return match.error;
	}
	await memoryClient.deleteMemory(template, session, match.entry.memory_id);
	return REMOVED_TEXT;
}

/**
 * Apply a batch of operations (contract §1 — "operations 批量原子，全成功
 * 才提交"; v1 可选，单 op 已满足核心).
 *
 * **v1 atomicity scope**: atomicity holds for every DOMAIN-level failure —
 * the preflight phase (below) validates ALL ops against ONE snapshot of the
 * current entries (working copy, hermes `apply_batch` semantics) and any
 * failing op aborts the whole batch with an error + the current entries,
 * with ZERO writes having happened. Only when all ops preflight cleanly
 * does the commit phase execute the RPCs, in order. True all-or-nothing
 * across the commit phase is NOT achievable over plain gRPC (no service
 * transaction API): an infrastructure failure mid-commit (e.g. the memory
 * service becoming unavailable) may leave EARLIER writes already applied —
 * v1 does NOT best-effort-roll them back (that would require compensating
 * deletes/updates of unknown success). The tool surfaces such a failure as
 * `memory failed: ...` text (never a thrown tool error).
 */
async function applyBatch(
	memoryClient: MemoryClient,
	template: string,
	session: string,
	operations: MemoryOp[],
): Promise<string> {
	if (operations.length === 0) {
		return EMPTY_BATCH_TEXT;
	}
	const entries = await memoryClient.listMemories(template, session);
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
			// Idempotent dedupe: an identical entry already in the batch's
			// view is skipped, not failed (hermes apply_batch).
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
			working[working.findIndex((e) => e.memory_id === match.entry.memory_id)] =
				{ ...match.entry, content };
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

	// Commit — all preflight checks passed. Executed in order; an
	// infrastructure failure mid-commit propagates to the tool handler
	// (`memory failed: ...` text) and MAY leave earlier writes applied —
	// v1 does not roll back (see the applyBatch doc comment).
	for (const write of writes) {
		if (write.kind === "create") {
			await memoryClient.createMemory(template, session, write.id, write.content);
		} else if (write.kind === "update") {
			await memoryClient.updateMemory(template, session, write.id, write.content);
		} else {
			await memoryClient.deleteMemory(template, session, write.id);
		}
	}
	return `memory: applied ${writes.length} operation(s)`;
}

/** Arguments accepted by the `memory` tool (contract §1). */
export interface MemoryToolArgs {
	action?: MemoryAction;
	content?: string;
	old_text?: string;
	operations?: MemoryOp[];
}

/**
 * The agent-side conversion core (exported for direct unit testing): turn a
 * hermes-style `memory` call into MemoryService RPCs (contract §1) and
 * return the text result. Never throws for domain-level outcomes — only
 * infrastructure failures (service unavailable) propagate to the caller,
 * which converts them into `memory failed: ...` text.
 */
export async function applyMemoryCall(
	memoryClient: MemoryClient,
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
		return applyBatch(memoryClient, template, session, args.operations as MemoryOp[]);
	}
	if (args.action === "add") {
		return applyAdd(memoryClient, template, session, args.content ?? "");
	}
	if (args.action === "replace") {
		return applyReplace(
			memoryClient,
			template,
			session,
			args.old_text ?? "",
			args.content ?? "",
		);
	}
	if (args.action === "remove") {
		return applyRemove(memoryClient, template, session, args.old_text ?? "");
	}
	return INVALID_CALL_TEXT;
}

/**
 * Create the session-bound memory `McpServer` for the planner (contract §2).
 *
 * `template`/`session` are closed over (FR-012): the tool's arguments carry
 * NEITHER template nor session — the closure builds the Memory resource
 * names. The mcp host creates one server per session (same per-session
 * lifecycle as the saolei mcp).
 *
 * @param memoryClient The MemoryService gRPC client (DI seam — the mcp
 *   server forwards via the agent and NEVER connects to the memory service
 *   itself, FR-007).
 * @param template The template path segment (e.g. `"saolei"`).
 * @param session The session id path segment.
 * @returns The planner-bound `McpServer` exposing exactly ONE `memory` tool.
 */
export function createMemoryMcpServer(
	memoryClient: MemoryClient,
	template: string,
	session: string,
): McpServer {
	const server = new McpServer(
		{ name: "memory", version: "0.1.0" },
		{ capabilities: { tools: {} } },
	);

	server.registerTool(
		"memory",
		{
			description:
				"Manage the planner's long-term review memory (hermes-style " +
				"single tool). action=add records a new entry (content); " +
				"action=replace/remove locate an existing entry by a SHORT " +
				"old_text substring and update (content) or delete it; " +
				"operations applies a batch atomically (all-or-nothing). " +
				"Substring matching is case-sensitive; a 0/multiple match " +
				"returns the current entries to pick a more specific " +
				"old_text. Changes persist immediately but the frozen " +
				"snapshot refreshes only at the compression boundary. " +
				"See the memory skill for when to record and what to skip.",
			inputSchema: {
				action: z
					.enum(MEMORY_ACTIONS)
					.optional()
					.describe(
						"single-operation form: add/replace/remove (mutually exclusive with operations)",
					),
				content: z
					.string()
					.optional()
					.describe("entry body (add/replace)"),
				old_text: z
					.string()
					.optional()
					.describe(
						"short substring locating an existing entry (replace/remove)",
					),
				operations: z
					.array(
						z.object({
							action: z.enum(MEMORY_ACTIONS),
							content: z.string().optional(),
							old_text: z.string().optional(),
						}),
					)
					.optional()
					.describe(
						"batch form: ordered memory operations (mutually exclusive with action)",
					),
			},
		},
		async (args: MemoryToolArgs) => {
			try {
				return textResult(
					await applyMemoryCall(memoryClient, template, session, args),
				);
			} catch (err) {
				// Errors are TEXT results, never tool exceptions (031 C15
				// neutral status) — the LLM decides how to react (contract §5).
				return textResult(`memory failed: ${describeErr(err)}`);
			}
		},
	);

	return server;
}
