/**
 * context-middleware.ts — RefreshTeam short-term memory clearing (FR-018).
 *
 * `RefreshTeam` clears BOTH per-agent short-term message channels
 * (`playerMessages` and `plannerMessages`) while leaving the long-term
 * memory (memory service / frozen snapshot) and the `gameEnded` control
 * field intact (specs/031-team-template-mode/contracts/team-graph-contract.md
 * §5; research.md D8). It also resets the `gameCounter` state field to 0 —
 * the counter shares the short-term lifetime (per-session, reset with the
 * team) and RefreshTeam must clear it alongside the channels
 * (specs/037-saolei-team-optimize/spec.md FR-014; data-model.md §2).
 *
 * 039 US3 (T029 — contract §7): init/compact instructions live IN the
 * `playerMessages` channel (written directly by the instruction nodes — no
 * separate slot), so clearing the channel clears them too — a stale
 * instruction cannot survive a RefreshTeam. The frozen memory snapshot is
 * untouched (its data lives in the memory service; the next compression
 * boundary naturally re-bakes it).
 *
 * **Mechanism note (deviation from the contract's "beforeModel hook"
 * wording)**: the contract text says the clear lands in a `beforeModel`
 * middleware hook returning `{ messages: [RemoveMessage(REMOVE_ALL_MESSAGES)] }`.
 * That is NOT possible in the Batch 1 architecture: the player/planner
 * `createAgent`s carry NO checkpointer (spike D14 A2), so their middleware
 * sees only the createAgent's OWN `{ messages }` channel — it cannot reach
 * the outer graph's `playerMessages`/`plannerMessages` channels, which live
 * in the single outer `MemorySaver` (D14 A3). The architecturally correct
 * landing point is `graph.updateState(..., values)` on the OUTER graph: the
 * update flows through each channel's `messagesStateReducer`, so per-channel
 * `RemoveMessage({ id: REMOVE_ALL_MESSAGES })` clears that channel
 * independently (spike A1). This module provides the `clearChannel` helper
 * (ported from `experimental/ts/team_graph_spike/src/team-graph.ts`) and the
 * `refreshTeamChannels` entry point `SessionTeam`/handler use.
 */

import { REMOVE_ALL_MESSAGES } from "@langchain/langgraph";
import { RemoveMessage } from "@langchain/core/messages";
import type { BaseMessage } from "@langchain/core/messages";

import { info } from "@dominion/common-js-logs";

import type { TeamChannel } from "./team/state.js";
import type { TeamGraphHandle } from "./team/graph.js";

/**
 * Build a state update that clears ONE `MessagesValue` channel via the
 * `REMOVE_ALL_MESSAGES` sentinel (research.md D8; spike A1). Per-channel
 * independence is structural: each channel has its own `messagesStateReducer`
 * instance, so clearing `playerMessages` leaves `plannerMessages` intact.
 *
 * The return type is a `Partial` record: the update carries exactly the one
 * requested channel (spread-merged by {@link refreshTeamChannels}).
 */
export function clearChannel(
  channel: TeamChannel,
): Partial<Record<TeamChannel, BaseMessage[]>> {
  return {
    [channel]: [new RemoveMessage({ id: REMOVE_ALL_MESSAGES })],
  };
}

/**
 * Clear the session's short-term memory: BOTH per-agent channels in ONE
 * `updateState` on the outer graph (FR-018). The update is applied through
 * the channels' reducers (checkpointer semantics — values passed to
 * `updateState` flow through the reducers), so each `RemoveMessage` with the
 * `REMOVE_ALL_MESSAGES` sentinel drops all prior messages of its channel.
 *
 * Out of scope by design: the long-term memory (memory service / frozen
 * snapshot) and the `gameEnded` control field — neither is touched (FR-018;
 * D8). `gameEnded` is cleared by the planner node's normal lifecycle (D6
 * step 6), and short-term memory is cleared ONLY by `RefreshTeam` (需求方
 * confirmed — no automatic clear at game boundaries).
 *
 * 039 US3 (T029 — contract §7): init/compact instructions live IN the
 * `playerMessages` channel, so clearing the channel clears stale
 * instructions alongside (no separate slot — see the header note).
 *
 * @param graph The compiled team graph handle (outer graph + checkpointer).
 * @param sessionId The session id — the checkpoint thread id (FR-013).
 */
export async function refreshTeamChannels(
	graph: TeamGraphHandle["graph"],
	sessionId: string,
): Promise<void> {
	const config = { configurable: { thread_id: sessionId } };
	// One update carrying both channel clears: per-channel independence (A1)
	// means the two `RemoveMessage`s never interfere. `gameCounter` is reset
	// alongside (last-write-wins reducer, FR-014); the init/compact
	// instructions live in `playerMessages`, so the channel clear covers them
	// (contract §7 — 039 US3).
	await graph.updateState(config, {
		...clearChannel("playerMessages"),
		...clearChannel("plannerMessages"),
		gameCounter: 0,
	});
	info("refresh team: cleared short-term message channels", { sessionId });
}
