/**
 * team/compress.ts — the saolei team graph `compress` node (US2).
 *
 * Triggered by the conditional edge after the planner when `gameCounter` is a
 * positive multiple of 5 (specs/037-saolei-team-optimize/contracts/
 * compression-contract.md §2). It compresses EACH non-empty short-term channel
 * (`playerMessages` / `plannerMessages`) into a single summary AIMessage and
 * routes to END — the player stops and waits for user input (FR-010).
 *
 * - Per channel (contract §3): serialize the messages to text, call the
 *   channel's own model (`playerModel` for the player channel, `plannerModel`
 *   for the planner channel — research.md D1 "复用各自 agent 模型") with the
 *   role-specific summary prompt, and validate the summary: the response is
 *   normalized to a plain string via `extractTextContent` (the OpenAI
 *   adapters may return the string OR the standard content-blocks array
 *   `[{type:"text",text}, ...]` — @langchain/core stamps
 *   `response_metadata.output_version: "v1"` on the block form), and a blank
 *   summary is rejected (FR-012: blank summary = compression failure, abort;
 *   a tool-call-only response has no text and is therefore blank too).
 * - The channel update is `[RemoveMessage({ id: REMOVE_ALL_MESSAGES }),
 *   summaryAIMessage]` — the same clear-then-write path `RefreshTeam` uses
 *   (team-graph-contract.md §5; messagesStateReducer REMOVE_ALL_MESSAGES
 *   support, spike A1). After the node, each compressed channel holds exactly
 *   the one summary message (FR-008).
 * - The summary AIMessage's `id` is a fresh `randomUUID()` and doubles as the
 *   live-frame dedup anchor: the node passes it as the `frameId` argument of
 *   `emitChannelFrame(agent, content, frameId)`, so the live frame and the
 *   reloaded ListMessages entry share one id and desktop `renderedMessageIds`
 *   dedups them (data-model.md §4 去重规则, research.md D9).
 * - Both channel summaries are generated BEFORE any frame is emitted: if the
 *   second summary throws, no frame has been emitted yet, so a failed
 *   compression never leaves an orphan frame (a frameId matching no persisted
 *   message, which reload dedup could not resolve).
 * - Failure semantics (FR-013): any LLM error, non-string response, or blank
 *   summary propagates out of the node untouched (no catch, no degrade) —
 *   the graph aborts the turn.
 * - Empty channel = no-op (FR-015): skipped, no summary message, no frame.
 * - **Frozen memory snapshot refresh (039 US2, T021 — contract §2.4, survey
 *   D4)**: after the summaries, the compression boundary re-bakes the
 *   planner's frozen long-term memory snapshot (re-read `listMemories` →
 *   re-bake) — the ONLY mid-session refresh boundary (team init + compress;
 *   FR-010). The refresh never throws (memory-snapshot.ts keeps the previous
 *   snapshot on failure — contract §5: memory unavailability must not abort
 *   the compression turn / the team run).
 *
 * The long-term memory (memory service / frozen snapshot) is never touched
 * by the channel summaries (FR-009 — the snapshot is separately re-baked at
 * this boundary, 039 T021).
 */

import { randomUUID } from "node:crypto";

import { AIMessage, RemoveMessage } from "@langchain/core/messages";
import type { BaseMessage } from "@langchain/core/messages";
import { REMOVE_ALL_MESSAGES } from "@langchain/langgraph";
import type { RunnableConfig } from "@langchain/core/runnables";

import type { ChatModel } from "../model-provider.js";
import type { MemoryClient } from "../memory-client.js";
import type { ChannelFrameEmitter } from "../session-team.js";
import { PLAYER_AGENT_NAME } from "./player.js";
import { PLANNER_AGENT_NAME } from "./planner.js";
import type { FrozenMemorySnapshot } from "./memory-snapshot.js";
import type { TeamStateValue } from "./state.js";

/** Dependencies of the compress node (all injected — DI seam). */
export interface CompressNodeDeps {
	/** The player's LLM — summarizes the player channel (research.md D1). */
	playerModel: ChatModel;
	/** The planner's LLM — summarizes the planner channel (research.md D1). */
	plannerModel: ChatModel;
	/**
	 * MemoryService gRPC client — re-reads the planner's long-term memory at
	 * the compression boundary (contract §2.4, survey D4).
	 */
	memoryClient: MemoryClient;
	/**
	 * The per-session frozen memory snapshot — refreshed here (contract §2.4:
	 * after the review, before END). One instance per session, shared with the
	 * planner node.
	 */
	frozenSnapshot: FrozenMemorySnapshot;
	/** The template path segment (memory resource scope, FR-012). */
	template: string;
	/** Session id (memory resource scope). */
	sessionId: string;
}

/**
 * Player-channel summary prompt (compression-contract.md §3 摘要提示词). The
 * serialized channel messages follow the "对话历史：" line; the trailing
 * "请输出摘要：" instruction is appended after them.
 */
const PLAYER_SUMMARY_PROMPT =
	"你是扫雷游戏的 player agent。以下是你之前若干局游戏的对话历史。\n" +
	"请概括关键信息，包括：已玩局数、胜负记录、使用过的策略与效果、关键决策与经验教训。\n" +
	"保持简洁但信息完整，使你在下一局游戏中能据此继续。\n\n" +
	"对话历史：\n";

/** Planner-channel summary prompt (compression-contract.md §3 摘要提示词). */
const PLANNER_SUMMARY_PROMPT =
	"你是扫雷团队的 planner（复盘规划者）。以下是你之前若干局游戏的复盘对话历史。\n" +
	"请概括关键信息，包括：已复盘局数、策略更新历史、关键观察与判断、策略效果评估。\n" +
	"保持简洁但信息完整，使你在下一次复盘中能据此继续。\n\n" +
	"对话历史：\n";

/**
 * Serialize a channel's `BaseMessage[]` into prompt text — one `[role]:
 * content` line per message, blank-line separated (compression-contract.md
 * §3 消息序列化).
 */
function serializeMessages(messages: BaseMessage[]): string {
	return messages
		.map((m) => {
			const role = m._getType();
			const content =
				typeof m.content === "string" ? m.content : JSON.stringify(m.content);
			return `[${role}]: ${content}`;
		})
		.join("\n\n");
}

/**
 * Generate the summary for ONE channel: invoke the channel's model with the
 * role prompt + serialized messages, validate the summary is meaningful, and
 * wrap it in an AIMessage whose `id` is a fresh UUID (the frameId dedup
 * anchor, data-model.md §3/§4).
 *
 * Throws on LLM error (propagates — FR-013) and on a blank summary (FR-012:
 * empty/whitespace summary counts as a compression failure). Content-block
 * responses are normalized to a string by extractTextContent, so only a
 * genuinely blank summary (trimmed text empty) triggers the abort.
 */
async function summarizeChannel(
	model: ChatModel,
	prompt: string,
	messages: BaseMessage[],
): Promise<{ content: string; message: AIMessage }> {
	const response = await model.invoke([
		{
			role: "human",
			content: prompt + serializeMessages(messages) + "\n\n请输出摘要：",
		},
	]);
	// Normalize the model response to a plain string. The OpenAI-compatible
	// adapters may return either a plain string OR the LangChain "standard
	// content blocks" shape (`[{type:"text",text}, ...]`, stamped with
	// `response_metadata.output_version: "v1"` by @langchain/core — the
	// content field then carries the blocks array). Extract the text blocks'
	// content so the summary is always a string (the emitted frame and the
	// channel AIMessage both need string content).
	const content = extractTextContent(response);
	if (content.trim().length === 0) {
		throw new Error(
			"compression failed: summary is blank (FR-012 — abort, no degrade)",
		);
	}
	return { content, message: new AIMessage({ id: randomUUID(), content }) };
}

/**
 * Extract the text content of a model response message, handling both the
 * plain-string form and the standard content-blocks array form
 * (`{type:"text", text}` blocks; reasoning/tool-call blocks contribute
 * nothing). A tool-call-only response yields "" — the caller rejects it as a
 * blank summary (FR-012).
 */
function extractTextContent(
	response: BaseMessage,
): string {
	if (typeof response.content === "string") {
		return response.content;
	}
	if (Array.isArray(response.content)) {
		return response.content
			.filter(
				(b): b is { type: "text"; text?: string } =>
					typeof b === "object" &&
					b !== null &&
					(b as { type?: string }).type === "text" &&
					typeof (b as { text?: unknown }).text === "string",
			)
			.map((b) => (b as { text: string }).text)
			.join("");
	}
	return "";
}

/**
 * Create the compress node function (compression-contract.md §3).
 *
 * @returns An async node `(state, config?) => Partial<TeamStateValue>`
 *   suitable for `StateGraph.addNode("compress", ...)`; the summary frames
 *   are emitted via `config?.configurable?.emitChannelFrame` (tasks.md 决策
 *   #1 — ChannelFrameEmitter from session-team.ts).
 */
export function createCompressNode(
	deps: CompressNodeDeps,
): (
	state: TeamStateValue,
	config?: RunnableConfig,
) => Promise<Partial<TeamStateValue>> {
	const { playerModel, plannerModel, memoryClient, frozenSnapshot, template, sessionId } =
		deps;

	return async (
		state: TeamStateValue,
		config?: RunnableConfig,
	): Promise<Partial<TeamStateValue>> => {
		const emitChannelFrame = config?.configurable?.emitChannelFrame as
			| ChannelFrameEmitter
			| undefined;

		const update: Partial<TeamStateValue> = {};

		// Generate BOTH channel summaries before emitting any frame (FR-015:
		// an empty channel is skipped — no summary message, no frame). If
		// either summary throws (FR-013 abort), no frame has been emitted yet,
		// so a failed compression never leaves an orphan frame whose frameId
		// matches no persisted message (reload dedup, data-model.md §4 /
		// research.md D9).
		const playerSummary =
			state.playerMessages.length > 0
				? await summarizeChannel(
						playerModel,
						PLAYER_SUMMARY_PROMPT,
						state.playerMessages,
					)
				: undefined;
		const plannerSummary =
			state.plannerMessages.length > 0
				? await summarizeChannel(
						plannerModel,
						PLANNER_SUMMARY_PROMPT,
						state.plannerMessages,
					)
				: undefined;

		// Both summaries succeeded — emit the live summary frames (FR-011;
		// frameId == message id, the desktop dedup anchor data-model.md §4 /
		// research.md D9) and build the clear-then-write channel updates.
		if (playerSummary) {
			if (emitChannelFrame) {
				emitChannelFrame(
					PLAYER_AGENT_NAME,
					playerSummary.content,
					playerSummary.message.id,
				);
			}
			update.playerMessages = [
				new RemoveMessage({ id: REMOVE_ALL_MESSAGES }),
				playerSummary.message,
			];
		}

		if (plannerSummary) {
			if (emitChannelFrame) {
				emitChannelFrame(
					PLANNER_AGENT_NAME,
					plannerSummary.content,
					plannerSummary.message.id,
				);
			}
			update.plannerMessages = [
				new RemoveMessage({ id: REMOVE_ALL_MESSAGES }),
				plannerSummary.message,
			];
		}

		// 039 US2 (T021, contract §2.4 / survey D4): the compression boundary
		// re-bakes the planner's frozen memory snapshot — re-read
		// `listMemories` → re-bake. This is the ONLY mid-session refresh
		// boundary (team init + compress; FR-010 — the review never refreshes).
		// The refresh itself never throws (memory-snapshot.ts keeps the
		// previous snapshot on failure — contract §5: memory unavailability
		// must not block the team run / abort the compression turn).
		await frozenSnapshot.refresh(memoryClient, template, sessionId);

		return update;
	};
}
