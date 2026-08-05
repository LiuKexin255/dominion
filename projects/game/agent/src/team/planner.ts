/**
 * team/planner.ts — the saolei team graph `planner` node (contract §2.2).
 *
 * The planner is the review agent: it is triggered EXACTLY ONCE per game end
 * (the conditional edge routes to it only when `TeamState.gameEnded ≠ null`,
 * FR-011), never fires per move, and NEVER touches the desktop (FR-010).
 *
 * - **Input**: `plannerMessages` history + a fresh review request built from
 *   the ephemeral buffer's `gameLog` (the full move sequence of the current
 *   game — Issue 2, `specs/036-team-mode-bugfix/contracts/
 *   team-graph-fix-contract.md` §2.2); its system context = [复盘指令] +
 *   [当前策略, 初始 `""`] (FR-014 — the strategy is read fresh each entry and
 *   injected as a fixed-id `SystemMessage`, then filtered out of the channel
 *   write-back: strategy stays in StrategyStore).
 * - **Tools**: ONLY `update_strategy` (FR-012, no read tools).
 * - **Retry/degrade**: `update_strategy` retries live INSIDE this node (D6 /
 *   需求方 #6): tool-call failures surface to the model within the agent
 *   loop (the model may retry the call), and a failing agent invoke is
 *   retried a bounded number of times before degrading. The graph scheduler
 *   never re-routes the planner.
 * - **Return**: `{ plannerMessages, gameEnded: null, gameCounter }` — the graph
 *   clears `gameEnded` UNCONDITIONALLY after the planner node returns (D6 step
 *   6, whether or not `update_strategy` succeeded), so the planner fires at
 *   most once per game end; the edge back to `player` follows (FR-009 —
 *   continuing is driven by the player LLM / user, not a forced loop). The
 *   node also increments the per-session `gameCounter` on BOTH paths (success
 *   and degrade): every ended game — won or lost — counts toward the 5-game
 *   compression trigger (specs/037-saolei-team-optimize/spec.md FR-006;
 *   contracts/compression-contract.md §4).
 *
 * **createAgent carries NO checkpointer** (D14 注意事项 4 / A2), same as the
 * player node: history lives in the outer graph's single `MemorySaver`.
 */

import { createAgent } from "langchain";
import { HumanMessage, SystemMessage } from "@langchain/core/messages";
import type { BaseMessage } from "@langchain/core/messages";
import type { RunnableConfig } from "@langchain/core/runnables";
import { renderBoardText } from "@dominion/game-saolei-board";
import { warn } from "@dominion/common-js-logs";

import type { ChatModel } from "../model-provider";
import type { ChannelFrameEmitter } from "../session-team";
import type { StrategyStore } from "../strategy-store";
import type { TeamStateValue } from "./state";
import type { EphemeralGameBuffer } from "./team-sink";
import { buildUpdateStrategyTool } from "./update-strategy";
import type { CreateAgentFn } from "./player";

/** The planner agent's name — the `TeamFrame.agent` value (FR-023/D12). */
export const PLANNER_AGENT_NAME = "planner";

/**
 * Fixed id of the strategy SystemMessage the node prepends to the model
 * input. Filtered out of the channel write-back (same rationale as the
 * player node — strategy not in short-term state, D4).
 */
const STRATEGY_MESSAGE_ID = "planner-strategy-current";

/**
 * Bounded retry count for a failing planner agent invoke (D6: retry is
 * handled inside the planner node; after that the node degrades).
 */
const MAX_PLANNER_ATTEMPTS = 3;

/**
 * The planner's DEFAULT base prompt (the template-fixed fallback base, FR-034
 * semantics A — used when `SaoleiProfile.planner_prompt` is empty, see
 * `specs/031-team-template-mode/spec.md` FR-034). The planner appends NO
 * skill body (it holds no saolei tools, FR-012). The current strategy is
 * injected per entry (FR-014), NOT baked into this prompt.
 */
export const DEFAULT_PLANNER_BASE =
	"你是扫雷团队的复盘规划者（planner）。每局游戏结束后你会收到本局的终局棋盘" +
	"与当前策略。你的职责：复盘本局表现（判断策略是否有效），" +
	"若你认为策略需要更新，调用 update_strategy 写入新策略（新策略将整体替换旧策略）；" +
	"若无需更新，则不调用任何工具直接结束。你不操作桌面，不持有任何读取工具。" +
	"策略将由 player 在下一局作为当前态势读取。";

/** Dependencies of the planner node (all injected — DI seam). */
export interface PlannerNodeDeps {
	/** The planner's LLM (from the TeamProfile, Batch 2 wiring). */
	model: ChatModel;
	/** Long-term strategy store (system context + `update_strategy` writes). */
	strategyStore: StrategyStore;
	/** Per-session ephemeral game-state buffer (review input, D6 step 6). */
	buffer: EphemeralGameBuffer;
	/** Session id — the StrategyStore key and the checkpoint thread id. */
	sessionId: string;
	/**
	 * The planner's base prompt from `SaoleiProfile.planner_prompt` (FR-034
	 * semantics A — empty string = unset = fall back to the template default
	 * `DEFAULT_PLANNER_BASE`; the planner appends NO skill body, see
	 * `specs/031-team-template-mode/spec.md` FR-034).
	 */
	plannerBasePrompt: string;
	/** Optional createAgent override (DI seam, defaults to the real one). */
	createAgentFn?: CreateAgentFn;
}

/** Build the strategy SystemMessage for the planner's system context. */
function buildStrategyMessage(strategy: string): BaseMessage {
	const display = strategy === "" ? "（无 — 初始为空）" : strategy;
	return new SystemMessage({
		id: STRATEGY_MESSAGE_ID,
		content: `当前策略：${display}`,
	});
}

/**
 * Build the review request from the ephemeral buffer's `gameLog` — the full
 * move sequence of the current game (Issue 2: the planner sees every step's
 * tool, coordinates and post-step board, not just the terminal snapshot —
 * `specs/036-team-mode-bugfix/contracts/team-graph-fix-contract.md` §2.2,
 * `specs/036-team-mode-bugfix/data-model.md` §2). Each entry's board is
 * rendered via `renderBoardText` so the model receives the same compact board
 * the saolei tools produce (no image, no tool-result parsing).
 */
function buildReviewInput(buffer: EphemeralGameBuffer): BaseMessage {
	const log = buffer.gameLog;
	if (log.length === 0) {
		return new HumanMessage("请复盘本局游戏（无可用游戏记录）。");
	}
	const lines: string[] = ["本局游戏过程："];
	for (let i = 0; i < log.length; i += 1) {
		const entry = log[i];
		const coord = entry.x != null ? `(${entry.x}, ${entry.y})` : "";
		lines.push(`${i + 1}. ${entry.tool}${coord} → ${entry.status}`);
		lines.push(renderBoardText(entry.state));
		lines.push("");
	}
	lines.push(
		"请复盘本局游戏表现，判断策略是否有效，若需要更新则调用 update_strategy。",
	);
	return new HumanMessage(lines.join("\n"));
}

/**
 * Run a stateless `createAgent` invoke with a bounded retry (D6 — retry
 * lives inside the planner node; the graph scheduler never re-routes the
 * planner). Re-throws when all attempts fail so the node can degrade.
 * The outer graph's `config` (recursionLimit / signal) is forwarded to the
 * agent invoke (Issue 4 — `specs/036-team-mode-bugfix/contracts/
 * team-graph-fix-contract.md` §3.2).
 */
async function invokeWithRetry(
	agent: {
		invoke(
			input: { messages: BaseMessage[] },
			config?: RunnableConfig,
		): Promise<{ messages: BaseMessage[] }>;
	},
	input: BaseMessage[],
	config?: RunnableConfig,
): Promise<{ messages: BaseMessage[] }> {
	let lastError: unknown;
	for (let attempt = 1; attempt <= MAX_PLANNER_ATTEMPTS; attempt += 1) {
		try {
			return (await agent.invoke({ messages: input }, config)) as {
				messages: BaseMessage[];
			};
		} catch (err) {
			lastError = err;
			if (attempt < MAX_PLANNER_ATTEMPTS) {
				const message = err instanceof Error ? err.message : String(err);
				warn("planner invoke failed; retrying", {
					attempt,
					error: message,
				});
			}
		}
	}
	throw lastError;
}

/**
 * Create the planner node function (contract §2.2). Runs the planner agent
 * (tools = `update_strategy` only), then unconditionally clears
 * `gameEnded` (D6 step 6).
 *
 * @returns An async node `(state, config?) => Partial<TeamStateValue>`
 *   suitable for `StateGraph.addNode("planner", ...)` (Issue 4 — the node
 *   accepts and forwards the outer graph's config, FR-013).
 */
export function createPlannerNode(
	deps: PlannerNodeDeps,
): (
	state: TeamStateValue,
	config?: RunnableConfig,
) => Promise<Partial<TeamStateValue>> {
	const { strategyStore, buffer, sessionId } = deps;
	const createAgentFn = deps.createAgentFn ?? createAgent;

	// FR-034 semantics A: the base prompt is the profile's planner_prompt
	// when non-empty, else the template default; NO skill body is appended
	// (the planner holds no saolei tools, FR-012) —
	// `specs/031-team-template-mode/spec.md` FR-034.
	const systemPrompt =
		deps.plannerBasePrompt !== "" ? deps.plannerBasePrompt : DEFAULT_PLANNER_BASE;
	const plannerAgent = createAgentFn({
		model: deps.model,
		tools: [buildUpdateStrategyTool(strategyStore, sessionId)],
		systemPrompt,
	});

	return async (
		state: TeamStateValue,
		config?: RunnableConfig,
	): Promise<Partial<TeamStateValue>> => {
		// FR-014: current strategy (initial "") as system context, fresh read.
		const strategy = await strategyStore.get(sessionId);
		// Issue 2: review input = the ephemeral buffer's full gameLog.
		const reviewInput = buildReviewInput(buffer);

		// US1 (specs/037-saolei-team-optimize/spec.md FR-001/FR-004): the
		// review input is a non-model-produced channel message — createAgent
		// injects it as INPUT, so streamEvents never emits it (bug root
		// cause, specs/031-team-template-mode/bug-analysis.md Issue 2). Emit
		// its content as a real-time frame so the desktop planner tab shows
		// it without a reload. The emitter rides LangGraph `configurable`
		// (tasks.md 决策 #1 — specs/037-saolei-team-optimize/plan.md), read
		// as `ChannelFrameEmitter | undefined` (session-team.ts exports it).
		const emitChannelFrame = config?.configurable?.emitChannelFrame as
			| ChannelFrameEmitter
			| undefined;
		if (emitChannelFrame) {
			const content =
				typeof reviewInput.content === "string" ? reviewInput.content : "";
			if (content) {
				// FR-004: the empty-gameLog notice ("无可用游戏记录") is a
				// non-empty content too — emitted along with full gameLogs.
				emitChannelFrame(PLANNER_AGENT_NAME, content);
			}
		}

		const input: BaseMessage[] = [
			buildStrategyMessage(strategy),
			...state.plannerMessages,
			reviewInput,
		];

		let result: { messages: BaseMessage[] };
		try {
			result = await invokeWithRetry(plannerAgent, input, config);
		} catch (err) {
			const message = err instanceof Error ? err.message : String(err);
			warn("planner failed after retries; degrading", { error: message });
			// D6 step 6: clear gameEnded unconditionally (success or failure)
			// so the planner does not re-trigger on the same game end.
			// Degrade trade-off: the reviewInput frame was already emitted
			// above (specs/037-saolei-team-optimize/spec.md FR-001/FR-004 —
			// real-time visible on the desktop planner tab), but this return
			// writes NO plannerMessages, so the live frame and the reloaded
			// channel history diverge while degraded — accepted: real-time
			// visibility takes priority when the planner LLM is unavailable.
			// The ended game still counts toward the compression trigger even
			// when degraded (FR-006, compression-contract.md §4).
			return {
				gameEnded: null,
				gameCounter: state.gameCounter + 1,
			};
		}

		return {
			plannerMessages: result.messages.filter(
				(m: BaseMessage) => m.id !== STRATEGY_MESSAGE_ID,
			),
			// D6 step 6: unconditional clear — the planner fires at most once
			// per game end; the edge routes back to the player (FR-009).
			gameEnded: null,
			// FR-006: the ended game (won or lost) counts toward the 5-game
			// compression trigger (compression-contract.md §4).
			gameCounter: state.gameCounter + 1,
		};
	};
}
