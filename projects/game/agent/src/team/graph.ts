/**
 * team/graph.ts — the saolei team graph builder (`TeamGraphHandle`).
 *
 * Builds and compiles the saolei template's LangGraph StateGraph (contract
 * §2.3 / §6; research.md D5/D6). Graph structure:
 *
 * ```text
 * START → [player] ──条件边(读 state.gameEnded)──→ [planner] ──→ [player] ...
 *                   │  gameEnded=null → END
 *                   │  gameEnded≠null → planner
 * ```
 *
 * - `player` (contract §2.1): createAgent full loop, saolei MCP tools
 *   (injected via deps — Batch 2 wires the real MCP tools, tests inject
 *   fakes), strategy injected as "当前态势" per entry, post-process consumes
 *   the buffer's gameEvent into `gameEnded`. `accepts_user_input=true`
 *   (FR-031).
 * - `planner` (contract §2.2): triggered exactly once per game end via the
 *   conditional edge; tools = `update_strategy` only; on return the graph
 *   unconditionally clears `gameEnded` (D6 step 6) and routes BACK to
 *   `player` (NOT END — FR-009: continuing is driven by the player LLM /
 *   user, not a forced loop). `accepts_user_input=false` (FR-031).
 * - One single outer `MemorySaver` (A3): per-agent history is reconstructed
 *   from the per-agent channels; the createAgents carry NO checkpointer
 *   (D14 注意事项 4 / A2).
 *
 * **TS2883**: the `Annotation.Root` schema const is module-private here (see
 * `state.ts` header note — exporting it would break declaration emit); the
 * builder's return is annotated with the structural {@link TeamGraphHandle}.
 */

import {
	Annotation,
	END,
	MemorySaver,
	START,
	StateGraph,
	messagesStateReducer,
} from "@langchain/langgraph";
import type { BaseMessage } from "@langchain/core/messages";
import type { StructuredToolInterface } from "@langchain/core/tools";

import type { ChatModel } from "../model-provider";
import type { StrategyStore } from "../strategy-store";
import type { GameEnded, TeamStateValue } from "./state";
import type { EphemeralGameBuffer } from "./team-sink";
import { createPlayerNode } from "./player";
import type { CreateAgentFn } from "./player";
import { createPlannerNode } from "./planner";

/**
 * The saolei team graph state schema (contract §1). Module-private: see the
 * TS2883 note in `state.ts`; the value type is exported there as the
 * structural {@link TeamStateValue} instead. Uses `Annotation.Root` (NOT
 * `new StateSchema` + zod — D14 注意事项 1) with `messagesStateReducer`
 * channels (REMOVE_ALL_MESSAGES support, D8/A1), a last-write-wins
 * `gameEnded` control field (A6) and a last-write-wins `gameCounter`
 * (specs/037-saolei-team-optimize/data-model.md §2).
 */
const TeamState = Annotation.Root({
	playerMessages: Annotation<BaseMessage[]>({
		reducer: messagesStateReducer,
		default: () => [],
	}),
	plannerMessages: Annotation<BaseMessage[]>({
		reducer: messagesStateReducer,
		default: () => [],
	}),
	gameEnded: Annotation<GameEnded>({
		// Overwrite (last-write-wins) control field; the conditional edge
		// reads it (A6). `null` after the planner clears it (D6 step 6).
		reducer: (_prev: GameEnded, next: GameEnded) => next,
		default: () => null,
	}),
	gameCounter: Annotation<number>({
		// Overwrite (last-write-wins) counter; the planner increments it on
		// return and RefreshTeam resets it (specs/037-saolei-team-optimize/
		// data-model.md §2, FR-006/FR-014). Same pattern as `gameEnded`.
		reducer: (_prev: number, next: number) => next,
		default: () => 0,
	}),
});

/** A team agent's template-schema description (FR-031, D3). */
export interface TeamAgent {
	name: string;
	accepts_user_input: boolean;
}

/**
 * The saolei template's agent list — the typed source of the `Team.agents`
 * resource description (D3; `TeamAgent` message fields). The desktop renders
 * its per-agent tabs from this list generically (FR-025), and the handler
 * gates user input on `accepts_user_input` (FR-032: `planner` accepts none).
 */
export const SAOLEI_TEAM_AGENTS: TeamAgent[] = [
	{ name: "player", accepts_user_input: true },
	{ name: "planner", accepts_user_input: false },
];

/** The compiled team graph handle (invoke/getState on a session thread). */
export interface TeamGraphHandle {
	/**
	 * The compiled team graph. Runs one team turn per `invoke` on the
	 * session's thread (thread_id = session id, FR-013); `gameEnded` is
	 * handled inside the turn by the conditional edge, so a turn never
	 * requires external continuation.
	 */
	graph: {
		invoke(
			input: Partial<TeamStateValue>,
			config?: Record<string, unknown>,
		): Promise<TeamStateValue>;
		/** Read the thread's latest state (per-agent channel reconstruction). */
		getState(
			config: Record<string, unknown>,
		): Promise<{ values: TeamStateValue } | null>;
		/**
		 * Edit the thread's state (Batch 2 — RefreshTeam, FR-018). Values
		 * flow through the channels' reducers (checkpointer semantics), so
		 * `RemoveMessage({ id: REMOVE_ALL_MESSAGES })` per channel clears
		 * that channel (spike A1; contract §5).
		 */
		updateState(
			config: Record<string, unknown>,
			values: Partial<TeamStateValue>,
		): Promise<unknown>;
		/**
		 * Stream a team turn (Batch 2 — `SessionTeam.runTeamTurn`). Yields
		 * per-node `updates` events; the turn completes on its own
		 * (`gameEnded` is handled inside by the conditional edge).
		 */
		streamEvents(
			input: Partial<TeamStateValue>,
			config?: Record<string, unknown>,
		): Promise<unknown>;
	};
	/** The single outer `MemorySaver` bound to the graph (A3). */
	checkpointer: MemorySaver;
}

/** Dependencies of the saolei team graph builder (all injected — DI). */
export interface TeamGraphDeps {
	/** The player's LLM (from the TeamProfile, Batch 2 wiring). */
	playerModel: ChatModel;
	/** The planner's LLM (from the TeamProfile, Batch 2 wiring). */
	plannerModel: ChatModel;
	/** Long-term strategy store (mongo in production; fake in tests). */
	strategyStore: StrategyStore;
	/** Per-session ephemeral game-state buffer (sink writes / node reads). */
	buffer: EphemeralGameBuffer;
	/** Session id — the StrategyStore key and the checkpoint thread id. */
	sessionId: string;
	/**
	 * The player's tools — the saolei MCP tools (FR-010, player only). The
	 * actual MCP tools are loaded in Batch 2 (server wiring); tests inject
	 * fake tools that drive the sink. The planner's `update_strategy` tool is
	 * built internally (planner holds no other tools, FR-012).
	 */
	playerTools: StructuredToolInterface[];
	/**
	 * The player's base prompt from `SaoleiProfile.player_prompt` (FR-034 —
	 * empty = template default; skill body always appended by the template).
	 */
	playerBasePrompt: string;
	/**
	 * The planner's base prompt from `SaoleiProfile.planner_prompt` (FR-034 —
	 * empty = template default; no skill body appended).
	 */
	plannerBasePrompt: string;
	/** Optional createAgent override (DI seam, defaults to the real one). */
	createAgentFn?: CreateAgentFn;
}

/**
 * Conditional edge after the player node (contract §2.3) — reads the
 * NON-messages `gameEnded` field (A6): a game ended during the player run
 * ⇒ planner (once per game end); no end ⇒ END (FR-009: no forced loop).
 */
function routeAfterPlayer(
	state: TeamStateValue,
): "planner" | typeof END {
	return state.gameEnded ? "planner" : END;
}

/**
 * Build and compile the saolei team graph (single TeamState + one outer
 * `MemorySaver`, architecture (i) — A3).
 *
 * @returns A fresh `TeamGraphHandle` — one per session (Batch 2 wiring).
 */
export function buildTeamGraph(deps: TeamGraphDeps): TeamGraphHandle {
	const playerNode = createPlayerNode({
		model: deps.playerModel,
		strategyStore: deps.strategyStore,
		buffer: deps.buffer,
		sessionId: deps.sessionId,
		tools: deps.playerTools,
		playerBasePrompt: deps.playerBasePrompt,
		createAgentFn: deps.createAgentFn,
	});
	const plannerNode = createPlannerNode({
		model: deps.plannerModel,
		strategyStore: deps.strategyStore,
		buffer: deps.buffer,
		sessionId: deps.sessionId,
		plannerBasePrompt: deps.plannerBasePrompt,
		createAgentFn: deps.createAgentFn,
	});

	const checkpointer = new MemorySaver();
	const graph = new StateGraph(TeamState)
		.addNode("player", playerNode)
		.addNode("planner", plannerNode)
		.addEdge(START, "player")
		.addConditionalEdges("player", routeAfterPlayer)
		// planner → player (NOT END): the planner clears gameEnded on return
		// (D6 step 6) and hands back to the player, which decides whether to
		// start another game or stop (FR-009 — no forced multi-game loop).
		.addEdge("planner", "player")
		.compile({ checkpointer });

	return { graph, checkpointer };
}
