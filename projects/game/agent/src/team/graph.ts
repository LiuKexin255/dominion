/**
 * team/graph.ts — the saolei team graph builder (`TeamGraphHandle`).
 *
 * Builds and compiles the saolei template's LangGraph StateGraph (contract
 * §2.3 / §6; research.md D5/D6). Graph structure (039 Phase 6 — T026):
 *
 * ```text
 * START ──条件边(runInitInstruction?)──→ [initInstruction] ──→ END
 *                   │ 普通 turn（无 init 标记）→ [player]
 *  player ──条件边(读 state.gameEnded)──→ [planner] ──条件边(读 state.gameCounter)──→ [compress] → [postCompactInstruction] → END
 *                   │  gameEnded=null → END            │  gameCounter%5!==0 → [player] ...
 *                   │  gameEnded≠null → planner
 * ```
 *
 * - `player` (contract §2.1): createAgent full loop, saolei MCP tools
 *   (injected via deps — Batch 2 wires the real MCP tools, tests inject
 *   fakes), post-process consumes the buffer's gameEvent into
 *   `gameEnded`. `accepts_user_input=true` (FR-031). Calibration
 *   instructions arrive as plain HumanMessages in `playerMessages` (039
 *   US3 — the instruction nodes write the channel directly, no pending
 *   slot).
 * - `planner` (contract §2.2): triggered exactly once per game end via the
 *   conditional edge; tools = the memory MCP tools + `instruct_player`
 *   (Phase 6 — the shared strategy tool/store are gone, FR-013); on return
 *   the graph unconditionally clears `gameEnded` (D6 step 6) and increments
 *   `gameCounter` (FR-006); a review-sent calibration instruction is
 *   appended to `playerMessages` from the node return value (FR-017); the
 *   conditional edge then routes BACK to `player` (NOT END — FR-009:
 *   continuing is driven by the player LLM / user, not a forced loop) or to
 *   `compress` when the counter is a positive multiple of 5
 *   (specs/037-saolei-team-optimize/spec.md FR-006/FR-007).
 *   `accepts_user_input=false` (FR-031).
 * - `initInstruction` (039 US3, T025/T026 — contract §2.3, FR-015): the
 *   team-init scenario node. The START conditional edge routes to it ONLY
 *   when the turn carries the `runInitInstruction` configurable flag (the
 *   async init turn triggered once after graph FIRST materialization —
 *   session-team.ts, R2); it produces a no-game-history instruction into
 *   `playerMessages` (LLM decides, R4) and routes to END — the player is
 *   NOT invoked (FR-015 "不立即激活 player": the instruction is delivered
 *   with the player's next activation, contract §6). Ordinary turns skip it
 *   entirely.
 * - `postCompactInstruction` (039 US3, T025/T026 — contract §2.3, FR-016):
 *   runs after `compress` (which cleared the channels AND refreshed the
 *   frozen memory snapshot — contract §2.4, T021) and before END; produces
 *   a no-game-history instruction into `playerMessages` and stops — the
 *   player stops and waits for user input (FR-010), the instruction is
 *   delivered with the next activation (037"压缩后自动停下"一致).
 * - `compress` (specs/037-saolei-team-optimize/contracts/compression-contract.md
 *   §2/§3): summarizes each non-empty channel into one AIMessage, refreshes
 *   the frozen memory snapshot (039 T021), then routes to
 *   `postCompactInstruction` → END.
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
import type { RunnableConfig } from "@langchain/core/runnables";
import type { StructuredToolInterface } from "@langchain/core/tools";

import type { ChatModel } from "../model-provider";
import type { MemoryClient } from "../memory-client";
import { resolveStreamIdleTimeout } from "../reasoning-timeouts";
import type { FrozenMemorySnapshot } from "./memory-snapshot";
import type { GameEnded, TeamStateValue } from "./state";
import type { EphemeralGameBuffer } from "./team-sink";
import { createCompressNode } from "./compress";
import { createInstructionNode } from "./instruction-node";
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
		 *
		 * 044 US3 (T007 — partial-output-contract.md §2): the optional
		 * `asNode` explicitly attributes the update to a graph node (the
		 * stalled node). Without it LangGraph infers the attributing node
		 * from `versions_seen`; passing it makes the attribution exact and
		 * robust on any thread state.
		 */
		updateState(
			config: Record<string, unknown>,
			values: Partial<TeamStateValue>,
			asNode?: string,
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
	 * The `instruct_player` calibration tool is built internally by the
	 * planner-family nodes (T027 — Phase 6).
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
	/**
	 * The player's bare model spec (e.g. `"openai/deepseek-v4-flash"`) — the
	 * string BEFORE `getProvider` resolution, used to apply the
	 * per-reasoning-model idle-timeout floor (044 US2,
	 * specs/044-llm-stall-recovery-fix/contracts/idle-timeout-contract.md
	 * §3). Optional: when omitted the node falls back to
	 * `STREAM_IDLE_TIMEOUT_MS`.
	 */
	playerModelSpec?: string;
	/** The planner's bare model spec — same floor semantics as
	 *  `playerModelSpec`. */
	plannerModelSpec?: string;
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
 * Conditional edge after START (039 US3, T026 — contract §2.3/§6): routes
 * to `initInstruction` ONLY on the async team-init turn — signalled via the
 * `runInitInstruction` configurable flag (installed by session-team.ts after
 * graph FIRST materialization, R2); every ordinary turn (user input) goes
 * straight to the player, so the init node never runs outside the init turn.
 * The init turn stops at `initInstruction` (→ END): the player is NOT
 * invoked (FR-015 — the instruction is delivered with the player's next
 * activation, contract §6).
 */
function routeAfterStart(
	_state: TeamStateValue,
	config?: RunnableConfig,
): "initInstruction" | "player" {
	const runInit = (config?.configurable as
		| { runInitInstruction?: boolean }
		| undefined)?.runInitInstruction;
	return runInit ? "initInstruction" : "player";
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
		buffer: deps.buffer,
		sessionId: deps.sessionId,
		tools: deps.playerTools,
		playerBasePrompt: deps.playerBasePrompt,
		createAgentFn: deps.createAgentFn,
	});
	const plannerNode = createPlannerNode({
		model: deps.plannerModel,
		frozenSnapshot: deps.frozenSnapshot,
		buffer: deps.buffer,
		plannerBasePrompt: deps.plannerBasePrompt,
		// US3 (specs/037-saolei-team-optimize/spec.md FR-016/FR-017): the
		// player tools are forwarded so the planner node can inject their
		// NAME + DESCRIPTION into its system prompt (static text — the tools
		// themselves stay out of the planner's tool set, FR-018).
		playerTools: deps.playerTools,
		// 039 US2 (T020): the planner's OWN tools — the memory MCP tools
		// (single hermes-style `memory` tool, FR-007/FR-008). The
		// `instruct_player` tool is built internally (T027 — Phase 6).
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
	// 039 US3 (T025/T026 — contract §2.3, FR-019): the init/compact scenario
	// nodes share one factory, differentiated by the `scenario` prompt. Both
	// use the planner model + the shared frozen snapshot (init reads the
	// team-init bake; postCompactInstruction reads the compress-boundary
	// refresh — the compress node refreshes BEFORE this node runs, T021).
	const initInstructionNode = createInstructionNode(
		{
			model: deps.plannerModel,
			frozenSnapshot: deps.frozenSnapshot,
			plannerBasePrompt: deps.plannerBasePrompt,
			createAgentFn: deps.createAgentFn,
		},
		"init",
	);
	const postCompactInstructionNode = createInstructionNode(
		{
			model: deps.plannerModel,
			frozenSnapshot: deps.frozenSnapshot,
			plannerBasePrompt: deps.plannerBasePrompt,
			createAgentFn: deps.createAgentFn,
		},
		"compact",
	);

	const outer = checkpointer ?? new MemorySaver();
	const graph = new StateGraph(TeamState)
		.addNode("initInstruction", initInstructionNode)
		.addNode("postCompactInstruction", postCompactInstructionNode)
		// The idle timeout (043 — specs/043-llm-stream-stall-recovery/
		// contracts/stall-recovery-contract.md §1.1) applies ONLY to the
		// model-holding nodes player/planner: a stalled LLM SSE stream must
		// raise NodeTimeoutError within the resolved idle period (FR-001).
		// 044 US2 (specs/044-llm-stall-recovery-fix/tasks.md T004): the
		// per-reasoning-model floor (specs/044-llm-stall-recovery-fix/
		// contracts/idle-timeout-contract.md §1) raises the effective
		// timeout via `resolveStreamIdleTimeout(modelSpec)` when the deps
		// carry the model spec; omitted specs fall back to
		// `STREAM_IDLE_TIMEOUT_MS`.
		// `setNodeDefaults` is intentionally NOT used — it would extend the
		// timeout to initInstruction/postCompactInstruction/compress, whose
		// event patterns are out of scope (tasks.md Phase 2 F2 scope note;
		// initInstruction/postCompactInstruction are covered by the init-turn
		// total timeout FR-009). `refreshOn: "auto"` refreshes on model
		// tokens + tool start/end; the mid-tool gap is covered by the
		// client-side MCP heartbeat wrapper
		// (specs/043-llm-stream-stall-recovery/research.md R7.2,
		// specs/043-llm-stream-stall-recovery/contracts/stall-recovery-contract.md
		// §1.2).
		.addNode("player", playerNode, {
			timeout: {
				idleTimeout: resolveStreamIdleTimeout(deps.playerModelSpec),
				refreshOn: "auto",
			},
		})
		.addNode("planner", plannerNode, {
			timeout: {
				idleTimeout: resolveStreamIdleTimeout(deps.plannerModelSpec),
				refreshOn: "auto",
			},
		})
		.addNode("compress", compressNode)
		// 039 US3 (T026 — R5): the START conditional edge routes to
		// `initInstruction` ONLY on the async team-init turn (configurable
		// `runInitInstruction` — session-team.ts, R2); ordinary turns go
		// straight to the player. initInstruction → END: the init turn does
		// NOT invoke the player — the instruction lands in `playerMessages`
		// and is delivered with the player's next activation (FR-015
		// "不立即激活 player", contract §6).
		.addConditionalEdges(START, routeAfterStart)
		.addEdge("initInstruction", END)
		.addConditionalEdges("player", routeAfterPlayer)
		// planner → compress | player: the planner clears gameEnded on return
		// (D6 step 6) and increments gameCounter (FR-006); a positive multiple
		// of 5 routes to compress, otherwise back to the player (FR-009 — no
		// forced multi-game loop; compression-contract.md §2).
		.addConditionalEdges("planner", routeAfterPlanner)
		// compress → postCompactInstruction → END: after the compress node
		// cleared the channels and refreshed the frozen snapshot (T021), the
		// compact scenario produces a no-game-history instruction into
		// `playerMessages` (LLM decides, R4), then the turn ENDs — the
		// player stops and waits for user input (FR-010; FR-016 — the
		// instruction is delivered with the next activation, 037"压缩后自动
		// 停下"一致).
		.addEdge("compress", "postCompactInstruction")
		.addEdge("postCompactInstruction", END)
		.compile({ checkpointer: outer });

	return { graph, checkpointer: outer };
}
