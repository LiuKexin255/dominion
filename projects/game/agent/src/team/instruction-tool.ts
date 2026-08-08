/**
 * team/instruction-tool.ts — the planner-only `instruct_player` tool
 * (FR-014/FR-017; `specs/039-planner-memory-calibration/contracts/
 * team-graph-contract.md` §4).
 *
 * The tool delivers a calibration instruction into the player's conversation
 * flow (a `HumanMessage` in `playerMessages` — survey D6 openclaw pattern).
 * Only the planner holds it; the player holds no instruction/memory tools
 * (FR-013/FR-009).
 *
 * **Cross-channel write mechanism (R1 — external buffer staging)**: the tool
 * runs INSIDE the planner's `createAgent` subgraph, so it cannot reliably
 * write the outer team graph's `playerMessages` channel directly. Instead it
 * stages the content into a configurable-provided external buffer — the same
 * `configurable`-based pattern as 037's `emitChannelFrame`
 * (`specs/037-saolei-team-optimize/`; session-team.ts): the enclosing node
 * (review / initInstruction / postCompactInstruction) installs a fresh
 * {@link InstructionBuffer} per turn via `config.configurable.
 * instructionBuffer`, the tool writes `buffer.content`, and the node reads
 * the staged content AFTER `createAgent.invoke` returns, writing the
 * instruction into the outer graph state from its own return value
 * (contract §4: review → `playerMessages`; init/compact → `pendingInstruction`).
 *
 * The tool does NOT write the outer channel itself and does NOT know the
 * scenario — it always returns `{ok: true}`.
 */

import { tool } from "langchain";
import { z } from "zod";
import type { RunnableConfig } from "@langchain/core/runnables";
import type { StructuredToolInterface } from "@langchain/core/tools";

/**
 * The per-turn external staging slot the `instruct_player` tool writes and
 * the enclosing instruction-producing nodes read (R1 — contract §4, same
 * `configurable` staging pattern as 037 `emitChannelFrame`). A fresh
 * instance is installed per turn by the caller (session-team.ts); the tool
 * sets `content`, the node reads it after the agent invoke returns and
 * resets it to `null`.
 */
export interface InstructionBuffer {
	content: string | null;
}

/**
 * Build the planner-only `instruct_player` tool (contract §4).
 *
 * The tool stages `content` into the configurable-provided
 * {@link InstructionBuffer} (R1) and returns `{ok: true}` — it never writes
 * the outer `playerMessages` channel directly; the enclosing node performs
 * the actual channel write from its return value after the agent invoke
 * completes.
 *
 * The tool is scenario-agnostic: whether the instruction lands in
 * `playerMessages` (review, FR-017 — same-turn injection) or in
 * `pendingInstruction` (init/compact, FR-015/FR-016 — deferred injection) is
 * decided by the enclosing node.
 *
 * @returns The `instruct_player` `StructuredToolInterface` (DI — no session
 *   binding; the buffer comes from the runtime config, so the same tool
 *   instance is safe for every session/rebuild).
 */
export function buildInstructPlayerTool(): StructuredToolInterface {
	return tool(
		async (
			{ content }: { content: string },
			// The langchain `tool()` wrapper passes the RunnableConfig as the
			// func's SECOND argument (`func(input, childConfig)` — the
			// documented `(input, runManager?, config?)` shape is not what the
			// current @langchain/core wrapper emits).
			config?: RunnableConfig,
		) => {
			// R1: stage the instruction into the external buffer (configurable-
			// provided, same pattern as 037 `emitChannelFrame`). A missing
			// buffer (tool invoked outside a team turn) degrades to a no-op —
			// the call still reports success, the instruction is simply not
			// delivered.
			const buffer = config?.configurable?.instructionBuffer as
				| InstructionBuffer
				| undefined;
			if (buffer) {
				buffer.content = content;
			}
			return { ok: true };
		},
		{
			name: "instruct_player",
			description:
				"Send a calibration instruction to the player as part of its " +
				"conversation flow (the player reads it like a user message; it " +
				"accumulates in its history and is visible on the player tab). " +
				"Call this only when the review concludes the player needs " +
				"guidance; the instruction replaces nothing — it is appended to " +
				"the player's ongoing conversation.",
			schema: z.object({ content: z.string() }),
		},
	);
}
