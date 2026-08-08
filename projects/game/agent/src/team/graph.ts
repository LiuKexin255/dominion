/**
 * team/graph.ts — the saolei team graph builder (`TeamGraphHandle`).
 *
 * Builds and compiles the saolei template's LangGraph StateGraph (contract
 * §2.3 / §6; research.md D5/D6). Graph structure:
 *
 * ```text
 * START → [player] ──条件边(读 state.gameEnded)──→ [planner] ──条件边(读 state.gameCounter)──→ [compress] ──→ END
 *                   │  gameEnded=null → END            │  gameCounter%5!==0 → [player] ...
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
 *   unconditionally clears `gameEnded` (D6 step 6) and increments
 *   `gameCounter` (FR-006); the conditional edge then routes BACK to `player`
 *   (NOT END — FR-009: continuing is driven by the player LLM / user, not a
 *   forced loop) or to `compress` when the counter is a positive multiple of 5
 *   (specs/037-saolei-team-optimize/spec.md FR-006/FR-007).
 *   `accepts_user_input=false` (FR-031).
 * - `compress` (specs/037-saolei-team-optimize/contracts/compression-contract.md
 *   §2/§3): summarizes each non-empty channel into one AIMessage, then routes
 *   to END — the player stops and waits for user input (FR-010).
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
import type { MemoryClient } from "../memory-client";
import type { StrategyStore } from "../strategy-store";
import type { FrozenMemorySnapshot } from "./memory-snapshot";
import type { GameEnded, TeamStateValue } from "./state";
import type { EphemeralGameBuffer } from "./team-sink";
import { createCompressNode } from "./compress";
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
	pendingInstruction: Annotation<string | null>({
		// Overwrite (last-write-wins) control field (D10 —
		// `specs/039-planner-memory-calibration/contracts/team-graph-contract.md`
		// §1): the init/compact scenarios' deferred instruction slot. Written
		// by the instruction nodes (Phase 6 T025/T026), consumed and cleared
		// by the player node's entry (Phase 6 T028); RefreshTeam clears it too
		// (contract §7 — Phase 6). Same pattern as `gameEnded`.
		reducer: (_prev: string | null, next: string | null) => next,
		default: () => null,
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
	/**
	 * MemoryService gRPC client — the planner's memory data plane
	 * (`specs/039-planner-memory-calibration/contracts/memory-mcp-contract.md`
	 * §3). Used by the compress node to refresh the frozen snapshot at the
	 * compression boundary (contract §2.4, survey D4).
	 */
	memoryClient: MemoryClient;
	/**
	 * The per-session frozen long-term-memory snapshot
	 * (`specs/039-planner-memory-calibration/contracts/team-graph-contract.md`
	 * §3, survey D5 plan b): injected into the planner's input as a
	 * pure-content SystemMessage (FR-011); refreshed at the compression
	 * boundary ONLY (FR-010). One instance per session — shared by the
	 * planner node and the compress node.
	 */
	frozenSnapshot: FrozenMemorySnapshot;
	/** The session's template path segment (e.g. `"saolei"`) — the memory
	 *  resource scope (FR-012) used by the compress-boundary snapshot refresh. */
	template: string;
	/**
	 * Long-term strategy store — Phase 5 INTERMEDIATE STATE
	 * (`specs/039-planner-memory-calibration/tasks.md` T019): retained ONLY
	 * for the `update_strategy` write path (the planner's tool still writes
	 * to it) and the player's "当前态势" read; the planner's strategy READ
	 * path is replaced by the frozen snapshot. Removed in Phase 6 (T030/T031,
	 * FR-013).
	 */
	strategyStore: StrategyStore;
	/** Per-session ephemeral game-state buffer (sink writes / node reads). */
	buffer: EphemeralGameBuffer;
	/** Session id — the checkpoint thread id. */
	sessionId: string;
	/**
	 * The player's tools — the saolei MCP tools (FR-010, player only). The
	 * actual MCP tools are loaded in server wiring; tests inject fake tools
	 * that drive the sink. Only the GAME-VISIBLE subset's NAME + DESCRIPTION
	 * are injected into the planner's system prompt as static text.
	 */
	playerTools: StructuredToolInterface[];
	/**
	 * The planner's OWN tools — the memory MCP tools (a single hermes-style
	 * `memory` tool, FR-007/FR-008), obtained via the mcp client
	 * (`buildMemoryMcpTools` — server wiring, T022) and DI-injected here.
	 * The `update_strategy` tool is built internally by the planner node
	 * (Phase 5 intermediate write path; removed in Phase 6).
	 */
	plannerTools: StructuredToolInterface[];
	/**
	 * The player's base prompt from `SaoleiProfile.player_prompt` (FR-034 —
	 * empty = template default; skill body always appended by the template).
	 */
	playerBasePrompt: string;
	/**
	 * The planner's base prompt from `SaoleiProfile.planner_prompt` (FR-034 —
	 * empty = template default; the memory skill body is ALWAYS appended by
	 * the template, FR-020).
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
 * Conditional edge after the planner node (specs/037-saolei-team-optimize/
 * contracts/compression-contract.md §2, D7) — reads the `gameCounter` the
 * planner just incremented: a positive multiple of 5 ⇒ compress (the player
 * stops, FR-010); otherwise ⇒ player (continuing, FR-009).
 */
function routeAfterPlanner(
	state: TeamStateValue,
): "compress" | "player" {
	return state.gameCounter > 0 && state.gameCounter % 5 === 0
		? "compress"
		: "player";
}

/**
 * Build and compile the saolei team graph (single TeamState + one outer
 * `MemorySaver`, architecture (i) — A3).
 *
 * The `checkpointer` parameter is the US3 profile-change rebuild seam
 * (specs/040-team-singleton-conformance/contracts/team-rebuild-contract.md
 * §2): an omitted checkpointer (first build) creates a fresh `MemorySaver`;
 * a provided one (rebuild) reuses the EXISTING checkpointer — MemorySaver is
 * a per-thread_id KV store decoupled from the graph instance, so the
 * recompiled graph restores `playerMessages`/`plannerMessages`/`gameEnded`/
 * `gameCounter` from the same thread (`thread_id = sessionId`) with no state
 * loss (FR-005).
 *
 * @param deps The graph's injected dependencies (models/prompts/tools — the
 *   items that change with the TeamProfile; see
 *   team-rebuild-contract.md §4).
 * @param checkpointer The outer MemorySaver to bind. Defaults to a fresh
 *   `new MemorySaver()` (first build); a rebuild MUST pass the existing
 *   `TeamGraphHandle.checkpointer` (never a new one — that would drop the
 *   session's history).
 * @returns A `TeamGraphHandle` — one per session (Batch 2 wiring); the
 *   returned handle carries the SAME checkpointer instance that was bound.
 */
export function buildTeamGraph(
	deps: TeamGraphDeps,
	checkpointer?: MemorySaver,
): TeamGraphHandle {
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
		memoryClient: deps.memoryClient,
		frozenSnapshot: deps.frozenSnapshot,
		// Phase 5 intermediate state (T019/T020): the strategyStore stays for
		// the `update_strategy` WRITE path only; the strategy READ path is
		// replaced by the frozen snapshot input. Removed in Phase 6 (T030).
		strategyStore: deps.strategyStore,
		buffer: deps.buffer,
		sessionId: deps.sessionId,
		plannerBasePrompt: deps.plannerBasePrompt,
		// US3 (specs/037-saolei-team-optimize/spec.md FR-016/FR-017): the
		// player tools are forwarded so the planner node can inject their
		// NAME + DESCRIPTION into its system prompt (static text — the tools
		// themselves stay out of the planner's tool set, FR-018).
		playerTools: deps.playerTools,
		// 039 US2 (T020): the planner's OWN tools — the memory MCP tools
		// (single hermes-style `memory` tool, FR-007/FR-008).
		plannerTools: deps.plannerTools,
		createAgentFn: deps.createAgentFn,
	});
	const compressNode = createCompressNode({
		playerModel: deps.playerModel,
		plannerModel: deps.plannerModel,
		// 039 US2 (T021): the compression boundary re-bakes the planner's
		// frozen memory snapshot (contract §2.4, survey D4).
		memoryClient: deps.memoryClient,
		frozenSnapshot: deps.frozenSnapshot,
		template: deps.template,
		sessionId: deps.sessionId,
	});

	const outer = checkpointer ?? new MemorySaver();
	const graph = new StateGraph(TeamState)
		.addNode("player", playerNode)
		.addNode("planner", plannerNode)
		.addNode("compress", compressNode)
		.addEdge(START, "player")
		.addConditionalEdges("player", routeAfterPlayer)
		// planner → compress | player: the planner clears gameEnded on return
		// (D6 step 6) and increments gameCounter (FR-006); a positive multiple
		// of 5 routes to compress, otherwise back to the player (FR-009 — no
		// forced multi-game loop; compression-contract.md §2).
		.addConditionalEdges("planner", routeAfterPlanner)
		// compress → END: the player stops and waits for user input (FR-010 —
		// the next turn resumes with the summary context).
		.addEdge("compress", END)
		.compile({ checkpointer: outer });

	return { graph, checkpointer: outer };
}
