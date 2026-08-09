/**
 * team/instruction-node.ts — the init/compact scenario instruction nodes
 * (T025; `specs/039-planner-memory-calibration/contracts/team-graph-contract.md`
 * §2.3 — FR-015/FR-016/FR-019).
 *
 * `initInstruction`（team 初始化异步触发一次）与 `postCompactInstruction`
 * （compress 之后、END 之前）共享同一节点函数，以 `scenario` 参数区分
 * （FR-019 两场景拆分）：
 *
 * - **无游戏历史**：planner 仅依冻结记忆快照（input SystemMessage，纯内容 —
 *   FR-011）+ 场景 prompt 要求，产出**无 gameLog** 指令（LLM 决定是否调用
 *   `instruct_player`，R4 — 无强制检验）；prompt 措辞与 review 场景
 *   （"必要时才调用"）区分：init/compact 是"要求给指令"（contract §2.3）。
 * - **不触发 player invoke**：指令写入 `TeamState.playerMessages`（作为
 *   HumanMessage，与 review 场景同机制 — FR-017 的通道写回，见
 *   planner.ts），节点不激活 player。init 场景 turn 停在 initInstruction
 *   （条件边，R5）；compact 场景 turn 在 postCompactInstruction 后 END
 *   （与 037"压缩后自动停下"一致）。指令进入 player 通道后对
 *   ListMessages 可见（player tab 可展示），后续用户输入正常拼接 history
 *   即可消费，无需额外步骤。
 * - **R1 外部 buffer 中转**（contract §4）：工具把指令 content 暂存到
 *   configurable 提供的 `instructionBuffer`；节点在 `createAgent.invoke`
 *   返回后读暂存、由节点返回值写 `playerMessages`。
 * - **降级**（contract §6）：agent invoke 失败 → 记日志、跳过指令，不阻断
 *   team（init 不阻塞 `UpdateTeam` 物化）。
 *
 * 输入 = `[frozenSnapshot.toSystemMessage(), ...state.plannerMessages,
 * instructionRequest]`；写回时过滤 `planner-memory-snapshot` id（同 review
 * 节点，contract §3）。tools = 仅 `instruct_player`（不持 memory 工具 —
 * "仅依冻结快照"）。systemPrompt = 与 review planner 相同的**共享核心 base**
 * （`DEFAULT_PLANNER_BASE`，不追加配套段）；init/compact 场景的特殊性
 * （无游戏历史、请勿复盘游戏、请勿更新长期记忆）在 input request
 * （`buildInstructionRequest`）中表达。
 */

import { createAgent } from "langchain";
import { randomUUID } from "node:crypto";
import { HumanMessage } from "@langchain/core/messages";
import type { BaseMessage } from "@langchain/core/messages";
import type { RunnableConfig } from "@langchain/core/runnables";
import { warn } from "@dominion/common-js-logs";

import type { MessagePart } from "../../game_types/projects/game/MessagePart";
import { extractToolCalls } from "../llm";
import type { ChatModel } from "../model-provider";
import {
	PRIMARY_AGENT_NAME,
	type ChannelFrameEmitter,
} from "../session-team";
import type { TeamStateValue } from "./state";
import { PLANNER_MEMORY_SNAPSHOT_ID } from "./memory-snapshot";
import type { FrozenMemorySnapshot } from "./memory-snapshot";
import type { CreateAgentFn } from "./player";
import { DEFAULT_PLANNER_BASE, PLANNER_AGENT_NAME } from "./planner";
import { invokeAgentWithRetry } from "./agent-invoke";
import {
	buildInstructPlayerTool,
	type InstructionBuffer,
} from "./instruction-tool";

/** The two no-game-history scenarios (FR-019 — contract §2.3). */
export type InstructionScenario = "init" | "compact";

/** Dependencies of the instruction nodes (all injected — DI seam). */
export interface InstructionNodeDeps {
	/** The planner's LLM (from the TeamProfile, Batch 2 wiring). */
	model: ChatModel;
	/**
	 * The frozen long-term-memory snapshot (contract §3): the ONLY knowledge
	 * source of the instruction scenario ("仅依冻结记忆快照" — the init turn
	 * reads the team-init bake, the compact turn reads the compress-boundary
	 * refresh, T021). Injected as an input SystemMessage per invoke.
	 */
	frozenSnapshot: FrozenMemorySnapshot;
	/**
	 * The planner's base prompt from `SaoleiProfile.planner_prompt` (FR-034
	 * semantics A — empty string = unset = fall back to the SHARED template
	 * default `DEFAULT_PLANNER_BASE`, the same core base the review planner
	 * uses: the core prompt is consistent across the review and instruction
	 * agents — differentiation happens in the tool set (no memory tool here)
	 * and in the scenario request message (`buildInstructionRequest`, which
	 * carries the "无游戏历史/请勿复盘/请勿更新记忆" caveats). No skill body is
	 * appended here: the instruction scenario holds NO memory tool (memory
	 * skill guidance would be dead text).
	 */
	plannerBasePrompt: string;
	/** Optional createAgent override (DI seam, defaults to the real one). */
	createAgentFn?: CreateAgentFn;
}

/**
 * The instruction request message — the scenario-specific prompt that
 * REQUIRES (区别于 review 的"必要时才调用") the planner to produce a
 * no-game-history instruction (R4: the final tool call is still the LLM's
 * decision — no enforcement; contract §2.3).
 *
 * The shared base (`DEFAULT_PLANNER_BASE`) describes the planner's NORMAL
 * review mode ("复盘本局表现"/"调用 memory 工具" — the review agent's mode);
 * this request is where the init/compact scenario's special cases are
 * stated: no game history (so no review) and no memory update (the frozen
 * snapshot stays unchanged). The agent's tool set (only `instruct_player`,
 * no `memory`) enforces the same boundary.
 */
function buildInstructionRequest(scenario: InstructionScenario): BaseMessage {
	if (scenario === "init") {
		return new HumanMessage(
			"团队初始化：player 尚无游戏记录，也没有收到过任何策略指令。" +
				"当前无游戏历史，请勿复盘游戏，也请勿更新长期记忆。" +
				"请依据你的冻结快照长期记忆，给 player 一条开局策略指令，帮助它开始第一局游戏。" +
				"请调用 instruct_player 发送这条指令。",
		);
	}
	return new HumanMessage(
		"上下文刚被压缩：player 的指令历史已被清理，需要重新建立引导。" +
			"当前无游戏历史，请勿复盘游戏，也请勿更新长期记忆。" +
			"请依据刷新后的冻结快照长期记忆，给 player 一条新的策略指令。" +
			"请调用 instruct_player 发送这条指令。",
	);
}

/**
 * Ensure the message carries a stable id and return it (041 T006 — the
 * frameId == messageId dedup anchor,
 * specs/041-realtime-init-push/contracts/realtime-channel-contract.md §4 /
 * FR-004). Messages built
 * here (the write-back HumanMessage) or returned by the agent invoke MAY
 * arrive with `id` undefined (`@langchain/core` BaseMessage defaults to
 * undefined); LangGraph's `messagesStateReducer` would then mint a DIFFERENT
 * id at checkpoint write time
 * (`@langchain/langgraph` dist/graph/messages_reducer.js — "Ensures all
 * messages have unique, stable IDs"), leaving the real-time frame's frameId
 * unmatched against the persisted message. Idempotent: an existing id (the
 * agent invoke already stamps input/output messages in place — verified
 * empirically against `langchain` createAgent) is kept. Same pattern as the
 * compress node's summary messages (`compress.ts:158` —
 * `new AIMessage({ id: randomUUID(), ... })`).
 */
function ensureMessageId(msg: BaseMessage): string {
	if (!msg.id) msg._updateId(randomUUID());
	return msg.id as string;
}

/**
 * Create the init/compact instruction node function (contract §2.3) — one
 * factory for BOTH scenarios (FR-019 共享节点函数), differentiated by the
 * `scenario` prompt.
 *
 * @returns An async node `(state, config?) => Partial<TeamStateValue>`
 *   suitable for `StateGraph.addNode("initInstruction" | "postCompactInstruction", ...)`.
 */
export function createInstructionNode(
	deps: InstructionNodeDeps,
	scenario: InstructionScenario,
): (
	state: TeamStateValue,
	config?: RunnableConfig,
) => Promise<Partial<TeamStateValue>> {
	const { frozenSnapshot } = deps;
	const createAgentFn = deps.createAgentFn ?? createAgent;

	// FR-034 semantics A: the base prompt is the profile's planner_prompt
	// when non-empty, else the SHARED template default `DEFAULT_PLANNER_BASE`
	// — the same core base the review planner uses (the base is NOT modified
	// for the init/compact scenarios; their special cases — 无游戏历史、请勿
	// 复盘、请勿更新记忆 — live in the input request message,
	// `buildInstructionRequest`). No skill body / tool description section:
	// the instruction agent's tool set is exactly `instruct_player`
	// (contract §2.3 — "仅依冻结快照"，不持 memory 工具).
	const systemPrompt =
		deps.plannerBasePrompt !== "" ? deps.plannerBasePrompt : DEFAULT_PLANNER_BASE;
	const instructionAgent = createAgentFn({
		model: deps.model,
		tools: [buildInstructPlayerTool()],
		systemPrompt,
	});

	return async (
		state: TeamStateValue,
		config?: RunnableConfig,
	): Promise<Partial<TeamStateValue>> => {
		const request = buildInstructionRequest(scenario);

		// 041 Phase 3 (T005/T006 — specs/041-realtime-init-push/research.md
		// D2, specs/041-realtime-init-push/contracts/
		// realtime-channel-contract.md §2.2): the
		// channel-frame emitter is installed in BOTH runners — the
		// user-turn runner (runTeamTurn, compact scenario) and the init-turn
		// runner (runInitTurn, init scenario — research.md D1/D2) — so the
		// produced instruction frames push through the stream-bound display
		// sink whenever the desktop is connected. The three frames (planner
		// request / planner tool-call response / player write-back) are
		// emitted AFTER the invoke resolves, in production order — a planner
		// failure degrades BEFORE any frame is emitted (contract §2.3: no
		// orphan frame whose frameId matches no persisted message;
		// research.md D9). The desktop's typing indicator is NOT driven by
		// these frames (init emits no wait/status — contract §2.4; the
		// probe reports IDLE during the init, session-team.ts isRunning).
		const emitChannelFrame = config?.configurable?.emitChannelFrame as
			| ChannelFrameEmitter
			| undefined;

		const input: BaseMessage[] = [
			// 冻结记忆快照作为首条 input SystemMessage（纯内容, FR-011）—
			// init 读首次烘焙、compact 读压缩边界刷新（T021）。
			frozenSnapshot.toSystemMessage(),
			...state.plannerMessages,
			request,
		];

		let result: { messages: BaseMessage[] };
		try {
			result = await invokeAgentWithRetry(instructionAgent, input, config);
		} catch (err) {
			// specs/039-planner-memory-calibration/contracts/
			// team-graph-contract.md §6 降级：planner model 不可用 → 记日志、
			// 跳过指令，不
			// 阻断 team（init 不阻塞 UpdateTeam 物化；压缩后 turn 仍正常
			// END）。不写 plannerMessages（失败无产出）。041 (contract §2.3):
			// 不发任何帧（包括 request 帧）— 帧的发射统一在 invoke 成功后。
			const message = err instanceof Error ? err.message : String(err);
			warn("instruction node failed; skipping instruction", {
				scenario,
				error: message,
			});
			return {};
		}

		// R1（contract §4）：invoke 返回后读外部 buffer 暂存，重置槽；有
		// 指令则写 `playerMessages`（与 review 节点同一通道写回机制 —
		// planner.ts，指令作为 HumanMessage 进入 player 对话流）。无指令
		// 则不写该字段。写 `playerMessages` 不触发 player invoke（节点仅
		// 返回通道更新，图路由由条件边决定）。
		const buffer = config?.configurable?.instructionBuffer as
			| InstructionBuffer
			| undefined;
		const instruction = buffer?.content ?? null;
		if (buffer) {
			buffer.content = null;
		}

		// 041 Phase 3 (T006 — specs/041-realtime-init-push/research.md
		// D2/D3, specs/041-realtime-init-push/contracts/
		// realtime-channel-contract.md §2.2 / specs/041-realtime-init-push/
		// data-model.md §3): 发射本次 invoke 新产出消息的 display 帧，每帧
		// frameId == 产生消息的 id（dedup anchor，
		// specs/041-realtime-init-push/contracts/
		// realtime-channel-contract.md §4 / FR-004）：
		//
		// (a) planner request 帧 — 场景 prompt（HumanMessage，也持久化在
		// plannerMessages，research.md D3 note），agent=planner role=USER；
		// (b) planner response 帧 — 含 instruct_player tool_call 的
		// AIMessage，以 toolCall MessagePart 发射（faithful mirroring —
		// 与 ListMessages 的转换一致，handler.ts:700-711），agent=planner
		// role=AGENT；
		// (c) player write-back 帧 — 指令文本（HumanMessage 写回
		// playerMessages），agent=player role=USER。
		if (emitChannelFrame) {
			const content =
				typeof request.content === "string" ? request.content : "";
			if (content) {
				emitChannelFrame(
					PLANNER_AGENT_NAME,
					content,
					ensureMessageId(request),
					"MESSAGE_ROLE_USER",
				);
			}
			const response = result.messages.find(
				(m: BaseMessage) =>
					m._getType() === "ai" && extractToolCalls(m).length > 0,
			);
			if (response) {
				const parts: MessagePart[] = [];
				for (const call of extractToolCalls(response)) {
					parts.push({
						toolCall: {
							toolId: call.id ?? "",
							name: call.name ?? "",
							argsJson: JSON.stringify(call.args ?? {}),
						},
					});
				}
				emitChannelFrame(
					PLANNER_AGENT_NAME,
					parts,
					ensureMessageId(response),
					"MESSAGE_ROLE_AGENT",
				);
			}
		}

		const update: Partial<TeamStateValue> = {
			// 过滤快照 SystemMessage（不进 plannerMessages 短期通道，contract
			// §3 — 同 review 节点模式）。
			plannerMessages: result.messages.filter(
				(m: BaseMessage) => m.id !== PLANNER_MEMORY_SNAPSHOT_ID,
			),
		};
		if (instruction !== null) {
			// 指令直接进入 player 通道（HumanMessage），随后续输入正常拼接
			// history（无需 pendingInstruction 中间槽；对 ListMessages 可见）。
			// 041 (specs/041-realtime-init-push/contracts/
			// realtime-channel-contract.md §2.2 / specs/041-realtime-init-push/
			// data-model.md §3.3): 同一消息作为 (c) 帧发射，
			// frameId = 写回消息的 id（ensureMessageId 保证与 checkpoint 中
			// 持久化后的 id 一致 —
			// specs/041-realtime-init-push/contracts/
			// realtime-channel-contract.md §4）。
			const writeBack = new HumanMessage(instruction);
			update.playerMessages = [writeBack];
			if (emitChannelFrame) {
				emitChannelFrame(
					PRIMARY_AGENT_NAME,
					instruction,
					ensureMessageId(writeBack),
					"MESSAGE_ROLE_USER",
				);
			}
		}
		return update;
	};
}
