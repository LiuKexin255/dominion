/**
 * team/state.ts — saolei team graph state TYPES.
 *
 * The state schema itself (`TeamState` via `Annotation.Root`) is defined in
 * `graph.ts` as a module-private const — see the TS2883 note below. This
 * module exports the state VALUE types consumed by the nodes, the router and
 * the graph handle.
 *
 * Mirrors `specs/031-team-template-mode/contracts/team-graph-contract.md` §1:
 *
 * - per-agent private message channels `playerMessages` / `plannerMessages`
 *   (D5 — messages are partitioned per agent, matching FR-005 and the
 *   desktop's per-agent tabs), each driven by `messagesStateReducer` so the
 *   channels support `REMOVE_ALL_MESSAGES` (D8 / RefreshTeam);
 * - a structured `gameEnded` control field (`"won" | "lost" | null`) with a
 *   last-write-wins reducer — read by the conditional edge (D6 / A6);
 * - a `gameCounter` field (integer, per-session completed-game counter) with
 *   a last-write-wins reducer — incremented by the planner on return and
 *   reset by RefreshTeam (specs/037-saolei-team-optimize/data-model.md §2;
 *   FR-014).
 *
 * The strategy is NOT in state (it lives in `StrategyStore`, injected into
 * prompts at code level — contract §3); gameState/gameEvent are NOT in state
 * (they live in the per-session ephemeral buffer, `team-sink.ts` — D7).
 *
 * **Must be defined via `Annotation.Root`** (NOT `new StateSchema` + zod):
 * under the pinned `@langchain/langgraph` ^1.4.8 + `zod` ^3.25.76 a plain zod
 * field does not satisfy `SerializableSchema` (spike D14 注意事项 1) — the
 * schema in `graph.ts` follows this.
 *
 * **TS2883 trap** (D14 注意事项 2): exporting an `Annotation.Root` const or
 * a `CompiledStateGraph`-typed value triggers TS2883 (the inferred type
 * cannot be named without a reference to langgraph's dual-package internal
 * `.cjs` paths) once the package emits declarations (the agent package does —
 * tsconfig `"declaration": true`). Empirically verified against the pinned
 * 1.4.8 types: even `export type X = typeof TeamState.State` fails. The
 * resolution (FINDINGS.md option 2 — "在导出边界加类型标注") is that the
 * schema const stays module-private where the graph is built, and this module
 * exports the state value type as a structural interface — the emitted `.d.ts`
 * only ever names OUR types. The channel value types are `BaseMessage[]`
 * (each channel's `Annotation<BaseMessage[]>` value type), so the structural
 * interface is exact.
 */

import type { BaseMessage } from "@langchain/core/messages";

/** Structured game-end flag (D6 step 4); `null` = no end event in this run. */
export type GameEnded = "won" | "lost" | null;

/** The two per-agent message channels (D5). */
export type TeamChannel = "playerMessages" | "plannerMessages";

/**
 * The saolei team graph state value type (contract §1) — structurally
 * identical to the schema's `State` type (see header note). Consumed by the
 * player/planner nodes, the conditional-edge router and `TeamGraphHandle`.
 */
export interface TeamStateValue {
	playerMessages: BaseMessage[];
	plannerMessages: BaseMessage[];
	gameEnded: GameEnded;
	/** Completed-game counter (won/lost, planner-returned); reset by RefreshTeam. */
	gameCounter: number;
}
