/**
 * team/update-strategy.ts — the planner-only `update_strategy` tool.
 *
 * Writes the strategy to the long-term `StrategyStore` (mongo-backed in
 * production, D4). Only the planner agent holds this tool (FR-012); the
 * planner has NO read tools — strategy reads happen at code level (contract
 * §2.4 / FR-014). The tool is bound to a session id (the strategy namespace
 * key, FR-013).
 */

import { tool } from "langchain";
import { z } from "zod";
import type { StructuredToolInterface } from "@langchain/core/tools";

import type { StrategyStore } from "../strategy-store";

/**
 * Build the planner-only `update_strategy` tool (contract §2.4).
 *
 * The tool writes `content` to the strategy store for the given session;
 * returns `{ ok: true }` on success. A failing `put` (e.g. mongo hiccup)
 * propagates to the agent loop, which surfaces it to the model as a tool
 * error — the model may then retry the call itself; the planner NODE's
 * retry/degrade wrapper (`planner.ts`) is the last line of defense (D6:
 * `update_strategy` retry is handled inside the planner node, the graph
 * scheduler never re-routes the planner).
 *
 * @param store The long-term strategy store (DI — tests inject the fake).
 * @param sessionId The session id used as the store key (FR-013).
 */
export function buildUpdateStrategyTool(
	store: StrategyStore,
	sessionId: string,
): StructuredToolInterface {
	return tool(
		async ({ content }: { content: string }) => {
			await store.put(sessionId, content);
			return { ok: true };
		},
		{
			name: "update_strategy",
			description:
				"Update the long-term strategy for this session based on the " +
				"reviewed game. Call this only when the review concludes the " +
				"strategy should change; the new strategy replaces the previous " +
				"one entirely.",
			schema: z.object({ content: z.string() }),
		},
	);
}
