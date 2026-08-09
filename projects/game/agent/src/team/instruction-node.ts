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
import { HumanMessage } from "@langchain/core/messages";
import type { BaseMessage } from "@langchain/core/messages";
import type { RunnableConfig } from "@langchain/core/runnables";
import { warn } from "@dominion/common-js-logs";

import type { ChatModel } from "../model-provider";
import type { ChannelFrameEmitter } from "../session-team";
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

		// typing-state 协调（contract §6 / research.md D5）：将场景 prompt
		// 作为实时 channel 帧发出（agent=planner），与 review 节点的
		// reviewInput 帧同一模式（037 — configurable 注入的
		// emitChannelFrame）。仅 compact 场景实际生效：postCompactInstruction
		// 运行在 user turn 内（runTeamTurn 注入 emitChannelFrame）；
		// init 场景的 runInitTurn 不注入 emitChannelFrame（init 发生在
		// desktop Connect 之前且 turnLoopEmit 仅在首次 submit 时赋值，帧
		// 不可达——desktop 的 typing 由 Connect status probe 驱动，见
		// session-team.ts runInitTurn），故此处对 init 自然跳过。
		const emitChannelFrame = config?.configurable?.emitChannelFrame as
			| ChannelFrameEmitter
			| undefined;
		if (emitChannelFrame) {
			const content =
				typeof request.content === "string" ? request.content : "";
			if (content) {
				emitChannelFrame(
					PLANNER_AGENT_NAME,
					content,
					undefined,
					"MESSAGE_ROLE_USER",
				);
			}
		}

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
			// contract §6 降级：planner model 不可用 → 记日志、跳过指令，不
			// 阻断 team（init 不阻塞 UpdateTeam 物化；压缩后 turn 仍正常
			// END）。不写 plannerMessages（失败无产出）。
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
			update.playerMessages = [new HumanMessage(instruction)];
		}
		return update;
	};
}
