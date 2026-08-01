/**
 * context-middleware.ts — RefreshTeam short-term memory clearing (FR-018).
 *
 * `RefreshTeam` clears BOTH per-agent short-term message channels
 * (`playerMessages` and `plannerMessages`) while leaving the long-term
 * strategy (StrategyStore/mongo) and the `gameEnded` control field intact
 * (specs/031-team-template-mode/contracts/team-graph-contract.md §5;
 * research.md D8).
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

import type { TeamChannel } from "./team/state";
import type { TeamGraphHandle } from "./team/graph";

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
 * Out of scope by design: the strategy (StrategyStore/mongo) and the
 * `gameEnded` control field — neither is touched (FR-018; D8). `gameEnded`
 * is cleared by the planner node's normal lifecycle (D6 step 6), and
 * short-term memory is cleared ONLY by `RefreshTeam` (需求方确认 — no
 * automatic clear at game boundaries).
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
  // means the two `RemoveMessage`s never interfere.
  await graph.updateState(config, {
    ...clearChannel("playerMessages"),
    ...clearChannel("plannerMessages"),
  });
  info("refresh team: cleared short-term message channels", { sessionId });
}
