/**
 * team/agent-invoke.ts — shared bounded-retry wrapper for the stateless
 * `createAgent` invokes inside the team graph's planner-family nodes (the
 * review node in planner.ts and the init/compact instruction nodes in
 * instruction-node.ts). Both nodes run a checkpointer-less agent to
 * completion and degrade (rather than letting the graph scheduler re-route)
 * when every attempt fails — the retry policy lives inside the node, so the
 * graph scheduler never re-routes the planner (D6).
 */

import type { BaseMessage } from "@langchain/core/messages";
import type { RunnableConfig } from "@langchain/core/runnables";
import { warn } from "@dominion/common-js-logs";

/** Bounded retry count for a failing planner-family agent invoke (D6). */
export const MAX_AGENT_ATTEMPTS = 3;

/** The minimal agent surface the wrapper drives (matches createAgent). */
export interface InvokeAgent {
	invoke(
		input: { messages: BaseMessage[] },
		config?: RunnableConfig,
	): Promise<{ messages: BaseMessage[] }>;
}

/**
 * Run a stateless `createAgent` invoke with a bounded retry (D6 — retry
 * lives inside the node; the graph scheduler never re-routes). Re-throws
 * when all attempts fail so the node can degrade.
 *
 * The outer graph's `config` (recursionLimit / signal) is forwarded to the
 * agent invoke (Issue 4 — `specs/036-team-mode-bugfix/contracts/
 * team-graph-fix-contract.md` §3.2).
 */
export async function invokeAgentWithRetry(
	agent: InvokeAgent,
	input: BaseMessage[],
	config?: RunnableConfig,
): Promise<{ messages: BaseMessage[] }> {
	let lastError: unknown;
	for (let attempt = 1; attempt <= MAX_AGENT_ATTEMPTS; attempt += 1) {
		try {
			return await agent.invoke({ messages: input }, config);
		} catch (err) {
			lastError = err;
			if (attempt < MAX_AGENT_ATTEMPTS) {
				const message = err instanceof Error ? err.message : String(err);
				warn("planner-family invoke failed; retrying", {
					attempt,
					error: message,
				});
			}
		}
	}
	throw lastError;
}
