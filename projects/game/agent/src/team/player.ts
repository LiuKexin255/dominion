/**
 * team/player.ts — the saolei team graph `player` node (contract §2.1).
 *
 * The player is the desktop-controlling agent: it is a `createAgent` whose
 * internal tool loop runs until the LLM stops on its own (D6 — one player
 * node run = one game session's play; the graph NEVER hands over mid-loop).
 * It is the ONLY agent holding the saolei MCP tools (FR-010).
 *
 * - **Tools**: injected via {@link PlayerNodeDeps.tools} (DI seam). The
 *   production saolei MCP tools are wired in server.ts (Batch 2); tests
 *   inject fake tools that drive the sink.
 * - **Strategy**: on entry the node reads `StrategyStore.get(sessionId)` and
 *   injects it into the model prompt as a "当前态势" `SystemMessage` (FR-015 —
 *   code-level injection; the player has NO read tool). The injected message
 *   carries a FIXED id and is filtered out of the channel write-back, so the
 *   strategy never becomes part of the short-term message state (D4: strategy
 *   and short-term messages are decoupled).
 * - **Game-end guard (Issue 1 — `specs/036-team-mode-bugfix/spec.md` FR-001)**:
 *   a `beforeModel` middleware stops the createAgent loop as soon as an
 *   unconsumed game-end event exists in the ephemeral buffer (the LLM never
 *   gets to restart a new game mid-run). The post-process is wrapped in
 *   try/finally, so the event is consumed and `gameEnded` set even when the
 *   invoke throws (FR-002 — US1 acceptance #5).
 * - **Mid-turn queue drain (Feature 038 — US1)**: a second `beforeModel`
 *   middleware (`queueDrain`) fires before EVERY model call inside the
 *   createAgent loop — the turn's first model call AND each call after a
 *   tool result — draining the TurnLoop buffer via
 *   `configurable.drainQueuedInput` and injecting the queued messages as a
 *   `HumanMessage` (FR-001, `specs/038-queue-input-mid-turn/contracts/
 *   injection-seam-contract.md` §3).
 * - **Post-process (once, after `createAgent` returns — D6 step 4)**: consume
 *   the ephemeral buffer's `gameEvent`; if an unconsumed end event exists,
 *   write `TeamState.gameEnded = status` (the conditional edge then routes to
 *   the planner). The event is marked consumed, so the planner fires at most
 *   once per game end.
 * - **Streaming output** (`ContentBlock` → `TeamFrame`, `agent="player"`):
 *   Batch 1 leaves this to Batch 2 (the handler streams the node's channel
 *   messages); {@link PLAYER_AGENT_NAME} is the frame's `agent` value.
 *
 * **createAgent carries NO checkpointer** (spike D14 注意事项 4 / A2): each
 * `.invoke()` is a stateless full loop; per-agent history lives in the OUTER
 * graph's single `MemorySaver`, reconstructed from the per-agent channels.
 */

import { createAgent } from "langchain";
import type { Runtime } from "langchain";
import { HumanMessage, SystemMessage } from "@langchain/core/messages";
import type { BaseMessage } from "@langchain/core/messages";
import type { RunnableConfig } from "@langchain/core/runnables";
import type { StructuredToolInterface } from "@langchain/core/tools";

import { appendSkillBodyToPrompt } from "../skill-loader";
import { buildContentBlocks } from "../llm";
import type { TurnContent } from "../llm";
import type { ChatModel } from "../model-provider";
import type { StrategyStore } from "../strategy-store";
import type { TeamStateValue } from "./state";
import type { EphemeralGameBuffer } from "./team-sink";
import { consumeGameEvent } from "./team-sink";

/** The player agent's name — the `TeamFrame.agent` value (FR-023/D12). */
export const PLAYER_AGENT_NAME = "player";

/**
 * Fixed id of the strategy "当前态势" SystemMessage the node prepends to the
 * model input. Filtering by this id on write-back keeps the strategy out of
 * the short-term message channel (D4 / contract §1: 策略不在 state).
 */
const STRATEGY_MESSAGE_ID = "player-strategy-current";

/**
 * The player's DEFAULT base prompt (the template-fixed fallback base, FR-034
 * semantics A — used when `SaoleiProfile.player_prompt` is empty, see
 * `specs/031-team-template-mode/spec.md` FR-034). The saolei skill body is
 * ALWAYS appended by the template on top of the base
 * (`appendSkillBodyToPrompt(base, ["saolei"])`), whether the base is the
 * profile override or this default — the skill guidance the previous
 * single-agent path injected for saolei profiles (spec 018 FR-023/024). The
 * CURRENT strategy is NOT part of this static prompt — it is injected per
 * entry (FR-015).
 */
export const DEFAULT_PLAYER_BASE =
	"你是扫雷游戏的操作者（player）。你的职责是操作桌面上的扫雷窗口完成一局游戏：" +
	"使用 saolei 工具落子（开新局、点击/标记/双击揭示格子、查询剩余雷数），" +
	"根据返回的文本棋盘持续推理并落子，直到一局以 won/lost 结束或你判断应当停止。" +
	"你独占桌面控制，不要等待其他 agent 的指令；每局结束后你可以自行决定是否开新局。";

/**
 * DI seam overriding `langchain`'s `createAgent` (same pattern as the
 * former `llm.ts` adapter path — `style/javascript.md` §测试: DI over
 * `vi.mock`). Tests inject a spy to assert the options passed; the node calls
 * the returned agent's `invoke({ messages })`.
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export type CreateAgentFn = (config: any) => any;

/** Dependencies of the player node (all injected — DI seam). */
export interface PlayerNodeDeps {
	/** The player's LLM (from the TeamProfile, Batch 2 wiring). */
	model: ChatModel;
	/** Long-term strategy store (reads the "当前态势", FR-015). */
	strategyStore: StrategyStore;
	/** Per-session ephemeral game-state buffer (sink writes, D6/D7). */
	buffer: EphemeralGameBuffer;
	/** Session id — the StrategyStore key and the checkpoint thread id. */
	sessionId: string;
	/** The player's tools (saolei MCP in production; fakes in tests). */
	tools: StructuredToolInterface[];
	/**
	 * The player's base prompt from `SaoleiProfile.player_prompt` (FR-034
	 * semantics A — empty string = unset = fall back to the template default
	 * `DEFAULT_PLAYER_BASE`; the saolei skill body is ALWAYS appended by the
	 * template regardless of this value, see `specs/031-team-template-mode/
	 * spec.md` FR-034).
	 */
	playerBasePrompt: string;
	/** Optional createAgent override (DI seam, defaults to the real one). */
	createAgentFn?: CreateAgentFn;
}

/** Build the current-态势 SystemMessage carrying the strategy text. */
function buildStrategyMessage(strategy: string): BaseMessage {
	const display = strategy === "" ? "（无 — 等待 planner 首局复盘后写入）" : strategy;
	return new SystemMessage({
		id: STRATEGY_MESSAGE_ID,
		content: `当前态势（当前策略）：${display}`,
	});
}

/**
 * Create the player node function (contract §2.1). The node wraps a
 * checkpointer-less `createAgent` (D14 注意事项 4), runs it to completion,
 * then post-processes the sink buffer into `TeamState.gameEnded`.
 *
 * @returns An async node `(state, config?) => Partial<TeamStateValue>`
 *   suitable for `StateGraph.addNode("player", ...)`.
 */
export function createPlayerNode(
	deps: PlayerNodeDeps,
): (
	state: TeamStateValue,
	config?: RunnableConfig,
) => Promise<Partial<TeamStateValue>> {
	const { strategyStore, buffer, sessionId } = deps;
	const createAgentFn = deps.createAgentFn ?? createAgent;

	// No checkpointer: stateless per-invoke agent loop (A2/A3); the outer
	// graph's MemorySaver owns per-agent history.
	//
	// FR-034 semantics A: the base prompt is the profile's player_prompt when
	// non-empty, else the template default; the saolei skill body is ALWAYS
	// appended by the template on top of the base (unaffected by the profile)
	// — `specs/031-team-template-mode/spec.md` FR-034.
	const systemPrompt = appendSkillBodyToPrompt(
		deps.playerBasePrompt !== "" ? deps.playerBasePrompt : DEFAULT_PLAYER_BASE,
		["saolei"],
	);
	const playerAgent = createAgentFn({
		model: deps.model,
		tools: deps.tools,
		systemPrompt,
		// Issue 1 (036): beforeModel guard — stops the createAgent loop before
		// the next model call once an unconsumed game-end event exists
		// (`specs/036-team-mode-bugfix/contracts/team-graph-fix-contract.md`
		// §1.1, FR-001 / US1 acceptance #4). `canJumpTo: ["end"]` is required
		// for the `jumpTo` return (research.md D1).
		middleware: [
			{
				name: "gameEndGuard",
				beforeModel: {
					canJumpTo: ["end"],
					hook: () => {
						if (buffer.gameEvent && !buffer.gameEvent.consumed) {
							return { jumpTo: "end" };
						}
					},
				},
			},
			// Feature 038 (US1): drains the TurnLoop buffer before every model
			// call and injects the queued content as a HumanMessage —
			// mid-turn delivery (FR-001, spec v2)
			// (`specs/038-queue-input-mid-turn/contracts/injection-seam-contract.md`
			// §3).
			{
				name: "queueDrain",
				beforeModel: {
					hook: (_state: unknown, runtime: Runtime) => {
						// The configurable bag is an index signature
						// (`{ [key: string]: unknown }`); the drain callback's
						// contract type is `(() => TurnContent | null) |
						// undefined` (injection-seam-contract.md §1) — the
						// `typeof` guard keeps the runtime check regardless.
						const drain = runtime.configurable?.drainQueuedInput as
							| (() => TurnContent | null)
							| undefined;
						if (typeof drain !== "function") return;
						const drained = drain();
						if (!drained) return;
						return {
							messages: [
								new HumanMessage({
									content: buildContentBlocks(drained),
								}),
							],
						};
					},
				},
			},
		],
	});

	return async (
		state: TeamStateValue,
		config?: RunnableConfig,
	): Promise<Partial<TeamStateValue>> => {
		// FR-015: code-level "当前态势" injection, read fresh each entry.
		const strategy = await strategyStore.get(sessionId);
		const input: BaseMessage[] = [
			buildStrategyMessage(strategy),
			...state.playerMessages,
		];

		// Issue 1 (036): try/finally — `consumeGameEvent` runs even when the
		// invoke throws (GraphRecursionError / model / tool errors), so the
		// game-end event is consumed and `gameEnded` set on BOTH paths. The
		// finally's return intentionally swallows the exception — the node
		// returns normally and the conditional edge routes to the planner
		// (`specs/036-team-mode-bugfix/contracts/team-graph-fix-contract.md`
		// §1.4, FR-002 / US1 acceptance #5).
		let result: { messages: BaseMessage[] } | undefined;
		try {
			result = (await playerAgent.invoke(
				{ messages: input },
				config,
			)) as { messages: BaseMessage[] };
		} finally {
			// D6 step 4: consume the buffer's end event ONCE (marks consumed).
			const gameEvent = consumeGameEvent(buffer);
			return {
				// Filter the strategy message out of the channel write-back —
				// the strategy stays in StrategyStore, not in short-term state
				// (D4). `result` is undefined when the invoke threw — no
				// messages to write back.
				playerMessages: (result?.messages ?? []).filter(
					(m: BaseMessage) => m.id !== STRATEGY_MESSAGE_ID,
				),
				...(gameEvent ? { gameEnded: gameEvent.status } : {}),
			};
		}
	};
}
